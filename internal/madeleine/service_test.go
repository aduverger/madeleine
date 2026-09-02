package madeleine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestServiceEndToEnd(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := newTestGitRepository(t, "")
	service, err := Open(ctx, Options{Home: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()

	repository, err := service.ResolveRepository(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	capture, err := service.StartCapture(ctx, StartCaptureRequest{
		RepositoryRoot:  root,
		ConversationKey: ConversationKey{Harness: HarnessPi, ExternalID: "service-e2e"},
		StartCursor:     "start",
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "tracked.txt")
	if err := os.WriteFile(path, []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := service.RecordWrite(ctx, RecordWriteRequest{CaptureID: capture.ID, Path: "tracked.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SealCapture(ctx, SealCaptureRequest{
		CaptureID: capture.ID, EndCursor: "end", Transcript: testTranscriptInput(),
	}); err != nil {
		t.Fatal(err)
	}
	episode, err := service.PublishEpisode(ctx, PublishEpisodeRequest{
		CaptureID:       capture.ID,
		L1:              "Updated the tracked file.",
		L2:              "The service recorded, sealed, and published the change.",
		CompactEvidence: "Service evidence",
	})
	if err != nil {
		t.Fatal(err)
	}
	contexts, err := service.ContextForPaths(ctx, ContextRequest{
		RepositoryRoot: root,
		Paths:          []string{"tracked.txt"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if repository.ID == "" || len(contexts) != 1 || len(contexts[0].Episodes) != 1 ||
		contexts[0].Episodes[0].EpisodeID != episode.ID {
		t.Fatalf("repository = %#v, contexts = %#v, Episode = %#v", repository, contexts, episode)
	}
}
