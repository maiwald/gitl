package ui

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/maiwald/gitl/internal/git"
)

// TestRepoOpenedPipedPathDispatchesConcurrentFetches pins down that a piped
// `git show <rev>` startup dispatches its batch of commands (header fetch,
// list load, worktree entries, position resolution) without erroring — the
// piped path doesn't gate on hasCommits the way the plain path does (see
// TestRepoOpenedPlainPathErrorsOnNoCommits).
func TestRepoOpenedPipedPathDispatchesConcurrentFetches(t *testing.T) {
	m := Model{start: StartTarget{Piped: &git.PipedPatch{Hash: "abc123"}}, jl: &jumplist{}}
	newModel, cmd := m.Update(repoOpenedMsg{repo: &git.Repo{Path: "."}, hasCommits: true})
	m = newModel.(Model)
	if cmd == nil {
		t.Fatal("expected a batched command for the piped-show path")
	}
	if m.err != nil {
		t.Fatalf("err = %v, want nil", m.err)
	}
}

// TestPipedJumpAppliesRegardlessOfArrivalOrder pins down that listPositionMsg
// and commitsLoadedMsg are independent concurrent fetches that can land in
// either order, and both orders end with the cursor on the resolved
// position — see pendingListPos's doc comment.
func TestPipedJumpAppliesRegardlessOfArrivalOrder(t *testing.T) {
	t.Run("position before commits", func(t *testing.T) {
		m := Model{start: StartTarget{Piped: &git.PipedPatch{Hash: "abc"}}, jl: &jumplist{}}
		m.width, m.height = 80, 24
		newModel, _ := m.Update(listPositionMsg{pos: 2, found: true})
		m = newModel.(Model)
		newModel, _ = m.Update(commitsLoadedMsg{commits: fixtureCommits(5), totalCount: 5})
		m = newModel.(Model)
		if m.list.cursor != 2 {
			t.Fatalf("cursor = %d, want 2", m.list.cursor)
		}
	})

	t.Run("commits before position", func(t *testing.T) {
		m := Model{start: StartTarget{Piped: &git.PipedPatch{Hash: "abc"}}, jl: &jumplist{}}
		m.width, m.height = 80, 24
		newModel, _ := m.Update(commitsLoadedMsg{commits: fixtureCommits(5), totalCount: 5})
		m = newModel.(Model)
		newModel, _ = m.Update(listPositionMsg{pos: 2, found: true})
		m = newModel.(Model)
		if m.list.cursor != 2 {
			t.Fatalf("cursor = %d, want 2", m.list.cursor)
		}
	})
}

// TestPipedJumpUnreachableLeavesCursorAtTopWithoutError pins down the documented
// fallback: a commit that isn't reachable from HEAD
// (listPositionMsg{found: false}) leaves the cursor wherever it already was,
// with no error surfaced — the detail view still opens from the piped patch.
func TestPipedJumpUnreachableLeavesCursorAtTopWithoutError(t *testing.T) {
	m := Model{start: StartTarget{Piped: &git.PipedPatch{Hash: "abc"}}, jl: &jumplist{}}
	m.width, m.height = 80, 24
	newModel, _ := m.Update(commitsLoadedMsg{commits: fixtureCommits(5), totalCount: 5})
	m = newModel.(Model)
	newModel, _ = m.Update(listPositionMsg{found: false})
	m = newModel.(Model)

	if m.err != nil {
		t.Fatalf("err = %v, want nil", m.err)
	}
	if m.list.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 (unchanged)", m.list.cursor)
	}
}

// TestResolveListPositionFindsCommitAndCachesOrder pins the position a piped
// commit resolves to against git's own log order, and that the order it was
// read from is kept, so the first search doesn't query it again.
func TestResolveListPositionFindsCommitAndCachesOrder(t *testing.T) {
	dir := t.TempDir()
	runGit := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return strings.TrimSpace(string(out))
	}
	runGit("init", "-q", "-b", "main")
	for _, subject := range []string{"one", "two", "three"} {
		runGit("commit", "-q", "--allow-empty", "-m", subject)
	}
	middle := runGit("rev-parse", "HEAD~1")
	runGit("checkout", "-q", "-b", "side", "HEAD~1")
	runGit("commit", "-q", "--allow-empty", "-m", "side")
	unreachable := runGit("rev-parse", "HEAD")
	runGit("checkout", "-q", "main")

	repo := &git.Repo{Path: dir}
	msg := resolveListPositionCmd(repo, middle)().(listPositionMsg)
	if !msg.found || msg.pos != 1 {
		t.Fatalf("got pos=%d found=%v, want pos=1 found=true", msg.pos, msg.found)
	}
	if len(msg.order) != 3 {
		t.Fatalf("order has %d hashes, want 3", len(msg.order))
	}

	m := Model{start: StartTarget{Piped: &git.PipedPatch{Hash: middle}}, jl: &jumplist{}}
	newModel, _ := m.Update(msg)
	if got := newModel.(Model).allHashes; !slices.Equal(got, msg.order) {
		t.Fatalf("allHashes = %v, want the resolved order cached", got)
	}

	if msg := resolveListPositionCmd(repo, unreachable)().(listPositionMsg); msg.found {
		t.Fatalf("an unreachable commit resolved to pos=%d, want found=false", msg.pos)
	}
}
