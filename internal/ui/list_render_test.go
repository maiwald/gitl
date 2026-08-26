package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestListModelIncrementalRenderMatchesFullRebuild is the correctness
// spine for the row-render cache: with leading rows and an active search
// in place (so dimming/highlighting are in play, not just plain rows),
// cursor movement's incremental patch (setCursor/renderCursorMoved) must
// produce exactly the same viewport content a full render() would. If this
// ever fails, the incremental path has drifted from renderRow — the single
// source of truth both paths are supposed to share.
func TestListModelIncrementalRenderMatchesFullRebuild(t *testing.T) {
	m := NewListModel(fixtureCommits(30), &jumplist{})
	m.SetLeadingSummaries(fixtureLeadingSummaries())
	m.SetSize(80, 10)
	m.SetTotalCount(30)
	m.SetSearchDisplay("subject", false)
	runListSearch(&m, []int{5, 10, 20})

	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyDown}, {Type: tea.KeyDown}, {Type: tea.KeyUp},
		{Type: tea.KeyDown}, {Type: tea.KeyDown}, {Type: tea.KeyDown},
		{Type: tea.KeyUp}, {Type: tea.KeyUp}, {Type: tea.KeyUp}, {Type: tea.KeyUp},
	} {
		m.Update(key)
	}

	incremental := m.vp.View()
	m.render()
	rebuilt := m.vp.View()

	if incremental != rebuilt {
		t.Fatalf("incremental render diverged from full rebuild:\nincremental=%q\nrebuilt=%q", incremental, rebuilt)
	}
}

// TestListModelIncrementalRenderMatchesFullRebuildAfterAppendCommits pins
// down the same equivalence for AppendCommits' incremental path (append
// new rows, patch the old+new cursor rows) once it resolves a pending
// jump landing inside the newly-appended batch.
func TestListModelIncrementalRenderMatchesFullRebuildAfterAppendCommits(t *testing.T) {
	m := NewListModel(fixtureCommits(5), &jumplist{})
	m.SetSize(80, 3)
	m.SetTotalCount(10)

	if needsLoad := m.JumpTo(7); !needsLoad {
		t.Fatal("expected JumpTo(7) to need a load (target beyond the initial 5 commits)")
	}
	m.AppendCommits(fixtureCommits(5))

	if c, ok := m.Selected(); !ok || c.Hash != "c" {
		t.Fatalf("selection = %+v ok=%v, want hash c (commit index 7, i.e. the new batch's local index 2)", c, ok)
	}

	incremental := m.vp.View()
	m.render()
	rebuilt := m.vp.View()

	if incremental != rebuilt {
		t.Fatalf("incremental render after AppendCommits diverged from full rebuild:\nincremental=%q\nrebuilt=%q", incremental, rebuilt)
	}
}

// TestListCursorMoveCostIsIndependentOfHistorySize is the list's half of the
// property the row viewport exists for (see the detail view's equivalent in
// detail_cursor_test.go): a G jump or a search jump can load the entire
// remaining history, and every j/k after it must still cost two row renders —
// not a re-read of everything loaded. Bytes rather than allocation count,
// since the regression this pins moved megabytes in a constant 174 of them.
func TestListCursorMoveCostIsIndependentOfHistorySize(t *testing.T) {
	perMove := func(commits int) uint64 {
		m := NewListModel(fixtureCommits(commits), &jumplist{})
		m.SetSize(80, 40)
		m.SetTotalCount(commits)
		down := tea.KeyMsg{Type: tea.KeyDown}
		return bytesPerRun(100, func() { m.Update(down) })
	}
	small, big := perMove(100), perMove(20000)
	if big > 2*small {
		t.Fatalf("a cursor move allocates %d bytes over 20000 commits against %d over 100; something on the keypress path scales with the loaded history", big, small)
	}
}

// BenchmarkListCursorMove measures the cost moveCursor's incremental patch
// pays per keystroke, over a repo-history-scale commit count — the
// scenario the row-render cache exists for (a G jump or a search jump can
// load the entire remaining history, and every subsequent j/k used to
// re-render all of it).
func BenchmarkListCursorMove(b *testing.B) {
	m := NewListModel(fixtureCommits(20000), &jumplist{})
	m.SetSize(80, 40)
	m.SetTotalCount(20000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
}
