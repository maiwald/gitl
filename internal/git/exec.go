package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// runGit runs git with the given arguments in dir and returns stdout.
// On failure, the error includes stderr output for diagnosability.
func runGit(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		stderrText := strings.TrimSpace(stderr.String())
		if stderrText != "" {
			return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, stderrText)
		}
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}

	return stdout.Bytes(), nil
}
