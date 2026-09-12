package git

import (
	"context"
	"testing"
)

// TestFallbackPagerFollowsGitsPrecedence pins the order a pager is resolved
// in against git's own: GIT_PAGER, core.pager, PAGER, less.
func TestFallbackPagerFollowsGitsPrecedence(t *testing.T) {
	repo, _ := initFixtureRepo(t, 1)
	ctx := context.Background()

	// The developer's own git config must not decide what this reports.
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	t.Setenv("GIT_PAGER", "")
	t.Setenv("PAGER", "")

	if got := FallbackPager(ctx, repo.Path); got != defaultPager {
		t.Errorf("with nothing configured = %q, want %q", got, defaultPager)
	}

	t.Setenv("PAGER", "pager-env")
	if got := FallbackPager(ctx, repo.Path); got != "pager-env" {
		t.Errorf("with PAGER set = %q, want the PAGER value", got)
	}

	runGitCmd(t, repo.Path, nil, "config", "core.pager", "core-pager --raw")
	if got := FallbackPager(ctx, repo.Path); got != "core-pager --raw" {
		t.Errorf("with core.pager set = %q, want the config value", got)
	}

	t.Setenv("GIT_PAGER", "git-pager-env")
	if got := FallbackPager(ctx, repo.Path); got != "git-pager-env" {
		t.Errorf("with GIT_PAGER set = %q, want the GIT_PAGER value", got)
	}
}

// TestFallbackPagerSkipsGitl covers the recursion guard: a candidate naming
// gitl would hand gitl's input back to gitl.
func TestFallbackPagerSkipsGitl(t *testing.T) {
	repo, _ := initFixtureRepo(t, 1)
	ctx := context.Background()

	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	t.Setenv("GIT_PAGER", "/usr/local/bin/gitl")
	t.Setenv("PAGER", "pager-env")

	if got := FallbackPager(ctx, repo.Path); got != "pager-env" {
		t.Errorf("with GIT_PAGER=gitl = %q, want the next candidate down", got)
	}
}

// TestFallbackPagerOutsideARepo covers the pathological case of a piped run
// whose cwd git can't read a config from: the env and the default still
// answer, since nothing about them needs a repo.
func TestFallbackPagerOutsideARepo(t *testing.T) {
	t.Setenv("GIT_PAGER", "")
	t.Setenv("PAGER", "")

	if got := FallbackPager(context.Background(), "/"); got != defaultPager {
		t.Errorf("outside a repo = %q, want %q", got, defaultPager)
	}
}
