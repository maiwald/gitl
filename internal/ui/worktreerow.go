package ui

import (
	"fmt"
	"strings"

	"github.com/maiwald/gitl/internal/git"
)

// entryTitleWidth pads a synthetic row's title so the file-count/+- total
// that follows lines up the same way for both "Working tree" and "Staged
// changes".
const entryTitleWidth = 16

// renderWorktreeRow renders one synthetic leading row (working tree or
// staged changes) above the commit list. The sha/date/clock/author columns
// renderCommitRow uses are left blank — a synthetic entry has none of that
// metadata — so the row's columns stay aligned with the real commit rows
// below it, with the title and its file-count/+- totals sitting where the
// subject column would be. There's no searchTerm/isActiveMatch parameter:
// a synthetic row can never be a search match, so dimmed is always passed
// directly rather than computed here (see renderCommitRow's doc comment).
func renderWorktreeRow(e git.WorktreeEntry, width int, selected, dimmed bool) string {
	narrow := width < narrowListWidth

	title := fmt.Sprintf("%-*s", entryTitleWidth, entryTitle(e.Kind))
	nFiles := fmt.Sprintf("%d %s", len(e.Files), pluralize(len(e.Files), "file", "files"))
	additions, deletions := diffTotals(e.Files)

	titleStyle := rowFieldStyle(entryTitleStyle, dimmed, selected)
	countStyle := rowFieldStyle(normalRowStyle, dimmed, selected)
	gap := rowGap(selected)

	// rowFieldStyle keeps diffAddStyle/diffDeleteStyle's own foreground
	// when selected (merging in just the cursor background), and only
	// swaps them out wholesale for dimStyle when dimmed — the same
	// treatment every other field on the row gets. A plain `dimmed ||
	// selected` swap here would flatten the +/- coloring to countStyle
	// the moment the row is selected, even without a search active.
	addStyle := rowFieldStyle(diffAddStyle, dimmed, selected)
	delStyle := rowFieldStyle(diffDeleteStyle, dimmed, selected)
	counts := formatCountsStyled(additions, deletions, addStyle, delStyle, countStyle)

	// leading mirrors renderCommitRow's sha/when blank columns exactly, at
	// whichever width is active, so the title/counts that follow keep
	// landing where a real row's author/subject would.
	leading := gap(strings.Repeat(" ", shaColumnWidth)) + gap(" ") +
		gap(strings.Repeat(" ", dateColumnWidth)) + gap(" ")
	if !narrow {
		leading += gap(strings.Repeat(" ", clockColumnWidth)) + gap(" ")
	}

	row := leading + titleStyle.Render(title) + gap("  ") +
		countStyle.Render(nFiles) + gap("  ") + counts

	return padRow(row, width, rowFillStyle(selected))
}
