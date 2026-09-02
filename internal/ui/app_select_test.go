package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestVTogglesSelection(t *testing.T) {
	m := newSearchableDetailModel()

	newModel, _ := m.Update(keyRune('V'))
	m = newModel.(Model)
	if !m.detail.selecting {
		t.Fatal("selecting = false after V, want true")
	}

	newModel, _ = m.Update(keyRune('V'))
	m = newModel.(Model)
	if m.detail.selecting {
		t.Error("selecting = true after a second V, want false")
	}
}

// TestEscCancelsSelectionWithoutLeavingDetailView is the esc ladder's new
// rung: one press peels the selection and nothing else, so a second press is
// what leaves the view.
func TestEscCancelsSelectionWithoutLeavingDetailView(t *testing.T) {
	m := newSearchableDetailModel()
	newModel, _ := m.Update(keyRune('V'))
	m = newModel.(Model)

	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = newModel.(Model)
	if m.detail.selecting {
		t.Error("selecting = true after esc, want false")
	}
	if m.active != viewDetail {
		t.Fatalf("active = %v after esc cancelled a selection, want viewDetail", m.active)
	}

	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = newModel.(Model)
	if m.active != viewList {
		t.Errorf("active = %v after a second esc, want viewList", m.active)
	}
}

// TestQCancelsSelectionLikeEsc pins that the two halves of the Back binding
// stay interchangeable here, as they are everywhere else.
func TestQCancelsSelectionLikeEsc(t *testing.T) {
	m := newSearchableDetailModel()
	newModel, _ := m.Update(keyRune('V'))
	m = newModel.(Model)

	newModel, _ = m.Update(keyRune('q'))
	m = newModel.(Model)
	if m.detail.selecting {
		t.Error("selecting = true after q, want false")
	}
	if m.quitting || m.active != viewDetail {
		t.Errorf("q cancelling a selection also left the view (quitting=%v active=%v)", m.quitting, m.active)
	}
}

func TestSelectionDoesNotSurviveLeavingDetailView(t *testing.T) {
	m := newSearchableDetailModel()
	newModel, _ := m.Update(keyRune('V'))
	m = newModel.(Model)
	newModel, _ = m.Update(keyRune('j'))
	m = newModel.(Model)

	// esc cancels the selection, the second leaves; the view must come back
	// with no selection either way.
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = newModel.(Model)
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = newModel.(Model)
	if m.detail.selecting {
		t.Error("selecting = true after leaving the detail view")
	}
}

// TestJumplistTraversalEndsASelection covers the other way out of the view:
// ctrl-o lands the cursor somewhere the anchor means nothing.
func TestJumplistTraversalEndsASelection(t *testing.T) {
	m := newSearchableDetailModel()
	m.jl.back = []jumpPos{{view: viewDetail, commit: m.detail.detail, cursorLine: 0}}

	newModel, _ := m.Update(keyRune('V'))
	m = newModel.(Model)
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	m = newModel.(Model)
	if m.detail.selecting {
		t.Error("selecting = true after a jumplist traversal, want false")
	}
}

func TestStatusBarShowsVisualMode(t *testing.T) {
	m := newSearchableDetailModel()
	newModel, _ := m.Update(keyRune('V'))
	m = newModel.(Model)
	if got := m.renderStatusBar(); !strings.Contains(got, "-- VISUAL --") || !strings.Contains(got, "1 line") {
		t.Errorf("status bar = %q, want a visual-mode report of 1 line", got)
	}

	newModel, _ = m.Update(keyRune('j'))
	m = newModel.(Model)
	if got := m.renderStatusBar(); !strings.Contains(got, "2 lines") {
		t.Errorf("status bar = %q, want 2 lines after extending", got)
	}
}
