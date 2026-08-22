package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/maiwald/gitl/internal/git"
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

// TestDeferredJumpIsRetriedAfterAnInFlightFetchLands pins down that a jump
// whose fetch was refused because another was already in flight doesn't
// simply wait forever. G asks for the very bottom of a long history while a
// lazy-load batch is out; when that batch lands short of the target, the
// deferred jump has to ask again on its own behalf.
func TestDeferredJumpIsRetriedAfterAnInFlightFetchLands(t *testing.T) {
	jl := &jumplist{}
	list := NewListModel(fixtureCommits(26), jl)
	list.SetTotalCount(1000)
	list.SetSize(80, 5)
	m := Model{active: viewList, list: list, repo: &git.Repo{}, jl: jl}
	m.width, m.height = 80, 24

	// Scroll close enough to the loaded end to put a lazy-load fetch in flight.
	m.list.cursor = 25 - loadMoreLookahead
	newModel, cmd := m.Update(keyRune('j'))
	m = newModel.(Model)
	if cmd == nil {
		t.Fatal("expected a lazy-load fetch once the cursor neared the loaded end")
	}

	// G now wants the last commit, but can't get a fetch of its own.
	newModel, cmd = m.Update(keyRune('G'))
	m = newModel.(Model)
	if cmd != nil {
		t.Fatal("a second fetch must not be dispatched while one is in flight")
	}
	if _, ok := m.list.pendingTarget(); !ok {
		t.Fatal("G should have left a deferred jump behind")
	}

	// The in-flight batch lands, still short of the target.
	newModel, cmd = m.Update(moreCommitsLoadedMsg{commits: fixtureCommits(26)})
	m = newModel.(Model)
	if cmd == nil {
		t.Fatal("the deferred jump should re-request the commits it still needs")
	}
}
