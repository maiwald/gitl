package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// runGit runs git with the given arguments in dir and returns stdout. On
// failure, stdout is still returned alongside the error (needed by
// runGitDiffExit's --no-index case, where a nonzero exit still carries the
// diff on stdout) and the error includes stderr output for diagnosability.
func runGit(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		stderrText := strings.TrimSpace(stderr.String())
		if stderrText != "" {
			return stdout.Bytes(), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, stderrText)
		}
		return stdout.Bytes(), fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}

	return stdout.Bytes(), nil
}

// runGitDiffExit runs a git diff variant whose exit status is itself part of
// the answer, not a failure: 0 means no differences, 1 means differences
// were found (and for --no-index, stdout still holds the diff), anything
// else is a real error. Mirrors the exit-code-as-signal pattern
// Repo.HasCommits uses for `git rev-parse --verify`.
func runGitDiffExit(ctx context.Context, dir string, args ...string) (out []byte, differs bool, err error) {
	out, err = runGit(ctx, dir, args...)
	if err == nil {
		return out, false, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return out, true, nil
	}
	return nil, false, err
}

// runGitTrimmed runs git like runGit, but trims the trailing whitespace
// from stdout — for commands whose entire output is a single scalar value
// (a hash, a count, a boolean).
func runGitTrimmed(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := runGit(ctx, dir, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// trimTrailingNewlines strips trailing "\n" characters from out.
func trimTrailingNewlines(out []byte) string {
	return strings.TrimRight(string(out), "\n")
}

// splitLines trims trailing newlines from out and splits it into
// non-empty lines. It returns nil for empty input.
func splitLines(out []byte) []string {
	text := trimTrailingNewlines(out)
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	result := make([]string, 0, len(lines))
	for _, l := range lines {
		if l != "" {
			result = append(result, l)
		}
	}
	return result
}
