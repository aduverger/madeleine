package madeleine

import (
	"errors"
	"fmt"

	"github.com/aduverger/madeleine/internal/repopath"
)

var (
	ErrNotFound          = errors.New("not found")
	ErrConflict          = errors.New("conflict")
	ErrInvalidState      = errors.New("invalid state")
	ErrNotGitRepository  = errors.New("not a Git repository")
	ErrOutsideRepository = repopath.ErrOutsideRepository
)

type operationError struct {
	op        string
	reference string
	err       error
}

func (e *operationError) Error() string {
	if e.reference == "" {
		return fmt.Sprintf("%s: %v", e.op, e.err)
	}
	return fmt.Sprintf("%s %q: %v", e.op, e.reference, e.err)
}

func (e *operationError) Unwrap() error {
	return e.err
}

func wrapError(op, reference string, err error) error {
	if err == nil {
		return nil
	}
	return &operationError{op: op, reference: reference, err: err}
}
