package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/maiwald/gitl/internal/git"
)

func TestRenderWorktreeRowShowsTitleAndCounts(t *testing.T) {
	s := git.EntrySummary{Kind: git.EntryStaged, Files: 2, Additions: 5, Deletions: 1}
	row := renderWorktreeRow(s, 80, false, false)

	if !strings.Contains(row, "Staged changes") {
		t.Errorf("row = %q, want it to contain the entry title", row)
	}
	if !strings.Contains(row, "2 files") {
		t.Errorf("row = %q, want it to contain the file count", row)
	}
	if !strings.Contains(row, diffAddStyle.Render("+5")) {
		t.Errorf("row = %q, want it to contain the styled addition total +5", row)
	}
	if !strings.Contains(row, diffDeleteStyle.Render("-1")) {
		t.Errorf("row = %q, want it to contain the styled deletion total -1", row)
	}
}

func TestRenderWorktreeRowDimmedReplacesCountColors(t *testing.T) {
	s := git.EntrySummary{Kind: git.EntryWorktree, Files: 1, Additions: 1}
	row := renderWorktreeRow(s, 80, false, true)

	if strings.Contains(row, diffAddStyle.Render("+1")) {
		t.Errorf("row = %q, want the addition count styled dim, not diffAddStyle, while dimmed", row)
	}
}

// TestRenderWorktreeRowLeadingColumnsAlignWithCommitRow pins down the
// alignment invariant renderWorktreeRow exists to preserve: its blank
// sha/date(/clock) columns must have the same visible width as
// renderCommitRow's real ones, at whatever width is currently active, so
// the synthetic row's title lands exactly where a real row's author would.
// Since the date/time block's width now genuinely differs between narrow
// and wide layouts (see narrowListWidth), this is checked at both.
func TestRenderWorktreeRowLeadingColumnsAlignWithCommitRow(t *testing.T) {
	c := fixtureCommitForRow()
	s := git.EntrySummary{Kind: git.EntryStaged}

	for _, width := range []int{80, 120} {
		commitRow := renderCommitRow(c, width, false, false, "", false)
		worktreeRow := renderWorktreeRow(s, width, false, false)

		authorWidth := authorColumnWidth
		authorName := c.AuthorName
		if width < narrowListWidth {
			authorWidth = narrowAuthorColumnWidth
			authorName = abbreviateName(authorName)
		}
		authorText := authorStyle.Render(fmt.Sprintf("%-*s", authorWidth, truncate(authorName, authorWidth)))
		authorIdx := strings.Index(commitRow, authorText)
		if authorIdx < 0 {
			t.Fatalf("width %d: could not locate author field in commit row %q", width, commitRow)
		}
		commitPrefixWidth := lipgloss.Width(commitRow[:authorIdx])

		titleText := entryTitleStyle.Render(fmt.Sprintf("%-*s", entryTitleWidth, entryTitle(s.Kind)))
		titleIdx := strings.Index(worktreeRow, titleText)
		if titleIdx < 0 {
			t.Fatalf("width %d: could not locate title field in worktree row %q", width, worktreeRow)
		}
		worktreePrefixWidth := lipgloss.Width(worktreeRow[:titleIdx])

		if commitPrefixWidth != worktreePrefixWidth {
			t.Errorf("width %d: commit row's author starts at visible column %d, worktree row's title starts at %d, want them equal", width, commitPrefixWidth, worktreePrefixWidth)
		}
	}
}
