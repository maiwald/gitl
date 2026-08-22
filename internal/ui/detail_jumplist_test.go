package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// DetailModel no longer owns JumpBack/JumpForward traversal or a seeded
// jumplist of its own — both moved to the root Model's shared jumplist (see
// jumplist's doc comment in app.go), since popping an entry can require
// switching the active view. These tests pin down DetailModel's remaining
// half of the contract: which motions push a jumplist entry (via pushJump,
// into the shared jl) and which don't. Traversal (JumpBack/JumpForward) and
// cross-view behavior are covered at the Model level in app_jumplist_test.go.

// TestDetailModelSectionJumpPushesJump pins down that [ / ] record a
// jumplist entry for the position they moved away from.
func TestDetailModelSectionJumpPushesJump(t *testing.T) {
	jl := &jumplist{}
	d := fixtureDetailWithFiles(2, 20)
	m := NewDetailModel(d, jl)
	m.SetSize(80, 8)

	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	from := m.cursorLine

	before := len(jl.back)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")}) // jump to the stats section
	if m.cursorLine == from {
		t.Fatal("cursorLine unchanged after ']', want it to move")
	}
	if len(jl.back) != before+1 {
		t.Fatalf("jl.back length = %d after ']', want %d", len(jl.back), before+1)
	}
	last := jl.back[len(jl.back)-1]
	if last.view != viewDetail || last.commit != d || last.cursorLine != from {
		t.Fatalf("jl.back top = %+v, want {view: viewDetail, commit: d, cursorLine: %d}", last, from)
	}
}

// TestDetailModelSearchPushesJumpOnEveryStep pins down that a submitted search's
// initial jump and every subsequent NextMatch step each record a jumplist
// entry — see jumpToMatch's doc comment for why this deliberately departs
// from vim (only the initial search is jumplisted there).
func TestDetailModelSearchPushesJumpOnEveryStep(t *testing.T) {
	jl := &jumplist{}
	m := NewDetailModel(fixtureDetailForSearch(), jl)
	m.SetSize(80, 4)

	runDetailSearch(&m, "needle") // jump 1: from cursorLine 0 to the first match
	afterFirst := m.cursorLine
	if len(jl.back) != 1 || jl.back[0].cursorLine != 0 {
		t.Fatalf("jl.back after the search = %+v, want one entry at cursorLine 0", jl.back)
	}

	m.NextMatch() // jump 2: from the first match to the second
	afterSecond := m.cursorLine
	if afterSecond == afterFirst {
		t.Fatal("cursorLine unchanged after NextMatch, want it to move to the second match")
	}
	if len(jl.back) != 2 || jl.back[1].cursorLine != afterFirst {
		t.Fatalf("jl.back after NextMatch = %+v, want a second entry at cursorLine %d", jl.back, afterFirst)
	}
}

// TestDetailModelNoOpJumpDoesNotPushJump pins down that [ / ] at the first/
// last section (a no-op landing) doesn't record a redundant jumplist entry.
func TestDetailModelNoOpJumpDoesNotPushJump(t *testing.T) {
	jl := &jumplist{}
	d := fixtureDetailWithFiles(1, 5)
	m := NewDetailModel(d, jl)
	m.SetSize(80, 24)

	before := len(jl.back)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[")}) // already at the first (only) section
	if len(jl.back) != before {
		t.Fatalf("jl.back length = %d after a no-op '[', want unchanged at %d", len(jl.back), before)
	}
}

// TestDetailModelPlainMotionDoesNotPushJump pins down that ordinary j/k
// (unlike g/G/[/]/search jumps) never record a jumplist entry — matching
// vim's own distinction between motions and jumps.
func TestDetailModelPlainMotionDoesNotPushJump(t *testing.T) {
	jl := &jumplist{}
	d := fixtureDetailWithFiles(1, 5)
	m := NewDetailModel(d, jl)
	m.SetSize(80, 24)

	before := len(jl.back)
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if len(jl.back) != before {
		t.Fatalf("jl.back length = %d after plain j/k, want unchanged at %d", len(jl.back), before)
	}
}
