package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/maiwald/gitl/internal/git"
)

func TestEnterRequestsDetailAndEscReturnsToSameListState(t *testing.T) {
	list := NewListModel(fixtureCommits(5), &jumplist{})
	list.SetSize(80, 3)
	list.Update(tea.KeyMsg{Type: tea.KeyDown}) // move cursor to index 1

	m := Model{active: viewList, list: list, jl: &jumplist{}}
	m.width, m.height = 80, 24

	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newModel.(Model)
	if cmd == nil {
		t.Fatal("Enter on a selected commit should return a fetch command")
	}
	if m.active != viewList {
		t.Fatalf("active view = %v, want viewList until detail arrives", m.active)
	}
	cursorBeforeDetail := m.list.cursor

	detail := &git.CommitDetail{
		Commit: git.Commit{
			Hash:       "abc123",
			AuthorName: "Someone",
			AuthorDate: time.Now(),
			Subject:    "a subject",
		},
		Body: "a subject\n\nfull message body",
	}
	newModel, _ = m.Update(commitDetailLoadedMsg{detail: detail})
	m = newModel.(Model)
	if m.active != viewDetail {
		t.Fatalf("active view = %v, want viewDetail after detail loads", m.active)
	}
	if !strings.Contains(m.View(), "full message body") {
		t.Fatalf("detail view = %q, want it to contain the full message body", m.View())
	}

	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = newModel.(Model)
	if m.active != viewList {
		t.Fatalf("active view = %v, want viewList after esc", m.active)
	}
	if m.list.cursor != cursorBeforeDetail {
		t.Fatalf("list cursor = %d, want %d (preserved across detail view)", m.list.cursor, cursorBeforeDetail)
	}
}
