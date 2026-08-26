package gitcmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
	"unicode"
)

const (
	DiscoveryTimeout = 5 * time.Second
	MaxStderrBytes   = 4096
	commandWaitDelay = 100 * time.Millisecond
)

var (
	urlCredentials = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^/@\s]+@`)
	urlQuery       = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://[^?\s'"]+)\?[^\s'"]+`)
	secretValue    = regexp.MustCompile(`(?i)\b(password|passwd|token|secret|api[_-]?key)=([^\s]+)`)
)

type CommandError struct {
	Operation  string
	ExitStatus int
	Stderr     string
	Err        error
}

func (e *CommandError) Error() string {
	status := "could not start"
	if e.ExitStatus >= 0 {
		status = fmt.Sprintf("exited with status %d", e.ExitStatus)
	}
	if e.Stderr == "" {
		return fmt.Sprintf("git %s %s", e.Operation, status)
	}
	return fmt.Sprintf("git %s %s: %s", e.Operation, status, e.Stderr)
}

func (e *CommandError) Unwrap() error {
	return e.Err
}

func Run(ctx context.Context, executable, workingDirectory string, args []string, timeout time.Duration) ([]byte, error) {
	if executable == "" {
		return nil, errors.New("git executable is required")
	}
	if workingDirectory == "" {
		return nil, errors.New("git working directory is required")
	}
	if len(args) == 0 {
		return nil, errors.New("git argument slice is required")
	}
	if timeout <= 0 {
		return nil, errors.New("git timeout must be positive")
	}

	commandContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	command := exec.CommandContext(commandContext, executable, args...)
	command.Dir = workingDirectory
	command.WaitDelay = commandWaitDelay
	var stdout bytes.Buffer
	stderr := &limitedBuffer{limit: MaxStderrBytes}
	command.Stdout = &stdout
	command.Stderr = stderr

	if err := command.Run(); err != nil {
		cause := err
		if commandContext.Err() != nil {
			cause = commandContext.Err()
		}
		exitStatus := -1
		var exitError *exec.ExitError
		if errors.As(err, &exitError) && commandContext.Err() == nil {
			exitStatus = exitError.ExitCode()
		}
		return nil, &CommandError{
			Operation:  sanitizeOperation(args[0]),
			ExitStatus: exitStatus,
			Stderr:     sanitizeStderr(stderr.buffer.Bytes(), stderr.truncated),
			Err:        cause,
		}
	}

	return stdout.Bytes(), nil
}

type limitedBuffer struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	originalLength := len(data)
	remaining := b.limit - b.buffer.Len()
	if remaining <= 0 {
		b.truncated = b.truncated || originalLength > 0
		return originalLength, nil
	}
	if len(data) > remaining {
		_, _ = b.buffer.Write(data[:remaining])
		b.truncated = true
		return originalLength, nil
	}
	_, _ = b.buffer.Write(data)
	return originalLength, nil
}

func sanitizeOperation(operation string) string {
	if operation == "" {
		return "command"
	}
	for _, value := range operation {
		if !(unicode.IsLetter(value) || unicode.IsDigit(value) || value == '-') {
			return "command"
		}
	}
	return operation
}

func sanitizeStderr(data []byte, truncated bool) string {
	text := strings.ToValidUTF8(string(data), "�")
	text = urlCredentials.ReplaceAllString(text, `${1}<redacted>@`)
	text = urlQuery.ReplaceAllString(text, `${1}?<redacted>`)
	text = secretValue.ReplaceAllString(text, `${1}=<redacted>`)
	text = strings.Map(func(value rune) rune {
		if unicode.IsControl(value) {
			return ' '
		}
		return value
	}, text)
	text = strings.Join(strings.Fields(text), " ")
	if truncated {
		if text == "" {
			return "…"
		}
		return text + " …"
	}
	return text
}
