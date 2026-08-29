package madeleine

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPublishEpisodeFinalizesCaptureAtomically(t *testing.T) {
	t.Parallel()

	store := openTestStore(t, t.TempDir())
	defer store.Close()
	root := newTestGitRepository(t, "")
	capture := sealTestCaptureWithPaths(t, store, root, "publish", "z.go", "a.go")
	sealed, err := store.GetCapture(context.Background(), capture.ID)
	if err != nil {
		t.Fatal(err)
	}

	episode, err := store.PublishEpisode(context.Background(), PublishEpisodeRequest{
		CaptureID: capture.ID,
		L1:        "  Added atomic Episode publication.  ",
		L2:        "\nPublished summaries and exact paths in one transaction.\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if episode.ID == "" || episode.CaptureID != capture.ID || episode.RepositoryID != capture.RepositoryID {
		t.Fatalf("Episode identity = %#v", episode)
	}
	if episode.ConversationID != capture.ConversationID || episode.ConversationKey != capture.ConversationKey {
		t.Fatalf("Episode Conversation = %#v, want %#v", episode.ConversationKey, capture.ConversationKey)
	}
	if episode.Harness != HarnessPi || episode.TranscriptRef != capture.TranscriptRef {
		t.Fatalf("Episode harness/transcript = %q/%q", episode.Harness, episode.TranscriptRef)
	}
	if episode.StartCursor != capture.StartCursor || episode.EndCursor != sealed.EndCursor {
		t.Fatalf("Episode cursors = %q/%q", episode.StartCursor, episode.EndCursor)
	}
	if !episode.StartedAt.Equal(capture.StartedAt) || !episode.EndedAt.Equal(*sealed.EndedAt) {
		t.Fatalf("Episode times = %v/%v, want %v/%v", episode.StartedAt, episode.EndedAt, capture.StartedAt, sealed.EndedAt)
	}
	if episode.CreatedAt.IsZero() || episode.CreatedAt.Location() != time.UTC {
		t.Fatalf("Episode creation time = %v", episode.CreatedAt)
	}
	if episode.L1 != "Added atomic Episode publication." || episode.L2 != "Published summaries and exact paths in one transaction." {
		t.Fatalf("Episode summaries = %q/%q", episode.L1, episode.L2)
	}
	if !reflect.DeepEqual(episode.Paths, []string{"a.go", "z.go"}) {
		t.Fatalf("Episode paths = %v", episode.Paths)
	}

	finalized, err := store.GetCapture(context.Background(), capture.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finalized.Status != CaptureStatusFinalized || finalized.EpisodeID != episode.ID {
		t.Fatalf("finalized Capture = %#v", finalized)
	}
	var rawPathCount int
	if err := store.db.QueryRow(
		"SELECT COUNT(*) FROM capture_paths WHERE capture_id = ?", capture.ID,
	).Scan(&rawPathCount); err != nil {
		t.Fatal(err)
	}
	if rawPathCount != 0 {
		t.Fatalf("raw Capture path count = %d, want 0", rawPathCount)
	}
	draft, err := store.SealCapture(context.Background(), SealCaptureRequest{CaptureID: capture.ID, EndCursor: "ignored"})
	if err != nil {
		t.Fatal(err)
	}
	if draft.Status != CaptureStatusFinalized || draft.EpisodeID != episode.ID {
		t.Fatalf("finalized seal draft = %#v", draft)
	}
}

func TestPublishEpisodeBatchesLargePathSets(t *testing.T) {
	t.Parallel()

	store := openTestStore(t, t.TempDir())
	defer store.Close()
	root := newTestGitRepository(t, "")
	capture := startTestCapture(t, store, root, "large-publication")
	paths := testEpisodePaths(11_000)
	insertTestCapturePaths(t, store, capture.ID, paths)
	if _, err := store.SealCapture(context.Background(), SealCaptureRequest{
		CaptureID: capture.ID, EndCursor: "end",
	}); err != nil {
		t.Fatal(err)
	}

	episode, err := store.PublishEpisode(context.Background(), PublishEpisodeRequest{
		CaptureID: capture.ID, L1: "Large path set", L2: "Published in bounded batches.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(episode.Paths) != len(paths) {
		t.Fatalf("Episode path count = %d, want %d", len(episode.Paths), len(paths))
	}
	if episode.Paths[0] != paths[0] || episode.Paths[len(episode.Paths)-1] != paths[len(paths)-1] {
		t.Fatalf("Episode path bounds = %q/%q, want %q/%q",
			episode.Paths[0], episode.Paths[len(episode.Paths)-1], paths[0], paths[len(paths)-1])
	}
}

func TestPublishEpisodeRetriesAreIdempotent(t *testing.T) {
	t.Parallel()

	store := openTestStore(t, t.TempDir())
	defer store.Close()
	capture := sealTestCaptureWithPaths(t, store, newTestGitRepository(t, ""), "retry", "file.go")
	request := PublishEpisodeRequest{CaptureID: capture.ID, L1: "Summary", L2: "Detail"}
	first, err := store.PublishEpisode(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	request.L1 = " Summary "
	request.L2 = "\nDetail\n"
	second, err := store.PublishEpisode(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(second, first) {
		t.Fatalf("retry Episode = %#v, want %#v", second, first)
	}

	for _, conflicting := range []PublishEpisodeRequest{
		{CaptureID: capture.ID, L1: "Different", L2: "Detail"},
		{CaptureID: capture.ID, L1: "Summary", L2: "Different"},
	} {
		if _, err := store.PublishEpisode(context.Background(), conflicting); !errors.Is(err, ErrConflict) {
			t.Errorf("conflicting retry error = %v, want ErrConflict", err)
		}
	}
	var episodeCount int
	if err := store.db.QueryRow(
		"SELECT COUNT(*) FROM episodes WHERE source_capture_id = ?", capture.ID,
	).Scan(&episodeCount); err != nil {
		t.Fatal(err)
	}
	if episodeCount != 1 {
		t.Fatalf("Episode count = %d, want 1", episodeCount)
	}
}

func TestPublishEpisodeRollsBackOnInsertionFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		trigger string
		paths   []string
	}{
		{
			name: "after Episode insertion",
			trigger: `CREATE TRIGGER fail_publication AFTER INSERT ON episodes
				BEGIN SELECT RAISE(ABORT, 'injected Episode failure'); END`,
			paths: []string{"a.go", "z.go"},
		},
		{
			name: "during second path batch",
			trigger: `CREATE TRIGGER fail_publication AFTER INSERT ON episode_files
				WHEN NEW.path = 'generated/00300.go'
				BEGIN SELECT RAISE(ABORT, 'injected path failure'); END`,
			paths: testEpisodePaths(301),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			store := openTestStore(t, t.TempDir())
			defer store.Close()
			capture := startTestCapture(t, store, newTestGitRepository(t, ""), test.name)
			insertTestCapturePaths(t, store, capture.ID, test.paths)
			if _, err := store.SealCapture(context.Background(), SealCaptureRequest{
				CaptureID: capture.ID, EndCursor: "end",
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := store.db.Exec(test.trigger); err != nil {
				t.Fatal(err)
			}

			_, err := store.PublishEpisode(context.Background(), PublishEpisodeRequest{
				CaptureID: capture.ID, L1: "Summary", L2: "Detail",
			})
			if err == nil {
				t.Fatal("PublishEpisode succeeded despite injected failure")
			}

			var episodeCount, episodePathCount, capturePathCount int
			if err := store.db.QueryRow("SELECT COUNT(*) FROM episodes").Scan(&episodeCount); err != nil {
				t.Fatal(err)
			}
			if err := store.db.QueryRow("SELECT COUNT(*) FROM episode_files").Scan(&episodePathCount); err != nil {
				t.Fatal(err)
			}
			if err := store.db.QueryRow(
				"SELECT COUNT(*) FROM capture_paths WHERE capture_id = ?", capture.ID,
			).Scan(&capturePathCount); err != nil {
				t.Fatal(err)
			}
			got, err := store.GetCapture(context.Background(), capture.ID)
			if err != nil {
				t.Fatal(err)
			}
			if episodeCount != 0 || episodePathCount != 0 || capturePathCount != len(test.paths) {
				t.Fatalf("row counts after rollback = Episodes %d, Episode paths %d, Capture paths %d", episodeCount, episodePathCount, capturePathCount)
			}
			if got.Status != CaptureStatusPendingSummary || got.EpisodeID != "" {
				t.Fatalf("Capture after rollback = %#v", got)
			}
		})
	}
}

func TestPublishEpisodeValidatesStateAndSummaries(t *testing.T) {
	t.Parallel()

	store := openTestStore(t, t.TempDir())
	defer store.Close()
	root := newTestGitRepository(t, "")
	openCapture := startTestCapture(t, store, root, "open-publication")
	if _, err := store.PublishEpisode(context.Background(), PublishEpisodeRequest{
		CaptureID: openCapture.ID, L1: "Summary", L2: "Detail",
	}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("open Capture error = %v, want ErrInvalidState", err)
	}
	if _, err := store.PublishEpisode(context.Background(), PublishEpisodeRequest{
		CaptureID: "missing", L1: "Summary", L2: "Detail",
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing Capture error = %v, want ErrNotFound", err)
	}

	capture := sealTestCaptureWithPaths(t, store, root, "summary-validation", "file.go")
	for _, request := range []PublishEpisodeRequest{
		{CaptureID: capture.ID, L1: " ", L2: "Detail"},
		{CaptureID: capture.ID, L1: "Summary", L2: "\n\t"},
		{CaptureID: capture.ID, L1: strings.Repeat("界", maxL1Characters+1), L2: "Detail"},
	} {
		if _, err := store.PublishEpisode(context.Background(), request); !errors.Is(err, ErrInvalidState) {
			t.Errorf("invalid summary error = %v, want ErrInvalidState", err)
		}
	}

	episode, err := store.PublishEpisode(context.Background(), PublishEpisodeRequest{
		CaptureID: capture.ID,
		L1:        strings.Repeat("界", maxL1Characters),
		L2:        "Unicode boundary accepted",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(episode.L1)) != maxL1Characters {
		t.Fatalf("L1 length = %d, want %d", len([]rune(episode.L1)), maxL1Characters)
	}
}

func testEpisodePaths(count int) []string {
	paths := make([]string, count)
	for index := range paths {
		paths[index] = fmt.Sprintf("generated/%05d.go", index)
	}
	return paths
}

func insertTestCapturePaths(t *testing.T, store *testService, captureID CaptureID, paths []string) {
	t.Helper()
	transaction, err := store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	statement, err := transaction.Prepare(`
		INSERT INTO capture_paths(capture_id, path, first_seen_at, last_seen_at)
		VALUES (?, ?, ?, ?)`)
	if err != nil {
		t.Fatal(err)
	}
	timestamp := utcTimestamp()
	for _, path := range paths {
		if _, err := statement.Exec(captureID, path, timestamp, timestamp); err != nil {
			t.Fatal(err)
		}
	}
	if err := statement.Close(); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
}

func sealTestCaptureWithPaths(t *testing.T, store *testService, root, externalID string, paths ...string) Capture {
	t.Helper()
	capture := startTestCapture(t, store, root, externalID)
	for _, path := range paths {
		if err := store.RecordWrite(context.Background(), RecordWriteRequest{CaptureID: capture.ID, Path: path}); err != nil {
			t.Fatalf("RecordWrite(%q): %v", path, err)
		}
	}
	if _, err := store.SealCapture(context.Background(), SealCaptureRequest{
		CaptureID: capture.ID, EndCursor: "end",
	}); err != nil {
		t.Fatalf("SealCapture: %v", err)
	}
	return capture
}
