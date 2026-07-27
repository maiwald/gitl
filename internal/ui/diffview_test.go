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
