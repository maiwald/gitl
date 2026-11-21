package ui

import (
	"fmt"
	"slices"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func openPrompt(t *testing.T, m Model, typed string) Model {
	t.Helper()
	newModel, _ := m.Update(keyRune('/'))
	m = newModel.(Model)
	for _, r := range typed {
		newModel, _ = m.Update(keyRune(r))
		m = newModel.(Model)
	}
	return m
}

func pressKey(m Model, k tea.KeyType) Model {
	newModel, _ := m.Update(tea.KeyMsg{Type: k})
	return newModel.(Model)
}

func TestSearchHistoryArrowsStepThroughPastTermsAndBackToDraft(t *testing.T) {
	m := newSearchableDetailModel()
	m = submitDetailSearch(t, m, "alpha")
	m = submitDetailSearch(t, m, "beta")
	m = openPrompt(t, m, "x")

	for i, step := range []struct {
		key  tea.KeyType
		want string
	}{
		{tea.KeyUp, "beta"},
		{tea.KeyUp, "alpha"},
		{tea.KeyUp, "alpha"},
		{tea.KeyDown, "beta"},
		{tea.KeyDown, "x"},
		{tea.KeyDown, "x"},
	} {
		m = pressKey(m, step.key)
		if got := m.searchInput.Value(); got != step.want {
			t.Fatalf("step %d: prompt = %q, want %q", i, got, step.want)
		}
	}
}

func TestSubmittingRecalledTermSearchesForItAndMovesItToNewest(t *testing.T) {
	m := newSearchableDetailModel()
	m = submitDetailSearch(t, m, "needle")
	m = submitDetailSearch(t, m, "beta")
	m = openPrompt(t, m, "")
	m = pressKey(m, tea.KeyUp)
	m = pressKey(m, tea.KeyUp)
	m = pressKey(m, tea.KeyEnter)

	if m.searchTerm != "needle" {
		t.Fatalf("searchTerm = %q, want the recalled needle", m.searchTerm)
	}
	if want := []string{"beta", "needle"}; !slices.Equal(m.searchHistory, want) {
		t.Fatalf("searchHistory = %q, want %q", m.searchHistory, want)
	}
}

func TestCancelledAndEmptyQueriesAreNotRecorded(t *testing.T) {
	m := newSearchableDetailModel()
	m = openPrompt(t, m, "gone")
	m = pressKey(m, tea.KeyEsc)
	m = openPrompt(t, m, "")
	m = pressKey(m, tea.KeyEnter)
	if len(m.searchHistory) != 0 {
		t.Fatalf("searchHistory = %q, want it empty", m.searchHistory)
	}
}

func TestPromptStillTypesKAndJ(t *testing.T) {
	m := newSearchableDetailModel()
	m = submitDetailSearch(t, m, "alpha")
	m = openPrompt(t, m, "kj")
	if got := m.searchInput.Value(); got != "kj" {
		t.Fatalf("prompt = %q, want k and j typed as letters", got)
	}
}

func TestSearchHistoryIsCapped(t *testing.T) {
	var m Model
	for i := range searchHistoryCap + 1 {
		m.recordSearch(fmt.Sprintf("t%d", i))
	}
	if len(m.searchHistory) != searchHistoryCap || m.searchHistory[0] != "t1" {
		t.Fatalf("history has %d entries starting %q, want %d starting t1", len(m.searchHistory), m.searchHistory[0], searchHistoryCap)
	}
}
