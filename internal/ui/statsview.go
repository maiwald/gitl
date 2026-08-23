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

// statLinesFor renders a `git show --stat`-style summary: one line per
// file with exact +N/-M counts, then a totals line — computed from the
// already-parsed CommitDetail.Files rather than re-shelling to `git show
// --stat`, whose own per-file column is a proportional visual, not exact
// counts. Returns nil when files is empty (merge commits / no diff).
//
// Additions and deletions each get their own right-aligned column (not one
// combined "+N -M" block), so every file's "+" lines up regardless of
// neighboring digit counts. The totals row reuses the same renderStatLine
// column layout — its "N files changed" label standing in for a file's
// name — instead of git's own prose summary, so its counts land in the
// same columns. A blank line separates it from the per-file rows (the same
// convention detail.go's reflow uses between blocks).
//
// Each row comes back as a lineRenderer, so which one the cursor is on is
// decided by the caller once it knows where they landed (see renderRows) —
// the same arrangement RenderDiff/renderHeaderLines use for their own
// content. The rows are in this block's own order: the "Files changed"
// heading, one per file, a blank separator, then the totals row — a local
// line convention statsFileIndexAt (detail.go) depends on.
func statLinesFor(files []git.File, width int) []lineRenderer {
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
		nameWidth = max(nameWidth, len(filePathLabel(f)))
		if !f.Binary {
			addCounts[i], delCounts[i] = formatCountParts(f.Additions, f.Deletions)
			addWidth = max(addWidth, len(addCounts[i]))
			delWidth = max(delWidth, len(delCounts[i]))
		}
		totalAdd += f.Additions
		totalDel += f.Deletions
	}

	totalAddCount, totalDelCount := formatCountParts(totalAdd, totalDel)
	addWidth = max(addWidth, len(totalAddCount))
	delWidth = max(delWidth, len(totalDelCount))

	summaryLabel := fmt.Sprintf("%d %s changed", len(files), pluralize(len(files), "file", "files"))
	nameWidth = max(nameWidth, len(summaryLabel))

	lines := make([]lineRenderer, 0, len(files)+3)
	lines = append(lines, clippedLine(fileHeaderStyle, statsSectionTitle, width))
	for i, f := range files {
		lines = append(lines, statRow(filePathLabel(f), f.Binary, nameWidth, addWidth, delWidth, addCounts[i], delCounts[i], width))
	}
	lines = append(lines, blankRow(width))
	lines = append(lines, statRow(summaryLabel, false, nameWidth, addWidth, delWidth, totalAddCount, totalDelCount, width))
	return lines
}

// statRow is the lineRenderer for one row of the stats table (see
// renderStatLine, which does the rendering once the cursor is known).
func statRow(label string, binary bool, nameWidth, addWidth, delWidth int, addCount, delCount string, width int) lineRenderer {
	return func(cursor bool) []string {
		return []string{renderStatLine(label, binary, nameWidth, addWidth, delWidth, addCount, delCount, width, cursor)}
	}
}

// formatCountParts renders a file's or totals row's addition/deletion
// counts as separate "+N"/"-M" strings (empty when that count is zero), so
// statLinesFor can right-align additions and deletions in their own
// columns rather than as one combined string.
func formatCountParts(additions, deletions int) (add, del string) {
	if additions > 0 {
		add = fmt.Sprintf("+%d", additions)
	}
	if deletions > 0 {
		del = fmt.Sprintf("-%d", deletions)
	}
	return add, del
}

// renderStatLine renders one row of the stats table: a left-justified
// label (a file's path, or the totals row's "N files changed") followed
// by additions/deletions right-aligned into the shared addWidth/delWidth
// columns statLinesFor computes across every row. binary and the
// both-empty case (a mode-only change, or an all-binary/zero totals row)
// each collapse to a single "Bin"/"0" marker, right-aligned across both
// columns combined. The default case's content mixes independently-
// Rendered spans (the styled counts), so it's clipped via
// renderClippedStyled, not renderClipped — see that helper's doc comment
// for why. cursor merges the cursor row's background across the whole
// line, the same treatment every other row kind in the detail view gets.
func renderStatLine(label string, binary bool, nameWidth, addWidth, delWidth int, addCount, delCount string, width int, cursor bool) string {
	name := fmt.Sprintf(" %-*s  ", nameWidth, label)
	combinedWidth := addWidth + 1 + delWidth

	switch {
	case binary:
		return renderClipped(cursorMerged(lipgloss.NewStyle(), cursor), name+padLeft("Bin", combinedWidth), width)
	case addCount == "" && delCount == "":
		return renderClipped(cursorMerged(lipgloss.NewStyle(), cursor), name+padLeft("0", combinedWidth), width)
	default:
		addStyle, delStyle, plain := diffAddStyle, diffDeleteStyle, lipgloss.NewStyle()
		if cursor {
			addStyle = addStyle.Background(cursorRowBg)
			delStyle = delStyle.Background(cursorRowBg)
			// The label, the column padding and the separator between the
			// two count columns are plain text sitting *between*
			// independently-Rendered spans, so they need the cursor
			// background applied here, before rendering. Left plain, they'd
			// stay unhighlighted: the outer fill renderClippedStyled wraps
			// the finished row in only reaches the first embedded ANSI reset
			// (see padRow), which the styled "+N" puts right in the middle of
			// the row — leaving a visible gap across the rest of it.
			plain = plain.Background(cursorRowBg)
		}
		addPad := strings.Repeat(" ", addWidth-len(addCount))
		delPad := strings.Repeat(" ", delWidth-len(delCount))
		content := plain.Render(name+addPad) + addStyle.Render(addCount) + plain.Render(" "+delPad) + delStyle.Render(delCount)
		return renderClippedStyled(content, width, cursor)
	}
}

// formatCountsStyled renders "+N -M" as one styled string, coloring each
// half via addStyle/delStyle and omitting either when its count is zero.
// Used where a single joined value is all that's needed (e.g. a worktree
// row's summary); statLinesFor's own table instead keeps additions and
// deletions apart via formatCountParts, so each gets its own column.
//
// addStyle/delStyle are parameterized (rather than always the plain
// diffAddStyle/diffDeleteStyle) so a selected/dimmed row elsewhere (e.g.
// renderWorktreeRow) can swap in its own per-state styles the same way
// every other per-field style on such a row does. sep styles the literal
// space joining the two halves, needed for the same ANSI-reset reason as
// rowFieldStyle's separators.
func formatCountsStyled(additions, deletions int, addStyle, delStyle, sep lipgloss.Style) string {
	var parts []string
	if additions > 0 {
		parts = append(parts, addStyle.Render(fmt.Sprintf("+%d", additions)))
	}
	if deletions > 0 {
		parts = append(parts, delStyle.Render(fmt.Sprintf("-%d", deletions)))
	}
	if len(parts) == 0 {
		return "0"
	}
	return strings.Join(parts, sep.Render(" "))
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
