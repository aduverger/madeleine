package madeleine

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestContextForPathsDeduplicatesAndLimitsNewestEpisodes(t *testing.T) {
	t.Parallel()

	store := openTestStore(t, t.TempDir())
	defer store.Close()
	root := newTestGitRepository(t, "")
	endedAt := []string{
		"2026-01-01T00:00:00Z",
		"2026-01-02T00:00:00Z",
		"2026-01-03T00:00:00Z",
		"2026-01-04T00:00:00Z",
		"2026-01-05T00:00:00Z",
		"2026-01-06T00:00:00Z",
		"2026-01-06T00:00:00Z",
	}
	episodes := make([]Episode, len(endedAt))
	for index, timestamp := range endedAt {
		paths := []string{"src/target.go"}
		if index == 5 {
			paths = append(paths, "src/other.go")
		}
		episodes[index] = publishTestEpisodeAt(t, store, root, timestamp, paths...)
	}
	otherEpisode := publishTestEpisodeAt(t, store, root, "2026-01-07T00:00:00Z", "src/other.go")

	repository, err := store.ResolveRepository(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	contexts, err := store.ContextForPaths(context.Background(), ContextRequest{
		RepositoryRoot: root,
		Paths: []string{
			"src/generated/../target.go",
			filepath.Join(repository.WorktreeRoot, "src", "target.go"),
			"src/other.go",
			"src/missing.go",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := contextPaths(contexts); !reflect.DeepEqual(got, []string{"src/target.go", "src/other.go", "src/missing.go"}) {
		t.Fatalf("context paths = %v", got)
	}
	if contexts[2].Episodes == nil || len(contexts[2].Episodes) != 0 {
		t.Fatalf("missing path Episodes = %#v, want non-nil empty slice", contexts[2].Episodes)
	}

	wantTarget := append([]Episode(nil), episodes...)
	sort.Slice(wantTarget, func(i, j int) bool {
		if wantTarget[i].EndedAt.Equal(wantTarget[j].EndedAt) {
			return wantTarget[i].ID > wantTarget[j].ID
		}
		return wantTarget[i].EndedAt.After(wantTarget[j].EndedAt)
	})
	wantTarget = wantTarget[:maxEpisodesPerPath]
	wantTargetIDs := episodeIDs(wantTarget)
	if got := summaryIDs(contexts[0].Episodes); !reflect.DeepEqual(got, wantTargetIDs) {
		t.Fatalf("target Episode IDs = %v, want %v", got, wantTargetIDs)
	}
	for index, summary := range contexts[0].Episodes {
		want := wantTarget[index]
		if summary.L1 != want.L1 || summary.Harness != want.Harness || !summary.EndedAt.Equal(want.EndedAt) {
			t.Errorf("target summary %d = %#v, want Episode %#v", index, summary, want)
		}
	}
	if got := summaryIDs(contexts[1].Episodes); !reflect.DeepEqual(got, []EpisodeID{otherEpisode.ID, episodes[5].ID}) {
		t.Fatalf("other Episode IDs = %v", got)
	}
}

func TestContextForPathsUsesExactRepositoryPaths(t *testing.T) {
	t.Parallel()

	store := openTestStore(t, t.TempDir())
	defer store.Close()
	root := newTestGitRepository(t, "")
	exact := publishTestEpisodeAt(t, store, root, "2026-02-01T00:00:00Z", "src/file.go")
	publishTestEpisodeAt(t, store, root, "2026-02-02T00:00:00Z", "src/file.go.bak")
	publishTestEpisodeAt(t, store, root, "2026-02-03T00:00:00Z", "src/file.go/child")
	otherRoot := newTestGitRepository(t, "")
	publishTestEpisodeAt(t, store, otherRoot, "2026-02-04T00:00:00Z", "src/file.go")

	contexts, err := store.ContextForPaths(context.Background(), ContextRequest{
		RepositoryRoot: root,
		Paths:          []string{"src/file.go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(contexts) != 1 || !reflect.DeepEqual(summaryIDs(contexts[0].Episodes), []EpisodeID{exact.ID}) {
		t.Fatalf("exact context = %#v", contexts)
	}

	empty, err := store.ContextForPaths(context.Background(), ContextRequest{RepositoryRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty request = %#v, want non-nil empty slice", empty)
	}
	if _, err := store.ContextForPaths(context.Background(), ContextRequest{
		RepositoryRoot: root, Paths: []string{"../outside.go"},
	}); !errors.Is(err, ErrOutsideRepository) {
		t.Fatalf("outside path error = %v, want ErrOutsideRepository", err)
	}
}

func TestGetEpisodeReturnsRepositoryScopedDetail(t *testing.T) {
	t.Parallel()

	store := openTestStore(t, t.TempDir())
	defer store.Close()
	root := newTestGitRepository(t, "")
	episode := publishTestEpisodeAt(t, store, root, "2026-03-01T00:00:00Z", "z.go", "a.go")

	detail, err := store.GetEpisode(context.Background(), EpisodeRequest{
		RepositoryRoot: root, EpisodeID: episode.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if detail.EpisodeID != episode.ID || detail.ConversationID != episode.ConversationID || detail.ConversationKey != episode.ConversationKey {
		t.Fatalf("Episode detail identity = %#v", detail)
	}
	if detail.Harness != episode.Harness || detail.L1 != episode.L1 || detail.L2 != episode.L2 {
		t.Fatalf("Episode detail summaries = %#v", detail)
	}
	if detail.TranscriptID != episode.TranscriptID {
		t.Fatalf("Episode detail Transcript ID = %q, want %q", detail.TranscriptID, episode.TranscriptID)
	}
	if !detail.StartedAt.Equal(episode.StartedAt) || !detail.EndedAt.Equal(episode.EndedAt) {
		t.Fatalf("Episode detail times = %#v", detail)
	}
	if !reflect.DeepEqual(detail.Paths, []string{"a.go", "z.go"}) {
		t.Fatalf("Episode detail paths = %v", detail.Paths)
	}

	otherRoot := newTestGitRepository(t, "")
	if _, err := store.GetEpisode(context.Background(), EpisodeRequest{
		RepositoryRoot: otherRoot, EpisodeID: episode.ID,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-Repository detail error = %v, want ErrNotFound", err)
	}
	if _, err := store.GetEpisode(context.Background(), EpisodeRequest{
		RepositoryRoot: root, EpisodeID: "missing",
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing detail error = %v, want ErrNotFound", err)
	}
	for _, request := range []EpisodeRequest{
		{EpisodeID: episode.ID},
		{RepositoryRoot: root},
	} {
		if _, err := store.GetEpisode(context.Background(), request); !errors.Is(err, ErrInvalidState) {
			t.Errorf("incomplete detail request %#v error = %v, want ErrInvalidState", request, err)
		}
	}
}

func publishTestEpisodeAt(t *testing.T, store *testService, root, endedAt string, paths ...string) Episode {
	t.Helper()
	capture := sealTestCaptureWithPaths(t, store, root, "episode-"+endedAt+"-"+paths[0], paths...)
	if _, err := store.db.Exec(
		"UPDATE captures SET ended_at = ? WHERE id = ?", endedAt, capture.ID,
	); err != nil {
		t.Fatalf("set Capture end time: %v", err)
	}
	episode, err := store.PublishEpisode(context.Background(), PublishEpisodeRequest{
		CaptureID:       capture.ID,
		L1:              "Summary for " + endedAt,
		L2:              "Detailed context for " + endedAt,
		CompactEvidence: "Evidence for " + endedAt,
	})
	if err != nil {
		t.Fatalf("PublishEpisode: %v", err)
	}
	wantEndedAt, err := time.Parse(time.RFC3339Nano, endedAt)
	if err != nil {
		t.Fatalf("parse fixture time: %v", err)
	}
	if !episode.EndedAt.Equal(wantEndedAt) {
		t.Fatalf("Episode ended at %v, want %v", episode.EndedAt, wantEndedAt)
	}
	return episode
}

func contextPaths(contexts []FileContext) []string {
	paths := make([]string, len(contexts))
	for index, context := range contexts {
		paths[index] = context.Path
	}
	return paths
}

func summaryIDs(summaries []EpisodeSummary) []EpisodeID {
	ids := make([]EpisodeID, len(summaries))
	for index, summary := range summaries {
		ids[index] = summary.EpisodeID
	}
	return ids
}

func episodeIDs(episodes []Episode) []EpisodeID {
	ids := make([]EpisodeID, len(episodes))
	for index, episode := range episodes {
		ids[index] = episode.ID
	}
	return ids
}
