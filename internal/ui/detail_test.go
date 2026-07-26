package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/maiwald/gitl/internal/git"
)

// fixtureDetailWithFiles builds a CommitDetail with nFiles files, each
// containing a single hunk with linesPerFile context lines, so diff length
// per file is precisely controlled for deterministic scroll-sync tests.
func fixtureDetailWithFiles(nFiles, linesPerFile int) *git.CommitDetail {
	files := make([]git.File, nFiles)
	for i := 0; i < nFiles; i++ {
		lines := make([]git.DiffLine, linesPerFile)
		for j := 0; j < linesPerFile; j++ {
			lines[j] = git.DiffLine{Op: git.OpContext, Text: fmt.Sprintf("line %d", j)}
		}
		files[i] = git.File{
			Path:   fmt.Sprintf("file%d.txt", i),
			Status: git.StatusModified,
			Hunks: []git.Hunk{
				{Header: "@@ -1 +1 @@", Lines: lines},
			},
		}
	}
	return &git.CommitDetail{
		Commit: git.Commit{
			Hash:       "abc123",
			AuthorName: "Someone",
			AuthorDate: time.Now(),
			Subject:    "subject",
		},
		Files: files,
	}
}

// TestDetailModelMessageScrollsWithDiff pins down that the commit
// message/header lives inside the scrollable diff content (rather than a
// fixed row above it), and that the file list points at the first file
// while still scrolled above it.
func TestDetailModelMessageScrollsWithDiff(t *testing.T) {
	d := fixtureDetailWithFiles(2, 20)
	d.Body = "message-scroll-marker"
	m := NewDetailModel(d)
	m.SetSize(80, 8)

	if !strings.Contains(m.leftVP.View(), "message-scroll-marker") {
		t.Fatalf("leftVP content = %q, want it to contain the commit message", m.leftVP.View())
	}
	if m.fileRanges[0].StartLine == 0 {
		t.Fatalf("fileRanges[0].StartLine = 0, want > 0 (message should precede the diff content)")
	}

	// Above the first file (still within the header/message), the first
	// file is treated as current since that's where scrolling down lands.
	if m.currentFile != 0 || m.files.highlightIndex != 0 {
		t.Fatalf("currentFile=%d highlightIndex=%d while above first file, want both 0", m.currentFile, m.files.highlightIndex)
	}

	// Scroll all the way to the bottom, into the last file's content.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	lastFile := len(m.fileRanges) - 1
	if m.currentFile != lastFile || m.files.highlightIndex != lastFile {
		t.Fatalf("currentFile=%d highlightIndex=%d at bottom, want both %d", m.currentFile, m.files.highlightIndex, lastFile)
	}
}

// fixtureDetailForSearch builds a CommitDetail with two files whose diff
// content is fully under test control, plus a commit message containing a
// marker that must never be treated as diff content.
func fixtureDetailForSearch() *git.CommitDetail {
	return &git.CommitDetail{
		Commit: git.Commit{
			Hash:       "abc123",
			AuthorName: "Someone",
			AuthorDate: time.Now(),
			Subject:    "subject with MARKER in it",
		},
		Body: "message body also has MARKER in it",
		Files: []git.File{
			{
				Path:   "a.txt",
				Status: git.StatusModified,
				Hunks: []git.Hunk{{
					Header: "@@ -1,2 +1,2 @@",
					Lines: []git.DiffLine{
						{Op: git.OpContext, Text: "nothing here"},
						{Op: git.OpAdd, Text: "needle in a.txt"},
					},
				}},
			},
			{
				Path:   "b.txt",
				Status: git.StatusModified,
				Hunks: []git.Hunk{{
					Header: "@@ -1,11 +1,11 @@",
					Lines: append(
						[]git.DiffLine{{Op: git.OpDelete, Text: "another needle in b.txt"}},
						fillerLines(10)...,
					),
				}},
			},
		},
	}
}

// fillerLines pads content well past any viewport height used in these
// tests, so a match near the top of a later file isn't clamped to the same
// scroll position as an earlier one purely because the whole diff already
// fits on screen.
func fillerLines(n int) []git.DiffLine {
	lines := make([]git.DiffLine, n)
	for i := range lines {
		lines[i] = git.DiffLine{Op: git.OpContext, Text: fmt.Sprintf("filler %d", i)}
	}
	return lines
}

func TestDetailModelSearchFindsAndJumpsToMatch(t *testing.T) {
	m := NewDetailModel(fixtureDetailForSearch())
	m.SetSize(80, 4)

	total := m.Search("needle")
	if total != 2 {
		t.Fatalf("Search returned %d matches, want 2", total)
	}
	if m.leftVP.YOffset != m.matchLines[0] {
		t.Fatalf("YOffset = %d after Search, want %d (first match's line)", m.leftVP.YOffset, m.matchLines[0])
	}
	if m.currentFile != 0 {
		t.Fatalf("currentFile = %d after jumping to the first match, want 0 (a.txt)", m.currentFile)
	}
}

func TestDetailModelSearchIgnoresHeaderAndMessageText(t *testing.T) {
	m := NewDetailModel(fixtureDetailForSearch())
	m.SetSize(80, 4)

	total := m.Search("MARKER")
	if total != 0 {
		t.Fatalf("Search(\"MARKER\") returned %d matches, want 0 (MARKER only appears in the subject/body, not the diff)", total)
	}
}

func TestDetailModelNextPrevMatchCyclesAcrossFilesAndWraps(t *testing.T) {
	m := NewDetailModel(fixtureDetailForSearch())
	m.SetSize(80, 4)
	m.Search("needle")

	pos, total := m.NextMatch()
	if pos != 2 || total != 2 {
		t.Fatalf("NextMatch = (%d, %d), want (2, 2)", pos, total)
	}
	if m.leftVP.YOffset != m.matchLines[1] {
		t.Fatalf("YOffset = %d after NextMatch, want %d (second match's line)", m.leftVP.YOffset, m.matchLines[1])
	}
	if m.currentFile != 1 {
		t.Fatalf("currentFile = %d after jumping to the second match, want 1 (b.txt)", m.currentFile)
	}

	pos, total = m.NextMatch()
	if pos != 1 || total != 2 {
		t.Fatalf("NextMatch past the last match = (%d, %d), want wrap to (1, 2)", pos, total)
	}
	if m.currentFile != 0 {
		t.Fatalf("currentFile = %d after wrapping to the first match, want 0", m.currentFile)
	}

	pos, _ = m.PrevMatch()
	if pos != 2 {
		t.Fatalf("PrevMatch before the first match = %d, want wrap to 2", pos)
	}
}

func TestDetailModelClearSearchRemovesHighlightState(t *testing.T) {
	m := NewDetailModel(fixtureDetailForSearch())
	m.SetSize(80, 4)
	m.Search("needle")

	m.ClearSearch()
	if term, pos, total, active := m.SearchStatus(); active || term != "" || pos != 0 || total != 0 {
		t.Fatalf("SearchStatus after ClearSearch = (%q, %d, %d, %v), want inactive/zero", term, pos, total, active)
	}
	if _, total := m.NextMatch(); total != 0 {
		t.Fatalf("NextMatch after ClearSearch should be a no-op, total = %d", total)
	}
}

func TestDetailModelHighlightFollowsScroll(t *testing.T) {
	m := NewDetailModel(fixtureDetailWithFiles(3, 10))
	m.SetSize(80, 8) // small viewport so scrolling moves meaningfully

	if m.currentFile != 0 {
		t.Fatalf("initial currentFile = %d, want 0", m.currentFile)
	}
	if m.files.highlightIndex != 0 {
		t.Fatalf("initial file list highlight = %d, want 0", m.files.highlightIndex)
	}

	// file 0 occupies fileRanges[0]; scroll past its end into file 1.
	targetOffset := m.fileRanges[0].EndLine
	for m.leftVP.YOffset < targetOffset {
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}

	if m.currentFile != 1 {
		t.Fatalf("currentFile = %d after scrolling into file 1, want 1", m.currentFile)
	}
	if m.files.highlightIndex != 1 {
		t.Fatalf("file list highlight = %d after scrolling into file 1, want 1", m.files.highlightIndex)
	}
}

func TestDetailModelTabTogglesFocusAndFileListNavigation(t *testing.T) {
	m := NewDetailModel(fixtureDetailWithFiles(3, 10))
	m.SetSize(80, 8)

	if m.focus != focusDiff {
		t.Fatalf("initial focus = %v, want focusDiff", m.focus)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.focus != focusFiles {
		t.Fatalf("focus after tab = %v, want focusFiles", m.focus)
	}

	// While focused on files, down moves the highlight without scrolling
	// the diff panel.
	yOffsetBefore := m.leftVP.YOffset
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.currentFile != 1 {
		t.Fatalf("currentFile after down in file list = %d, want 1", m.currentFile)
	}
	if m.files.highlightIndex != 1 {
		t.Fatalf("file list highlight = %d, want 1", m.files.highlightIndex)
	}
	if m.leftVP.YOffset != yOffsetBefore {
		t.Fatalf("diff YOffset changed to %d before enter, want unchanged %d", m.leftVP.YOffset, yOffsetBefore)
	}

	// Enter jumps the diff panel to the selected file's start.
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.leftVP.YOffset != m.fileRanges[1].StartLine {
		t.Fatalf("diff YOffset after enter = %d, want %d", m.leftVP.YOffset, m.fileRanges[1].StartLine)
	}

	// Tab back to the diff panel; down should scroll the diff again.
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.focus != focusDiff {
		t.Fatalf("focus after second tab = %v, want focusDiff", m.focus)
	}
	yOffsetBeforeScroll := m.leftVP.YOffset
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.leftVP.YOffset != yOffsetBeforeScroll+1 {
		t.Fatalf("diff YOffset after down in diff focus = %d, want %d", m.leftVP.YOffset, yOffsetBeforeScroll+1)
	}
}

func TestDetailModelTabNoopsWithoutFiles(t *testing.T) {
	m := NewDetailModel(&git.CommitDetail{
		Commit: git.Commit{Hash: "abc", AuthorName: "A", AuthorDate: time.Now(), Subject: "s"},
	})
	m.SetSize(80, 8)

	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.focus != focusDiff {
		t.Fatalf("focus = %v after tab with no files, want focusDiff (no-op)", m.focus)
	}
}

func TestDetailModelMergeCommitShowsNotice(t *testing.T) {
	m := NewDetailModel(&git.CommitDetail{
		Commit: git.Commit{
			Hash:       "merge123",
			Parents:    []string{"p1", "p2"},
			AuthorName: "A",
			AuthorDate: time.Now(),
			Subject:    "merge",
		},
	})
	m.SetSize(80, 24)

	if !strings.Contains(m.View(), "Merge commit") {
		t.Errorf("View() = %q, want it to mention the merge-commit limitation", m.View())
	}
}

func TestDetailModelEmptyCommitShowsNoChangesNotice(t *testing.T) {
	m := NewDetailModel(&git.CommitDetail{
		Commit: git.Commit{Hash: "empty123", AuthorName: "A", AuthorDate: time.Now(), Subject: "empty"},
	})
	m.SetSize(80, 24)

	if !strings.Contains(m.View(), "No changes") {
		t.Errorf("View() = %q, want it to say there are no changes", m.View())
	}
}

func TestDetailModelReflowClampsWidthsOnNarrowTerminal(t *testing.T) {
	m := NewDetailModel(fixtureDetailWithFiles(2, 5))
	m.SetSize(15, 24) // narrower than minFileListWidth + minDiffWidth

	if m.leftVP.Width < minDiffWidth {
		t.Errorf("diff panel width = %d, want at least %d even on a narrow terminal", m.leftVP.Width, minDiffWidth)
	}
	if m.leftVP.Width < 0 {
		t.Errorf("diff panel width = %d, must not be negative", m.leftVP.Width)
	}
}
