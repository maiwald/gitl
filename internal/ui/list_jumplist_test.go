package ui

import "testing"

// ListModel no longer owns JumpBack/JumpForward traversal or a seeded
// jumplist of its own — both moved to the root Model's shared jumplist (see
// jumplist's doc comment in app.go), since popping an entry can require
// switching the active view. These tests pin down ListModel's remaining
// half of the contract: which motions push a jumplist entry (via pushJump,
// into the shared jl) and which don't. Traversal (JumpBack/JumpForward) and
// cross-view behavior are covered at the Model level in app_jumplist_test.go.

// TestListModelTopPushesJump pins down that 'g' (Top) records a jumplist
// entry for the position it moved away from.
func TestListModelTopPushesJump(t *testing.T) {
	jl := &jumplist{}
	m := NewListModel(fixtureCommits(5), jl)
	m.SetSize(80, 3)

	m.Update(keyRune('j'))
	m.Update(keyRune('j'))
	m.Update(keyRune('j')) // cursor at commit index 3 ("d")

	before := len(jl.back)
	m.Update(keyRune('g')) // Top: jumps to 0
	if c, _ := m.Selected(); c.Hash != "a" {
		t.Fatalf("selection after 'g': hash = %q, want a", c.Hash)
	}
	if len(jl.back) != before+1 {
		t.Fatalf("jl.back length = %d after 'g', want %d", len(jl.back), before+1)
	}
	last := jl.back[len(jl.back)-1]
	if last.view != viewList || last.listCursor != 3 {
		t.Fatalf("jl.back top = %+v, want {view: viewList, listCursor: 3}", last)
	}
}

// TestListModelEveryMatchStepPushesJump pins down the deliberate deviation
// from vim: every NextMatch/PrevMatch step (not just the initial search)
// records a jumplist entry, since a single step here can move much farther
// than vim's local from-cursor re-search.
func TestListModelEveryMatchStepPushesJump(t *testing.T) {
	jl := &jumplist{}
	m := NewListModel(fixtureCommits(5), jl)
	m.SetTotalCount(5)
	m.SetSize(80, 3)

	m.Search([]int{1, 3}) // cursor -> 1, pushes jump from 0
	m.NextMatch()         // cursor -> 3, pushes jump from 1

	if len(jl.back) != 2 {
		t.Fatalf("jl.back length = %d after Search+NextMatch, want 2", len(jl.back))
	}
	if jl.back[0].listCursor != 0 || jl.back[1].listCursor != 1 {
		t.Fatalf("jl.back = %+v, want cursors [0, 1]", jl.back)
	}
}

// TestListModelNoOpJumpDoesNotPushJump pins down that jumping to where the
// cursor already is (e.g. pressing 'g' while already at the top) doesn't
// record a redundant jumplist entry.
func TestListModelNoOpJumpDoesNotPushJump(t *testing.T) {
	jl := &jumplist{}
	m := NewListModel(fixtureCommits(5), jl)
	m.SetSize(80, 3)

	before := len(jl.back)
	m.Update(keyRune('g')) // already at the top
	if len(jl.back) != before {
		t.Fatalf("jl.back length = %d after a no-op 'g', want unchanged at %d", len(jl.back), before)
	}
}

// TestListModelPlainMotionDoesNotPushJump pins down that ordinary j/k
// (unlike g/G/search jumps) never record a jumplist entry — matching vim's
// own distinction between motions and jumps.
func TestListModelPlainMotionDoesNotPushJump(t *testing.T) {
	jl := &jumplist{}
	m := NewListModel(fixtureCommits(5), jl)
	m.SetSize(80, 3)

	before := len(jl.back)
	m.Update(keyRune('j'))
	m.Update(keyRune('j'))
	m.Update(keyRune('k'))
	if len(jl.back) != before {
		t.Fatalf("jl.back length = %d after plain j/k, want unchanged at %d", len(jl.back), before)
	}
}
