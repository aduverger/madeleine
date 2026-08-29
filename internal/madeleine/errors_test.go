package madeleine

import (
	"errors"
	"strings"
	"testing"
)

func TestWrappedSentinelPreservesErrorsIs(t *testing.T) {
	t.Parallel()

	err := wrapError("get capture", "018f", ErrNotFound)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("errors.Is(%v, ErrNotFound) = false", err)
	}
	if !strings.Contains(err.Error(), "get capture") || !strings.Contains(err.Error(), "018f") {
		t.Fatalf("error %q lacks operation or reference", err)
	}
}
