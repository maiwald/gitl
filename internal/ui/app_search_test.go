package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/maiwald/gitl/internal/git"
)

// fixtureHashOrder is the hash order fixtureCommits(5) produces (a..e), in
// the same index order the list stores them — matching what a real
// AllCommitHashes call would report for these commits.
var fixtureHashOrder = []string{"a", "b", "c", "d", "e"}

// fixtureMarkerCommitDetail is a commit whose only search-relevant text is
// in its message body (not its subject), for tests covering the case a
// commit matches only via a field the list row never displays.
func fixtureMarkerCommitDetail() *git.CommitDetail {
	return &git.CommitDetail{
		Commit: git.Commit{
			Hash:       "abc123",
			AuthorName: "Someone",
			AuthorDate: time.Now(),
			Subject:    "subject",
		},
		Body: "message body with MARKER in it",
	}
}

func newSearchableListModel() Model {
	jl := &jumplist{}
	list := NewListModel(fixtureCommits(5), jl)
	list.SetSize(80, 20)
	m := Model{active: viewList, list: list, repo: &git.Repo{}, jl: jl}
	m.width, m.height = 80, 24
	return m
}

// newSearchableDetailModel builds a Model in the detail view over
// fixtureDetailForSearch (see detail_test.go), which has two "needle"
// matches across its two files.
func newSearchableDetailModel() Model {
	jl := &jumplist{}
	detail := NewDetailModel(fixtureDetailForSearch(), jl)
	detail.SetSize(80, 4)
	m := Model{active: viewDetail, detail: detail, repo: &git.Repo{}, jl: jl}
	m.width, m.height = 80, 24
	return m
}

// submitListSearch drives the prompt (open, type, Enter) to submit term in
// list view, without invoking the real command it returns (which would
// shell out to git) — tests instead feed in hand-built
// searchHashesLoadedMsg/allHashesLoadedMsg messages to control results.
func submitListSearch(t *testing.T, m Model, term string) Model {
	t.Helper()
	newModel, _ := m.Update(keyRune('/'))
	m = newModel.(Model)
	for _, r := range term {
		newModel, _ = m.Update(keyRune(r))
		m = newModel.(Model)
	}
	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newModel.(Model)
	if cmd == nil {
		t.Fatal("expected a command dispatching the position-finding queries")
	}
	if m.pendingSearch == nil || m.pendingSearch.term != term {
		t.Fatalf("pendingSearch = %+v, want a pending search for %q", m.pendingSearch, term)
	}
	return m
}

func TestSlashOpensPromptAndTypingUpdatesSearchInput(t *testing.T) {
	m := newSearchableListModel()

	newModel, cmd := m.Update(keyRune('/'))
	m = newModel.(Model)
	if !m.searchPromptOpen {
		t.Fatal("searchPromptOpen = false after /, want true")
	}
	if cmd == nil {
		t.Fatal("expected a focus command after opening the prompt")
	}
	if !m.searchInput.Focused() {
		t.Fatal("search input should be focused after opening the prompt")
	}

	for _, r := range "foo" {
		newModel, _ = m.Update(keyRune(r))
		m = newModel.(Model)
	}
	if m.searchInput.Value() != "foo" {
		t.Fatalf("search input value = %q, want %q", m.searchInput.Value(), "foo")
	}
}

func TestSlashIsNoopWithoutRepo(t *testing.T) {
	m := newSearchableListModel()
	m.repo = nil

	newModel, _ := m.Update(keyRune('/'))
	m = newModel.(Model)
	if m.searchPromptOpen {
		t.Fatal("searchPromptOpen = true without a loaded repo, want false")
	}
}

func TestSearchPromptAcceptsLiteralQAndEscCancels(t *testing.T) {
	m := newSearchableListModel()
	newModel, _ := m.Update(keyRune('/'))
	m = newModel.(Model)

	newModel, _ = m.Update(keyRune('q'))
	m = newModel.(Model)
	if m.quitting {
		t.Fatal("typing q into the search prompt must not quit the app")
	}
	if m.searchInput.Value() != "q" {
		t.Fatalf("search input value = %q, want %q", m.searchInput.Value(), "q")
	}

	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = newModel.(Model)
	if m.searchPromptOpen {
		t.Fatal("searchPromptOpen = true after Esc, want false")
	}
	if m.searchInput.Focused() {
		t.Fatal("search input should be blurred after Esc")
	}
}

func TestBackspaceOnEmptyPromptCancels(t *testing.T) {
	m := newSearchableListModel()
	newModel, _ := m.Update(keyRune('/'))
	m = newModel.(Model)

	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = newModel.(Model)

	if m.searchPromptOpen {
		t.Fatal("searchPromptOpen = true after Backspace on an empty query, want false")
	}
	if m.searchInput.Focused() {
		t.Fatal("search input should be blurred after Backspace closes the prompt")
	}
}

func TestBackspaceOnNonEmptyPromptDeletesInsteadOfCancelling(t *testing.T) {
	m := newSearchableListModel()
	newModel, _ := m.Update(keyRune('/'))
	m = newModel.(Model)
	newModel, _ = m.Update(keyRune('x'))
	m = newModel.(Model)

	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = newModel.(Model)

	if !m.searchPromptOpen {
		t.Fatal("Backspace on a non-empty query should delete a character, not cancel the prompt")
	}
	if m.searchInput.Value() != "" {
		t.Fatalf("search input value = %q, want empty after deleting the only character", m.searchInput.Value())
	}
}

func TestEnterOnEmptyQueryClearsActiveSearch(t *testing.T) {
	m := newSearchableListModel()
	m = submitListSearch(t, m, "Author")
	newModel, _ := m.Update(allHashesLoadedMsg{term: "Author", hashes: fixtureHashOrder})
	m = newModel.(Model)
	newModel, _ = m.Update(searchHashesLoadedMsg{term: "Author", hashes: map[string]bool{"a": true}})
	m = newModel.(Model)
	if !m.hasActiveSearch() {
		t.Fatal("search should be active before submitting an empty query")
	}

	newModel, _ = m.Update(keyRune('/'))
	m = newModel.(Model)
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newModel.(Model)

	if m.hasActiveSearch() {
		t.Fatal("an empty query should clear the active search")
	}
}

func TestListSearchResolvesRegardlessOfMessageOrder(t *testing.T) {
	for _, order := range []string{"hashesFirst", "orderFirst"} {
		t.Run(order, func(t *testing.T) {
			m := newSearchableListModel()
			m = submitListSearch(t, m, "x")

			hashesMsg := searchHashesLoadedMsg{term: "x", hashes: map[string]bool{"c": true}}
			orderMsg := allHashesLoadedMsg{term: "x", hashes: fixtureHashOrder}

			var first, second tea.Msg = hashesMsg, orderMsg
			if order == "orderFirst" {
				first, second = orderMsg, hashesMsg
			}

			newModel, _ := m.Update(first)
			m = newModel.(Model)
			if m.pendingSearch == nil {
				t.Fatal("pendingSearch resolved too early, before both queries arrived")
			}

			newModel, _ = m.Update(second)
			m = newModel.(Model)
			if m.pendingSearch != nil {
				t.Fatal("pendingSearch should be resolved once both queries have arrived")
			}
			pos, total := m.list.MatchPosition()
			if !m.hasActiveSearch() || m.searchTerm != "x" || total != 1 || pos != 1 {
				t.Fatalf("term=%q pos=%d total=%d active=%v, want (\"x\", 1, 1, true)", m.searchTerm, pos, total, m.hasActiveSearch())
			}
			if c, ok := m.list.Selected(); !ok || c.Hash != "c" {
				t.Fatalf("selection = %+v, ok=%v, want the matched commit (hash c)", c, ok)
			}
		})
	}
}

func TestListSearchIgnoresStaleResultFromSupersededSearch(t *testing.T) {
	m := newSearchableListModel()
	m = submitListSearch(t, m, "first")

	// Start a second search before the first one's queries return.
	m = submitListSearch(t, m, "second")

	newModel, _ := m.Update(searchHashesLoadedMsg{term: "first", hashes: map[string]bool{"a": true}})
	m = newModel.(Model)
	if m.pendingSearch == nil || m.pendingSearch.term != "second" {
		t.Fatalf("a stale result for the superseded search must not affect the current pendingSearch, got %+v", m.pendingSearch)
	}
}

func TestNAndShiftNCycleListMatchesEndToEnd(t *testing.T) {
	m := newSearchableListModel()
	m = submitListSearch(t, m, "Author")
	newModel, _ := m.Update(allHashesLoadedMsg{term: "Author", hashes: fixtureHashOrder})
	m = newModel.(Model)
	newModel, _ = m.Update(searchHashesLoadedMsg{term: "Author", hashes: map[string]bool{"a": true, "b": true}})
	m = newModel.(Model)

	_, total := m.list.MatchPosition()
	if !m.hasActiveSearch() || m.searchTerm != "Author" || total != 2 {
		t.Fatalf("term=%q total=%d active=%v, want (\"Author\", 2, true)", m.searchTerm, total, m.hasActiveSearch())
	}
	if c, ok := m.list.Selected(); !ok || c.Hash != "a" {
		t.Fatalf("initial match = %+v, ok=%v, want hash a", c, ok)
	}

	newModel, _ = m.Update(keyRune('n'))
	m = newModel.(Model)
	if c, ok := m.list.Selected(); !ok || c.Hash != "b" {
		t.Fatalf("after n: selection = %+v, ok=%v, want hash b", c, ok)
	}

	newModel, _ = m.Update(keyRune('N'))
	m = newModel.(Model)
	if c, ok := m.list.Selected(); !ok || c.Hash != "a" {
		t.Fatalf("after N: selection = %+v, ok=%v, want hash a", c, ok)
	}
}

func TestSlashSearchesDetailViewContentSynchronously(t *testing.T) {
	m := newSearchableDetailModel()

	newModel, _ := m.Update(keyRune('/'))
	m = newModel.(Model)
	for _, r := range "needle" {
		newModel, _ = m.Update(keyRune(r))
		m = newModel.(Model)
	}
	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newModel.(Model)

	if cmd != nil {
		t.Fatal("detail search is fully synchronous (the diff is already in memory), should not dispatch a command")
	}
	pos, total := m.detail.MatchPosition()
	if !m.hasActiveSearch() || m.searchTerm != "needle" || total != 2 || pos != 1 {
		t.Fatalf("term=%q pos=%d total=%d active=%v, want (\"needle\", 1, 2, true)", m.searchTerm, pos, total, m.hasActiveSearch())
	}
}

func TestNAndShiftNCycleDetailMatchesEndToEnd(t *testing.T) {
	m := newSearchableDetailModel()
	m.setSearchDisplay("needle", false)
	m.detail.RunSearch("needle")

	newModel, _ := m.Update(keyRune('n'))
	m = newModel.(Model)
	if pos, total := m.detail.MatchPosition(); pos != 2 || total != 2 {
		t.Fatalf("MatchPosition after n = %d/%d, want 2/2", pos, total)
	}

	newModel, _ = m.Update(keyRune('N'))
	m = newModel.(Model)
	if pos, _ := m.detail.MatchPosition(); pos != 1 {
		t.Fatalf("MatchPosition pos after N = %d, want wrap to 1", pos)
	}
}

func TestEnterOnEmptyQueryClearsDetailSearch(t *testing.T) {
	m := newSearchableDetailModel()
	m.setSearchDisplay("needle", false)
	m.detail.RunSearch("needle")

	newModel, _ := m.Update(keyRune('/'))
	m = newModel.(Model)
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newModel.(Model)

	if m.hasActiveSearch() {
		t.Fatal("an empty query should clear the active detail search")
	}
}

func TestStatusBarShowsDetailSearchPosition(t *testing.T) {
	m := newSearchableDetailModel()
	m.setSearchDisplay("needle", false)
	m.detail.RunSearch("needle")

	bar := m.renderStatusBar()
	if !strings.Contains(bar, "needle") || !strings.Contains(bar, "1/2") {
		t.Fatalf("status bar = %q, want it to show the term and position", bar)
	}
}

func TestStatusBarShowsNoMatches(t *testing.T) {
	m := newSearchableListModel()
	m = submitListSearch(t, m, "nomatch")
	newModel, _ := m.Update(allHashesLoadedMsg{term: "nomatch", hashes: fixtureHashOrder})
	m = newModel.(Model)
	newModel, _ = m.Update(searchHashesLoadedMsg{term: "nomatch", hashes: map[string]bool{}})
	m = newModel.(Model)

	bar := m.renderStatusBar()
	if !strings.Contains(bar, "no matches") {
		t.Fatalf("status bar = %q, want it to say no matches", bar)
	}
}

// activeListSearch submits term as a list search and resolves it with the
// given match set, the same two-message resolution
// TestNAndShiftNCycleListMatchesEndToEnd drives manually.
func activeListSearch(t *testing.T, m Model, term string, matchedHashes map[string]bool) Model {
	t.Helper()
	m = submitListSearch(t, m, term)
	newModel, _ := m.Update(allHashesLoadedMsg{term: term, hashes: fixtureHashOrder})
	m = newModel.(Model)
	newModel, _ = m.Update(searchHashesLoadedMsg{term: term, hashes: matchedHashes})
	return newModel.(Model)
}

func TestEscOnActiveListSearchDismissesWithoutQuitting(t *testing.T) {
	m := newSearchableListModel()
	m = activeListSearch(t, m, "Author", map[string]bool{"a": true, "b": true})

	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = newModel.(Model)

	if m.quitting {
		t.Fatal("Esc on an active search should dismiss it, not quit")
	}
	if !m.hasActiveSearch() {
		t.Fatal("dismissing must not discard the search term/matches")
	}
	if !m.searchDismissed {
		t.Fatal("searchDismissed should be true after the first Esc")
	}
	if strings.Contains(m.renderStatusBar(), "Author") {
		t.Fatalf("status bar = %q, should hide the search indicator while dismissed", m.renderStatusBar())
	}
}

func TestSecondEscOnDismissedListSearchQuits(t *testing.T) {
	m := newSearchableListModel()
	m = activeListSearch(t, m, "Author", map[string]bool{"a": true, "b": true})

	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc}) // dismiss
	m = newModel.(Model)
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc}) // now quit
	m = newModel.(Model)

	if !m.quitting {
		t.Fatal("a second Esc, with the search already dismissed, should quit")
	}
}

func TestNRevivesDismissedListSearchAndStatusBar(t *testing.T) {
	m := newSearchableListModel()
	m = activeListSearch(t, m, "Author", map[string]bool{"a": true, "b": true})

	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = newModel.(Model)
	if !m.searchDismissed {
		t.Fatal("expected dismissed after Esc")
	}

	newModel, _ = m.Update(keyRune('n'))
	m = newModel.(Model)

	if m.searchDismissed {
		t.Fatal("n should revive a dismissed search")
	}
	if c, ok := m.list.Selected(); !ok || c.Hash != "b" {
		t.Fatalf("after reviving with n: selection = %+v, ok=%v, want hash b (the next match)", c, ok)
	}
	if !strings.Contains(m.renderStatusBar(), "Author") {
		t.Fatalf("status bar = %q, want the search indicator to reappear after reviving", m.renderStatusBar())
	}
}

func TestEscOnActiveDetailSearchDismissesWithoutGoingBack(t *testing.T) {
	m := newSearchableDetailModel()
	m.setSearchDisplay("needle", false)
	m.detail.RunSearch("needle")

	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = newModel.(Model)

	if m.active != viewDetail {
		t.Fatal("Esc on an active detail search should dismiss it, not go back to the list")
	}
	if !m.searchDismissed {
		t.Fatal("searchDismissed should be true after the first Esc")
	}
}

func TestSecondEscOnDismissedDetailSearchGoesBackToList(t *testing.T) {
	m := newSearchableDetailModel()
	m.setSearchDisplay("needle", false)
	m.detail.RunSearch("needle")

	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc}) // dismiss
	m = newModel.(Model)
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc}) // now go back
	m = newModel.(Model)

	if m.active != viewList {
		t.Fatal("a second Esc, with the search already dismissed, should go back to the list")
	}
	if !m.searchDismissed {
		t.Fatal("dismissed state should persist across the view transition, since it's shared")
	}
}

func TestNRevivesDismissedDetailSearchAndStatusBar(t *testing.T) {
	m := newSearchableDetailModel()
	m.setSearchDisplay("needle", false)
	m.detail.RunSearch("needle")

	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = newModel.(Model)

	newModel, _ = m.Update(keyRune('n'))
	m = newModel.(Model)

	if m.searchDismissed {
		t.Fatal("n should revive a dismissed search")
	}
	if !strings.Contains(m.renderStatusBar(), "needle") {
		t.Fatalf("status bar = %q, want the search indicator to reappear after reviving", m.renderStatusBar())
	}
}

// TestOpeningCommitWithActiveSearchHighlightsMessageMatch covers the bug
// this whole feature was built to fix: a commit matched only via its
// message body (invisible in the list row, since the row only shows the
// subject) must show the match the moment it's opened, not just once n/N
// is pressed.
func TestOpeningCommitWithActiveSearchHighlightsMessageMatch(t *testing.T) {
	list := NewListModel(fixtureCommits(5), &jumplist{})
	list.SetSize(80, 20)
	m := Model{active: viewList, list: list, repo: &git.Repo{}, jl: &jumplist{}}
	m.width, m.height = 80, 24
	m.setSearchDisplay("MARKER", false)

	detail := fixtureMarkerCommitDetail()
	newModel, _ := m.Update(commitDetailLoadedMsg{detail: detail})
	m = newModel.(Model)

	if m.active != viewDetail {
		t.Fatalf("active view = %v, want viewDetail", m.active)
	}
	if pos, total := m.detail.MatchPosition(); total != 1 || pos != 1 {
		t.Fatalf("detail MatchPosition = (%d, %d), want (1, 1) — opening the commit should search it immediately", pos, total)
	}
	if !strings.Contains(m.View(), searchActiveMatchStyle.Render("MARKER")) {
		t.Fatalf("detail view = %q, want the message match visibly highlighted right after opening", m.View())
	}
}

func TestOpeningCommitWithDismissedSearchStaysDismissed(t *testing.T) {
	list := NewListModel(fixtureCommits(5), &jumplist{})
	list.SetSize(80, 20)
	m := Model{active: viewList, list: list, repo: &git.Repo{}, jl: &jumplist{}}
	m.width, m.height = 80, 24
	m.setSearchDisplay("MARKER", true) // already dismissed in the list

	detail := fixtureMarkerCommitDetail()
	newModel, _ := m.Update(commitDetailLoadedMsg{detail: detail})
	m = newModel.(Model)

	if pos, total := m.detail.MatchPosition(); total != 1 || pos != 1 {
		t.Fatalf("detail MatchPosition = (%d, %d), want (1, 1) — matches should still be computed even while dismissed", pos, total)
	}
	view := m.View()
	if strings.Contains(view, searchActiveMatchStyle.Render("MARKER")) || strings.Contains(view, searchMatchStyle.Render("MARKER")) {
		t.Fatalf("detail view = %q, want no highlight while the search is dismissed", view)
	}
}
