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
			ShortHash:  string(rune('a' + i)),
			AuthorName: "Author",
			AuthorDate: time.Now(),
			Subject:    "subject",
		}
	}
	return commits
}

func TestListModelCursorMovement(t *testing.T) {
	m := NewListModel(fixtureCommits(5))
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
	m := NewListModel(fixtureCommits(2))
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
	m := NewListModel(fixtureCommits(10))
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
