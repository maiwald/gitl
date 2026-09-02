package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/maiwald/gitl/internal/git"
)

// fixtureDetailWithBlanks builds a one-file commit whose diff has blank lines
// in known places: a context blank, and an added line that is empty in the
// file it came from (its text is just the "+" marker on screen).
func fixtureDetailWithBlanks() *git.CommitDetail {
	line := func(op git.LineOp, text string) git.DiffLine {
		return git.DiffLine{Op: op, Text: text}
	}
	return &git.CommitDetail{
		Commit: git.Commit{Hash: "abc123", Subject: "subject"},
		Files: []git.File{{
			Path:   "para.go",
			Status: git.StatusModified,
			Hunks: []git.Hunk{{
				Header: "@@ -1,7 +1,7 @@",
				Lines: []git.DiffLine{
					line(git.OpContext, "package main"), // +0
					line(git.OpContext, ""),             // +1 blank
					line(git.OpContext, "func a() {}"),  // +2
					line(git.OpAdd, ""),                 // +3 blank (added empty line)
					line(git.OpAdd, "func b() {}"),      // +4
					line(git.OpContext, "func c() {}"),  // +5
				},
			}},
		}},
	}
}

// diffLineRow is the flattened row of the nth diff content line of the first
// file: past the file header and the hunk header.
func diffLineRow(m *DetailModel, n int) int {
	return m.fileRanges[0].StartLine + 2 + n
}

func TestParagraphMotionStopsAtBlankLines(t *testing.T) {
	m := NewDetailModel(fixtureDetailWithBlanks(), &jumplist{})
	m.SetSize(120, 40)

	m.setCursor(diffLineRow(&m, 0)) // "package main"
	m.jumpParagraph(1)
	if got, want := m.cursorLine, diffLineRow(&m, 1); got != want {
		t.Fatalf("} from a text line: cursor %d, want the blank at %d", got, want)
	}

	// From a blank, } crosses the block that follows and stops at the blank
	// after it — the added empty line, whose text on screen is just "+".
	m.jumpParagraph(1)
	if got, want := m.cursorLine, diffLineRow(&m, 3); got != want {
		t.Fatalf("} from a blank: cursor %d, want the added blank at %d", got, want)
	}

	m.jumpParagraph(-1)
	if got, want := m.cursorLine, diffLineRow(&m, 1); got != want {
		t.Errorf("{ back: cursor %d, want the blank at %d", got, want)
	}
}

func TestParagraphMotionClampsAtTheEnds(t *testing.T) {
	m := NewDetailModel(fixtureDetailWithBlanks(), &jumplist{})
	m.SetSize(120, 40)

	m.setCursor(0)
	for i := 0; i < 20; i++ {
		m.jumpParagraph(1)
	}
	if m.cursorLine != m.blockStart(m.contentLen-1) {
		t.Errorf("} past the end: cursor %d, want the last line at %d", m.cursorLine, m.blockStart(m.contentLen-1))
	}

	for i := 0; i < 20; i++ {
		m.jumpParagraph(-1)
	}
	if m.cursorLine != 0 {
		t.Errorf("{ past the start: cursor %d, want 0", m.cursorLine)
	}
}

func TestParagraphMotionIsBoundToBraces(t *testing.T) {
	m := NewDetailModel(fixtureDetailWithBlanks(), &jumplist{})
	m.SetSize(120, 40)
	m.setCursor(diffLineRow(&m, 0))

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'}'}})
	if got, want := m.cursorLine, diffLineRow(&m, 1); got != want {
		t.Fatalf("} key: cursor %d, want %d", got, want)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'{'}})
	if got, want := m.cursorLine, diffLineRow(&m, 0); got > want {
		t.Errorf("{ key: cursor %d, want at or above %d", got, want)
	}
}

// TestParagraphMotionIsJumpWorthy pins that { and } are jump commands, as they
// are in vim — a press can move the cursor a long way.
func TestParagraphMotionIsJumpWorthy(t *testing.T) {
	jl := &jumplist{}
	m := NewDetailModel(fixtureDetailWithBlanks(), jl)
	m.SetSize(120, 40)
	m.setCursor(diffLineRow(&m, 0))

	before := len(jl.back)
	m.jumpParagraph(1)
	if len(jl.back) != before+1 {
		t.Errorf("jumplist has %d entries after }, want %d", len(jl.back), before+1)
	}

	// A press that can't move (already clamped) records nothing.
	m.setCursor(0)
	m.jumpParagraph(-1)
	at := len(jl.back)
	m.jumpParagraph(-1)
	if len(jl.back) != at {
		t.Errorf("a { that couldn't move pushed a jump anyway")
	}
}

// TestParagraphMotionExtendsSelection is the whole point of routing it through
// setCursor: a new motion has to extend a selection without knowing one exists.
func TestParagraphMotionExtendsSelection(t *testing.T) {
	m := NewDetailModel(fixtureDetailWithBlanks(), &jumplist{})
	m.SetSize(120, 40)
	m.setCursor(diffLineRow(&m, 0))
	m.startSelection()

	m.jumpParagraph(1)
	if n := m.selectedLineCount(); n != 2 {
		t.Errorf("selectedLineCount = %d after } from the anchor, want 2", n)
	}
}
