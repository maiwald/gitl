package git

import (
	"context"
	"os/exec"
	"testing"
)

// runGitCmd runs git with args in dir, with env appended to the current
// environment, failing the test immediately on error. It returns combined
// stdout+stderr, which is enough for the fixture-setup commands these tests
// use it for (init, config, commit, checkout, merge, rev-parse).
func runGitCmd(t *testing.T, dir string, env []string, args ...string) []byte {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return out
}

// headHash returns the full 40-character hash HEAD resolves to in dir.
func headHash(t *testing.T, dir string) string {
	t.Helper()
	out := runGitCmd(t, dir, nil, "rev-parse", "HEAD")
	return string(out[:40])
}
