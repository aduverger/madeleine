package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var testBinary string

func TestMain(m *testing.M) {
	directory, err := os.MkdirTemp("", "madeleine-cli-test-")
	if err != nil {
		panic(err)
	}
	testBinary = filepath.Join(directory, "madeleine")
	command := exec.Command("go", "build", "-o", testBinary, "../../cmd/madeleine")
	if output, err := command.CombinedOutput(); err != nil {
		_, _ = os.Stderr.Write(output)
		_ = os.RemoveAll(directory)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(directory)
	os.Exit(code)
}

func TestRunExitStatusesAndStreams(t *testing.T) {
	build := BuildInfo{Version: "0.1.0", Commit: "unknown"}
	home := t.TempDir()

	tests := []struct {
		name       string
		args       []string
		input      string
		wantStatus int
		wantStdout string
		wantStderr string
	}{
		{
			name: "version", args: []string{"version"}, wantStatus: exitSuccess,
			wantStdout: "0.1.0\n",
		},
		{
			name: "missing command", wantStatus: exitInvalidInvocation,
			wantStderr: "command is required",
		},
		{
			name: "missing RPC method", args: []string{"rpc"}, wantStatus: exitInvalidInvocation,
			wantStderr: "rpc requires exactly one method",
		},
		{
			name: "invalid protocol", args: []string{"rpc", "capture.get"}, input: `{}`,
			wantStatus: exitInvalidInvocation, wantStdout: `"code":"unsupported_protocol"`,
		},
		{
			name: "operation error", args: []string{"rpc", "capture.get"},
			input:      `{"protocol_version":1,"params":{"capture_id":"missing"}}`,
			wantStatus: exitOperationFailure, wantStdout: `"code":"not_found"`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			status := run(context.Background(), test.args, strings.NewReader(test.input),
				&stdout, &stderr, home, build)
			if status != test.wantStatus {
				t.Fatalf("status = %d, want %d", status, test.wantStatus)
			}
			if test.wantStdout == "" && stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if test.wantStdout != "" && !strings.Contains(stdout.String(), test.wantStdout) {
				t.Fatalf("stdout = %q, want to contain %q", stdout.String(), test.wantStdout)
			}
			if test.wantStderr == "" && stderr.Len() != 0 {
				t.Fatalf("stderr = %q, want empty", stderr.String())
			}
			if test.wantStderr != "" && !strings.Contains(stderr.String(), test.wantStderr) {
				t.Fatalf("stderr = %q, want to contain %q", stderr.String(), test.wantStderr)
			}
		})
	}
}

func TestConcurrentCLIProcessesShareHome(t *testing.T) {
	repository := initializeCLIRepository(t, filepath.Join(t.TempDir(), "repository with spaces"))
	home := filepath.Join(t.TempDir(), "home with spaces")
	request, err := json.Marshal(map[string]any{
		"protocol_version": 1,
		"params": map[string]any{
			"repository_root": repository,
			"paths":           []string{"initial file.txt"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	type result struct {
		stdout string
		stderr string
		status int
	}
	results := make(chan result, 2)
	var start sync.WaitGroup
	start.Add(1)
	for range 2 {
		go func() {
			start.Wait()
			stdout, stderr, status := runCLI(home, request, "rpc", "context.for_paths")
			results <- result{stdout: stdout, stderr: stderr, status: status}
		}()
	}
	start.Done()

	for range 2 {
		result := <-results
		if result.status != exitSuccess || result.stderr != "" {
			t.Fatalf("status = %d, stdout = %q, stderr = %q", result.status, result.stdout, result.stderr)
		}
		if !strings.Contains(result.stdout, `"ok":true`) || strings.Count(result.stdout, "\n") != 1 {
			t.Fatalf("stdout = %q", result.stdout)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "madeleine.db")); err != nil {
		t.Fatalf("MADELEINE_HOME was not used: %v", err)
	}
}

func TestCLIReadOnlyHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "read-only home")
	if err := os.Mkdir(home, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(home, 0o700)
	probe, err := os.Create(filepath.Join(home, "probe"))
	if err == nil {
		_ = probe.Close()
		_ = os.Remove(probe.Name())
		t.Skip("permissions do not make the test directory read-only")
	}

	request := []byte(`{"protocol_version":1,"params":{"capture_id":"missing"}}`)
	stdout, stderr, status := runCLI(home, request, "rpc", "capture.get")
	if status != exitOperationFailure || stderr != "" {
		t.Fatalf("status = %d, stdout = %q, stderr = %q", status, stdout, stderr)
	}
	if !strings.Contains(stdout, `"code":"internal"`) || strings.Contains(stdout, home) {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestCLIDoctorHumanAndJSON(t *testing.T) {
	repository := initializeCLIRepository(t, filepath.Join(t.TempDir(), "doctor repository"))
	home := t.TempDir()

	stdout, stderr, status := runCLI(home, nil, "doctor", "--repo", repository)
	if status != exitSuccess || stderr != "" {
		t.Fatalf("human doctor: status = %d, stdout = %q, stderr = %q", status, stdout, stderr)
	}
	if !strings.Contains(stdout, "repository: ok - resolved") {
		t.Fatalf("human doctor stdout = %q", stdout)
	}

	stdout, stderr, status = runCLI(home, nil, "doctor", "--json", "--repo", t.TempDir())
	if status != exitOperationFailure || stderr != "" {
		t.Fatalf("JSON doctor: status = %d, stdout = %q, stderr = %q", status, stdout, stderr)
	}
	if !strings.Contains(stdout, `"protocol_version":1,"ok":true`) ||
		!strings.Contains(stdout, `"name":"repository","ok":false`) {
		t.Fatalf("JSON doctor stdout = %q", stdout)
	}
}

func runCLI(home string, input []byte, args ...string) (string, string, int) {
	command := exec.Command(testBinary, args...)
	command.Env = environmentWithHome(home)
	command.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	status := 0
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			status = exitError.ExitCode()
		} else {
			status = -1
		}
	}
	return stdout.String(), stderr.String(), status
}

func environmentWithHome(home string) []string {
	environment := make([]string, 0, len(os.Environ())+1)
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "MADELEINE_HOME=") {
			environment = append(environment, value)
		}
	}
	return append(environment, "MADELEINE_HOME="+home)
}

func initializeCLIRepository(t *testing.T, directory string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	runCLIGit(t, directory, "init")
	runCLIGit(t, directory, "config", "user.email", "test@example.com")
	runCLIGit(t, directory, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(directory, "initial file.txt"), []byte("initial\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runCLIGit(t, directory, "add", "initial file.txt")
	runCLIGit(t, directory, "commit", "-m", "initial")
	return directory
}

func runCLIGit(t *testing.T, directory string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = directory
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
}
