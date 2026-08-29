package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aduverger/madeleine/internal/madeleine"
)

type decodedResponse struct {
	ProtocolVersion int             `json:"protocol_version"`
	OK              bool            `json:"ok"`
	Result          json.RawMessage `json:"result"`
	Error           *responseError  `json:"error"`
}

func TestEveryRPCMethod(t *testing.T) {
	repository := initializeTestRepository(t, filepath.Join(t.TempDir(), "repository with spaces"))
	home := filepath.Join(t.TempDir(), "Madeleine home with spaces")
	ctx := context.Background()

	start := callRPC(t, ctx, home, "capture.start", madeleine.StartCaptureRequest{
		RepositoryRoot: repository,
		ConversationKey: madeleine.ConversationKey{
			Harness: madeleine.HarnessPi, ExternalID: "conversation-1",
		},
		TranscriptRef: "transcript.jsonl",
		StartCursor:   "entry-1",
	})
	var capture madeleine.Capture
	decodeResult(t, start, &capture)
	if capture.Status != madeleine.CaptureStatusOpen {
		t.Fatalf("start status = %q", capture.Status)
	}

	get := callRPC(t, ctx, home, "capture.get", captureReference{CaptureID: capture.ID})
	var fetched madeleine.Capture
	decodeResult(t, get, &fetched)
	if fetched.ID != capture.ID {
		t.Fatalf("get ID = %q, want %q", fetched.ID, capture.ID)
	}

	path := filepath.Join(capture.WorktreeRoot, "path with spaces.txt")
	if err := os.WriteFile(path, []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	callRPC(t, ctx, home, "capture.record_write", madeleine.RecordWriteRequest{
		CaptureID: capture.ID,
		Path:      path,
	})

	pending := callRPC(t, ctx, home, "capture.list_pending", madeleine.PendingCaptureQuery{
		RepositoryRoot: repository,
		ConversationKey: &madeleine.ConversationKey{
			Harness: madeleine.HarnessPi, ExternalID: "conversation-1",
		},
	})
	var captures []madeleine.Capture
	decodeResult(t, pending, &captures)
	if len(captures) != 1 || captures[0].ID != capture.ID {
		t.Fatalf("pending captures = %#v", captures)
	}

	sealed := callRPC(t, ctx, home, "capture.seal", madeleine.SealCaptureRequest{
		CaptureID: capture.ID,
		EndCursor: "entry-2",
	})
	var draft madeleine.FinalizationDraft
	decodeResult(t, sealed, &draft)
	if draft.Status != madeleine.CaptureStatusPendingSummary {
		t.Fatalf("sealed status = %q", draft.Status)
	}

	published := callRPC(t, ctx, home, "episode.publish", madeleine.PublishEpisodeRequest{
		CaptureID: capture.ID,
		L1:        "Changed a path to verify RPC dispatch.",
		L2:        "The integration test invokes every RPC method through a fresh service.",
	})
	var episode madeleine.Episode
	decodeResult(t, published, &episode)

	contextResponse := callRPC(t, ctx, home, "context.for_paths", madeleine.ContextRequest{
		RepositoryRoot: repository,
		Paths:          []string{path},
	})
	var contexts []madeleine.FileContext
	decodeResult(t, contextResponse, &contexts)
	if len(contexts) != 1 || len(contexts[0].Episodes) != 1 {
		t.Fatalf("contexts = %#v", contexts)
	}

	detailResponse := callRPC(t, ctx, home, "episode.get", madeleine.EpisodeRequest{
		RepositoryRoot: repository,
		EpisodeID:      episode.ID,
	})
	var detail madeleine.EpisodeDetail
	decodeResult(t, detailResponse, &detail)
	if detail.EpisodeID != episode.ID {
		t.Fatalf("detail Episode ID = %q, want %q", detail.EpisodeID, episode.ID)
	}

	secondStart := callRPC(t, ctx, home, "capture.start", madeleine.StartCaptureRequest{
		RepositoryRoot: repository,
		ConversationKey: madeleine.ConversationKey{
			Harness: madeleine.HarnessPi, ExternalID: "conversation-2",
		},
		StartCursor: "entry-3",
	})
	var secondCapture madeleine.Capture
	decodeResult(t, secondStart, &secondCapture)
	callRPC(t, ctx, home, "capture.abandon", captureReference{CaptureID: secondCapture.ID})
}

func callRPC(t *testing.T, ctx context.Context, home, method string, params any) decodedResponse {
	t.Helper()
	request, err := json.Marshal(struct {
		ProtocolVersion int `json:"protocol_version"`
		Params          any `json:"params"`
	}{ProtocolVersion: ProtocolVersion, Params: params})
	if err != nil {
		t.Fatal(err)
	}

	var output, diagnostics bytes.Buffer
	status := Run(ctx, method, bytes.NewReader(request), &output, &diagnostics, Config{Home: home})
	if status != OutcomeSuccess {
		t.Fatalf("%s status = %d, stdout = %q, stderr = %q", method, status, output.String(), diagnostics.String())
	}
	if diagnostics.Len() != 0 {
		t.Fatalf("%s stderr = %q, want empty", method, diagnostics.String())
	}
	if strings.Count(output.String(), "\n") != 1 || !strings.HasSuffix(output.String(), "\n") {
		t.Fatalf("%s stdout is not one newline-terminated object: %q", method, output.String())
	}

	var response decodedResponse
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatalf("decode %s response: %v", method, err)
	}
	if response.ProtocolVersion != ProtocolVersion || !response.OK || response.Error != nil {
		t.Fatalf("%s response = %#v", method, response)
	}
	return response
}

func decodeResult(t *testing.T, response decodedResponse, destination any) {
	t.Helper()
	if err := json.Unmarshal(response.Result, destination); err != nil {
		t.Fatalf("decode result: %v", err)
	}
}

func initializeTestRepository(t *testing.T, directory string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, directory, "init")
	runGit(t, directory, "config", "user.email", "test@example.com")
	runGit(t, directory, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(directory, "initial.txt"), []byte("initial\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, directory, "add", "initial.txt")
	runGit(t, directory, "commit", "-m", "initial")
	return directory
}

func runGit(t *testing.T, directory string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = directory
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
}
