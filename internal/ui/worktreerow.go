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

// renderWorktreeRow renders one synthetic leading row (working tree or staged
// changes) above the commit list. A synthetic entry has no sha/date/clock/
// author metadata, so those columns are left blank and the row stays aligned
// with the real commit rows below it, its title and file-count/+- totals
// sitting where the subject column would be. There's no searchTerm parameter:
// such a row can never be a search match, so callers pass dimmed directly.
func renderWorktreeRow(e git.WorktreeEntry, width int, selected, dimmed bool) string {
	narrow := width < narrowListWidth

	title := fmt.Sprintf("%-*s", entryTitleWidth, entryTitle(e.Kind))
	nFiles := fmt.Sprintf("%d %s", len(e.Files), pluralize(len(e.Files), "file", "files"))
	additions, deletions := diffTotals(e.Files)

	titleStyle := rowFieldStyle(entryTitleStyle, dimmed, selected)
	countStyle := rowFieldStyle(normalRowStyle, dimmed, selected)
	gap := rowGap(selected)

	// rowFieldStyle keeps diffAddStyle/diffDeleteStyle's own foreground
	// when selected, merging in just the cursor background, and swaps them
	// for dimStyle only when dimmed. A `dimmed || selected` swap here would
	// flatten the +/- coloring the moment the row is selected, even with no
	// search active.
	addStyle := rowFieldStyle(diffAddStyle, dimmed, selected)
	delStyle := rowFieldStyle(diffDeleteStyle, dimmed, selected)
	counts := formatCountsStyled(additions, deletions, addStyle, delStyle, countStyle)

	// leading mirrors renderCommitRow's sha/when columns at whichever width
	// is active, so the title and counts land where a real row's
	// author/subject would.
	leading := gap(strings.Repeat(" ", shaColumnWidth)) + gap(" ") +
		gap(strings.Repeat(" ", dateColumnWidth)) + gap(" ")
	if !narrow {
		leading += gap(strings.Repeat(" ", clockColumnWidth)) + gap(" ")
	}

	row := leading + titleStyle.Render(title) + gap("  ") +
		countStyle.Render(nFiles) + gap("  ") + counts

	return padRow(row, width, selected)
}
