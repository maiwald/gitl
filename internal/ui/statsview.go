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
//
// matches (from FindStatMatches, in the same file order this walks) are
// highlighted within their row's file label unless dismissed is true;
// activeMatch indexes into them for the one n/N has selected (-1 for none,
// ignored when dismissed). matchLines is parallel to matches — the logical
// line each sits on, counted from this block's own start — and is filled in
// regardless of dismissed, so a dismissed match still lands correctly once
// n/N revives it.
func statLinesFor(files []git.File, width int, matches []StatMatch, activeMatch int, dismissed bool) (lines []lineRenderer, matchLines []int) {
	if len(files) == 0 {
		return nil, nil
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

	mc := newMatchCursor[int](matches, activeMatch, dismissed)

	lines = make([]lineRenderer, 0, len(files)+3)
	lines = append(lines, clippedLine(fileHeaderStyle, statsSectionTitle, width))
	for i, f := range files {
		rowMatches := mc.take(len(lines), i)
		lines = append(lines, statRow(filePathLabel(f), f.Binary, nameWidth, addWidth, delWidth, addCounts[i], delCounts[i], rowMatches, width))
	}
	lines = append(lines, blankRow(width))
	// The totals row's label names no file, so it never carries a match — see
	// FindStatMatches.
	lines = append(lines, statRow(summaryLabel, false, nameWidth, addWidth, delWidth, totalAddCount, totalDelCount, nil, width))
	return lines, mc.lines
}

// StatMatch identifies one case-insensitive occurrence of a search term within
// a stats row's file label. StartCol/EndCol are byte offsets into
// filePathLabel(files[FileIndex]) — the same string the diff's own file header
// matches against, so a name shown in both blocks is found once per place it
// appears.
type StatMatch struct {
	FileIndex        int
	StartCol, EndCol int
}

func (m StatMatch) at() int                { return m.FileIndex }
func (m StatMatch) cols() (start, end int) { return m.StartCol, m.EndCol }

// FindStatMatches returns every occurrence of term across the stats block's
// per-file rows, in the file order statLinesFor emits them — an invariant the
// two rely on to stay in sync, the same way FindDiffMatches and RenderDiff do.
//
// Only the file rows are searched. The heading and the totals row name no
// file, so a term matching "Files changed" or "3 files changed" would put a
// hit on a row that says nothing about where the term occurs. Returns nil for
// an empty term.
func FindStatMatches(files []git.File, term string) []StatMatch {
	if term == "" {
		return nil
	}
	var matches []StatMatch
	for fi, f := range files {
		for _, occ := range findOccurrences(filePathLabel(f), term) {
			matches = append(matches, StatMatch{FileIndex: fi, StartCol: occ[0], EndCol: occ[1]})
		}
	}
	return matches
}

// statRow is the lineRenderer for one row of the stats table (see
// renderStatLine, which does the rendering once the cursor is known).
func statRow(label string, binary bool, nameWidth, addWidth, delWidth int, addCount, delCount string, matches []lineMatch, width int) lineRenderer {
	return lineRenderer{
		text: statLineText(label, binary, nameWidth, addWidth, delWidth, addCount, delCount),
		render: func(cursor bool) []string {
			return []string{renderStatLine(label, binary, nameWidth, addWidth, delWidth, addCount, delCount, matches, width, cursor)}
		},
	}
}

// statLineText lays out one stats row as plain text: what renderStatLine
// styles piecewise, and what a selection copies. The two marker cases are
// rendered from this string directly; the default case has to re-assemble the
// same layout around its independently-styled counts, which is what
// TestStatLineTextMatchesRenderedRow pins.
func statLineText(label string, binary bool, nameWidth, addWidth, delWidth int, addCount, delCount string) string {
	name := fmt.Sprintf(" %-*s  ", nameWidth, label)
	switch {
	case binary:
		return name + padLeft("Bin", addWidth+1+delWidth)
	case addCount == "" && delCount == "":
		return name + padLeft("0", addWidth+1+delWidth)
	default:
		return name + padLeft(addCount, addWidth) + " " + padLeft(delCount, delWidth)
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
//
// matches are byte offsets into label, highlighted inline wherever the label
// lands in the row.
func renderStatLine(label string, binary bool, nameWidth, addWidth, delWidth int, addCount, delCount string, matches []lineMatch, width int, cursor bool) string {
	name := fmt.Sprintf(" %-*s  ", nameWidth, label)

	// name puts a single space in front of the label the offsets were found
	// against, so every match sits one byte further along the row.
	if len(matches) > 0 {
		matches = shiftMatches(matches, shiftBy(1))
	}

	switch {
	case binary, addCount == "" && delCount == "":
		text := statLineText(label, binary, nameWidth, addWidth, delWidth, addCount, delCount)
		return statMarkerRow(text, matches, width, cursor)
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
		content := statText(name+addPad, matches, cursor) + addStyle.Render(addCount) + plain.Render(" "+delPad) + delStyle.Render(delCount)
		return renderClippedStyled(content, width, cursor)
	}
}

// statMarkerRow renders the rows that collapse both count columns into a
// single "Bin"/"0" marker, where the whole row is one uniform style. With no
// matches it keeps renderClipped's single Render; a highlighted match puts
// embedded ANSI resets mid-row, which Width/MaxWidth would miscount, so those
// clip through padRow instead (see renderClippedStyled).
func statMarkerRow(text string, matches []lineMatch, width int, cursor bool) string {
	if len(matches) == 0 {
		return renderClipped(cursorMerged(lipgloss.NewStyle(), cursor), text, width)
	}
	return renderClippedStyled(statText(text, matches, cursor), width, cursor)
}

// statText renders a stretch of a stats row that carries no color of its own,
// with any search matches within it highlighted. Without matches that's the
// single Render the row always cost.
func statText(text string, matches []lineMatch, cursor bool) string {
	if len(matches) == 0 {
		return cursorMerged(lipgloss.NewStyle(), cursor).Render(text)
	}
	return renderLineWithBaseSpans(text, nil, matches, plainLine, cursor)
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
	add, del := formatCountParts(additions, deletions)
	var parts []string
	if add != "" {
		parts = append(parts, addStyle.Render(add))
	}
	if del != "" {
		parts = append(parts, delStyle.Render(del))
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
