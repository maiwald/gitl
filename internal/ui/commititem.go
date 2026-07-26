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
func renderCommitRow(c git.Commit, width int, selected bool, searchTerm string, isActiveMatch bool) string {
	sha := fmt.Sprintf("%-*s", shaColumnWidth, shaPrefix(c.Hash))
	date := c.AuthorDate.Format("2006-01-02")
	clock := c.AuthorDate.Local().Format("15:04")
	author := fmt.Sprintf("%-*s", authorColumnWidth, truncate(c.AuthorName, authorColumnWidth))
	subject := c.Subject

	if !selected {
		matchStyle := searchMatchStyle
		if isActiveMatch {
			matchStyle = searchActiveMatchStyle
		}
		sha = highlightSpans(sha, searchTerm, shaStyle, matchStyle)
		date = highlightSpans(date, searchTerm, dateStyle, matchStyle)
		clock = highlightSpans(clock, searchTerm, dateStyle, matchStyle)
		author = highlightSpans(author, searchTerm, authorStyle, matchStyle)
		subject = highlightSpans(subject, searchTerm, lipgloss.NewStyle(), matchStyle)
	}
	refs := formatRefs(c.Refs, selected)

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
	lowerText, lowerTerm := strings.ToLower(text), strings.ToLower(term)
	var b strings.Builder
	pos := 0
	for {
		idx := strings.Index(lowerText[pos:], lowerTerm)
		if idx < 0 {
			b.WriteString(base.Render(text[pos:]))
			break
		}
		idx += pos
		b.WriteString(base.Render(text[pos:idx]))
		b.WriteString(match.Render(text[idx : idx+len(term)]))
		pos = idx + len(term)
	}
	return b.String()
}

// formatRefs renders branch/tag/HEAD decorations as bracketed badges. When
// selected, badges are left as plain text rather than run through
// Style.Render: any embedded ANSI reset (which Render always emits at the
// end of its span) would terminate the selected row's background applied
// by the outer style.Render(row) call in renderCommitRow, leaving
// everything after the ref badge — the subject, in particular —
// unhighlighted for the rest of the line.
func formatRefs(refs []string, selected bool) string {
	if len(refs) == 0 {
		return ""
	}
	parts := make([]string, len(refs))
	for i, r := range refs {
		if selected {
			parts[i] = "[" + r + "]"
		} else {
			parts[i] = refStyle.Render("[" + r + "]")
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
