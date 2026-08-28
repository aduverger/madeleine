package gitstate

import (
	"context"
	"fmt"
	"sort"

	"github.com/aduverger/madeleine/internal/repopath"
)

func Reconcile(ctx context.Context, worktreeRoot string, start, end Snapshot) ([]string, error) {
	finalPaths := make(map[string]struct{})
	committedPaths, err := changedCommittedPaths(ctx, worktreeRoot, start, end)
	if err != nil {
		return nil, err
	}
	for _, path := range committedPaths {
		finalPaths[path] = struct{}{}
	}
	for path := range end.Paths {
		if _, dirtyAtStart := start.Paths[path]; !dirtyAtStart {
			finalPaths[path] = struct{}{}
		}
	}
	for path, startState := range start.Paths {
		if end.Paths[path] != startState {
			finalPaths[path] = struct{}{}
		}
	}

	paths := make([]string, 0, len(finalPaths))
	for path := range finalPaths {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

func changedCommittedPaths(ctx context.Context, worktreeRoot string, start, end Snapshot) ([]string, error) {
	switch {
	case start.HeadExists && end.HeadExists:
		if start.Head == end.Head {
			return nil, nil
		}
		return changedPathsBetweenHeads(ctx, worktreeRoot, start.Head, end.Head)
	case start.HeadExists:
		return pathsInGitTree(ctx, worktreeRoot, start.Head)
	case end.HeadExists:
		return pathsInGitTree(ctx, worktreeRoot, end.Head)
	default:
		return nil, nil
	}
}

func changedPathsBetweenHeads(ctx context.Context, worktreeRoot, startHead, endHead string) ([]string, error) {
	output, err := runGitObservation(ctx, worktreeRoot,
		"diff", "--name-only", "-z", "--no-renames", startHead+".."+endHead, "--")
	if err != nil {
		return nil, fmt.Errorf("observe Git commit range: %w", err)
	}
	return normalizeGitPathList(worktreeRoot, "diff", output)
}

func pathsInGitTree(ctx context.Context, worktreeRoot, head string) ([]string, error) {
	output, err := runGitObservation(ctx, worktreeRoot, "ls-tree", "-r", "-z", "--name-only", head)
	if err != nil {
		return nil, fmt.Errorf("observe Git tree: %w", err)
	}
	return normalizeGitPathList(worktreeRoot, "tree", output)
}

func normalizeGitPathList(worktreeRoot, source string, output []byte) ([]string, error) {
	paths := make([]string, 0)
	seen := make(map[string]struct{})
	for _, record := range splitNullRecords(output) {
		path, err := repopath.Normalize(worktreeRoot, string(record))
		if err != nil {
			return nil, fmt.Errorf("normalize Git %s path %q: %w", source, record, err)
		}
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}
