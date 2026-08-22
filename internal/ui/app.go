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
//
// Paths is the optional pathspec limiting that startup diff, the way `git
// diff -- <path>...` limits its own. It applies to the startup detail view
// only, never to the commit list behind it or to a commit later opened from
// there: nothing about the list is path-filtered, so a filtered detail view
// reached from it would be answering a question the list never asked.
type StartTarget struct {
	Rev    string
	Staged bool
	Diff   bool
	Paths  []string
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
	// resolveRevPositionCmd hasn't returned yet — two independent halves
	// that can land in either order, joined by pumpPendingJump.
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
	// gitl has no refresh mechanism (see CLAUDE.md's "Known limitations"),
	// so this can't go stale during a run, and refetching it via a git
	// subprocess on every search submission would be wasted I/O on a large
	// repo. Nil means "not fetched yet": a genuinely empty history would be
	// indistinguishable, but a run with no commits either fails at startup
	// or has no list to search (see StartTarget.opensList).
	allHashes []string

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
	// would otherwise be silently dropped by the replacement. Nil while the
	// list's half of the search hasn't run: it's only fetched when something
	// needs it, see ensureListSearch.
	listMatches []int
	// jumpOnResolve is what the list should do with the match positions it's
	// waiting on — see the jumpNone/jumpNextMatch/jumpPrevMatch constants.
	jumpOnResolve int

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
	m.detail = m.newDetailModel(detail)
	m.active = viewDetail
	return m
}

// newDetailModel builds a detail view over detail, sized and carrying the
// shared search state — everything a DetailModel needs before it's shown,
// wherever it's shown from (a commit opened from the list, a CLI start
// target, or a jumplist entry being restored).
func (m Model) newDetailModel(detail *git.CommitDetail) DetailModel {
	d := NewDetailModel(detail, m.jl)
	d.SetSize(m.width, m.contentHeight())
	d.SetSearchDisplay(m.searchTerm, m.searchDismissed)
	return d
}

// fetchDetailCmd's severity depends on the caller: fatal from the `<rev>`-
// jump startup dispatch (the detail view is that path's whole payload),
// nonFatal from Enter on a real commit in the list (which stays up if this
// one fetch fails). paths is only ever the CLI's own pathspec, from that
// same startup dispatch — see StartTarget.Paths for why a commit opened
// from the list is never filtered.
func fetchDetailCmd(repo *git.Repo, hash string, severity errSeverity, paths ...string) tea.Cmd {
	return runCmd(severity, func(ctx context.Context) (*git.CommitDetail, error) {
		return repo.FetchCommitDetail(ctx, hash, paths...)
	}, func(d *git.CommitDetail) tea.Msg { return commitDetailLoadedMsg{detail: d} })
}

// fetchEntryDetailCmd fetches a single synthetic entry's diff directly (for
// a CLI `--staged`/`--diff` startup jump, which has no list to select the
// row from) and opens it the same way any other detail view opens, via
// commitDetailLoadedMsg. severity is hardcoded fatal since that startup
// path has no list to fall back to.
func fetchEntryDetailCmd(repo *git.Repo, kind git.EntryKind, paths []string) tea.Cmd {
	return runCmd(fatal, func(ctx context.Context) ([]git.File, error) {
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
// bypasses errMsg on purpose: clearing searchingTerm has to be tied to this
// search's own failure, so an unrelated error can't leave the status bar
// reporting a search that's still genuinely running — or stop reporting one
// that is.
type searchFailedMsg struct {
	term string
	err  error
}

// searchCmd runs the queries a list search needs. They're two independent
// git reads, so they overlap in one command the same way loadListCmd's do,
// rather than racing back as two messages the model has to reassemble. The
// hash order is only queried the first time — it can't change during a run
// (see allHashes), so later searches are handed the cached one.
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
// ascending. Hash matches (HashesMatching, an in-memory filter over the same
// order) are unioned in here rather than queried separately — see its doc
// comment in internal/git.
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
	// Any successful fetch retires a previously-shown non-fatal error: git
	// is answering again, whatever specifically failed before. Checked
	// once here rather than repeated in each success case below, so a
	// future new success case can't forget it.
	switch msg.(type) {
	case commitsLoadedMsg, moreCommitsLoadedMsg, commitDetailLoadedMsg,
		worktreeEntriesLoadedMsg, searchLoadedMsg:
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
			return m, fetchEntryDetailCmd(msg.repo, git.EntryStaged, m.start.Paths)
		}
		if m.start.Diff {
			return m, fetchEntryDetailCmd(msg.repo, git.EntryWorktree, m.start.Paths)
		}
		if m.start.Rev != "" {
			// fetchDetailCmd passes the raw revision straight to `git log -1`
			// (which already accepts any git-resolvable revision), so the
			// detail view paints as soon as that returns, concurrently with
			// the list loading and positioning itself behind it via
			// resolveRevPositionCmd/pumpPendingJump. Severity split: see
			// fetchDetailCmd's doc comment.
			return m, tea.Batch(
				fetchDetailCmd(msg.repo, m.start.Rev, fatal, m.start.Paths...),
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
		m, matchCmd := m.applyListMatches(m.listMatches)
		m, jumpCmd := m.pumpPendingJump()
		return m, tea.Batch(matchCmd, jumpCmd)
	case moreCommitsLoadedMsg:
		m.list.AppendCommits(msg.commits)
		// A jump requested while this fetch was in flight was refused a fetch
		// of its own (see RequestLoadUpTo) and is still waiting for a
		// position this batch may not have reached. Now that the fetch is
		// done, offer it again — otherwise the cursor silently never arrives.
		if target, ok := m.list.pendingTarget(); ok {
			if req, ok := m.list.RequestLoadUpTo(target); ok {
				return m, fetchMoreCmd(m.repo, req)
			}
		}
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
// arrive. Recorded at the moment the search is started, not read off the
// active view when it resolves, since a search started only to light the
// list's rows up must not also drag its cursor around.
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
// list's half of a search is repo-wide (two `git log` scans over all of
// HEAD), so it's only worth running once something is actually about to use
// it — the list becoming visible with the search still showing, or n/N
// reviving it there. A search that stays inside one commit never pays for
// it, and neither does the common exit path, where the first q/esc dismisses
// the search before the second one returns to the list.
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
	// search always installs a non-nil slice (empty when it found nothing),
	// so nil here means "not searched yet", not "searched, no matches".
	if m.listMatches != nil {
		return m, nil
	}
	return m.startListSearch(m.searchTerm, jump)
}

// applyListMatches installs a resolved search's match positions on the list
// and carries out whatever jump was asked for when it was started (see
// jumpOnResolve), dispatching a targeted fetch when the match it lands on
// isn't loaded yet. matches is also kept here as the canonical copy, so a
// list model built later (see commitsLoadedMsg) can be handed the same set
// rather than losing the search — which is why the jump isn't consumed until
// there are commits to land it on: a search can resolve before the very
// first batch of commits does.
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
		return m.ensureListSearch(jumpNone)
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
// term. There is one search term and both views answer to it, but only the
// view in front of the user is searched now: the detail view resolves
// synchronously as setSearchDisplay installs the term, while the list's
// repo-wide queries wait until something needs them (ensureListSearch).
// Whichever view was active jumps to its first match; the other never moves
// its cursor behind the user's back — the same surprise openDetail avoids
// when a commit is opened with a search running. An empty term clears the
// search entirely instead (a vim-:noh-like escape hatch) — unlike
// dismissSearch, which only hides it.
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
		// Whatever queries are in flight are for the term being replaced:
		// forgetting the term they belong to discards their results when
		// they land (see searchLoadedMsg) and stops the status bar from
		// reporting a search the user has already moved on from. A new
		// search, if there is one, records its own immediately after.
		m.searchingTerm = ""
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
