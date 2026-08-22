package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestDetailModelJumpBackReturnsToSeededTop mirrors
// TestListModelJumpBackReturnsToTopSeed: the jumplist is seeded with an
// entry at 0 in NewDetailModel, so ctrl-o always reaches the top of the
// commit even after the cursor got elsewhere via plain j/k, never through
// an actual jump.
func TestDetailModelJumpBackReturnsToSeededTop(t *testing.T) {
	d := fixtureDetailWithFiles(1, 5)
	m := NewDetailModel(d)
	m.SetSize(80, 24)

	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.cursorLine != 2 {
		t.Fatalf("cursorLine after 2 Down = %d, want 2", m.cursorLine)
	}

	if !m.JumpBack() {
		t.Fatal("JumpBack() = false, want true (seeded entry at 0)")
	}
	if m.cursorLine != 0 {
		t.Fatalf("cursorLine after JumpBack = %d, want 0", m.cursorLine)
	}
}

// TestDetailModelSectionJumpPushesJumpUndoneByJumpBack pins down that
// [ / ] record a jumplist entry, and that JumpBack/JumpForward exactly
// undo one another.
func TestDetailModelSectionJumpPushesJumpUndoneByJumpBack(t *testing.T) {
	d := fixtureDetailWithFiles(2, 20)
	m := NewDetailModel(d)
	m.SetSize(80, 8)

	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	from := m.cursorLine

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")}) // jump to the stats section
	if m.cursorLine == from {
		t.Fatal("cursorLine unchanged after ']', want it to move")
	}

	if !m.JumpBack() {
		t.Fatal("JumpBack() = false, want true")
	}
	if m.cursorLine != from {
		t.Fatalf("cursorLine after JumpBack = %d, want %d (undoing the ']' jump)", m.cursorLine, from)
	}

	if !m.JumpForward() {
		t.Fatal("JumpForward() = false, want true")
	}
	if m.cursorLine == from {
		t.Fatal("cursorLine after JumpForward unchanged, want it to redo the ']' jump")
	}
}

// TestDetailModelSearchPushesJumpOnEveryStep pins down that RunSearch's
// initial jump and every subsequent NextMatch step each record a jumplist
// entry — see jumpToMatch's doc comment for why this deliberately departs
// from vim (only the initial search is jumplisted there).
func TestDetailModelSearchPushesJumpOnEveryStep(t *testing.T) {
	m := NewDetailModel(fixtureDetailForSearch())
	m.SetSize(80, 4)

	m.RunSearch("needle") // jump 1: from cursorLine 0 to the first match
	afterFirst := m.cursorLine

	m.NextMatch() // jump 2: from the first match to the second
	afterSecond := m.cursorLine
	if afterSecond == afterFirst {
		t.Fatal("cursorLine unchanged after NextMatch, want it to move to the second match")
	}

	if !m.JumpBack() {
		t.Fatal("JumpBack() = false, want true")
	}
	if m.cursorLine != afterFirst {
		t.Fatalf("cursorLine after JumpBack = %d, want %d (undoing NextMatch)", m.cursorLine, afterFirst)
	}

	if !m.JumpBack() {
		t.Fatal("second JumpBack() = false, want true")
	}
	if m.cursorLine != 0 {
		t.Fatalf("cursorLine after second JumpBack = %d, want 0 (undoing the initial search jump)", m.cursorLine)
	}
}

// TestDetailModelNewJumpClearsForwardStack mirrors
// TestListModelNewJumpClearsForwardStack: a fresh jump after a JumpBack
// discards whatever JumpForward history existed before it.
func TestDetailModelNewJumpClearsForwardStack(t *testing.T) {
	d := fixtureDetailWithFiles(2, 20)
	m := NewDetailModel(d)
	m.SetSize(80, 8)

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")}) // pushes 0
	m.JumpBack()                                                 // back to 0, pushes onto jumpForward

	if len(m.jumpForward) == 0 {
		t.Fatal("jumpForward is empty before the fresh jump, test setup is wrong")
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")}) // a fresh jump
	if len(m.jumpForward) != 0 {
		t.Fatalf("jumpForward = %v after a fresh jump, want cleared", m.jumpForward)
	}
}

// TestDetailModelJumpBackEmptyIsNoop mirrors TestListModelJumpBackEmptyIsNoop.
func TestDetailModelJumpBackEmptyIsNoop(t *testing.T) {
	d := fixtureDetailWithFiles(1, 5)
	m := NewDetailModel(d)
	m.SetSize(80, 24)

	if m.JumpForward() {
		t.Fatal("JumpForward() = true with nothing to redo, want false")
	}

	m.JumpBack() // consume the seeded entry at 0
	if m.JumpBack() {
		t.Fatal("JumpBack() = true with an empty jumpBack stack, want false")
	}
}

// TestDetailModelPlainMotionDoesNotPushJump pins down that ordinary j/k
// (unlike g/G/[/]/search jumps) never record a jumplist entry — matching
// vim's own distinction between motions and jumps.
func TestDetailModelPlainMotionDoesNotPushJump(t *testing.T) {
	d := fixtureDetailWithFiles(1, 5)
	m := NewDetailModel(d)
	m.SetSize(80, 24)

	before := len(m.jumpBack)
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if len(m.jumpBack) != before {
		t.Fatalf("jumpBack length = %d after plain j/k, want unchanged at %d", len(m.jumpBack), before)
	}
}
