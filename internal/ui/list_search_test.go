package ui

import "testing"

func TestListModelSearchJumpsToFirstMatchAtOrAfterCursorAndWraps(t *testing.T) {
	m := NewListModel(fixtureCommits(10))
	m.SetTotalCount(10)
	m.SetSize(80, 5)
	m.cursor = 4

	needsLoad, target := m.Search([]int{1, 6, 8})
	if needsLoad {
		t.Fatal("needsLoad = true for an already-loaded match, want false")
	}
	if target != 6 {
		t.Fatalf("target = %d, want 6 (first match at/after cursor)", target)
	}
	if c, ok := m.Selected(); !ok || c.Hash != string(rune('a'+6)) {
		t.Fatalf("selection = %+v, ok=%v, want commit at index 6", c, ok)
	}

	// All matches before the cursor: wraps to the first.
	m.cursor = 9
	_, target = m.Search([]int{1, 6, 8})
	if target != 1 {
		t.Fatalf("target = %d, want 1 (wrap to first match)", target)
	}
}

func TestListModelNextPrevMatchCyclesAndWraps(t *testing.T) {
	m := NewListModel(fixtureCommits(10))
	m.SetTotalCount(10)
	m.SetSize(80, 5)
	m.cursor = 0

	m.Search([]int{2, 5, 8})

	if _, target := m.NextMatch(); target != 5 {
		t.Fatalf("target after NextMatch = %d, want 5", target)
	}
	if _, target := m.NextMatch(); target != 8 {
		t.Fatalf("target after 2x NextMatch = %d, want 8", target)
	}
	if _, target := m.NextMatch(); target != 2 {
		t.Fatalf("NextMatch past the last match = %d, want wrap to 2", target)
	}
	if _, target := m.PrevMatch(); target != 8 {
		t.Fatalf("PrevMatch before the first match = %d, want wrap to 8", target)
	}
}

func TestListModelSearchReportsNeedsLoadForUnloadedMatch(t *testing.T) {
	m := NewListModel(fixtureCommits(10))
	m.SetTotalCount(100)
	m.SetSize(80, 5)

	needsLoad, target := m.Search([]int{50})
	if !needsLoad {
		t.Fatal("needsLoad = false for a match beyond the loaded range, want true")
	}
	if target != 50 {
		t.Fatalf("target = %d, want 50", target)
	}
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want unchanged (0) until the target is loaded", m.cursor)
	}

	req, ok := m.RequestLoadUpTo(target)
	if !ok {
		t.Fatal("RequestLoadUpTo should dispatch a fetch")
	}
	if req.skip != 10 {
		t.Fatalf("fetch request skip = %d, want 10", req.skip)
	}

	rest := fixtureCommits(100)[10:]
	m.AppendCommits(rest)
	if c, ok := m.Selected(); !ok || c.Hash != string(rune('a'+50)) {
		t.Fatalf("after AppendCommits: selection = %+v, ok=%v, want commit at index 50", c, ok)
	}
}

func TestListModelSearchNoMatches(t *testing.T) {
	m := NewListModel(fixtureCommits(10))
	m.SetTotalCount(10)
	m.SetSize(80, 5)

	needsLoad, _ := m.Search(nil)
	if needsLoad {
		t.Fatal("needsLoad = true with no matches, want false")
	}
	if pos, total := m.MatchPosition(); pos != 0 || total != 0 {
		t.Fatalf("MatchPosition = (%d, %d), want (0, 0)", pos, total)
	}
}

func TestListModelClearSearchResetsStatus(t *testing.T) {
	m := NewListModel(fixtureCommits(10))
	m.SetTotalCount(10)
	m.SetSize(80, 5)

	m.Search([]int{2, 5})
	m.ClearSearch()

	if pos, total := m.MatchPosition(); pos != 0 || total != 0 {
		t.Fatalf("MatchPosition after ClearSearch = (%d, %d), want (0, 0)", pos, total)
	}
	if _, target := m.NextMatch(); target != 0 {
		t.Fatalf("NextMatch after ClearSearch should be a no-op, target = %d", target)
	}
}

func TestJumpToBottomStillGoesThroughBeginLoadingUpTo(t *testing.T) {
	m := NewListModel(fixtureCommits(10))
	m.SetTotalCount(100)
	m.SetSize(80, 5)

	req, ok := m.jumpToBottom()
	if !ok {
		t.Fatal("jumpToBottom should request a fetch when history isn't fully loaded")
	}
	if req.skip != 10 || req.limit != 90 {
		t.Fatalf("fetch request = %+v, want skip=10 limit=90 (load everything, unchanged behavior)", req)
	}
}
