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
	// narrowAuthorColumnWidth is used instead of authorColumnWidth once the
	// author name has been abbreviateName-shortened for a narrow pane (see
	// renderCommitRow) — abbreviateName caps its output at two 3-letter
	// groups plus an optional "…" (e.g. "Ada Lov", or "Gra Bre…" for a
	// name with more parts), so 8 chars ("Xxx Yyy…") is the true max width,
	// with truncate's own ellipsis kept as a defensive no-op fallback.
	narrowAuthorColumnWidth = 8
	// shaColumnWidth matches git's own default abbreviated-hash length (the
	// same length `%h`/`git rev-parse --short` use), prioritizing space for
	// the subject over a wider margin against same-prefix collisions.
	shaColumnWidth = 7
	// dateColumnWidth and clockColumnWidth match the "2006-01-02"/"15:04"
	// formats renderCommitRow uses. Named here (rather than left implicit)
	// so renderWorktreeRow's blank columns can't drift from these.
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
// truncated/padded to width. searchTerm highlights matching substrings
// (case insensitive) in the author and subject fields only, since those
// are the only visible fields SearchCommitHashes actually searches (author
// name and commit message — sha/date/refs are never match targets, so
// they're never highlighted even if their text happens to contain the
// term). isActiveMatch picks a visually distinct style for whichever match
// n/N currently points at. A commit can be a search match via its full
// message body, which isn't shown in the row at all — in that case
// there's simply nothing here to highlight, which is expected.
//
// dimmed fades a row that isn't among the search's real matches (as
// determined by the actual git query, not by whether this row's visible
// text happens to contain the term — see ListModel.render) — this is what
// makes a body-only match (nothing highlighted in the row itself) visually
// distinguishable from an outright non-match. Each field's base style is
// resolved via rowFieldStyle, which explains why dimming/selection are
// baked into each field rather than one outer style around the row.
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

	// sha/date/clock are never search targets (SearchCommitHashes only
	// matches author name and commit message), so they're just rendered in
	// their base style — only author and subject go through highlightSpans.
	sha = shaBase.Render(sha)

	// when collapses date+clock into whichever single value is most useful
	// for this row once the pane is narrow (see narrowListWidth): today's
	// commits show the time, since the date is implied; anything else
	// shows the date, since the time of day rarely matters at a glance.
	// Padded to dateColumnWidth alone (not the full date+gap+clock width)
	// since shrinking this column is the actual point of collapsing it.
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

	if !selected {
		matchStyle := searchMatchStyle
		if isActiveMatch {
			matchStyle = searchActiveMatchStyle
		}
		author = highlightSpans(author, searchTerm, authorBase, matchStyle)
		subject = highlightSpans(subject, searchTerm, subjectBase, matchStyle)
	} else {
		author = authorBase.Render(author)
		subject = subjectBase.Render(subject)
	}
	refs := formatRefs(c.Refs, selected, dimmed)

	var row string
	if refs != "" {
		row = sha + gap(" ") + when + gap(" ") + author + gap("  ") + refs + gap(" ") + subject
	} else {
		row = sha + gap(" ") + when + gap(" ") + author + gap("  ") + subject
	}

	return padRow(row, width, rowFillStyle(selected))
}

// rowFieldStyle resolves a row field's base style against the row's
// dimmed/selected state: dimmed swaps in dimStyle wholesale (rather than
// darkening base), then selected merges in cursorRowBg on top — merged
// into each field's own style (and the literal separators via rowGap)
// rather than applied as one flat style wrapped around the finished row,
// since every field is independently Render()'d and each Render() call
// ends with its own ANSI reset, which would cancel a single outer
// background the moment the first one fires. Shared by renderCommitRow,
// formatRefs, and renderWorktreeRow.
func rowFieldStyle(base lipgloss.Style, dimmed, selected bool) lipgloss.Style {
	if dimmed {
		base = dimStyle
	}
	if selected {
		base = base.Background(cursorRowBg)
	}
	return base
}

// rowFillStyle is the style used both for a row's literal gap/separator
// text and as padRow's fill style — normalRowStyle normally, or
// selectedRowStyle (the cursor background) for a selected row.
func rowFillStyle(selected bool) lipgloss.Style {
	if selected {
		return selectedRowStyle
	}
	return normalRowStyle
}

// rowGap returns a closure rendering a literal separator (spaces between
// fields) in rowFillStyle, so a selected row's cursor highlight has no gaps
// between fields.
func rowGap(selected bool) func(string) string {
	style := rowFillStyle(selected)
	return func(s string) string { return style.Render(s) }
}

// padRow truncates row to width and pads any remainder with trailing
// spaces in style. MaxWidth alone (no Width) truncates in place; combining
// Width with MaxWidth here would run row — which already contains several
// independently-Rendered, reset-terminated spans — through lipgloss's
// paragraph reflow, which miscounts cell positions across those resets and
// can hard-wrap in the middle of a field (e.g. mid-date) regardless of the
// requested width. Padding to width is done by hand instead, since Width()
// no longer does it for us.
func padRow(row string, width int, style lipgloss.Style) string {
	row = style.MaxWidth(width).Render(row)
	if pad := width - lipgloss.Width(row); pad > 0 {
		row += style.Render(strings.Repeat(" ", pad))
	}
	return row
}

// RenderCommitList renders commits as plain list rows, one per line, with
// no selection/dim/search styling — used by the -n non-interactive flag.
func RenderCommitList(commits []git.Commit, width int) string {
	lines := make([]string, len(commits))
	for i, c := range commits {
		lines[i] = renderCommitRow(c, width, false, false, "", false)
	}
	return strings.Join(lines, "\n")
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

// formatRefs renders branch/tag/HEAD decorations as bracketed badges,
// resolved via rowFieldStyle so a badge fades with the rest of a dimmed
// row and carries the cursor background when selected. The join separator
// below carries the same style for the same reset-hazard reason
// (rowFieldStyle's doc comment), so multiple badges on one row don't leave
// a gap in the cursor highlight between them.
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

// isToday reports whether t falls on the same calendar day as now, in t's
// own location (renderCommitRow always calls this with an already-
// .Local()-converted time, so both sides compare in local time).
func isToday(t time.Time) bool {
	y1, m1, d1 := t.Date()
	y2, m2, d2 := time.Now().Date()
	return y1 == y2 && m1 == m2 && d1 == d2
}

// abbreviateName shortens a name for the narrow list layout by cutting each
// whitespace-delimited part down to its first 3 runes (e.g. "Ada Lovelace"
// -> "Ada Lov") rather than truncating the whole string, so every part of
// the name stays recognizable instead of just the first — deliberately not
// truncate, which cuts only the tail and adds an ellipsis. At most two
// abbreviated groups are kept (further shrinking the narrow author column);
// a name with more parts than that gets a trailing "…" to signal the drop
// (e.g. "Grace Brewster Murray Hopper" -> "Gra Bre…").
func abbreviateName(name string) string {
	fields := strings.Fields(name)
	kept := len(fields)
	if kept > 2 {
		kept = 2
	}
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
