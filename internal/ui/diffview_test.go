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
	lines, ranges := RenderDiff(files, 80)

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

func TestFileAtBoundaries(t *testing.T) {
	files := fixtureDiffFiles()
	_, ranges := RenderDiff(files, 80)

	tests := []struct {
		name      string
		lineIndex int
		want      int
	}{
		{"first line of file 0", ranges[0].StartLine, 0},
		{"last line of file 0", ranges[0].EndLine - 1, 0},
		{"first line of file 1", ranges[1].StartLine, 1},
		{"last line of file 1", ranges[1].EndLine - 1, 1},
		{"before start", -1, -1},
		{"past end", ranges[1].EndLine, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FileAt(ranges, tt.lineIndex); got != tt.want {
				t.Errorf("FileAt(ranges, %d) = %d, want %d", tt.lineIndex, got, tt.want)
			}
		})
	}
}

func TestFileAtEmptyRanges(t *testing.T) {
	if got := FileAt(nil, 0); got != -1 {
		t.Errorf("FileAt(nil, 0) = %d, want -1", got)
	}
}

func TestRenderFileHeaderShowsRenameArrow(t *testing.T) {
	files := []git.File{
		{OldPath: "old.txt", Path: "new.txt", Status: git.StatusRenamed},
	}
	lines, _ := RenderDiff(files, 80)
	if len(lines) == 0 {
		t.Fatal("expected at least one rendered line")
	}
	for _, sub := range []string{"old.txt", "->", "new.txt"} {
		if !strings.Contains(lines[0], sub) {
			t.Errorf("header line = %q, want it to contain %q", lines[0], sub)
		}
	}
}
