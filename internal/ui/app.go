package ui

import (
	"context"
	"errors"

	"github.com/charmbracelet/bubbles/key"
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

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.list.SetSize(m.width, m.contentHeight())
		m.detail.SetSize(m.width, m.contentHeight())
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
		return m, nil
	case errMsg:
		m.err = msg.err
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, DefaultKeyMap.Quit) {
		m.quitting = true
		return m, tea.Quit
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

// renderStatusBar renders the bottom status bar. It's currently always
// empty; search state will give it content in a later change.
func (m Model) renderStatusBar() string {
	return statusBarStyle.Width(m.width).Render("")
}
