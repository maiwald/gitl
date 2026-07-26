package ui

import (
	"fmt"
	"strings"

	"github.com/maiwald/gitl/internal/git"
)

// DetailModel renders full metadata and the commit message for a single
// commit. Diff rendering is added in a later stage.
type DetailModel struct {
	detail        *git.CommitDetail
	width, height int
}

// NewDetailModel constructs a detail view for the given commit.
func NewDetailModel(detail *git.CommitDetail) DetailModel {
	return DetailModel{detail: detail}
}

// SetSize records the available rendering area.
func (m *DetailModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m DetailModel) View() string {
	if m.detail == nil {
		return ""
	}
	d := m.detail

	var b strings.Builder
	fmt.Fprintf(&b, "commit %s\n", d.Hash)
	if len(d.Parents) > 1 {
		fmt.Fprintf(&b, "Merge:  %s\n", strings.Join(d.Parents, " "))
	}
	fmt.Fprintf(&b, "Author: %s <%s>\n", d.AuthorName, d.AuthorEmail)
	fmt.Fprintf(&b, "Date:   %s\n\n", d.AuthorDate.Format("2006-01-02 15:04:05 -0700"))
	b.WriteString(strings.TrimRight(d.Body, "\n"))
	b.WriteString("\n")

	for _, f := range d.Files {
		fmt.Fprintf(&b, "\ndiff --git a/%s b/%s\n", f.OldPath, f.Path)
		for _, h := range f.Hunks {
			b.WriteString(h.Header)
			b.WriteString("\n")
			for _, line := range h.Lines {
				b.WriteString(diffLinePrefix(line.Op))
				b.WriteString(line.Text)
				b.WriteString("\n")
			}
		}
	}

	return b.String()
}

func diffLinePrefix(op git.LineOp) string {
	switch op {
	case git.OpAdd:
		return "+"
	case git.OpDelete:
		return "-"
	default:
		return " "
	}
}
