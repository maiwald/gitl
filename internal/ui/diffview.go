package ui

import (
	"fmt"
	"strings"

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
// uses) are highlighted inline; activeMatch is an index into matches for
// the one that should stand out with a distinct style (-1 for none). The
// returned matchLines is parallel to matches: the flattened line index of
// each match's logical line (the start of the line, not the precise
// wrapped sub-row/column — landing on the line is enough to bring it into
// view).
func RenderDiff(files []git.File, width int, matches []DiffMatch, activeMatch int) ([]string, []FileRange, []int) {
	var lines []string
	ranges := make([]FileRange, 0, len(files))
	matchLines := make([]int, len(matches))
	mi := 0

	for fi, f := range files {
		start := len(lines)
		lines = append(lines, renderFileHeader(f, width))
		if f.Binary {
			lines = append(lines, binaryNoticeStyle.Render("Binary file, diff not shown"))
		}
		for hi, h := range f.Hunks {
			lines = append(lines, renderClipped(hunkHeaderStyle, h.Header, width))
			for li, l := range h.Lines {
				lineStart := len(lines)
				var lineMatches []lineMatch
				for mi < len(matches) && matches[mi].FileIndex == fi && matches[mi].HunkIndex == hi && matches[mi].LineIndex == li {
					lineMatches = append(lineMatches, lineMatch{
						start: matches[mi].StartCol, end: matches[mi].EndCol, active: mi == activeMatch,
					})
					matchLines[mi] = lineStart
					mi++
				}
				lines = append(lines, renderDiffLine(l, width, lineMatches)...)
			}
		}
		ranges = append(ranges, FileRange{FileIndex: fi, StartLine: start, EndLine: len(lines)})
	}
	return lines, ranges, matchLines
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
	lowerTerm := strings.ToLower(term)

	var matches []DiffMatch
	for fi, f := range files {
		for hi, h := range f.Hunks {
			for li, l := range h.Lines {
				lowerText := strings.ToLower(l.Text)
				pos := 0
				for {
					idx := strings.Index(lowerText[pos:], lowerTerm)
					if idx < 0 {
						break
					}
					idx += pos
					matches = append(matches, DiffMatch{
						FileIndex: fi, HunkIndex: hi, LineIndex: li,
						StartCol: idx, EndCol: idx + len(term),
					})
					pos = idx + len(term)
				}
			}
		}
	}
	return matches
}

// FileAt returns the index into files (as returned alongside ranges by
// RenderDiff) whose rendered lines contain lineIndex, via binary search.
// It returns -1 if lineIndex falls outside all ranges (e.g. no files).
func FileAt(ranges []FileRange, lineIndex int) int {
	lo, hi := 0, len(ranges)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		r := ranges[mid]
		switch {
		case lineIndex < r.StartLine:
			hi = mid - 1
		case lineIndex >= r.EndLine:
			lo = mid + 1
		default:
			return r.FileIndex
		}
	}
	return -1
}

func renderFileHeader(f git.File, width int) string {
	path := f.Path
	if f.Status == git.StatusRenamed && f.OldPath != f.Path {
		path = f.OldPath + " -> " + f.Path
	}
	label := fmt.Sprintf("%s %s", fileStatusGlyph(f.Status), path)
	return renderClipped(fileHeaderStyle, label, width)
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
// keeping FileRange line-index bookkeeping correct.
func renderDiffLine(l git.DiffLine, width int, matches []lineMatch) []string {
	style, prefix := diffLineStyle(l.Op)
	text := prefix + l.Text

	var content string
	if len(matches) == 0 {
		content = style.Render(text)
	} else {
		content = renderLineWithMatches(text, len(prefix), style, matches)
	}

	if width <= 0 {
		return []string{content}
	}
	// The outer wrap is colorless: content is already fully styled (base
	// color plus any highlight spans), and wrapping it through a further
	// *colored* style here would risk that color clobbering the inner
	// spans — the same nested-style convention used in commititem.go,
	// where an already-styled row is wrapped by a colorless outer style.
	return strings.Split(lipgloss.NewStyle().Width(width).Render(content), "\n")
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

// renderLineWithMatches splits text into pre/match/post spans at each
// match's byte offsets (into text, hence prefixLen to shift DiffMatch's
// DiffLine.Text-relative columns), styling unmatched spans with base and
// matched spans with searchMatchStyle/searchActiveMatchStyle, then
// concatenating the already-rendered pieces.
func renderLineWithMatches(text string, prefixLen int, base lipgloss.Style, matches []lineMatch) string {
	var b strings.Builder
	pos := 0
	for _, m := range matches {
		start, end := prefixLen+m.start, prefixLen+m.end
		b.WriteString(base.Render(text[pos:start]))
		style := searchMatchStyle
		if m.active {
			style = searchActiveMatchStyle
		}
		b.WriteString(style.Render(text[start:end]))
		pos = end
	}
	b.WriteString(base.Render(text[pos:]))
	return b.String()
}
