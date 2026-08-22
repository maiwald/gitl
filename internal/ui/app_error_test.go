package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestNonFatalErrorLeavesListVisible pins down the core behavior change:
// a non-fatal errMsg must not blank the screen — the list stays visible,
// and the failure surfaces in the status bar instead.
func TestNonFatalErrorLeavesListVisible(t *testing.T) {
	list := NewListModel(fixtureCommits(3), &jumplist{})
	list.SetSize(80, 10)
	m := Model{active: viewList, list: list, jl: &jumplist{}}
	m.width, m.height = 80, 24

	newModel, _ := m.Update(errMsg{err: errors.New("boom"), severity: nonFatal})
	m = newModel.(Model)

	if m.err != nil {
		t.Fatalf("err = %v, want nil for a non-fatal error", m.err)
	}
	if !strings.Contains(m.View(), "subject") {
		t.Fatalf("View() = %q, want the list still visible", m.View())
	}
	if !strings.Contains(m.renderStatusBar(), "boom") {
		t.Fatalf("renderStatusBar() = %q, want it to contain the error message", m.renderStatusBar())
	}
}

// TestFatalErrorBlanksScreen pins down that a fatal errMsg keeps today's
// behavior: the whole screen replaced by the error.
func TestFatalErrorBlanksScreen(t *testing.T) {
	m := Model{jl: &jumplist{}}
	m.width, m.height = 80, 24

	newModel, _ := m.Update(errMsg{err: errors.New("boom"), severity: fatal})
	m = newModel.(Model)

	if m.err == nil {
		t.Fatal("err = nil, want set for a fatal error")
	}
	if !strings.Contains(m.View(), "boom") {
		t.Fatalf("View() = %q, want it to show the error", m.View())
	}
}

// TestNonFatalErrorClearedByNextSuccessfulLoad pins down the clearing
// rule: any successful fetch retires a previously-shown non-fatal error,
// not just a retry of the same kind of fetch that failed.
func TestNonFatalErrorClearedByNextSuccessfulLoad(t *testing.T) {
	list := NewListModel(fixtureCommits(3), &jumplist{})
	list.SetSize(80, 10)
	m := Model{active: viewList, list: list, jl: &jumplist{}}
	m.width, m.height = 80, 24

	newModel, _ := m.Update(errMsg{err: errors.New("boom"), severity: nonFatal})
	m = newModel.(Model)
	if m.lastError == nil {
		t.Fatal("expected lastError to be set")
	}

	newModel, _ = m.Update(worktreeEntriesLoadedMsg{})
	m = newModel.(Model)
	if m.lastError != nil {
		t.Fatalf("lastError = %v, want nil after a successful load", m.lastError)
	}
}

// TestActiveSearchTakesPriorityOverLastErrorInStatusBar pins down that
// lastError is the status bar's lowest-priority content, per
// renderStatusBar's switch order.
func TestActiveSearchTakesPriorityOverLastErrorInStatusBar(t *testing.T) {
	list := NewListModel(fixtureCommits(3), &jumplist{})
	list.SetSize(80, 10)
	m := Model{active: viewList, list: list, jl: &jumplist{}}
	m.width, m.height = 80, 24
	m.lastError = errors.New("boom")
	m.setSearchDisplay("subject", false)
	runListSearch(&m.list, []int{0})

	bar := m.renderStatusBar()
	if strings.Contains(bar, "boom") {
		t.Fatalf("renderStatusBar() = %q, want the active search to take priority over lastError", bar)
	}
}

// TestStatusBarErrorTextFlattensNewlinesAndRespectsWidth guards against
// runGit's raw (often multi-line) stderr text ever pushing the one-row
// status bar to several rows.
func TestStatusBarErrorTextFlattensNewlinesAndRespectsWidth(t *testing.T) {
	err := errors.New("line one\nline two\nline three that is quite a bit longer than the width")
	text := statusBarErrorText(err, 20)

	if strings.Contains(text, "\n") {
		t.Fatalf("statusBarErrorText = %q, want no embedded newlines", text)
	}
	if len(text) > 20 {
		t.Fatalf("statusBarErrorText = %q (len %d), want length <= 20", text, len(text))
	}
}

// TestKeypressOnNonFatalErrorWithEmptyListDoesNotPanic mirrors
// TestModelErrorStateDoesNotPanicOnKeypress's guard, for the newly
// reachable state of a non-fatal error with the list not yet loaded.
func TestKeypressOnNonFatalErrorWithEmptyListDoesNotPanic(t *testing.T) {
	m := Model{lastError: errors.New("boom"), jl: &jumplist{}}
	m.width, m.height = 80, 24
	for _, k := range []tea.KeyMsg{
		{Type: tea.KeyDown}, {Type: tea.KeyUp}, {Type: tea.KeyEnter},
	} {
		newModel, _ := m.Update(k)
		m = newModel.(Model)
	}
}

// TestFetchMoreFailureClearsLoadingMoreWithoutUnrelatedSuccess is the
// regression guard for the real correctness risk moreCommitsFailedMsg
// exists to avoid: a pagination failure must clear loadingMore via its
// own message, not via any errMsg, so an unrelated error can't un-latch a
// fetch that's still genuinely in flight (which would risk a duplicate
// AppendCommits and corrupted indices).
func TestFetchMoreFailureClearsLoadingMoreWithoutUnrelatedSuccess(t *testing.T) {
	list := NewListModel(fixtureCommits(30), &jumplist{})
	list.SetTotalCount(100)
	list.SetSize(80, 5)
	m := Model{active: viewList, list: list, jl: &jumplist{}}
	m.width, m.height = 80, 24

	for i := 0; i < 25; i++ {
		m.list.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	if !m.list.loadingMore {
		t.Fatal("expected loadingMore to be true after scrolling near the loaded end")
	}

	newModel, _ := m.Update(moreCommitsFailedMsg{err: errors.New("boom")})
	m = newModel.(Model)

	if m.list.loadingMore {
		t.Fatal("loadingMore should be cleared by its own failure message")
	}
	if m.lastError == nil {
		t.Fatal("expected lastError to be set")
	}
}

// TestSearchFailureClearsTheInFlightReport is
// TestFetchMoreFailureClearsLoadingMoreWithoutUnrelatedSuccess's counterpart
// for search: a failed query has to retire its own "searching…" report via
// its own message, or the status bar keeps announcing a search that will
// never come back.
func TestSearchFailureClearsTheInFlightReport(t *testing.T) {
	m := newSearchableListModel()
	m = submitSearchPrompt(t, m, "Author")

	newModel, _ := m.Update(searchFailedMsg{term: "Author", err: errors.New("boom")})
	m = newModel.(Model)

	if m.searchingTerm != "" {
		t.Fatalf("searchingTerm = %q, want it cleared by the search's own failure", m.searchingTerm)
	}
	if m.lastError == nil {
		t.Fatal("expected lastError to be set")
	}
	if strings.Contains(m.View(), "searching") {
		t.Fatalf("status bar = %q, want no report of a search that failed", m.renderStatusBar())
	}
}
