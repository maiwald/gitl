package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/maiwald/gitl/internal/git"
)

func keyRune(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func TestListModelHJKLMovement(t *testing.T) {
	m := NewListModel(fixtureCommits(5), &jumplist{})
	m.SetSize(80, 3)

	m.Update(keyRune('j'))
	if c, ok := m.Selected(); !ok || c.Hash != "b" {
		t.Fatalf("after j: selection = %+v, ok=%v, want hash b", c, ok)
	}
	m.Update(keyRune('k'))
	if c, ok := m.Selected(); !ok || c.Hash != "a" {
		t.Fatalf("after k: selection = %+v, ok=%v, want hash a", c, ok)
	}
}

func TestListModelTopAndBottomFullyLoaded(t *testing.T) {
	m := NewListModel(fixtureCommits(10), &jumplist{})
	m.SetTotalCount(10)
	m.SetSize(80, 4)

	if _, more := m.Update(keyRune('G')); more {
		t.Fatal("G should not request a fetch when fully loaded")
	}
	if c, ok := m.Selected(); !ok || c.Hash != string(rune('a'+9)) {
		t.Fatalf("after G: selection = %+v, ok=%v, want last commit", c, ok)
	}

	if _, more := m.Update(keyRune('g')); more {
		t.Fatal("g should not request a fetch")
	}
	if c, ok := m.Selected(); !ok || c.Hash != "a" {
		t.Fatalf("after g: selection = %+v, ok=%v, want hash a", c, ok)
	}
}

// TestListModelNavigationOnEmptyHistoryStaysInRowSpace pins down the cursor
// invariant topIndex() <= cursor <= lastIndex() on a list with nothing in it
// — the state every run starts in, before commitsLoadedMsg lands, and the
// permanent state of a piped run. Without it, G lands the
// cursor on len(commits)-1 == -1, which addresses a synthetic leading row
// that isn't there, and SelectedSummary then indexes leading[-1].
func TestListModelNavigationOnEmptyHistoryStaysInRowSpace(t *testing.T) {
	m := NewListModel(nil, &jumplist{})
	m.SetSize(80, 5)

	for _, k := range []rune{'G', 'j', 'k', 'g'} {
		m.Update(keyRune(k))
		if m.cursor != 0 {
			t.Fatalf("cursor = %d after %q on an empty list, want 0", m.cursor, k)
		}
		if _, ok := m.Selected(); ok {
			t.Fatalf("Selected() reports a commit after %q on an empty list", k)
		}
		if _, ok := m.SelectedSummary(); ok {
			t.Fatalf("SelectedSummary() reports a row after %q on an empty list", k)
		}
	}
}

// TestListModelJumpToBottomWithOnlyLeadingRows covers the same invariant with
// the synthetic rows loaded but the commits not yet (worktreeSummariesLoadedMsg
// can land before commitsLoadedMsg): G belongs on the last leading row, the
// last row there actually is.
func TestListModelJumpToBottomWithOnlyLeadingRows(t *testing.T) {
	m := NewListModel(nil, &jumplist{})
	m.SetSize(80, 5)
	m.SetLeadingSummaries([]git.EntrySummary{
		{Kind: git.EntryWorktree, Files: 1},
		{Kind: git.EntryStaged, Files: 1},
	})

	m.Update(keyRune('G'))
	if m.cursor != -1 {
		t.Fatalf("cursor = %d after G, want -1 (the last leading row)", m.cursor)
	}
	if e, ok := m.SelectedSummary(); !ok || e.Kind != git.EntryStaged {
		t.Fatalf("SelectedSummary() = %+v, ok=%v, want the staged entry", e, ok)
	}
}

func TestListModelHalfPageMovement(t *testing.T) {
	m := NewListModel(fixtureCommits(20), &jumplist{})
	m.SetTotalCount(20)
	m.SetSize(80, 10) // half page = 5

	m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if m.cursor != 5 {
		t.Fatalf("after ctrl+d: cursor = %d, want 5", m.cursor)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if m.cursor != 10 {
		t.Fatalf("after 2x ctrl+d: cursor = %d, want 10", m.cursor)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	if m.cursor != 5 {
		t.Fatalf("after ctrl+u: cursor = %d, want 5", m.cursor)
	}
}

func TestListModelJumpToBottomTriggersFetchWhenIncomplete(t *testing.T) {
	m := NewListModel(fixtureCommits(10), &jumplist{})
	m.SetTotalCount(100) // more commits exist than are loaded
	m.SetSize(80, 5)

	req, more := m.Update(keyRune('G'))
	if !more {
		t.Fatal("G should request a fetch when history isn't fully loaded")
	}
	if req.skip != 10 || req.limit != 90 {
		t.Fatalf("fetch request = %+v, want skip=10 limit=90", req)
	}

	// A second G while the fetch is in flight shouldn't request again.
	if _, more := m.Update(keyRune('G')); more {
		t.Fatal("G should not re-request while a fetch is already in flight")
	}

	// Once the batch arrives, the pending jump completes.
	rest := fixtureCommits(100)[10:]
	m.AppendCommits(rest)
	if c, ok := m.Selected(); !ok || c.Hash != rest[len(rest)-1].Hash {
		t.Fatalf("after AppendCommits: selection = %+v, ok=%v, want last commit", c, ok)
	}
}

func TestListModelLazyLoadTriggersNearEnd(t *testing.T) {
	m := NewListModel(fixtureCommits(30), &jumplist{})
	m.SetTotalCount(1000)
	m.SetSize(80, 5)

	// Cursor far from the loaded end: no fetch yet.
	m.cursor = 0
	if _, more := m.maybeLoadMore(); more {
		t.Fatal("should not request more commits when far from loaded end")
	}

	// Cursor within lookahead of the loaded end: fetch triggered.
	m.cursor = 29 - (loadMoreLookahead - 1)
	req, more := m.maybeLoadMore()
	if !more {
		t.Fatal("should request more commits when near loaded end")
	}
	if req.skip != 30 || req.limit != batchSize {
		t.Fatalf("fetch request = %+v, want skip=30 limit=%d", req, batchSize)
	}

	// While loading, a second check should not re-trigger.
	if _, more := m.maybeLoadMore(); more {
		t.Fatal("should not re-request while a fetch is already in flight")
	}
}
