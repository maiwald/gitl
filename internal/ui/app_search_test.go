package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/maiwald/gitl/internal/git"
)

func newSearchableListModel() Model {
	list := NewListModel(fixtureCommits(5))
	list.SetSize(80, 20)
	m := Model{active: viewList, list: list, repo: &git.Repo{}}
	m.width, m.height = 80, 24
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

func TestEnterSubmitsAndClosesPrompt(t *testing.T) {
	m := newSearchableListModel()
	newModel, _ := m.Update(keyRune('/'))
	m = newModel.(Model)
	newModel, _ = m.Update(keyRune('x'))
	m = newModel.(Model)

	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newModel.(Model)
	if m.searchPromptOpen {
		t.Fatal("searchPromptOpen = true after Enter, want false")
	}
}
