package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
)

func TestModelQuitsOnQ(t *testing.T) {
	tm := teatest.NewTestModel(t, NewModel(StartTarget{}), teatest.WithInitialTermSize(80, 24))
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

func TestModelQuitsOnEsc(t *testing.T) {
	tm := teatest.NewTestModel(t, NewModel(StartTarget{}), teatest.WithInitialTermSize(80, 24))
	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

func TestModelQuitsOnCtrlC(t *testing.T) {
	tm := teatest.NewTestModel(t, NewModel(StartTarget{}), teatest.WithInitialTermSize(80, 24))
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

func TestModelShowsErrorMessage(t *testing.T) {
	m := Model{err: errors.New("this repository has no commits yet"), jl: &jumplist{}}
	if !strings.Contains(m.View(), "this repository has no commits yet") {
		t.Fatalf("View() = %q, want it to contain the error message", m.View())
	}
}

// TestModelErrorStateDoesNotPanicOnKeypress guards against a zero-valued
// repo/list (as happens when startup fails before commitsLoadedMsg arrives)
// causing a nil-pointer panic when the user presses keys anyway.
func TestModelErrorStateDoesNotPanicOnKeypress(t *testing.T) {
	m := Model{err: errors.New("boom"), jl: &jumplist{}}
	for _, k := range []tea.KeyMsg{
		{Type: tea.KeyDown}, {Type: tea.KeyUp}, {Type: tea.KeyEnter},
	} {
		newModel, _ := m.Update(k)
		m = newModel.(Model)
	}
}
