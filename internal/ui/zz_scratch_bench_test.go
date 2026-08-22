package ui

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/maiwald/gitl/internal/git"
)

func bigDetail(files, linesPerHunk int) *git.CommitDetail {
	d := &git.CommitDetail{Body: "subject\n\nbody line\n"}
	for f := 0; f < files; f++ {
		file := git.File{Path: fmt.Sprintf("internal/pkg/file%d.go", f), Status: git.StatusModified}
		h := git.Hunk{Header: "@@ -1,20 +1,20 @@ func something()"}
		for i := 0; i < linesPerHunk; i++ {
			op := git.OpContext
			if i%5 == 0 {
				op = git.OpAdd
			} else if i%7 == 0 {
				op = git.OpDelete
			}
			h.Lines = append(h.Lines, git.DiffLine{Op: op, Text: fmt.Sprintf("\tsomeCall(arg%d, other) // a fairly typical line of source code", i)})
		}
		file.Hunks = append(file.Hunks, h)
		d.Files = append(d.Files, file)
	}
	return d
}

func BenchmarkDetailCursorMove(b *testing.B) {
	m := NewDetailModel(bigDetail(40, 200), &jumplist{})
	m.SetSize(100, 40)
	b.ResetTimer()
	down := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	for i := 0; i < b.N; i++ {
		m.Update(down)
	}
}
