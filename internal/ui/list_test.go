package ui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/maiwald/gitl/internal/git"
)

func fixtureCommits(n int) []git.Commit {
	commits := make([]git.Commit, n)
	for i := 0; i < n; i++ {
		commits[i] = git.Commit{
			Hash:       string(rune('a' + i)),
			AuthorName: "Author",
			AuthorDate: time.Now(),
			Subject:    "subject",
		}
	}
	return commits
}

func TestListModelCursorMovement(t *testing.T) {
	m := NewListModel(fixtureCommits(5), &jumplist{})
	m.SetSize(80, 3)

	if c, ok := m.Selected(); !ok || c.Hash != "a" {
		t.Fatalf("initial selection = %+v, ok=%v, want hash a", c, ok)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if c, ok := m.Selected(); !ok || c.Hash != "b" {
		t.Fatalf("after down: selection = %+v, ok=%v, want hash b", c, ok)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if c, ok := m.Selected(); !ok || c.Hash != "a" {
		t.Fatalf("after up: selection = %+v, ok=%v, want hash a", c, ok)
	}
}

func TestListModelCursorClampsAtBounds(t *testing.T) {
	m := NewListModel(fixtureCommits(2), &jumplist{})
	m.SetSize(80, 3)

	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if c, ok := m.Selected(); !ok || c.Hash != "a" {
		t.Fatalf("up at top: selection = %+v, ok=%v, want hash a", c, ok)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if c, ok := m.Selected(); !ok || c.Hash != "b" {
		t.Fatalf("down past bottom: selection = %+v, ok=%v, want hash b", c, ok)
	}
}

func TestListModelScrollsToKeepCursorVisible(t *testing.T) {
	m := NewListModel(fixtureCommits(10), &jumplist{})
	m.SetSize(80, 3)

	for i := 0; i < 5; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.cursor != 5 {
		t.Fatalf("cursor = %d, want 5", m.cursor)
	}
	if m.vp.YOffset > m.cursor || m.cursor >= m.vp.YOffset+m.vp.Height {
		t.Fatalf("cursor %d not visible with YOffset=%d height=%d", m.cursor, m.vp.YOffset, m.vp.Height)
	}
}

// TestListModelJumpToAlreadyLoaded pins down JumpTo's immediate path: when
// the target position is already loaded, the cursor moves there right
// away and no fetch is needed.
func TestListModelJumpToAlreadyLoaded(t *testing.T) {
	m := NewListModel(fixtureCommits(5), &jumplist{})
	m.SetSize(80, 3)
	m.SetTotalCount(5)

	if needsLoad := m.JumpTo(3); needsLoad {
		t.Fatal("JumpTo(3): needsLoad = true, want false (already loaded)")
	}
	if c, ok := m.Selected(); !ok || c.Hash != "d" {
		t.Fatalf("after JumpTo(3): selection = %+v ok=%v, want hash d (commit index 3)", c, ok)
	}
}

// TestListModelJumpToDefersUntilLoaded pins down JumpTo's deferred path:
// when the target isn't loaded yet, it reports needsLoad and the cursor
// only moves once AppendCommits brings the target into range — the same
// pendingCursor mechanism jumpToBottom and search jumps use.
func TestListModelJumpToDefersUntilLoaded(t *testing.T) {
	m := NewListModel(fixtureCommits(5), &jumplist{})
	m.SetSize(80, 3)
	m.SetTotalCount(10)

	if needsLoad := m.JumpTo(7); !needsLoad {
		t.Fatal("JumpTo(7): needsLoad = false, want true (not loaded yet)")
	}
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want unchanged at 0 until AppendCommits delivers the target", m.cursor)
	}

	m.AppendCommits(fixtureCommits(5)) // brings total loaded to 10, including index 7
	if c, ok := m.Selected(); !ok || c.Hash != "c" {
		t.Fatalf("after AppendCommits: selection = %+v ok=%v, want hash c (the new batch's local index 2, i.e. absolute index 7)", c, ok)
	}
}

func fixtureLeadingSummaries() []git.EntrySummary {
	return []git.EntrySummary{
		{Kind: git.EntryWorktree, Files: 1, Additions: 1},
		{Kind: git.EntryStaged, Files: 1, Additions: 2},
	}
}

// TestListModelCursorMovesOntoLeadingRowsAndClamps pins down the
// negative-cursor scheme: moving up from commits[0] steps onto the
// synthetic leading rows (staged, then working tree, matching display
// order), Selected() reports false there while SelectedSummary() reports the
// row, and the cursor clamps at the topmost leading row rather than going
// negative indefinitely.
func TestListModelCursorMovesOntoLeadingRowsAndClamps(t *testing.T) {
	m := NewListModel(fixtureCommits(3), &jumplist{})
	m.SetLeadingSummaries(fixtureLeadingSummaries())
	m.SetSize(80, 10)

	if c, ok := m.Selected(); !ok || c.Hash != "a" {
		t.Fatalf("initial selection = %+v ok=%v, want hash a", c, ok)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if _, ok := m.Selected(); ok {
		t.Fatal("Selected() = true while cursor is on a leading row, want false")
	}
	if e, ok := m.SelectedSummary(); !ok || e.Kind != git.EntryStaged {
		t.Fatalf("after 1 up: entry = %+v ok=%v, want EntryStaged", e, ok)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if e, ok := m.SelectedSummary(); !ok || e.Kind != git.EntryWorktree {
		t.Fatalf("after 2 up: entry = %+v ok=%v, want EntryWorktree", e, ok)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyUp}) // one more: should clamp, not go further negative
	if e, ok := m.SelectedSummary(); !ok || e.Kind != git.EntryWorktree {
		t.Fatalf("after clamping: entry = %+v ok=%v, want still EntryWorktree", e, ok)
	}
}

// TestListModelTopKeyLandsOnTopmostLeadingRow pins down that 'g' (Top) goes
// all the way to the topmost synthetic row when leading rows are present,
// not just to commits[0].
func TestListModelTopKeyLandsOnTopmostLeadingRow(t *testing.T) {
	m := NewListModel(fixtureCommits(5), &jumplist{})
	m.SetLeadingSummaries(fixtureLeadingSummaries())
	m.SetSize(80, 3)

	for i := 0; i < 3; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})

	e, ok := m.SelectedSummary()
	if !ok || e.Kind != git.EntryWorktree {
		t.Fatalf("after 'g': entry = %+v ok=%v, want topmost EntryWorktree", e, ok)
	}
}

// TestSetLeadingSummariesDoesNotMoveCursorOffCommit pins down that installing
// leading rows after the cursor is already on a commit leaves that
// selection untouched — the cursor stays in commit-index space regardless
// of how many synthetic rows precede it.
func TestSetLeadingSummariesDoesNotMoveCursorOffCommit(t *testing.T) {
	m := NewListModel(fixtureCommits(5), &jumplist{})
	m.SetSize(80, 3)
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyDown})

	m.SetLeadingSummaries(fixtureLeadingSummaries())

	c, ok := m.Selected()
	if !ok || c.Hash != "c" {
		t.Fatalf("after SetLeadingSummaries: selection = %+v ok=%v, want hash c (unmoved)", c, ok)
	}
}

// TestListModelSearchIndicesUnaffectedByLeadingRows is the key regression
// check for the negative-cursor design: with leading rows present, search
// match positions are still real commit indices (never offset by
// len(leading)), so Search/NextMatch land the cursor on the correct commit.
func TestListModelSearchIndicesUnaffectedByLeadingRows(t *testing.T) {
	m := NewListModel(fixtureCommits(5), &jumplist{})
	m.SetLeadingSummaries(fixtureLeadingSummaries())
	m.SetSize(80, 3)
	m.SetTotalCount(5)

	needsLoad, target := runListSearch(&m, []int{2, 4})
	if needsLoad {
		t.Fatal("Search: needsLoad = true, want false (already loaded)")
	}
	if target != 2 {
		t.Fatalf("Search: target = %d, want 2", target)
	}
	if c, ok := m.Selected(); !ok || c.Hash != "c" {
		t.Fatalf("after Search: selection = %+v ok=%v, want hash c (commit index 2)", c, ok)
	}

	needsLoad, target = m.NextMatch()
	if needsLoad || target != 4 {
		t.Fatalf("NextMatch: needsLoad=%v target=%d, want false,4", needsLoad, target)
	}
	if c, ok := m.Selected(); !ok || c.Hash != "e" {
		t.Fatalf("after NextMatch: selection = %+v ok=%v, want hash e (commit index 4)", c, ok)
	}
}

// TestListModelLoadMoreSkipLimitUnaffectedByLeadingRows is the other key
// regression check: maybeLoadMore's fetchRequest is still keyed to
// len(commits), never shifted by len(leading).
func TestListModelLoadMoreSkipLimitUnaffectedByLeadingRows(t *testing.T) {
	m := NewListModel(fixtureCommits(30), &jumplist{})
	m.SetLeadingSummaries(fixtureLeadingSummaries())
	m.SetTotalCount(100)
	m.SetSize(80, 5)

	// loadingMore latches true on the first triggering press (see
	// maybeLoadMore), so only the first press that crosses the lookahead
	// threshold actually returns a fetch request — later presses in the
	// same loop would see loadingMore already true and report false.
	var req fetchRequest
	var ok bool
	for i := 0; i < 15 && !ok; i++ {
		req, ok = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	if !ok {
		t.Fatal("expected a fetch request once the cursor is within lookahead of the loaded end")
	}
	if req.skip != 30 || req.limit != batchSize {
		t.Fatalf("fetchRequest = %+v, want skip=30 limit=%d (unaffected by leading rows)", req, batchSize)
	}
}
