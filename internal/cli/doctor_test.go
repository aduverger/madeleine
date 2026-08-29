package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aduverger/madeleine/internal/rpc"
)

func TestDoctorInsideAndOutsideGit(t *testing.T) {
	repository := initializeCLIRepository(t, filepath.Join(t.TempDir(), "doctor repository"))
	build := BuildInfo{Version: "0.1.0", Commit: "abc123"}

	t.Run("human inside Git", func(t *testing.T) {
		var output, diagnostics bytes.Buffer
		status := executeDoctor(context.Background(), repository, false, &output, &diagnostics,
			t.TempDir(), build)
		if status != exitSuccess {
			t.Fatalf("status = %d, stdout = %q, stderr = %q", status, output.String(), diagnostics.String())
		}
		if diagnostics.Len() != 0 {
			t.Fatalf("stderr = %q, want empty", diagnostics.String())
		}
		for _, check := range []string{
			"binary_version: ok - 0.1.0 (abc123)",
			"data_directory: ok - accessible",
			"application: ok - initialized",
			"schema_version: ok - version 3",
			"git_executable: ok - git version",
			"repository: ok - resolved",
		} {
			if !strings.Contains(output.String(), check) {
				t.Errorf("stdout does not contain %q: %q", check, output.String())
			}
		}
	})

	t.Run("JSON outside Git", func(t *testing.T) {
		var output, diagnostics bytes.Buffer
		status := executeDoctor(context.Background(), t.TempDir(), true, &output, &diagnostics,
			t.TempDir(), build)
		if status != exitOperationFailure {
			t.Fatalf("status = %d, stdout = %q, stderr = %q", status, output.String(), diagnostics.String())
		}
		if diagnostics.Len() != 0 {
			t.Fatalf("stderr = %q, want empty", diagnostics.String())
		}
		var response struct {
			ProtocolVersion int `json:"protocol_version"`
			OK              bool
			Result          DoctorResult `json:"result"`
		}
		if err := json.Unmarshal(output.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response.ProtocolVersion != rpc.ProtocolVersion || !response.OK {
			t.Fatalf("response = %#v", response)
		}
		checks := checksByName(response.Result.Checks)
		if !checks["application"].OK || !checks["schema_version"].OK {
			t.Fatalf("database checks did not run: %#v", response.Result.Checks)
		}
		if checks["repository"].OK || checks["repository"].Detail != "path is not inside a Git repository" {
			t.Fatalf("repository check = %#v", checks["repository"])
		}
	})
}

func TestDoctorReportsMissingGit(t *testing.T) {
	t.Setenv("PATH", "")
	var output, diagnostics bytes.Buffer
	status := executeDoctor(context.Background(), t.TempDir(), true, &output, &diagnostics,
		t.TempDir(), BuildInfo{Version: "dev", Commit: "unknown"})
	if status != exitOperationFailure {
		t.Fatalf("status = %d, stdout = %q", status, output.String())
	}
	var response struct {
		Result DoctorResult `json:"result"`
	}
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	checks := checksByName(response.Result.Checks)
	if checks["git_executable"].OK || checks["git_executable"].Detail != "unavailable" {
		t.Fatalf("Git check = %#v", checks["git_executable"])
	}
	if !checks["application"].OK {
		t.Fatalf("application check should not depend on Git: %#v", checks["application"])
	}
}

func checksByName(checks []DoctorCheck) map[string]DoctorCheck {
	byName := make(map[string]DoctorCheck, len(checks))
	for _, check := range checks {
		byName[check.Name] = check
	}
	return byName
}
