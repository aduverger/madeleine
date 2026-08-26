package madeleine

import (
	"errors"
	"path/filepath"
	"strings"
)

func normalizeRepositoryPath(worktreeRoot, input string) (string, error) {
	if input == "" {
		return "", wrapError("normalize repository path", input, ErrOutsideRepository)
	}

	root, err := filepath.Abs(worktreeRoot)
	if err != nil {
		return "", wrapError("normalize repository path", worktreeRoot, err)
	}
	root = filepath.Clean(root)

	candidate := input
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}
	candidate, err = filepath.Abs(candidate)
	if err != nil {
		return "", wrapError("normalize repository path", input, err)
	}
	candidate = filepath.Clean(candidate)

	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return "", wrapError("normalize repository path", input, errors.Join(ErrOutsideRepository, err))
	}
	if relative == "." || relative == "" || isRelativeTraversal(relative) {
		return "", wrapError("normalize repository path", input, ErrOutsideRepository)
	}

	return filepath.ToSlash(relative), nil
}

func isRelativeTraversal(path string) bool {
	return path == ".." || strings.HasPrefix(path, "../") || strings.HasPrefix(path, `..\`)
}
