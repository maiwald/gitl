package ui

import (
	"slices"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/maiwald/gitl/internal/git"
)

// loadMoreLookahead is how close to the end of loaded commits the cursor
// must get before a lazy-load fetch is triggered.
const loadMoreLookahead = 20

// fetchRequest describes a batch of commits ListModel needs from the repo
// to satisfy a navigation that has run past the currently loaded data.
type fetchRequest struct {
	skip, limit int
}

// ListModel renders a scrollable list of commits with a movable cursor,
// lazily requesting more commits as the cursor approaches the loaded end.
type ListModel struct {
	commits []git.Commit
	// cursor is a position in commits; negative values address the
	// synthetic leading rows instead (leading[i] ⟺ cursor == i-len(leading)).
	// It stays in commit-index space rather than a combined row space so
	// that every other index computation here can ignore leading rows — see
	// CLAUDE.md's Conventions section. It is always within [topIndex(),
	// lastIndex()], the rows that actually exist.
	cursor int
	vp     rowViewport
	width  int

	// leading holds the synthetic, non-commit rows rendered above real
	// history (working tree, then staged), in display order. They take no
	// part in the skip/limit/totalCount/match arithmetic below, which is
	// keyed to real positions in HEAD's history; they're layered on at
	// render time only.
	leading []git.EntrySummary

	totalCount  int
	loadingMore bool
	// pendingCursor is an absolute commit position not yet loaded, that
	// should become the cursor as soon as AppendCommits brings it into the
	// loaded range. -1 means nothing is pending; a newer target overwrites
	// an older one, so the most recent navigation wins once its fetch lands.
	pendingCursor int

	// displayTerm and searchDismissed mirror the root Model's canonical
	// search state, pushed down via SetSearchDisplay. The root Model is the
	// only writer.
	displayTerm     string
	searchDismissed bool

	matches  []int // absolute commit positions, ascending
	matchPos int   // index into matches; -1 when there are none

	// jl is the shared, cross-view jumplist (ctrl-o/ctrl-i), owned by the
	// root Model. pushJump is this model's only interaction with it;
	// traversal lives on Model.
	jl *jumplist

	// renderedRows caches one rendered row per position in the flattened
	// row space (leading rows first, then commits — see rowIndex). Every
	// writer of leading/commits/matches/displayTerm/searchDismissed/width
	// must end in a full render(); pure cursor moves go through setCursor
	// instead, and a step between matches (matchPos) through
	// renderMatchMoved. Assigning cursor directly (only tests do) leaves two
	// rows stale until the next full render.
	renderedRows []string
}

// NewListModel constructs a list model over the given commits, sharing the
// root Model's cross-view jumplist for pushJump to record into.
func NewListModel(commits []git.Commit, jl *jumplist) ListModel {
	return ListModel{
		commits:       commits,
		totalCount:    len(commits),
		matchPos:      -1,
		pendingCursor: -1,
		jl:            jl,
	}
}

// SetSize resizes the underlying viewport and re-renders content to match.
func (m *ListModel) SetSize(width, height int) {
	m.width = width
	m.vp.Height = height
	m.render()
}

// SetTotalCount records the total number of commits reachable from HEAD, so
// the list knows when it has reached the true end of history.
func (m *ListModel) SetTotalCount(n int) {
	m.totalCount = n
}

// AppendCommits adds a freshly fetched batch to the loaded set, completing a
// pending jump (see pendingCursor) once its target position is in view. Only
// the newly-appended rows are rendered from scratch; the rest of
// renderedRows is left alone, and the old and new cursor rows are patched.
func (m *ListModel) AppendCommits(commits []git.Commit) {
	from := m.cursor
	prevCount := len(m.commits)
	m.commits = append(m.commits, commits...)
	m.loadingMore = false
	if m.pendingCursor >= 0 && m.pendingCursor < len(m.commits) {
		m.cursor = m.pendingCursor
		m.pendingCursor = -1
		m.ensureVisible()
	}

	if len(m.renderedRows) != len(m.leading)+prevCount {
		m.render()
		return
	}
	ctx := m.rowContext()
	for pos := prevCount; pos < len(m.commits); pos++ {
		m.renderedRows = append(m.renderedRows, m.renderRow(pos, ctx))
	}
	m.patchRow(from, ctx)
	m.patchRow(m.cursor, ctx)
	m.setContent()
}

// Selected returns the commit currently under the cursor, if any — false
// when the cursor is on a synthetic leading row instead (see
// SelectedSummary).
func (m ListModel) Selected() (git.Commit, bool) {
	if m.cursor < 0 || m.cursor >= len(m.commits) {
		return git.Commit{}, false
	}
	return m.commits[m.cursor], true
}

// SelectedSummary returns the synthetic leading row currently under the
// cursor, if any — false when the cursor is on a real commit instead (see
// Selected). It's a summary, not the entry itself: the diff behind the row is
// only fetched when the row is opened.
func (m ListModel) SelectedSummary() (git.EntrySummary, bool) {
	if m.cursor >= 0 {
		return git.EntrySummary{}, false
	}
	return m.leading[m.rowIndex(m.cursor)], true
}

// topIndex is the cursor position of the topmost row, whether that's a
// synthetic leading row or (with none loaded) commits[0].
func (m ListModel) topIndex() int {
	return -len(m.leading)
}

// lastIndex is the cursor position of the bottommost row: normally the last
// loaded commit, floored at topIndex() so a list with no commits loaded yet
// can't put the cursor below its own row space.
func (m ListModel) lastIndex() int {
	return max(len(m.commits)-1, m.topIndex())
}

// rowIndex maps a cursor-space position (negative for a leading row) to its
// index in the flattened rendered row space, where leading rows come first
// and commits follow.
func (m ListModel) rowIndex(pos int) int {
	return pos + len(m.leading)
}

// rowCount is the total number of rendered rows: leading rows plus loaded
// commits.
func (m ListModel) rowCount() int {
	return len(m.leading) + len(m.commits)
}

// SetLeadingSummaries installs the synthetic rows shown above the commit list.
// The cursor needs no adjustment: it stays a position in commits regardless
// of how many leading rows precede it. Only the rendered row it corresponds
// to shifts, which ensureVisible accounts for.
func (m *ListModel) SetLeadingSummaries(summaries []git.EntrySummary) {
	m.leading = summaries
	m.ensureVisible()
	m.render()
}

// Update processes a navigation key press. It returns a fetchRequest and
// true when more commits must be loaded from the repo to satisfy the
// requested movement (e.g. scrolling near the loaded end, or jumping to the
// bottom of a not-fully-loaded history).
func (m *ListModel) Update(msg tea.KeyMsg) (fetchRequest, bool) {
	switch {
	case key.Matches(msg, DefaultKeyMap.Up):
		m.moveCursor(-1)
	case key.Matches(msg, DefaultKeyMap.Down):
		m.moveCursor(1)
	case key.Matches(msg, DefaultKeyMap.HalfPageUp):
		m.moveCursor(-halfPage(m.vp.Height))
	case key.Matches(msg, DefaultKeyMap.HalfPageDown):
		m.moveCursor(halfPage(m.vp.Height))
	case key.Matches(msg, DefaultKeyMap.Top):
		m.jumpCursor(m.topIndex())
	case key.Matches(msg, DefaultKeyMap.Bottom):
		return m.jumpToBottom()
	}
	return m.maybeLoadMore()
}

// jumpCursor moves the cursor straight to target, recording the jump first
// unless target is where the cursor already is. jumpToBottom and
// applyCurrentMatch push their own jump instead, since their landing may be
// deferred until a fetch completes.
func (m *ListModel) jumpCursor(target int) {
	if target != m.cursor {
		m.pushJump(m.cursor)
	}
	m.setCursor(target)
}

// pushJump records pos — the position the cursor is about to leave — onto
// the shared jumplist.
func (m *ListModel) pushJump(pos int) {
	m.jl.push(jumpPos{view: viewList, listCursor: pos})
}

func (m *ListModel) moveCursor(delta int) {
	m.setCursor(clamp(m.cursor+delta, m.topIndex(), m.lastIndex()))
}

// jumpToBottom moves the cursor to the last loaded commit, or, if history
// isn't fully loaded yet, requests the remaining commits and defers the
// jump until they arrive.
func (m *ListModel) jumpToBottom() (fetchRequest, bool) {
	if len(m.commits) >= m.totalCount {
		m.jumpCursor(m.lastIndex())
		return fetchRequest{}, false
	}
	// The target isn't loaded, so it's necessarily different from the
	// cursor's current position: push unconditionally.
	m.pushJump(m.cursor)
	m.pendingCursor = m.totalCount - 1
	return m.RequestLoadUpTo(m.totalCount - 1)
}

// pendingTarget reports the position a deferred jump is still waiting to be
// loaded (see pendingCursor), if there is one. RequestLoadUpTo refuses while
// another fetch is in flight, so a jump requested during one has to be
// re-offered once that fetch lands — see moreCommitsLoadedMsg in app.go.
func (m ListModel) pendingTarget() (int, bool) {
	return m.pendingCursor, m.pendingCursor >= 0
}

// RequestLoadUpTo requests whatever commits are needed so that index target
// (plus a little lookahead) is loaded, unless a fetch is already in flight or
// the position is already loaded. Search uses it to load only as far as the
// relevant match rather than the entire remaining history; jumpToBottom uses
// it with target = totalCount-1.
func (m *ListModel) RequestLoadUpTo(target int) (fetchRequest, bool) {
	if m.loadingMore || len(m.commits) >= m.totalCount {
		return fetchRequest{}, false
	}
	need := min(target+1+loadMoreLookahead, m.totalCount)
	if need <= len(m.commits) {
		return fetchRequest{}, false
	}
	m.loadingMore = true
	return fetchRequest{skip: len(m.commits), limit: need - len(m.commits)}, true
}

// maybeLoadMore requests the next batch when the cursor has scrolled near
// the end of what's currently loaded and more commits remain.
func (m *ListModel) maybeLoadMore() (fetchRequest, bool) {
	if m.loadingMore || len(m.commits) >= m.totalCount {
		return fetchRequest{}, false
	}
	if m.cursor < len(m.commits)-loadMoreLookahead {
		return fetchRequest{}, false
	}
	m.loadingMore = true
	return fetchRequest{skip: len(m.commits), limit: batchSize}, true
}

// ensureVisible scrolls the viewport so the cursor row stays on screen. It
// compares against the cursor's rendered row (rowIndex), not m.cursor, which
// stays in commit-index space and can be negative.
//
// It writes YOffset directly rather than going through the clamping
// SetYOffset: AppendCommits calls this after growing m.commits but before
// handing the grown rows over, so the viewport still holds the pre-append set
// and would clamp a landing position that is about to be perfectly valid back
// off it. The row index this derives from always exists in the row space the
// next setContent hands over, so max(..., 0) is the only bound needed.
func (m *ListModel) ensureVisible() {
	if m.vp.Height <= 0 {
		return
	}
	row := m.rowIndex(m.cursor)
	if row < m.vp.YOffset {
		m.vp.YOffset = row
	} else if row >= m.vp.YOffset+m.vp.Height {
		m.vp.YOffset = row - m.vp.Height + 1
	}
	m.vp.YOffset = max(m.vp.YOffset, 0)
}

// SetMatches records matches (absolute commit positions, ascending) without
// moving the cursor, so a search submitted from the detail view can light up
// this view's rows without dragging its cursor somewhere the user isn't
// looking. The term itself arrives separately, via SetSearchDisplay.
func (m *ListModel) SetMatches(matches []int) {
	m.matches = matches
	m.matchPos = -1
	m.pendingCursor = -1
	m.render()
}

// jumpToFirstMatch jumps to the first match at or after the cursor, wrapping
// to the first match if none qualify. It reports needsLoad=true when that
// position isn't loaded yet, in which case the caller should dispatch a fetch
// via RequestLoadUpTo(target); the jump completes once AppendCommits delivers
// it.
func (m *ListModel) jumpToFirstMatch() (needsLoad bool, target int) {
	if len(m.matches) == 0 {
		return false, 0
	}
	prev := m.activeMatchPos()
	m.matchPos = 0 // wrap to the first match if none qualify at/after the cursor
	for i, idx := range m.matches {
		if idx >= m.cursor {
			m.matchPos = i
			break
		}
	}
	return m.applyCurrentMatch(prev)
}

// jumpToLastMatch is jumpToFirstMatch's mirror: the last match at or before
// the cursor, wrapping to the last match overall if none qualify. It's where
// N lands when N is the key that triggered the search (see ensureListSearch
// in app.go).
func (m *ListModel) jumpToLastMatch() (needsLoad bool, target int) {
	if len(m.matches) == 0 {
		return false, 0
	}
	prev := m.activeMatchPos()
	m.matchPos = len(m.matches) - 1
	for i := len(m.matches) - 1; i >= 0; i-- {
		if m.matches[i] <= m.cursor {
			m.matchPos = i
			break
		}
	}
	return m.applyCurrentMatch(prev)
}

// NextMatch and PrevMatch cycle to the following/preceding search match,
// wrapping at both ends. See jumpToFirstMatch for the needsLoad/target
// contract.
func (m *ListModel) NextMatch() (needsLoad bool, target int) { return m.stepMatch(1) }
func (m *ListModel) PrevMatch() (needsLoad bool, target int) { return m.stepMatch(-1) }

func (m *ListModel) stepMatch(delta int) (needsLoad bool, target int) {
	if len(m.matches) == 0 {
		return false, 0
	}
	prev := m.activeMatchPos()
	m.matchPos = stepMatchPos(m.matchPos, delta, len(m.matches))
	return m.applyCurrentMatch(prev)
}

// applyCurrentMatch moves the cursor to the current match if it's already
// loaded, or marks a jump as pending (resolved by AppendCommits) and reports
// needsLoad so the caller dispatches a targeted fetch. prev is the position of
// the match that was active before matchPos changed (see activeMatchPos).
// Either way it repaints just the rows whose look changed: the one the active
// match left, the one it landed on, and the cursor's two. Which match is
// active changes only that row's highlight — which rows are dimmed depends on
// the match *set*, not on which of them is active.
//
// It backs both the initial search jump and every later NextMatch/PrevMatch
// step, so every one of them pushes a jumplist entry — unlike vim, which
// jumplists only the initial search, since a single step here can jump much
// farther than vim's local from-cursor re-search.
func (m *ListModel) applyCurrentMatch(prev int) (needsLoad bool, target int) {
	target = m.matches[m.matchPos]
	if target != m.cursor {
		m.pushJump(m.cursor)
	}
	from := m.cursor
	needsLoad = m.jumpOrDefer(target)
	m.renderMatchMoved(from, prev)
	return needsLoad, target
}

// renderMatchMoved is renderCursorMoved for a step between matches: it
// patches the rows the cursor left and arrived at, and the row the active
// match left and the one it landed on. A target that isn't loaded yet has no
// row to patch; AppendCommits renders it when it arrives.
func (m *ListModel) renderMatchMoved(cursorFrom, prevActive int) {
	if len(m.renderedRows) != m.rowCount() {
		m.render()
		return
	}
	ctx := m.rowContext()
	m.patchRow(cursorFrom, ctx)
	m.patchRow(m.cursor, ctx)
	m.patchRow(prevActive, ctx)
	m.patchRow(ctx.activeMatch, ctx)
	m.setContent()
}

// JumpTo positions the cursor at absolute commit position target, or, if it
// isn't loaded yet, marks it as pending (resolved by AppendCommits) and
// reports needsLoad so the caller dispatches a fetch via RequestLoadUpTo.
// Used for jumplist traversal.
func (m *ListModel) JumpTo(target int) (needsLoad bool) {
	from := m.cursor
	needsLoad = m.jumpOrDefer(target)
	if !needsLoad {
		m.renderCursorMoved(from)
	}
	return needsLoad
}

// jumpOrDefer is the shared "move the cursor there now, or defer until it's
// loaded" logic behind applyCurrentMatch and JumpTo. It renders nothing
// itself: callers differ in what else changed, so they choose between a full
// render() and a renderCursorMoved patch.
func (m *ListModel) jumpOrDefer(target int) (needsLoad bool) {
	if target >= len(m.commits) {
		m.pendingCursor = target
		return true
	}
	m.cursor = target
	m.ensureVisible()
	return false
}

// SetSearchDisplay updates the term to highlight and whether highlighting is
// dismissed, re-rendering if either changed. Called by the root Model
// whenever the shared search state changes. A changed term drops the previous
// term's matches, so rows are never dimmed against a match set that no longer
// describes them; the new term's matches arrive later, via SetMatches, since
// finding them takes a git query.
func (m *ListModel) SetSearchDisplay(term string, dismissed bool) {
	if term == m.displayTerm && dismissed == m.searchDismissed {
		return
	}
	if term != m.displayTerm {
		m.displayTerm = term
		m.matches, m.matchPos = nil, -1
	}
	m.searchDismissed = dismissed
	m.render()
}

// MatchPosition reports the current match position (1-based) and total
// count for status-bar display.
func (m ListModel) MatchPosition() (pos, total int) {
	return matchPosition(m.matchPos, len(m.matches))
}

// rowContext captures the two styling inputs every row in one render shares:
// the effective search term, blanked when dismissed, and which commit
// position is the active n/N match. Hoisted so the full rebuild and a
// single-row patch derive them identically.
type rowContext struct {
	term        string
	activeMatch int
}

func (m ListModel) rowContext() rowContext {
	term := m.displayTerm
	activeMatch := -1
	if m.searchDismissed {
		term = ""
	} else {
		activeMatch = m.activeMatchPos()
	}
	return rowContext{term: term, activeMatch: activeMatch}
}

// activeMatchPos is the commit position of the n/N-selected match, or -1 when
// there is none.
func (m ListModel) activeMatchPos() int {
	if m.matchPos >= 0 && m.matchPos < len(m.matches) {
		return m.matches[m.matchPos]
	}
	return -1
}

// isMatch reports whether commit position pos is one of the list's actual
// search matches (m.matches, ascending).
func (m ListModel) isMatch(pos int) bool {
	_, found := slices.BinarySearch(m.matches, pos)
	return found
}

// renderRow renders the row at cursor-space position pos (negative for a
// leading row) using ctx's shared styling inputs. It is the single definition
// of what a row looks like: both the full rebuild (render) and a single-row
// patch (patchRow) go through it.
func (m ListModel) renderRow(pos int, ctx rowContext) string {
	if pos < 0 {
		s := m.leading[m.rowIndex(pos)]
		return renderWorktreeRow(s, m.width, pos == m.cursor, ctx.term != "")
	}
	c := m.commits[pos]
	// Matches come from git log queries over real commits only, so a
	// synthetic row is never one and is dimmed whenever a search is
	// active.
	dimmed := ctx.term != "" && !m.isMatch(pos)
	return renderCommitRow(c, m.width, pos == m.cursor, dimmed, ctx.term, pos == ctx.activeMatch)
}

// setContent hands the cached rows to the viewport, which borrows the slice
// rather than copying it. So a row patched in place (patchRow) is already
// visible without this call; what needs it is render()'s fresh slice and
// AppendCommits' append, either of which may move the backing array.
func (m *ListModel) setContent() {
	m.vp.SetRows(m.renderedRows)
}

// patchRow re-renders a single row in place. A no-op if pos maps outside the
// cache's current bounds, so callers can pass an out-of-range "old cursor"
// without special-casing it.
func (m *ListModel) patchRow(pos int, ctx rowContext) {
	row := m.rowIndex(pos)
	if row < 0 || row >= len(m.renderedRows) {
		return
	}
	m.renderedRows[row] = m.renderRow(pos, ctx)
}

// renderCursorMoved patches only the row the cursor left (from) and the row
// it arrived at, so a pure cursor move costs two row renders instead of one
// per loaded row. Falls back to a full render() when the cache isn't the size
// render() would produce.
func (m *ListModel) renderCursorMoved(from int) {
	if len(m.renderedRows) != m.rowCount() {
		m.render()
		return
	}
	ctx := m.rowContext()
	m.patchRow(from, ctx)
	m.patchRow(m.cursor, ctx)
	m.setContent()
}

// setCursor moves the cursor to pos and repaints just the two rows that
// changed. Every pure cursor-movement path routes through it, since it's
// where the old cursor position is captured before being overwritten.
func (m *ListModel) setCursor(pos int) {
	from := m.cursor
	m.cursor = pos
	m.ensureVisible()
	m.renderCursorMoved(from)
}

// render fully rebuilds renderedRows and the viewport content. It's the only
// path that can grow or shrink the row space, or repaint arbitrarily many
// rows at once — a width change, new leading rows, or a change to which rows
// search dims or highlights. Pure cursor movement goes through
// setCursor/renderCursorMoved instead.
func (m *ListModel) render() {
	ctx := m.rowContext()
	m.renderedRows = make([]string, m.rowCount())
	for pos := m.topIndex(); pos < len(m.commits); pos++ {
		m.renderedRows[m.rowIndex(pos)] = m.renderRow(pos, ctx)
	}
	m.setContent()
}

func (m ListModel) View() string {
	return m.vp.View()
}
