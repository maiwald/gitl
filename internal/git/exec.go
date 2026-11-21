package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// runGit runs git with the given arguments in dir and returns stdout, even
// on failure (needed by runGitNoIndexDiff, where a nonzero
// exit still carries the diff on stdout); the error includes stderr output
// for diagnosability.
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

// runGitNoIndexDiff runs a `git diff --no-index` variant, which exits 1 when
// the files differ (stdout still holds the diff) and 0 only when they don't.
// Any other failure is a real error — see isExitCode.
func runGitNoIndexDiff(ctx context.Context, dir string, args ...string) ([]byte, error) {
	out, err := runGit(ctx, dir, args...)
	if err != nil && !isExitCode(err, 1) {
		return nil, err
	}
	return out, nil
}

// isExitCode reports whether err is (or wraps) a git process exiting with
// exactly code — the "exit status is itself the answer" pattern behind `git
// rev-parse --verify --quiet` (1 = no such ref) and `git diff --quiet` /
// `--no-index` (1 = differences found).
func isExitCode(err error, code int) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == code
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

// splitLines trims trailing newlines from out and splits it into
// non-empty lines. It returns nil for empty input.
func splitLines(out []byte) []string {
	return splitRecords(out, "\n")
}

// splitNullTerminated splits NUL-delimited git output (from a -z flag) into
// non-empty entries. Unlike splitLines, this is safe for paths that
// themselves contain newlines and avoids git's own quoting of unusual path
// characters, since -z output is never quoted.
func splitNullTerminated(out []byte) []string {
	return splitRecords(out, "\x00")
}

// splitRecords is the shared body of both. git's record-per-entry output
// always ends with a trailing separator, sometimes a run of them, so those are
// trimmed and empty entries dropped rather than reported.
func splitRecords(out []byte, sep string) []string {
	text := strings.TrimRight(string(out), sep)
	if text == "" {
		return nil
	}
	parts := strings.Split(text, sep)
	// Filtered in place: parts is this function's own, and for the usual
	// output (no empty entries) nothing moves at all.
	result := parts[:0]
	for _, p := range parts {
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
