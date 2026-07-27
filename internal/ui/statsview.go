package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/maiwald/gitl/internal/git"
)

// statLinesFor renders a `git show --stat`-style summary: one line per file
// with exact +N/-M counts, followed by a totals line. It's computed
// entirely from the already-parsed diff data in CommitDetail.Files (see
// git.File.Additions/Deletions in internal/git/diff.go) rather than
// shelling out to `git show --stat` a second time — that command's own
// per-file column is a proportionally-scaled visual anyway, not exact
// counts. Returns nil when files is empty (merge commits / no diff), so
// callers can skip the block entirely.
func statLinesFor(files []git.File, width int) []string {
	if len(files) == 0 {
		return nil
	}

	nameWidth := 0
	for _, f := range files {
		if n := len(filePathLabel(f)); n > nameWidth {
			nameWidth = n
		}
	}

	var totalAdd, totalDel int
	lines := make([]string, 0, len(files)+1)
	for _, f := range files {
		totalAdd += f.Additions
		totalDel += f.Deletions
		lines = append(lines, renderStatLine(f, nameWidth, width))
	}
	lines = append(lines, renderClipped(lipgloss.NewStyle(), statTotalsLine(len(files), totalAdd, totalDel), width))
	return lines
}

func renderStatLine(f git.File, nameWidth, width int) string {
	counts := binaryNoticeStyle.Render("Bin")
	if !f.Binary {
		counts = formatCounts(f.Additions, f.Deletions)
	}
	content := fmt.Sprintf(" %-*s  %s", nameWidth, filePathLabel(f), counts)
	return renderClipped(lipgloss.NewStyle(), content, width)
}

// formatCounts renders "+N -M", coloring each half like the corresponding
// diff line op (diffAddStyle/diffDeleteStyle) and omitting either half when
// its count is zero, mirroring how statTotalsLine drops a zero-valued
// clause.
func formatCounts(additions, deletions int) string {
	var parts []string
	if additions > 0 {
		parts = append(parts, diffAddStyle.Render(fmt.Sprintf("+%d", additions)))
	}
	if deletions > 0 {
		parts = append(parts, diffDeleteStyle.Render(fmt.Sprintf("-%d", deletions)))
	}
	if len(parts) == 0 {
		return "0"
	}
	return strings.Join(parts, " ")
}

// statTotalsLine mirrors git's own "N files changed, X insertions(+), Y
// deletions(-)" summary, including its pluralization and its omission of
// an insertions/deletions clause when that count is zero.
func statTotalsLine(nFiles, additions, deletions int) string {
	segs := []string{fmt.Sprintf("%d %s changed", nFiles, pluralize(nFiles, "file", "files"))}
	if additions > 0 {
		segs = append(segs, fmt.Sprintf("%d %s(+)", additions, pluralize(additions, "insertion", "insertions")))
	}
	if deletions > 0 {
		segs = append(segs, fmt.Sprintf("%d %s(-)", deletions, pluralize(deletions, "deletion", "deletions")))
	}
	return " " + strings.Join(segs, ", ")
}

func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}
