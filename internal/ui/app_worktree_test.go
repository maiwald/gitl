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
		detail: &git.CommitDetail{
			Kind:  git.EntryWorktree,
			Files: []git.File{{Path: "a.txt", Additions: 2}},
		},
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

// TestQuitOnBackGate pins down handleDetailViewKey's Back branch: on a run
// with no list (a piped patch), Back quits the program
// outright; otherwise it returns to the list, exactly as it does for a real
// commit's detail view (see TestEnterRequestsDetailAndEscReturnsToSameListState
// in app_detail_test.go).
func TestQuitOnBackGate(t *testing.T) {
	t.Run("quits when the run has no list", func(t *testing.T) {
		m := Model{active: viewDetail, start: StartTarget{Piped: &git.PipedPatch{}}, jl: &jumplist{}}
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

	t.Run("returns to list when the run has one", func(t *testing.T) {
		m := Model{active: viewDetail, jl: &jumplist{}}
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

// TestRepoOpenedPipedPathSkipsNoCommitsError pins down that a commit-less
// repo is not an error on the piped startup path — git already produced the
// diff, so there is something to show whatever HEAD does — unlike the plain
// startup path (see TestRepoOpenedPlainPathErrorsOnNoCommits).
func TestRepoOpenedPipedPathSkipsNoCommitsError(t *testing.T) {
	m := Model{start: StartTarget{Piped: &git.PipedPatch{Hash: "abc"}}, jl: &jumplist{}}
	newModel, cmd := m.Update(repoOpenedMsg{repo: &git.Repo{Path: "."}, hasCommits: false})
	m = newModel.(Model)
	if m.err != nil {
		t.Errorf("err = %v, want nil (a commit-less repo is fine for a piped patch)", m.err)
	}
	if cmd == nil {
		t.Fatal("expected a command to fetch the piped commit's header")
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

// TestEntryDetailFetchFromListFailsNonFatally pins down that Enter on a
// synthetic row reports a failed fetch nonFatally: the list it was opened
// from stays up to receive the error, rather than the whole screen being
// replaced the way a startup failure replaces it.
func TestEntryDetailFetchFromListFailsNonFatally(t *testing.T) {
	cmd := fetchEntryDetailCmd(&git.Repo{Path: t.TempDir()}, git.EntryStaged)
	msg, ok := cmd().(errMsg)
	if !ok {
		t.Fatalf("cmd() = %T, want errMsg outside a git repository", cmd())
	}
	if msg.severity != nonFatal {
		t.Errorf("severity = %v, want nonFatal", msg.severity)
	}
}
