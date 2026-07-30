package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/maiwald/gitl/internal/git"
)

// statsSectionTitle is the stats block's in-content heading, reused as-is
// for the detail view's sticky header (detail.go) while the stats block is
// in view, so the two stay identical without a second literal to drift.
const statsSectionTitle = "Files changed"

// statLinesFor renders a `git show --stat`-style summary: one line per file
// with exact +N/-M counts, followed by a totals line. It's computed
// entirely from the already-parsed diff data in CommitDetail.Files (see
// git.File.Additions/Deletions in internal/git/diff.go) rather than
// shelling out to `git show --stat` a second time — that command's own
// per-file column is a proportionally-scaled visual anyway, not exact
// counts. Returns nil when files is empty (merge commits / no diff), so
// callers can skip the block entirely.
//
// Additions and deletions get their own right-aligned column each (rather
// than right-aligning the combined "+N -M" string as one block), so a
// file's "+" lines up with every other file's "+" regardless of how many
// digits its neighbors' counts have — e.g. "+3" above "+120" stay aligned
// on their own column instead of drifting because "+120" is longer. The
// totals line is rendered through the same renderStatLine column layout
// (its "N files changed" label standing in for a file's name, and the
// summed counts running through the same formatCountParts/padding logic),
// rather than git's own prose "X insertions(+), Y deletions(-)" — so the
// totals' own +/- counts land in the exact same columns as every file
// above them instead of floating at whatever column a sentence happens to
// put them. A blank line separates the per-file rows from the totals row
// (the same blank-line convention detail.go's reflow uses between the
// header/stats/diff blocks), so the totals row reads as a summary set
// apart from the files rather than one more row among them, while still
// sharing their column layout.
func statLinesFor(files []git.File, width int) []string {
	if len(files) == 0 {
		return nil
	}

	nameWidth := 0
	addWidth := 0
	delWidth := 0
	addCounts := make([]string, len(files))
	delCounts := make([]string, len(files))
	var totalAdd, totalDel int
	for i, f := range files {
		if n := len(filePathLabel(f)); n > nameWidth {
			nameWidth = n
		}
		if !f.Binary {
			addCounts[i], delCounts[i] = formatCountParts(f.Additions, f.Deletions)
			if n := len(addCounts[i]); n > addWidth {
				addWidth = n
			}
			if n := len(delCounts[i]); n > delWidth {
				delWidth = n
			}
		}
		totalAdd += f.Additions
		totalDel += f.Deletions
	}

	totalAddCount, totalDelCount := formatCountParts(totalAdd, totalDel)
	if n := len(totalAddCount); n > addWidth {
		addWidth = n
	}
	if n := len(totalDelCount); n > delWidth {
		delWidth = n
	}

	summaryLabel := fmt.Sprintf("%d %s changed", len(files), pluralize(len(files), "file", "files"))
	if n := len(summaryLabel); n > nameWidth {
		nameWidth = n
	}

	lines := make([]string, 0, len(files)+3)
	lines = append(lines, renderClipped(fileHeaderStyle, statsSectionTitle, width))
	for i, f := range files {
		lines = append(lines, renderStatLine(filePathLabel(f), f.Binary, nameWidth, addWidth, delWidth, addCounts[i], delCounts[i], width))
	}
	lines = append(lines, "")
	lines = append(lines, renderStatLine(summaryLabel, false, nameWidth, addWidth, delWidth, totalAddCount, totalDelCount, width))
	return lines
}

// formatCountParts renders a file's or totals row's addition/deletion
// counts as separate "+N"/"-M" strings (empty when that count is zero), so
// statLinesFor can right-align additions and deletions in their own
// columns rather than as one combined string. Compare formatCounts, which
// joins the same two pieces into a single value for callers (e.g. a
// worktree row's summary) that just need one combined "+N -M" string.
func formatCountParts(additions, deletions int) (add, del string) {
	if additions > 0 {
		add = fmt.Sprintf("+%d", additions)
	}
	if deletions > 0 {
		del = fmt.Sprintf("-%d", deletions)
	}
	return add, del
}

// renderStatLine renders one row of the stats table: a left-justified label
// (a file's path, or the totals line's "N files changed") followed by its
// additions/deletions right-aligned into the shared addWidth/delWidth
// columns computed across every row by statLinesFor, so every row's counts
// land in the same columns regardless of how many digits they or their
// neighbors have. binary and the case where both counts are empty (a
// mode-only change with no diff content, or an all-binary/all-zero totals
// row) each collapse to a single marker ("Bin" / "0") right-aligned across
// the combined width of both columns, since there's nothing to split
// between them.
func renderStatLine(label string, binary bool, nameWidth, addWidth, delWidth int, addCount, delCount string, width int) string {
	name := fmt.Sprintf(" %-*s  ", nameWidth, label)
	combinedWidth := addWidth + 1 + delWidth

	switch {
	case binary:
		return renderClipped(lipgloss.NewStyle(), name+padLeft("Bin", combinedWidth), width)
	case addCount == "" && delCount == "":
		return renderClipped(lipgloss.NewStyle(), name+padLeft("0", combinedWidth), width)
	default:
		addPad := strings.Repeat(" ", addWidth-len(addCount))
		delPad := strings.Repeat(" ", delWidth-len(delCount))
		content := fmt.Sprintf("%s%s%s %s%s", name, addPad, diffAddStyle.Render(addCount), delPad, diffDeleteStyle.Render(delCount))
		return renderClipped(lipgloss.NewStyle(), content, width)
	}
}

// padLeft right-aligns s within width, left-padding with spaces. It's a
// no-op (never truncates) when s is already at or beyond width.
func padLeft(s string, width int) string {
	if n := width - len(s); n > 0 {
		return strings.Repeat(" ", n) + s
	}
	return s
}

// formatCounts renders "+N -M" both plain (for width measurement, since
// callers need to right-align counts across files without an ANSI-aware
// width directive) and styled (coloring each half like the corresponding
// diff line op, diffAddStyle/diffDeleteStyle), omitting either half when
// its count is zero. The two are computed together so they can't drift out
// of sync. Used where only a single joined value is needed (e.g. a
// worktree row's summary); statLinesFor's own table instead keeps additions
// and deletions apart via formatCountParts, so each gets its own column.
func formatCounts(additions, deletions int) (plain, styled string) {
	return formatCountsStyled(additions, deletions, diffAddStyle, diffDeleteStyle)
}

// formatCountsStyled is formatCounts generalized over the add/delete
// styles used for the styled half, so a selected/dimmed row elsewhere (e.g.
// renderWorktreeRow) can swap in its own per-state styles the same way
// every other per-field style on such a row does, rather than always using
// the plain diffAddStyle/diffDeleteStyle colors.
func formatCountsStyled(additions, deletions int, addStyle, delStyle lipgloss.Style) (plain, styled string) {
	var plainParts, styledParts []string
	if additions > 0 {
		plainParts = append(plainParts, fmt.Sprintf("+%d", additions))
		styledParts = append(styledParts, addStyle.Render(fmt.Sprintf("+%d", additions)))
	}
	if deletions > 0 {
		plainParts = append(plainParts, fmt.Sprintf("-%d", deletions))
		styledParts = append(styledParts, delStyle.Render(fmt.Sprintf("-%d", deletions)))
	}
	if len(plainParts) == 0 {
		return "0", "0"
	}
	return strings.Join(plainParts, " "), strings.Join(styledParts, " ")
}

// diffTotals sums additions/deletions across files — used by
// renderWorktreeRow's own +A -D summary (statLinesFor accumulates its
// per-file totals inline, alongside the width computation its own loop
// already does, so it doesn't go through this).
func diffTotals(files []git.File) (additions, deletions int) {
	for _, f := range files {
		additions += f.Additions
		deletions += f.Deletions
	}
	return additions, deletions
}

func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}
