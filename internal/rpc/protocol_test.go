package rpc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/aduverger/madeleine/internal/madeleine"
)

func TestProtocolGoldenShapes(t *testing.T) {
	t.Run("request", func(t *testing.T) {
		params, err := decodeRequest(strings.NewReader(`{"protocol_version":1,"params":{"capture_id":"capture-1"}}`))
		if err != nil {
			t.Fatalf("decodeRequest() error = %v", err)
		}
		if got, want := string(params), `{"capture_id":"capture-1"}`; got != want {
			t.Fatalf("params = %s, want %s", got, want)
		}
	})

	tests := []struct {
		name     string
		response responseEnvelope
		want     string
	}{
		{
			name:     "success",
			response: successResponse(emptyResult{}),
			want:     `{"protocol_version":1,"ok":true,"result":{}}` + "\n",
		},
		{
			name:     "error",
			response: errorResponse(operationError(codeInvalidState, "operation is not valid in the current state")),
			want:     `{"protocol_version":1,"ok":false,"error":{"code":"invalid_state","message":"operation is not valid in the current state"}}` + "\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := encodeResponse(&output, test.response); err != nil {
				t.Fatalf("encodeResponse() error = %v", err)
			}
			if output.String() != test.want {
				t.Fatalf("response = %q, want %q", output.String(), test.want)
			}
		})
	}
}

func TestRunRejectsInvalidProtocolInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
		code  string
	}{
		{name: "empty", input: "", code: codeInvalidRequest},
		{name: "malformed", input: `{`, code: codeInvalidRequest},
		{name: "array", input: `[]`, code: codeInvalidRequest},
		{name: "missing version", input: `{"params":{}}`, code: codeUnsupportedProtocol},
		{name: "unknown version", input: `{"protocol_version":2,"params":{}}`, code: codeUnsupportedProtocol},
		{name: "missing params", input: `{"protocol_version":1}`, code: codeInvalidRequest},
		{name: "null params", input: `{"protocol_version":1,"params":null}`, code: codeInvalidRequest},
		{name: "trailing JSON", input: `{"protocol_version":1,"params":{}} {}`, code: codeInvalidRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output, diagnostics bytes.Buffer
			outcome := Run(context.Background(), "capture.get", strings.NewReader(test.input), &output, &diagnostics, "")
			if outcome != OutcomeInvalidRequest {
				t.Fatalf("outcome = %d, want %d", outcome, OutcomeInvalidRequest)
			}
			if diagnostics.Len() != 0 {
				t.Fatalf("stderr = %q, want empty", diagnostics.String())
			}
			if !strings.Contains(output.String(), `"code":"`+test.code+`"`) {
				t.Fatalf("stdout = %q, want code %q", output.String(), test.code)
			}
		})
	}
}

func TestRunMethodValidation(t *testing.T) {
	request := `{"protocol_version":1,"params":{}}`

	t.Run("unknown method", func(t *testing.T) {
		var output, diagnostics bytes.Buffer
		outcome := Run(context.Background(), "missing", strings.NewReader(request), &output, &diagnostics, "")
		if outcome != OutcomeInvalidRequest || !strings.Contains(output.String(), `"code":"unknown_method"`) {
			t.Fatalf("outcome = %d, stdout = %q", outcome, output.String())
		}
		if diagnostics.Len() != 0 {
			t.Fatalf("stderr = %q, want empty", diagnostics.String())
		}
	})

	t.Run("invalid params", func(t *testing.T) {
		var output, diagnostics bytes.Buffer
		outcome := Run(context.Background(), "capture.get", strings.NewReader(
			`{"protocol_version":1,"params":[]}`,
		), &output, &diagnostics, t.TempDir())
		if outcome != OutcomeInvalidRequest || !strings.Contains(output.String(), `"code":"invalid_request"`) {
			t.Fatalf("outcome = %d, stdout = %q", outcome, output.String())
		}
		if diagnostics.Len() != 0 {
			t.Fatalf("stderr = %q, want empty", diagnostics.String())
		}
	})

	t.Run("unknown fields", func(t *testing.T) {
		var output, diagnostics bytes.Buffer
		outcome := Run(context.Background(), "capture.get", strings.NewReader(
			`{"protocol_version":1,"params":{"capture_id":"missing","extra":true},"extra":true}`,
		), &output, &diagnostics, t.TempDir())
		if outcome != OutcomeOperationFailure || !strings.Contains(output.String(), `"code":"not_found"`) {
			t.Fatalf("outcome = %d, stdout = %q", outcome, output.String())
		}
		if diagnostics.Len() != 0 {
			t.Fatalf("stderr = %q, want empty", diagnostics.String())
		}
	})
}

type sqliteBusyError struct{ code int }

func (e sqliteBusyError) Error() string { return "SQL with password=secret" }
func (e sqliteBusyError) Code() int     { return e.code }

func TestMapOperationError(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		code    string
		message string
	}{
		{name: "not found", err: madeleine.ErrNotFound, code: codeNotFound, message: "requested object was not found"},
		{name: "conflict", err: madeleine.ErrConflict, code: codeConflict, message: "operation conflicts with existing state"},
		{name: "invalid state", err: madeleine.ErrInvalidState, code: codeInvalidState, message: "operation is not valid in the current state"},
		{name: "not Git", err: madeleine.ErrNotGitRepository, code: codeNotGitRepository, message: "path is not inside a Git repository"},
		{name: "outside repository", err: madeleine.ErrOutsideRepository, code: codeOutsideRepository, message: "path is outside the repository"},
		{name: "database busy", err: fmt.Errorf("wrapped: %w", sqliteBusyError{code: 5}), code: codeDatabaseBusy, message: "database is busy"},
		{name: "database locked", err: sqliteBusyError{code: 6}, code: codeDatabaseBusy, message: "database is busy"},
		{name: "internal", err: errors.New("SELECT transcript_ref, password=secret"), code: codeInternal, message: "internal error"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mapped := mapOperationError(test.err)
			if mapped.code != test.code || mapped.message != test.message {
				t.Fatalf("mapped = %#v, want code %q message %q", mapped, test.code, test.message)
			}
			if strings.Contains(mapped.message, "secret") || strings.Contains(mapped.message, "SELECT") {
				t.Fatalf("message leaked internal content: %q", mapped.message)
			}
		})
	}
}
