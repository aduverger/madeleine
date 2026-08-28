package madeleine

import (
	"context"
	"errors"
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
	}{
		{
			name: "after Episode insertion",
			trigger: `CREATE TRIGGER fail_publication AFTER INSERT ON episodes
				BEGIN SELECT RAISE(ABORT, 'injected Episode failure'); END`,
		},
		{
			name: "after path insertion",
			trigger: `CREATE TRIGGER fail_publication AFTER INSERT ON episode_files
				WHEN NEW.path = 'z.go'
				BEGIN SELECT RAISE(ABORT, 'injected path failure'); END`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			store := openTestStore(t, t.TempDir())
			defer store.Close()
			capture := sealTestCaptureWithPaths(t, store, newTestGitRepository(t, ""), test.name, "a.go", "z.go")
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
			if episodeCount != 0 || episodePathCount != 0 || capturePathCount != 2 {
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

func TestEpisodeSchemaIndexes(t *testing.T) {
	t.Parallel()

	store := openTestStore(t, t.TempDir())
	defer store.Close()
	for _, index := range []string{
		"episodes_repository_ended_id_idx",
		"episode_files_repository_path_episode_idx",
	} {
		if !databaseObjectExists(t, store.db, "index", index) {
			t.Errorf("index %q does not exist", index)
		}
	}
}

func sealTestCaptureWithPaths(t *testing.T, store *Store, root, externalID string, paths ...string) Capture {
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
