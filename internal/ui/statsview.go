package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/maiwald/gitl/internal/git"
)

// statsSectionTitle is the stats block's in-content heading, reused as-is for
// the detail view's sticky header while the stats block is in view.
const statsSectionTitle = "Files changed"

// statLinesFor renders a `git show --stat`-style summary: one line per file
// with exact +N/-M counts, then a totals line. The counts come from the
// already-parsed CommitDetail.Files rather than `git show --stat`, whose
// per-file column is a proportional visual rather than exact counts. Returns
// nil when files is empty (merge commits / no diff).
//
// Additions and deletions each get their own right-aligned column, so every
// file's "+" lines up regardless of neighboring digit counts. The totals row
// reuses the same renderStatLine layout, its "N files changed" label standing
// in for a file's name, so its counts land in the same columns.
//
// Each row comes back as a lineRenderer, leaving the cursor to the caller (see
// renderRows). The rows are in this block's own order — heading, one per file,
// a blank separator, then the totals row — which statsFileIndexAt (detail.go)
// depends on.
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

// formatCountParts renders a row's addition/deletion counts as separate
// "+N"/"-M" strings (empty when that count is zero), so statLinesFor can
// right-align each in its own column.
func formatCountParts(additions, deletions int) (add, del string) {
	if additions > 0 {
		add = fmt.Sprintf("+%d", additions)
	}
	if deletions > 0 {
		del = fmt.Sprintf("-%d", deletions)
	}
	return add, del
}

// renderStatLine renders one row of the stats table: a left-justified label (a
// file's path, or the totals row's "N files changed") followed by
// additions/deletions right-aligned into the shared addWidth/delWidth columns
// statLinesFor computes across every row. A binary file and the both-empty
// case (a mode-only change, or a zero totals row) collapse to a single
// "Bin"/"0" marker spanning both columns. The default case's content mixes
// independently-Rendered spans, so it's clipped via renderClippedStyled.
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
			// stay unhighlighted: the outer fill only reaches the first
			// embedded ANSI reset (see padRow), which the styled "+N" puts
			// mid-row.
			plain = plain.Background(cursorRowBg)
		}
		addPad := strings.Repeat(" ", addWidth-len(addCount))
		delPad := strings.Repeat(" ", delWidth-len(delCount))
		content := plain.Render(name+addPad) + addStyle.Render(addCount) + plain.Render(" "+delPad) + delStyle.Render(delCount)
		return renderClippedStyled(content, width, cursor)
	}
}

// formatCountsStyled renders "+N -M" as one styled string, coloring each half
// via addStyle/delStyle and omitting either when its count is zero. Used where
// a single joined value is all that's needed, e.g. a worktree row's summary;
// statLinesFor's table keeps the two apart via formatCountParts instead.
//
// The styles are parameters so a selected or dimmed row can swap in its own
// per-state versions. sep styles the literal space joining the two halves,
// needed for the same ANSI-reset reason as rowFieldStyle's separators.
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

func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}
