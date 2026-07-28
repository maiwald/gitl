package ui

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/maiwald/gitl/internal/git"
)

// batchSize is how many commits are fetched per request, both for the
// initial synchronous load and for subsequent lazy-loaded batches.
const batchSize = 300

// view identifies which screen is currently active.
type view int

const (
	viewList view = iota
	viewDetail
)

// StartTarget describes what gitl was asked to open on startup: nothing
// (the plain list), or the staged-changes entry. Built by main.go from the
// command line.
type StartTarget struct {
	Rev    string
	Staged bool
}

// Model is the root Bubble Tea model for the application.
type Model struct {
	width, height int
	quitting      bool

	start StartTarget
	// quitOnBack makes Back/q from the detail view quit outright instead of
	// returning to the list. Set once, in NewModel, from start.Staged: the
	// whole point of `gitl --staged` is to replace `git diff --staged |
	// less`, and there is no list behind that detail view to return to.
	// Every other path — including opening the staged row interactively
	// from the list — leaves this false, since nothing else ever writes it.
	quitOnBack bool

	repo    *git.Repo
	active  view
	list    ListModel
	detail  DetailModel
	err     error
	entries []git.WorktreeEntry // canonical copy, mirrored into list.leading as it and the list each become available

	searchPromptOpen bool
	searchInput      textinput.Model
	pendingSearch    *pendingSearch

	// searchTerm and searchDismissed are the single, canonical search state
	// shared by both views — see setSearchDisplay, the only place that
	// writes them. Neither ListModel nor DetailModel owns its own copy;
	// each keeps a display mirror pushed down from here (via
	// SetSearchDisplay) purely so their own internal render calls don't
	// need term/dismissed threaded through as parameters everywhere.
	searchTerm      string
	searchDismissed bool
}

// pendingSearch tracks the two independent, concurrent git queries a list
// search needs before it can compute match positions: which commits match
// (by hash), and the full hash order (to map a matched hash to a position).
// Whichever message arrives first is recorded and pumpPendingSearch is
// re-tried; the search only resolves once both are in.
type pendingSearch struct {
	term         string
	matchSet     map[string]bool
	haveMatchSet bool
	order        []string
	haveOrder    bool
}

// NewModel constructs the initial root model for the given startup target.
func NewModel(target StartTarget) Model {
	return Model{start: target, quitOnBack: target.Staged}
}

func (m Model) Init() tea.Cmd {
	return openRepoCmd()
}

// repoOpenedMsg reports the outcome of opening the repo and checking
// whether it has any commits — the one thing every startup path needs
// before it can dispatch its own next commands (see Update's handling of
// it).
type repoOpenedMsg struct {
	repo       *git.Repo
	hasCommits bool
}

type commitsLoadedMsg struct {
	commits    []git.Commit
	totalCount int
}

type moreCommitsLoadedMsg struct {
	commits []git.Commit
}

type commitDetailLoadedMsg struct {
	detail *git.CommitDetail
}

// worktreeEntriesLoadedMsg carries the synthetic working-tree/staged rows.
// It can arrive before or after commitsLoadedMsg (they're independent
// concurrent fetches), so it's never applied only to whatever ListModel
// exists at the moment it arrives — see Update's handling of both messages,
// which each re-mirror Model.entries into the list.
type worktreeEntriesLoadedMsg struct {
	entries []git.WorktreeEntry
}

type errMsg struct{ err error }

func openRepoCmd() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		repo, err := git.OpenRepo(ctx, ".")
		if err != nil {
			return errMsg{err}
		}
		hasCommits, err := repo.HasCommits(ctx)
		if err != nil {
			return errMsg{err}
		}
		return repoOpenedMsg{repo: repo, hasCommits: hasCommits}
	}
}

func loadListCmd(repo *git.Repo) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		commits, err := repo.FetchLogBatch(ctx, 0, batchSize)
		if err != nil {
			return errMsg{err}
		}
		total, err := repo.CommitCount(ctx)
		if err != nil {
			return errMsg{err}
		}
		return commitsLoadedMsg{commits: commits, totalCount: total}
	}
}

func loadWorktreeEntriesCmd(repo *git.Repo) tea.Cmd {
	return func() tea.Msg {
		entries, err := repo.FetchWorktreeEntries(context.Background())
		if err != nil {
			return errMsg{err}
		}
		return worktreeEntriesLoadedMsg{entries: entries}
	}
}

func fetchMoreCmd(repo *git.Repo, req fetchRequest) tea.Cmd {
	return func() tea.Msg {
		commits, err := repo.FetchLogBatch(context.Background(), req.skip, req.limit)
		if err != nil {
			return errMsg{err}
		}
		return moreCommitsLoadedMsg{commits: commits}
	}
}

// openDetail installs detail as the active view, mirroring the shared
// search state into it so an already-active search keeps working across
// commits/entries (a freshly constructed DetailModel otherwise defaults to
// searchDismissed=false, which would make an already-dismissed search
// suddenly reappear just because a new commit/entry was opened).
func (m Model) openDetail(detail *git.CommitDetail) Model {
	m.detail = NewDetailModel(detail)
	m.detail.SetSize(m.width, m.contentHeight())
	m.active = viewDetail
	if m.searchTerm != "" {
		m.detail.SetSearchDisplay(m.searchTerm, m.searchDismissed)
		m.detail.RunSearch(m.searchTerm)
	}
	return m
}

func fetchDetailCmd(repo *git.Repo, hash string) tea.Cmd {
	return func() tea.Msg {
		detail, err := repo.FetchCommitDetail(context.Background(), hash)
		if err != nil {
			return errMsg{err}
		}
		return commitDetailLoadedMsg{detail: detail}
	}
}

// fetchEntryDetailCmd fetches a single synthetic entry's diff directly (for
// a CLI `--staged` startup jump, which has no list to select the row from)
// and opens it the same way any other detail view opens, via
// commitDetailLoadedMsg.
func fetchEntryDetailCmd(repo *git.Repo, kind git.EntryKind) tea.Cmd {
	return func() tea.Msg {
		files, err := repo.FetchWorktreeEntry(context.Background(), kind)
		if err != nil {
			return errMsg{err}
		}
		return commitDetailLoadedMsg{detail: git.EntryDetail(git.WorktreeEntry{Kind: kind, Files: files})}
	}
}

type searchHashesLoadedMsg struct {
	term   string
	hashes map[string]bool
}

type allHashesLoadedMsg struct {
	term   string
	hashes []string
}

func searchHashesCmd(repo *git.Repo, term string) tea.Cmd {
	return func() tea.Msg {
		hashes, err := repo.SearchCommitHashes(context.Background(), term)
		if err != nil {
			return errMsg{err}
		}
		return searchHashesLoadedMsg{term: term, hashes: hashes}
	}
}

func allHashesCmd(repo *git.Repo, term string) tea.Cmd {
	return func() tea.Msg {
		hashes, err := repo.AllCommitHashes(context.Background())
		if err != nil {
			return errMsg{err}
		}
		return allHashesLoadedMsg{term: term, hashes: hashes}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.list.SetSize(m.width, m.contentHeight())
		m.detail.SetSize(m.width, m.contentHeight())
		if m.searchPromptOpen {
			m.searchInput.Width = m.width - 2
		}
		return m, nil
	case repoOpenedMsg:
		m.repo = msg.repo
		if m.start.Staged {
			// A commit-less repo is not an error here: there's no HEAD to
			// diff the index against yet, but FetchWorktreeEntry already
			// treats that as the empty tree, same as `git diff --cached`
			// itself does.
			return m, fetchEntryDetailCmd(msg.repo, git.EntryStaged)
		}
		if !msg.hasCommits {
			m.err = errors.New("this repository has no commits yet")
			return m, nil
		}
		return m, tea.Batch(loadListCmd(msg.repo), loadWorktreeEntriesCmd(msg.repo))
	case commitsLoadedMsg:
		m.list = NewListModel(msg.commits)
		m.list.SetTotalCount(msg.totalCount)
		m.list.SetSize(m.width, m.contentHeight())
		m.list.SetLeadingEntries(m.entries)
		return m, nil
	case moreCommitsLoadedMsg:
		m.list.AppendCommits(msg.commits)
		return m, nil
	case worktreeEntriesLoadedMsg:
		m.entries = msg.entries
		m.list.SetLeadingEntries(m.entries)
		return m, nil
	case commitDetailLoadedMsg:
		return m.openDetail(msg.detail), nil
	case searchHashesLoadedMsg:
		if m.pendingSearch != nil && m.pendingSearch.term == msg.term {
			m.pendingSearch.matchSet = msg.hashes
			m.pendingSearch.haveMatchSet = true
			return m.pumpPendingSearch()
		}
		return m, nil
	case allHashesLoadedMsg:
		if m.pendingSearch != nil && m.pendingSearch.term == msg.term {
			m.pendingSearch.order = msg.hashes
			m.pendingSearch.haveOrder = true
			return m.pumpPendingSearch()
		}
		return m, nil
	case errMsg:
		m.err = msg.err
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// startListSearch kicks off the two independent git queries a list search
// needs (which commits match, and the full hash order to place them),
// concurrently — they're read-only subprocess calls with no shared state,
// so there's no reason to make one wait for the other.
func (m Model) startListSearch(term string) (Model, tea.Cmd) {
	m.pendingSearch = &pendingSearch{term: term}
	return m, tea.Batch(searchHashesCmd(m.repo, term), allHashesCmd(m.repo, term))
}

// pumpPendingSearch finalizes a list search once both halves of
// pendingSearch have arrived, computing match positions and applying them
// to the list (which may itself require a targeted fetch, dispatched here,
// to bring the first relevant match into the loaded range).
func (m Model) pumpPendingSearch() (Model, tea.Cmd) {
	p := m.pendingSearch
	if p == nil || !p.haveMatchSet || !p.haveOrder {
		return m, nil
	}
	m.pendingSearch = nil

	matches := make([]int, 0, len(p.matchSet))
	for i, h := range p.order {
		if p.matchSet[h] {
			matches = append(matches, i)
		}
	}

	needsLoad, target := m.list.Search(matches)
	if needsLoad {
		if req, ok := m.list.RequestLoadUpTo(target); ok {
			return m, fetchMoreCmd(m.repo, req)
		}
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, DefaultKeyMap.Quit) {
		m.quitting = true
		return m, tea.Quit
	}

	if m.searchPromptOpen {
		return m.handleSearchPromptKey(msg)
	}
	if m.repo != nil && key.Matches(msg, DefaultKeyMap.Search) {
		m.searchInput = textinput.New()
		m.searchInput.Prompt = "/"
		m.searchInput.Width = m.width - 2
		m.searchPromptOpen = true
		return m, m.searchInput.Focus()
	}
	if m.hasActiveSearch() {
		if key.Matches(msg, DefaultKeyMap.NextMatch) {
			return m.advanceMatch(1)
		}
		if key.Matches(msg, DefaultKeyMap.PrevMatch) {
			return m.advanceMatch(-1)
		}
		if !m.searchDismissed && key.Matches(msg, DefaultKeyMap.Back) {
			return m.dismissSearch()
		}
	}

	if m.active == viewDetail {
		return m.handleDetailViewKey(msg)
	}
	return m.handleListViewKey(msg)
}

// handleDetailViewKey routes a key press while the detail view is active:
// Back returns to the list (or quits outright, for a `gitl --staged`
// startup jump — see quitOnBack), everything else goes to the diff
// viewport.
func (m Model) handleDetailViewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, DefaultKeyMap.Back) {
		if m.quitOnBack {
			m.quitting = true
			return m, tea.Quit
		}
		m.active = viewList
		return m, nil
	}
	m.detail.Update(msg)
	return m, nil
}

// handleListViewKey routes a key press while the list view is active: Back
// quits (there's nowhere "back" to go from the list), Enter opens the
// selected commit or synthetic entry, everything else goes to the list's
// own cursor/scroll handling.
func (m Model) handleListViewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, DefaultKeyMap.Back) {
		m.quitting = true
		return m, tea.Quit
	}
	if key.Matches(msg, DefaultKeyMap.Enter) {
		// A synthetic entry's files are already loaded (from startup), so
		// opening one needs no fetch — unlike a real commit, which is only
		// ever loaded on demand.
		if entry, ok := m.list.SelectedEntry(); ok {
			return m.openDetail(git.EntryDetail(entry)), nil
		}
		if commit, ok := m.list.Selected(); ok {
			return m, fetchDetailCmd(m.repo, commit.Hash)
		}
		return m, nil
	}
	if req, ok := m.list.Update(msg); ok {
		return m, fetchMoreCmd(m.repo, req)
	}
	return m, nil
}

// handleSearchPromptKey routes a key press while the search prompt is open.
// Esc cancels (checked directly, not via DefaultKeyMap.Back, since Back is
// also bound to "q" and a query must be able to contain a literal q), and so
// does Backspace on an already-empty query (the common "one more backspace
// closes the prompt" convention, e.g. browser address bars). Everything else
// not handled here is forwarded to the text input so the query can contain
// any character.
func (m Model) handleSearchPromptKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyEsc:
		return m.cancelSearchPrompt()
	case msg.Type == tea.KeyBackspace && m.searchInput.Value() == "":
		return m.cancelSearchPrompt()
	case key.Matches(msg, DefaultKeyMap.Enter):
		return m.submitSearch()
	}
	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	return m, cmd
}

// cancelSearchPrompt closes the search prompt without submitting, shared by
// both of handleSearchPromptKey's cancellation paths.
func (m Model) cancelSearchPrompt() (tea.Model, tea.Cmd) {
	m.searchPromptOpen = false
	m.searchInput.Blur()
	return m, nil
}

// submitSearch closes the prompt on Enter and starts a search for the
// submitted term in whichever view is active. An empty term clears the
// search entirely instead (a vim-:noh-like escape hatch) — unlike
// dismissSearch, which only hides it.
func (m Model) submitSearch() (tea.Model, tea.Cmd) {
	term := m.searchInput.Value()
	m.searchPromptOpen = false
	m.searchInput.Blur()

	if term == "" {
		m.setSearchDisplay("", false)
		m.list.ClearSearch()
		m.detail.ClearSearch()
		return m, nil
	}

	m.setSearchDisplay(term, false)
	if m.active == viewDetail {
		m.detail.RunSearch(term)
		return m, nil
	}
	return m.startListSearch(term)
}

// setSearchDisplay is the sole writer of the canonical search term/
// dismissed state, pushing both down to each view's own display mirror so
// highlighting and the status bar stay consistent no matter which view is
// active when the change happens or which view later becomes active.
func (m *Model) setSearchDisplay(term string, dismissed bool) {
	m.searchTerm = term
	m.searchDismissed = dismissed
	m.list.SetSearchDisplay(term, dismissed)
	m.detail.SetSearchDisplay(term, dismissed)
}

// hasActiveSearch reports whether a search is currently active, gating n/N
// and Esc/q-dismiss interception in handleKey. It's shared by both views —
// see setSearchDisplay.
func (m Model) hasActiveSearch() bool {
	return m.searchTerm != ""
}

// dismissSearch hides the active search's highlighting without discarding
// its term/matches (vim :noh-style) — the first Esc/q after a search goes
// active is absorbed here instead of falling through to Back's normal
// quit/go-back handling; n/N (via advanceMatch) revives it.
func (m Model) dismissSearch() (Model, tea.Cmd) {
	m.setSearchDisplay(m.searchTerm, true)
	return m, nil
}

// advanceMatch cycles the active view's search to the next (delta>0) or
// previous match, reviving a dismissed search first. In the list view,
// this may dispatch a targeted fetch if the newly-selected match isn't
// loaded yet; the detail view is fully synchronous and never needs one.
func (m Model) advanceMatch(delta int) (Model, tea.Cmd) {
	m.setSearchDisplay(m.searchTerm, false)

	if m.active == viewDetail {
		if delta > 0 {
			m.detail.NextMatch()
		} else {
			m.detail.PrevMatch()
		}
		return m, nil
	}

	var needsLoad bool
	var target int
	if delta > 0 {
		needsLoad, target = m.list.NextMatch()
	} else {
		needsLoad, target = m.list.PrevMatch()
	}
	if needsLoad {
		if req, ok := m.list.RequestLoadUpTo(target); ok {
			return m, fetchMoreCmd(m.repo, req)
		}
	}
	return m, nil
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}
	if m.err != nil {
		return errStyle.Render("error: "+m.err.Error()) + "\n"
	}
	var body string
	if m.active == viewDetail {
		body = m.detail.View()
	} else {
		body = m.list.View()
	}
	return body + "\n" + m.renderStatusBar()
}

// contentHeight is how many rows remain for the list/detail views once the
// bottom status bar's single row is reserved.
func (m Model) contentHeight() int {
	h := m.height - 1
	if h < 0 {
		h = 0
	}
	return h
}

// renderStatusBar renders the bottom status bar: the live query while the
// search prompt is open, a loading indicator while the list's
// position-finding queries are in flight, or the active view's search
// term/position otherwise.
func (m Model) renderStatusBar() string {
	var content string
	switch {
	case m.searchPromptOpen:
		content = m.searchInput.View()
	case m.pendingSearch != nil:
		content = fmt.Sprintf("/%s  searching…", m.pendingSearch.term)
	case m.searchTerm != "" && !m.searchDismissed:
		var pos, total int
		if m.active == viewDetail {
			pos, total = m.detail.MatchPosition()
		} else {
			pos, total = m.list.MatchPosition()
		}
		if total == 0 {
			content = fmt.Sprintf("/%s  no matches", m.searchTerm)
		} else {
			content = fmt.Sprintf("/%s  [%d/%d]", m.searchTerm, pos, total)
		}
	}
	return statusBarStyle.Width(m.width).Render(content)
}
