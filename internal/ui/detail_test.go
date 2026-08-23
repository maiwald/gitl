package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/maiwald/gitl/internal/git"
)

// runDetailSearch submits term the way the root Model does when "/" is
// entered from inside an open commit — install the term (which computes the
// matches, see SetSearchDisplay) and jump to the first one — and reports how
// many matches it found.
func runDetailSearch(m *DetailModel, term string) int {
	m.SetSearchDisplay(term, false)
	m.jumpToFirstMatch()
	return m.totalMatches()
}

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

// fileSectionIndex returns the index into sections of the file section for
// fileIndex, or -1 if not found. Tests use this instead of hardcoding a
// section index, since the file sections' position shifts depending on
// whether a stats block precedes them (see DetailModel.sections).
func fileSectionIndex(sections []detailSection, fileIndex int) int {
	for i, s := range sections {
		if s.kind == sectionKindFile && s.fileIndex == fileIndex {
			return i
		}
	}
	return -1
}

// TestDetailModelMessageScrollsWithDiff pins down that the commit
// message/header lives inside the scrollable diff content (rather than a
// fixed row above it), and that currentSection points at the commit section
// while still scrolled above the diff.
func TestDetailModelMessageScrollsWithDiff(t *testing.T) {
	d := fixtureDetailWithFiles(2, 20)
	d.Body = "message-scroll-marker"
	m := NewDetailModel(d, &jumplist{})
	m.SetSize(80, 8)

	if !strings.Contains(m.leftVP.View(), "message-scroll-marker") {
		t.Fatalf("leftVP content = %q, want it to contain the commit message", m.leftVP.View())
	}
	if m.fileRanges[0].StartLine == 0 {
		t.Fatalf("fileRanges[0].StartLine = 0, want > 0 (message should precede the diff content)")
	}

	// At the top, still within the header/message, the current section is
	// the commit metadata section itself.
	if m.currentSection != 0 || m.sections[0].kind != sectionKindCommit {
		t.Fatalf("currentSection=%d kind=%v at top, want section 0 (commit)", m.currentSection, m.sections[m.currentSection].kind)
	}

	// Scroll all the way to the bottom, into the last file's content.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	lastSection := len(m.sections) - 1
	if m.currentSection != lastSection || m.sections[lastSection].kind != sectionKindFile {
		t.Fatalf("currentSection=%d at bottom, want last section (%d), a file", m.currentSection, lastSection)
	}
}

// TestDetailModelShowsStatsBlockBeforeDiff pins down that the stats summary
// (statLinesFor, statsview.go) is spliced in between the header/message and
// the diff content, and that fileRanges is shifted past both blocks.
func TestDetailModelShowsStatsBlockBeforeDiff(t *testing.T) {
	d := fixtureDetailWithFiles(2, 5)
	d.Files[0].Additions = 3
	d.Files[0].Deletions = 1
	d.Files[1].Additions = 10
	m := NewDetailModel(d, &jumplist{})
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
	m := NewDetailModel(fixtureDetailForSearch(), &jumplist{})
	m.SetSize(80, 4)

	total := runDetailSearch(&m, "needle")
	if total != 2 {
		t.Fatalf("detail search found %d matches, want 2", total)
	}
	if m.leftVP.YOffset != m.diffMatchLines[0] {
		t.Fatalf("YOffset = %d after the search, want %d (first match's line)", m.leftVP.YOffset, m.diffMatchLines[0])
	}
	if want := fileSectionIndex(m.sections, 0); m.currentSection != want {
		t.Fatalf("currentSection = %d after jumping to the first match, want %d (a.txt)", m.currentSection, want)
	}
}

// TestDetailModelSearchFindsMessageMatchButIgnoresMetadata pins down the
// boundary of what "message" search covers: the body (here, "MARKER"), but
// not the hash/parent/date metadata lines — matching exactly what
// internal/git.SearchCommitHashes matches for list search (author name OR
// message).
func TestDetailModelSearchFindsMessageMatchButIgnoresMetadata(t *testing.T) {
	m := NewDetailModel(fixtureDetailForSearch(), &jumplist{})
	m.SetSize(80, 4)

	total := runDetailSearch(&m, "MARKER")
	if total != 1 {
		t.Fatalf("search(\"MARKER\") found %d matches, want 1 (the body match; the subject field itself isn't scanned)", total)
	}
	// The sole match is also the current (n/N) match, so it renders with
	// searchActiveMatchStyle rather than the plain searchMatchStyle.
	if !strings.Contains(m.leftVP.View(), searchActiveMatchStyle.Render("MARKER")) {
		t.Fatalf("leftVP content = %q, want the message match visibly highlighted", m.leftVP.View())
	}

	total = runDetailSearch(&m, m.detail.Hash)
	if total != 0 {
		t.Fatalf("search(hash) found %d matches, want 0 (the commit-hash metadata line isn't searched)", total)
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
	dateText := d.AuthorDate.Local().Format("2006-01-02 15:04:05 -0700")
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

	got := renderLineWithBaseSpans(text, spans, matches, lipgloss.NewStyle(), false)

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
	m := NewDetailModel(d, &jumplist{})
	m.SetSize(80, 30)
	// The cursor defaults to line 0 ("Commit: <hash>"), and the Author line
	// sits right below it — either would otherwise merge the cursor
	// background into the span this test is checking. Move past both, onto
	// the blank line separating the metadata from the body; this test is
	// about base span coloring, not the cursor.
	for i := 0; i < 3; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}

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

// TestDetailModelSearchOrdersMessageMatchesBeforeDiffMatches pins down
// the combined n/N traversal order: message matches (document order) come
// before diff matches (file/hunk/line order), so n/N sweeps a commit the
// way you'd read it top to bottom.
func TestDetailModelSearchOrdersMessageMatchesBeforeDiffMatches(t *testing.T) {
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
	m := NewDetailModel(d, &jumplist{})
	// A small viewport (well under the total content length, padded by
	// fillerLines) so SetYOffset actually moves rather than clamping to 0
	// because everything already fits on screen.
	m.SetSize(80, 4)

	total := runDetailSearch(&m, "dual")
	if total != 2 {
		t.Fatalf("detail search found %d matches, want 2 (one message, one diff)", total)
	}
	if len(m.messageMatches) != 1 || len(m.diffMatches) != 1 {
		t.Fatalf("messageMatches=%d diffMatches=%d, want 1 and 1", len(m.messageMatches), len(m.diffMatches))
	}
	if m.leftVP.YOffset != m.messageMatchLines[0] {
		t.Fatalf("YOffset after the search = %d, want %d (the message match, visited first)", m.leftVP.YOffset, m.messageMatchLines[0])
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
	m := NewDetailModel(fixtureDetailForSearch(), &jumplist{})
	m.SetSize(80, 4)
	runDetailSearch(&m, "needle")

	pos, total := m.NextMatch()
	if pos != 2 || total != 2 {
		t.Fatalf("NextMatch = (%d, %d), want (2, 2)", pos, total)
	}
	if m.leftVP.YOffset != m.diffMatchLines[1] {
		t.Fatalf("YOffset = %d after NextMatch, want %d (second match's line)", m.leftVP.YOffset, m.diffMatchLines[1])
	}
	if want := fileSectionIndex(m.sections, 1); m.currentSection != want {
		t.Fatalf("currentSection = %d after jumping to the second match, want %d (b.txt)", m.currentSection, want)
	}

	pos, total = m.NextMatch()
	if pos != 1 || total != 2 {
		t.Fatalf("NextMatch past the last match = (%d, %d), want wrap to (1, 2)", pos, total)
	}
	if want := fileSectionIndex(m.sections, 0); m.currentSection != want {
		t.Fatalf("currentSection = %d after wrapping to the first match, want %d (a.txt)", m.currentSection, want)
	}

	pos, _ = m.PrevMatch()
	if pos != 2 {
		t.Fatalf("PrevMatch before the first match = %d, want wrap to 2", pos)
	}
}

// TestDetailModelEmptyTermRemovesHighlightState pins down that clearing the
// search is just the term going empty — there's no separate "clear" step for
// matches to be forgotten by.
func TestDetailModelEmptyTermRemovesHighlightState(t *testing.T) {
	m := NewDetailModel(fixtureDetailForSearch(), &jumplist{})
	m.SetSize(80, 4)
	runDetailSearch(&m, "needle")

	m.SetSearchDisplay("", false)
	if pos, total := m.MatchPosition(); pos != 0 || total != 0 {
		t.Fatalf("MatchPosition after clearing the term = (%d, %d), want (0, 0)", pos, total)
	}
	if _, total := m.NextMatch(); total != 0 {
		t.Fatalf("NextMatch after clearing the term should be a no-op, total = %d", total)
	}
}

// TestDetailModelMatchesFollowTermChange pins down that a match set never
// outlives the term it was computed for: the root Model pushes a new term
// down here even when some other view was the one the user searched from
// (see setSearchDisplay in app.go), and the matches must follow it rather
// than stay behind under the new term's label.
func TestDetailModelMatchesFollowTermChange(t *testing.T) {
	m := NewDetailModel(fixtureDetailForSearch(), &jumplist{})
	m.SetSize(80, 4)
	runDetailSearch(&m, "needle")

	m.SetSearchDisplay("MARKER", false)
	if _, total := m.MatchPosition(); total != 1 {
		t.Fatalf("total matches after the term changed = %d, want 1 (MARKER's, not needle's 2)", total)
	}
}

func TestDetailModelHighlightFollowsScroll(t *testing.T) {
	m := NewDetailModel(fixtureDetailWithFiles(3, 10), &jumplist{})
	m.SetSize(80, 8) // small viewport so scrolling moves meaningfully

	if m.currentSection != 0 || m.sections[0].kind != sectionKindCommit {
		t.Fatalf("initial currentSection = %d, want 0 (commit)", m.currentSection)
	}

	// file 0 occupies fileRanges[0]; scroll past its end into file 1.
	targetOffset := m.fileRanges[0].EndLine
	for m.leftVP.YOffset < targetOffset {
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}

	if want := fileSectionIndex(m.sections, 1); m.currentSection != want {
		t.Fatalf("currentSection = %d after scrolling into file 1, want %d", m.currentSection, want)
	}
}

// TestDetailModelStickyHeaderShowsSectionTitles pins down that the sticky
// row shows "Commit" while scrolled within the header/message, the stats
// block's own heading (statsSectionTitle) while scrolled within it, and
// each file's own header while scrolled within its diff — stepping through
// via jumpToSection the same way [ / ] does.
func TestDetailModelStickyHeaderShowsSectionTitles(t *testing.T) {
	m := NewDetailModel(fixtureDetailWithFiles(2, 20), &jumplist{})
	m.SetSize(80, 8)

	if !strings.Contains(m.View(), "Commit") {
		t.Fatalf("View() = %q, want sticky header to show \"Commit\" at the top", m.View())
	}

	m.jumpToSection(1)
	if !strings.Contains(m.View(), statsSectionTitle) {
		t.Fatalf("View() = %q, want sticky header to show %q over the stats block", m.View(), statsSectionTitle)
	}

	m.jumpToSection(1)
	if !strings.Contains(m.View(), "file0.txt") {
		t.Fatalf("View() = %q, want sticky header to show file0.txt", m.View())
	}

	m.jumpToSection(1)
	if !strings.Contains(m.View(), "file1.txt") {
		t.Fatalf("View() = %q, want sticky header to show file1.txt after jumping", m.View())
	}
}

// TestDetailModelStickyHeaderDoesNotDuplicateHeaderWhileScrolling pins down
// that the sticky row never duplicates a file's own header line. Scrolling one
// line at a time (unlike jumpToSection, which lands past the header via
// sectionContentStart) passes through the scroll position where a file's
// header is the topmost visible line, and the sticky row must keep showing the
// previous file there instead of repeating what's visible just below it.
func TestDetailModelStickyHeaderDoesNotDuplicateHeaderWhileScrolling(t *testing.T) {
	m := NewDetailModel(fixtureDetailWithFiles(2, 20), &jumplist{})
	m.SetSize(80, 8)

	target := m.fileRanges[1].StartLine
	for m.leftVP.YOffset < target {
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}

	contentTopLine := strings.SplitN(m.leftVP.View(), "\n", 2)[0]
	if !strings.Contains(contentTopLine, "file1.txt") {
		t.Fatalf("leftVP.View() top line = %q, want it to already show file1.txt's header", contentTopLine)
	}
	stickyLine := strings.SplitN(m.View(), "\n", 2)[0]
	if strings.Contains(stickyLine, "file1.txt") {
		t.Fatalf("sticky line = %q, want it to still show file0.txt (not duplicate the header line right below it)", stickyLine)
	}
	if !strings.Contains(stickyLine, "file0.txt") {
		t.Fatalf("sticky line = %q, want it to still show file0.txt", stickyLine)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	stickyLine = strings.SplitN(m.View(), "\n", 2)[0]
	if !strings.Contains(stickyLine, "file1.txt") {
		t.Fatalf("sticky line = %q, want sticky header to switch to file1.txt once its header line has scrolled out of view", stickyLine)
	}
}

// TestDetailModelNextPrevSectionJumpsAndClamps pins down [ / ] navigation:
// each key jumps straight to the adjacent section (commit metadata, the
// stats block, or a file's diff), clamping (not wrapping) at the first/last
// section.
func TestDetailModelNextPrevSectionJumpsAndClamps(t *testing.T) {
	m := NewDetailModel(fixtureDetailWithFiles(3, 10), &jumplist{})
	m.SetSize(80, 8)
	last := len(m.sections) - 1

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")})
	if m.currentSection != 1 || m.leftVP.YOffset != m.sections[1].startLine+1 {
		t.Fatalf("after ']': currentSection=%d YOffset=%d, want section 1 at %d", m.currentSection, m.leftVP.YOffset, m.sections[1].startLine+1)
	}

	for i := 0; i < last; i++ { // more than enough presses to clamp at the last section
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")})
	}
	if m.currentSection != last {
		t.Fatalf("currentSection = %d after repeated ']', want clamped at %d (last section)", m.currentSection, last)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[")})
	if m.currentSection != last-1 || m.leftVP.YOffset != m.sections[last-1].startLine+1 {
		t.Fatalf("after '[': currentSection=%d YOffset=%d, want section %d at %d", m.currentSection, m.leftVP.YOffset, last-1, m.sections[last-1].startLine+1)
	}

	for i := 0; i < last; i++ { // more than enough presses to clamp at the first section
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[")})
	}
	if m.currentSection != 0 {
		t.Fatalf("currentSection = %d after repeated '[', want clamped at 0", m.currentSection)
	}
}

// TestDetailModelEnterOnStatsRowJumpsToFile pins down that Enter on one of
// the "Files changed" stats block's per-file rows jumps straight to that
// file's diff section, the same landing point [ / ] use.
func TestDetailModelEnterOnStatsRowJumpsToFile(t *testing.T) {
	d := fixtureDetailWithFiles(3, 10)
	m := NewDetailModel(d, &jumplist{})
	m.SetSize(80, 8)

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")}) // land on the stats block, cursor on file 0's row
	if fi, ok := m.statsFileIndexAt(m.cursorLine); !ok || fi != 0 {
		t.Fatalf("statsFileIndexAt(%d) = (%d, %v), want (0, true) right after landing on the stats block", m.cursorLine, fi, ok)
	}

	m.moveCursor(1) // down to file 1's row
	if fi, ok := m.statsFileIndexAt(m.cursorLine); !ok || fi != 1 {
		t.Fatalf("statsFileIndexAt(%d) = (%d, %v), want (1, true) on file 1's row", m.cursorLine, fi, ok)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	target := fileSectionIndex(m.sections, 1)
	if m.currentSection != target || m.leftVP.YOffset != m.sections[target].startLine+1 {
		t.Fatalf("after Enter on file 1's stats row: currentSection=%d YOffset=%d, want section %d at %d", m.currentSection, m.leftVP.YOffset, target, m.sections[target].startLine+1)
	}
}

// TestDetailModelEnterElsewhereIsNoop pins down that Enter does nothing
// when the cursor isn't on one of the stats block's per-file rows: the
// stats heading row, the totals row, and a file's own header row inside
// the diff all leave currentSection/YOffset untouched.
func TestDetailModelEnterElsewhereIsNoop(t *testing.T) {
	m := NewDetailModel(fixtureDetailWithFiles(2, 10), &jumplist{})
	m.SetSize(80, 8)

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")}) // stats heading's file-0 row... move back up to the heading itself
	m.moveCursor(-1)
	if _, ok := m.statsFileIndexAt(m.cursorLine); ok {
		t.Fatalf("statsFileIndexAt(%d) = ok, want false on the stats heading row", m.cursorLine)
	}
	before := m.currentSection
	beforeOffset := m.leftVP.YOffset
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.currentSection != before || m.leftVP.YOffset != beforeOffset {
		t.Fatalf("Enter on the stats heading row moved the view: currentSection=%d YOffset=%d, want unchanged (%d, %d)", m.currentSection, m.leftVP.YOffset, before, beforeOffset)
	}

	m.jumpCursor(m.fileRanges[0].StartLine) // a file's own header row inside the diff (not a stats row)
	if _, ok := m.statsFileIndexAt(m.cursorLine); ok {
		t.Fatalf("statsFileIndexAt(%d) = ok, want false on a file's own header row inside the diff", m.cursorLine)
	}
	before = m.currentSection
	beforeOffset = m.leftVP.YOffset
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.currentSection != before || m.leftVP.YOffset != beforeOffset {
		t.Fatalf("Enter on a file's own header row moved the view: currentSection=%d YOffset=%d, want unchanged (%d, %d)", m.currentSection, m.leftVP.YOffset, before, beforeOffset)
	}
}

// TestDetailModelNextFileReachesShortTrailingFile pins down that ] can
// reach the last file's header even when that file's own content is too
// short to fill the remaining viewport on its own — viewport.SetYOffset
// clamps to the content's natural max scroll position, which without
// padding would leave the last file's StartLine unreachable as the
// top-of-viewport line, the same way scrolling a short final section to
// the top of the screen would otherwise be impossible.
func TestDetailModelNextFileReachesShortTrailingFile(t *testing.T) {
	d := fixtureDetailWithFiles(2, 30)
	d.Files[1].Hunks[0].Lines = d.Files[1].Hunks[0].Lines[:2] // short trailing file
	m := NewDetailModel(d, &jumplist{})
	m.SetSize(80, 20) // tall enough that the short file alone can't fill it

	target := fileSectionIndex(m.sections, 1)
	for i := 0; i < target; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")})
	}
	if m.currentSection != target || m.leftVP.YOffset != m.sections[target].startLine+1 {
		t.Fatalf("after %d ']' presses: currentSection=%d YOffset=%d, want section %d at %d", target, m.currentSection, m.leftVP.YOffset, target, m.sections[target].startLine+1)
	}
}

// TestDetailModelNextPrevFileNoopWithoutFiles pins down that [ / ] don't
// panic or move anything on a diff-less commit (merge commit / no diff),
// where the commit section is the only section there is.
func TestDetailModelNextPrevFileNoopWithoutFiles(t *testing.T) {
	m := NewDetailModel(&git.CommitDetail{
		Commit: git.Commit{
			Hash: "merge123", Parents: []string{"p1", "p2"},
			AuthorName: "A", AuthorDate: time.Now(), Subject: "merge",
		},
	}, &jumplist{})
	m.SetSize(80, 24)

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[")})
	if m.currentSection != 0 {
		t.Fatalf("currentSection = %d after [/] on a diff-less commit, want 0 (untouched)", m.currentSection)
	}
}

// TestDetailModelNoStickyHeaderWithoutFiles pins down that a merge/empty
// commit (no fileRanges) doesn't render a sticky row or lose a line of
// viewport height reserving one for nothing.
func TestDetailModelNoStickyHeaderWithoutFiles(t *testing.T) {
	m := NewDetailModel(&git.CommitDetail{
		Commit: git.Commit{Hash: "empty123", AuthorName: "A", AuthorDate: time.Now(), Subject: "empty"},
	}, &jumplist{})
	m.SetSize(80, 24)

	if m.leftVP.Height != 24 {
		t.Fatalf("leftVP.Height = %d, want 24 (no row reserved without files)", m.leftVP.Height)
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
	}, &jumplist{})
	m.SetSize(80, 24)

	if !strings.Contains(m.View(), "Merge commit") {
		t.Errorf("View() = %q, want it to mention the merge-commit limitation", m.View())
	}
}

func TestDetailModelEmptyCommitShowsNoChangesNotice(t *testing.T) {
	m := NewDetailModel(&git.CommitDetail{
		Commit: git.Commit{Hash: "empty123", AuthorName: "A", AuthorDate: time.Now(), Subject: "empty"},
	}, &jumplist{})
	m.SetSize(80, 24)

	if !strings.Contains(m.View(), "No changes") {
		t.Errorf("View() = %q, want it to say there are no changes", m.View())
	}
}

// fixturePseudoDetail builds a CommitDetail for a synthetic working-tree/
// staged entry the way git.EntryDetail does: Kind set, Commit/Body left
// zero, only Files populated.
func fixturePseudoDetail(kind git.EntryKind, files []git.File) *git.CommitDetail {
	return &git.CommitDetail{Kind: kind, Files: files}
}

// TestDetailModelPseudoEntryHeaderHasNoCommitMetadata pins down that a
// pseudo entry's header shows only its title and description — no
// Commit:/Author:/Date: lines, since there is no commit metadata to show —
// and that its sticky header (scrolled to the top) shows the entry title
// instead of the literal "Commit".
func TestDetailModelPseudoEntryHeaderHasNoCommitMetadata(t *testing.T) {
	m := NewDetailModel(fixturePseudoDetail(git.EntryWorktree, []git.File{
		{Path: "a.txt", Status: git.StatusModified, Additions: 1},
	}), &jumplist{})
	m.SetSize(80, 24)

	view := m.View()
	if !strings.Contains(view, "Working tree") {
		t.Fatalf("View() = %q, want it to contain the entry title \"Working tree\"", view)
	}
	for _, unwanted := range []string{"Commit:", "Author:", "Date:"} {
		if strings.Contains(view, unwanted) {
			t.Errorf("View() = %q, must not contain %q for a pseudo entry", view, unwanted)
		}
	}
}

// TestDetailModelPseudoEntryNoDiffNoticeIsKindSpecific pins down that an
// empty pseudo entry (nothing staged / nothing uncommitted) shows a message
// specific to its kind, distinct from a real commit's "No changes" notice.
func TestDetailModelPseudoEntryNoDiffNoticeIsKindSpecific(t *testing.T) {
	staged := NewDetailModel(fixturePseudoDetail(git.EntryStaged, nil), &jumplist{})
	staged.SetSize(80, 24)
	if !strings.Contains(staged.View(), "No staged changes") {
		t.Errorf("View() = %q, want \"No staged changes\"", staged.View())
	}

	worktree := NewDetailModel(fixturePseudoDetail(git.EntryWorktree, nil), &jumplist{})
	worktree.SetSize(80, 24)
	if !strings.Contains(worktree.View(), "No uncommitted changes") {
		t.Errorf("View() = %q, want \"No uncommitted changes\"", worktree.View())
	}
}

// TestDetailModelNoDiffNoticeNamesThePathspec pins down that a view opened
// with a CLI pathspec says so when it comes out empty, so "nothing here"
// can't be read as "nothing anywhere" — see StartTarget.Paths.
func TestDetailModelNoDiffNoticeNamesThePathspec(t *testing.T) {
	tests := []struct {
		name   string
		detail *git.CommitDetail
		want   string
	}{
		{
			"staged entry",
			&git.CommitDetail{Kind: git.EntryStaged, Paths: []string{"a.txt"}},
			"No staged changes in a.txt.",
		},
		{
			"worktree entry",
			&git.CommitDetail{Kind: git.EntryWorktree, Paths: []string{"a.txt", "b.txt"}},
			"No uncommitted changes in a.txt, b.txt.",
		},
		{
			"commit",
			&git.CommitDetail{Paths: []string{"a.txt"}},
			"No changes to a.txt in this commit.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewDetailModel(tt.detail, &jumplist{})
			m.SetSize(80, 24)
			if !strings.Contains(m.View(), tt.want) {
				t.Errorf("View() = %q, want it to contain %q", m.View(), tt.want)
			}
		})
	}
}

// TestDetailModelPseudoEntryMessageSearchFindsNothingButDiffSearchWorks pins
// down FindMessageMatches' authorLine=-1 guard (a pseudo entry has no
// author/message region to search) alongside diff search, which still works
// normally since it only depends on Files.
func TestDetailModelPseudoEntryMessageSearchFindsNothingButDiffSearchWorks(t *testing.T) {
	m := NewDetailModel(fixturePseudoDetail(git.EntryStaged, []git.File{
		{
			Path:   "a.txt",
			Status: git.StatusModified,
			Hunks: []git.Hunk{{
				Header: "@@ -1 +1 @@",
				Lines:  []git.DiffLine{{Op: git.OpAdd, Text: "needle here"}},
			}},
		},
	}), &jumplist{})
	m.SetSize(80, 24)

	// A term appearing in the title ("Staged changes" contains "Staged")
	// must not be found via message search, since a pseudo entry's title
	// isn't a searchable message region.
	if total := runDetailSearch(&m, "Staged"); total != 0 {
		t.Fatalf(`search("Staged") = %d, want 0 (the title isn't a searchable message region)`, total)
	}

	if total := runDetailSearch(&m, "needle"); total != 1 {
		t.Fatalf(`search("needle") = %d, want 1 (diff search still works)`, total)
	}
}
