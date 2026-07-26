package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/maiwald/gitl/internal/git"
)

// fixtureHashOrder is the hash order fixtureCommits(5) produces (a..e), in
// the same index order the list stores them — matching what a real
// AllCommitHashes call would report for these commits.
var fixtureHashOrder = []string{"a", "b", "c", "d", "e"}

func newSearchableListModel() Model {
	list := NewListModel(fixtureCommits(5))
	list.SetSize(80, 20)
	m := Model{active: viewList, list: list, repo: &git.Repo{}}
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

func TestEnterOnEmptyQueryClearsActiveSearch(t *testing.T) {
	m := newSearchableListModel()
	m = submitListSearch(t, m, "Author")
	newModel, _ := m.Update(allHashesLoadedMsg{term: "Author", hashes: fixtureHashOrder})
	m = newModel.(Model)
	newModel, _ = m.Update(searchHashesLoadedMsg{term: "Author", hashes: map[string]bool{"a": true}})
	m = newModel.(Model)
	if _, _, _, active := m.list.SearchStatus(); !active {
		t.Fatal("search should be active before submitting an empty query")
	}

	newModel, _ = m.Update(keyRune('/'))
	m = newModel.(Model)
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newModel.(Model)

	if _, _, _, active := m.list.SearchStatus(); active {
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
			if term, pos, total, active := m.list.SearchStatus(); !active || term != "x" || total != 1 || pos != 1 {
				t.Fatalf("SearchStatus = (%q, pos=%d, total=%d, active=%v), want (\"x\", 1, 1, true)", term, pos, total, active)
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

	if term, _, total, active := m.list.SearchStatus(); !active || term != "Author" || total != 2 {
		t.Fatalf("SearchStatus = (%q, total=%d, active=%v), want (\"Author\", 2, true)", term, total, active)
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
