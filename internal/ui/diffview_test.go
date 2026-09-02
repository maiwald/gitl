package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/maiwald/gitl/internal/git"
)

func fixtureDiffFiles() []git.File {
	return []git.File{
		{
			Path:   "a.txt",
			Status: git.StatusModified,
			Hunks: []git.Hunk{
				{
					Header: "@@ -1,2 +1,2 @@",
					Lines: []git.DiffLine{
						{Op: git.OpContext, Text: "unchanged"},
						{Op: git.OpDelete, Text: "old"},
						{Op: git.OpAdd, Text: "new"},
					},
				},
			},
		},
		{
			Path:   "b.txt",
			Status: git.StatusAdded,
			Hunks: []git.Hunk{
				{
					Header: "@@ -0,0 +1,2 @@",
					Lines: []git.DiffLine{
						{Op: git.OpAdd, Text: "hello"},
						{Op: git.OpAdd, Text: "world"},
					},
				},
			},
		},
	}
}

// renderDiffRows lays a diff out the way DetailModel.reflow does — every
// logical line rendered, with the one containing cursorLine carrying the
// cursor background — and rebases RenderDiff's logical coordinates onto the
// rows they produced, which is the flattened shape these tests assert on.
func renderDiffRows(files []git.File, width int, matches []DiffMatch, activeMatch int, dismissed bool, cursorLine int) (rows []string, ranges []FileRange, matchRows []int) {
	lines, logicalRanges, matchLines := RenderDiff(files, width, matches, activeMatch, dismissed)
	rows, lineStart := renderRows(lines, cursorLine, cursorLine)

	rowOf := func(logical int) int {
		if logical < len(lineStart) {
			return lineStart[logical]
		}
		return len(rows)
	}
	ranges = make([]FileRange, len(logicalRanges))
	for i, r := range logicalRanges {
		ranges[i] = FileRange{FileIndex: r.FileIndex, StartLine: rowOf(r.StartLine), EndLine: rowOf(r.EndLine)}
	}
	matchRows = make([]int, len(matchLines))
	for i, ln := range matchLines {
		matchRows[i] = rowOf(ln)
	}
	return rows, ranges, matchRows
}

func TestRenderDiffProducesContiguousRanges(t *testing.T) {
	files := fixtureDiffFiles()
	lines, ranges, _ := renderDiffRows(files, 80, nil, -1, false, -1)

	if len(ranges) != 2 {
		t.Fatalf("got %d ranges, want 2", len(ranges))
	}
	if ranges[0].StartLine != 0 {
		t.Errorf("ranges[0].StartLine = %d, want 0", ranges[0].StartLine)
	}
	if ranges[0].EndLine != ranges[1].StartLine {
		t.Errorf("ranges not contiguous: file 0 ends at %d, file 1 starts at %d", ranges[0].EndLine, ranges[1].StartLine)
	}
	if ranges[1].EndLine != len(lines) {
		t.Errorf("last range EndLine = %d, want %d (total lines)", ranges[1].EndLine, len(lines))
	}
	// file 0: header + hunk header + 3 lines = 5 lines
	if ranges[0].EndLine-ranges[0].StartLine != 5 {
		t.Errorf("file 0 range length = %d, want 5", ranges[0].EndLine-ranges[0].StartLine)
	}
}

func TestRenderDiffWrapsLongLinesAndKeepsRangesContiguous(t *testing.T) {
	longText := strings.Repeat("word ", 40) // much wider than a 20-col panel
	files := []git.File{
		{
			Path:   "long.txt",
			Status: git.StatusModified,
			Hunks: []git.Hunk{
				{
					Header: "@@ -1 +1 @@",
					Lines: []git.DiffLine{
						{Op: git.OpAdd, Text: longText},
						{Op: git.OpContext, Text: "short"},
					},
				},
			},
		},
		{
			Path:   "next.txt",
			Status: git.StatusModified,
			Hunks: []git.Hunk{
				{Header: "@@ -1 +1 @@", Lines: []git.DiffLine{{Op: git.OpContext, Text: "x"}}},
			},
		},
	}

	lines, ranges, _ := renderDiffRows(files, 20, nil, -1, false, -1)

	// The long line must have wrapped into more than one rendered row.
	if ranges[0].EndLine-ranges[0].StartLine <= 3 { // header + hunk header + short line + wrapped long line(s)
		t.Fatalf("file 0 has %d lines, expected wrapping to add extra rows", ranges[0].EndLine-ranges[0].StartLine)
	}

	// Ranges must still be contiguous and cover every line with no gaps or
	// overlaps, even after wrapping expands a file's line count.
	if ranges[0].StartLine != 0 || ranges[0].EndLine != ranges[1].StartLine || ranges[1].EndLine != len(lines) {
		t.Fatalf("ranges not contiguous: %+v (total lines %d)", ranges, len(lines))
	}
}

func TestFindDiffMatchesFindsOccurrencesInLineOrder(t *testing.T) {
	files := []git.File{
		{
			Path: "a.txt",
			Hunks: []git.Hunk{{
				Header: "@@ -1,2 +1,2 @@",
				Lines: []git.DiffLine{
					{Op: git.OpContext, Text: "hello world"},
					{Op: git.OpAdd, Text: "another hello"},
				},
			}},
		},
		{
			Path: "b.txt",
			Hunks: []git.Hunk{{
				Header: "@@ -1 +1 @@",
				Lines:  []git.DiffLine{{Op: git.OpDelete, Text: "HELLO there"}},
			}},
		},
	}

	got := FindDiffMatches(files, "hello")
	want := []DiffMatch{
		{FileIndex: 0, HunkIndex: 0, LineIndex: 0, StartCol: 0, EndCol: 5},
		{FileIndex: 0, HunkIndex: 0, LineIndex: 1, StartCol: 8, EndCol: 13},
		{FileIndex: 1, HunkIndex: 0, LineIndex: 0, StartCol: 0, EndCol: 5},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d matches, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("match %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestFindDiffMatchesMultipleOccurrencesInOneLine(t *testing.T) {
	files := []git.File{{
		Hunks: []git.Hunk{{
			Header: "@@ -1 +1 @@",
			Lines:  []git.DiffLine{{Op: git.OpContext, Text: "cat cat cat"}},
		}},
	}}

	got := FindDiffMatches(files, "cat")
	want := []DiffMatch{
		{FileIndex: 0, HunkIndex: 0, LineIndex: 0, StartCol: 0, EndCol: 3},
		{FileIndex: 0, HunkIndex: 0, LineIndex: 0, StartCol: 4, EndCol: 7},
		{FileIndex: 0, HunkIndex: 0, LineIndex: 0, StartCol: 8, EndCol: 11},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d matches, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("match %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestFindDiffMatchesExcludesHunkHeaders(t *testing.T) {
	files := []git.File{{
		Path: "unrelated.txt",
		Hunks: []git.Hunk{{
			Header: "@@ secretterm @@",
			Lines:  []git.DiffLine{{Op: git.OpContext, Text: "nothing relevant here"}},
		}},
	}}

	got := FindDiffMatches(files, "secretterm")
	if len(got) != 0 {
		t.Errorf("got %+v, want no matches (search must not consider hunk headers)", got)
	}
}

func TestFindDiffMatchesIncludesFileName(t *testing.T) {
	files := []git.File{{
		Path: "secretterm.txt",
		Hunks: []git.Hunk{{
			Header: "@@ -1 +1 @@",
			Lines:  []git.DiffLine{{Op: git.OpContext, Text: "nothing relevant here"}},
		}},
	}}

	got := FindDiffMatches(files, "secretterm")
	want := []DiffMatch{{Kind: DiffMatchFileName, FileIndex: 0, StartCol: 0, EndCol: 10}}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("got %+v, want %+v (search must match file names)", got, want)
	}
}

func TestFindDiffMatchesFileNameSortsBeforeItsOwnLineMatches(t *testing.T) {
	files := []git.File{{
		Path: "needle.txt",
		Hunks: []git.Hunk{{
			Header: "@@ -1 +1 @@",
			Lines:  []git.DiffLine{{Op: git.OpContext, Text: "needle in a haystack"}},
		}},
	}}

	got := FindDiffMatches(files, "needle")
	want := []DiffMatch{
		{Kind: DiffMatchFileName, FileIndex: 0, StartCol: 0, EndCol: 6},
		{Kind: DiffMatchLine, FileIndex: 0, HunkIndex: 0, LineIndex: 0, StartCol: 0, EndCol: 6},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d matches, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("match %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestFindDiffMatchesRenamedFileMatchesOldAndNewPath(t *testing.T) {
	files := []git.File{{
		Status:  git.StatusRenamed,
		OldPath: "old_name.txt",
		Path:    "new_name.txt",
	}}

	oldMatches := FindDiffMatches(files, "old_name")
	if len(oldMatches) != 1 || oldMatches[0].Kind != DiffMatchFileName {
		t.Errorf("got %+v, want one file-name match against the old path", oldMatches)
	}

	newMatches := FindDiffMatches(files, "new_name")
	if len(newMatches) != 1 || newMatches[0].Kind != DiffMatchFileName {
		t.Errorf("got %+v, want one file-name match against the new path", newMatches)
	}
}

func TestFindDiffMatchesEmptyTerm(t *testing.T) {
	if got := FindDiffMatches(fixtureDiffFiles(), ""); got != nil {
		t.Errorf("got %+v, want nil for an empty term", got)
	}
}

func TestRenderDiffHighlightsMatchesAndMarksActiveOneDistinctly(t *testing.T) {
	files := []git.File{{
		Hunks: []git.Hunk{{
			Header: "@@ -1,2 +1,2 @@",
			Lines: []git.DiffLine{
				{Op: git.OpContext, Text: "needle one"},
				{Op: git.OpContext, Text: "needle two"},
			},
		}},
	}}
	matches := FindDiffMatches(files, "needle")
	if len(matches) != 2 {
		t.Fatalf("got %d matches, want 2", len(matches))
	}

	lines, _, matchLines := renderDiffRows(files, 80, matches, 0, false, -1)

	if !strings.Contains(lines[matchLines[0]], searchActiveMatchStyle.Render("needle")) {
		t.Errorf("line %d = %q, want the active match styled with searchActiveMatchStyle", matchLines[0], lines[matchLines[0]])
	}
	if !strings.Contains(lines[matchLines[1]], searchMatchStyle.Render("needle")) {
		t.Errorf("line %d = %q, want the non-active match styled with searchMatchStyle", matchLines[1], lines[matchLines[1]])
	}
	if strings.Contains(lines[matchLines[1]], searchActiveMatchStyle.Render("needle")) {
		t.Errorf("line %d = %q, non-active match must not use the active style", matchLines[1], lines[matchLines[1]])
	}
}

func TestRenderDiffMatchLinesFallWithinCorrespondingFileRange(t *testing.T) {
	files := []git.File{
		{
			Hunks: []git.Hunk{{
				Header: "@@ -1 +1 @@",
				Lines:  []git.DiffLine{{Op: git.OpContext, Text: "needle"}},
			}},
		},
		{
			Hunks: []git.Hunk{{
				Header: "@@ -1 +1 @@",
				Lines:  []git.DiffLine{{Op: git.OpContext, Text: "another needle"}},
			}},
		},
	}
	matches := FindDiffMatches(files, "needle")
	_, ranges, matchLines := renderDiffRows(files, 80, matches, -1, false, -1)

	if len(matches) != 2 || len(matchLines) != 2 {
		t.Fatalf("got %d matches / %d matchLines, want 2/2", len(matches), len(matchLines))
	}
	for i, ln := range matchLines {
		r := ranges[matches[i].FileIndex]
		if ln < r.StartLine || ln >= r.EndLine {
			t.Errorf("matchLines[%d] = %d, want within file %d's range [%d, %d)", i, ln, matches[i].FileIndex, r.StartLine, r.EndLine)
		}
	}
}

func TestRenderFileHeaderShowsRenameArrow(t *testing.T) {
	files := []git.File{
		{OldPath: "old.txt", Path: "new.txt", Status: git.StatusRenamed},
	}
	lines, _, _ := renderDiffRows(files, 80, nil, -1, false, -1)
	if len(lines) == 0 {
		t.Fatal("expected at least one rendered line")
	}
	for _, sub := range []string{"old.txt", "->", "new.txt"} {
		if !strings.Contains(lines[0], sub) {
			t.Errorf("header line = %q, want it to contain %q", lines[0], sub)
		}
	}
}

func TestIntralineDiffFindsChangedWordInMiddle(t *testing.T) {
	oldText, newText := "foo bar baz", "foo QUX baz"
	oldSpan, newSpan, ok := intralineDiff(oldText, newText)
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if got := oldText[oldSpan[0]:oldSpan[1]]; got != "bar" {
		t.Errorf("old changed span = %q, want %q", got, "bar")
	}
	if got := newText[newSpan[0]:newSpan[1]]; got != "QUX" {
		t.Errorf("new changed span = %q, want %q", got, "QUX")
	}
}

func TestIntralineDiffFindsAppendedSuffix(t *testing.T) {
	oldText, newText := "line two", "line two changed"
	oldSpan, newSpan, ok := intralineDiff(oldText, newText)
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if got := oldText[oldSpan[0]:oldSpan[1]]; got != "" {
		t.Errorf("old changed span = %q, want empty (nothing removed)", got)
	}
	if got := newText[newSpan[0]:newSpan[1]]; got != " changed" {
		t.Errorf("new changed span = %q, want %q", got, " changed")
	}
}

func TestIntralineDiffIsRuneSafeAroundMultibyteCommonPrefix(t *testing.T) {
	// "café" sits in the common prefix, ahead of the byte offset where old
	// and new diverge — this only comes out right if the split point is
	// computed in rune counts and converted to a byte offset afterward,
	// not by indexing the string directly with a rune count.
	oldText, newText := "the café is nice", "the café is great"
	oldSpan, newSpan, ok := intralineDiff(oldText, newText)
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if got := oldText[oldSpan[0]:oldSpan[1]]; got != "nice" {
		t.Errorf("old changed span = %q, want %q", got, "nice")
	}
	if got := newText[newSpan[0]:newSpan[1]]; got != "great" {
		t.Errorf("new changed span = %q, want %q", got, "great")
	}
}

func TestIntralineDiffBailsOutOnIdenticalLines(t *testing.T) {
	if _, _, ok := intralineDiff("same", "same"); ok {
		t.Error("ok = true for identical lines, want false (nothing to highlight)")
	}
}

func TestIntralineDiffBailsOutWhenNothingIsShared(t *testing.T) {
	if _, _, ok := intralineDiff("abc", "xyz"); ok {
		t.Error("ok = true for lines sharing no prefix or suffix, want false (nothing to anchor a highlight to)")
	}
}

func TestIntralineDiffBailsOutWhenSharedContentIsOnlyWhitespace(t *testing.T) {
	// Both lines start with the same two-space indent, but that's the only
	// thing they share — matches diff-highlight's own rule that a shared
	// run of pure whitespace doesn't count as "meaningful" common content.
	if _, _, ok := intralineDiff("  foo", "  bar"); ok {
		t.Error("ok = true when the only shared prefix is whitespace, want false")
	}
}

func TestIntralineDiffHighlightsEvenWhenMostOfLineChanged(t *testing.T) {
	// diff-highlight (and diff-so-fancy, which wraps it) has no ratio-based
	// cutoff: a single shared non-space character at either end is enough
	// to highlight, no matter how much of the rest of the line differs.
	oldText, newText := "x short", "x an entirely different and much longer tail"
	oldSpan, newSpan, ok := intralineDiff(oldText, newText)
	if !ok {
		t.Fatal("ok = false, want true (shared leading \"x\" should be enough to anchor a highlight)")
	}
	if got := oldText[oldSpan[0]:oldSpan[1]]; got != "short" {
		t.Errorf("old changed span = %q, want %q", got, "short")
	}
	if got := newText[newSpan[0]:newSpan[1]]; got != "an entirely different and much longer tail" {
		t.Errorf("new changed span = %q, want %q", got, "an entirely different and much longer tail")
	}
}

func TestIntralineSpansPairsDeleteAddLinesPositionally(t *testing.T) {
	h := git.Hunk{Lines: []git.DiffLine{
		{Op: git.OpContext, Text: "unchanged"},
		{Op: git.OpDelete, Text: "foo bar baz"},
		{Op: git.OpAdd, Text: "foo QUX baz"},
		{Op: git.OpContext, Text: "unchanged"},
	}}
	spans := intralineSpans(h)

	oldSpans, ok := spans[1]
	if !ok || len(oldSpans) != 1 {
		t.Fatalf("spans[1] = %v, want one span", spans[1])
	}
	if got := h.Lines[1].Text[oldSpans[0].start:oldSpans[0].end]; got != "bar" {
		t.Errorf("delete line span = %q, want %q", got, "bar")
	}

	newSpans, ok := spans[2]
	if !ok || len(newSpans) != 1 {
		t.Fatalf("spans[2] = %v, want one span", spans[2])
	}
	if got := h.Lines[2].Text[newSpans[0].start:newSpans[0].end]; got != "QUX" {
		t.Errorf("add line span = %q, want %q", got, "QUX")
	}

	if _, ok := spans[0]; ok {
		t.Error("context line got a span, want none")
	}
	if _, ok := spans[3]; ok {
		t.Error("context line got a span, want none")
	}
}

func TestIntralineSpansSkipsWholeBlockOnUnequalCounts(t *testing.T) {
	// Two deletes, one add: diff-highlight's show_hunk only pairs (and
	// highlights) a block when its removed/added counts match exactly, so
	// an unequal block like this gets no spans at all, not even a partial
	// best-effort pairing of the first line on each side.
	h := git.Hunk{Lines: []git.DiffLine{
		{Op: git.OpDelete, Text: "foo bar"},
		{Op: git.OpDelete, Text: "extra removed line"},
		{Op: git.OpAdd, Text: "foo QUX"},
	}}
	if spans := intralineSpans(h); len(spans) != 0 {
		t.Errorf("spans = %v, want none for a block with unequal delete/add counts", spans)
	}
}

func TestIntralineSpansSkipsPureInsertionsAndDeletions(t *testing.T) {
	h := git.Hunk{Lines: []git.DiffLine{
		{Op: git.OpAdd, Text: "brand new line"},
	}}
	if spans := intralineSpans(h); len(spans) != 0 {
		t.Errorf("spans = %v, want none for a pure insertion (no delete counterpart)", spans)
	}
}

func TestRenderDiffAppliesIntralineEmphasisStyles(t *testing.T) {
	files := []git.File{{
		Hunks: []git.Hunk{{
			Header: "@@ -1 +1 @@",
			Lines: []git.DiffLine{
				{Op: git.OpDelete, Text: "foo bar baz"},
				{Op: git.OpAdd, Text: "foo QUX baz"},
			},
		}},
	}}
	lines, _, _ := renderDiffRows(files, 0, nil, -1, false, -1)
	if len(lines) != 4 { // file header + hunk header + delete + add
		t.Fatalf("got %d lines, want 4 (file header, hunk header, delete, add)", len(lines))
	}
	deleteLine, addLine := lines[2], lines[3]
	if !strings.Contains(deleteLine, diffDeleteEmphStyle.Render("bar")) {
		t.Errorf("delete line = %q, want it to contain the emphasized changed word", deleteLine)
	}
	if !strings.Contains(addLine, diffAddEmphStyle.Render("QUX")) {
		t.Errorf("add line = %q, want it to contain the emphasized changed word", addLine)
	}
}

// TestRenderDiffLineCursorRowCountMatchesPlain pins down that a line's
// wrapped height doesn't depend on whether the cursor is on it. RenderDiff
// renders each line plain to learn how many rows it occupies and only then
// re-renders it with the cursor flag, so a mismatch would corrupt every line
// index below it (FileRanges, match positions, the cursor's own bound).
//
// The specific failure this guards against: lipgloss's wrapper only breaks
// at a space while the style in effect has no background, so baking the
// cursor background in before wrapping turned the whole line into one
// unbreakable word — see wrapRanges in render.go.
func TestRenderDiffLineCursorRowCountMatchesPlain(t *testing.T) {
	const width = 40
	tests := []struct {
		name string
		text string
	}{
		{"wraps on a space", "func checkHasChanges(check func(*git.Repo, context.Context) (bool, error)) (bool, error) {"},
		{"tab-indented", "\tif err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {"},
		{"one long unbreakable word", strings.Repeat("a", width*2+1)},
		{"exactly one row wide", strings.Repeat("a", width-1)},
		{"short", "short"},
		{"empty", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := git.DiffLine{Op: git.OpDelete, Text: tt.text}
			plain := renderDiffLine(line, width, nil, nil, false)
			cursor := renderDiffLine(line, width, nil, nil, true)
			if len(plain) != len(cursor) {
				t.Fatalf("plain wrapped to %d rows, cursor to %d — they must agree", len(plain), len(cursor))
			}
			for i, row := range cursor {
				if got := lipgloss.Width(row); got != width {
					t.Errorf("cursor row %d width = %d, want %d (rows are padded to full width)", i, got, width)
				}
			}
		})
	}
}

// TestRenderDiffRowsNeverEmbedNewlines pins down that every row RenderDiff
// returns is exactly one display line. A hunk header carrying a long
// function context, or a long file path, is *truncated* to width, never
// wrapped: callers treat each returned row as one entry in the flattened
// line space, so a row that quietly renders as two display lines shifts
// every line below it out of step with the cursor, FileRanges and scroll
// positions that index into it — the diff then scrolls past what the cursor
// can reach. See renderClipped's Inline in render.go.
func TestRenderDiffRowsNeverEmbedNewlines(t *testing.T) {
	const width = 60
	files := []git.File{{
		Path:   strings.Repeat("deeply/nested/", 8) + "file.go",
		Status: git.StatusModified,
		Hunks: []git.Hunk{{
			Header: "@@ -140,7 +140,7 @@ func checkHasChanges(check func(*git.Repo, context.Context) (bool, error)) (bool, error) {",
			Lines:  []git.DiffLine{{Op: git.OpContext, Text: "x"}},
		}},
	}}

	for _, cursorLine := range []int{-1, 0, 1, 2} {
		lines, ranges, _ := renderDiffRows(files, width, nil, -1, false, cursorLine)
		for i, row := range lines {
			if strings.Contains(row, "\n") {
				t.Errorf("cursorLine %d: row %d spans %d display lines: %q",
					cursorLine, i, strings.Count(row, "\n")+1, row)
			}
		}
		if last := ranges[len(ranges)-1].EndLine; last != len(lines) {
			t.Errorf("cursorLine %d: last range ends at %d, want %d", cursorLine, last, len(lines))
		}
	}
}
