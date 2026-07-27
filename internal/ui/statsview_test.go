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

func TestStatLinesForIncludesTotalsAsLastLine(t *testing.T) {
	files := []git.File{
		{Path: "a.txt", Status: git.StatusModified, Additions: 3, Deletions: 1},
		{Path: "b.txt", Status: git.StatusAdded, Additions: 10},
	}
	lines := statLinesFor(files, 80)
	if len(lines) != len(files)+1 {
		t.Fatalf("got %d lines, want %d (one per file plus totals)", len(lines), len(files)+1)
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

	if !strings.Contains(lines[0], diffAddStyle.Render("+5")) {
		t.Errorf("added.txt line = %q, want it to contain +5 styled with diffAddStyle", lines[0])
	}
	if strings.Contains(lines[0], "-0") {
		t.Errorf("added.txt line = %q, want no deletions clause when deletions is 0", lines[0])
	}

	if !strings.Contains(lines[1], diffDeleteStyle.Render("-2")) {
		t.Errorf("removed.txt line = %q, want it to contain -2 styled with diffDeleteStyle", lines[1])
	}
	if strings.Contains(lines[1], "+0") {
		t.Errorf("removed.txt line = %q, want no insertions clause when additions is 0", lines[1])
	}

	if !strings.Contains(lines[2], diffAddStyle.Render("+1")) || !strings.Contains(lines[2], diffDeleteStyle.Render("-1")) {
		t.Errorf("both.txt line = %q, want both +1 and -1 styled", lines[2])
	}
}

func TestStatLinesForBinaryFileShowsBin(t *testing.T) {
	files := []git.File{{Path: "image.png", Status: git.StatusModified, Binary: true}}
	lines := statLinesFor(files, 80)
	if !strings.Contains(lines[0], "Bin") {
		t.Errorf("binary file line = %q, want it to contain Bin", lines[0])
	}
	if strings.Contains(lines[0], "+") || strings.Contains(lines[0], "-") {
		t.Errorf("binary file line = %q, want no +/- counts", lines[0])
	}
}

func TestStatLinesForRenameShowsArrow(t *testing.T) {
	files := []git.File{{OldPath: "old.txt", Path: "new.txt", Status: git.StatusRenamed}}
	lines := statLinesFor(files, 80)
	for _, sub := range []string{"old.txt", "->", "new.txt"} {
		if !strings.Contains(lines[0], sub) {
			t.Errorf("rename line = %q, want it to contain %q", lines[0], sub)
		}
	}
}

func TestStatLinesForClipsToWidth(t *testing.T) {
	files := []git.File{{Path: strings.Repeat("x", 100), Status: git.StatusModified, Additions: 1234}}
	const width = 20
	lines := statLinesFor(files, width)
	if got := lipgloss.Width(lines[0]); got > width {
		t.Errorf("rendered width = %d, want <= %d (clipped, not wrapped)", got, width)
	}
}

func TestStatLinesForZeroChangeFileShowsZero(t *testing.T) {
	files := []git.File{{Path: "mode-only.txt", Status: git.StatusModified}}
	lines := statLinesFor(files, 80)
	if !strings.Contains(lines[0], fmt.Sprintf("%s  0", "mode-only.txt")) {
		t.Errorf("zero-change line = %q, want it to contain %q", lines[0], "mode-only.txt  0")
	}
}
