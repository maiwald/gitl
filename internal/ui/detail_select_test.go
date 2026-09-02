package ui

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/maiwald/gitl/internal/git"
)

// highlightedRows reports which flattened rows carry the cursor row's
// background, which is what a selection and the cursor both paint with.
func highlightedRows(m *DetailModel) []int {
	var rows []int
	for i, row := range m.content[:m.contentLen] {
		if strings.Contains(row, cursorRowSGR()) {
			rows = append(rows, i)
		}
	}
	return rows
}

// cursorRowSGR is the background sequence a highlighted row carries, taken
// from lipgloss itself rather than spelled out, so a palette change doesn't
// silently make these tests assert on nothing.
func cursorRowSGR() string {
	rendered := selectedRowStyle.Render(" ")
	return rendered[:strings.Index(rendered, " ")]
}

func TestVisualSelectionCoversAnchorToCursor(t *testing.T) {
	m := NewDetailModel(fixtureDetailWithFiles(1, 6), &jumplist{})
	m.SetSize(80, 30)
	m.setCursor(0)

	if got := highlightedRows(&m); len(got) != 1 {
		t.Fatalf("before V: %d highlighted rows, want 1 (the cursor)", len(got))
	}

	m.startSelection()
	m.moveCursor(1)
	m.moveCursor(1)

	want := []int{0, 1, 2}
	if got := highlightedRows(&m); !slices.Equal(got, want) {
		t.Errorf("highlighted rows = %v, want %v", got, want)
	}
	if n := m.selectedLineCount(); n != 3 {
		t.Errorf("selectedLineCount = %d, want 3", n)
	}
}

// TestVisualSelectionExtendsUpwards pins that the anchor is an end, not a
// floor: moving above it selects downwards to it.
func TestVisualSelectionExtendsUpwards(t *testing.T) {
	m := NewDetailModel(fixtureDetailWithFiles(1, 6), &jumplist{})
	m.SetSize(80, 30)
	m.setCursor(3)
	m.startSelection()
	m.moveCursor(-1)
	m.moveCursor(-1)

	want := []int{1, 2, 3}
	if got := highlightedRows(&m); !slices.Equal(got, want) {
		t.Errorf("highlighted rows = %v, want %v", got, want)
	}
}

func TestClearingSelectionLeavesOnlyTheCursorHighlighted(t *testing.T) {
	m := NewDetailModel(fixtureDetailWithFiles(1, 6), &jumplist{})
	m.SetSize(80, 30)
	m.setCursor(0)
	m.startSelection()
	m.moveCursor(1)
	m.moveCursor(1)
	m.clearSelection()

	if got := highlightedRows(&m); !slices.Equal(got, []int{2}) {
		t.Errorf("highlighted rows = %v, want just the cursor at 2", got)
	}
	if m.selecting {
		t.Error("selecting = true after clearSelection")
	}
}

// TestSelectionPatchMatchesFullReflow is the selection's half of
// TestDetailModelCursorPatchMatchesFullReflow: growing, shrinking and
// reversing a selection patches lines into the cached content, which must stay
// byte-for-byte what a full reflow at the same position would produce.
func TestSelectionPatchMatchesFullReflow(t *testing.T) {
	m := NewDetailModel(fixtureDetailWithLongLines(), &jumplist{})
	m.SetSize(60, 8)
	m.SetSearchDisplay("context", false)

	press := func(r rune) { m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}) }
	steps := []struct {
		name string
		do   func()
	}{
		{"start", func() { m.startSelection() }},
		{"down", func() { press('j') }},
		{"down", func() { press('j') }},
		{"up", func() { press('k') }},
		{"reverse past the anchor", func() { press('k') }},
		{"up again", func() { press('k') }},
		{"bottom", func() { press('G') }},
		{"top", func() { press('g') }},
		{"next file", func() { press(']') }},
		{"next paragraph", func() { press('}') }},
		{"clear", func() { m.clearSelection() }},
	}
	for _, step := range steps {
		step.do()
		assertPatchMatchesReflow(t, &m, step.name)
	}
}

func TestSelectedTextIsVerbatim(t *testing.T) {
	m := NewDetailModel(fixtureDetailWithTabs(), &jumplist{})
	m.SetSize(200, 30)

	// Land on the first diff line, then take the three of them.
	fileStart := m.fileRanges[0].StartLine
	m.setCursor(fileStart + 2) // file header, hunk header, then content
	m.startSelection()
	m.moveCursor(1)
	m.moveCursor(1)

	want := " \tkept\n-\tremoved\n+\tadded\n"
	if got := m.SelectedText(); got != want {
		t.Errorf("SelectedText() = %q, want %q", got, want)
	}
}

func TestSelectedTextIsEmptyWithoutASelection(t *testing.T) {
	m := NewDetailModel(fixtureDetailWithFiles(1, 3), &jumplist{})
	m.SetSize(80, 20)
	if got := m.SelectedText(); got != "" {
		t.Errorf("SelectedText() = %q with no selection, want empty", got)
	}
}

// fixtureDetailWithTabs is a one-file commit whose diff lines are tab-indented,
// for asserting that a copy keeps real tabs rather than the spaces the
// renderer expands them into.
func fixtureDetailWithTabs() *git.CommitDetail {
	return &git.CommitDetail{
		Commit: git.Commit{Hash: "abc123", Subject: "subject"},
		Files: []git.File{{
			Path:   "tabbed.go",
			Status: git.StatusModified,
			Hunks: []git.Hunk{{
				Header: "@@ -1,2 +1,2 @@",
				Lines: []git.DiffLine{
					{Op: git.OpContext, Text: "\tkept"},
					{Op: git.OpDelete, Text: "\tremoved"},
					{Op: git.OpAdd, Text: "\tadded"},
				},
			}},
		}},
	}
}

// TestResizeDoesNotResurrectAClippedAnchor pins the anchor's half of reflow's
// re-clamp. A resize that shrinks the content used to leave the anchor
// pointing past the end, where reading it clipped it back into range — until
// a later resize made the content that long again and the selection silently
// grew, with no key pressed.
func TestResizeDoesNotResurrectAClippedAnchor(t *testing.T) {
	m := NewDetailModel(fixtureDetailWithLongLines(), &jumplist{})
	m.SetSize(30, 10) // narrow, so lines wrap and the content is long

	m.setCursor(m.contentLen - 1)
	m.startSelection()
	m.moveCursor(-3)
	if n := m.selectedLineCount(); n < 2 {
		t.Fatalf("fixture selected %d lines, want a multi-line selection to shrink", n)
	}

	m.SetSize(200, 10) // wide: the content gets much shorter
	shrunk := m.selectedLineCount()
	if m.selectAnchor >= m.contentLen {
		t.Errorf("anchor = %d past contentLen = %d, want it clamped in range", m.selectAnchor, m.contentLen)
	}

	m.SetSize(30, 10) // back to where it started
	if got := m.selectedLineCount(); got != shrunk {
		t.Errorf("selection grew to %d lines across a resize round trip, want it to stay at %d", got, shrunk)
	}
}
