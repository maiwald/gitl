package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/maiwald/gitl/internal/git"
)

// TestEnterOnSyntheticRowOpensDetailSynchronously pins down that opening a
// synthetic row needs no fetch command (its files are already loaded from
// startup, unlike a real commit which is only ever loaded on demand), and
// that Esc afterward returns to the list with the cursor preserved, the
// same as it does for a real commit (see
// TestEnterRequestsDetailAndEscReturnsToSameListState in app_detail_test.go).
func TestEnterOnSyntheticRowOpensDetailSynchronously(t *testing.T) {
	list := NewListModel(fixtureCommits(3))
	list.SetSize(80, 10)
	list.SetLeadingEntries([]git.WorktreeEntry{
		{Kind: git.EntryWorktree, Files: []git.File{{Path: "a.txt", Additions: 2}}},
	})
	list.Update(tea.KeyMsg{Type: tea.KeyUp}) // move cursor onto the synthetic row

	m := Model{active: viewList, list: list}
	m.width, m.height = 80, 24
	cursorBefore := m.list.cursor

	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newModel.(Model)
	if cmd != nil {
		t.Fatal("Enter on a synthetic row should open the detail view synchronously, with no command")
	}
	if m.active != viewDetail {
		t.Fatalf("active view = %v, want viewDetail", m.active)
	}
	if !strings.Contains(m.View(), "Working tree") {
		t.Fatalf("detail view = %q, want it to show the entry title", m.View())
	}

	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = newModel.(Model)
	if m.active != viewList {
		t.Fatalf("active view = %v, want viewList after esc", m.active)
	}
	if m.list.cursor != cursorBefore {
		t.Fatalf("list cursor = %d, want %d (preserved across detail view)", m.list.cursor, cursorBefore)
	}
}

// TestWorktreeEntriesMirroredRegardlessOfArrivalOrder pins down that
// worktreeEntriesLoadedMsg and commitsLoadedMsg are independent concurrent
// fetches that can land in either order, and both orders end up with the
// entries applied to the list — see the doc comment on
// worktreeEntriesLoadedMsg.
func TestWorktreeEntriesMirroredRegardlessOfArrivalOrder(t *testing.T) {
	entries := []git.WorktreeEntry{{Kind: git.EntryStaged, Files: []git.File{{Path: "a.txt"}}}}

	t.Run("entries before commits", func(t *testing.T) {
		m := Model{}
		m.width, m.height = 80, 24
		newModel, _ := m.Update(worktreeEntriesLoadedMsg{entries: entries})
		m = newModel.(Model)
		newModel, _ = m.Update(commitsLoadedMsg{commits: fixtureCommits(3), totalCount: 3})
		m = newModel.(Model)
		if len(m.list.leading) != 1 {
			t.Fatalf("list.leading = %v, want 1 entry", m.list.leading)
		}
	})

	t.Run("commits before entries", func(t *testing.T) {
		m := Model{}
		m.width, m.height = 80, 24
		newModel, _ := m.Update(commitsLoadedMsg{commits: fixtureCommits(3), totalCount: 3})
		m = newModel.(Model)
		newModel, _ = m.Update(worktreeEntriesLoadedMsg{entries: entries})
		m = newModel.(Model)
		if len(m.list.leading) != 1 {
			t.Fatalf("list.leading = %v, want 1 entry", m.list.leading)
		}
	})
}
