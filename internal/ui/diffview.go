package ui

import (
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/maiwald/gitl/internal/git"
)

// FileRange marks where a single file's lines live within the diff content
// produced by RenderDiff, so a scroll position can be mapped back to "which
// file". RenderDiff states it in logical lines; DetailModel.reflow rebases it
// onto the flattened rows those lines rendered into, which is the form every
// other reader of a FileRange (sections, the sticky header) works in.
type FileRange struct {
	FileIndex int
	StartLine int
	EndLine   int // exclusive
}

// RenderDiff describes a commit's parsed diff files as one flat sequence of
// logical lines — file header, optional binary notice, then each hunk's
// header and lines — suitable for a single scrollable viewport, along with
// the range of them each file occupies. matches (from FindDiffMatches, in
// the same file/hunk/line traversal order this function uses) are
// highlighted inline unless dismissed is true — a DiffMatchFileName match
// highlights within that file's header, a DiffMatchLine match within the
// diff line it belongs to. activeMatch indexes into matches for the one that
// should stand out with a distinct style (-1 for none; ignored when
// dismissed).
//
// Nothing here knows where its lines land on screen or which one the cursor
// is on: every coordinate it returns counts logical lines, and each line
// comes back as a lineRenderer the caller renders once it has decided both
// (see renderRows). That's what lets a cursor move re-render a single line
// rather than the whole diff.
//
// matchLines is always computed from the full matches list regardless of
// dismissed, since a line's position doesn't depend on whether a span within
// it is styled — so a dismissed match still lands correctly once n/N revives
// it. It's parallel to matches: the logical line each match sits on.
func RenderDiff(files []git.File, width int, matches []DiffMatch, activeMatch int, dismissed bool) ([]lineRenderer, []FileRange, []int) {
	var lines []lineRenderer
	ranges := make([]FileRange, 0, len(files))
	matchLines := make([]int, len(matches))
	mi := 0

	for fi, f := range files {
		start := len(lines)

		var headerMatches []lineMatch
		for mi < len(matches) && matches[mi].Kind == DiffMatchFileName && matches[mi].FileIndex == fi {
			if !dismissed {
				headerMatches = append(headerMatches, lineMatch{
					start: matches[mi].StartCol, end: matches[mi].EndCol, active: mi == activeMatch,
				})
			}
			matchLines[mi] = len(lines)
			mi++
		}
		lines = append(lines, func(cursor bool) []string {
			return []string{renderFileHeader(f, width, cursor, headerMatches)}
		})
		if f.Binary {
			lines = append(lines, clippedLine(binaryNoticeStyle, "Binary file, diff not shown", width))
		}
		for hi, h := range f.Hunks {
			lines = append(lines, clippedLine(hunkHeaderStyle, h.Header, width))
			hunkSpans := intralineSpans(h)
			for li, l := range h.Lines {
				var lineMatches []lineMatch
				for mi < len(matches) && matches[mi].Kind == DiffMatchLine && matches[mi].FileIndex == fi && matches[mi].HunkIndex == hi && matches[mi].LineIndex == li {
					if !dismissed {
						lineMatches = append(lineMatches, lineMatch{
							start: matches[mi].StartCol, end: matches[mi].EndCol, active: mi == activeMatch,
						})
					}
					matchLines[mi] = len(lines)
					mi++
				}
				spans := hunkSpans[li]
				lines = append(lines, func(cursor bool) []string {
					return renderDiffLine(l, width, spans, lineMatches, cursor)
				})
			}
		}
		ranges = append(ranges, FileRange{FileIndex: fi, StartLine: start, EndLine: len(lines)})
	}
	return lines, ranges, matchLines
}

// DiffMatchKind distinguishes a DiffMatch found in a file's own displayed
// name (DiffMatchFileName) from one found in a diff line's content
// (DiffMatchLine). DiffMatchLine is deliberately the zero value: existing
// DiffMatch struct literals that predate this field (e.g. in tests) omit
// Kind entirely and must keep meaning "line match", not silently become
// file-name matches.
type DiffMatchKind int

const (
	DiffMatchLine DiffMatchKind = iota
	DiffMatchFileName
)

// DiffMatch identifies one case-insensitive occurrence of a search term
// either within a file's displayed name (Kind == DiffMatchFileName —
// StartCol/EndCol are byte offsets into filePathLabel(files[FileIndex]);
// HunkIndex/LineIndex are unused) or within a single diff line's content
// (Kind == DiffMatchLine — DiffLine.Text, excluding the leading +/-/space
// prefix and excluding hunk headers; StartCol/EndCol are byte offsets into
// that text).
type DiffMatch struct {
	Kind                            DiffMatchKind
	FileIndex, HunkIndex, LineIndex int
	StartCol, EndCol                int
}

// FindDiffMatches returns every occurrence of term across files' displayed
// names and diff lines, in the same file/hunk/line traversal order
// RenderDiff uses — a documented invariant both functions rely on to stay
// in sync. Within a file, its own name-match(es), if any, sort ahead of
// its line matches, matching the file header row's position above its
// hunks in the rendered/flattened content. Returns nil for an empty term.
func FindDiffMatches(files []git.File, term string) []DiffMatch {
	if term == "" {
		return nil
	}

	var matches []DiffMatch
	for fi, f := range files {
		for _, occ := range findOccurrences(filePathLabel(f), term) {
			matches = append(matches, DiffMatch{
				Kind: DiffMatchFileName, FileIndex: fi,
				StartCol: occ[0], EndCol: occ[1],
			})
		}
		for hi, h := range f.Hunks {
			for li, l := range h.Lines {
				for _, occ := range findOccurrences(l.Text, term) {
					matches = append(matches, DiffMatch{
						Kind: DiffMatchLine, FileIndex: fi, HunkIndex: hi, LineIndex: li,
						StartCol: occ[0], EndCol: occ[1],
					})
				}
			}
		}
	}
	return matches
}

// renderFileHeader renders a file's header row: its status glyph plus
// displayed name (filePathLabel), with any file-name search matches (see
// DiffMatchFileName) highlighted inline. matches are in the same
// byte-offset-into-filePathLabel(f) coordinate space as DiffMatch, shifted
// by the glyph+space prefix before rendering, the same convention
// renderDiffLine uses for its own prefix. Falls back to a single
// renderClipped call when there's nothing to highlight and no cursor, to
// skip the extra composite-then-clip render pass on the common case.
func renderFileHeader(f git.File, width int, cursor bool, matches []lineMatch) string {
	prefix := fileStatusGlyph(f.Status) + " "
	label := prefix + filePathLabel(f)
	if len(matches) == 0 && !cursor {
		return renderClipped(fileHeaderStyle, label, width)
	}
	content := renderLineWithBaseSpans(label, nil, shiftMatches(matches, shiftBy(len(prefix))), fileHeaderStyle, cursor)
	return renderClippedStyled(content, width, cursor)
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

	at := shiftBy(len(prefix))
	return wrapStyledRows(text, shiftSpans(spans, at), shiftMatches(matches, at), style, width, cursor)
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
