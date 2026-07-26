package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func keyRune(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func TestListModelHJKLMovement(t *testing.T) {
	m := NewListModel(fixtureCommits(5))
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
	m := NewListModel(fixtureCommits(10))
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

func TestListModelHalfPageMovement(t *testing.T) {
	m := NewListModel(fixtureCommits(20))
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
	m := NewListModel(fixtureCommits(10))
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
	m := NewListModel(fixtureCommits(30))
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
