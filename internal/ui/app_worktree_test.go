package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/maiwald/gitl/internal/git"
)

// TestEnterOnSyntheticRowFetchesItsDiff pins down that opening a synthetic
// row goes through a fetch exactly as a real commit does — the list holds
// only that row's counts, never the diff behind them — and that Esc
// afterward returns to the list with the cursor preserved, the same as it
// does for a real commit (see
// TestEnterRequestsDetailAndEscReturnsToSameListState in app_detail_test.go).
func TestEnterOnSyntheticRowFetchesItsDiff(t *testing.T) {
	list := NewListModel(fixtureCommits(3), &jumplist{})
	list.SetSize(80, 10)
	list.SetLeadingSummaries([]git.EntrySummary{
		{Kind: git.EntryWorktree, Files: 1, Additions: 2},
	})
	list.Update(tea.KeyMsg{Type: tea.KeyUp}) // move cursor onto the synthetic row

	m := Model{active: viewList, list: list, jl: &jumplist{}}
	m.width, m.height = 80, 24
	cursorBefore := m.list.cursor

	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newModel.(Model)
	if cmd == nil {
		t.Fatal("Enter on a synthetic row should return a command fetching its diff")
	}
	if m.active != viewList {
		t.Fatalf("active view = %v, want viewList until the fetch lands", m.active)
	}

	newModel, _ = m.Update(commitDetailLoadedMsg{
		detail: git.EntryDetail(git.WorktreeEntry{
			Kind:  git.EntryWorktree,
			Files: []git.File{{Path: "a.txt", Additions: 2}},
		}),
	})
	m = newModel.(Model)
	if m.active != viewDetail {
		t.Fatalf("active view = %v, want viewDetail once the fetch lands", m.active)
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
// wired purely from StartTarget.Staged/Diff/Rev at construction time —
// nothing else ever writes it, so the interactively-opened staged/worktree
// row or commit (Enter from the list) is unaffected by construction rather
// than by a runtime check.
func TestNewModelSetsQuitOnBackFromStagedTarget(t *testing.T) {
	if !NewModel(StartTarget{Staged: true}).quitOnBack {
		t.Error("quitOnBack = false for StartTarget{Staged: true}, want true")
	}
	if !NewModel(StartTarget{Diff: true}).quitOnBack {
		t.Error("quitOnBack = false for StartTarget{Diff: true}, want true")
	}
	if !NewModel(StartTarget{Rev: "HEAD~2"}).quitOnBack {
		t.Error("quitOnBack = false for StartTarget{Rev: \"HEAD~2\"}, want true")
	}
	if NewModel(StartTarget{}).quitOnBack {
		t.Error("quitOnBack = true for StartTarget{}, want false")
	}
}

// TestStagedStartupHasNoListToJumpBackTo pins down that a `gitl --staged`
// run — which never loads a list at all (see StartTarget.opensList) — leaves
// the jumplist unseeded, so ctrl-o stays in the detail view instead of
// stranding the user on a permanently empty list view.
func TestStagedStartupHasNoListToJumpBackTo(t *testing.T) {
	m := NewModel(StartTarget{Staged: true})
	m.width, m.height = 80, 24
	newModel, _ := m.Update(commitDetailLoadedMsg{
		detail: git.EntryDetail(git.WorktreeEntry{Kind: git.EntryStaged, Files: []git.File{{Path: "a.txt"}}}),
	})
	m = newModel.(Model)

	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	m = newModel.(Model)
	if m.active != viewDetail {
		t.Fatalf("active view = %v after ctrl+o, want viewDetail (there is no list to go back to)", m.active)
	}
	if !strings.Contains(m.View(), "Staged changes") {
		t.Fatalf("view = %q, want the staged entry still on screen", m.View())
	}
}

// TestQuitOnBackGate pins down handleDetailViewKey's Back branch: with
// quitOnBack set (a `gitl --staged` startup jump), Back quits the program
// outright; otherwise it returns to the list, exactly as it does for a real
// commit's detail view (see TestEnterRequestsDetailAndEscReturnsToSameListState
// in app_detail_test.go).
func TestQuitOnBackGate(t *testing.T) {
	t.Run("quits when quitOnBack is set", func(t *testing.T) {
		m := Model{active: viewDetail, quitOnBack: true, jl: &jumplist{}}
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
		m := Model{active: viewDetail, quitOnBack: false, jl: &jumplist{}}
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
	m := Model{start: StartTarget{Staged: true}, jl: &jumplist{}}
	newModel, cmd := m.Update(repoOpenedMsg{repo: &git.Repo{Path: "."}, hasCommits: false})
	m = newModel.(Model)
	if m.err != nil {
		t.Errorf("err = %v, want nil (a commit-less repo is fine for --staged)", m.err)
	}
	if cmd == nil {
		t.Fatal("expected a command to fetch the staged entry")
	}
}

// TestRepoOpenedDiffPathSkipsNoCommitsError is TestRepoOpenedStagedPathSkipsNoCommitsError's
// counterpart for --diff: a commit-less repo is fine there too, since
// worktreeDiff's tracked-file half diffs against the empty tree the same
// way stagedDiff does, and untracked files don't need a HEAD at all.
func TestRepoOpenedDiffPathSkipsNoCommitsError(t *testing.T) {
	m := Model{start: StartTarget{Diff: true}, jl: &jumplist{}}
	newModel, cmd := m.Update(repoOpenedMsg{repo: &git.Repo{Path: "."}, hasCommits: false})
	m = newModel.(Model)
	if m.err != nil {
		t.Errorf("err = %v, want nil (a commit-less repo is fine for --diff)", m.err)
	}
	if cmd == nil {
		t.Fatal("expected a command to fetch the worktree entry")
	}
}

func TestRepoOpenedPlainPathErrorsOnNoCommits(t *testing.T) {
	m := Model{jl: &jumplist{}}
	newModel, _ := m.Update(repoOpenedMsg{repo: &git.Repo{Path: "."}, hasCommits: false})
	m = newModel.(Model)
	if m.err == nil {
		t.Fatal("expected an error for a commit-less repo on the plain startup path")
	}
}

// TestWorktreeSummariesMirroredRegardlessOfArrivalOrder pins down that
// worktreeSummariesLoadedMsg and commitsLoadedMsg are independent concurrent
// fetches that can land in either order, and both orders end up with the
// summaries applied to the list — see the doc comment on
// worktreeSummariesLoadedMsg.
func TestWorktreeSummariesMirroredRegardlessOfArrivalOrder(t *testing.T) {
	summaries := []git.EntrySummary{{Kind: git.EntryStaged, Files: 1}}

	t.Run("summaries before commits", func(t *testing.T) {
		m := Model{jl: &jumplist{}}
		m.width, m.height = 80, 24
		newModel, _ := m.Update(worktreeSummariesLoadedMsg{summaries: summaries})
		m = newModel.(Model)
		newModel, _ = m.Update(commitsLoadedMsg{commits: fixtureCommits(3), totalCount: 3})
		m = newModel.(Model)
		if len(m.list.leading) != 1 {
			t.Fatalf("list.leading = %v, want 1 row", m.list.leading)
		}
	})

	t.Run("commits before summaries", func(t *testing.T) {
		m := Model{jl: &jumplist{}}
		m.width, m.height = 80, 24
		newModel, _ := m.Update(commitsLoadedMsg{commits: fixtureCommits(3), totalCount: 3})
		m = newModel.(Model)
		newModel, _ = m.Update(worktreeSummariesLoadedMsg{summaries: summaries})
		m = newModel.(Model)
		if len(m.list.leading) != 1 {
			t.Fatalf("list.leading = %v, want 1 row", m.list.leading)
		}
	})
}

// TestEntryDetailFetchFromListFailsNonFatally pins down the severity Enter
// passes: a synthetic row's fetch can now fail where it previously couldn't,
// and the list it was opened from has to stay up to receive the error, rather
// than the whole screen being replaced the way a `--staged` startup failure
// replaces it.
func TestEntryDetailFetchFromListFailsNonFatally(t *testing.T) {
	cmd := fetchEntryDetailCmd(&git.Repo{Path: t.TempDir()}, git.EntryStaged, nonFatal)
	msg, ok := cmd().(errMsg)
	if !ok {
		t.Fatalf("cmd() = %T, want errMsg outside a git repository", cmd())
	}
	if msg.severity != nonFatal {
		t.Errorf("severity = %v, want nonFatal", msg.severity)
	}
}
