package madeleine_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/aduverger/madeleine"
)

func TestPublicStoreFacade(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	runGit(t, root, "init", "--quiet")
	writeFile(t, root, "tracked.txt", "initial\n")
	runGit(t, root, "add", "tracked.txt")
	runGit(t, root, "-c", "user.name=Madeleine Test", "-c", "user.email=test@example.com",
		"commit", "--quiet", "-m", "initial")

	discovered, err := madeleine.ResolveRepository(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	store, err := madeleine.Open(ctx, madeleine.Options{Home: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	persisted, err := store.ResolveRepository(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.ID == "" || persisted.WorktreeRoot != discovered.WorktreeRoot {
		t.Fatalf("persisted repository = %#v, discovered = %#v", persisted, discovered)
	}

	key := madeleine.ConversationKey{Harness: madeleine.HarnessPi, ExternalID: "public-facade"}
	capture, err := store.StartCapture(ctx, madeleine.StartCaptureRequest{
		RepositoryRoot: root, ConversationKey: key, TranscriptRef: "session.jsonl", StartCursor: "start",
	})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "tracked.txt", "modified\n")
	if err := store.RecordWrite(ctx, madeleine.RecordWriteRequest{
		CaptureID: capture.ID, Path: "tracked.txt",
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetCapture(ctx, capture.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ConversationKey != key || loaded.TranscriptRef != "session.jsonl" {
		t.Fatalf("loaded Capture = %#v", loaded)
	}
	pending, err := store.ListPendingCaptures(ctx, madeleine.PendingCaptureQuery{
		RepositoryRoot: root, ConversationKey: &key,
	})
	if err != nil || len(pending) != 1 || pending[0].ID != capture.ID {
		t.Fatalf("pending Captures = %#v, error = %v", pending, err)
	}

	draft, err := store.SealCapture(ctx, madeleine.SealCaptureRequest{CaptureID: capture.ID, EndCursor: "end"})
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.Paths) != 1 || draft.Paths[0] != "tracked.txt" {
		t.Fatalf("finalization draft = %#v", draft)
	}
	episode, err := store.PublishEpisode(ctx, madeleine.PublishEpisodeRequest{
		CaptureID: capture.ID, L1: "Updated the tracked file.", L2: "The tracked file was changed through the public API.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if episode.CaptureID != capture.ID || episode.ConversationKey != key {
		t.Fatalf("published Episode = %#v", episode)
	}

	contexts, err := store.ContextForPaths(ctx, madeleine.ContextRequest{
		RepositoryRoot: root, Paths: []string{"tracked.txt"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(contexts) != 1 || len(contexts[0].Episodes) != 1 ||
		contexts[0].Episodes[0].EpisodeID != episode.ID {
		t.Fatalf("file context = %#v", contexts)
	}
	detail, err := store.GetEpisode(ctx, madeleine.EpisodeRequest{RepositoryRoot: root, EpisodeID: episode.ID})
	if err != nil {
		t.Fatal(err)
	}
	if detail.EpisodeID != episode.ID || detail.ConversationKey != key {
		t.Fatalf("Episode detail = %#v", detail)
	}
}

func writeFile(t *testing.T, root, path, contents string) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, root string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = root
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", arguments, err, output)
	}
}
