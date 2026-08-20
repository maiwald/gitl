package ui

import (
	"strings"
	"testing"

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

func TestRenderDiffProducesContiguousRanges(t *testing.T) {
	files := fixtureDiffFiles()
	lines, ranges, _ := RenderDiff(files, 80, nil, -1, false)

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

	lines, ranges, _ := RenderDiff(files, 20, nil, -1, false)

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

func TestFindDiffMatchesExcludesHeadersAndHunkHeaders(t *testing.T) {
	files := []git.File{{
		Path: "secretterm.txt",
		Hunks: []git.Hunk{{
			Header: "@@ secretterm @@",
			Lines:  []git.DiffLine{{Op: git.OpContext, Text: "nothing relevant here"}},
		}},
	}}

	got := FindDiffMatches(files, "secretterm")
	if len(got) != 0 {
		t.Errorf("got %+v, want no matches (search must not consider file paths or hunk headers)", got)
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

	lines, _, matchLines := RenderDiff(files, 80, matches, 0, false)

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
	_, ranges, matchLines := RenderDiff(files, 80, matches, -1, false)

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
	lines, _, _ := RenderDiff(files, 80, nil, -1, false)
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
	lines, _, _ := RenderDiff(files, 0, nil, -1, false)
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
