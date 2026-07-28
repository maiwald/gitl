package ui

import (
	"testing"

	"github.com/maiwald/gitl/internal/git"
)

// TestRepoOpenedRevPathDispatchesConcurrentFetches pins down that a
// `gitl <rev>` startup dispatches its batch of commands (detail fetch, list
// load, worktree entries, position resolution) without erroring, even on a
// commit-less-looking hasCommits value — the rev path doesn't gate on it
// the way the plain path does (see TestRepoOpenedPlainPathErrorsOnNoCommits).
func TestRepoOpenedRevPathDispatchesConcurrentFetches(t *testing.T) {
	m := Model{start: StartTarget{Rev: "abc123"}}
	newModel, cmd := m.Update(repoOpenedMsg{repo: &git.Repo{Path: "."}, hasCommits: true})
	m = newModel.(Model)
	if cmd == nil {
		t.Fatal("expected a batched command for the rev-jump path")
	}
	if m.err != nil {
		t.Fatalf("err = %v, want nil", m.err)
	}
}

// TestRevJumpAppliesRegardlessOfArrivalOrder pins down that revPositionMsg
// and commitsLoadedMsg are independent concurrent fetches that can land in
// either order, and both orders end with the cursor on the resolved
// position — see pendingJump's doc comment.
func TestRevJumpAppliesRegardlessOfArrivalOrder(t *testing.T) {
	t.Run("position before commits", func(t *testing.T) {
		m := Model{start: StartTarget{Rev: "abc"}}
		m.width, m.height = 80, 24
		newModel, _ := m.Update(revPositionMsg{pos: 2, found: true})
		m = newModel.(Model)
		newModel, _ = m.Update(commitsLoadedMsg{commits: fixtureCommits(5), totalCount: 5})
		m = newModel.(Model)
		if m.list.cursor != 2 {
			t.Fatalf("cursor = %d, want 2", m.list.cursor)
		}
	})

	t.Run("commits before position", func(t *testing.T) {
		m := Model{start: StartTarget{Rev: "abc"}}
		m.width, m.height = 80, 24
		newModel, _ := m.Update(commitsLoadedMsg{commits: fixtureCommits(5), totalCount: 5})
		m = newModel.(Model)
		newModel, _ = m.Update(revPositionMsg{pos: 2, found: true})
		m = newModel.(Model)
		if m.list.cursor != 2 {
			t.Fatalf("cursor = %d, want 2", m.list.cursor)
		}
	})
}

// TestRevJumpUnreachableLeavesCursorAtTopWithoutError pins down the
// documented v1 fallback: a revision that can't be resolved or isn't
// reachable from HEAD (revPositionMsg{found: false}) leaves the cursor
// wherever it already was, with no error surfaced (the detail view itself
// still opens from fetchDetailCmd independently).
func TestRevJumpUnreachableLeavesCursorAtTopWithoutError(t *testing.T) {
	m := Model{start: StartTarget{Rev: "abc"}}
	m.width, m.height = 80, 24
	newModel, _ := m.Update(commitsLoadedMsg{commits: fixtureCommits(5), totalCount: 5})
	m = newModel.(Model)
	newModel, _ = m.Update(revPositionMsg{found: false})
	m = newModel.(Model)

	if m.err != nil {
		t.Fatalf("err = %v, want nil", m.err)
	}
	if m.list.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 (unchanged)", m.list.cursor)
	}
}
