package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/maiwald/gitl/internal/git"
)

// TestRepoOpenedPipedPathFetchesOnlyTheHeader pins down that a piped
// `git show <rev>` startup fetches the commit's header and nothing else: it
// doesn't gate on hasCommits the way the plain path does (see
// TestRepoOpenedPlainPathErrorsOnNoCommits), and loads no list behind the
// detail view.
func TestRepoOpenedPipedPathFetchesOnlyTheHeader(t *testing.T) {
	m := NewModel(StartTarget{Piped: &git.PipedPatch{Hash: "abc123"}})
	newModel, cmd := m.Update(repoOpenedMsg{repo: &git.Repo{Path: "."}, hasCommits: false})
	m = newModel.(Model)
	if cmd == nil {
		t.Fatal("expected a command fetching the piped commit's header")
	}
	if m.err != nil {
		t.Fatalf("err = %v, want nil", m.err)
	}
	if _, isBatch := cmd().(tea.BatchMsg); isBatch {
		t.Fatal("got a batch of commands, want only the header fetch")
	}
}

// TestPipedRunIsASingleScreen pins down that a piped patch — with a commit
// header or without — behaves like a pager: ctrl-o has nowhere to go, and
// Back quits rather than revealing a list.
func TestPipedRunIsASingleScreen(t *testing.T) {
	for _, hash := range []string{"", "abc123"} {
		m := NewModel(StartTarget{Piped: &git.PipedPatch{Hash: hash}})
		m.width, m.height = 80, 24
		newModel, _ := m.Update(commitDetailLoadedMsg{
			detail: &git.CommitDetail{Kind: git.EntryPiped, Files: []git.File{{Path: "a.txt"}}},
		})
		m = newModel.(Model)

		newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
		m = newModel.(Model)
		if m.active != viewDetail {
			t.Fatalf("hash %q: active view = %v after ctrl+o, want viewDetail", hash, m.active)
		}

		newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
		if !newModel.(Model).quitting || cmd == nil {
			t.Fatalf("hash %q: q did not quit", hash)
		}
	}
}
