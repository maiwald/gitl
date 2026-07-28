package ui

import (
	"strings"
	"testing"

	"github.com/maiwald/gitl/internal/git"
)

func TestRenderWorktreeRowShowsTitleAndCounts(t *testing.T) {
	e := git.WorktreeEntry{
		Kind: git.EntryStaged,
		Files: []git.File{
			{Path: "a.txt", Additions: 3, Deletions: 1},
			{Path: "b.txt", Additions: 2},
		},
	}
	row := renderWorktreeRow(e, 80, false, false)

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
	e := git.WorktreeEntry{Kind: git.EntryWorktree, Files: []git.File{{Path: "a.txt", Additions: 1}}}
	row := renderWorktreeRow(e, 80, false, true)

	if strings.Contains(row, diffAddStyle.Render("+1")) {
		t.Errorf("row = %q, want the addition count styled dim, not diffAddStyle, while dimmed", row)
	}
}
