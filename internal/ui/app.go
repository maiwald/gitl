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

// Model is the root Bubble Tea model for the application.
type Model struct {
	width, height int
	quitting      bool

	repo   *git.Repo
	active view
	list   ListModel
	detail DetailModel
	err    error

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

// NewModel constructs the initial root model.
func NewModel() Model {
	return Model{}
}

func (m Model) Init() tea.Cmd {
	return loadInitialCommits
}

type commitsLoadedMsg struct {
	repo       *git.Repo
	commits    []git.Commit
	totalCount int
}

type moreCommitsLoadedMsg struct {
	commits []git.Commit
}

type commitDetailLoadedMsg struct {
	detail *git.CommitDetail
}

type errMsg struct{ err error }

func loadInitialCommits() tea.Msg {
	ctx := context.Background()
	repo, err := git.OpenRepo(ctx, ".")
	if err != nil {
		return errMsg{err}
	}
	hasCommits, err := repo.HasCommits(ctx)
	if err != nil {
		return errMsg{err}
	}
	if !hasCommits {
		return errMsg{errors.New("this repository has no commits yet")}
	}
	commits, err := repo.FetchLogBatch(ctx, 0, batchSize)
	if err != nil {
		return errMsg{err}
	}
	total, err := repo.CommitCount(ctx)
	if err != nil {
		return errMsg{err}
	}
	return commitsLoadedMsg{repo: repo, commits: commits, totalCount: total}
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

func fetchDetailCmd(repo *git.Repo, hash string) tea.Cmd {
	return func() tea.Msg {
		detail, err := repo.FetchCommitDetail(context.Background(), hash)
		if err != nil {
			return errMsg{err}
		}
		return commitDetailLoadedMsg{detail: detail}
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
	case commitsLoadedMsg:
		m.repo = msg.repo
		m.list = NewListModel(msg.commits)
		m.list.SetTotalCount(msg.totalCount)
		m.list.SetSize(m.width, m.contentHeight())
		return m, nil
	case moreCommitsLoadedMsg:
		m.list.AppendCommits(msg.commits)
		return m, nil
	case commitDetailLoadedMsg:
		m.detail = NewDetailModel(msg.detail)
		m.detail.SetSize(m.width, m.contentHeight())
		m.active = viewDetail
		if m.searchTerm != "" {
			// A freshly constructed DetailModel defaults to
			// searchDismissed=false; mirror the real shared state before
			// searching so an already-dismissed search doesn't suddenly
			// reappear just because a new commit was opened.
			m.detail.SetSearchDisplay(m.searchTerm, m.searchDismissed)
			m.detail.RunSearch(m.searchTerm)
		}
		return m, nil
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

	switch m.active {
	case viewDetail:
		if key.Matches(msg, DefaultKeyMap.Back) {
			m.active = viewList
			return m, nil
		}
		m.detail.Update(msg)
		return m, nil
	default: // viewList
		if key.Matches(msg, DefaultKeyMap.Back) {
			m.quitting = true
			return m, tea.Quit
		}
		if key.Matches(msg, DefaultKeyMap.Enter) {
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
}

// handleSearchPromptKey routes a key press while the search prompt is open.
// Esc cancels (checked directly, not via DefaultKeyMap.Back, since Back is
// also bound to "q" and a query must be able to contain a literal q).
// Everything else not handled here is forwarded to the text input so the
// query can contain any character.
func (m Model) handleSearchPromptKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyEsc:
		m.searchPromptOpen = false
		m.searchInput.Blur()
		return m, nil
	case key.Matches(msg, DefaultKeyMap.Enter):
		return m.submitSearch()
	}
	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	return m, cmd
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
