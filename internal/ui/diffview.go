package ui

import (
	"strings"
	"unicode"

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
// logical lines — file header, optional binary notice, then each hunk's header
// and lines — along with the range of them each file occupies. matches (from
// FindDiffMatches, in the same file/hunk/line traversal order this function
// uses) are highlighted inline unless dismissed is true. activeMatch indexes
// into matches for the one that should stand out with a distinct style (-1 for
// none; ignored when dismissed).
//
// Nothing here knows where its lines land on screen or which one the cursor is
// on: every coordinate it returns counts logical lines, and each line comes
// back as a lineRenderer the caller renders once it has decided both (see
// renderRows).
//
// matchLines is parallel to matches — the logical line each match sits on —
// and is computed from the full list regardless of dismissed, so a dismissed
// match still lands correctly once n/N revives it.
func RenderDiff(files []git.File, width int, matches []DiffMatch, activeMatch int, dismissed bool) ([]lineRenderer, []FileRange, []matchLoc) {
	lines := make([]lineRenderer, 0, diffLineCount(files))
	ranges := make([]FileRange, 0, len(files))
	mc := newMatchCursor[diffPos](matches, activeMatch, dismissed)

	for fi, f := range files {
		start := len(lines)

		headerMatches := mc.take(len(lines), diffPos{kind: DiffMatchFileName, file: fi}, len(fileStatusGlyph(f.Status))+1)
		lines = append(lines, lineRenderer{
			text: fileHeaderText(f),
			render: func(cursor bool) []string {
				return []string{renderFileHeader(f, width, cursor, headerMatches)}
			},
		})
		if f.Binary {
			lines = append(lines, clippedLine(binaryNoticeStyle, "Binary file, diff not shown", width))
		}
		for hi, h := range f.Hunks {
			lines = append(lines, clippedLine(hunkHeaderStyle, h.Header, width))
			hunkSpans := intralineSpans(h)
			for li, l := range h.Lines {
				_, prefix := diffLineStyle(l.Op)
				lineMatches := mc.take(len(lines), diffPos{kind: DiffMatchLine, file: fi, hunk: hi, line: li}, len(prefix))
				spans := withTrailingWhitespace(l, hunkSpans[li])
				text := diffLineText(l)
				lines = append(lines, lineRenderer{
					text: text,
					// The marker column doesn't count: a line added or
					// removed empty is a paragraph break in the file it
					// came from.
					blank: strings.TrimSpace(l.Text) == "",
					render: func(cursor bool) []string {
						return renderDiffLine(l.Op, text, width, spans, lineMatches, cursor)
					},
				})
			}
		}
		ranges = append(ranges, FileRange{FileIndex: fi, StartLine: start, EndLine: len(lines)})
	}
	return lines, ranges, mc.locs
}

// diffLineCount is how many logical lines RenderDiff will emit for files, so
// it can size its slice once instead of regrowing it the length of the whole
// commit. It mirrors RenderDiff's own structure: a header per file, a notice
// for a binary one, and a header plus its lines for every hunk.
func diffLineCount(files []git.File) int {
	n := len(files)
	for _, f := range files {
		if f.Binary {
			n++
		}
		for _, h := range f.Hunks {
			n += 1 + len(h.Lines)
		}
	}
	return n
}

// DiffMatchKind distinguishes a DiffMatch found in a file's own displayed name
// (DiffMatchFileName) from one found in a diff line's content (DiffMatchLine).
// DiffMatchLine is the zero value, so a literal omitting Kind means a line
// match.
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

// diffPos is where a DiffMatch sits in RenderDiff's traversal: a file's header
// row, or one line of one of its hunks.
type diffPos struct {
	kind             DiffMatchKind
	file, hunk, line int
}

func (m DiffMatch) at() diffPos {
	return diffPos{kind: m.Kind, file: m.FileIndex, hunk: m.HunkIndex, line: m.LineIndex}
}
func (m DiffMatch) cols() (start, end int) { return m.StartCol, m.EndCol }

// FindDiffMatches returns every occurrence of term across files' displayed
// names and diff lines, in the same file/hunk/line traversal order RenderDiff
// uses — an invariant both rely on to stay in sync. Within a file, its own
// name matches sort ahead of its line matches, matching the file header row's
// position above its hunks. Returns nil for an empty term.
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
// displayed name (filePathLabel), with any file-name search matches
// highlighted inline. matches are in DiffMatch's byte-offset-into-
// filePathLabel(f) coordinate space, shifted past the glyph+space prefix
// before rendering. A row with nothing to highlight and no cursor takes a
// single renderClipped call instead of the composite-then-clip pass.
func renderFileHeader(f git.File, width int, cursor bool, matches []lineMatch) string {
	prefix := fileStatusGlyph(f.Status) + " "
	label := fileHeaderText(f)
	if len(matches) == 0 && !cursor {
		return renderClipped(fileHeaderStyle, label, width)
	}
	content := renderLineWithBaseSpans(label, nil, shiftMatches(matches, shiftBy(len(prefix))), fileHeaderLine, cursor)
	return renderClippedStyled(content, width, cursor)
}

// fileHeaderText is a file header's plain text — its status glyph and display
// path — shared by the header's own renderer and by the copyable text of the
// line it produces.
func fileHeaderText(f git.File) string {
	return fileStatusGlyph(f.Status) + " " + filePathLabel(f)
}

// filePathLabel is a file's display path: "old -> new" for renames (when the
// two differ), otherwise its current path. Shared by the diff file header and
// the stats summary so the two agree on how a rename reads.
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

// renderDiffLine renders a single diff line, word-wrapping it to width when it
// doesn't fit on one row. Each wrapped row is its own element, so it becomes
// its own entry in the flattened diff content and FileRange line-index
// bookkeeping stays correct.
//
// text is the line as diffLineText builds it, marker column included — handed
// in rather than rebuilt, the caller having already built it to carry as the
// line's plain text. spans (from intralineSpans) mark the intraline change
// within a paired add/delete line, in the same byte-offset-into-DiffLine.Text
// space as matches, so both shift past the prefix together.
func renderDiffLine(op git.LineOp, text string, width int, spans []baseSpan, matches []lineMatch, cursor bool) []string {
	style, prefix := diffLineStyle(op)

	at := shiftBy(len(prefix))
	return wrapStyledRows(text, shiftSpans(spans, at), shiftMatches(matches, at), style, width, cursor)
}

// diffLineText is a diff line as it reads on screen and as a selection copies
// it: its marker column followed by the line's own text, with tabs still tabs
// (the renderer expands those itself, per row, after wrapping).
func diffLineText(l git.DiffLine) string {
	_, prefix := diffLineStyle(l.Op)
	return prefix + l.Text
}

func diffLineStyle(op git.LineOp) (lineStyle, string) {
	switch op {
	case git.OpAdd:
		return diffAddLine, "+"
	case git.OpDelete:
		return diffDeleteLine, "-"
	default:
		return diffContextLine, " "
	}
}

// intralineSpans finds, for each maximal run of consecutive non-context lines
// in a hunk (a "replace block": the deleted lines followed by the lines that
// replaced them), the intraline change within each positionally paired
// delete/add line, keyed by that line's index into h.Lines. It follows
// diff-highlight's show_hunk: a block is paired up only when it has exactly as
// many removed lines as added ones, and an unequal block gets no spans at all.
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
			spans[di] = []baseSpan{{start: oldSpan[0], end: oldSpan[1], style: &diffDeleteEmphSpan}}
			spans[ai] = []baseSpan{{start: newSpan[0], end: newSpan[1], style: &diffAddEmphSpan}}
		}
	}

	return spans
}

// withTrailingWhitespace puts a span marking l's trailing whitespace in front
// of spans, when l is an added or removed line that has any — git's
// blank-at-eol, flagged on both sides so that removing it is as visible as
// introducing it. Context lines aren't flagged: whitespace the change didn't
// touch is noise. Leading the slice is what makes it win over an intraline
// emphasis covering the same bytes (styleAt takes the first span that does),
// which for a line differing only in trailing whitespace is exactly that run.
func withTrailingWhitespace(l git.DiffLine, spans []baseSpan) []baseSpan {
	if l.Op == git.OpContext {
		return spans
	}
	end := len(strings.TrimRight(l.Text, " \t"))
	if end == len(l.Text) {
		return spans
	}
	ws := baseSpan{start: end, end: len(l.Text), style: &diffWhitespaceSpan}
	return append([]baseSpan{ws}, spans...)
}

// intralineDiff finds the byte-offset [start,end) "changed middle" of oldText
// and newText: the region left after trimming their common prefix and common
// suffix (rune-aware, so a multi-byte UTF-8 sequence is never split), the same
// technique git's contrib/diff-highlight uses to stay cheap on short lines
// instead of needing a general LCS.
//
// ok is false when the lines are identical, or when neither the common prefix
// nor the common suffix holds any non-space content — the two lines then share
// nothing meaningful, and marking almost the whole line as changed says no
// more than the flat add/delete color already does. That mirrors
// diff-highlight's is_pair_interesting check rather than a percentage cutoff:
// one shared non-space character at either end is enough, however large the
// changed middle.
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
