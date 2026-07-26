package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/maiwald/gitl/internal/git"
	"github.com/muesli/termenv"
)

// Tests run without a real terminal, where lipgloss's auto-detected color
// profile strips all styling (Render becomes a no-op), making it
// impossible to assert which style was applied to a span. Force a color
// profile so Render always emits distinguishable output.
func init() {
	lipgloss.SetColorProfile(termenv.TrueColor)
}

func TestHighlightSpansStylesMatchedSubstringCaseInsensitively(t *testing.T) {
	base := lipgloss.NewStyle()
	match := lipgloss.NewStyle().Bold(true)

	got := highlightSpans("Hello World", "world", base, match)
	want := base.Render("Hello ") + match.Render("World")
	if got != want {
		t.Errorf("highlightSpans = %q, want %q", got, want)
	}
}

func TestHighlightSpansNoMatchReturnsBaseRender(t *testing.T) {
	base := lipgloss.NewStyle().Bold(true)
	match := lipgloss.NewStyle()

	got := highlightSpans("Hello World", "xyz", base, match)
	want := base.Render("Hello World")
	if got != want {
		t.Errorf("highlightSpans = %q, want %q", got, want)
	}
}

func TestHighlightSpansEmptyTermReturnsBaseRenderUnchanged(t *testing.T) {
	base := lipgloss.NewStyle().Bold(true)
	got := highlightSpans("Hello World", "", base, lipgloss.NewStyle())
	want := base.Render("Hello World")
	if got != want {
		t.Errorf("highlightSpans = %q, want %q", got, want)
	}
}

func fixtureCommitForRow() git.Commit {
	return git.Commit{
		Hash:       "abc123",
		AuthorName: "Alice",
		AuthorDate: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		Subject:    "fix the searchable bug",
	}
}

func TestRenderCommitRowHighlightsAuthorMatch(t *testing.T) {
	row := renderCommitRow(fixtureCommitForRow(), 80, false, "alice", false)
	want := searchMatchStyle.Render("Alice")
	if !strings.Contains(row, want) {
		t.Errorf("row = %q, want it to contain the highlighted author %q", row, want)
	}
}

func TestRenderCommitRowHighlightsSubjectMatch(t *testing.T) {
	row := renderCommitRow(fixtureCommitForRow(), 80, false, "searchable", false)
	want := searchMatchStyle.Render("searchable")
	if !strings.Contains(row, want) {
		t.Errorf("row = %q, want it to contain the highlighted subject match %q", row, want)
	}
}

func TestRenderCommitRowActiveMatchUsesDistinctStyleFromOrdinaryMatch(t *testing.T) {
	c := fixtureCommitForRow()
	ordinary := renderCommitRow(c, 80, false, "alice", false)
	active := renderCommitRow(c, 80, false, "alice", true)

	if strings.Contains(ordinary, searchActiveMatchStyle.Render("Alice")) {
		t.Error("ordinary match row unexpectedly used the active-match style")
	}
	if !strings.Contains(active, searchActiveMatchStyle.Render("Alice")) {
		t.Errorf("active match row = %q, want it styled with searchActiveMatchStyle", active)
	}
	if ordinary == active {
		t.Error("ordinary and active match rows should render differently")
	}
}

func TestRenderCommitRowSkipsHighlightingWhenSelected(t *testing.T) {
	row := renderCommitRow(fixtureCommitForRow(), 80, true, "alice", true)
	if strings.Contains(row, searchMatchStyle.Render("Alice")) || strings.Contains(row, searchActiveMatchStyle.Render("Alice")) {
		t.Errorf("selected row = %q, should not apply search highlight styles (selection style stays authoritative)", row)
	}
}
