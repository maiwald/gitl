package ui

import (
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestScrollPositionTracksViewportAndFile(t *testing.T) {
	m := NewDetailModel(fixtureDetailWithFiles(3, 20), &jumplist{})
	m.SetSize(80, 8)

	top := m.ScrollPosition()
	if !strings.HasSuffix(top, "Top") || strings.Contains(top, "file") || !strings.HasPrefix(top, "L1/") {
		t.Fatalf("at top: ScrollPosition() = %q, want L1/…, no file part, Top", top)
	}

	// Commit -> stats -> first file -> second file.
	m.Update(keyRune(']'))
	m.Update(keyRune(']'))
	m.Update(keyRune(']'))
	mid := m.ScrollPosition()
	if !strings.HasPrefix(mid, "file 2/3") || !strings.HasSuffix(mid, "%") {
		t.Fatalf("on the second file: ScrollPosition() = %q, want file 2/3 and a percentage", mid)
	}

	m.Update(keyRune('G'))
	total := len(m.lines)
	want := "file 3/3  L" + strconv.Itoa(total) + "/" + strconv.Itoa(total) + "  Bot"
	if got := m.ScrollPosition(); got != want {
		t.Fatalf("after G: ScrollPosition() = %q, want %q", got, want)
	}
}

func TestScrollPositionAllWhenEverythingFits(t *testing.T) {
	m := NewDetailModel(fixtureDetailWithFiles(1, 1), &jumplist{})
	m.SetSize(80, 60)
	if got := m.ScrollPosition(); !strings.HasSuffix(got, "All") {
		t.Fatalf("ScrollPosition() = %q, want All for a commit that fits on screen", got)
	}
}

// The position has to stay on screen through every stage of a search: while
// the query is being typed, next to the match count, and after n moves on.
func TestStatusBarShowsScrollPositionWhileSearching(t *testing.T) {
	m := newSearchableDetailModel()

	newModel, _ := m.Update(keyRune('/'))
	m = newModel.(Model)
	for _, r := range "needle" {
		newModel, _ = m.Update(keyRune(r))
		m = newModel.(Model)
	}
	if bar := m.renderStatusBar(); !strings.Contains(bar, "needle") || !strings.Contains(bar, "Top") {
		t.Fatalf("prompt open: status bar = %q, want the query and the position", bar)
	}

	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newModel.(Model)
	bar := m.renderStatusBar()
	if !strings.Contains(bar, "[1/2]") || !strings.Contains(bar, "file 1/2") {
		t.Fatalf("first match: status bar = %q, want [1/2] and file 1/2", bar)
	}

	newModel, _ = m.Update(keyRune('n'))
	m = newModel.(Model)
	bar = m.renderStatusBar()
	if !strings.Contains(bar, "[2/2]") || !strings.Contains(bar, "file 2/2") {
		t.Fatalf("after n: status bar = %q, want [2/2] and file 2/2", bar)
	}
}

func TestStatusBarShowsScrollPositionWithNoMatches(t *testing.T) {
	m := newSearchableDetailModel()
	m = submitDetailSearch(t, m, "zzz")
	if bar := m.renderStatusBar(); !strings.Contains(bar, "no matches") || !strings.Contains(bar, "Top") {
		t.Fatalf("status bar = %q, want no matches and the position", bar)
	}
}

func TestStatusBarStaysOneRowWhenNarrow(t *testing.T) {
	m := newSearchableDetailModel()
	m.setSearchDisplay("a-rather-long-search-term", false)
	runDetailSearch(&m.detail, "a-rather-long-search-term")
	m.width = 30
	bar := m.renderStatusBar()
	if w := lipgloss.Width(bar); w != m.width || strings.Contains(bar, "\n") {
		t.Fatalf("status bar = %q (width %d), want one row of width %d", bar, w, m.width)
	}
	if !strings.Contains(bar, "Top") {
		t.Fatalf("status bar = %q, want the position kept and the left side truncated", bar)
	}
}

func TestJoinStatus(t *testing.T) {
	tests := []struct {
		left, right string
		width       int
		want        string
	}{
		{"/foo", "Top", 12, "/foo     Top"},
		{"/a-long-term", "Top", 10, "/a-lo… Top"},
		{"/foo", "L1/2  Top", 5, "/foo"},
		{"/foo", "", 12, "/foo"},
		{"/foo        ", "Top", 12, "/foo     Top"},
	}
	for _, tt := range tests {
		if got := joinStatus(tt.left, tt.right, tt.width); got != tt.want {
			t.Errorf("joinStatus(%q, %q, %d) = %q, want %q", tt.left, tt.right, tt.width, got, tt.want)
		}
	}
}
