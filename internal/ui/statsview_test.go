package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/maiwald/gitl/internal/git"
)

func TestStatLinesForEmptyFilesReturnsNil(t *testing.T) {
	if got := statLinesFor(nil, 80); got != nil {
		t.Errorf("got %+v, want nil for no files", got)
	}
}

func TestStatTotalsLinePluralizationAndOmission(t *testing.T) {
	tests := []struct {
		name             string
		nFiles, add, del int
		want             string
	}{
		{"single file no changes", 1, 0, 0, " 1 file changed"},
		{"single insertion", 1, 1, 0, " 1 file changed, 1 insertion(+)"},
		{"single deletion", 1, 0, 1, " 1 file changed, 1 deletion(-)"},
		{"many files both", 3, 42, 7, " 3 files changed, 42 insertions(+), 7 deletions(-)"},
		{"insertions only, many", 2, 5, 0, " 2 files changed, 5 insertions(+)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := statTotalsLine(tt.nFiles, tt.add, tt.del); got != tt.want {
				t.Errorf("statTotalsLine(%d, %d, %d) = %q, want %q", tt.nFiles, tt.add, tt.del, got, tt.want)
			}
		})
	}
}

// TestStatLinesForStructure pins down the block's shape: a "Files changed"
// header bar (matching the diff's per-file header bar style), one line per
// file, then the totals line last.
func TestStatLinesForStructure(t *testing.T) {
	files := []git.File{
		{Path: "a.txt", Status: git.StatusModified, Additions: 3, Deletions: 1},
		{Path: "b.txt", Status: git.StatusAdded, Additions: 10},
	}
	lines := statLinesFor(files, 80)
	if len(lines) != len(files)+2 {
		t.Fatalf("got %d lines, want %d (header bar + one per file + totals)", len(lines), len(files)+2)
	}

	if !strings.Contains(lines[0], "Files changed") {
		t.Errorf("lines[0] = %q, want the header bar text", lines[0])
	}
	if lines[0] != renderClipped(fileHeaderStyle, "Files changed", 80) {
		t.Errorf("lines[0] = %q, want it styled with fileHeaderStyle (same as a diff file's header bar)", lines[0])
	}

	want := statTotalsLine(2, 13, 1)
	if !strings.Contains(lines[len(lines)-1], strings.TrimSpace(want)) {
		t.Errorf("last line = %q, want it to contain %q", lines[len(lines)-1], want)
	}
}

func TestStatLinesForPerFileCountsOmitZeroClause(t *testing.T) {
	files := []git.File{
		{Path: "added.txt", Status: git.StatusAdded, Additions: 5},
		{Path: "removed.txt", Status: git.StatusDeleted, Deletions: 2},
		{Path: "both.txt", Status: git.StatusModified, Additions: 1, Deletions: 1},
	}
	lines := statLinesFor(files, 80)
	// lines[0] is the "Files changed" header bar; file lines start at 1.

	if !strings.Contains(lines[1], diffAddStyle.Render("+5")) {
		t.Errorf("added.txt line = %q, want it to contain +5 styled with diffAddStyle", lines[1])
	}
	if strings.Contains(lines[1], "-0") {
		t.Errorf("added.txt line = %q, want no deletions clause when deletions is 0", lines[1])
	}

	if !strings.Contains(lines[2], diffDeleteStyle.Render("-2")) {
		t.Errorf("removed.txt line = %q, want it to contain -2 styled with diffDeleteStyle", lines[2])
	}
	if strings.Contains(lines[2], "+0") {
		t.Errorf("removed.txt line = %q, want no insertions clause when additions is 0", lines[2])
	}

	if !strings.Contains(lines[3], diffAddStyle.Render("+1")) || !strings.Contains(lines[3], diffDeleteStyle.Render("-1")) {
		t.Errorf("both.txt line = %q, want both +1 and -1 styled", lines[3])
	}
}

func TestStatLinesForBinaryFileShowsBin(t *testing.T) {
	files := []git.File{{Path: "image.png", Status: git.StatusModified, Binary: true}}
	lines := statLinesFor(files, 80)
	if !strings.Contains(lines[1], "Bin") {
		t.Errorf("binary file line = %q, want it to contain Bin", lines[1])
	}
	if strings.Contains(lines[1], "+") || strings.Contains(lines[1], "-") {
		t.Errorf("binary file line = %q, want no +/- counts", lines[1])
	}
}

func TestStatLinesForRenameShowsArrow(t *testing.T) {
	files := []git.File{{OldPath: "old.txt", Path: "new.txt", Status: git.StatusRenamed}}
	lines := statLinesFor(files, 80)
	for _, sub := range []string{"old.txt", "->", "new.txt"} {
		if !strings.Contains(lines[1], sub) {
			t.Errorf("rename line = %q, want it to contain %q", lines[1], sub)
		}
	}
}

func TestStatLinesForClipsToWidth(t *testing.T) {
	files := []git.File{{Path: strings.Repeat("x", 100), Status: git.StatusModified, Additions: 1234}}
	const width = 20
	lines := statLinesFor(files, width)
	for i, line := range lines {
		if got := lipgloss.Width(line); got > width {
			t.Errorf("lines[%d] rendered width = %d, want <= %d (clipped, not wrapped)", i, got, width)
		}
	}
}

func TestStatLinesForZeroChangeFileShowsZero(t *testing.T) {
	files := []git.File{{Path: "mode-only.txt", Status: git.StatusModified}}
	lines := statLinesFor(files, 80)
	if !strings.Contains(lines[1], fmt.Sprintf("%s  0", "mode-only.txt")) {
		t.Errorf("zero-change line = %q, want it to contain %q", lines[1], "mode-only.txt  0")
	}
}

// TestStatLinesForRightAlignsCounts pins down that the +N/-M counts column
// lines up across files with differing digit counts (e.g. "+3 -1" vs
// "+100"), padded by their plain (unstyled) width so ANSI escape bytes
// aren't miscounted as visible width.
func TestStatLinesForRightAlignsCounts(t *testing.T) {
	files := []git.File{
		{Path: "short.txt", Status: git.StatusModified, Additions: 3, Deletions: 1},
		{Path: "longer-name.txt", Status: git.StatusModified, Additions: 100},
	}
	// width 0 skips renderClipped's outer full-width padding entirely (see
	// renderClipped's own width<=0 guard), so the compared lines are just
	// each file's meaningful content — with a fixed width, every line
	// would already be padded to that width regardless of alignment,
	// making the comparison meaningless.
	lines := statLinesFor(files, 0)

	w1, w2 := lipgloss.Width(lines[1]), lipgloss.Width(lines[2])
	if w1 != w2 {
		t.Errorf("file line widths = %d, %d, want equal (name column is left-padded, counts column is right-padded, both to a fixed width)", w1, w2)
	}
}

func TestFormatCountsPlainMatchesStyledContent(t *testing.T) {
	tests := []struct {
		additions, deletions int
		wantPlain            string
	}{
		{0, 0, "0"},
		{5, 0, "+5"},
		{0, 3, "-3"},
		{5, 3, "+5 -3"},
	}
	for _, tt := range tests {
		plain, styled := formatCounts(tt.additions, tt.deletions)
		if plain != tt.wantPlain {
			t.Errorf("formatCounts(%d, %d) plain = %q, want %q", tt.additions, tt.deletions, plain, tt.wantPlain)
		}
		if lipgloss.Width(styled) != len(plain) {
			t.Errorf("formatCounts(%d, %d) styled visible width = %d, want %d (matching plain)", tt.additions, tt.deletions, lipgloss.Width(styled), len(plain))
		}
	}
}
