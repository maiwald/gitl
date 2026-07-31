package ui

import (
	"regexp"
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

// TestStatLinesForTotalsPluralizationAndOmission pins down the totals row's
// "N file(s) changed" label and its +N/-M clauses (each omitted when that
// side's sum is zero), the same way an individual file's own counts omit a
// zero side.
func TestStatLinesForTotalsPluralizationAndOmission(t *testing.T) {
	tests := []struct {
		name      string
		files     []git.File
		wantLabel string
		wantAdd   string // "" means the styled +N clause must be absent
		wantDel   string // "" means the styled -M clause must be absent
	}{
		{"single file no changes", []git.File{{Path: "a.txt"}}, "1 file changed", "", ""},
		{"single insertion", []git.File{{Path: "a.txt", Additions: 1}}, "1 file changed", "+1", ""},
		{"single deletion", []git.File{{Path: "a.txt", Deletions: 1}}, "1 file changed", "", "-1"},
		{
			"many files both",
			[]git.File{{Path: "a.txt", Additions: 40, Deletions: 7}, {Path: "b.txt", Additions: 2}},
			"2 files changed", "+42", "-7",
		},
		{
			"insertions only, many",
			[]git.File{{Path: "a.txt", Additions: 3}, {Path: "b.txt", Additions: 2}},
			"2 files changed", "+5", "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			total := statLinesFor(tt.files, 80)
			last := total[len(total)-1]
			if !strings.Contains(last, tt.wantLabel) {
				t.Errorf("totals line = %q, want it to contain %q", last, tt.wantLabel)
			}
			if tt.wantAdd != "" && !strings.Contains(last, diffAddStyle.Render(tt.wantAdd)) {
				t.Errorf("totals line = %q, want it to contain %s styled with diffAddStyle", last, tt.wantAdd)
			}
			if tt.wantDel != "" && !strings.Contains(last, diffDeleteStyle.Render(tt.wantDel)) {
				t.Errorf("totals line = %q, want it to contain %s styled with diffDeleteStyle", last, tt.wantDel)
			}
		})
	}
}

// TestStatLinesForTotalsRowAlignsWithFiles pins down the "bonus" alignment:
// the totals row runs through the exact same renderStatLine column layout
// (shared nameWidth/addWidth/delWidth) as every file row above it, so its
// own +N/-M lands in the identical columns rather than floating wherever a
// prose sentence happens to put it. The blank separator line between the
// files and the totals row is deliberately excluded from this comparison —
// it's a spacer, not a column-bearing row.
func TestStatLinesForTotalsRowAlignsWithFiles(t *testing.T) {
	files := []git.File{
		{Path: "a.txt", Status: git.StatusModified, Additions: 3, Deletions: 1},
		{Path: "b.txt", Status: git.StatusModified, Additions: 120, Deletions: 30},
	}
	lines := statLinesFor(files, 0)

	want := lipgloss.Width(lines[1])
	last := len(lines) - 1
	for i := 1; i < len(lines); i++ {
		if i == last-1 {
			continue // blank separator line before the totals row
		}
		if got := lipgloss.Width(lines[i]); got != want {
			t.Errorf("lines[%d] width = %d, want %d (every row, including totals, shares one column layout)", i, got, want)
		}
	}
}

// TestStatLinesForStructure pins down the block's shape: a "Files changed"
// header bar (matching the diff's per-file header bar style), one line per
// file, a blank separator, then the totals line last.
func TestStatLinesForStructure(t *testing.T) {
	files := []git.File{
		{Path: "a.txt", Status: git.StatusModified, Additions: 3, Deletions: 1},
		{Path: "b.txt", Status: git.StatusAdded, Additions: 10},
	}
	lines := statLinesFor(files, 80)
	if len(lines) != len(files)+3 {
		t.Fatalf("got %d lines, want %d (header bar + one per file + blank separator + totals)", len(lines), len(files)+3)
	}
	if sep := lines[len(lines)-2]; sep != "" {
		t.Errorf("separator line = %q, want blank line between files and totals", sep)
	}

	if !strings.Contains(lines[0], "Files changed") {
		t.Errorf("lines[0] = %q, want the header bar text", lines[0])
	}
	if lines[0] != renderClipped(fileHeaderStyle, "Files changed", 80) {
		t.Errorf("lines[0] = %q, want it styled with fileHeaderStyle (same as a diff file's header bar)", lines[0])
	}

	last := lines[len(lines)-1]
	if !strings.Contains(last, "2 files changed") {
		t.Errorf("last line = %q, want it to contain the file count", last)
	}
	if !strings.Contains(last, diffAddStyle.Render("+13")) || !strings.Contains(last, diffDeleteStyle.Render("-1")) {
		t.Errorf("last line = %q, want it to contain the summed +13/-1 counts", last)
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
	// The name column's width can grow to fit the totals row's own "N
	// files changed" label, so the gap before "0" isn't a fixed two
	// spaces — just check the name is followed by whitespace then "0".
	if !regexp.MustCompile(`mode-only\.txt\s+0`).MatchString(lines[1]) {
		t.Errorf("zero-change line = %q, want it to contain %q followed by whitespace and 0", lines[1], "mode-only.txt")
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

func TestFormatCountsStyledPlainMatchesStyledContent(t *testing.T) {
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
		plain, styled := formatCountsStyled(tt.additions, tt.deletions, diffAddStyle, diffDeleteStyle, lipgloss.NewStyle())
		if plain != tt.wantPlain {
			t.Errorf("formatCountsStyled(%d, %d) plain = %q, want %q", tt.additions, tt.deletions, plain, tt.wantPlain)
		}
		if lipgloss.Width(styled) != len(plain) {
			t.Errorf("formatCountsStyled(%d, %d) styled visible width = %d, want %d (matching plain)", tt.additions, tt.deletions, lipgloss.Width(styled), len(plain))
		}
	}
}
