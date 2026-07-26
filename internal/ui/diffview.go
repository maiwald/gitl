package ui

import (
	"fmt"

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
			lines = append(lines, hunkHeaderStyle.Render(h.Header))
			for _, l := range h.Lines {
				lines = append(lines, renderDiffLine(l))
			}
		}
		ranges = append(ranges, FileRange{FileIndex: i, StartLine: start, EndLine: len(lines)})
	}
	return lines, ranges
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
	return fileHeaderStyle.Width(width).MaxWidth(width).Render(label)
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

func renderDiffLine(l git.DiffLine) string {
	switch l.Op {
	case git.OpAdd:
		return diffAddStyle.Render("+" + l.Text)
	case git.OpDelete:
		return diffDeleteStyle.Render("-" + l.Text)
	default:
		return diffContextStyle.Render(" " + l.Text)
	}
}
