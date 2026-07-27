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

// TestDetailModelShowsStatsBlockBeforeDiff pins down that the stats summary
// (statLinesFor, statsview.go) is spliced in between the header/message and
// the diff content, and that fileRanges is shifted past both the header and
// the stats block (not just the header, as it was before the stats block
// existed).
func TestDetailModelShowsStatsBlockBeforeDiff(t *testing.T) {
	d := fixtureDetailWithFiles(2, 5)
	d.Files[0].Additions = 3
	d.Files[0].Deletions = 1
	d.Files[1].Additions = 10
	m := NewDetailModel(d)
	m.SetSize(80, 30) // tall enough that the stats block isn't scrolled out of view

	view := m.leftVP.View()
	if !strings.Contains(view, "2 files changed") {
		t.Fatalf("leftVP content = %q, want it to contain the stats totals line", view)
	}
	if !strings.Contains(view, "file0.txt") {
		t.Fatalf("leftVP content = %q, want it to contain the per-file stats line", view)
	}

	if m.fileRanges[0].StartLine == 0 {
		t.Fatalf("fileRanges[0].StartLine = 0, want > 0 (header + stats block should precede the diff content)")
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

	total := m.RunSearch("needle")
	if total != 2 {
		t.Fatalf("RunSearch returned %d matches, want 2", total)
	}
	if m.leftVP.YOffset != m.diffMatchLines[0] {
		t.Fatalf("YOffset = %d after RunSearch, want %d (first match's line)", m.leftVP.YOffset, m.diffMatchLines[0])
	}
	if m.currentFile != 0 {
		t.Fatalf("currentFile = %d after jumping to the first match, want 0 (a.txt)", m.currentFile)
	}
}

// TestDetailModelSearchFindsMessageMatchButIgnoresMetadata pins down the
// boundary of what "message" search covers: the body (here, "MARKER"), but
// not the hash/parent/date metadata lines — matching exactly what
// internal/git.SearchCommitHashes matches for list search (author name OR
// message).
func TestDetailModelSearchFindsMessageMatchButIgnoresMetadata(t *testing.T) {
	m := NewDetailModel(fixtureDetailForSearch())
	m.SetSize(80, 4)

	total := m.RunSearch("MARKER")
	if total != 1 {
		t.Fatalf("RunSearch(\"MARKER\") returned %d matches, want 1 (the body match; the subject field itself isn't scanned)", total)
	}
	// The sole match is also the current (n/N) match, so it renders with
	// searchActiveMatchStyle rather than the plain searchMatchStyle.
	if !strings.Contains(m.leftVP.View(), searchActiveMatchStyle.Render("MARKER")) {
		t.Fatalf("leftVP content = %q, want the message match visibly highlighted", m.leftVP.View())
	}

	total = m.RunSearch(m.detail.Hash)
	if total != 0 {
		t.Fatalf("RunSearch(hash) returned %d matches, want 0 (the commit-hash metadata line isn't searched)", total)
	}
}

func TestFindMessageMatchesScansAuthorAndBodyOnly(t *testing.T) {
	lines, authorLine, bodyStart, _ := headerLinesFor(&git.CommitDetail{
		Commit: git.Commit{
			Hash:        "deadbeef",
			AuthorName:  "Ada Lovelace",
			AuthorEmail: "ada@example.com",
			AuthorDate:  time.Now(),
		},
		Body: "first line\nsecond dual line",
	})

	if matches := FindMessageMatches(lines, authorLine, bodyStart, "dual"); len(matches) != 1 || matches[0].LineIndex != bodyStart+1 {
		t.Fatalf("matches = %+v, want exactly one match on body line %d", matches, bodyStart+1)
	}
	if matches := FindMessageMatches(lines, authorLine, bodyStart, "Lovelace"); len(matches) != 1 || matches[0].LineIndex != authorLine {
		t.Fatalf("matches = %+v, want exactly one match on the author line %d", matches, authorLine)
	}
	if matches := FindMessageMatches(lines, authorLine, bodyStart, "deadbeef"); len(matches) != 0 {
		t.Fatalf("matches = %+v, want none (the hash line isn't searched)", matches)
	}
	if matches := FindMessageMatches(lines, authorLine, bodyStart, ""); matches != nil {
		t.Fatalf("matches = %+v, want nil for an empty term", matches)
	}
}

// TestHeaderLinesForColorsHashAuthorAndDate pins down which byte ranges
// headerLinesFor marks for base coloring: the hash (not the "commit "
// label), the author's name (not the email), and the date value (not the
// "Date:   " label) — the same substrings commititem.go colors in the list
// view, kept consistent in the detail header.
func TestHeaderLinesForColorsHashAuthorAndDate(t *testing.T) {
	d := &git.CommitDetail{
		Commit: git.Commit{
			Hash:        "deadbeef",
			AuthorName:  "Ada Lovelace",
			AuthorEmail: "ada@example.com",
			AuthorDate:  time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC),
		},
	}
	lines, authorLine, _, spans := headerLinesFor(d)

	hashSpans := spans[0]
	if len(hashSpans) != 1 || lines[0][hashSpans[0].start:hashSpans[0].end] != d.Hash {
		t.Fatalf("spans[0] = %+v on line %q, want a single span covering %q", hashSpans, lines[0], d.Hash)
	}
	if hashSpans[0].style.GetForeground() != shaStyle.GetForeground() {
		t.Errorf("hash span style = %v, want shaStyle", hashSpans[0].style)
	}

	authorSpans := spans[authorLine]
	if len(authorSpans) != 1 || lines[authorLine][authorSpans[0].start:authorSpans[0].end] != d.AuthorName {
		t.Fatalf("spans[authorLine] = %+v on line %q, want a single span covering %q", authorSpans, lines[authorLine], d.AuthorName)
	}
	if authorSpans[0].style.GetForeground() != authorStyle.GetForeground() {
		t.Errorf("author span style = %v, want authorStyle", authorSpans[0].style)
	}

	dateLine := authorLine + 1
	dateText := d.AuthorDate.Format("2006-01-02 15:04:05 -0700")
	dateSpans := spans[dateLine]
	if len(dateSpans) != 1 || lines[dateLine][dateSpans[0].start:dateSpans[0].end] != dateText {
		t.Fatalf("spans[dateLine] = %+v on line %q, want a single span covering %q", dateSpans, lines[dateLine], dateText)
	}
	if dateSpans[0].style.GetForeground() != dateStyle.GetForeground() {
		t.Errorf("date span style = %v, want dateStyle", dateSpans[0].style)
	}
}

// TestHeaderLinesForColorsMergeParents pins down that every parent hash on
// a merge commit's "Merge:  <hash> <hash>" line gets its own shaStyle span,
// matching the primary commit hash's coloring.
func TestHeaderLinesForColorsMergeParents(t *testing.T) {
	d := &git.CommitDetail{
		Commit: git.Commit{
			Hash:       "abc123",
			Parents:    []string{"parent1", "parent2"},
			AuthorDate: time.Now(),
		},
	}
	lines, _, _, spans := headerLinesFor(d)

	mergeSpans := spans[1]
	if len(mergeSpans) != 2 {
		t.Fatalf("spans[1] = %+v, want 2 spans (one per parent)", mergeSpans)
	}
	for i, want := range d.Parents {
		if got := lines[1][mergeSpans[i].start:mergeSpans[i].end]; got != want {
			t.Errorf("mergeSpans[%d] covers %q, want %q", i, got, want)
		}
		if mergeSpans[i].style.GetForeground() != shaStyle.GetForeground() {
			t.Errorf("mergeSpans[%d] style = %v, want shaStyle", i, mergeSpans[i].style)
		}
	}
}

// TestHeaderLinesForIndentsBody pins down that the message body is
// indented 4 spaces, matching real `git show`'s own convention, while
// metadata lines (commit/Author/Date) stay flush left.
func TestHeaderLinesForIndentsBody(t *testing.T) {
	lines, _, bodyStart, _ := headerLinesFor(&git.CommitDetail{
		Commit: git.Commit{AuthorDate: time.Now()},
		Body:   "subject\n\nbody line",
	})

	for i, want := range []string{"    subject", "    ", "    body line"} {
		if got := lines[bodyStart+i]; got != want {
			t.Errorf("body line %d = %q, want %q", i, got, want)
		}
	}
	if strings.HasPrefix(lines[0], " ") {
		t.Errorf("commit line = %q, want it to stay flush left (not indented)", lines[0])
	}
}

// TestRenderLineWithBaseSpansMatchWinsOnOverlap pins down the composite
// renderer's priority rule: a search match inside a colored base span
// (e.g. a hit inside the author's name) shows the match style, not the
// base color, and text outside both keeps its own base style.
func TestRenderLineWithBaseSpansMatchWinsOnOverlap(t *testing.T) {
	text := "Author: Ada Lovelace <ada@example.com>"
	nameStart := strings.Index(text, "Ada Lovelace")
	nameEnd := nameStart + len("Ada Lovelace")
	spans := []baseSpan{{start: nameStart, end: nameEnd, style: authorStyle}}

	matchStart := strings.Index(text, "Lovelace")
	matchEnd := matchStart + len("Lovelace")
	matches := []lineMatch{{start: matchStart, end: matchEnd, active: true}}

	got := renderLineWithBaseSpans(text, spans, matches)

	wantMatch := searchActiveMatchStyle.Render("Lovelace")
	if !strings.Contains(got, wantMatch) {
		t.Errorf("got %q, want the overlapping match styled with searchActiveMatchStyle, not authorStyle", got)
	}
	wantBase := authorStyle.Render("Ada ")
	if !strings.Contains(got, wantBase) {
		t.Errorf("got %q, want the non-matched part of the name (%q) still styled with authorStyle", got, "Ada ")
	}
	if strings.Contains(got, authorStyle.Render("Lovelace")) {
		t.Errorf("got %q, the matched span must not also carry authorStyle", got)
	}
}

// TestDetailModelHeaderColorsAndIndentationRenderInView is an end-to-end
// check that headerLinesFor's spans and body indentation actually reach
// the rendered viewport content, not just the intermediate data.
func TestDetailModelHeaderColorsAndIndentationRenderInView(t *testing.T) {
	d := fixtureDetailWithFiles(1, 3)
	d.AuthorName = "Ada Lovelace"
	d.Body = "subject line"
	m := NewDetailModel(d)
	m.SetSize(80, 30)

	view := m.leftVP.View()
	if !strings.Contains(view, shaStyle.Render(d.Hash)) {
		t.Errorf("view does not contain the hash styled with shaStyle")
	}
	if !strings.Contains(view, authorStyle.Render(d.AuthorName)) {
		t.Errorf("view does not contain the author name styled with authorStyle")
	}
	if !strings.Contains(view, "    subject line") {
		t.Errorf("view does not contain the 4-space-indented message body")
	}
}

// TestDetailModelRunSearchOrdersMessageMatchesBeforeDiffMatches pins down
// the combined n/N traversal order: message matches (document order) come
// before diff matches (file/hunk/line order), so n/N sweeps a commit the
// way you'd read it top to bottom.
func TestDetailModelRunSearchOrdersMessageMatchesBeforeDiffMatches(t *testing.T) {
	d := &git.CommitDetail{
		Commit: git.Commit{
			Hash:       "abc123",
			AuthorName: "Someone",
			AuthorDate: time.Now(),
			Subject:    "subject",
		},
		Body: "message body mentions dual right here",
		Files: []git.File{{
			Path:   "a.txt",
			Status: git.StatusModified,
			Hunks: []git.Hunk{{
				Header: "@@ -1 +1 @@",
				Lines: append(
					[]git.DiffLine{{Op: git.OpAdd, Text: "diff also mentions dual here"}},
					fillerLines(10)...,
				),
			}},
		}},
	}
	m := NewDetailModel(d)
	// A small viewport (well under the total content length, padded by
	// fillerLines) so SetYOffset actually moves rather than clamping to 0
	// because everything already fits on screen.
	m.SetSize(80, 4)

	total := m.RunSearch("dual")
	if total != 2 {
		t.Fatalf("RunSearch returned %d matches, want 2 (one message, one diff)", total)
	}
	if len(m.messageMatches) != 1 || len(m.diffMatches) != 1 {
		t.Fatalf("messageMatches=%d diffMatches=%d, want 1 and 1", len(m.messageMatches), len(m.diffMatches))
	}
	if m.leftVP.YOffset != m.messageMatchLines[0] {
		t.Fatalf("YOffset after RunSearch = %d, want %d (the message match, visited first)", m.leftVP.YOffset, m.messageMatchLines[0])
	}
	if m.fileRanges[0].StartLine <= m.leftVP.YOffset {
		t.Fatalf("expected the message match's line (%d) to precede the first file's content (starts at %d)", m.leftVP.YOffset, m.fileRanges[0].StartLine)
	}

	pos, total := m.NextMatch()
	if pos != 2 || total != 2 {
		t.Fatalf("NextMatch = (%d, %d), want (2, 2)", pos, total)
	}
	if m.leftVP.YOffset != m.diffMatchLines[0] {
		t.Fatalf("YOffset after NextMatch = %d, want %d (the diff match)", m.leftVP.YOffset, m.diffMatchLines[0])
	}
}

func TestDetailModelNextPrevMatchCyclesAcrossFilesAndWraps(t *testing.T) {
	m := NewDetailModel(fixtureDetailForSearch())
	m.SetSize(80, 4)
	m.RunSearch("needle")

	pos, total := m.NextMatch()
	if pos != 2 || total != 2 {
		t.Fatalf("NextMatch = (%d, %d), want (2, 2)", pos, total)
	}
	if m.leftVP.YOffset != m.diffMatchLines[1] {
		t.Fatalf("YOffset = %d after NextMatch, want %d (second match's line)", m.leftVP.YOffset, m.diffMatchLines[1])
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
	m.RunSearch("needle")

	m.ClearSearch()
	if pos, total := m.MatchPosition(); pos != 0 || total != 0 {
		t.Fatalf("MatchPosition after ClearSearch = (%d, %d), want (0, 0)", pos, total)
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
