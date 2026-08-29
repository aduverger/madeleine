package rpc

import (
	"errors"

	"github.com/aduverger/madeleine/internal/madeleine"
)

type Outcome uint8

const (
	OutcomeSuccess Outcome = iota
	OutcomeOperationFailure
	OutcomeInvalidRequest
)

var errInvalidParams = errors.New("invalid params")

const (
	codeNotFound            = "not_found"
	codeConflict            = "conflict"
	codeInvalidState        = "invalid_state"
	codeNotGitRepository    = "not_git_repository"
	codeOutsideRepository   = "outside_repository"
	codeInvalidRequest      = "invalid_request"
	codeUnsupportedProtocol = "unsupported_protocol"
	codeUnknownMethod       = "unknown_method"
	codeDatabaseBusy        = "database_busy"
	codeInternal            = "internal"
)

type boundaryError struct {
	code    string
	message string
	outcome Outcome
}

func invalidRequest(message string) *boundaryError {
	return &boundaryError{code: codeInvalidRequest, message: message, outcome: OutcomeInvalidRequest}
}

func unsupportedProtocol(message string) *boundaryError {
	return &boundaryError{code: codeUnsupportedProtocol, message: message, outcome: OutcomeInvalidRequest}
}

func unknownMethod(method string) *boundaryError {
	message := "RPC method is not supported"
	if method == "" {
		message = "RPC method is required"
	}
	return &boundaryError{code: codeUnknownMethod, message: message, outcome: OutcomeInvalidRequest}
}

func SafeMessage(err error) string {
	return mapOperationError(err).message
}

func mapOperationError(err error) *boundaryError {
	switch {
	case errors.Is(err, madeleine.ErrNotFound):
		return operationError(codeNotFound, "requested object was not found")
	case errors.Is(err, madeleine.ErrConflict):
		return operationError(codeConflict, "operation conflicts with existing state")
	case errors.Is(err, madeleine.ErrInvalidState):
		return operationError(codeInvalidState, "operation is not valid in the current state")
	case errors.Is(err, madeleine.ErrNotGitRepository):
		return operationError(codeNotGitRepository, "path is not inside a Git repository")
	case errors.Is(err, madeleine.ErrOutsideRepository):
		return operationError(codeOutsideRepository, "path is outside the repository")
	case isDatabaseBusy(err):
		return operationError(codeDatabaseBusy, "database is busy")
	default:
		return operationError(codeInternal, "internal error")
	}
}

func operationError(code, message string) *boundaryError {
	return &boundaryError{code: code, message: message, outcome: OutcomeOperationFailure}
}

func isDatabaseBusy(err error) bool {
	var sqliteError interface{ Code() int }
	if !errors.As(err, &sqliteError) {
		return false
	}
	code := sqliteError.Code() & 0xff
	return code == 5 || code == 6
}
