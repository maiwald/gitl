package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
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

// StartTarget describes what gitl was asked to open on startup: the plain
// commit list, or a patch git piped in while running gitl as its pager. Built
// by main.go.
type StartTarget struct {
	// Piped is a patch read from stdin — git running gitl as its pager —
	// rather than from one of gitl's own git invocations. Its diff is
	// already parsed; what may still be needed is the commit header behind
	// it, when Piped.Hash names one. Nil for a plain `gitl`, which opens the
	// commit list.
	Piped *git.PipedPatch
}

// opensList reports whether this run has a commit list at all. A piped patch
// is a single screen, the way a pager is: it opens the detail view, q/esc
// quit, and no list is loaded behind it for anything to reach.
func (t StartTarget) opensList() bool { return t.Piped == nil }

// Model is the root Bubble Tea model for the application.
type Model struct {
	width, height int
	quitting      bool

	start StartTarget

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

	searchPromptOpen bool
	searchInput      textinput.Model
	// searchHistory is every term submitted this run, oldest first and
	// without duplicates (resubmitting moves a term to the end), for the
	// prompt's ↑/↓. historyPos indexes it while the prompt is open, with
	// len(searchHistory) meaning the query being typed, which historyDraft
	// keeps while ↑ has replaced it with an older term.
	searchHistory []string
	historyPos    int
	historyDraft  string
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

	// out is where an out-of-band terminal write goes — currently only the
	// clipboard's OSC 52 sequence. os.Stdout in both run modes: the normal
	// path never overrides the program's output, and the pager path has
	// already established stdout is a terminal (its /dev/tty handle is open
	// for reading only). Nil falls back to os.Stdout, so a Model built
	// without going through NewModel still copies.
	out io.Writer

	// copied is the line count of the most recent copy, reported in the
	// status bar until the next key press.
	copied int
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

// jumplistCap bounds the jumplist so a long session's can't grow
// unboundedly — matching vim's own default jumplist size.
const jumplistCap = 100

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
	// the user on an empty list view.
	jl := &jumplist{}
	if target.opensList() {
		jl.back = []jumpPos{{view: viewList, listCursor: 0}}
	}
	return Model{
		start: target,
		jl:    jl,
		out:   os.Stdout,
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
//
// A patch carrying no files at all never reaches here: runAsPager hands that
// input to a real pager instead of opening gitl on it.
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
// message, on failure errMsg{err, severity} does. fetchMoreCmd reports its
// failures its own way instead.
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
		repo, hasCommits, err := git.OpenRepoWithHead(ctx, ".")
		if err != nil {
			return repoOpenedMsg{}, err
		}
		return repoOpenedMsg{repo: repo, hasCommits: hasCommits}, nil
	}, func(m repoOpenedMsg) tea.Msg { return m })
}

// loadListCmd fetches the first batch of commits and the total reachable
// count, two independent reads run concurrently. Its failure is fatal: the
// list is the whole screen.
func loadListCmd(repo *git.Repo) tea.Cmd {
	return runCmd(fatal, func(ctx context.Context) (commitsLoadedMsg, error) {
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
	d.installSearch(m.searchTerm, m.searchDismissed)
	d.SetSize(m.width, m.contentHeight())
	return d
}

// fetchDetailCmd fetches one commit's metadata and diff, for Enter in the
// list. Its failure is nonFatal: the list stays up and reports it in the
// status bar.
func fetchDetailCmd(repo *git.Repo, hash string) tea.Cmd {
	return runCmd(nonFatal, func(ctx context.Context) (*git.CommitDetail, error) {
		return repo.FetchCommitDetail(ctx, hash)
	}, func(d *git.CommitDetail) tea.Msg { return commitDetailLoadedMsg{detail: d} })
}

// fetchEntryDetailCmd fetches a single synthetic entry's diff and opens it via
// commitDetailLoadedMsg like any other detail view. Reached only from Enter in
// the list, so its failure is nonFatal for the same reason fetchDetailCmd's is.
func fetchEntryDetailCmd(repo *git.Repo, kind git.EntryKind) tea.Cmd {
	return runCmd(nonFatal, func(ctx context.Context) ([]git.File, error) {
		return repo.FetchWorktreeEntry(ctx, kind)
	}, func(files []git.File) tea.Msg {
		return commitDetailLoadedMsg{detail: &git.CommitDetail{Kind: kind, Files: files}}
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
			// repo (see Init). The repo is needed for the header alone.
			return m, fetchPipedCommitCmd(msg.repo, p)
		}
		if !msg.hasCommits {
			m.err = errors.New("this repository has no commits yet")
			return m, nil
		}
		return m, tea.Batch(loadListCmd(msg.repo), loadWorktreeSummariesCmd(msg.repo))
	case commitsLoadedMsg:
		m.list = NewListModel(msg.commits, m.jl)
		m.list.SetTotalCount(msg.totalCount)
		m.list.SetSize(m.width, m.contentHeight())
		m.list.SetLeadingSummaries(m.summaries)
		// The search state can already be set: the prompt is available as
		// soon as the repo opens, a whole fetch before this message.
		m.list.SetSearchDisplay(m.searchTerm, m.searchDismissed)
		return m.applyListMatches(m.listMatches)
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
	case copiedMsg:
		m.copied = msg.lines
		return m, nil
	case tea.KeyMsg:
		// The confirmation stands until the user does anything else.
		m.copied = 0
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
	// A piped run has no list to search, and a headerless one no repo to
	// query — see canSearch.
	if m.repo == nil || !m.start.opensList() {
		return m, nil
	}
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

	if jump == jumpNextMatch {
		return m.loadIfNeeded(m.list.jumpToFirstMatch())
	}
	return m.loadIfNeeded(m.list.jumpToLastMatch())
}

// loadIfNeeded finishes a list jump that reported whether its target still
// has to be fetched (see ListModel's jump methods).
func (m Model) loadIfNeeded(needsLoad bool, target int) (Model, tea.Cmd) {
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

// quit ends the program; the next View renders nothing so the last frame
// doesn't linger in the scrollback.
func (m Model) quit() (tea.Model, tea.Cmd) {
	m.quitting = true
	return m, tea.Quit
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, DefaultKeyMap.Quit) {
		return m.quit()
	}

	if m.searchPromptOpen {
		return m.handleSearchPromptKey(msg)
	}
	if m.canSearch() && key.Matches(msg, DefaultKeyMap.Search) {
		// A search and a selection are mutually exclusive states — see
		// toggleSelection, which retires a search the other way round.
		m.detail.clearSelection()
		m.searchInput = textinput.New()
		m.searchInput.Prompt = "/"
		m.searchInput.Width = m.width - 2
		m.searchPromptOpen = true
		m.historyPos, m.historyDraft = len(m.searchHistory), ""
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
			m.dismissSearch()
			return m, nil
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

// canSearch reports whether opening the search prompt can lead anywhere. The
// list's half of a search is git queries and needs a repo, but the detail
// view resolves its matches in memory — which is what lets a headerless piped
// patch (`git diff`, `git diff --staged`), a run that never opens a repo at
// all, still be searched.
func (m Model) canSearch() bool {
	return m.repo != nil || m.detail.detail != nil
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
func (m Model) doJumpBack() (tea.Model, tea.Cmd)    { return m.jumpAcross(&m.jl.back, &m.jl.forward) }
func (m Model) doJumpForward() (tea.Model, tea.Cmd) { return m.jumpAcross(&m.jl.forward, &m.jl.back) }

func (m Model) jumpAcross(from, to *[]jumpPos) (tea.Model, tea.Cmd) {
	n := len(*from)
	if n == 0 {
		return m, nil
	}
	target := (*from)[n-1]
	*from = (*from)[:n-1]
	*to = append(*to, m.currentJumpPos())
	return m.gotoJumpPos(target)
}

// gotoJumpPos applies pos, switching the active view if needed. A detail
// target reuses the already-fetched *git.CommitDetail directly rather than
// re-fetching it, and installs the search the same non-jumping way opening a
// commit does — a jump to the first match would fight the cursorLine this
// restore is trying to land on.
func (m Model) gotoJumpPos(pos jumpPos) (Model, tea.Cmd) {
	// A jump lands the cursor somewhere the selection was never anchored
	// from, and can leave the detail view entirely, so it ends any selection
	// rather than dragging one along behind it.
	m.detail.clearSelection()
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
// the selection keys first, then Back to return to the list (or quit, when
// the run has no list — see opensList), everything else to the diff viewport.
//
// Selection is handled here rather than in DetailModel.Update because both
// halves of it reach past the detail view: V has to retire a visible search
// (the canonical search state lives on Model), and a copy is the one detail
// view key that produces a tea.Cmd.
func (m Model) handleDetailViewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, DefaultKeyMap.Select) {
		return m.toggleSelection()
	}
	if m.detail.selecting {
		// Back cancels the selection and goes no further: it never falls
		// through to leaving the view, so one press undoes one thing.
		if key.Matches(msg, DefaultKeyMap.Back) {
			m.detail.clearSelection()
			return m, nil
		}
		// Enter's other meaning (jump to this file from a stats row) is
		// unreachable while selecting, which is what a selection is for.
		if key.Matches(msg, DefaultKeyMap.Enter) || key.Matches(msg, DefaultKeyMap.Yank) {
			text, lines := m.detail.SelectedText(), m.detail.selectedLineCount()
			m.detail.clearSelection()
			return m, copyCmd(m.out, text, lines)
		}
	}
	if key.Matches(msg, DefaultKeyMap.Back) {
		if !m.start.opensList() {
			return m.quit()
		}
		m.detail.clearSelection()
		m.active = viewList
		return m.ensureListSearch(jumpNone)
	}
	m.detail.Update(msg)
	return m, nil
}

// toggleSelection starts a linewise selection at the cursor, or ends the one
// in progress (vim's V). Starting one retires a visible search: the two are
// mutually exclusive states, which is what keeps a single esc unambiguous
// about which of them it is cancelling.
func (m Model) toggleSelection() (tea.Model, tea.Cmd) {
	if m.detail.selecting {
		m.detail.clearSelection()
		return m, nil
	}
	if m.hasActiveSearch() && !m.searchDismissed {
		m.dismissSearch()
	}
	m.detail.startSelection()
	return m, nil
}

// handleListViewKey routes a key press while the list view is active: Back
// quits (there's nowhere "back" to go from the list), Enter opens the
// selected commit or synthetic entry, everything else goes to the list's own
// cursor/scroll handling.
func (m Model) handleListViewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, DefaultKeyMap.Back) {
		return m.quit()
	}
	if key.Matches(msg, DefaultKeyMap.Enter) {
		// A synthetic row carries only the counts it displays, so opening
		// one fetches its diff on demand exactly as a commit does. Either
		// way the transition is jump-worthy: push the list position being
		// left so ctrl-o can return to it, before the detail view exists.
		var cmd tea.Cmd
		if summary, ok := m.list.SelectedSummary(); ok {
			cmd = fetchEntryDetailCmd(m.repo, summary.Kind)
		} else if commit, ok := m.list.Selected(); ok {
			cmd = fetchDetailCmd(m.repo, commit.Hash)
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
// Backspace on an already-empty query. The ↑/↓ arrows step through the search
// history, matched by key type for the same reason: DefaultKeyMap.Up/Down also
// bind k/j, which a query must be able to contain. Everything else is
// forwarded to the text input so the query can contain any character.
func (m Model) handleSearchPromptKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyEsc:
		return m.cancelSearchPrompt()
	case msg.Type == tea.KeyUp:
		m.recallHistory(-1)
		return m, nil
	case msg.Type == tea.KeyDown:
		m.recallHistory(1)
		return m, nil
	case msg.Type == tea.KeyBackspace && m.searchInput.Value() == "":
		return m.cancelSearchPrompt()
	case key.Matches(msg, DefaultKeyMap.Enter):
		return m.submitSearch()
	}
	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	return m, cmd
}

// searchHistoryCap bounds searchHistory, dropping the oldest terms past it.
const searchHistoryCap = 100

// recallHistory moves delta entries through the search history (negative is
// older) and puts that entry in the prompt. Moving past the newest entry
// brings back the query that was being typed, saved on the way out of it.
func (m *Model) recallHistory(delta int) {
	pos := clamp(m.historyPos+delta, 0, len(m.searchHistory))
	if pos == m.historyPos {
		return
	}
	if m.historyPos == len(m.searchHistory) {
		m.historyDraft = m.searchInput.Value()
	}
	m.historyPos = pos
	if pos == len(m.searchHistory) {
		m.searchInput.SetValue(m.historyDraft)
	} else {
		m.searchInput.SetValue(m.searchHistory[pos])
	}
	m.searchInput.CursorEnd()
}

// recordSearch adds term to the history as its newest entry. An earlier copy
// is removed first, so ↑ never steps through the same term twice.
func (m *Model) recordSearch(term string) {
	h := slices.DeleteFunc(slices.Clone(m.searchHistory), func(s string) bool { return s == term })
	h = append(h, term)
	if len(h) > searchHistoryCap {
		h = h[len(h)-searchHistoryCap:]
	}
	m.searchHistory = h
}

// cancelSearchPrompt closes the search prompt without submitting.
func (m Model) cancelSearchPrompt() (tea.Model, tea.Cmd) {
	m.closeSearchPrompt()
	return m, nil
}

func (m *Model) closeSearchPrompt() {
	m.searchPromptOpen = false
	m.searchInput.Blur()
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
	m.closeSearchPrompt()

	if term == "" {
		m.setSearchDisplay("", false)
		return m, nil
	}
	m.recordSearch(term)

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
func (m *Model) dismissSearch() {
	m.setSearchDisplay(m.searchTerm, true)
}

// advanceMatch cycles the active view's search to the next (delta>0) or
// previous match, reviving a dismissed search first. In the list view this
// may dispatch a targeted fetch if the newly-selected match isn't loaded
// yet; the detail view is synchronous and never needs one.
func (m Model) advanceMatch(delta int) (Model, tea.Cmd) {
	// Stepping to a match makes the search the visible state, so it ends a
	// selection rather than dragging one to wherever the match is.
	m.detail.clearSelection()

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
		jump := jumpPrevMatch
		if delta > 0 {
			jump = jumpNextMatch
		}
		return m.ensureListSearch(jump)
	}

	if delta > 0 {
		return m.loadIfNeeded(m.list.NextMatch())
	}
	return m.loadIfNeeded(m.list.PrevMatch())
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
// query while the search prompt is open, the selection in progress, a loading
// indicator while the list's queries are in flight, the active view's search
// term/position, or the most recent non-fatal fetch failure.
//
// The selection outranks the search cases, which can't fire under it anyway —
// starting a selection retires a visible search — but the order says which
// state the bar is reporting rather than leaving it to that coincidence.
//
// In the detail view the scroll position is right-aligned next to whichever of
// those is showing. It's added after the switch rather than as one of its
// cases so that no state can hide it, the search prompt included: n/N and
// typing a query are when you most want to see where you are.
func (m Model) renderStatusBar() string {
	style := statusBarStyle
	var content string
	switch {
	case m.searchPromptOpen:
		content = m.searchInput.View()
	case m.active == viewDetail && m.detail.selecting:
		n := m.detail.selectedLineCount()
		content = fmt.Sprintf("-- VISUAL --  %d %s", n, pluralize(n, "line", "lines"))
	case m.copied > 0:
		content = fmt.Sprintf("copied %d %s", m.copied, pluralize(m.copied, "line", "lines"))
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
	if m.active == viewDetail {
		// statusBarStyle's Padding(0, 1) takes two columns.
		content = joinStatus(content, m.detail.ScrollPosition(), m.width-2)
	}
	return style.Width(m.width).Render(content)
}

// joinStatus right-aligns right within width, truncating left to make room
// for it. right is dropped if it can't fit even on its own. left can carry
// ANSI (the search prompt's cursor), so it's measured and cut escape-aware.
//
// The open prompt pads itself with spaces out to the bar's width, so cutting
// left often only removes padding; the ellipsis is kept for when it removes
// text, where it would otherwise read as a query that's been cut short.
func joinStatus(left, right string, width int) string {
	rw := displayWidth(right)
	if right == "" || rw > width {
		return left
	}
	room := width - rw
	if displayWidth(left) > room-1 {
		tail := "…"
		if displayWidth(strings.TrimRight(ansi.Strip(left), " ")) <= room-1 {
			tail = ""
		}
		left = ansi.Truncate(left, max(room-1, 0), tail)
	}
	return left + strings.Repeat(" ", room-displayWidth(left)) + right
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
