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

	return b.String()
}
