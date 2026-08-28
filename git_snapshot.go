package madeleine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aduverger/madeleine/internal/gitcmd"
)

const gitObservationTimeout = 20 * time.Second

type gitPathSnapshot struct {
	PorcelainStatus     string
	WorktreeFingerprint string
	IndexIdentity       string
}

type gitSnapshot struct {
	Head       string
	HeadExists bool
	Paths      map[string]gitPathSnapshot
}

type gitStatusEntry struct {
	Path            string
	PorcelainStatus string
}

func captureGitSnapshot(ctx context.Context, worktreeRoot string, additionalPaths []string) (gitSnapshot, error) {
	head, headExists, err := observeGitHead(ctx, worktreeRoot)
	if err != nil {
		return gitSnapshot{}, err
	}
	statusOutput, err := runGitObservation(ctx, worktreeRoot,
		"status", "--porcelain=v1", "-z", "--untracked-files=all", "--no-renames")
	if err != nil {
		return gitSnapshot{}, fmt.Errorf("observe Git status: %w", err)
	}
	statusEntries, err := parsePorcelainStatus(statusOutput)
	if err != nil {
		return gitSnapshot{}, err
	}
	paths := make(map[string]gitPathSnapshot, len(statusEntries)+len(additionalPaths))
	for _, entry := range statusEntries {
		path, err := normalizeRepositoryPath(worktreeRoot, entry.Path)
		if err != nil {
			return gitSnapshot{}, fmt.Errorf("normalize Git status path %q: %w", entry.Path, err)
		}
		if _, duplicate := paths[path]; duplicate {
			return gitSnapshot{}, fmt.Errorf("Git status contains duplicate path %q", path)
		}
		paths[path] = gitPathSnapshot{PorcelainStatus: entry.PorcelainStatus}
	}
	for _, path := range additionalPaths {
		normalized, err := normalizeRepositoryPath(worktreeRoot, path)
		if err != nil {
			return gitSnapshot{}, err
		}
		if _, exists := paths[normalized]; !exists {
			paths[normalized] = gitPathSnapshot{}
		}
	}

	indexIdentities := map[string]string{}
	if len(paths) > 0 {
		indexOutput, err := runGitObservation(ctx, worktreeRoot, "ls-files", "--stage", "-z")
		if err != nil {
			return gitSnapshot{}, fmt.Errorf("observe Git index: %w", err)
		}
		indexIdentities, err = parseIndexIdentities(indexOutput)
		if err != nil {
			return gitSnapshot{}, err
		}
	}

	for path, snapshot := range paths {
		fingerprint, err := fingerprintWorktreePath(worktreeRoot, path)
		if err != nil {
			return gitSnapshot{}, fmt.Errorf("fingerprint %q: %w", path, err)
		}
		snapshot.WorktreeFingerprint = fingerprint
		snapshot.IndexIdentity = indexIdentities[path]
		paths[path] = snapshot
	}
	return gitSnapshot{Head: head, HeadExists: headExists, Paths: paths}, nil
}

func observeGitHead(ctx context.Context, worktreeRoot string) (string, bool, error) {
	output, err := runGitObservation(ctx, worktreeRoot, "rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil {
		var commandError *gitcmd.CommandError
		if errors.As(err, &commandError) && commandError.ExitStatus == 1 {
			return "", false, nil
		}
		return "", false, fmt.Errorf("observe Git HEAD: %w", err)
	}
	head := strings.TrimSpace(string(output))
	if head == "" {
		return "", false, errors.New("observe Git HEAD: Git returned an empty object ID")
	}
	return head, true, nil
}

func runGitObservation(ctx context.Context, worktreeRoot string, args ...string) ([]byte, error) {
	readOnlyArgs := make([]string, 0, len(args)+1)
	readOnlyArgs = append(readOnlyArgs, "--no-optional-locks")
	readOnlyArgs = append(readOnlyArgs, args...)
	return gitcmd.Run(ctx, "git", worktreeRoot, readOnlyArgs, gitObservationTimeout)
}

func parsePorcelainStatus(output []byte) ([]gitStatusEntry, error) {
	records := splitNullRecords(output)
	entries := make([]gitStatusEntry, 0, len(records))
	for index := 0; index < len(records); index++ {
		record := records[index]
		if len(record) < 4 || record[2] != ' ' {
			return nil, fmt.Errorf("parse Git status: invalid porcelain record %q", record)
		}
		status := string(record[:2])
		entries = append(entries, gitStatusEntry{Path: string(record[3:]), PorcelainStatus: status})
		if record[0] == 'R' || record[0] == 'C' || record[1] == 'R' || record[1] == 'C' {
			index++
			if index >= len(records) {
				return nil, errors.New("parse Git status: rename or copy has no second path")
			}
			entries = append(entries, gitStatusEntry{Path: string(records[index]), PorcelainStatus: status})
		}
	}
	return entries, nil
}

func parseIndexIdentities(output []byte) (map[string]string, error) {
	identitiesByPath := make(map[string][]string)
	for _, record := range splitNullRecords(output) {
		tab := bytes.IndexByte(record, '\t')
		if tab < 0 {
			return nil, fmt.Errorf("parse Git index: invalid stage record %q", record)
		}
		identity, path := string(record[:tab]), string(record[tab+1:])
		if path == "" || len(strings.Fields(identity)) != 3 {
			return nil, fmt.Errorf("parse Git index: invalid stage record %q", record)
		}
		identitiesByPath[path] = append(identitiesByPath[path], identity)
	}

	identities := make(map[string]string, len(identitiesByPath))
	for path, values := range identitiesByPath {
		sort.Strings(values)
		identities[path] = strings.Join(values, "\n")
	}
	return identities, nil
}

func splitNullRecords(output []byte) [][]byte {
	if len(output) == 0 {
		return nil
	}
	records := make([][]byte, 0, bytes.Count(output, []byte{0})+1)
	for len(output) > 0 {
		end := bytes.IndexByte(output, 0)
		if end < 0 {
			records = append(records, output)
			break
		}
		if end > 0 {
			records = append(records, output[:end])
		}
		output = output[end+1:]
	}
	return records
}

func fingerprintWorktreePath(worktreeRoot, repositoryPath string) (string, error) {
	path := filepath.Join(worktreeRoot, filepath.FromSlash(repositoryPath))
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		sum := sha256.Sum256([]byte("missing\x00"))
		return fmt.Sprintf("sha256:%x", sum), nil
	}
	if err != nil {
		return "", err
	}

	hash := sha256.New()
	_, _ = io.WriteString(hash, "present\x00")
	_, _ = io.WriteString(hash, strconv.FormatUint(uint64(info.Mode()), 10))
	_, _ = io.WriteString(hash, "\x00")
	switch {
	case info.Mode().IsRegular():
		file, err := os.Open(path)
		if err != nil {
			return "", err
		}
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", closeErr
		}
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(path)
		if err != nil {
			return "", err
		}
		_, _ = io.WriteString(hash, target)
	}
	return fmt.Sprintf("sha256:%x", hash.Sum(nil)), nil
}
