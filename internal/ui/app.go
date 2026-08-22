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
// (the plain list), the staged-changes entry, or the working-tree (unstaged)
// entry. Built by main.go from the command line.
type StartTarget struct {
	Rev    string
	Staged bool
	Diff   bool
}

// opensDetail reports whether gitl was asked to open straight into a detail
// view, skipping the list as the initial screen.
func (t StartTarget) opensDetail() bool { return t.Staged || t.Diff || t.Rev != "" }

// opensList reports whether the commit list gets loaded at all. Deliberately
// not the negation of opensDetail: a `gitl <rev>` jump opens a detail view
// *and* loads the list behind it (so ctrl-o can reach the commit's place in
// history), while `--staged`/`--diff` have no list at any point.
func (t StartTarget) opensList() bool { return !t.Staged && !t.Diff }

// Model is the root Bubble Tea model for the application.
type Model struct {
	width, height int
	quitting      bool

	start StartTarget
	// quitOnBack makes Back/q from the detail view quit outright instead of
	// returning to the list — set in NewModel for `gitl --staged`/`--diff`/
	// `<rev>` startup jumps, which open straight into a detail view with no
	// list to return to. Nothing else ever writes it.
	quitOnBack bool

	repo   *git.Repo
	active view
	list   ListModel
	detail DetailModel
	// err is a fatal startup failure: View() replaces the whole screen
	// with it, and it's never cleared.
	err error
	// lastError is the latest non-fatal fetch failure (pagination, worktree
	// entries, search, or a list-opened commit detail), shown in the status
	// bar until the next successful fetch of any kind clears it.
	lastError error
	entries   []git.WorktreeEntry // canonical copy, mirrored into list.leading as it and the list each become available

	// pendingJump is a CLI-requested list position (`gitl <rev>`) not yet
	// applied, either because the list doesn't exist yet or because
	// resolveRevPositionCmd hasn't returned yet — the same
	// "independent concurrent halves, apply once both are in" shape as
	// pendingSearch, resolved by pumpPendingJump.
	pendingJump *int

	searchPromptOpen bool
	searchInput      textinput.Model
	pendingSearch    *pendingSearch
	// allHashes is the full reachable commit-hash history in log order,
	// fetched once on the first search and reused for every later one —
	// gitl has no refresh mechanism (see CLAUDE.md's "Known limitations"),
	// so this can't go stale during a run, and refetching it via a git
	// subprocess on every search submission would be wasted I/O on a large
	// repo. haveAllHashes distinguishes "not fetched yet" from "fetched an
	// empty/unborn history".
	allHashes     []string
	haveAllHashes bool

	// searchTerm and searchDismissed are the single, canonical search state
	// shared by both views — see setSearchDisplay, the only place that
	// writes them. Neither ListModel nor DetailModel owns its own copy;
	// each keeps a display mirror pushed down from here (via
	// SetSearchDisplay) purely so their own internal render calls don't
	// need term/dismissed threaded through as parameters everywhere.
	searchTerm      string
	searchDismissed bool
	// listMatches is the canonical copy of the list's match positions for
	// searchTerm, kept here rather than only inside ListModel because the
	// list model is rebuilt from scratch when commits arrive — a search
	// submitted before that (the prompt only needs the repo, not the list)
	// would otherwise be silently dropped by the replacement.
	listMatches []int

	// jl is the single, cross-view jumplist backing ctrl-o/ctrl-i (vim's
	// jumpBack/jumpForward). Shared by pointer with ListModel and
	// DetailModel — see jumplist's doc comment for why a jump made from
	// inside either view, or the view transition of opening/leaving a
	// commit, is recorded onto one shared history instead of two
	// independent per-view ones.
	jl *jumplist
}

// jumpPos identifies one position recorded in the jumplist: either a list
// cursor position, or a line within a specific commit/entry's detail view
// (commit, not cursorLine alone, since JumpForward may need to reconstruct a
// DetailModel for a commit that isn't the currently active one — the exact
// *git.CommitDetail pointer it was fetched as, reused directly with no
// re-fetch).
type jumpPos struct {
	view       view
	listCursor int
	commit     *git.CommitDetail
	cursorLine int
}

// jumplist is the shared, cross-view jumplist backing ctrl-o/ctrl-i, matching
// vim's own semantics where switching buffers (here: list <-> detail) is
// itself jump-recordable, not just movement within one — see CLAUDE.md's
// jumplist note. Owned by the root Model and referenced by pointer from both
// ListModel and DetailModel (constructed with it, see NewListModel/
// NewDetailModel) so a jump made from inside either view (g/G, [/], a
// search-match step) pushes onto this one shared stack; JumpBack/JumpForward
// themselves are handled centrally by the root Model (see doJumpBack/
// doJumpForward) since popping an entry can require switching the active
// view, which neither child model can do on its own.
type jumplist struct {
	back, forward []jumpPos
}

// push records pos as a jumplist entry reachable via JumpBack, discarding any
// forward history from an earlier JumpBack — a fresh jump invalidates
// whatever "redo" existed before it, matching vim's own jumplist semantics.
// Capped at jumplistCap entries, matching vim's own default jumplist size.
func (j *jumplist) push(pos jumpPos) {
	j.forward = nil
	j.back = append(j.back, pos)
	if len(j.back) > jumplistCap {
		j.back = j.back[len(j.back)-jumplistCap:]
	}
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
	// Seeded with an entry at the list's top (position 0), so ctrl-o can
	// always get back there even if the cursor got elsewhere via plain
	// motion first, never through an actual jump — same reasoning
	// ListModel/DetailModel's own seed used to carry individually. Skipped
	// when the run has no list at all (see opensList), where ctrl-o would
	// otherwise strand the user on a permanently empty list view.
	jl := &jumplist{}
	if target.opensList() {
		jl.back = []jumpPos{{view: viewList, listCursor: 0}}
	}
	return Model{
		start:      target,
		quitOnBack: target.opensDetail(),
		jl:         jl,
		// The list is built empty here rather than left a zero value until
		// commitsLoadedMsg replaces it: keys reach it from the very first
		// frame (viewList is the initial view, and a jumplist entry can send
		// the user there at any time), and a zero ListModel has no jumplist
		// to push onto — a nil-pointer panic on the first jump-worthy motion.
		list: NewListModel(nil, jl),
	}
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

// revPositionMsg carries the resolved list position of a CLI-requested
// revision (`gitl <rev>`), for positioning the list's cursor behind the
// detail view that opens immediately from fetchDetailCmd. found is false
// when the revision couldn't be resolved at all, or resolved but isn't
// reachable from HEAD (e.g. a commit only on another branch) — in either
// case there's no sensible list position, so the cursor is simply left
// where it is (see Update's handling of it).
type revPositionMsg struct {
	pos   int
	found bool
}

// errSeverity says how much of the UI a failed fetch invalidates.
type errSeverity int

const (
	// nonFatal: whatever is already on screen is still valid and stays;
	// the failure is reported in the status bar until the next successful
	// fetch retires it.
	nonFatal errSeverity = iota
	// fatal: there is nothing left to show, so the error replaces the
	// whole screen. Reserved for startup failures.
	fatal
)

type errMsg struct {
	err      error
	severity errSeverity
}

// runCmd wraps fetch as a tea.Cmd: on success, toMsg(v) becomes the
// resulting message; on failure, errMsg{err, severity} does — the shape
// behind nearly every fetch below. fetchMoreCmd and resolveRevPositionCmd
// are exceptions — see their own doc comments.
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
// count. The two are independent reads on an already-open repo, so they
// run concurrently via concurrent.RunConcurrently rather than one after
// another. severity is fatal on the plain startup path (no list, nothing
// to show) and nonFatal on the `<rev>`-jump path (the detail view is that
// path's primary payload and can succeed independently of the list).
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

// loadWorktreeEntriesCmd's failure is always nonFatal: the rows it
// produces are always supplementary to the list/detail view, regardless of
// whether it happens to arrive before or after the list itself.
func loadWorktreeEntriesCmd(repo *git.Repo) tea.Cmd {
	return runCmd(nonFatal, func(ctx context.Context) ([]git.WorktreeEntry, error) {
		return repo.FetchWorktreeEntries(ctx)
	}, func(entries []git.WorktreeEntry) tea.Msg {
		return worktreeEntriesLoadedMsg{entries: entries}
	})
}

// resolveRevPositionCmd resolves rev to a commit position within HEAD's
// history for cursor placement. Any failure to resolve or place it (an
// invalid revision, or one unreachable from HEAD) is reported as
// revPositionMsg{found: false} rather than an errMsg: fetchDetailCmd
// already surfaces an invalid revision as the user-visible error, so this
// doesn't need to duplicate it — it only ever affects where the cursor
// ends up.
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
// errMsg/runCmd on purpose: clearing ListModel.loadingMore has to be tied
// to *this* fetch's own failure, not to any errMsg — an unrelated error
// (e.g. a failed search) arriving while a pagination fetch is genuinely
// still in flight must not un-latch it, since that would let a duplicate
// fetch for the same range through and AppendCommits would append the same
// commits twice, corrupting every index after that point.
type moreCommitsFailedMsg struct{ err error }

// fetchMoreCmd deliberately doesn't go through runCmd — see
// moreCommitsFailedMsg for why its failures need to identify themselves
// separately.
func fetchMoreCmd(repo *git.Repo, req fetchRequest) tea.Cmd {
	return func() tea.Msg {
		commits, err := repo.FetchLogBatch(context.Background(), req.skip, req.limit)
		if err != nil {
			return moreCommitsFailedMsg{err: err}
		}
		return moreCommitsLoadedMsg{commits: commits}
	}
}

// openDetail installs detail as the active view, mirroring the shared
// search state into it so an already-active search keeps working across
// commits/entries (a freshly constructed DetailModel otherwise defaults to
// an empty term and searchDismissed=false, which would drop the
// highlighting, or make an already-dismissed search suddenly reappear, just
// because a new commit/entry was opened). Unlike a "/" search submitted
// while already viewing this commit (which jumps straight to the first
// match, see submitSearch), opening a commit/entry only computes and
// highlights matches — the cursor stays at the natural top of the view,
// since jumping straight past the commit metadata into the diff/message
// would be a surprising way for a view to open. n/N reach the first match
// from here same as any other.
func (m Model) openDetail(detail *git.CommitDetail) Model {
	m.detail = NewDetailModel(detail, m.jl)
	m.detail.SetSize(m.width, m.contentHeight())
	m.detail.SetSearchDisplay(m.searchTerm, m.searchDismissed)
	m.active = viewDetail
	return m
}

// fetchDetailCmd's severity depends on the caller: fatal from the `<rev>`-
// jump startup dispatch (the detail view is that path's whole payload),
// nonFatal from Enter on a real commit in the list (which stays up if this
// one fetch fails).
func fetchDetailCmd(repo *git.Repo, hash string, severity errSeverity) tea.Cmd {
	return runCmd(severity, func(ctx context.Context) (*git.CommitDetail, error) {
		return repo.FetchCommitDetail(ctx, hash)
	}, func(d *git.CommitDetail) tea.Msg { return commitDetailLoadedMsg{detail: d} })
}

// fetchEntryDetailCmd fetches a single synthetic entry's diff directly (for
// a CLI `--staged`/`--diff` startup jump, which has no list to select the
// row from) and opens it the same way any other detail view opens, via
// commitDetailLoadedMsg. severity is hardcoded fatal since that startup
// path has no list to fall back to.
func fetchEntryDetailCmd(repo *git.Repo, kind git.EntryKind) tea.Cmd {
	return runCmd(fatal, func(ctx context.Context) ([]git.File, error) {
		return repo.FetchWorktreeEntry(ctx, kind)
	}, func(files []git.File) tea.Msg {
		return commitDetailLoadedMsg{detail: git.EntryDetail(git.WorktreeEntry{Kind: kind, Files: files})}
	})
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
	return runCmd(nonFatal, func(ctx context.Context) (map[string]bool, error) {
		return repo.SearchCommitHashes(ctx, term)
	}, func(hashes map[string]bool) tea.Msg {
		return searchHashesLoadedMsg{term: term, hashes: hashes}
	})
}

func allHashesCmd(repo *git.Repo, term string) tea.Cmd {
	return runCmd(nonFatal, func(ctx context.Context) ([]string, error) {
		return repo.AllCommitHashes(ctx)
	}, func(hashes []string) tea.Msg {
		return allHashesLoadedMsg{term: term, hashes: hashes}
	})
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Any successful fetch retires a previously-shown non-fatal error: git
	// is answering again, whatever specifically failed before. Checked
	// once here rather than repeated in each success case below, so a
	// future new success case can't forget it.
	switch msg.(type) {
	case commitsLoadedMsg, moreCommitsLoadedMsg, commitDetailLoadedMsg,
		worktreeEntriesLoadedMsg, searchHashesLoadedMsg, allHashesLoadedMsg:
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
		if m.start.Staged {
			// A commit-less repo is not an error here: there's no HEAD to
			// diff the index against yet, but FetchWorktreeEntry already
			// treats that as the empty tree, same as `git diff --cached`
			// itself does.
			return m, fetchEntryDetailCmd(msg.repo, git.EntryStaged)
		}
		if m.start.Diff {
			return m, fetchEntryDetailCmd(msg.repo, git.EntryWorktree)
		}
		if m.start.Rev != "" {
			// fetchDetailCmd passes the raw revision straight to `git log -1`
			// (which already accepts any git-resolvable revision), so the
			// detail view paints as soon as that returns, concurrently with
			// the list loading and positioning itself behind it via
			// resolveRevPositionCmd/pumpPendingJump. Severity split: see
			// fetchDetailCmd's doc comment.
			return m, tea.Batch(
				fetchDetailCmd(msg.repo, m.start.Rev, fatal),
				loadListCmd(msg.repo, nonFatal),
				loadWorktreeEntriesCmd(msg.repo),
				resolveRevPositionCmd(msg.repo, m.start.Rev),
			)
		}
		if !msg.hasCommits {
			m.err = errors.New("this repository has no commits yet")
			return m, nil
		}
		return m, tea.Batch(loadListCmd(msg.repo, fatal), loadWorktreeEntriesCmd(msg.repo))
	case commitsLoadedMsg:
		m.list = NewListModel(msg.commits, m.jl)
		m.list.SetTotalCount(msg.totalCount)
		m.list.SetSize(m.width, m.contentHeight())
		m.list.SetLeadingEntries(m.entries)
		// The search state can already be set: the prompt is available as
		// soon as the repo opens, which is a whole fetch before this message.
		m.list.SetSearchDisplay(m.searchTerm, m.searchDismissed)
		m, searchCmd := m.applyListMatches(m.listMatches)
		m, jumpCmd := m.pumpPendingJump()
		return m, tea.Batch(searchCmd, jumpCmd)
	case moreCommitsLoadedMsg:
		m.list.AppendCommits(msg.commits)
		return m, nil
	case moreCommitsFailedMsg:
		m.list.loadingMore = false
		m.lastError = msg.err
		return m, nil
	case worktreeEntriesLoadedMsg:
		m.entries = msg.entries
		m.list.SetLeadingEntries(m.entries)
		return m, nil
	case revPositionMsg:
		if !msg.found {
			// Not reachable from HEAD (or couldn't be resolved at all) — the
			// documented v1 fallback is to leave the cursor at the top
			// rather than error; the detail view itself already opened (or
			// will) from fetchDetailCmd regardless.
			return m, nil
		}
		pos := msg.pos
		m.pendingJump = &pos
		return m.pumpPendingJump()
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
		m.allHashes = msg.hashes
		m.haveAllHashes = true
		if m.pendingSearch != nil && m.pendingSearch.term == msg.term {
			m.pendingSearch.order = msg.hashes
			m.pendingSearch.haveOrder = true
			return m.pumpPendingSearch()
		}
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

// startListSearch kicks off the git queries a list search needs (which
// commits match, and the full hash order to place them). The hash order is
// invariant for the process's lifetime, so it's only fetched on the first
// search — see allHashes's doc comment — and reused directly here on every
// later one instead of being requeried concurrently with the match query.
func (m Model) startListSearch(term string) (Model, tea.Cmd) {
	m.pendingSearch = &pendingSearch{term: term}
	if m.haveAllHashes {
		m.pendingSearch.order = m.allHashes
		m.pendingSearch.haveOrder = true
		return m, searchHashesCmd(m.repo, term)
	}
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

	hashMatches := git.HashesMatching(p.order, p.term)
	matches := make([]int, 0, len(p.matchSet)+len(hashMatches))
	for i, h := range p.order {
		if p.matchSet[h] || hashMatches[h] {
			matches = append(matches, i)
		}
	}

	return m.applyListMatches(matches)
}

// applyListMatches installs a resolved search's match positions on the list,
// and — only when the list is the view the user is actually looking at —
// jumps to the first match, dispatching a targeted fetch when that match
// isn't loaded yet. A search submitted from the detail view still highlights
// the list, it just doesn't move its cursor (see submitSearch); n/N reach
// the matches from there the same as from anywhere else. matches is also
// kept here as the canonical copy, so a list model built later (see
// commitsLoadedMsg) can be handed the same set rather than losing the search.
func (m Model) applyListMatches(matches []int) (Model, tea.Cmd) {
	m.listMatches = matches
	m.list.SetMatches(matches)
	if m.active != viewList {
		return m, nil
	}
	if needsLoad, target := m.list.jumpToFirstMatch(); needsLoad {
		if req, ok := m.list.RequestLoadUpTo(target); ok {
			return m, fetchMoreCmd(m.repo, req)
		}
	}
	return m, nil
}

// pumpPendingJump applies a CLI-requested list position (`gitl <rev>`)
// once both of its independent halves are available: the list itself
// (commitsLoadedMsg constructs a fresh ListModel that would otherwise
// discard anything applied before it existed) and the resolved position
// (revPositionMsg, a concurrent fetch that can land in either order) — see
// pendingJump's doc comment.
func (m Model) pumpPendingJump() (Model, tea.Cmd) {
	if m.pendingJump == nil || len(m.list.commits) == 0 {
		return m, nil
	}
	pos := *m.pendingJump
	m.pendingJump = nil

	if needsLoad := m.list.JumpTo(pos); needsLoad {
		if req, ok := m.list.RequestLoadUpTo(pos); ok {
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

	// Handled centrally, ahead of view routing, since popping a jumplist
	// entry can require switching the active view — neither ListModel nor
	// DetailModel can do that on its own (see jumplist's doc comment).
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

// currentJumpPos captures the active view's current position as a jumpPos,
// for recording onto the opposite stack when doJumpBack/doJumpForward moves
// away from it.
func (m Model) currentJumpPos() jumpPos {
	if m.active == viewDetail {
		return jumpPos{view: viewDetail, commit: m.detail.detail, cursorLine: m.detail.cursorLine}
	}
	return jumpPos{view: viewList, listCursor: m.list.cursor}
}

// doJumpBack and doJumpForward step through the shared jumplist (see
// jumplist's doc comment), vim's ctrl-o/ctrl-i: browser-back/forward-style,
// each pushing the position it leaves onto the other stack so they exactly
// undo one another. A no-op (nil cmd, unchanged model) when the relevant
// stack is empty.
func (m Model) doJumpBack() (tea.Model, tea.Cmd) {
	if len(m.jl.back) == 0 {
		return m, nil
	}
	target := m.jl.back[len(m.jl.back)-1]
	m.jl.back = m.jl.back[:len(m.jl.back)-1]
	m.jl.forward = append(m.jl.forward, m.currentJumpPos())
	return m.gotoJumpPos(target), nil
}

func (m Model) doJumpForward() (tea.Model, tea.Cmd) {
	if len(m.jl.forward) == 0 {
		return m, nil
	}
	target := m.jl.forward[len(m.jl.forward)-1]
	m.jl.forward = m.jl.forward[:len(m.jl.forward)-1]
	m.jl.back = append(m.jl.back, m.currentJumpPos())
	return m.gotoJumpPos(target), nil
}

// gotoJumpPos applies target, switching the active view if needed. A list
// target always replays through ListModel.JumpTo, which already handles a
// not-yet-loaded position by deferring (see pumpPendingJump for the CLI
// startup path that also uses it) — every jumplist entry was, by
// construction, the cursor's actual position at some past point, so it was
// already loaded then and can only have grown available since. A detail
// target reuses the already-fetched *git.CommitDetail directly (no re-fetch)
// when it isn't the commit already active, computing search matches the
// same non-jumping way openDetail does before landing the cursor exactly on
// cursorLine — SetSearchDisplay computes matches without moving the cursor
// precisely so a restore like this one lands where it means to.
func (m Model) gotoJumpPos(pos jumpPos) Model {
	if pos.view == viewList {
		m.active = viewList
		m.list.JumpTo(pos.listCursor)
		return m
	}
	if m.detail.detail != pos.commit {
		m.detail = NewDetailModel(pos.commit, m.jl)
		m.detail.SetSize(m.width, m.contentHeight())
		m.detail.SetSearchDisplay(m.searchTerm, m.searchDismissed)
	}
	m.detail.setCursor(pos.cursorLine)
	m.active = viewDetail
	return m
}

// handleDetailViewKey routes a key press while the detail view is active:
// Back returns to the list (or quits outright, for a `gitl --staged`,
// `gitl --diff`, or `gitl <rev>` startup jump — see quitOnBack), everything
// else goes to the diff viewport.
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
		// ever loaded on demand. Either way, opening a commit/entry is
		// itself a jump-worthy transition (see jumplist's doc comment): push
		// the list position being left so ctrl-o can return to it, even
		// before the detail view exists yet on the fetchDetailCmd path.
		if entry, ok := m.list.SelectedEntry(); ok {
			m.jl.push(jumpPos{view: viewList, listCursor: m.list.cursor})
			return m.openDetail(git.EntryDetail(entry)), nil
		}
		if commit, ok := m.list.Selected(); ok {
			m.jl.push(jumpPos{view: viewList, listCursor: m.list.cursor})
			return m, fetchDetailCmd(m.repo, commit.Hash, nonFatal)
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

// submitSearch closes the prompt on Enter and searches for the submitted
// term. There is one search term, and both views match against it: the
// detail view synchronously via setSearchDisplay above, the list via the git
// queries startListSearch dispatches. Only the *active* view jumps to its
// first match, since moving the other view's cursor behind the user's back
// would be the same surprise openDetail already avoids when a commit is
// opened with a search running. An empty term clears the search entirely
// instead (a vim-:noh-like escape hatch) — unlike dismissSearch, which only
// hides it.
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
	}
	if !m.start.opensList() {
		return m, nil
	}
	return m.startListSearch(term)
}

// setSearchDisplay is the sole writer of the canonical search term/
// dismissed state, pushing both down to each view's own display mirror so
// highlighting and the status bar stay consistent no matter which view is
// active when the change happens or which view later becomes active. A
// changed term retires the previous term's list matches here for the same
// reason each view drops its own: a match set must never outlive the term it
// describes.
func (m *Model) setSearchDisplay(term string, dismissed bool) {
	if term != m.searchTerm {
		m.listMatches = nil
	}
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
	// Only reviving a dismissed search needs setSearchDisplay's own
	// render/reflow: NextMatch/PrevMatch below always re-render themselves
	// to show the newly active match, so calling it unconditionally would
	// pay for a full re-render/reflow twice on every plain n/N press.
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
	return max(m.height-1, 0)
}

// renderStatusBar renders the bottom status bar: the live query while the
// search prompt is open, a loading indicator while the list's
// position-finding queries are in flight, the active view's search
// term/position, or — lowest priority, since any of the above is more
// specific and more current — the most recent non-fatal fetch failure.
func (m Model) renderStatusBar() string {
	style := statusBarStyle
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
	case m.lastError != nil:
		style = statusBarErrStyle
		content = statusBarErrorText(m.lastError, m.width)
	}
	return style.Width(m.width).Render(content)
}

// statusBarErrorText renders err's message for the one-row status bar:
// flattened to a single line (runGit folds raw, often multi-line stderr
// into its error text, and an unflattened message would push the bar to
// several rows and shove the layout off screen) and truncated to fit
// width (statusBarStyle's own Padding(0, 1) accounts for the -2).
func statusBarErrorText(err error, width int) string {
	text := "error: " + strings.Join(strings.Fields(err.Error()), " ")
	if width > 2 {
		text = truncate(text, width-2)
	}
	return text
}
