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
	countWidth := 0
	plainCounts := make([]string, len(files))
	styledCounts := make([]string, len(files))
	var totalAdd, totalDel int
	for i, f := range files {
		if n := len(filePathLabel(f)); n > nameWidth {
			nameWidth = n
		}
		if f.Binary {
			plainCounts[i], styledCounts[i] = "Bin", binaryNoticeStyle.Render("Bin")
		} else {
			plainCounts[i], styledCounts[i] = formatCounts(f.Additions, f.Deletions)
		}
		if n := len(plainCounts[i]); n > countWidth {
			countWidth = n
		}
		totalAdd += f.Additions
		totalDel += f.Deletions
	}

	lines := make([]string, 0, len(files)+2)
	lines = append(lines, renderClipped(fileHeaderStyle, "Files changed", width))
	for i, f := range files {
		lines = append(lines, renderStatLine(f, nameWidth, countWidth, plainCounts[i], styledCounts[i], width))
	}
	lines = append(lines, renderClipped(lipgloss.NewStyle(), statTotalsLine(len(files), totalAdd, totalDel), width))
	return lines
}

func renderStatLine(f git.File, nameWidth, countWidth int, plainCounts, styledCounts string, width int) string {
	pad := strings.Repeat(" ", countWidth-len(plainCounts))
	content := fmt.Sprintf(" %-*s  %s%s", nameWidth, filePathLabel(f), pad, styledCounts)
	return renderClipped(lipgloss.NewStyle(), content, width)
}

// formatCounts renders "+N -M" both plain (for width measurement, since
// callers need to right-align counts across files without an ANSI-aware
// width directive) and styled (coloring each half like the corresponding
// diff line op, diffAddStyle/diffDeleteStyle), omitting either half when
// its count is zero, mirroring how statTotalsLine drops a zero-valued
// clause. The two are computed together so they can't drift out of sync.
func formatCounts(additions, deletions int) (plain, styled string) {
	var plainParts, styledParts []string
	if additions > 0 {
		plainParts = append(plainParts, fmt.Sprintf("+%d", additions))
		styledParts = append(styledParts, diffAddStyle.Render(fmt.Sprintf("+%d", additions)))
	}
	if deletions > 0 {
		plainParts = append(plainParts, fmt.Sprintf("-%d", deletions))
		styledParts = append(styledParts, diffDeleteStyle.Render(fmt.Sprintf("-%d", deletions)))
	}
	if len(plainParts) == 0 {
		return "0", "0"
	}
	return strings.Join(plainParts, " "), strings.Join(styledParts, " ")
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
