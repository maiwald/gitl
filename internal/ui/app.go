package ui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/maiwald/gitl/internal/git"
)

// batchSize is how many commits are fetched per request, both for the
// initial synchronous load and for subsequent lazy-loaded batches.
const batchSize = 300

// Model is the root Bubble Tea model for the application.
type Model struct {
	width, height int
	quitting      bool

	repo *git.Repo
	list ListModel
	err  error
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

type errMsg struct{ err error }

func loadInitialCommits() tea.Msg {
	ctx := context.Background()
	repo, err := git.OpenRepo(ctx, ".")
	if err != nil {
		return errMsg{err}
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

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.list.SetSize(m.width, m.height)
		return m, nil
	case commitsLoadedMsg:
		m.repo = msg.repo
		m.list = NewListModel(msg.commits)
		m.list.SetTotalCount(msg.totalCount)
		m.list.SetSize(m.width, m.height)
		return m, nil
	case moreCommitsLoadedMsg:
		m.list.AppendCommits(msg.commits)
		return m, nil
	case errMsg:
		m.err = msg.err
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		}
		if req, ok := m.list.Update(msg); ok {
			return m, fetchMoreCmd(m.repo, req)
		}
		return m, nil
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
	return m.list.View()
}
