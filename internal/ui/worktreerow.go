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
// subject column would be. There's no searchTerm/isActiveMatch parameter
// here: a synthetic row can never be a search match (matches come from git
// log queries over real commits only), which is exactly why dimmed is
// passed whenever a search is active, rather than computed from the row's
// own text.
func renderWorktreeRow(e git.WorktreeEntry, width int, selected, dimmed bool) string {
	title := fmt.Sprintf("%-*s", entryTitleWidth, entryTitle(e.Kind))
	nFiles := fmt.Sprintf("%d %s", len(e.Files), pluralize(len(e.Files), "file", "files"))
	additions, deletions := diffTotals(e.Files)

	titleStyle := rowFieldStyle(entryTitleStyle, dimmed, selected)
	countStyle := rowFieldStyle(normalRowStyle, dimmed, selected)
	gap := rowGap(selected)

	addStyle, delStyle := diffAddStyle, diffDeleteStyle
	if dimmed || selected {
		// Once dimmed or selected, the counts' own coloring is replaced
		// wholesale by countStyle (which already carries the cursor
		// background when selected), the same way every other field on a
		// selected/dimmed row swaps its whole base style rather than
		// layering onto its normal one — see rowFieldStyle.
		addStyle, delStyle = countStyle, countStyle
	}
	_, counts := formatCountsStyled(additions, deletions, addStyle, delStyle)

	row := gap(strings.Repeat(" ", shaColumnWidth)) + gap("  ") +
		gap(strings.Repeat(" ", dateColumnWidth)) + gap(" ") +
		gap(strings.Repeat(" ", clockColumnWidth)) + gap("  ") +
		titleStyle.Render(title) + gap("  ") +
		countStyle.Render(nFiles) + gap("  ") + counts

	return padRow(row, width, rowFillStyle(selected))
}
