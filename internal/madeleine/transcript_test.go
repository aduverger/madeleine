package madeleine

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func TestTranscriptSealPersistsEntriesAndPagesRawView(t *testing.T) {
	t.Parallel()

	service := openTestStore(t, t.TempDir())
	defer service.Close()
	root := newTestGitRepository(t, "")
	capture := startTestCapture(t, service, root, "raw-transcript")
	if err := service.RecordWrite(context.Background(), RecordWriteRequest{
		CaptureID: capture.ID, Path: "src/a.go",
	}); err != nil {
		t.Fatal(err)
	}

	entries := make([]TranscriptEntry, rawTranscriptPageSize+5)
	for index := range entries {
		entries[index] = TranscriptEntry{Kind: TranscriptEntryUser, Text: fmt.Sprintf("message-%03d", index)}
	}
	draft, err := service.SealCapture(context.Background(), SealCaptureRequest{
		CaptureID:  capture.ID,
		EndCursor:  "end",
		Transcript: &TranscriptInput{FormatVersion: TranscriptFormatVersion, Entries: entries},
	})
	if err != nil {
		t.Fatal(err)
	}
	if draft.TranscriptID == "" || !reflect.DeepEqual(draft.Paths, []string{"src/a.go"}) {
		t.Fatalf("draft = %#v", draft)
	}

	first, err := service.GetTranscript(context.Background(), TranscriptRequest{
		RepositoryRoot: root,
		TranscriptID:   draft.TranscriptID,
		View:           TranscriptViewRaw,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Entries) != rawTranscriptPageSize || first.NextOffset == nil || *first.NextOffset != rawTranscriptPageSize {
		t.Fatalf("first raw page = %#v", first)
	}
	second, err := service.GetTranscript(context.Background(), TranscriptRequest{
		RepositoryRoot: root,
		TranscriptID:   draft.TranscriptID,
		View:           TranscriptViewRaw,
		Offset:         *first.NextOffset,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(second.Entries, entries[rawTranscriptPageSize:]) || second.NextOffset != nil {
		t.Fatalf("second raw page = %#v", second)
	}
}

func TestSealCapturePersistsSessionScaleTranscript(t *testing.T) {
	t.Parallel()

	service := openTestStore(t, t.TempDir())
	defer service.Close()
	root := newTestGitRepository(t, "")
	capture := startTestCapture(t, service, root, "large-transcript")
	if err := service.RecordWrite(context.Background(), RecordWriteRequest{
		CaptureID: capture.ID, Path: "src/a.go",
	}); err != nil {
		t.Fatal(err)
	}
	const entryCount = 2_000
	entries := make([]TranscriptEntry, entryCount)
	for index := range entries {
		entries[index] = TranscriptEntry{
			Kind: TranscriptEntryAssistant,
			Text: fmt.Sprintf("session evidence %04d: %s", index, "semantic context retained in full"),
		}
	}
	draft, err := service.SealCapture(context.Background(), SealCaptureRequest{
		CaptureID:  capture.ID,
		EndCursor:  "end",
		Transcript: &TranscriptInput{FormatVersion: TranscriptFormatVersion, Entries: entries},
	})
	if err != nil {
		t.Fatal(err)
	}
	var storedCount int
	if err := service.db.QueryRow(
		"SELECT COUNT(*) FROM transcript_entries WHERE transcript_id = ?", draft.TranscriptID,
	).Scan(&storedCount); err != nil {
		t.Fatal(err)
	}
	if storedCount != entryCount {
		t.Fatalf("stored Transcript entries = %d, want %d", storedCount, entryCount)
	}
}

func TestTranscriptPublicationFreezesCompactEvidence(t *testing.T) {
	t.Parallel()

	service := openTestStore(t, t.TempDir())
	defer service.Close()
	root := newTestGitRepository(t, "")
	capture := sealTestCaptureWithPaths(t, service, root, "compact-transcript", "src/a.go")
	sealed, err := service.GetCapture(context.Background(), capture.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetTranscript(context.Background(), TranscriptRequest{
		RepositoryRoot: root, TranscriptID: sealed.TranscriptID, View: TranscriptViewCompact,
	}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("pending compact view error = %v, want ErrInvalidState", err)
	}

	const compact = "Exact final evidence passed to the model"
	episode, err := service.PublishEpisode(context.Background(), PublishEpisodeRequest{
		CaptureID: capture.ID, L1: "Summary", L2: "Detail", CompactEvidence: compact,
	})
	if err != nil {
		t.Fatal(err)
	}
	view, err := service.GetTranscript(context.Background(), TranscriptRequest{
		RepositoryRoot: root, TranscriptID: episode.TranscriptID, View: TranscriptViewCompact,
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.Compact != compact {
		t.Fatalf("compact evidence = %q, want %q", view.Compact, compact)
	}
	if _, err := service.PublishEpisode(context.Background(), PublishEpisodeRequest{
		CaptureID: capture.ID, L1: "Summary", L2: "Detail", CompactEvidence: compact,
	}); err != nil {
		t.Fatalf("identical publication retry: %v", err)
	}
}

func TestTranscriptRetrievalIsRepositoryScoped(t *testing.T) {
	t.Parallel()

	service := openTestStore(t, t.TempDir())
	defer service.Close()
	root := newTestGitRepository(t, "")
	capture := sealTestCaptureWithPaths(t, service, root, "scoped-transcript", "src/a.go")
	sealed, err := service.GetCapture(context.Background(), capture.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetTranscript(context.Background(), TranscriptRequest{
		RepositoryRoot: root, TranscriptID: "missing", View: TranscriptViewRaw,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing Transcript error = %v, want ErrNotFound", err)
	}
	otherRoot := newTestGitRepository(t, "")
	_, err = service.GetTranscript(context.Background(), TranscriptRequest{
		RepositoryRoot: otherRoot, TranscriptID: sealed.TranscriptID, View: TranscriptViewRaw,
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-Repository error = %v, want ErrNotFound", err)
	}
}

func TestSealedTranscriptAndPathsSurviveStoreReopen(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	root := newTestGitRepository(t, "")
	service := openTestStore(t, home)
	capture := sealTestCaptureWithPaths(t, service, root, "sealed-recovery", "src/a.go")
	sealed, err := service.GetCapture(context.Background(), capture.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := openTestStore(t, home)
	defer reopened.Close()
	view, err := reopened.GetTranscript(context.Background(), TranscriptRequest{
		RepositoryRoot: root, TranscriptID: sealed.TranscriptID, View: TranscriptViewRaw,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(view.Entries, testTranscriptInput().Entries) {
		t.Fatalf("recovered entries = %#v", view.Entries)
	}
	draft, err := reopened.SealCapture(context.Background(), SealCaptureRequest{
		CaptureID: capture.ID, EndCursor: sealed.EndCursor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if draft.TranscriptID != sealed.TranscriptID || !reflect.DeepEqual(draft.Paths, []string{"src/a.go"}) {
		t.Fatalf("recovered draft = %#v", draft)
	}
}

func TestTranscriptRetrievalRejectsInvalidViewAndOffset(t *testing.T) {
	t.Parallel()

	service := openTestStore(t, t.TempDir())
	defer service.Close()
	root := newTestGitRepository(t, "")
	capture := sealTestCaptureWithPaths(t, service, root, "invalid-transcript-request", "src/a.go")
	sealed, err := service.GetCapture(context.Background(), capture.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []TranscriptRequest{
		{RepositoryRoot: root, TranscriptID: sealed.TranscriptID, View: "unknown"},
		{RepositoryRoot: root, TranscriptID: sealed.TranscriptID, View: TranscriptViewRaw, Offset: -1},
		{RepositoryRoot: root, TranscriptID: sealed.TranscriptID, View: TranscriptViewRaw, Offset: 100},
		{RepositoryRoot: root, TranscriptID: sealed.TranscriptID, View: TranscriptViewCompact, Offset: 1},
	} {
		if _, err := service.GetTranscript(context.Background(), request); !errors.Is(err, ErrInvalidState) {
			t.Errorf("request %#v error = %v, want ErrInvalidState", request, err)
		}
	}
}

func TestTranscriptFormatIsHarnessAgnostic(t *testing.T) {
	t.Parallel()

	service := openTestStore(t, t.TempDir())
	defer service.Close()
	root := newTestGitRepository(t, "")
	capture := startCaptureWithKey(t, service, root, ConversationKey{
		Harness: "codex", ExternalID: "codex-session-1",
	})
	if err := service.RecordWrite(context.Background(), RecordWriteRequest{
		CaptureID: capture.ID, Path: "src/a.go",
	}); err != nil {
		t.Fatal(err)
	}
	input := &TranscriptInput{
		FormatVersion: TranscriptFormatVersion,
		Entries: []TranscriptEntry{
			{Kind: TranscriptEntryUser, Text: "Implement cross-harness evidence"},
			{Kind: TranscriptEntryMutation, Operation: "write", Path: "src/a.go", Status: "success"},
		},
	}
	draft, err := service.SealCapture(context.Background(), SealCaptureRequest{
		CaptureID: capture.ID, EndCursor: "codex-end", Transcript: input,
	})
	if err != nil {
		t.Fatal(err)
	}
	view, err := service.GetTranscript(context.Background(), TranscriptRequest{
		RepositoryRoot: root, TranscriptID: draft.TranscriptID, View: TranscriptViewRaw,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(view.Entries, input.Entries) {
		t.Fatalf("canonical entries = %#v, want %#v", view.Entries, input.Entries)
	}
}

func TestTranscriptInsertionFailureLeavesCaptureOpen(t *testing.T) {
	t.Parallel()

	service := openTestStore(t, t.TempDir())
	defer service.Close()
	root := newTestGitRepository(t, "")
	capture := startTestCapture(t, service, root, "transcript-rollback")
	if err := service.RecordWrite(context.Background(), RecordWriteRequest{
		CaptureID: capture.ID, Path: "src/a.go",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.db.Exec(`
		CREATE TRIGGER fail_transcript_entry AFTER INSERT ON transcript_entries
		BEGIN SELECT RAISE(ABORT, 'injected Transcript failure'); END`); err != nil {
		t.Fatal(err)
	}

	_, err := service.SealCapture(context.Background(), SealCaptureRequest{
		CaptureID: capture.ID, EndCursor: "end", Transcript: testTranscriptInput(),
	})
	if err == nil {
		t.Fatal("SealCapture succeeded despite injected failure")
	}
	got, err := service.GetCapture(context.Background(), capture.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != CaptureStatusOpen || got.TranscriptID != "" || got.EndCursor != "" {
		t.Fatalf("Capture after rollback = %#v", got)
	}
	var transcriptCount int
	if err := service.db.QueryRow("SELECT COUNT(*) FROM transcripts").Scan(&transcriptCount); err != nil {
		t.Fatal(err)
	}
	if transcriptCount != 0 {
		t.Fatalf("Transcript count = %d, want 0", transcriptCount)
	}
}
