package store

import (
	"context"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/aduverger/madeleine/internal/gitcmd"
)

var errUnsupportedOrigin = errors.New("unsupported repository origin")

func ResolveRepository(ctx context.Context, path string) (Repository, error) {
	workingDirectory, err := repositoryWorkingDirectory(path)
	if err != nil {
		return Repository{}, wrapError("resolve repository", path, err)
	}

	rootOutput, err := runDiscovery(ctx, workingDirectory, []string{"rev-parse", "--show-toplevel"})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Repository{}, wrapError("resolve repository", path, err)
		}
		var commandError *gitcmd.CommandError
		if errors.As(err, &commandError) && commandError.ExitStatus >= 0 {
			return Repository{}, wrapError("resolve repository", path, errors.Join(ErrNotGitRepository, err))
		}
		return Repository{}, wrapError("resolve repository", path, err)
	}
	worktreeRoot, err := canonicalGitPath(workingDirectory, rootOutput)
	if err != nil {
		return Repository{}, wrapError("resolve worktree root", path, err)
	}

	commonOutput, err := runDiscovery(ctx, workingDirectory, []string{"rev-parse", "--git-common-dir"})
	if err != nil {
		return Repository{}, wrapError("resolve Git common directory", path, err)
	}
	gitCommonDir, err := canonicalGitPath(workingDirectory, commonOutput)
	if err != nil {
		return Repository{}, wrapError("resolve Git common directory", path, err)
	}

	origin, err := discoverOrigin(ctx, workingDirectory)
	if err != nil {
		return Repository{}, wrapError("resolve repository origin", path, err)
	}

	return Repository{
		WorktreeRoot: worktreeRoot,
		GitCommonDir: gitCommonDir,
		Origin:       origin,
	}, nil
}

func repositoryWorkingDirectory(path string) (string, error) {
	if path == "" {
		return "", ErrNotGitRepository
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		resolved = filepath.Dir(resolved)
	}
	return filepath.Clean(resolved), nil
}

func canonicalGitPath(workingDirectory string, output []byte) (string, error) {
	value := strings.TrimSuffix(string(output), "\n")
	value = strings.TrimSuffix(value, "\r")
	if value == "" {
		return "", errors.New("Git returned an empty path")
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(workingDirectory, value)
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}

func runDiscovery(ctx context.Context, workingDirectory string, args []string) ([]byte, error) {
	return gitcmd.Run(ctx, "git", workingDirectory, args, gitcmd.DiscoveryTimeout)
}

func discoverOrigin(ctx context.Context, workingDirectory string) (string, error) {
	output, err := runDiscovery(ctx, workingDirectory, []string{"config", "--get", "remote.origin.url"})
	if err != nil {
		var commandError *gitcmd.CommandError
		if errors.As(err, &commandError) && commandError.ExitStatus == 1 {
			return "", nil
		}
		return "", err
	}

	normalized, err := normalizeOrigin(string(output))
	if errors.Is(err, errUnsupportedOrigin) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return normalized, nil
}

func normalizeOrigin(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("repository origin is empty")
	}
	if strings.IndexFunc(trimmed, unicode.IsControl) >= 0 {
		return "", errors.New("repository origin contains a control character")
	}

	if !strings.Contains(trimmed, "://") {
		colon := strings.IndexByte(trimmed, ':')
		slash := strings.IndexByte(trimmed, '/')
		if colon <= 0 || (slash >= 0 && slash < colon) {
			return "", errUnsupportedOrigin
		}
		hostPart := trimmed[:colon]
		if at := strings.LastIndexByte(hostPart, '@'); at >= 0 {
			hostPart = hostPart[at+1:]
		}
		return normalizedHostPath(hostPart, trimmed[colon+1:])
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", errors.New("repository origin URL is invalid")
	}
	if strings.EqualFold(parsed.Scheme, "file") {
		return "", errUnsupportedOrigin
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("repository origin URL is invalid")
	}
	host := parsed.Hostname()
	if host == "" {
		return "", errors.New("repository origin host is empty")
	}
	if port := parsed.Port(); port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return normalizedHostPath(host, parsed.Path)
}

func normalizedHostPath(host, repositoryPath string) (string, error) {
	host = strings.ToLower(strings.TrimSpace(host))
	repositoryPath = strings.TrimLeft(repositoryPath, "/")
	repositoryPath = strings.TrimRight(repositoryPath, "/")
	repositoryPath = strings.TrimSuffix(repositoryPath, ".git")
	repositoryPath = strings.TrimRight(repositoryPath, "/")
	if host == "" || repositoryPath == "" {
		return "", errors.New("repository origin must contain a host and path")
	}
	if strings.IndexFunc(host+repositoryPath, unicode.IsControl) >= 0 {
		return "", errors.New("repository origin contains a control character")
	}
	return host + "/" + repositoryPath, nil
}
