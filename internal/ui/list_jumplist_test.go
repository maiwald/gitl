package ui

import "testing"

// TestListModelJumpBackReturnsToTopSeed pins down that the jumplist is
// seeded with position 0 at construction (see NewListModel), so ctrl-o
// always gets back to the top of history even when the cursor got
// somewhere else purely via plain j/k, never through an actual jump.
func TestListModelJumpBackReturnsToTopSeed(t *testing.T) {
	m := NewListModel(fixtureCommits(5))
	m.SetSize(80, 3)

	m.Update(keyRune('j'))
	m.Update(keyRune('j'))
	if c, _ := m.Selected(); c.Hash != "c" {
		t.Fatalf("selection after 2 j: hash = %q, want c", c.Hash)
	}

	if !m.JumpBack() {
		t.Fatal("JumpBack() = false, want true (seeded entry at 0)")
	}
	if c, _ := m.Selected(); c.Hash != "a" {
		t.Fatalf("selection after JumpBack: hash = %q, want a", c.Hash)
	}
}

// TestListModelTopPushesJumpUndoneByJumpBack pins down that 'g' (Top)
// records a jumplist entry, and that JumpBack/JumpForward exactly undo one
// another (browser-style), landing back on the position each left.
func TestListModelTopPushesJumpUndoneByJumpBack(t *testing.T) {
	m := NewListModel(fixtureCommits(5))
	m.SetSize(80, 3)

	m.Update(keyRune('j'))
	m.Update(keyRune('j'))
	m.Update(keyRune('j')) // cursor at commit index 3 ("d")

	m.Update(keyRune('g')) // Top: jumps to 0, pushing 3 onto jumpBack
	if c, _ := m.Selected(); c.Hash != "a" {
		t.Fatalf("selection after 'g': hash = %q, want a", c.Hash)
	}

	if !m.JumpBack() {
		t.Fatal("JumpBack() = false, want true")
	}
	if c, _ := m.Selected(); c.Hash != "d" {
		t.Fatalf("selection after JumpBack: hash = %q, want d (undoing the 'g' jump)", c.Hash)
	}

	if !m.JumpForward() {
		t.Fatal("JumpForward() = false, want true")
	}
	if c, _ := m.Selected(); c.Hash != "a" {
		t.Fatalf("selection after JumpForward: hash = %q, want a (redoing the 'g' jump)", c.Hash)
	}
}

// TestListModelNewJumpClearsForwardStack pins down vim's own jumplist
// semantics: making a fresh jump after a JumpBack discards whatever
// JumpForward history existed before it.
func TestListModelNewJumpClearsForwardStack(t *testing.T) {
	m := NewListModel(fixtureCommits(5))
	m.SetTotalCount(5)
	m.SetSize(80, 3)

	m.Update(keyRune('G')) // jump to the bottom, pushes 0
	m.JumpBack()           // back to 0, pushes 4 onto jumpForward

	if len(m.jumpForward) == 0 {
		t.Fatal("jumpForward is empty before the fresh jump, test setup is wrong")
	}

	m.Update(keyRune('j')) // plain motion: not a jump, must not itself clear anything
	needsLoad, target := m.Search([]int{3})
	if needsLoad || target != 3 {
		t.Fatalf("Search: needsLoad=%v target=%d, want false,3", needsLoad, target)
	}

	if len(m.jumpForward) != 0 {
		t.Fatalf("jumpForward = %v after a fresh jump, want cleared", m.jumpForward)
	}
}

// TestListModelEveryMatchStepPushesJump pins down the deliberate deviation
// from vim: every NextMatch/PrevMatch step (not just the initial search)
// records a jumplist entry, since a single step here can move much farther
// than vim's local from-cursor re-search.
func TestListModelEveryMatchStepPushesJump(t *testing.T) {
	m := NewListModel(fixtureCommits(5))
	m.SetTotalCount(5)
	m.SetSize(80, 3)

	m.Search([]int{1, 3}) // cursor -> 1, pushes jump from 0
	m.NextMatch()         // cursor -> 3, pushes jump from 1
	if !m.JumpBack() {
		t.Fatal("JumpBack() = false, want true")
	}
	if c, _ := m.Selected(); c.Hash != "b" {
		t.Fatalf("selection after JumpBack: hash = %q, want b (commit index 1, undoing NextMatch)", c.Hash)
	}
	if !m.JumpBack() {
		t.Fatal("second JumpBack() = false, want true")
	}
	if c, _ := m.Selected(); c.Hash != "a" {
		t.Fatalf("selection after second JumpBack: hash = %q, want a (undoing the initial search jump)", c.Hash)
	}
}

// TestListModelNoOpJumpDoesNotPollutJumplist pins down that jumping to
// where the cursor already is (e.g. pressing 'g' while already at the top)
// doesn't record a redundant jumplist entry.
func TestListModelNoOpJumpDoesNotPollutJumplist(t *testing.T) {
	m := NewListModel(fixtureCommits(5))
	m.SetSize(80, 3)

	before := len(m.jumpBack)
	m.Update(keyRune('g')) // already at the top
	if len(m.jumpBack) != before {
		t.Fatalf("jumpBack length = %d after a no-op 'g', want unchanged at %d", len(m.jumpBack), before)
	}
}

// TestListModelJumpBackEmptyIsNoop pins down JumpBack/JumpForward's report
// when their stack is empty, so callers (app.go) know not to treat it as a
// move.
func TestListModelJumpBackEmptyIsNoop(t *testing.T) {
	m := NewListModel(fixtureCommits(5))
	m.SetSize(80, 3)

	if m.JumpForward() {
		t.Fatal("JumpForward() = true with nothing to redo, want false")
	}

	m.JumpBack() // consume the seeded entry at 0
	if m.JumpBack() {
		t.Fatal("JumpBack() = true with an empty jumpBack stack, want false")
	}
}
