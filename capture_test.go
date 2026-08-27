package madeleine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestCaptureStateMachine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		status   CaptureStatus
		action   captureAction
		hasPaths bool
		want     CaptureStatus
		wantErr  bool
	}{
		{name: "write open", status: CaptureStatusOpen, action: captureActionRecordWrite, want: CaptureStatusOpen},
		{name: "write pending", status: CaptureStatusPendingSummary, action: captureActionRecordWrite, wantErr: true},
		{name: "write finalized", status: CaptureStatusFinalized, action: captureActionRecordWrite, wantErr: true},
		{name: "write abandoned", status: CaptureStatusAbandoned, action: captureActionRecordWrite, wantErr: true},
		{name: "seal open with paths", status: CaptureStatusOpen, action: captureActionSeal, hasPaths: true, want: CaptureStatusPendingSummary},
		{name: "seal empty open", status: CaptureStatusOpen, action: captureActionSeal, want: CaptureStatusAbandoned},
		{name: "seal pending", status: CaptureStatusPendingSummary, action: captureActionSeal, want: CaptureStatusPendingSummary},
		{name: "seal finalized", status: CaptureStatusFinalized, action: captureActionSeal, want: CaptureStatusFinalized},
		{name: "seal abandoned", status: CaptureStatusAbandoned, action: captureActionSeal, want: CaptureStatusAbandoned},
		{name: "abandon open", status: CaptureStatusOpen, action: captureActionAbandon, want: CaptureStatusAbandoned},
		{name: "abandon pending", status: CaptureStatusPendingSummary, action: captureActionAbandon, want: CaptureStatusAbandoned},
		{name: "abandon abandoned", status: CaptureStatusAbandoned, action: captureActionAbandon, want: CaptureStatusAbandoned},
		{name: "abandon finalized", status: CaptureStatusFinalized, action: captureActionAbandon, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := transitionCapture(test.status, test.action, test.hasPaths)
			if test.wantErr {
				if !errors.Is(err, ErrInvalidState) {
					t.Fatalf("error = %v, want ErrInvalidState", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("status = %q, want %q", got, test.want)
			}
		})
	}
}

func TestStartAndGetCapture(t *testing.T) {
	t.Parallel()

	store := openTestStore(t, t.TempDir())
	defer store.Close()
	root := newTestGitRepository(t, "")
	request := StartCaptureRequest{
		RepositoryRoot:  root,
		ConversationKey: ConversationKey{Harness: HarnessPi, ExternalID: "session-start"},
		TranscriptRef:   "session.jsonl",
		StartCursor:     "entry-10",
	}

	started, err := store.StartCapture(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.GetCapture(context.Background(), started.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, started) {
		t.Fatalf("GetCapture() = %#v, want %#v", got, started)
	}
	if got.StartedAt.Location() != time.UTC || got.LastSeenAt.Location() != time.UTC {
		t.Fatalf("Capture timestamps are not UTC: %#v", got)
	}

	if _, err := store.StartCapture(context.Background(), StartCaptureRequest{
		RepositoryRoot: root, ConversationKey: request.ConversationKey,
	}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("empty start cursor error = %v, want ErrInvalidState", err)
	}
	if _, err := store.GetCapture(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing Capture error = %v, want ErrNotFound", err)
	}
}

func TestConcurrentStartCaptureAllowsOneOpenCapture(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	root := newTestGitRepository(t, "")
	stores := []*Store{openTestStore(t, home), openTestStore(t, home)}
	for _, store := range stores {
		defer store.Close()
	}
	request := StartCaptureRequest{
		RepositoryRoot:  root,
		ConversationKey: ConversationKey{Harness: HarnessPi, ExternalID: "racing-session"},
		TranscriptRef:   "session.jsonl",
		StartCursor:     "start",
	}

	type result struct {
		capture Capture
		err     error
	}
	results := make(chan result, len(stores))
	start := make(chan struct{})
	for _, store := range stores {
		go func() {
			<-start
			capture, err := store.StartCapture(context.Background(), request)
			results <- result{capture: capture, err: err}
		}()
	}
	close(start)

	successes, conflicts := 0, 0
	for range stores {
		result := <-results
		switch {
		case result.err == nil:
			successes++
		case errors.Is(result.err, ErrConflict):
			conflicts++
		default:
			t.Fatalf("StartCapture error = %v", result.err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes = %d, conflicts = %d, want 1 each", successes, conflicts)
	}
	var count int
	if err := stores[0].db.QueryRow("SELECT COUNT(*) FROM captures").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("Capture count = %d, want 1", count)
	}
}

func TestRecordWriteNormalizesAndRefreshesPath(t *testing.T) {
	t.Parallel()

	store := openTestStore(t, t.TempDir())
	defer store.Close()
	root := newTestGitRepository(t, "")
	capture := startTestCapture(t, store, root, "record-write")

	request := RecordWriteRequest{CaptureID: capture.ID, Path: "src/generated/../main.go"}
	if err := store.RecordWrite(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	var path, source, firstSeen, firstLastSeen string
	if err := store.db.QueryRow(`
		SELECT path, source, first_seen_at, last_seen_at
		FROM capture_paths WHERE capture_id = ?`, capture.ID,
	).Scan(&path, &source, &firstSeen, &firstLastSeen); err != nil {
		t.Fatal(err)
	}
	if path != "src/main.go" || source != "tool" {
		t.Fatalf("stored path/source = %q/%q, want src/main.go/tool", path, source)
	}

	time.Sleep(2 * time.Millisecond)
	request.Path = filepath.Join(capture.WorktreeRoot, "src", "main.go")
	if err := store.RecordWrite(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	var secondFirstSeen, secondLastSeen string
	if err := store.db.QueryRow(`
		SELECT first_seen_at, last_seen_at FROM capture_paths
		WHERE capture_id = ? AND path = ?`, capture.ID, "src/main.go",
	).Scan(&secondFirstSeen, &secondLastSeen); err != nil {
		t.Fatal(err)
	}
	if secondFirstSeen != firstSeen {
		t.Fatalf("first_seen_at changed from %q to %q", firstSeen, secondFirstSeen)
	}
	firstLastTime := mustParseTime(t, firstLastSeen)
	secondLastTime := mustParseTime(t, secondLastSeen)
	if !secondLastTime.After(firstLastTime) {
		t.Fatalf("last_seen_at did not advance: %q then %q", firstLastSeen, secondLastSeen)
	}
	got, err := store.GetCapture(context.Background(), capture.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.LastSeenAt.Equal(secondLastTime) {
		t.Fatalf("Capture last_seen_at = %v, want %v", got.LastSeenAt, secondLastTime)
	}
}

func TestRecordWriteRejectsOutsideAndTerminalCaptures(t *testing.T) {
	t.Parallel()

	store := openTestStore(t, t.TempDir())
	defer store.Close()
	root := newTestGitRepository(t, "")
	outside := filepath.Join(filepath.Dir(root), "outside.go")
	capture := startTestCapture(t, store, root, "outside-path")
	for _, path := range []string{"../outside.go", outside} {
		err := store.RecordWrite(context.Background(), RecordWriteRequest{CaptureID: capture.ID, Path: path})
		if !errors.Is(err, ErrOutsideRepository) {
			t.Errorf("path %q error = %v, want ErrOutsideRepository", path, err)
		}
	}

	statuses := []CaptureStatus{
		CaptureStatusPendingSummary,
		CaptureStatusFinalized,
		CaptureStatusAbandoned,
	}
	for _, status := range statuses {
		capture := startTestCapture(t, store, root, "write-"+string(status))
		if _, err := store.db.Exec(
			"UPDATE captures SET status = ? WHERE id = ?", status, capture.ID,
		); err != nil {
			t.Fatal(err)
		}
		err := store.RecordWrite(context.Background(), RecordWriteRequest{CaptureID: capture.ID, Path: "blocked.go"})
		if !errors.Is(err, ErrInvalidState) {
			t.Errorf("status %q error = %v, want ErrInvalidState", status, err)
		}
	}
	if err := store.RecordWrite(context.Background(), RecordWriteRequest{CaptureID: "missing", Path: "file.go"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing Capture error = %v, want ErrNotFound", err)
	}
}

func TestConcurrentRecordWriteIsIdempotent(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	root := newTestGitRepository(t, "")
	stores := make([]*Store, 6)
	for index := range stores {
		stores[index] = openTestStore(t, home)
		defer stores[index].Close()
	}
	capture := startTestCapture(t, stores[0], root, "concurrent-writes")

	errorsByWrite := make(chan error, len(stores)*2)
	start := make(chan struct{})
	for index, store := range stores {
		go func() {
			<-start
			errorsByWrite <- store.RecordWrite(context.Background(), RecordWriteRequest{
				CaptureID: capture.ID, Path: "shared.go",
			})
			errorsByWrite <- store.RecordWrite(context.Background(), RecordWriteRequest{
				CaptureID: capture.ID, Path: fmt.Sprintf("independent/%d.go", index),
			})
		}()
	}
	close(start)
	for range len(stores) * 2 {
		if err := <-errorsByWrite; err != nil {
			t.Fatalf("RecordWrite: %v", err)
		}
	}

	var count, distinctCount int
	if err := stores[0].db.QueryRow(`
		SELECT COUNT(*), COUNT(DISTINCT path) FROM capture_paths WHERE capture_id = ?`, capture.ID,
	).Scan(&count, &distinctCount); err != nil {
		t.Fatal(err)
	}
	want := len(stores) + 1
	if count != want || distinctCount != want {
		t.Fatalf("path counts = %d/%d, want %d/%d", count, distinctCount, want, want)
	}
}

func TestRecordWriteAcrossProcesses(t *testing.T) {
	if os.Getenv("MADELEINE_CAPTURE_WRITE_HELPER") == "1" {
		store, err := Open(context.Background(), Options{Home: os.Getenv("MADELEINE_TEST_HOME")})
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		err = store.RecordWrite(context.Background(), RecordWriteRequest{
			CaptureID: CaptureID(os.Getenv("MADELEINE_TEST_CAPTURE_ID")),
			Path:      os.Getenv("MADELEINE_TEST_PATH"),
		})
		if err != nil {
			t.Fatal(err)
		}
		return
	}

	home := t.TempDir()
	store := openTestStore(t, home)
	defer store.Close()
	capture := startTestCapture(t, store, newTestGitRepository(t, ""), "process-writes")
	paths := []string{"shared.go", "shared.go", "shared.go", "shared.go", "one.go", "two.go", "three.go", "four.go"}

	type result struct {
		output string
		err    error
	}
	results := make(chan result, len(paths))
	start := make(chan struct{})
	for _, path := range paths {
		go func() {
			<-start
			command := exec.Command(os.Args[0], "-test.run=^TestRecordWriteAcrossProcesses$")
			command.Env = append(os.Environ(),
				"MADELEINE_CAPTURE_WRITE_HELPER=1",
				"MADELEINE_TEST_HOME="+home,
				"MADELEINE_TEST_CAPTURE_ID="+string(capture.ID),
				"MADELEINE_TEST_PATH="+path,
			)
			output, err := command.CombinedOutput()
			results <- result{output: string(output), err: err}
		}()
	}
	close(start)
	for range paths {
		result := <-results
		if result.err != nil {
			t.Fatalf("write helper: %v\n%s", result.err, result.output)
		}
	}

	var count int
	if err := store.db.QueryRow(
		"SELECT COUNT(*) FROM capture_paths WHERE capture_id = ?", capture.ID,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Fatalf("path count = %d, want 5", count)
	}
}

func TestSealCaptureOrdersPathsAndIsIdempotent(t *testing.T) {
	t.Parallel()

	store := openTestStore(t, t.TempDir())
	defer store.Close()
	root := newTestGitRepository(t, "")
	capture := startTestCapture(t, store, root, "seal-paths")
	for _, path := range []string{"z.go", "a.go", "nested/m.go", "a.go"} {
		if err := store.RecordWrite(context.Background(), RecordWriteRequest{CaptureID: capture.ID, Path: path}); err != nil {
			t.Fatal(err)
		}
	}

	first, err := store.SealCapture(context.Background(), SealCaptureRequest{CaptureID: capture.ID, EndCursor: "end-1"})
	if err != nil {
		t.Fatal(err)
	}
	wantPaths := []string{"a.go", "nested/m.go", "z.go"}
	if first.Status != CaptureStatusPendingSummary || first.Empty || !reflect.DeepEqual(first.Paths, wantPaths) {
		t.Fatalf("first draft = %#v, want ordered pending draft %v", first, wantPaths)
	}
	sealed, err := store.GetCapture(context.Background(), capture.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sealed.EndCursor != "end-1" || sealed.EndedAt == nil {
		t.Fatalf("sealed Capture boundaries = %#v", sealed)
	}

	second, err := store.SealCapture(context.Background(), SealCaptureRequest{CaptureID: capture.ID, EndCursor: "end-2"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(second, first) {
		t.Fatalf("repeated draft = %#v, want %#v", second, first)
	}
	repeated, err := store.GetCapture(context.Background(), capture.ID)
	if err != nil {
		t.Fatal(err)
	}
	if repeated.EndCursor != sealed.EndCursor || !repeated.EndedAt.Equal(*sealed.EndedAt) {
		t.Fatalf("repeated seal mutated boundaries: before %#v, after %#v", sealed, repeated)
	}

	if _, err := store.SealCapture(context.Background(), SealCaptureRequest{CaptureID: capture.ID}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("empty end cursor error = %v, want ErrInvalidState", err)
	}
}

func TestSealBeforeGitReconciliationIgnoresUnrecordedChanges(t *testing.T) {
	t.Parallel()

	store := openTestStore(t, t.TempDir())
	defer store.Close()
	root := newTestGitRepository(t, "")
	capture := startTestCapture(t, store, root, "pre-git-reconciliation")
	if err := os.WriteFile(filepath.Join(root, "shell-created.go"), []byte("package example\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	draft, err := store.SealCapture(context.Background(), SealCaptureRequest{
		CaptureID: capture.ID, EndCursor: "end",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !draft.Empty || draft.Status != CaptureStatusAbandoned {
		t.Fatalf("draft = %#v, want empty abandoned Capture", draft)
	}
}

func TestSealEmptyAndFinalizedCapturesIsIdempotent(t *testing.T) {
	t.Parallel()

	store := openTestStore(t, t.TempDir())
	defer store.Close()
	root := newTestGitRepository(t, "")

	emptyCapture := startTestCapture(t, store, root, "empty-seal")
	first, err := store.SealCapture(context.Background(), SealCaptureRequest{CaptureID: emptyCapture.ID, EndCursor: "end"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != CaptureStatusAbandoned || !first.Empty || len(first.Paths) != 0 {
		t.Fatalf("empty draft = %#v", first)
	}
	second, err := store.SealCapture(context.Background(), SealCaptureRequest{CaptureID: emptyCapture.ID, EndCursor: "different"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(second, first) {
		t.Fatalf("repeated empty draft = %#v, want %#v", second, first)
	}
	var terminalRows int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM captures WHERE id = ?", emptyCapture.ID).Scan(&terminalRows); err != nil {
		t.Fatal(err)
	}
	if terminalRows != 1 {
		t.Fatalf("terminal Capture rows = %d, want 1", terminalRows)
	}

	finalized := startTestCapture(t, store, root, "finalized-seal")
	if _, err := store.db.Exec(`
		UPDATE captures SET status = ?, episode_id = ?, end_cursor = ?, ended_at = ? WHERE id = ?`,
		CaptureStatusFinalized, "episode-1", "original-end", utcTimestamp(), finalized.ID,
	); err != nil {
		t.Fatal(err)
	}
	draft, err := store.SealCapture(context.Background(), SealCaptureRequest{CaptureID: finalized.ID, EndCursor: "new-end"})
	if err != nil {
		t.Fatal(err)
	}
	if draft.Status != CaptureStatusFinalized || draft.EpisodeID != "episode-1" {
		t.Fatalf("finalized draft = %#v", draft)
	}
	got, err := store.GetCapture(context.Background(), finalized.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.EndCursor != "original-end" {
		t.Fatalf("finalized seal changed end cursor to %q", got.EndCursor)
	}
}

func TestAbandonCaptureDeletesPathsAndKeepsTerminalRow(t *testing.T) {
	t.Parallel()

	store := openTestStore(t, t.TempDir())
	defer store.Close()
	root := newTestGitRepository(t, "")
	capture := startTestCapture(t, store, root, "abandon")
	if err := store.RecordWrite(context.Background(), RecordWriteRequest{CaptureID: capture.ID, Path: "file.go"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SealCapture(context.Background(), SealCaptureRequest{CaptureID: capture.ID, EndCursor: "end"}); err != nil {
		t.Fatal(err)
	}
	sealed, err := store.GetCapture(context.Background(), capture.ID)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.AbandonCapture(context.Background(), capture.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.AbandonCapture(context.Background(), capture.ID); err != nil {
		t.Fatalf("repeat abandon: %v", err)
	}
	got, err := store.GetCapture(context.Background(), capture.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != CaptureStatusAbandoned || got.EndedAt == nil {
		t.Fatalf("abandoned Capture = %#v", got)
	}
	if !got.EndedAt.Equal(*sealed.EndedAt) {
		t.Fatalf("abandon changed sealed end time from %v to %v", sealed.EndedAt, got.EndedAt)
	}
	var captureCount, pathCount int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM captures WHERE id = ?", capture.ID).Scan(&captureCount); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow("SELECT COUNT(*) FROM capture_paths WHERE capture_id = ?", capture.ID).Scan(&pathCount); err != nil {
		t.Fatal(err)
	}
	if captureCount != 1 || pathCount != 0 {
		t.Fatalf("Capture/path rows = %d/%d, want 1/0", captureCount, pathCount)
	}

	finalized := startTestCapture(t, store, root, "abandon-finalized")
	if _, err := store.db.Exec("UPDATE captures SET status = ? WHERE id = ?", CaptureStatusFinalized, finalized.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.AbandonCapture(context.Background(), finalized.ID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("abandon finalized error = %v, want ErrInvalidState", err)
	}
	if err := store.AbandonCapture(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("abandon missing error = %v, want ErrNotFound", err)
	}
}

func TestListPendingCapturesIsIsolatedAndOldestFirst(t *testing.T) {
	t.Parallel()

	store := openTestStore(t, t.TempDir())
	defer store.Close()
	firstRoot := newTestGitRepository(t, "")
	secondRoot := newTestGitRepository(t, "")
	keyA := ConversationKey{Harness: HarnessPi, ExternalID: "pending-a"}
	keyB := ConversationKey{Harness: HarnessPi, ExternalID: "pending-b"}

	pendingA := startCaptureWithKey(t, store, firstRoot, keyA)
	if err := store.RecordWrite(context.Background(), RecordWriteRequest{CaptureID: pendingA.ID, Path: "a.go"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SealCapture(context.Background(), SealCaptureRequest{CaptureID: pendingA.ID, EndCursor: "end"}); err != nil {
		t.Fatal(err)
	}
	openA := startCaptureWithKey(t, store, firstRoot, keyA)
	openB := startCaptureWithKey(t, store, firstRoot, keyB)
	abandoned := startTestCapture(t, store, firstRoot, "not-pending")
	if err := store.AbandonCapture(context.Background(), abandoned.ID); err != nil {
		t.Fatal(err)
	}
	otherRepository := startTestCapture(t, store, secondRoot, "other-repository")

	timestamps := map[CaptureID]string{
		openA.ID:           "2026-01-01T00:00:00Z",
		pendingA.ID:        "2026-01-02T00:00:00Z",
		openB.ID:           "2026-01-03T00:00:00Z",
		abandoned.ID:       "2026-01-04T00:00:00Z",
		otherRepository.ID: "2026-01-05T00:00:00Z",
	}
	for captureID, startedAt := range timestamps {
		if _, err := store.db.Exec("UPDATE captures SET started_at = ? WHERE id = ?", startedAt, captureID); err != nil {
			t.Fatal(err)
		}
	}

	captures, err := store.ListPendingCaptures(context.Background(), PendingCaptureQuery{RepositoryRoot: firstRoot})
	if err != nil {
		t.Fatal(err)
	}
	gotIDs := captureIDs(captures)
	wantIDs := []CaptureID{openA.ID, pendingA.ID, openB.ID}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("pending IDs = %v, want %v", gotIDs, wantIDs)
	}

	captures, err = store.ListPendingCaptures(context.Background(), PendingCaptureQuery{
		RepositoryRoot: firstRoot, ConversationKey: &keyA,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := captureIDs(captures); !reflect.DeepEqual(got, wantIDs[:2]) {
		t.Fatalf("Conversation pending IDs = %v, want %v", got, wantIDs[:2])
	}

	missingKey := ConversationKey{Harness: HarnessPi, ExternalID: "missing"}
	captures, err = store.ListPendingCaptures(context.Background(), PendingCaptureQuery{
		RepositoryRoot: firstRoot, ConversationKey: &missingKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(captures) != 0 {
		t.Fatalf("missing Conversation returned %d Captures", len(captures))
	}
}

func TestOpenCaptureAndPathsSurviveStoreReopen(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	root := newTestGitRepository(t, "")
	store := openTestStore(t, home)
	capture := startTestCapture(t, store, root, "crash-recovery")
	if err := store.RecordWrite(context.Background(), RecordWriteRequest{CaptureID: capture.ID, Path: "recovered.go"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := openTestStore(t, home)
	defer reopened.Close()
	got, err := reopened.GetCapture(context.Background(), capture.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != CaptureStatusOpen {
		t.Fatalf("reopened status = %q, want open", got.Status)
	}
	draft, err := reopened.SealCapture(context.Background(), SealCaptureRequest{CaptureID: capture.ID, EndCursor: "recovered-end"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(draft.Paths, []string{"recovered.go"}) {
		t.Fatalf("recovered paths = %v", draft.Paths)
	}
}

func TestCaptureSchemaIndexes(t *testing.T) {
	t.Parallel()

	store := openTestStore(t, t.TempDir())
	defer store.Close()
	indexes := []string{
		"captures_one_open_per_conversation_idx",
		"captures_repository_status_started_idx",
		"captures_conversation_status_started_idx",
	}
	for _, index := range indexes {
		if !databaseObjectExists(t, store.db, "index", index) {
			t.Errorf("index %q does not exist", index)
		}
	}
}

func startTestCapture(t *testing.T, store *Store, root, externalID string) Capture {
	t.Helper()
	return startCaptureWithKey(t, store, root, ConversationKey{Harness: HarnessPi, ExternalID: externalID})
}

func startCaptureWithKey(t *testing.T, store *Store, root string, key ConversationKey) Capture {
	t.Helper()
	capture, err := store.StartCapture(context.Background(), StartCaptureRequest{
		RepositoryRoot:  root,
		ConversationKey: key,
		TranscriptRef:   key.ExternalID + ".jsonl",
		StartCursor:     "start",
	})
	if err != nil {
		t.Fatalf("StartCapture: %v", err)
	}
	return capture
}

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		t.Fatalf("parse time %q: %v", value, err)
	}
	return parsed
}

func captureIDs(captures []Capture) []CaptureID {
	ids := make([]CaptureID, len(captures))
	for index, capture := range captures {
		ids[index] = capture.ID
	}
	return ids
}
