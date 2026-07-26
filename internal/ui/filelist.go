package ui

import (
	"fmt"
	"strings"

	"github.com/maiwald/gitl/internal/git"
)

// FileListModel renders the list of files changed in a commit, with the
// entry corresponding to the diff panel's current scroll position
// highlighted (wired up in a later stage; for now the first file is
// highlighted as a static placeholder).
type FileListModel struct {
	files          []git.File
	highlightIndex int
	width, height  int
}

// NewFileListModel constructs a file list view over the given files.
func NewFileListModel(files []git.File) FileListModel {
	return FileListModel{files: files}
}

// SetSize records the available rendering area.
func (m *FileListModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m FileListModel) View() string {
	lines := make([]string, len(m.files))
	for i, f := range m.files {
		label := fmt.Sprintf("%s %s", fileStatusGlyph(f.Status), f.Path)
		style := normalRowStyle
		if i == m.highlightIndex {
			style = selectedRowStyle
		}
		lines[i] = style.Width(m.width).MaxWidth(m.width).Render(label)
	}
	return strings.Join(lines, "\n")
}
