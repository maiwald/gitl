package ui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/maiwald/gitl/internal/git"
)

// These tests cover the root Model's shared, cross-view jumplist (see
// jumplist's doc comment in app.go): ctrl-o/ctrl-i now traverse both list
// cursor positions and detail cursor-line positions on one stack, including
// the view transition of opening a commit/entry (Enter) — unlike the two
// independent per-view jumplists this replaced, which couldn't return from
// a fresh detail view to the list position it was opened from at all.

func fixtureJumplistDetail(hash string) *git.CommitDetail {
	return &git.CommitDetail{
		Commit: git.Commit{
			Hash:       hash,
			AuthorName: "Someone",
			AuthorDate: time.Now(),
			Subject:    "subject " + hash,
		},
		Body: "subject " + hash + "\n\nmessage body",
	}
}

func jumpBack(t *testing.T, m Model) Model {
	t.Helper()
	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	return newModel.(Model)
}

func jumpForward(t *testing.T, m Model) Model {
	t.Helper()
	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	return newModel.(Model)
}

// TestJumpBackFromFreshDetailReturnsToListPosition pins down the reported
// bug's simplest form: opening a commit (Enter) is itself a jump-worthy
// transition, so ctrl-o from the freshly opened detail view returns to the
// exact list position it was opened from — not, as the old per-view
// jumplists did, a no-op local jump within the brand new detail view.
func TestJumpBackFromFreshDetailReturnsToListPosition(t *testing.T) {
	jl := &jumplist{}
	list := NewListModel(fixtureCommits(5), jl)
	list.SetSize(80, 3)
	list.Update(tea.KeyMsg{Type: tea.KeyDown})
	list.Update(tea.KeyMsg{Type: tea.KeyDown}) // cursor at commit index 2 ("c")

	m := Model{active: viewList, list: list, jl: jl}
	m.width, m.height = 80, 24

	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newModel.(Model)
	if cmd == nil {
		t.Fatal("Enter on a selected commit should return a fetch command")
	}

	newModel, _ = m.Update(commitDetailLoadedMsg{detail: fixtureJumplistDetail("c")})
	m = newModel.(Model)
	if m.active != viewDetail {
		t.Fatalf("active view = %v, want viewDetail after detail loads", m.active)
	}

	m = jumpBack(t, m)
	if m.active != viewList {
		t.Fatalf("active view = %v, want viewList after ctrl-o", m.active)
	}
	if c, _ := m.list.Selected(); c.Hash != "c" {
		t.Fatalf("selection after ctrl-o = %q, want c (the position Enter was pressed from)", c.Hash)
	}
}

// TestJumpForwardRedoesIntoSameDetailInstance pins down that ctrl-i, after a
// ctrl-o back to the list, reopens the exact same *git.CommitDetail (no
// re-fetch) at the exact cursorLine it was left at.
func TestJumpForwardRedoesIntoSameDetailInstance(t *testing.T) {
	jl := &jumplist{}
	list := NewListModel(fixtureCommits(5), jl)
	list.SetSize(80, 3)

	m := Model{active: viewList, list: list, jl: jl}
	m.width, m.height = 80, 24

	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newModel.(Model)
	detail := fixtureJumplistDetail("a")
	newModel, _ = m.Update(commitDetailLoadedMsg{detail: detail})
	m = newModel.(Model)

	m.detail.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.detail.Update(tea.KeyMsg{Type: tea.KeyDown})
	lineBeforeBack := m.detail.cursorLine
	if lineBeforeBack == 0 {
		t.Fatal("cursorLine unchanged after 2 Down, test setup is wrong")
	}

	m = jumpBack(t, m)
	if m.active != viewList {
		t.Fatalf("active view = %v, want viewList after ctrl-o", m.active)
	}

	m = jumpForward(t, m)
	if m.active != viewDetail {
		t.Fatalf("active view = %v, want viewDetail after ctrl-i", m.active)
	}
	if m.detail.detail != detail {
		t.Fatal("ctrl-i reconstructed a different *git.CommitDetail instead of reusing the original")
	}
	if m.detail.cursorLine != lineBeforeBack {
		t.Fatalf("cursorLine after ctrl-i = %d, want %d (restored)", m.detail.cursorLine, lineBeforeBack)
	}
}

// TestSearchMatchThenOpenThenJumpBackReturnsToMatch is the exact scenario
// reported as inconsistent: searching in the list jumps the cursor to a
// match, opening that commit and then pressing ctrl-o in the diff view used
// to just move the cursor within that one commit (or no-op) — it now steps
// back out to the list, landing on the matched commit; a second ctrl-o then
// undoes the search jump itself, landing back where the cursor was before
// searching.
func TestSearchMatchThenOpenThenJumpBackReturnsToMatch(t *testing.T) {
	m := newSearchableListModel()
	m = submitSearchPrompt(t, m, "x")

	m = resolveSearch(m, "x", map[string]bool{"c": true})

	c, _ := m.list.Selected()
	if c.Hash != "c" {
		t.Fatalf("selection after search = %q, want c (the match)", c.Hash)
	}

	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newModel.(Model)
	if cmd == nil {
		t.Fatal("Enter on the matched commit should return a fetch command")
	}
	newModel, _ = m.Update(commitDetailLoadedMsg{detail: fixtureJumplistDetail("c")})
	m = newModel.(Model)
	if m.active != viewDetail {
		t.Fatalf("active view = %v, want viewDetail after detail loads", m.active)
	}

	m = jumpBack(t, m)
	if m.active != viewList {
		t.Fatalf("active view after first ctrl-o = %v, want viewList", m.active)
	}
	if c, _ := m.list.Selected(); c.Hash != "c" {
		t.Fatalf("selection after first ctrl-o = %q, want c (the matched commit Enter opened)", c.Hash)
	}

	m = jumpBack(t, m)
	if c, _ := m.list.Selected(); c.Hash != "a" {
		t.Fatalf("selection after second ctrl-o = %q, want a (undoing the search jump)", c.Hash)
	}
}

// TestJumpBackEmptyStackIsNoop pins down that ctrl-o with nothing on the
// jumplist leaves the model unchanged rather than panicking.
func TestJumpBackEmptyStackIsNoop(t *testing.T) {
	jl := &jumplist{}
	list := NewListModel(fixtureCommits(5), jl)
	list.SetSize(80, 3)
	list.Update(tea.KeyMsg{Type: tea.KeyDown})

	m := Model{active: viewList, list: list, jl: jl}
	m.width, m.height = 80, 24

	m = jumpBack(t, m)
	if m.active != viewList {
		t.Fatalf("active view = %v, want unchanged viewList", m.active)
	}
	if c, _ := m.list.Selected(); c.Hash != "b" {
		t.Fatalf("selection = %q, want unchanged b", c.Hash)
	}
}

// TestFreshJumpAfterJumpBackClearsForwardStack pins down vim's own jumplist
// semantics at the Model level: a fresh jump made after a ctrl-o discards
// whatever ctrl-i history existed before it.
func TestFreshJumpAfterJumpBackClearsForwardStack(t *testing.T) {
	jl := &jumplist{}
	list := NewListModel(fixtureCommits(5), jl)
	list.SetSize(80, 3)
	list.Update(tea.KeyMsg{Type: tea.KeyDown})
	list.Update(tea.KeyMsg{Type: tea.KeyDown})
	list.Update(tea.KeyMsg{Type: tea.KeyDown}) // cursor at commit index 3

	m := Model{active: viewList, list: list, jl: jl}
	m.width, m.height = 80, 24

	newModel, _ := m.Update(keyRune('g')) // Top: jumps to 0, pushes 3 onto jl.back
	m = newModel.(Model)

	m = jumpBack(t, m) // back to 3, pushes 0 onto jl.forward
	if len(jl.forward) == 0 {
		t.Fatal("jl.forward is empty before the fresh jump, test setup is wrong")
	}

	newModel, _ = m.Update(keyRune('G')) // a fresh jump
	m = newModel.(Model)
	if len(jl.forward) != 0 {
		t.Fatalf("jl.forward = %v after a fresh jump, want cleared", jl.forward)
	}
}

// TestJumpForwardIntoReusedDetailPicksUpSearchChangedMeanwhile pins that the
// detail view left behind while the list is in front is not kept in step with
// the canonical search state, and that coming back to it through the jumplist
// (which reuses it for the same commit rather than building a new one) brings
// it up to date first.
func TestJumpForwardIntoReusedDetailPicksUpSearchChangedMeanwhile(t *testing.T) {
	jl := &jumplist{}
	list := NewListModel(fixtureCommits(5), jl)
	list.SetSize(80, 3)
	m := Model{active: viewList, list: list, jl: jl}
	m.width, m.height = 80, 24

	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	newModel, _ = newModel.(Model).Update(commitDetailLoadedMsg{detail: fixtureJumplistDetail("c")})
	m = jumpBack(t, newModel.(Model))
	if m.active != viewList {
		t.Fatalf("active view = %v, want viewList after ctrl-o", m.active)
	}

	m.setSearchDisplay("message", false)
	if m.detail.displayTerm != "" {
		t.Fatalf("hidden detail view displayTerm = %q, want it left alone while the list is in front", m.detail.displayTerm)
	}

	m = jumpForward(t, m)
	if m.active != viewDetail {
		t.Fatalf("active view = %v, want viewDetail after ctrl-i", m.active)
	}
	if m.detail.displayTerm != "message" || m.detail.totalMatches() == 0 {
		t.Errorf("detail displayTerm = %q with %d matches, want %q with matches", m.detail.displayTerm, m.detail.totalMatches(), "message")
	}
}
