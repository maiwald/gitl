package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/maiwald/gitl/internal/git"
)

const (
	authorColumnWidth = 18
	// shaColumnWidth is deliberately longer than git's default abbreviated
	// hash (7 chars) to make same-prefix commits easier to tell apart at a
	// glance.
	shaColumnWidth = 10
)

// renderCommitRow renders a single commit list row: abbreviated sha, date,
// local time, author, ref decorations, and the commit subject,
// truncated/padded to width. searchTerm highlights matching substrings
// within the row (case insensitive); isActiveMatch picks a visually
// distinct style for whichever match n/N currently points at. A commit can
// be a search match via its full message body, which isn't shown in the
// row at all — in that case there's simply nothing here to highlight,
// which is expected.
//
// dimmed fades a row that isn't among the search's real matches (as
// determined by the actual git query, not by whether this row's visible
// text happens to contain the term — see ListModel.render) — this is what
// makes a body-only match (nothing highlighted in the row itself) visually
// distinguishable from an outright non-match. It replaces each field's own
// color with dimStyle rather than wrapping the whole rendered row in an
// outer faint style: every field below is already independently
// Render()'d, and each Render() call ends with its own ANSI reset, which
// would cancel an outer style applied around the concatenated result (see
// the same concern in formatRefs below) — so faintness has to be baked
// into each field's own style instead.
func renderCommitRow(c git.Commit, width int, selected, dimmed bool, searchTerm string, isActiveMatch bool) string {
	sha := fmt.Sprintf("%-*s", shaColumnWidth, shaPrefix(c.Hash))
	localDate := c.AuthorDate.Local()
	date := localDate.Format("2006-01-02")
	clock := localDate.Format("15:04")
	author := fmt.Sprintf("%-*s", authorColumnWidth, truncate(c.AuthorName, authorColumnWidth))
	subject := c.Subject

	shaBase, dateBase, authorBase, subjectBase := shaStyle, dateStyle, authorStyle, lipgloss.NewStyle()
	if dimmed {
		shaBase, dateBase, authorBase, subjectBase = dimStyle, dimStyle, dimStyle, dimStyle
	}
	gapStyle := normalRowStyle
	if selected {
		// Merged into each field's own base style (and the literal
		// separators via gapStyle) rather than applied as one flat style
		// wrapped around the finished row: every field below ends its own
		// Render() call with an ANSI reset, which would cancel a
		// single outer background the moment the first one fires (see the
		// same reset hazard called out in formatRefs).
		shaBase = shaBase.Background(cursorRowBg)
		dateBase = dateBase.Background(cursorRowBg)
		authorBase = authorBase.Background(cursorRowBg)
		subjectBase = subjectBase.Background(cursorRowBg)
		gapStyle = gapStyle.Background(cursorRowBg)
	}

	if !selected {
		matchStyle := searchMatchStyle
		if isActiveMatch {
			matchStyle = searchActiveMatchStyle
		}
		sha = highlightSpans(sha, searchTerm, shaBase, matchStyle)
		date = highlightSpans(date, searchTerm, dateBase, matchStyle)
		clock = highlightSpans(clock, searchTerm, dateBase, matchStyle)
		author = highlightSpans(author, searchTerm, authorBase, matchStyle)
		subject = highlightSpans(subject, searchTerm, subjectBase, matchStyle)
	} else {
		sha = shaBase.Render(sha)
		date = dateBase.Render(date)
		clock = dateBase.Render(clock)
		author = authorBase.Render(author)
		subject = subjectBase.Render(subject)
	}
	refs := formatRefs(c.Refs, selected, dimmed)
	gap := func(s string) string { return gapStyle.Render(s) }

	var row string
	if refs != "" {
		row = sha + gap("  ") + date + gap(" ") + clock + gap("  ") + author + gap("  ") + refs + gap(" ") + subject
	} else {
		row = sha + gap("  ") + date + gap(" ") + clock + gap("  ") + author + gap("  ") + subject
	}

	style := normalRowStyle
	if selected {
		style = selectedRowStyle
	}
	// MaxWidth alone (no Width) truncates in place; combining Width with
	// MaxWidth here would run row — which already contains several
	// independently-Rendered, reset-terminated spans — through lipgloss's
	// paragraph reflow, which miscounts cell positions across those resets
	// and can hard-wrap in the middle of a field (e.g. mid-date) regardless
	// of the requested width. Padding to width is done by hand below
	// instead, since Width() no longer does it for us.
	row = style.MaxWidth(width).Render(row)
	if pad := width - lipgloss.Width(row); pad > 0 {
		row += style.Render(strings.Repeat(" ", pad))
	}
	return row
}

// highlightSpans splits text at case-insensitive occurrences of term,
// rendering unmatched runs with base and matched runs with match, then
// concatenating the already-rendered pieces — the same nested-style
// pattern used above (style a plain-text span, then render it), just
// generalized to N spans instead of one field per style. Returns
// base.Render(text) unchanged when term is empty.
func highlightSpans(text, term string, base, match lipgloss.Style) string {
	if term == "" {
		return base.Render(text)
	}
	var b strings.Builder
	pos := 0
	for _, occ := range findOccurrences(text, term) {
		b.WriteString(base.Render(text[pos:occ[0]]))
		b.WriteString(match.Render(text[occ[0]:occ[1]]))
		pos = occ[1]
	}
	b.WriteString(base.Render(text[pos:]))
	return b.String()
}

// formatRefs renders branch/tag/HEAD decorations as bracketed badges.
// dimmed swaps in the same dimStyle used for the rest of a non-matching
// row, rather than leaving refs at their normal bold/colored style, so a
// badge doesn't stand out against an otherwise faded row. selected merges
// in cursorRowBg (see renderCommitRow) rather than leaving badges
// unstyled, since every badge here is independently Render()'d and each
// Render() call ends with its own ANSI reset — the join separator below
// carries the same background for the same reason, so multiple badges
// on one row don't leave a gap in the cursor highlight between them.
func formatRefs(refs []string, selected, dimmed bool) string {
	if len(refs) == 0 {
		return ""
	}
	style := refStyle
	if dimmed {
		style = dimStyle
	}
	if selected {
		style = style.Background(cursorRowBg)
	}
	parts := make([]string, len(refs))
	for i, r := range refs {
		parts[i] = style.Render("[" + r + "]")
	}
	return strings.Join(parts, style.Render(" "))
}

// shaPrefix returns the first shaColumnWidth characters of a full commit
// hash, unabbreviated by git and unadorned by an ellipsis (unlike
// truncate) since a partial hash is still a valid, meaningful prefix.
func shaPrefix(hash string) string {
	if len(hash) <= shaColumnWidth {
		return hash
	}
	return hash[:shaColumnWidth]
}

// truncate shortens s to at most n runes, appending an ellipsis if cut.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}
