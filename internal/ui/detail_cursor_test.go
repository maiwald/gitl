package ui

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/maiwald/gitl/internal/git"
)

// cursorFillOpen is the raw SGR escape prefix a cursor-background-filled
// span opens with (everything rendered before the styled content itself),
// derived from cursorRowBg rather than hardcoded, so tests that count its
// occurrences don't duplicate the color value as a magic string.
func cursorFillOpen() string {
	marker := lipgloss.NewStyle().Background(cursorRowBg).Render("X")
	return strings.SplitN(marker, "X", 2)[0]
}

// TestDetailModelCursorDefaultsToTopLine pins down that a freshly opened
// commit starts with the cursor on line 0 (the "Commit: <hash>" line),
// rendered with the cursor row's background merged into the hash's own
// shaStyle span — tig's own default of highlighting the topmost line.
func TestDetailModelCursorDefaultsToTopLine(t *testing.T) {
	d := fixtureDetailWithFiles(1, 3)
	m := NewDetailModel(d, &jumplist{})
	m.SetSize(80, 24)

	if m.cursorLine != 0 {
		t.Fatalf("cursorLine = %d, want 0", m.cursorLine)
	}
	want := shaStyle.Background(cursorRowBg).Render(d.Hash)
	if !strings.Contains(m.vp.View(), want) {
		t.Fatalf("view does not contain the hash styled with the cursor background merged in")
	}
}

// TestDetailModelCursorMovesWithJK pins down that j/k move cursorLine by
// one, and that the highlight moves off the previous line onto the new
// one — not left behind or duplicated.
func TestDetailModelCursorMovesWithJK(t *testing.T) {
	d := fixtureDetailWithFiles(1, 3)
	m := NewDetailModel(d, &jumplist{})
	m.SetSize(80, 24)

	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.cursorLine != 1 {
		t.Fatalf("cursorLine after Down = %d, want 1", m.cursorLine)
	}
	if strings.Contains(m.vp.View(), shaStyle.Background(cursorRowBg).Render(d.Hash)) {
		t.Fatal("hash line still shows the cursor background after moving off it")
	}
	if !strings.Contains(m.vp.View(), authorStyle.Background(cursorRowBg).Render(d.AuthorName)) {
		t.Fatal("author line does not show the cursor background after moving onto it")
	}

	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.cursorLine != 0 {
		t.Fatalf("cursorLine after Up = %d, want 0", m.cursorLine)
	}
}

// TestDetailModelCursorClampsAtBounds pins down that the cursor can't move
// above line 0 or past the last flattened content line.
func TestDetailModelCursorClampsAtBounds(t *testing.T) {
	d := fixtureDetailWithFiles(1, 2)
	m := NewDetailModel(d, &jumplist{})
	m.SetSize(80, 24)

	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.cursorLine != 0 {
		t.Fatalf("cursorLine after Up at top = %d, want clamped at 0", m.cursorLine)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	if m.cursorLine != m.contentLen-1 {
		t.Fatalf("cursorLine after G = %d, want %d (contentLen-1)", m.cursorLine, m.contentLen-1)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.cursorLine != m.contentLen-1 {
		t.Fatalf("cursorLine after Down past the end = %d, want still clamped at %d", m.cursorLine, m.contentLen-1)
	}
}

// TestDetailModelCursorScrollFollowsOnSmallViewport pins down that plain
// cursor movement scrolls the viewport by the minimum needed to keep the
// cursor visible, the DetailModel analogue of
// TestListModelScrollsToKeepCursorVisible.
func TestDetailModelCursorScrollFollowsOnSmallViewport(t *testing.T) {
	d := fixtureDetailWithFiles(2, 20)
	m := NewDetailModel(d, &jumplist{})
	m.SetSize(80, 5)

	for i := 0; i < 10; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.cursorLine != 10 {
		t.Fatalf("cursorLine = %d, want 10", m.cursorLine)
	}
	if m.vp.YOffset > m.cursorLine || m.cursorLine >= m.vp.YOffset+m.vp.Height {
		t.Fatalf("cursor %d not visible with YOffset=%d height=%d", m.cursorLine, m.vp.YOffset, m.vp.Height)
	}
}

// TestDetailModelHalfPageMovesCursor pins down that ctrl-d/ctrl-u move the
// cursor by half the viewport's height, the shared halfPage step the list
// view moves its own cursor by.
func TestDetailModelHalfPageMovesCursor(t *testing.T) {
	d := fixtureDetailWithFiles(2, 20)
	m := NewDetailModel(d, &jumplist{})
	// vp.Height is m.height minus 1 (the sticky file-header row), so
	// half page here is (10-1)/2 = 4, not simply half of 10.
	m.SetSize(80, 10)

	m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if m.cursorLine != 4 {
		t.Fatalf("cursorLine after ctrl+d = %d, want 4", m.cursorLine)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if m.cursorLine != 8 {
		t.Fatalf("cursorLine after 2x ctrl+d = %d, want 8", m.cursorLine)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	if m.cursorLine != 4 {
		t.Fatalf("cursorLine after ctrl+u = %d, want 4", m.cursorLine)
	}
}

// TestDetailModelCursorHighlightsDiffLine pins down that the cursor
// background reaches diff content too (not just the header block), merged
// into the line's own diff-op color (diffContextStyle here) the same way
// it merges into a header span.
func TestDetailModelCursorHighlightsDiffLine(t *testing.T) {
	d := fixtureDetailWithFiles(1, 3)
	m := NewDetailModel(d, &jumplist{})
	m.SetSize(80, 24)

	target := m.fileRanges[0].StartLine // the file header row
	for m.cursorLine < target {
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	if !strings.Contains(m.vp.View(), fileHeaderStyle.Background(cursorRowBg).Render("M file0.txt")) {
		t.Fatalf("view does not contain the file header styled with the cursor background merged in: %q", m.vp.View())
	}
}

// TestDetailModelCursorSurvivesReflowClampedInRange pins down that a
// resize which shrinks contentLen re-clamps an out-of-range cursorLine
// instead of leaving it dangling (see cursorLine's doc comment).
func TestDetailModelCursorSurvivesReflowClampedInRange(t *testing.T) {
	d := fixtureDetailWithFiles(1, 3)
	m := NewDetailModel(d, &jumplist{})
	m.SetSize(80, 24)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	before := m.cursorLine

	m.SetSize(10, 24) // much narrower: wrapping likely changes contentLen
	if m.cursorLine < 0 || m.cursorLine >= m.contentLen {
		t.Fatalf("cursorLine = %d out of range [0, %d) after resize (was %d before)", m.cursorLine, m.contentLen, before)
	}
}

// TestDetailModelBottomStaysWithinRealContentNotPadding pins down a
// regression: reflow pads content with trailing blank rows past the real
// diff (see reflow's doc comment on the append just after contentLen is
// captured) purely so jumpToSection can scroll a short trailing file's
// header to the very top of the viewport. G must land the cursor on the
// last *real* content line, not wander into that invisible padding —
// contentLen is captured before the padding is appended specifically so
// G (which targets contentLen-1) can't reach it.
func TestDetailModelBottomStaysWithinRealContentNotPadding(t *testing.T) {
	d := fixtureDetailWithFiles(2, 30)
	d.Files[1].Hunks[0].Lines = d.Files[1].Hunks[0].Lines[:2] // short trailing file, triggers padding
	m := NewDetailModel(d, &jumplist{})
	m.SetSize(80, 20) // tall enough that the short file alone can't fill it

	lastEnd := m.fileRanges[len(m.fileRanges)-1].EndLine
	if m.contentLen != lastEnd {
		t.Fatalf("contentLen = %d, want %d (the real content's end, excluding jumpToSection's trailing padding)", m.contentLen, lastEnd)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	if m.cursorLine != lastEnd-1 {
		t.Fatalf("cursorLine after G = %d, want %d (the last real content line, not the padding)", m.cursorLine, lastEnd-1)
	}
}

// TestDetailModelCursorHighlightFillsFullRowWidth pins down that the
// cursor background covers the whole row, not just the text — a
// regression check for a bug where the trailing padding a prior word-wrap
// pass added stayed plain/uncolored, since re-wrapping already-ANSI'd
// content in a further Render() call doesn't reliably recolor past an
// embedded reset (see renderDiffLine's doc comment in diffview.go). The
// cursor background's open code should appear at least twice on the
// line: once behind the short "Author: x"-style text, once again for the
// independently rendered trailing fill span.
func TestDetailModelCursorHighlightFillsFullRowWidth(t *testing.T) {
	d := fixtureDetailWithFiles(1, 3)
	m := NewDetailModel(d, &jumplist{})
	m.SetSize(80, 24)
	m.Update(tea.KeyMsg{Type: tea.KeyDown}) // onto the short "Author: ..." line

	row := strings.Split(m.vp.View(), "\n")[m.cursorLine]
	if got := strings.Count(row, cursorFillOpen()); got < 2 {
		t.Fatalf("row = %q, cursor background open code appears %d times, want >= 2 (text span + trailing fill)", row, got)
	}
}

// TestDetailModelCursorHighlightsBlankLine pins down that a blank line
// (e.g. the separator between the commit metadata and the message body)
// still shows a full-width cursor highlight when the cursor lands on it,
// rather than nothing at all — a blank line has no text for the cursor
// background to merge into otherwise.
func TestDetailModelCursorHighlightsBlankLine(t *testing.T) {
	d := fixtureDetailWithFiles(1, 3)
	d.Body = "line one\n\nline three" // the middle line is blank
	m := NewDetailModel(d, &jumplist{})
	m.SetSize(80, 24)

	// lines: 0 Commit, 1 Author, 2 Date, 3 "", 4 "    line one",
	// 5 "    " (blank body line), 6 "    line three"
	for m.cursorLine != 5 {
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	row := strings.Split(m.vp.View(), "\n")[m.cursorLine]
	if !strings.Contains(row, cursorFillOpen()) {
		t.Fatalf("blank line row = %q, want it to show the cursor background despite having no text", row)
	}
}

// TestDetailModelCursorHighlightsStatsRow pins down that the stats block
// (statsview.go) also honors the cursor, matching the diff/header content
// treatment — a regression check for the stats block being altogether
// skipped in an earlier pass of this feature.
func TestDetailModelCursorHighlightsStatsRow(t *testing.T) {
	d := fixtureDetailWithFiles(1, 5)
	d.Files[0].Additions = 3
	d.Files[0].Deletions = 1
	m := NewDetailModel(d, &jumplist{})
	m.SetSize(80, 30)

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")}) // jump into the stats block
	if !strings.Contains(m.View(), statsSectionTitle) {
		t.Fatalf("View() = %q, want the sticky header to show the stats section title after jumping to it", m.View())
	}

	row := strings.SplitN(m.vp.View(), "\n", 2)[0]
	if !strings.Contains(row, cursorFillOpen()) {
		t.Fatalf("stats header row = %q, want it to show the cursor background", row)
	}
}

// longWrappingLine is wider than any width these tests render at, and
// contains spaces and a hyphen — the two things lipgloss's wrapper treats as
// break opportunities — so wrapping it is a real word-wrap decision, not a
// hard break in the middle of an unbreakable run.
const longWrappingLine = "func checkHasChanges(check func(*git.Repo, context.Context, ...string) (bool, error)) (bool, error) {"

// fixtureDetailWithLongLines is a one-file commit whose diff lines all
// word-wrap at any reasonable width.
func fixtureDetailWithLongLines() *git.CommitDetail {
	return &git.CommitDetail{
		Commit: git.Commit{Hash: strings.Repeat("a", 40), Subject: "subject"},
		Body:   "subject\n\n" + longWrappingLine,
		Files: []git.File{{
			Path: "main.go", Status: git.StatusModified, Additions: 1, Deletions: 1,
			Hunks: []git.Hunk{{
				Header: "@@ -1,3 +1,3 @@",
				Lines: []git.DiffLine{
					{Op: git.OpContext, Text: "context"},
					{Op: git.OpDelete, Text: longWrappingLine},
					{Op: git.OpAdd, Text: longWrappingLine + " // added"},
				},
			}},
		}},
	}
}

// TestDetailModelContentLengthIsCursorIndependent pins down that moving the
// cursor never changes how many flattened lines the content occupies.
//
// It used to: the cursor row's background was baked into a line *before* it
// was word-wrapped, and lipgloss's wrapper stops treating spaces as word
// boundaries once a background is in effect, so a wrapped line under the
// cursor re-wrapped as one unbreakable word and landed on a different number
// of rows than the same line without it (see wrapRanges). Since contentLen
// is what bounds the cursor, that let the cursor walk past the last line the
// viewport can actually scroll to, and shifted every line index below it.
func TestDetailModelContentLengthIsCursorIndependent(t *testing.T) {
	m := NewDetailModel(fixtureDetailWithLongLines(), &jumplist{})
	m.SetSize(60, 12)

	want := m.contentLen
	for i := 0; i < want+5; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
		if m.contentLen != want {
			t.Fatalf("contentLen = %d after moving the cursor to line %d, want a constant %d",
				m.contentLen, m.cursorLine, want)
		}
	}
	// The cursor rests on the *start* of the last logical line, which is a
	// row or two above the last flattened line when that line wrapped.
	if wantLast := m.blockStart(want - 1); m.cursorLine != wantLast {
		t.Fatalf("cursorLine = %d after running past the end, want %d (the last logical line's start)",
			m.cursorLine, wantLast)
	}
}

// TestDetailModelCursorMovesWholeWrappedLines pins down that j/k step one
// *logical* line at a time, not one visual row: a line that word-wrapped to
// several rows is highlighted as one block, so stepping through its rows
// individually would make the first press look like it did nothing.
func TestDetailModelCursorMovesWholeWrappedLines(t *testing.T) {
	m := NewDetailModel(fixtureDetailWithLongLines(), &jumplist{})
	m.SetSize(60, 24)

	// Walk down the whole view; every stop must be a logical line's start,
	// and each press must actually move.
	seen := map[int]bool{}
	for prev := -1; m.cursorLine != prev; {
		prev = m.cursorLine
		if m.blockStart(m.cursorLine) != m.cursorLine {
			t.Fatalf("cursor stopped at line %d, which continues a wrapped line", m.cursorLine)
		}
		if seen[m.cursorLine] {
			t.Fatalf("cursor revisited line %d", m.cursorLine)
		}
		seen[m.cursorLine] = true
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}

	// Some line in this fixture really did wrap, otherwise the walk above
	// proves nothing.
	if len(seen) == m.contentLen {
		t.Fatalf("no line wrapped at width 60 — the fixture no longer exercises wrapping")
	}

	// And back up: k from the line below a wrapped one lands on that line's
	// start, not on its last row.
	for m.cursorLine > 0 {
		before := m.cursorLine
		m.Update(tea.KeyMsg{Type: tea.KeyUp})
		if m.cursorLine >= before {
			t.Fatalf("Up from line %d did not move (now %d)", before, m.cursorLine)
		}
		if m.blockStart(m.cursorLine) != m.cursorLine {
			t.Fatalf("Up stopped at line %d, which continues a wrapped line", m.cursorLine)
		}
	}
}

// TestDetailModelWrappedCursorBlockIsFullyVisible pins down that moving onto
// a wrapped line scrolls far enough for *all* of its rows to be on screen —
// the highlight covers the whole block, so a half-shown block reads as the
// viewport having stopped short.
func TestDetailModelWrappedCursorBlockIsFullyVisible(t *testing.T) {
	m := NewDetailModel(fixtureDetailWithLongLines(), &jumplist{})
	m.SetSize(60, 8)

	for prev := -1; m.cursorLine != prev; {
		prev = m.cursorLine
		last := m.blockEnd(m.cursorLine) - 1
		if last >= m.vp.YOffset+m.vp.Height {
			t.Fatalf("cursor block [%d,%d] runs past the visible range [%d,%d)",
				m.cursorLine, last, m.vp.YOffset, m.vp.YOffset+m.vp.Height)
		}
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
}

// TestDetailModelTallLinesScrollIntoViewRowByRow pins down that a line
// wrapping to more rows than the viewport holds (a minified file is one
// line) can be read in full: j/k scroll through it before stepping off it,
// rather than jumping from one logical line's start to the next and skipping
// everything off screen in between. The last line of a diff is the case that
// made this visible — with nothing below it to step to, the rest of it could
// never be scrolled into view at all.
func TestDetailModelTallLinesScrollIntoViewRowByRow(t *testing.T) {
	tall := strings.Repeat("word ", 400)
	d := &git.CommitDetail{
		Commit: git.Commit{Hash: strings.Repeat("a", 40), Subject: "subject"},
		Body:   "subject",
		Files: []git.File{{
			Path: "spec.json", Status: git.StatusModified, Additions: 1, Deletions: 1,
			Hunks: []git.Hunk{{
				Header: "@@ -1 +1 @@",
				Lines: []git.DiffLine{
					{Op: git.OpDelete, Text: tall},
					{Op: git.OpAdd, Text: tall + "changed"},
				},
			}},
		}},
	}
	m := NewDetailModel(d, &jumplist{})
	m.SetSize(60, 8)
	if last := m.contentLen - 1; last-m.blockStart(last) < 2*m.vp.Height {
		t.Fatalf("last line spans %d rows at height %d — the fixture no longer exercises a tall line",
			last-m.blockStart(last)+1, m.vp.Height)
	}

	walk := func(msg tea.KeyMsg) {
		t.Helper()
		seen := make([]bool, m.contentLen)
		for press := 0; press < 10*m.contentLen; press++ {
			for r := m.vp.YOffset; r < min(m.vp.YOffset+m.vp.Height, m.contentLen); r++ {
				seen[r] = true
			}
			m.Update(msg)
		}
		for r, ok := range seen {
			if !ok {
				t.Fatalf("%s: row %d of %d never came into view", msg, r, m.contentLen)
			}
		}
	}
	walk(tea.KeyMsg{Type: tea.KeyDown})
	walk(tea.KeyMsg{Type: tea.KeyUp})
	if m.cursorLine != 0 || m.vp.YOffset != 0 {
		t.Fatalf("after walking back up: cursorLine=%d YOffset=%d, want both 0", m.cursorLine, m.vp.YOffset)
	}
}

// TestDetailModelCursorPatchMatchesFullReflow pins down the bargain
// repaintHighlight makes: moving the cursor re-renders only the two logical
// lines whose highlight changed and splices them into the cached content, so
// that content must stay byte-for-byte identical to what a full reflow at
// the same cursor position would have produced. Anything that made a line's
// rendering depend on state a patch doesn't refresh — or that shifted a
// wrapped line's row count under the cursor — would show up here as a
// divergence.
//
// The walk covers plain motion (j), a section jump ([ / ]), a paragraph jump
// ({ / }), and a match step (n), the ways the cursor gets somewhere.
func TestDetailModelCursorPatchMatchesFullReflow(t *testing.T) {
	m := NewDetailModel(fixtureDetailWithLongLines(), &jumplist{})
	m.SetSize(60, 8)
	m.SetSearchDisplay("context", false)

	press := func(msg tea.KeyMsg) { m.Update(msg) }
	steps := []struct {
		name string
		do   func()
	}{
		{"down", func() { press(tea.KeyMsg{Type: tea.KeyDown}) }},
		{"down", func() { press(tea.KeyMsg{Type: tea.KeyDown}) }},
		{"down", func() { press(tea.KeyMsg{Type: tea.KeyDown}) }},
		{"next file", func() { press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}}) }},
		{"next paragraph", func() { press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'}'}}) }},
		{"prev paragraph", func() { press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'{'}}) }},
		{"next match", func() { m.NextMatch() }},
		{"up", func() { press(tea.KeyMsg{Type: tea.KeyUp}) }},
		{"bottom", func() { press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}}) }},
		{"top", func() { press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}}) }},
	}
	for _, step := range steps {
		step.do()
		at := m.cursorLine
		assertPatchMatchesReflow(t, &m, step.name)
		if at != m.cursorLine {
			t.Fatalf("after %s: reflow moved the cursor from %d to %d", step.name, at, m.cursorLine)
		}
	}
}

// assertPatchMatchesReflow checks the bargain both patch tests are about: the
// content the incremental repaints have been splicing into must be
// byte-for-byte what a full reflow at the same position produces. It reflows
// m as a side effect, which is the point — the comparison is against a freshly
// built content, and the model carries on from it.
func assertPatchMatchesReflow(t *testing.T, m *DetailModel, after string) {
	t.Helper()
	patched := append([]string(nil), m.content...)

	m.reflow()
	if len(patched) != len(m.content) {
		t.Fatalf("after %s: patched content has %d rows, a full reflow produces %d", after, len(patched), len(m.content))
	}
	for i := range patched {
		if patched[i] != m.content[i] {
			t.Fatalf("after %s (cursor at %d): row %d differs\n patched: %q\n reflown: %q",
				after, m.cursorLine, i, patched[i], m.content[i])
		}
	}
}

// fixtureBigDetail builds a commit large enough for the difference between
// re-rendering one line and re-rendering all of them to be obvious.
func fixtureBigDetail(files, linesPerHunk int) *git.CommitDetail {
	d := &git.CommitDetail{
		Commit: git.Commit{Hash: strings.Repeat("a", 40), Subject: "subject"},
		Body:   "subject\n\nbody\n",
	}
	for f := 0; f < files; f++ {
		hunk := git.Hunk{Header: "@@ -1,20 +1,20 @@ func something()"}
		for i := 0; i < linesPerHunk; i++ {
			op := git.OpContext
			switch {
			case i%5 == 0:
				op = git.OpAdd
			case i%7 == 0:
				op = git.OpDelete
			}
			hunk.Lines = append(hunk.Lines, git.DiffLine{
				Op:   op,
				Text: fmt.Sprintf("\tsomeCall(arg%d, other) // a fairly typical line of source code", i),
			})
		}
		d.Files = append(d.Files, git.File{
			Path:   fmt.Sprintf("internal/pkg/file%d.go", f),
			Status: git.StatusModified,
			Hunks:  []git.Hunk{hunk},
		})
	}
	return d
}

// TestDetailCursorMoveCostIsIndependentOfCommitSize is the other half of
// repaintHighlight's bargain: not just "don't re-render every line on a keypress"
// but "don't re-read them either". The benchmarks below only ever compare a
// cursor move against a reflow of the *same* commit, so they stayed happy
// while bubbles/viewport re-joined and re-split the whole content and measured
// every line of it with ansi.StringWidth on every handoff — 90% of a keypress,
// and what made a held-down j outrun the input reader.
//
// Bytes, not allocation count: that regression moved a megabyte per keystroke
// in a constant number of allocations, so AllocsPerRun would have sat at 77
// through all of it.
func TestDetailCursorMoveCostIsIndependentOfCommitSize(t *testing.T) {
	perMove := func(files int) uint64 {
		m := NewDetailModel(fixtureBigDetail(files, 200), &jumplist{})
		m.SetSize(100, 40)
		down := tea.KeyMsg{Type: tea.KeyDown}
		return bytesPerRun(100, func() { m.Update(down) })
	}
	small, big := perMove(2), perMove(40)
	if big > 2*small {
		t.Fatalf("a cursor move allocates %d bytes on a 40-file commit against %d on a 2-file one; something on the keypress path scales with the whole commit", big, small)
	}
}

// bytesPerRun is testing.AllocsPerRun's missing counterpart: bytes allocated
// per call to f, averaged over runs. f is called once first so one-time lazy
// work inside it isn't billed to the measurement.
func bytesPerRun(runs int, f func()) uint64 {
	f()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for range runs {
		f()
	}
	runtime.ReadMemStats(&after)
	return (after.TotalAlloc - before.TotalAlloc) / uint64(runs)
}

// BenchmarkDetailCursorMove and BenchmarkDetailReflow are the measurement
// behind repaintHighlight: a cursor move must cost a couple of line renders and
// an O(1) viewport handoff, not a re-render of the whole commit. On an
// 8000-line diff the two differ by three orders of magnitude (~6 µs against
// ~13 ms), and if they ever converge, something has started reflowing on every
// keypress.
func BenchmarkDetailCursorMove(b *testing.B) {
	m := NewDetailModel(fixtureBigDetail(40, 200), &jumplist{})
	m.SetSize(100, 40)
	down := tea.KeyMsg{Type: tea.KeyDown}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Update(down)
	}
}

func BenchmarkDetailReflow(b *testing.B) {
	m := NewDetailModel(fixtureBigDetail(40, 200), &jumplist{})
	m.SetSize(100, 40)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.reflow()
	}
}

// TestJumpToHeaderOnlyLastFileKeepsCursorOnContent pins that [ / ] onto a last
// file with nothing under its header (a pure rename, a mode change) leaves the
// cursor on a line that exists. The viewport is allowed to scroll to the row
// past the content — reflow pads it for exactly that — but the cursor has no
// line there to highlight.
func TestJumpToHeaderOnlyLastFileKeepsCursorOnContent(t *testing.T) {
	m := NewDetailModel(fixturePseudoDetail(git.EntryWorktree, []git.File{
		{
			Path:   "a.txt",
			Status: git.StatusModified,
			Hunks: []git.Hunk{{
				Header: "@@ -1 +1 @@",
				Lines:  []git.DiffLine{{Op: git.OpAdd, Text: "changed"}},
			}},
		},
		{OldPath: "old.txt", Path: "new.txt", Status: git.StatusRenamed},
	}), &jumplist{})
	m.SetSize(80, 10)

	for range 4 {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	}
	if m.cursorLine < 0 || m.cursorLine >= m.contentLen {
		t.Errorf("cursorLine = %d after ] onto the last file, want it within [0, %d)", m.cursorLine, m.contentLen)
	}
}
