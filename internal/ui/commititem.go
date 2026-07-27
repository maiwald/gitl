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

	if !selected {
		matchStyle := searchMatchStyle
		if isActiveMatch {
			matchStyle = searchActiveMatchStyle
		}
		shaBase, dateBase, authorBase, subjectBase := shaStyle, dateStyle, authorStyle, lipgloss.NewStyle()
		if dimmed {
			shaBase, dateBase, authorBase, subjectBase = dimStyle, dimStyle, dimStyle, dimStyle
		}
		sha = highlightSpans(sha, searchTerm, shaBase, matchStyle)
		date = highlightSpans(date, searchTerm, dateBase, matchStyle)
		clock = highlightSpans(clock, searchTerm, dateBase, matchStyle)
		author = highlightSpans(author, searchTerm, authorBase, matchStyle)
		subject = highlightSpans(subject, searchTerm, subjectBase, matchStyle)
	}
	refs := formatRefs(c.Refs, selected, dimmed)

	var row string
	if refs != "" {
		row = fmt.Sprintf("%s  %s %s  %s  %s %s", sha, date, clock, author, refs, subject)
	} else {
		row = fmt.Sprintf("%s  %s %s  %s  %s", sha, date, clock, author, subject)
	}

	style := normalRowStyle
	if selected {
		style = selectedRowStyle
	}
	return style.Width(width).MaxWidth(width).Render(row)
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

// formatRefs renders branch/tag/HEAD decorations as bracketed badges. When
// selected, badges are left as plain text rather than run through
// Style.Render: any embedded ANSI reset (which Render always emits at the
// end of its span) would terminate the selected row's background applied
// by the outer style.Render(row) call in renderCommitRow, leaving
// everything after the ref badge — the subject, in particular —
// unhighlighted for the rest of the line. dimmed swaps in the same
// dimStyle used for the rest of a non-matching row, rather than leaving
// refs at their normal bold/colored style, so a badge doesn't stand out
// against an otherwise faded row.
func formatRefs(refs []string, selected, dimmed bool) string {
	if len(refs) == 0 {
		return ""
	}
	style := refStyle
	if dimmed {
		style = dimStyle
	}
	parts := make([]string, len(refs))
	for i, r := range refs {
		if selected {
			parts[i] = "[" + r + "]"
		} else {
			parts[i] = style.Render("[" + r + "]")
		}
	}
	return strings.Join(parts, " ")
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
