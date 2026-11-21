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

// fixtureCommitEnv is the fixed author/committer identity and timestamp
// used by initFixtureRepo, so commit hashes and dates are deterministic.
var fixtureCommitEnv = []string{
	"GIT_AUTHOR_NAME=Test Author",
	"GIT_AUTHOR_EMAIL=test@example.com",
	"GIT_COMMITTER_NAME=Test Author",
	"GIT_COMMITTER_EMAIL=test@example.com",
	"GIT_AUTHOR_DATE=2024-01-01T12:00:00+00:00",
	"GIT_COMMITTER_DATE=2024-01-01T12:00:00+00:00",
}

// initFixtureDir makes dir an empty repository on main with a committer
// identity configured, for the tests that commit without fixtureCommitEnv.
func initFixtureDir(t *testing.T, dir string) {
	t.Helper()
	runGitCmd(t, dir, nil, "init", "-b", "main")
	runGitCmd(t, dir, nil, "config", "user.name", "Test Author")
	runGitCmd(t, dir, nil, "config", "user.email", "test@example.com")
}

// mustOpen opens the repository at dir, failing the test on error.
func mustOpen(t *testing.T, dir string) *Repo {
	t.Helper()
	repo, err := OpenRepo(context.Background(), dir)
	if err != nil {
		t.Fatalf("OpenRepo: %v", err)
	}
	return repo
}

// newestFirst returns hashes in the opposite order — fixtures create commits
// oldest first, while git log lists them newest first.
func newestFirst(hashes []string) []string {
	out := make([]string, len(hashes))
	for i, h := range hashes {
		out[len(hashes)-1-i] = h
	}
	return out
}
