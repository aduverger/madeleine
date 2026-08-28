package repopath

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

var ErrOutsideRepository = errors.New("path is outside repository")

func Normalize(worktreeRoot, input string) (string, error) {
	if input == "" {
		return "", wrapError(input, ErrOutsideRepository)
	}

	root, err := filepath.Abs(worktreeRoot)
	if err != nil {
		return "", wrapError(worktreeRoot, err)
	}
	root = filepath.Clean(root)

	candidate := input
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}
	candidate, err = filepath.Abs(candidate)
	if err != nil {
		return "", wrapError(input, err)
	}
	candidate = filepath.Clean(candidate)

	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return "", wrapError(input, errors.Join(ErrOutsideRepository, err))
	}
	if relative == "." || relative == "" || isRelativeTraversal(relative) {
		return "", wrapError(input, ErrOutsideRepository)
	}

	return filepath.ToSlash(relative), nil
}

func isRelativeTraversal(path string) bool {
	return path == ".." || strings.HasPrefix(path, "../") || strings.HasPrefix(path, `..\`)
}

func wrapError(reference string, err error) error {
	if reference == "" {
		return fmt.Errorf("normalize repository path: %w", err)
	}
	return fmt.Errorf("normalize repository path %q: %w", reference, err)
}
