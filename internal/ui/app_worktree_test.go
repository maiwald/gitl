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

// TestNewModelSetsQuitOnBackFromStagedTarget pins down that quitOnBack is
// wired purely from StartTarget.Staged/Rev at construction time — nothing
// else ever writes it, so the interactively-opened staged row or commit
// (Enter from the list) is unaffected by construction rather than by a
// runtime check.
func TestNewModelSetsQuitOnBackFromStagedTarget(t *testing.T) {
	if !NewModel(StartTarget{Staged: true}).quitOnBack {
		t.Error("quitOnBack = false for StartTarget{Staged: true}, want true")
	}
	if !NewModel(StartTarget{Rev: "HEAD~2"}).quitOnBack {
		t.Error("quitOnBack = false for StartTarget{Rev: \"HEAD~2\"}, want true")
	}
	if NewModel(StartTarget{}).quitOnBack {
		t.Error("quitOnBack = true for StartTarget{}, want false")
	}
}

// TestQuitOnBackGate pins down handleDetailViewKey's Back branch: with
// quitOnBack set (a `gitl --staged` startup jump), Back quits the program
// outright; otherwise it returns to the list, exactly as it does for a real
// commit's detail view (see TestEnterRequestsDetailAndEscReturnsToSameListState
// in app_detail_test.go).
func TestQuitOnBackGate(t *testing.T) {
	t.Run("quits when quitOnBack is set", func(t *testing.T) {
		m := Model{active: viewDetail, quitOnBack: true}
		newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
		m = newModel.(Model)
		if !m.quitting {
			t.Error("quitting = false, want true")
		}
		if cmd == nil {
			t.Fatal("expected a command")
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("cmd() = %T, want tea.QuitMsg", cmd())
		}
	})

	t.Run("returns to list when quitOnBack is unset", func(t *testing.T) {
		m := Model{active: viewDetail, quitOnBack: false}
		newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
		m = newModel.(Model)
		if m.active != viewList {
			t.Errorf("active = %v, want viewList", m.active)
		}
		if m.quitting {
			t.Error("quitting = true, want false")
		}
		if cmd != nil {
			t.Errorf("cmd = %v, want nil", cmd())
		}
	})
}

// TestRepoOpenedStagedPathSkipsNoCommitsError pins down that a commit-less
// repo is not an error on the --staged startup path (there's no HEAD to
// diff against yet, but FetchWorktreeEntry already treats that as the
// empty tree), unlike the plain startup path (see
// TestRepoOpenedPlainPathErrorsOnNoCommits).
func TestRepoOpenedStagedPathSkipsNoCommitsError(t *testing.T) {
	m := Model{start: StartTarget{Staged: true}}
	newModel, cmd := m.Update(repoOpenedMsg{repo: &git.Repo{Path: "."}, hasCommits: false})
	m = newModel.(Model)
	if m.err != nil {
		t.Errorf("err = %v, want nil (a commit-less repo is fine for --staged)", m.err)
	}
	if cmd == nil {
		t.Fatal("expected a command to fetch the staged entry")
	}
}

func TestRepoOpenedPlainPathErrorsOnNoCommits(t *testing.T) {
	m := Model{}
	newModel, _ := m.Update(repoOpenedMsg{repo: &git.Repo{Path: "."}, hasCommits: false})
	m = newModel.(Model)
	if m.err == nil {
		t.Fatal("expected an error for a commit-less repo on the plain startup path")
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
