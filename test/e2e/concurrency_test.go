package e2e

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/aduverger/madeleine/internal/madeleine"
)

func TestConcurrentReadersWritersAndPublishers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := newGitRepository(t)
	service, err := madeleine.Open(ctx, madeleine.Options{Home: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()

	const agents = 20
	readerPaths := make([]string, agents)
	writerCaptures := make([]madeleine.Capture, agents)
	publisherDrafts := make([]madeleine.FinalizationDraft, agents)
	for index := range agents {
		readerPath := fmt.Sprintf("reader/%03d.go", index)
		readerPaths[index] = readerPath
		publishEpisode(t, ctx, service, repository, fmt.Sprintf("reader-%03d", index), readerPath)

		writerCaptures[index] = startCapture(
			t, ctx, service, repository, fmt.Sprintf("writer-%03d", index),
		)

		publisherCapture := startCapture(
			t, ctx, service, repository, fmt.Sprintf("publisher-%03d", index),
		)
		path := fmt.Sprintf("publisher/%03d.go", index)
		if err := service.RecordWrite(ctx, madeleine.RecordWriteRequest{
			CaptureID: publisherCapture.ID,
			Path:      path,
		}); err != nil {
			t.Fatal(err)
		}
		publisherDrafts[index], err = service.SealCapture(ctx, madeleine.SealCaptureRequest{
			CaptureID:  publisherCapture.ID,
			EndCursor:  "end",
			Transcript: transcriptForPath(path),
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	start := make(chan struct{})
	errors := make(chan error, agents*3)
	var workers sync.WaitGroup
	for index := range agents {
		index := index
		workers.Add(3)
		go func() {
			defer workers.Done()
			<-start
			for range 20 {
				contexts, err := service.ContextForPaths(ctx, madeleine.ContextRequest{
					RepositoryRoot: repository,
					Paths:          []string{readerPaths[index]},
				})
				if err != nil {
					errors <- err
					return
				}
				if len(contexts) != 1 || len(contexts[0].Episodes) != 1 {
					errors <- fmt.Errorf("reader %d returned incomplete context", index)
					return
				}
			}
		}()
		go func() {
			defer workers.Done()
			<-start
			for pathIndex := range 10 {
				path := fmt.Sprintf("writer/%03d/%03d.go", index, pathIndex)
				if err := service.RecordWrite(ctx, madeleine.RecordWriteRequest{
					CaptureID: writerCaptures[index].ID,
					Path:      path,
				}); err != nil {
					errors <- err
					return
				}
			}
		}()
		go func() {
			defer workers.Done()
			<-start
			_, err := service.PublishEpisode(ctx, madeleine.PublishEpisodeRequest{
				CaptureID:       publisherDrafts[index].CaptureID,
				L1:              fmt.Sprintf("Published agent %d", index),
				L2:              "Concurrent publication test",
				CompactEvidence: fmt.Sprintf("publisher evidence %d", index),
			})
			if err != nil {
				errors <- err
			}
		}()
	}
	close(start)
	workers.Wait()
	close(errors)
	for err := range errors {
		t.Errorf("concurrent operation: %v", err)
	}
	if t.Failed() {
		return
	}

	for index, capture := range writerCaptures {
		path := fmt.Sprintf("writer/%03d/%03d.go", index, 9)
		draft, err := service.SealCapture(ctx, madeleine.SealCaptureRequest{
			CaptureID:  capture.ID,
			EndCursor:  "end",
			Transcript: transcriptForPath(path),
		})
		if err != nil {
			t.Fatalf("seal writer %d: %v", index, err)
		}
		if len(draft.Paths) != 10 {
			t.Errorf("writer %d retained %d paths, want 10", index, len(draft.Paths))
		}
	}
}

func BenchmarkConcurrentAgents(b *testing.B) {
	for _, agents := range []int{1, 10, 100, 500} {
		b.Run(fmt.Sprintf("agents-%d", agents), func(b *testing.B) {
			benchmarkConcurrentAgents(b, agents)
		})
	}
}

func benchmarkConcurrentAgents(b *testing.B, agents int) {
	ctx := context.Background()
	repository := newGitRepository(b)
	service, err := madeleine.Open(ctx, madeleine.Options{Home: b.TempDir()})
	if err != nil {
		b.Fatal(err)
	}
	defer service.Close()

	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		captures := make([]madeleine.Capture, agents)
		for index := range agents {
			captures[index] = startCapture(
				b,
				ctx,
				service,
				repository,
				fmt.Sprintf("benchmark-%d-%d", iteration, index),
			)
		}
		var workers sync.WaitGroup
		workers.Add(agents)
		for index, capture := range captures {
			index, capture := index, capture
			go func() {
				defer workers.Done()
				path := fmt.Sprintf("benchmark/%d/%d.go", iteration, index)
				if err := service.RecordWrite(ctx, madeleine.RecordWriteRequest{
					CaptureID: capture.ID,
					Path:      path,
				}); err != nil {
					b.Error(err)
					return
				}
				draft, err := service.SealCapture(ctx, madeleine.SealCaptureRequest{
					CaptureID:  capture.ID,
					EndCursor:  "end",
					Transcript: transcriptForPath(path),
				})
				if err != nil {
					b.Error(err)
					return
				}
				if _, err := service.PublishEpisode(ctx, madeleine.PublishEpisodeRequest{
					CaptureID:       draft.CaptureID,
					L1:              "Benchmark publication",
					L2:              "Concurrent benchmark publication",
					CompactEvidence: "benchmark evidence",
				}); err != nil {
					b.Error(err)
					return
				}
				contexts, err := service.ContextForPaths(ctx, madeleine.ContextRequest{
					RepositoryRoot: repository,
					Paths:          []string{path},
				})
				if err != nil {
					b.Error(err)
					return
				}
				if len(contexts) != 1 || len(contexts[0].Episodes) != 1 {
					b.Errorf("agent %d returned incomplete context", index)
				}
			}()
		}
		workers.Wait()
	}
}

func publishEpisode(
	t testing.TB,
	ctx context.Context,
	service *madeleine.Service,
	repository, externalID, path string,
) {
	t.Helper()
	capture := startCapture(t, ctx, service, repository, externalID)
	if err := service.RecordWrite(ctx, madeleine.RecordWriteRequest{
		CaptureID: capture.ID,
		Path:      path,
	}); err != nil {
		t.Fatal(err)
	}
	draft, err := service.SealCapture(ctx, madeleine.SealCaptureRequest{
		CaptureID:  capture.ID,
		EndCursor:  "end",
		Transcript: transcriptForPath(path),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.PublishEpisode(ctx, madeleine.PublishEpisodeRequest{
		CaptureID:       draft.CaptureID,
		L1:              "Reader fixture",
		L2:              "Published before concurrent lookup",
		CompactEvidence: "reader evidence",
	}); err != nil {
		t.Fatal(err)
	}
}

func startCapture(
	t testing.TB,
	ctx context.Context,
	service *madeleine.Service,
	repository, externalID string,
) madeleine.Capture {
	t.Helper()
	capture, err := service.StartCapture(ctx, madeleine.StartCaptureRequest{
		RepositoryRoot: repository,
		ConversationKey: madeleine.ConversationKey{
			Harness:    madeleine.HarnessPi,
			ExternalID: externalID,
		},
		StartCursor: "start",
	})
	if err != nil {
		t.Fatal(err)
	}
	return capture
}

func transcriptForPath(path string) *madeleine.TranscriptInput {
	return &madeleine.TranscriptInput{
		FormatVersion: 1,
		Entries: []madeleine.TranscriptEntry{{
			Kind:      madeleine.TranscriptEntryMutation,
			Operation: "write",
			Path:      path,
			Status:    "success",
		}},
	}
}

func newGitRepository(t testing.TB) string {
	t.Helper()
	repository := t.TempDir()
	command := exec.Command("git", "init")
	command.Dir = repository
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	command = exec.Command("git", "rev-parse", "--show-toplevel")
	command.Dir = repository
	output, err := command.Output()
	if err != nil {
		t.Fatalf("git root: %v", err)
	}
	return filepath.Clean(string(output[:len(output)-1]))
}
