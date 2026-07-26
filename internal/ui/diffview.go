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
// range each file occupies within that flattened content.
func RenderDiff(files []git.File, width int) ([]string, []FileRange) {
	var lines []string
	ranges := make([]FileRange, 0, len(files))

	for i, f := range files {
		start := len(lines)
		lines = append(lines, renderFileHeader(f, width))
		if f.Binary {
			lines = append(lines, binaryNoticeStyle.Render("Binary file, diff not shown"))
		}
		for _, h := range f.Hunks {
			lines = append(lines, renderClipped(hunkHeaderStyle, h.Header, width))
			for _, l := range h.Lines {
				lines = append(lines, renderDiffLine(l, width)...)
			}
		}
		ranges = append(ranges, FileRange{FileIndex: i, StartLine: start, EndLine: len(lines)})
	}
	return lines, ranges
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

// renderDiffLine renders a single diff line, word-wrapping it to width when
// it doesn't fit on one row. Each wrapped row is returned as its own
// element so it becomes its own entry in the flattened diff content,
// keeping FileRange line-index bookkeeping correct.
func renderDiffLine(l git.DiffLine, width int) []string {
	var style lipgloss.Style
	var prefix string
	switch l.Op {
	case git.OpAdd:
		style, prefix = diffAddStyle, "+"
	case git.OpDelete:
		style, prefix = diffDeleteStyle, "-"
	default:
		style, prefix = diffContextStyle, " "
	}

	text := prefix + l.Text
	if width <= 0 {
		return []string{style.Render(text)}
	}
	return strings.Split(style.Width(width).Render(text), "\n")
}
