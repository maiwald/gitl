package ui

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/maiwald/gitl/internal/git"
)

// FileRange marks where a single file's rendered lines live within the
// flattened diff content produced by RenderDiff, so a scroll position (a
// line index into that content) can be mapped back to "which file".
type FileRange struct {
	FileIndex int
	StartLine int
	EndLine   int // exclusive
}

// RenderDiff flattens a commit's parsed diff files into one styled slice of
// lines suitable for a single scrollable viewport, along with the line
// range each file occupies within that flattened content. matches (from
// FindDiffMatches, in the same file/hunk/line traversal order this function
// uses) are highlighted inline unless dismissed is true. activeMatch
// indexes into matches for the one that should stand out with a distinct
// style (-1 for none; ignored when dismissed).
//
// cursorLine, if it falls within this diff's own flattened line space
// (negative or past the end otherwise selects nothing — the caller doesn't
// need to guard the call, just offset its own coordinate into this
// function's local space), gets the cursor row background merged into
// whichever line it lands on. A logical diff line that wraps to several
// visual rows is only known to fully occupy [lineStart, lineStart+n) after
// it's been rendered once, so each line is rendered once at plain, and
// only re-rendered (once) with the cursor flag set if cursorLine turns out
// to fall in its range — see renderDiffLine.
//
// matchLines is always computed from the full matches list regardless of
// dismissed, since word-wrapping (which determines line positions) is
// ANSI-aware and doesn't depend on whether a span is styled — so a
// dismissed match still lands correctly once n/N revives it. It's
// parallel to matches: the flattened line index where each match's line
// starts (enough to bring it into view).
func RenderDiff(files []git.File, width int, matches []DiffMatch, activeMatch int, dismissed bool, cursorLine int) ([]string, []FileRange, []int) {
	var lines []string
	ranges := make([]FileRange, 0, len(files))
	matchLines := make([]int, len(matches))
	mi := 0

	for fi, f := range files {
		start := len(lines)

		fileHeaderRow := len(lines)
		lines = append(lines, renderFileHeader(f, width, cursorLine == fileHeaderRow))
		if f.Binary {
			binaryRow := len(lines)
			lines = append(lines, renderClipped(cursorMerged(binaryNoticeStyle, cursorLine == binaryRow), "Binary file, diff not shown", width))
		}
		for hi, h := range f.Hunks {
			hunkHeaderRow := len(lines)
			lines = append(lines, renderClipped(cursorMerged(hunkHeaderStyle, cursorLine == hunkHeaderRow), h.Header, width))
			hunkSpans := intralineSpans(h)
			for li, l := range h.Lines {
				lineStart := len(lines)
				var lineMatches []lineMatch
				for mi < len(matches) && matches[mi].FileIndex == fi && matches[mi].HunkIndex == hi && matches[mi].LineIndex == li {
					if !dismissed {
						lineMatches = append(lineMatches, lineMatch{
							start: matches[mi].StartCol, end: matches[mi].EndCol, active: mi == activeMatch,
						})
					}
					matchLines[mi] = lineStart
					mi++
				}
				rendered := renderDiffLine(l, width, hunkSpans[li], lineMatches, false)
				if cursorLine >= lineStart && cursorLine < lineStart+len(rendered) {
					rendered = renderDiffLine(l, width, hunkSpans[li], lineMatches, true)
				}
				lines = append(lines, rendered...)
			}
		}
		ranges = append(ranges, FileRange{FileIndex: fi, StartLine: start, EndLine: len(lines)})
	}
	return lines, ranges, matchLines
}

// cursorMerged merges the cursor row's background into base when isCursor
// is true, else returns base unchanged. Only safe for a row rendered with a
// single, uniform style (a file/hunk header, the binary notice) — content
// that composites independently-rendered spans (diff/header text lines)
// instead threads the cursor flag into renderLineWithBaseSpans, which
// merges the background per-span before any Render() call fires (see
// CLAUDE.md's "style plain-text spans before any .Render() call" rule).
func cursorMerged(base lipgloss.Style, isCursor bool) lipgloss.Style {
	if isCursor {
		return base.Background(cursorRowBg)
	}
	return base
}

// DiffMatch identifies one case-insensitive occurrence of a search term
// within a single diff line's content (DiffLine.Text — excluding the
// leading +/-/space prefix, and excluding file paths and hunk headers;
// only line content participates in diff search).
type DiffMatch struct {
	FileIndex, HunkIndex, LineIndex int
	StartCol, EndCol                int // byte offsets into DiffLine.Text
}

// FindDiffMatches returns every occurrence of term across files' diff
// lines, in the same file/hunk/line traversal order RenderDiff uses — a
// documented invariant both functions rely on to stay in sync. Returns nil
// for an empty term.
func FindDiffMatches(files []git.File, term string) []DiffMatch {
	if term == "" {
		return nil
	}

	var matches []DiffMatch
	for fi, f := range files {
		for hi, h := range f.Hunks {
			for li, l := range h.Lines {
				for _, occ := range findOccurrences(l.Text, term) {
					matches = append(matches, DiffMatch{
						FileIndex: fi, HunkIndex: hi, LineIndex: li,
						StartCol: occ[0], EndCol: occ[1],
					})
				}
			}
		}
	}
	return matches
}

// findOccurrences returns the byte [start, end) offset of every
// case-insensitive occurrence of term within text.
func findOccurrences(text, term string) [][2]int {
	lowerText, lowerTerm := strings.ToLower(text), strings.ToLower(term)
	var occs [][2]int
	pos := 0
	for {
		idx := strings.Index(lowerText[pos:], lowerTerm)
		if idx < 0 {
			break
		}
		idx += pos
		occs = append(occs, [2]int{idx, idx + len(term)})
		pos = idx + len(term)
	}
	return occs
}

func renderFileHeader(f git.File, width int, cursor bool) string {
	label := fmt.Sprintf("%s %s", fileStatusGlyph(f.Status), filePathLabel(f))
	return renderClipped(cursorMerged(fileHeaderStyle, cursor), label, width)
}

// filePathLabel is a file's display path: "old -> new" for renames (when
// the two differ), otherwise just its current path. Shared by the diff
// file header and the stats summary (statsview.go) so the two views agree
// on how a rename reads.
func filePathLabel(f git.File) string {
	if f.Status == git.StatusRenamed && f.OldPath != f.Path {
		return f.OldPath + " -> " + f.Path
	}
	return f.Path
}

// renderClipped renders text in a single line, truncated (not wrapped) to
// width. It skips the width constraint entirely when width isn't positive,
// since lipgloss's Width/MaxWidth degenerate at zero.
func renderClipped(style lipgloss.Style, text string, width int) string {
	if width <= 0 {
		return style.Render(text)
	}
	return style.Width(width).MaxWidth(width).Render(text)
}

func fileStatusGlyph(s git.FileStatus) string {
	switch s {
	case git.StatusAdded:
		return "A"
	case git.StatusDeleted:
		return "D"
	case git.StatusRenamed:
		return "R"
	case git.StatusCopied:
		return "C"
	default:
		return "M"
	}
}

// lineMatch is a search match's position within a single diff line's
// rendered text (prefix + DiffLine.Text), plus whether it's the active
// (n/N-selected) match.
type lineMatch struct {
	start, end int
	active     bool
}

// renderDiffLine renders a single diff line, word-wrapping it to width when
// it doesn't fit on one row. Each wrapped row is returned as its own
// element so it becomes its own entry in the flattened diff content,
// keeping FileRange line-index bookkeeping correct. spans (from
// intralineSpans) mark the intraline change within a paired add/delete
// line, in the same byte-offset-into-DiffLine.Text coordinate space as
// matches, so both are shifted by len(prefix) together before rendering.
// cursor merges the cursor row's background across the whole line (see
// RenderDiff's doc comment for the two-pass call convention this expects).
func renderDiffLine(l git.DiffLine, width int, spans []baseSpan, matches []lineMatch, cursor bool) []string {
	style, prefix := diffLineStyle(l.Op)
	text := prefix + l.Text

	var content string
	if len(spans) == 0 && len(matches) == 0 && !cursor {
		content = style.Render(text)
	} else {
		shiftedMatches := make([]lineMatch, len(matches))
		for i, m := range matches {
			shiftedMatches[i] = lineMatch{start: len(prefix) + m.start, end: len(prefix) + m.end, active: m.active}
		}
		shiftedSpans := make([]baseSpan, len(spans))
		for i, s := range spans {
			shiftedSpans[i] = baseSpan{start: len(prefix) + s.start, end: len(prefix) + s.end, style: s.style}
		}
		content = renderLineWithBaseSpans(text, shiftedSpans, shiftedMatches, style, cursor)
	}

	if width <= 0 {
		return []string{content}
	}
	// The outer wrap is colorless: content is already fully styled (base
	// color plus any highlight spans), and wrapping it through a further
	// *colored* style here would risk that color clobbering the inner
	// spans — the same nested-style convention used in commititem.go,
	// where an already-styled row is wrapped by a colorless outer style.
	rows := strings.Split(lipgloss.NewStyle().Width(width).Render(content), "\n")
	if cursor {
		// The wrap above pads short rows with plain (uncolored) spaces, so
		// a line shorter than width — or entirely empty, like the blank
		// lines separating sections — would otherwise show the cursor
		// background only behind its own text, not across the row. Strip
		// that plain padding back off before handing the row to padRow: if
		// left in place, padRow finds the row already at width (nothing
		// left for its own pad step to add) and only wraps the *existing*
		// plain padding in an outer Render() call — which doesn't reliably
		// recolor it, since the row already contains its own embedded
		// reset partway through (see CLAUDE.md's "no facility for
		// splicing a style into an already-ANSI'd string" rule; this is
		// that same hazard one layer removed). Stripped first, padRow's
		// own pad step re-adds the same width as a fresh, independently
		// rendered, correctly colored span.
		fill := lipgloss.NewStyle().Background(cursorRowBg)
		for i, r := range rows {
			rows[i] = padRow(strings.TrimRight(r, " "), width, fill)
		}
	}
	return rows
}

func diffLineStyle(op git.LineOp) (lipgloss.Style, string) {
	switch op {
	case git.OpAdd:
		return diffAddStyle, "+"
	case git.OpDelete:
		return diffDeleteStyle, "-"
	default:
		return diffContextStyle, " "
	}
}

// intralineSpans finds, for each maximal run of consecutive non-context
// lines in a hunk (a "replace block" — the deleted lines followed by the
// lines that replaced them), the intraline change within each positionally
// paired delete/add line, keyed by that line's index into h.Lines. Matches
// diff-highlight's own show_hunk: a block is only paired up, and only its
// lines individually highlighted, when it has exactly as many removed
// lines as added lines — an unequal block is left with no spans at all
// (same as a pure insertion or deletion block), since diff-highlight
// doesn't attempt any partial or best-effort pairing when the counts
// don't line up.
func intralineSpans(h git.Hunk) map[int][]baseSpan {
	spans := make(map[int][]baseSpan)

	i := 0
	for i < len(h.Lines) {
		if h.Lines[i].Op == git.OpContext {
			i++
			continue
		}

		var deleteIdx, addIdx []int
		for i < len(h.Lines) && h.Lines[i].Op != git.OpContext {
			if h.Lines[i].Op == git.OpDelete {
				deleteIdx = append(deleteIdx, i)
			} else {
				addIdx = append(addIdx, i)
			}
			i++
		}

		if len(deleteIdx) != len(addIdx) {
			continue
		}
		for k := range deleteIdx {
			di, ai := deleteIdx[k], addIdx[k]
			oldSpan, newSpan, ok := intralineDiff(h.Lines[di].Text, h.Lines[ai].Text)
			if !ok {
				continue
			}
			spans[di] = []baseSpan{{start: oldSpan[0], end: oldSpan[1], style: diffDeleteEmphStyle}}
			spans[ai] = []baseSpan{{start: newSpan[0], end: newSpan[1], style: diffAddEmphStyle}}
		}
	}

	return spans
}

// intralineDiff finds the byte-offset [start,end) "changed middle" of
// oldText and newText — the region left after trimming their common prefix
// and common suffix (rune-aware, so a multi-byte UTF-8 sequence is never
// split) — the same common-prefix/common-suffix technique git's own
// contrib/diff-highlight uses to keep this cheap for short lines rather
// than needing a general LCS. ok is false when the lines are identical, or
// when neither the common prefix nor the common suffix has any non-space
// content — i.e. the two lines don't actually share anything meaningful,
// so marking almost the whole line as "changed" wouldn't clarify anything
// over the plain flat add/delete color. This mirrors diff-highlight's own
// is_pair_interesting check (a pair is worth highlighting only if the
// shared prefix or suffix has content beyond its diff marker and
// whitespace) rather than a percentage-based cutoff — deliberately, since
// diff-highlight (which diff-so-fancy wraps) has no such ratio: a single
// shared non-space character at either end is enough to highlight, however
// large the changed middle is.
func intralineDiff(oldText, newText string) (oldSpan, newSpan [2]int, ok bool) {
	oldRunes, newRunes := []rune(oldText), []rune(newText)
	maxCommon := min(len(oldRunes), len(newRunes))

	prefix := 0
	for prefix < maxCommon && oldRunes[prefix] == newRunes[prefix] {
		prefix++
	}

	suffix := 0
	for suffix < maxCommon-prefix &&
		oldRunes[len(oldRunes)-1-suffix] == newRunes[len(newRunes)-1-suffix] {
		suffix++
	}

	oldMiddle, newMiddle := len(oldRunes)-prefix-suffix, len(newRunes)-prefix-suffix
	if oldMiddle <= 0 && newMiddle <= 0 {
		return oldSpan, newSpan, false
	}
	if !hasNonSpace(oldRunes[:prefix]) && !hasNonSpace(oldRunes[len(oldRunes)-suffix:]) {
		return oldSpan, newSpan, false
	}

	oldSpan = [2]int{runeIndexToByte(oldRunes, prefix), runeIndexToByte(oldRunes, len(oldRunes)-suffix)}
	newSpan = [2]int{runeIndexToByte(newRunes, prefix), runeIndexToByte(newRunes, len(newRunes)-suffix)}
	return oldSpan, newSpan, true
}

func hasNonSpace(runes []rune) bool {
	for _, r := range runes {
		if !unicode.IsSpace(r) {
			return true
		}
	}
	return false
}

func runeIndexToByte(runes []rune, idx int) int {
	return len(string(runes[:idx]))
}
