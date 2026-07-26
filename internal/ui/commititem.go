package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/maiwald/gitl/internal/git"
)

const authorColumnWidth = 18

// renderCommitRow renders a single commit list row: date, author, ref
// decorations, and the commit subject, truncated/padded to width.
func renderCommitRow(c git.Commit, width int, selected bool) string {
	date := c.AuthorDate.Format("2006-01-02")
	author := fmt.Sprintf("%-*s", authorColumnWidth, truncate(c.AuthorName, authorColumnWidth))
	if !selected {
		date = dateStyle.Render(date)
		author = authorStyle.Render(author)
	}
	refs := formatRefs(c.Refs, selected)

	var row string
	if refs != "" {
		row = fmt.Sprintf("%s  %s  %s %s", date, author, refs, c.Subject)
	} else {
		row = fmt.Sprintf("%s  %s  %s", date, author, c.Subject)
	}

	style := normalRowStyle
	if selected {
		style = selectedRowStyle
	}
	return style.Width(width).MaxWidth(width).Render(row)
}

// formatRefs renders branch/tag/HEAD decorations as bracketed badges. When
// selected, badges skip their own foreground color so the row's selection
// background stays consistent.
func formatRefs(refs []string, selected bool) string {
	if len(refs) == 0 {
		return ""
	}
	style := refStyle
	if selected {
		style = lipgloss.NewStyle().Bold(true)
	}
	parts := make([]string, len(refs))
	for i, r := range refs {
		parts[i] = style.Render("[" + r + "]")
	}
	return strings.Join(parts, " ")
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
