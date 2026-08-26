package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/maiwald/gitl/internal/concurrent"
	"github.com/maiwald/gitl/internal/git"
)

// batchSize is how many commits are fetched per request, for the initial
// load and for each lazy-loaded batch.
const batchSize = 300

// view identifies which screen is currently active.
type view int

const (
	viewList view = iota
	viewDetail
)

// StartTarget describes what gitl was asked to open on startup: nothing (the
// plain list), a revision, the staged-changes entry, or the working-tree
// entry. Built by main.go from the command line.
//
// Paths is the optional pathspec limiting that startup diff, the way `git
// diff -- <path>...` limits its own. It applies to the startup detail view
// only, never to the commit list or to a commit opened from it.
type StartTarget struct {
	// Piped is a patch read from stdin — git running gitl as its pager —
	// rather than from one of gitl's own git invocations. Its diff is
	// already parsed; what may still be needed is the commit header behind
	// it, when Piped.Hash names one. Nil for a plain `gitl`, which opens the
	// commit list.
	Piped *git.PipedPatch
}

// opensDetail reports whether gitl was asked to open straight into a detail
// view, skipping the list as the initial screen.
func (t StartTarget) opensDetail() bool { return t.Piped != nil }

// opensList reports whether the commit list gets loaded at all. Not the
// negation of opensDetail: a `gitl <rev>` jump opens a detail view *and*
// loads the list behind it (so ctrl-o can reach the commit's place in
// history), while `--staged`/`--diff` have no list at any point.
// A piped patch loads the list only when its preamble named a commit, which
// is what gives ctrl-o somewhere to go; a bare `git diff` has no position in
// history to return to.
func (t StartTarget) opensList() bool {
	return t.Piped == nil || t.Piped.Hash != ""
}

// Model is the root Bubble Tea model for the application.
type Model struct {
	width, height int
	quitting      bool

	start StartTarget
	// quitOnBack makes Back/q from the detail view quit outright instead of
	// returning to the list — set for the startup jumps that open straight
	// into a detail view with no list to return to.
	quitOnBack bool

	repo   *git.Repo
	active view
	list   ListModel
	detail DetailModel
	// err is a fatal startup failure: View() replaces the whole screen
	// with it, and it's never cleared.
	err error
	// lastError is the latest non-fatal fetch failure, shown in the status
	// bar until the next successful fetch of any kind clears it.
	lastError error
	summaries []git.EntrySummary // canonical copy, mirrored into list.leading as it and the list each become available

	// pendingJump is a CLI-requested list position (`gitl <rev>`) not yet
	// applied, because the list or the resolved position is still missing —
	// two independent halves that can land in either order, joined by
	// pumpPendingJump.
	pendingJump *int

	searchPromptOpen bool
	searchInput      textinput.Model
	// searchingTerm is the term whose list queries are currently in flight,
	// empty when none are: it identifies which results are still wanted (a
	// superseded or cleared search's are discarded) and drives the status
	// bar's "searching…" report.
	searchingTerm string
	// allHashes is the full reachable commit-hash history in log order,
	// fetched once on the first search and reused for every later one —
	// gitl has no refresh mechanism, so it can't go stale during a run.
	// Nil means "not fetched yet".
	allHashes []string

	// searchTerm and searchDismissed are the single, canonical search state
	// shared by both views, written only by setSearchDisplay. Each view
	// keeps a display mirror pushed down from here via SetSearchDisplay.
	searchTerm      string
	searchDismissed bool
	// listMatches is the canonical copy of the list's match positions for
	// searchTerm, kept here because ListModel is rebuilt from scratch when
	// commits arrive and would otherwise drop a search submitted before
	// that. Nil while the list's half of the search hasn't run — it's only
	// fetched when something needs it, see ensureListSearch.
	listMatches []int
	// jumpOnResolve is what the list should do with the match positions it's
	// waiting on — see the jumpNone/jumpNextMatch/jumpPrevMatch constants.
	jumpOnResolve int

	// jl is the single, cross-view jumplist backing ctrl-o/ctrl-i, shared
	// by pointer with ListModel and DetailModel.
	jl *jumplist
}

// jumpPos identifies one position recorded in the jumplist: either a list
// cursor position, or a line within a specific commit/entry's detail view.
// It carries the commit, not cursorLine alone, so a jump can reconstruct a
// DetailModel for a commit that isn't the active one without re-fetching it.
type jumpPos struct {
	view       view
	listCursor int
	commit     *git.CommitDetail
	cursorLine int
}

// jumplist is the shared, cross-view jumplist backing ctrl-o/ctrl-i, matching
// vim's semantics where switching buffers (here: list <-> detail) is itself
// jump-recordable, not just movement within one. Owned by the root Model and
// referenced by pointer from both ListModel and DetailModel, which only push
// onto it; traversal lives on Model (doJumpBack/doJumpForward) since popping
// an entry can require switching the active view.
type jumplist struct {
	back, forward []jumpPos
}

// push records pos as a jumplist entry reachable via JumpBack. A fresh jump
// invalidates whatever forward history an earlier JumpBack left, as in vim.
func (j *jumplist) push(pos jumpPos) {
	j.forward = nil
	j.back = append(j.back, pos)
	if len(j.back) > jumplistCap {
		j.back = j.back[len(j.back)-jumplistCap:]
	}
}

// NewModel constructs the initial root model for the given startup target.
func NewModel(target StartTarget) Model {
	// Seeded with an entry at the list's top so ctrl-o can always get back
	// there even if the cursor only ever moved by plain motion. Skipped when
	// the run has no list at all (see opensList), where ctrl-o would strand
	// the user on a permanently empty list view.
	jl := &jumplist{}
	if target.opensList() {
		jl.back = []jumpPos{{view: viewList, listCursor: 0}}
	}
	return Model{
		start:      target,
		quitOnBack: target.opensDetail(),
		jl:         jl,
		// Built empty rather than left a zero value until commitsLoadedMsg
		// replaces it: keys reach it from the very first frame, and a zero
		// ListModel has no jumplist to push onto.
		list: NewListModel(nil, jl),
	}
}

func (m Model) Init() tea.Cmd {
	// A piped patch naming no commit needs nothing from a repo, and may not
	// be in one at all (`cat foo.patch | gitl`), so it skips opening one.
	if m.start.Piped != nil && m.start.Piped.Hash == "" {
		return showPipedCmd(m.start.Piped)
	}
	return openRepoCmd()
}

// showPipedCmd hands an already-parsed piped patch to the detail view as it
// stands, with no commit header behind it.
func showPipedCmd(p *git.PipedPatch) tea.Cmd {
	return func() tea.Msg { return commitDetailLoadedMsg{detail: p.Detail()} }
}

// fetchPipedCommitCmd fetches the header for the commit a piped patch's
// preamble named, keeping the piped diff itself — which is what preserves any
// pathspec the user gave git. A commit the repo can't produce (a shallow
// clone, a patch from elsewhere) falls back to showing the preamble verbatim
// rather than failing the run.
func fetchPipedCommitCmd(repo *git.Repo, p *git.PipedPatch) tea.Cmd {
	return func() tea.Msg {
		detail, err := repo.FetchCommitMeta(context.Background(), p.Hash)
		if err != nil {
			return commitDetailLoadedMsg{detail: p.Detail()}
		}
		detail.Files = p.Files
		return commitDetailLoadedMsg{detail: detail}
	}
}

// repoOpenedMsg reports the outcome of opening the repo and checking whether
// it has any commits — what every startup path needs before it can dispatch
// its own next commands.
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

// worktreeSummariesLoadedMsg carries the counts behind the synthetic
// working-tree/staged rows. It can arrive before or after commitsLoadedMsg
// (independent concurrent fetches), so both messages re-mirror
// Model.summaries into the list rather than relying on whichever ListModel
// exists when this one arrives.
type worktreeSummariesLoadedMsg struct {
	summaries []git.EntrySummary
}

// revPositionMsg carries the resolved list position of a CLI-requested
// revision (`gitl <rev>`), for positioning the list's cursor behind the
// detail view. found is false when the revision couldn't be resolved, or
// isn't reachable from HEAD — either way there's no sensible list position
// and the cursor is left where it is.
type revPositionMsg struct {
	pos   int
	found bool
}

// errSeverity says how much of the UI a failed fetch invalidates.
type errSeverity int

const (
	// nonFatal: whatever is already on screen stays; the failure is
	// reported in the status bar until the next successful fetch.
	nonFatal errSeverity = iota
	// fatal: there is nothing left to show, so the error replaces the
	// whole screen. Reserved for startup failures.
	fatal
)

type errMsg struct {
	err      error
	severity errSeverity
}

// runCmd wraps fetch as a tea.Cmd: on success toMsg(v) becomes the resulting
// message, on failure errMsg{err, severity} does. fetchMoreCmd and
// resolveRevPositionCmd report failures their own way instead.
func runCmd[T any](severity errSeverity, fetch func(context.Context) (T, error), toMsg func(T) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		v, err := fetch(context.Background())
		if err != nil {
			return errMsg{err: err, severity: severity}
		}
		return toMsg(v)
	}
}

func openRepoCmd() tea.Cmd {
	return runCmd(fatal, func(ctx context.Context) (repoOpenedMsg, error) {
		repo, err := git.OpenRepo(ctx, ".")
		if err != nil {
			return repoOpenedMsg{}, err
		}
		hasCommits, err := repo.HasCommits(ctx)
		if err != nil {
			return repoOpenedMsg{}, err
		}
		return repoOpenedMsg{repo: repo, hasCommits: hasCommits}, nil
	}, func(m repoOpenedMsg) tea.Msg { return m })
}

// loadListCmd fetches the first batch of commits and the total reachable
// count, two independent reads run concurrently. severity is fatal on the
// plain startup path (no list, nothing to show) and nonFatal on the
// `<rev>`-jump path, whose detail view can succeed without the list.
func loadListCmd(repo *git.Repo, severity errSeverity) tea.Cmd {
	return runCmd(severity, func(ctx context.Context) (commitsLoadedMsg, error) {
		var commits []git.Commit
		var total int
		err := concurrent.RunConcurrently(
			func() (err error) { commits, err = repo.FetchLogBatch(ctx, 0, batchSize); return },
			func() (err error) { total, err = repo.CommitCount(ctx); return },
		)
		if err != nil {
			return commitsLoadedMsg{}, err
		}
		return commitsLoadedMsg{commits: commits, totalCount: total}, nil
	}, func(m commitsLoadedMsg) tea.Msg { return m })
}

// loadWorktreeSummariesCmd fetches the counts the synthetic leading rows
// show — not the diffs behind them, which are left to the fetch that opens
// one. Its failure is always nonFatal: those rows are supplementary to the
// list/detail view.
func loadWorktreeSummariesCmd(repo *git.Repo) tea.Cmd {
	return runCmd(nonFatal, func(ctx context.Context) ([]git.EntrySummary, error) {
		return repo.FetchWorktreeSummaries(ctx)
	}, func(summaries []git.EntrySummary) tea.Msg {
		return worktreeSummariesLoadedMsg{summaries: summaries}
	})
}

// resolveRevPositionCmd resolves rev to a commit position within HEAD's
// history for cursor placement. Failure to resolve or place it reports
// revPositionMsg{found: false} rather than an errMsg: fetchDetailCmd already
// surfaces an invalid revision, and this only affects where the cursor ends
// up.
func resolveRevPositionCmd(repo *git.Repo, rev string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		hash, err := repo.ResolveRevision(ctx, rev)
		if err != nil {
			return revPositionMsg{found: false}
		}
		pos, ok, err := repo.CommitPosition(ctx, hash)
		if err != nil || !ok {
			return revPositionMsg{found: false}
		}
		return revPositionMsg{pos: pos, found: true}
	}
}

// moreCommitsFailedMsg reports a failed pagination fetch. It bypasses
// errMsg/runCmd so that clearing ListModel.loadingMore is tied to *this*
// fetch's own failure: an unrelated error arriving while a pagination fetch
// is still in flight must not un-latch it, which would let a duplicate fetch
// for the same range append the same commits twice.
type moreCommitsFailedMsg struct{ err error }

// fetchMoreCmd doesn't go through runCmd — see moreCommitsFailedMsg for why
// its failures need to identify themselves separately.
func fetchMoreCmd(repo *git.Repo, req fetchRequest) tea.Cmd {
	return func() tea.Msg {
		commits, err := repo.FetchLogBatch(context.Background(), req.skip, req.limit)
		if err != nil {
			return moreCommitsFailedMsg{err: err}
		}
		return moreCommitsLoadedMsg{commits: commits}
	}
}

// openDetail installs detail as the active view, carrying the shared search
// state into it so an active search keeps working across commits/entries.
// Opening only computes and highlights matches; the cursor stays at the top
// of the view rather than jumping to the first match, which n/N still reach.
func (m Model) openDetail(detail *git.CommitDetail) Model {
	m.detail = m.newDetailModel(detail)
	m.active = viewDetail
	return m
}

// newDetailModel builds a detail view over detail, sized and carrying the
// shared search state — everything a DetailModel needs before it's shown,
// from wherever it's shown.
func (m Model) newDetailModel(detail *git.CommitDetail) DetailModel {
	d := NewDetailModel(detail, m.jl)
	d.SetSize(m.width, m.contentHeight())
	d.SetSearchDisplay(m.searchTerm, m.searchDismissed)
	return d
}

// fetchDetailCmd fetches one commit's metadata and diff. severity is fatal
// from the `<rev>`-jump startup dispatch (the detail view is that path's
// whole payload) and nonFatal from Enter in the list, which stays up if the
// fetch fails. paths is only ever the CLI's own pathspec — see
// StartTarget.Paths.
func fetchDetailCmd(repo *git.Repo, hash string, severity errSeverity, paths ...string) tea.Cmd {
	return runCmd(severity, func(ctx context.Context) (*git.CommitDetail, error) {
		return repo.FetchCommitDetail(ctx, hash, paths...)
	}, func(d *git.CommitDetail) tea.Msg { return commitDetailLoadedMsg{detail: d} })
}

// fetchEntryDetailCmd fetches a single synthetic entry's diff and opens it via
// commitDetailLoadedMsg like any other detail view. severity is fatal from the
// `--staged`/`--diff` startup dispatch, which has no list to fall back to, and
// nonFatal from Enter in the list, which stays up if the fetch fails. paths is
// only ever the CLI's own pathspec — see StartTarget.Paths.
func fetchEntryDetailCmd(repo *git.Repo, kind git.EntryKind, severity errSeverity, paths ...string) tea.Cmd {
	return runCmd(severity, func(ctx context.Context) ([]git.File, error) {
		return repo.FetchWorktreeEntry(ctx, kind, paths...)
	}, func(files []git.File) tea.Msg {
		entry := git.WorktreeEntry{Kind: kind, Files: files, Paths: paths}
		return commitDetailLoadedMsg{detail: git.EntryDetail(entry)}
	})
}

// searchLoadedMsg carries everything a list search needs to place its
// matches: which commits match term (by hash), and the full hash order to
// resolve those hashes to positions in the list.
type searchLoadedMsg struct {
	term    string
	matches map[string]bool
	order   []string
}

// searchFailedMsg reports a failed list search. Like moreCommitsFailedMsg it
// bypasses errMsg so clearing searchingTerm is tied to this search's own
// failure, and an unrelated error can't stop the status bar reporting a
// search that is still running.
type searchFailedMsg struct {
	term string
	err  error
}

// searchCmd runs the queries a list search needs, overlapping the two
// independent git reads in one command rather than racing back as two
// messages the model has to reassemble. The hash order is only queried the
// first time; later searches are handed the cached one (see allHashes).
func searchCmd(repo *git.Repo, term string, order []string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		msg := searchLoadedMsg{term: term, order: order}
		var err error
		if order != nil {
			msg.matches, err = repo.SearchCommitHashes(ctx, term)
		} else {
			err = concurrent.RunConcurrently(
				func() (err error) { msg.matches, err = repo.SearchCommitHashes(ctx, term); return },
				func() (err error) { msg.order, err = repo.AllCommitHashes(ctx); return },
			)
		}
		if err != nil {
			return searchFailedMsg{term: term, err: err}
		}
		return msg
	}
}

// matchPositions maps a search's matched hashes to their positions in order,
// ascending, unioning in the hash-prefix matches (HashesMatching, an
// in-memory filter over that same order).
func matchPositions(order []string, matched map[string]bool, term string) []int {
	byHash := git.HashesMatching(order, term)
	positions := make([]int, 0, len(matched)+len(byHash))
	for i, h := range order {
		if matched[h] || byHash[h] {
			positions = append(positions, i)
		}
	}
	return positions
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Any successful fetch retires a previously-shown non-fatal error, checked
	// once here rather than repeated in each success case below.
	switch msg.(type) {
	case commitsLoadedMsg, moreCommitsLoadedMsg, commitDetailLoadedMsg,
		worktreeSummariesLoadedMsg, searchLoadedMsg:
		m.lastError = nil
	}

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
		if p := m.start.Piped; p != nil {
			// Only reached with a hash: the header-less case never opened a
			// repo (see Init). The list loads behind the detail view exactly
			// as a `gitl <rev>` jump does, so ctrl-o reaches the commit's
			// place in history.
			return m, tea.Batch(
				fetchPipedCommitCmd(msg.repo, p),
				loadListCmd(msg.repo, nonFatal),
				loadWorktreeSummariesCmd(msg.repo),
				resolveRevPositionCmd(msg.repo, p.Hash),
			)
		}
		if !msg.hasCommits {
			m.err = errors.New("this repository has no commits yet")
			return m, nil
		}
		return m, tea.Batch(loadListCmd(msg.repo, fatal), loadWorktreeSummariesCmd(msg.repo))
	case commitsLoadedMsg:
		m.list = NewListModel(msg.commits, m.jl)
		m.list.SetTotalCount(msg.totalCount)
		m.list.SetSize(m.width, m.contentHeight())
		m.list.SetLeadingSummaries(m.summaries)
		// The search state can already be set: the prompt is available as
		// soon as the repo opens, a whole fetch before this message.
		m.list.SetSearchDisplay(m.searchTerm, m.searchDismissed)
		m, matchCmd := m.applyListMatches(m.listMatches)
		m, jumpCmd := m.pumpPendingJump()
		return m, tea.Batch(matchCmd, jumpCmd)
	case moreCommitsLoadedMsg:
		m.list.AppendCommits(msg.commits)
		// A jump requested while this fetch was in flight was refused a fetch
		// of its own (see RequestLoadUpTo) and may still be waiting for a
		// position this batch didn't reach. Offer it again now, or the cursor
		// silently never arrives.
		if target, ok := m.list.pendingTarget(); ok {
			return m, m.loadUpToCmd(target)
		}
		return m, nil
	case moreCommitsFailedMsg:
		m.list.loadingMore = false
		m.lastError = msg.err
		return m, nil
	case worktreeSummariesLoadedMsg:
		m.summaries = msg.summaries
		m.list.SetLeadingSummaries(m.summaries)
		return m, nil
	case revPositionMsg:
		if !msg.found {
			// Not reachable from HEAD, or not resolvable at all: leave the
			// cursor at the top rather than error. The detail view opens from
			// fetchDetailCmd regardless.
			return m, nil
		}
		pos := msg.pos
		m.pendingJump = &pos
		return m.pumpPendingJump()
	case commitDetailLoadedMsg:
		return m.openDetail(msg.detail), nil
	case searchLoadedMsg:
		// Worth caching even from a search that's since been superseded: the
		// order is invariant, and it's the expensive half of the query.
		m.allHashes = msg.order
		if msg.term != m.searchingTerm {
			return m, nil
		}
		m.searchingTerm = ""
		return m.applyListMatches(matchPositions(msg.order, msg.matches, msg.term))
	case searchFailedMsg:
		if msg.term == m.searchingTerm {
			m.searchingTerm = ""
		}
		m.lastError = msg.err
		return m, nil
	case errMsg:
		if msg.severity == fatal {
			m.err = msg.err
		} else {
			m.lastError = msg.err
		}
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// jumpOnResolve values: what the list should do once its match positions
// arrive. Recorded when the search is started, not read off the active view
// when it resolves, since a search started only to light up the list's rows
// must not also drag its cursor around.
const (
	jumpNone      = 0
	jumpNextMatch = 1  // land on the first match at or after the cursor
	jumpPrevMatch = -1 // land on the last match at or before it
)

// startListSearch kicks off the git queries a list search needs, recording
// the term so their results can be recognized as still wanted when they
// arrive, and what to do with them once they do.
func (m Model) startListSearch(term string, jump int) (Model, tea.Cmd) {
	m.searchingTerm = term
	m.jumpOnResolve = jump
	return m, searchCmd(m.repo, term, m.allHashes)
}

// ensureListSearch starts the list's queries if its match positions aren't
// available for the active term yet. Nothing calls this speculatively: the
// list's half of a search is two repo-wide `git log` scans, so it only runs
// once something is about to use it — the list becoming visible with the
// search still showing, or n/N reviving it there.
//
// jump says what to do when the results land (see jumpOnResolve); an
// already-running search has its intent upgraded rather than being started
// twice, so an n pressed while the queries are still out still lands on a
// match.
func (m Model) ensureListSearch(jump int) (Model, tea.Cmd) {
	if m.searchTerm == "" || m.searchDismissed || m.repo == nil || !m.start.opensList() {
		return m, nil
	}
	if m.searchingTerm == m.searchTerm {
		if jump != jumpNone {
			m.jumpOnResolve = jump
		}
		return m, nil
	}
	// setSearchDisplay drops listMatches on every term change and a resolved
	// search always installs a non-nil slice, so nil here means "not searched
	// yet", not "searched, no matches".
	if m.listMatches != nil {
		return m, nil
	}
	return m.startListSearch(m.searchTerm, jump)
}

// applyListMatches installs a resolved search's match positions on the list
// and carries out whatever jump was asked for when it was started (see
// jumpOnResolve), dispatching a targeted fetch when the match it lands on
// isn't loaded yet. The jump isn't consumed until there are commits to land
// it on, since a search can resolve before the first batch of them does.
func (m Model) applyListMatches(matches []int) (Model, tea.Cmd) {
	m.listMatches = matches
	m.list.SetMatches(matches)
	if m.jumpOnResolve == jumpNone || len(m.list.commits) == 0 {
		return m, nil
	}
	jump := m.jumpOnResolve
	m.jumpOnResolve = jumpNone

	var needsLoad bool
	var target int
	if jump == jumpNextMatch {
		needsLoad, target = m.list.jumpToFirstMatch()
	} else {
		needsLoad, target = m.list.jumpToLastMatch()
	}
	if needsLoad {
		return m, m.loadUpToCmd(target)
	}
	return m, nil
}

// loadUpToCmd dispatches the targeted fetch a deferred list jump to target
// needs, or nil when there's nothing to fetch — RequestLoadUpTo refuses when
// the position is already loaded or another fetch is in flight. Shared by
// every navigation that can outrun the loaded range.
func (m Model) loadUpToCmd(target int) tea.Cmd {
	if req, ok := m.list.RequestLoadUpTo(target); ok {
		return fetchMoreCmd(m.repo, req)
	}
	return nil
}

// pumpPendingJump applies a CLI-requested list position (`gitl <rev>`) once
// both of its independent halves are available: the list itself and the
// resolved position. Called from whichever of the two lands last.
func (m Model) pumpPendingJump() (Model, tea.Cmd) {
	if m.pendingJump == nil || len(m.list.commits) == 0 {
		return m, nil
	}
	pos := *m.pendingJump
	m.pendingJump = nil

	if needsLoad := m.list.JumpTo(pos); needsLoad {
		return m, m.loadUpToCmd(pos)
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

	// Handled ahead of view routing, since popping a jumplist entry can
	// require switching the active view.
	if key.Matches(msg, DefaultKeyMap.JumpBack) {
		return m.doJumpBack()
	}
	if key.Matches(msg, DefaultKeyMap.JumpForward) {
		return m.doJumpForward()
	}

	if m.active == viewDetail {
		return m.handleDetailViewKey(msg)
	}
	return m.handleListViewKey(msg)
}

// currentJumpPos captures the active view's current position as a jumpPos.
func (m Model) currentJumpPos() jumpPos {
	if m.active == viewDetail {
		return jumpPos{view: viewDetail, commit: m.detail.detail, cursorLine: m.detail.cursorLine}
	}
	return jumpPos{view: viewList, listCursor: m.list.cursor}
}

// doJumpBack and doJumpForward step through the shared jumplist (ctrl-o/
// ctrl-i), each pushing the position it leaves onto the other stack so they
// exactly undo one another. A no-op when the relevant stack is empty.
func (m Model) doJumpBack() (tea.Model, tea.Cmd) {
	if len(m.jl.back) == 0 {
		return m, nil
	}
	target := m.jl.back[len(m.jl.back)-1]
	m.jl.back = m.jl.back[:len(m.jl.back)-1]
	m.jl.forward = append(m.jl.forward, m.currentJumpPos())
	return m.gotoJumpPos(target)
}

func (m Model) doJumpForward() (tea.Model, tea.Cmd) {
	if len(m.jl.forward) == 0 {
		return m, nil
	}
	target := m.jl.forward[len(m.jl.forward)-1]
	m.jl.forward = m.jl.forward[:len(m.jl.forward)-1]
	m.jl.back = append(m.jl.back, m.currentJumpPos())
	return m.gotoJumpPos(target)
}

// gotoJumpPos applies pos, switching the active view if needed. A detail
// target reuses the already-fetched *git.CommitDetail directly rather than
// re-fetching it, and installs the search the same non-jumping way opening a
// commit does — a jump to the first match would fight the cursorLine this
// restore is trying to land on.
func (m Model) gotoJumpPos(pos jumpPos) (Model, tea.Cmd) {
	if pos.view == viewList {
		m.active = viewList
		m.list.JumpTo(pos.listCursor)
		// Landing here with the search still showing is the other moment the
		// list's own matches are needed — see ensureListSearch.
		return m.ensureListSearch(jumpNone)
	}
	if m.detail.detail != pos.commit {
		m.detail = m.newDetailModel(pos.commit)
	}
	m.detail.setCursor(pos.cursorLine)
	m.active = viewDetail
	return m, nil
}

// handleDetailViewKey routes a key press while the detail view is active:
// Back returns to the list (or quits, see quitOnBack), everything else goes
// to the diff viewport.
func (m Model) handleDetailViewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, DefaultKeyMap.Back) {
		if m.quitOnBack {
			m.quitting = true
			return m, tea.Quit
		}
		m.active = viewList
		return m.ensureListSearch(jumpNone)
	}
	m.detail.Update(msg)
	return m, nil
}

// handleListViewKey routes a key press while the list view is active: Back
// quits (there's nowhere "back" to go from the list), Enter opens the
// selected commit or synthetic entry, everything else goes to the list's own
// cursor/scroll handling.
func (m Model) handleListViewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, DefaultKeyMap.Back) {
		m.quitting = true
		return m, tea.Quit
	}
	if key.Matches(msg, DefaultKeyMap.Enter) {
		// A synthetic row carries only the counts it displays, so opening
		// one fetches its diff on demand exactly as a commit does — with no
		// pathspec, which is scoped to the startup target alone. Either way
		// the transition is jump-worthy: push the list position being left
		// so ctrl-o can return to it, before the detail view exists.
		var cmd tea.Cmd
		if summary, ok := m.list.SelectedSummary(); ok {
			cmd = fetchEntryDetailCmd(m.repo, summary.Kind, nonFatal)
		} else if commit, ok := m.list.Selected(); ok {
			cmd = fetchDetailCmd(m.repo, commit.Hash, nonFatal)
		}
		if cmd == nil {
			return m, nil
		}
		m.jl.push(m.currentJumpPos())
		return m, cmd
	}
	if req, ok := m.list.Update(msg); ok {
		return m, fetchMoreCmd(m.repo, req)
	}
	return m, nil
}

// handleSearchPromptKey routes a key press while the search prompt is open.
// Esc cancels — checked directly, not via DefaultKeyMap.Back, which is also
// bound to "q" and a query must be able to contain a literal q — and so does
// Backspace on an already-empty query. Everything else is forwarded to the
// text input so the query can contain any character.
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

// cancelSearchPrompt closes the search prompt without submitting.
func (m Model) cancelSearchPrompt() (tea.Model, tea.Cmd) {
	m.searchPromptOpen = false
	m.searchInput.Blur()
	return m, nil
}

// submitSearch closes the prompt on Enter and searches for the submitted
// term. Both views answer to it, but only the view in front of the user is
// searched now: the detail view resolves synchronously as setSearchDisplay
// installs the term, while the list's repo-wide queries wait until something
// needs them (ensureListSearch). Only the active view jumps to its first
// match; the other never moves its cursor behind the user's back. An empty
// term clears the search entirely, unlike dismissSearch, which only hides it.
func (m Model) submitSearch() (tea.Model, tea.Cmd) {
	term := m.searchInput.Value()
	m.searchPromptOpen = false
	m.searchInput.Blur()

	if term == "" {
		m.setSearchDisplay("", false)
		return m, nil
	}

	m.setSearchDisplay(term, false)
	if m.active == viewDetail {
		m.detail.jumpToFirstMatch()
		return m, nil
	}
	return m.startListSearch(term, jumpNextMatch)
}

// setSearchDisplay is the sole writer of the canonical search term/dismissed
// state, pushing both down to each view's display mirror so highlighting and
// the status bar stay consistent whichever view is active. A changed term
// retires the previous term's list matches: a match set must never outlive
// the term it describes.
func (m *Model) setSearchDisplay(term string, dismissed bool) {
	if term != m.searchTerm {
		m.listMatches = nil
		// Whatever queries are in flight are for the term being replaced:
		// forgetting which term they belong to discards their results when
		// they land (see searchLoadedMsg) and stops the status bar from
		// reporting a search the user has moved on from.
		m.searchingTerm = ""
	}
	m.searchTerm = term
	m.searchDismissed = dismissed
	m.list.SetSearchDisplay(term, dismissed)
	m.detail.SetSearchDisplay(term, dismissed)
}

// hasActiveSearch reports whether a search is active, gating n/N and the
// Esc/q-dismiss interception in handleKey.
func (m Model) hasActiveSearch() bool {
	return m.searchTerm != ""
}

// dismissSearch hides the active search's highlighting without discarding its
// term/matches (vim :noh-style): the first Esc/q after a search goes active is
// absorbed here instead of falling through to Back. n/N revives it.
func (m Model) dismissSearch() (Model, tea.Cmd) {
	m.setSearchDisplay(m.searchTerm, true)
	return m, nil
}

// advanceMatch cycles the active view's search to the next (delta>0) or
// previous match, reviving a dismissed search first. In the list view this
// may dispatch a targeted fetch if the newly-selected match isn't loaded
// yet; the detail view is synchronous and never needs one.
func (m Model) advanceMatch(delta int) (Model, tea.Cmd) {
	// Only reviving a dismissed search needs setSearchDisplay's own
	// render/reflow: NextMatch/PrevMatch below re-render anyway, so calling
	// it unconditionally would pay for that twice on every n/N press.
	if m.searchDismissed {
		m.setSearchDisplay(m.searchTerm, false)
	}

	if m.active == viewDetail {
		if delta > 0 {
			m.detail.NextMatch()
		} else {
			m.detail.PrevMatch()
		}
		return m, nil
	}

	// n/N is one of the two moments the list's own matches are actually
	// needed (see ensureListSearch); until they land there is nothing to
	// step through, so the step itself becomes the jump they resolve into.
	if m.listMatches == nil {
		return m.ensureListSearch(delta)
	}

	var needsLoad bool
	var target int
	if delta > 0 {
		needsLoad, target = m.list.NextMatch()
	} else {
		needsLoad, target = m.list.PrevMatch()
	}
	if needsLoad {
		return m, m.loadUpToCmd(target)
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
// status bar's single row is reserved.
func (m Model) contentHeight() int {
	return max(m.height-1, 0)
}

// renderStatusBar renders the bottom status bar, in priority order: the live
// query while the search prompt is open, a loading indicator while the list's
// queries are in flight, the active view's search term/position, or the most
// recent non-fatal fetch failure.
func (m Model) renderStatusBar() string {
	style := statusBarStyle
	var content string
	switch {
	case m.searchPromptOpen:
		content = m.searchInput.View()
	case m.searchingTerm != "":
		content = fmt.Sprintf("/%s  searching…", m.searchingTerm)
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
	case m.lastError != nil:
		style = statusBarErrStyle
		content = statusBarErrorText(m.lastError, m.width)
	}
	return style.Width(m.width).Render(content)
}

// statusBarErrorText renders err's message for the one-row status bar:
// flattened to a single line (runGit folds often multi-line stderr into its
// error text, which would otherwise push the bar to several rows and shove
// the layout off screen) and truncated to fit width (statusBarStyle's
// Padding(0, 1) accounts for the -2).
func statusBarErrorText(err error, width int) string {
	text := "error: " + strings.Join(strings.Fields(err.Error()), " ")
	if width > 2 {
		text = truncate(text, width-2)
	}
	return text
}
