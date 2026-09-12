package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/maiwald/gitl/internal/git"
)

const (
	authorColumnWidth = 18
	// narrowAuthorColumnWidth replaces authorColumnWidth once the author
	// name has been abbreviateName-shortened for a narrow pane.
	// abbreviateName caps its output at two 3-letter groups plus an optional
	// "…", so 8 chars ("Xxx Yyy…") is its true maximum width.
	narrowAuthorColumnWidth = 8
	// shaColumnWidth matches git's own default abbreviated-hash length (the
	// same length `%h`/`git rev-parse --short` use), prioritizing space for
	// the subject over a wider margin against same-prefix collisions.
	shaColumnWidth = 7
	// dateColumnWidth and clockColumnWidth match the "2006-01-02"/"15:04"
	// formats renderCommitRow uses. Named so renderWorktreeRow's blank
	// columns can't drift from them.
	dateColumnWidth  = 10
	clockColumnWidth = 5
	// narrowListWidth is the pane-width breakpoint below which
	// renderCommitRow/renderWorktreeRow collapse the date+clock columns
	// into one and abbreviate the author name, trading detail for space
	// back to the subject/message.
	narrowListWidth = 100
)

// renderCommitRow renders a single commit list row: abbreviated sha, date,
// local time, author, ref decorations, and the commit subject,
// truncated/padded to width. searchTerm highlights matches in the sha, author
// and subject fields; a match in the message body, or in the hash beyond the
// visible prefix, is a real match with nothing visible here to highlight.
// isActiveMatch picks a distinct style for whichever match n/N points at.
//
// dimmed fades a row outside the search's real match set (from the git query,
// not from re-scanning this row's own text), which is what distinguishes such
// a body-only match from a true non-match. Each field's base style comes from
// rowFieldStyle, so dimming and selection are baked into the fields rather
// than wrapped around the finished row.
func renderCommitRow(c git.Commit, width int, selected, dimmed bool, searchTerm string, isActiveMatch bool) string {
	narrow := width < narrowListWidth

	sha := fmt.Sprintf("%-*s", shaColumnWidth, shaPrefix(c.Hash))
	localDate := c.AuthorDate.Local()

	authorName := c.AuthorName
	authorWidth := authorColumnWidth
	if narrow {
		authorName = abbreviateName(authorName)
		authorWidth = narrowAuthorColumnWidth
	}
	author := fmt.Sprintf("%-*s", authorWidth, truncate(authorName, authorWidth))
	subject := c.Subject

	shaBase := rowFieldStyle(shaStyle, dimmed, selected)
	dateBase := rowFieldStyle(dateStyle, dimmed, selected)
	authorBase := rowFieldStyle(authorStyle, dimmed, selected)
	subjectBase := rowFieldStyle(normalRowStyle, dimmed, selected)
	gap := rowGap(selected)

	// date/clock are never search targets, so they're rendered in their base
	// style; sha, author and subject go through highlightSpans below.

	// when collapses date+clock into whichever single value is most useful
	// once the pane is narrow (see narrowListWidth): today's commits show
	// the time, since the date is implied; anything else shows the date.
	// Padded to dateColumnWidth alone, since shrinking the column is the
	// point of collapsing it.
	var when string
	if narrow {
		text := localDate.Format("2006-01-02")
		if isToday(localDate) {
			text = localDate.Format("15:04")
		}
		when = dateBase.Render(fmt.Sprintf("%-*s", dateColumnWidth, text))
	} else {
		date := dateBase.Render(localDate.Format("2006-01-02"))
		clock := dateBase.Render(localDate.Format("15:04"))
		when = date + gap(" ") + clock
	}

	// A match keeps its own highlight on the cursor row too, exactly as in
	// renderLineWithBaseSpans: the match style carries its own background, so
	// it stays legible over the cursor's, and blanking it would hide which
	// field matched on the one row the user is actually looking at.
	ms := matchStyle(isActiveMatch)
	sha = highlightSpans(sha, searchTerm, shaBase, ms)
	author = highlightSpans(author, searchTerm, authorBase, ms)
	subject = highlightSpans(subject, searchTerm, subjectBase, ms)
	refs := formatRefs(c.Refs, selected, dimmed)

	var row string
	if refs != "" {
		row = sha + gap(" ") + when + gap(" ") + author + gap("  ") + refs + gap(" ") + subject
	} else {
		row = sha + gap(" ") + when + gap(" ") + author + gap("  ") + subject
	}

	return padRow(row, width, selected)
}

// rowFieldStyle resolves a row field's base style against dimmed/selected
// state: dimmed swaps in dimStyle wholesale, then selected merges in
// cursorRowBg. Applied per field (and to literal separators via rowGap)
// rather than as one style around the finished row, since each field is
// independently Render()'d and its ANSI reset would cancel a single outer
// background the moment it fires.
func rowFieldStyle(base lipgloss.Style, dimmed, selected bool) lipgloss.Style {
	if dimmed {
		base = dimStyle
	}
	if selected {
		base = base.Background(cursorRowBg)
	}
	return base
}

// rowGap returns a closure rendering a literal separator (spaces between
// fields) in rowFillStyle, so a selected row's cursor highlight has no gaps
// between fields.
func rowGap(selected bool) func(string) string {
	style := rowFillStyle(selected)
	return func(s string) string { return style.Render(s) }
}

// RenderCommitList renders commits as plain list rows, one per line, with
// no selection/dim/search styling — used by the -n non-interactive flag.
//
// The rows come back padded out to width, which is what the TUI wants (every
// row is exactly one full-width line) and what printed output doesn't: the
// trailing spaces wrap onto a second line as soon as the window is narrower
// than the width the rows were measured at. A plain row's padding is appended
// as literal spaces after the styled fields (see padMeasuredRow), so trimming
// them back off here touches no escape sequence.
func RenderCommitList(commits []git.Commit, width int) string {
	lines := make([]string, len(commits))
	for i, c := range commits {
		lines[i] = strings.TrimRight(renderCommitRow(c, width, false, false, "", false), " ")
	}
	return strings.Join(lines, "\n")
}

// formatRefs renders branch/tag/HEAD decorations as bracketed badges, styled
// via rowFieldStyle so a badge fades with a dimmed row and carries the cursor
// background when selected. The join separator carries the same style, for
// rowFieldStyle's reset reason, so badges leave no gap in the highlight.
func formatRefs(refs []string, selected, dimmed bool) string {
	if len(refs) == 0 {
		return ""
	}
	style := rowFieldStyle(refStyle, dimmed, selected)
	parts := make([]string, len(refs))
	for i, r := range refs {
		parts[i] = style.Render("[" + r + "]")
	}
	return strings.Join(parts, style.Render(" "))
}

// shaPrefix returns the first shaColumnWidth characters of a full commit hash,
// with no ellipsis (unlike truncate), since a partial hash is still a valid
// prefix.
func shaPrefix(hash string) string {
	if len(hash) <= shaColumnWidth {
		return hash
	}
	return hash[:shaColumnWidth]
}

// isToday reports whether t falls on the same calendar day as now, in t's own
// location.
func isToday(t time.Time) bool {
	y1, m1, d1 := t.Date()
	y2, m2, d2 := time.Now().Date()
	return y1 == y2 && m1 == m2 && d1 == d2
}

// abbreviateName shortens a name for the narrow list layout by cutting each
// whitespace-delimited part to its first 3 runes (e.g. "Ada Lovelace" ->
// "Ada Lov"), unlike truncate, which would cut only the tail. At most two
// parts are kept; a name with more gets a trailing "…" (e.g. "Grace
// Brewster Murray Hopper" -> "Gra Bre…").
func abbreviateName(name string) string {
	fields := strings.Fields(name)
	kept := min(len(fields), 2)
	for i := 0; i < kept; i++ {
		r := []rune(fields[i])
		if len(r) > 3 {
			fields[i] = string(r[:3])
		}
	}
	abbreviated := strings.Join(fields[:kept], " ")
	if len(fields) > kept {
		abbreviated += "…"
	}
	return abbreviated
}
