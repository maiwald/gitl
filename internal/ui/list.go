package ui

import (
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/maiwald/gitl/internal/git"
)

// loadMoreLookahead is how close to the end of loaded commits the cursor
// must get before a lazy-load fetch is triggered.
const loadMoreLookahead = 20

// jumplistCap bounds jumpBack/jumpForward so a long session's jumplist
// can't grow unboundedly — matching vim's own default jumplist size.
const jumplistCap = 100

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
	// See CLAUDE.md's Conventions section for why this stays commit-index
	// space rather than a combined row space.
	cursor int
	vp     viewport.Model

	// leading holds the synthetic, non-commit rows rendered above real
	// history (working tree, then staged), in display order. They take no
	// part in the skip/limit/totalCount/match index arithmetic below, all
	// of which is keyed to real positions in HEAD's history; they're
	// layered on at render time only.
	leading []git.WorktreeEntry

	totalCount  int
	loadingMore bool
	// pendingCursor is an absolute commit position not yet loaded, that
	// should become the cursor as soon as AppendCommits brings it into the
	// loaded range — set by jumpToBottom, applyCurrentMatch, and JumpTo,
	// the three ways a navigation can target a position past what's
	// currently loaded. -1 means nothing is pending. A newer pending
	// target simply overwrites an older one: only the most recent
	// navigation's destination should win once its fetch lands.
	pendingCursor int

	// displayTerm and searchDismissed mirror the root Model's canonical
	// search state (searchTerm/searchDismissed in app.go), pushed down via
	// SetSearchDisplay. They exist here purely so render() — called
	// internally by cursor movement, scrolling, and resizing — doesn't need
	// them threaded through as parameters everywhere; the root Model is the
	// only writer.
	displayTerm     string
	searchDismissed bool

	matches  []int // absolute commit positions, ascending
	matchPos int   // index into matches; -1 when there are none

	// jl is the shared, cross-view jumplist (vim's ctrl-o/ctrl-i) — see its
	// doc comment in app.go for why it lives on the root Model rather than
	// here. pushJump below is this model's only interaction with it: actual
	// JumpBack/JumpForward key handling lives on the root Model instead,
	// since popping an entry can require switching the active view.
	jl *jumplist

	// renderedRows caches one rendered row per position in the flattened
	// row space (leading rows first, then commits — see rowIndex). Every
	// writer of leading/commits/matches/matchPos/displayTerm/
	// searchDismissed/vp.Width must end in a full render() (see its doc
	// comment, and CLAUDE.md); pure cursor moves go through setCursor
	// instead. Direct assignment to cursor outside setCursor (only tests
	// do this) leaves two rows stale until the next full render — never
	// wrong once one happens, only briefly stale.
	renderedRows []string
}

// NewListModel constructs a list model over the given commits, sharing jl —
// the root Model's single cross-view jumplist — for pushJump to record into.
func NewListModel(commits []git.Commit, jl *jumplist) ListModel {
	return ListModel{
		commits:       commits,
		vp:            viewport.New(0, 0),
		totalCount:    len(commits),
		matchPos:      -1,
		pendingCursor: -1,
		jl:            jl,
	}
}

// SetSize resizes the underlying viewport and re-renders content to match.
func (m *ListModel) SetSize(width, height int) {
	m.vp.Width = width
	m.vp.Height = height
	m.render()
}

// SetTotalCount records the total number of commits reachable from HEAD, so
// the list knows when it has reached the true end of history.
func (m *ListModel) SetTotalCount(n int) {
	m.totalCount = n
}

// AppendCommits adds a freshly fetched batch to the loaded set, completing
// a pending jump (see pendingCursor) once its target position is in view.
// Only the newly-appended rows need rendering from scratch; the rest of
// renderedRows is untouched, then the old and new cursor rows are patched
// (harmless if that's the same row twice, or if the new cursor row was
// just rendered by the append loop already — see renderRow).
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
// when the cursor is on a synthetic leading row instead (see SelectedEntry).
func (m ListModel) Selected() (git.Commit, bool) {
	if m.cursor < 0 || m.cursor >= len(m.commits) {
		return git.Commit{}, false
	}
	return m.commits[m.cursor], true
}

// SelectedEntry returns the synthetic leading row currently under the
// cursor, if any — false when the cursor is on a real commit instead (see
// Selected).
func (m ListModel) SelectedEntry() (git.WorktreeEntry, bool) {
	if m.cursor >= 0 {
		return git.WorktreeEntry{}, false
	}
	return m.leading[m.rowIndex(m.cursor)], true
}

// topIndex is the cursor position of the topmost row, whether that's a
// synthetic leading row or (with none loaded) commits[0].
func (m ListModel) topIndex() int {
	return -len(m.leading)
}

// rowIndex maps a cursor-space position (negative for a leading row — see
// the cursor field's doc comment) to its index in the flattened rendered
// row space (leading rows first, then commits), consolidating a transform
// that used to be computed inline and separately in SelectedEntry,
// ensureVisible, and render().
func (m ListModel) rowIndex(pos int) int {
	return pos + len(m.leading)
}

// rowCount is the total number of rendered rows: leading rows plus loaded
// commits.
func (m ListModel) rowCount() int {
	return len(m.leading) + len(m.commits)
}

// SetLeadingEntries installs the synthetic rows shown above the commit
// list. The cursor itself needs no adjustment: it stays a position in
// commits regardless of how many leading rows precede it, so a change here
// can't move it off whatever commit (or leading row) it already points to —
// only the rendered row it corresponds to shifts, which ensureVisible
// accounts for.
func (m *ListModel) SetLeadingEntries(entries []git.WorktreeEntry) {
	m.leading = entries
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
		m.moveCursor(-m.halfPageSize())
	case key.Matches(msg, DefaultKeyMap.HalfPageDown):
		m.moveCursor(m.halfPageSize())
	case key.Matches(msg, DefaultKeyMap.Top):
		m.jumpCursor(m.topIndex())
	case key.Matches(msg, DefaultKeyMap.Bottom):
		return m.jumpToBottom()
	}
	return m.maybeLoadMore()
}

// jumpCursor moves the cursor straight to target, recording the jump first
// (see pushJump) unless target is where the cursor already is — a no-op
// jump has nothing worth returning to. Used by g (Top) and JumpBack/
// JumpForward's setCursor-only landing; jumpToBottom and applyCurrentMatch
// push their own jump directly since their landing may be deferred until a
// fetch completes.
func (m *ListModel) jumpCursor(target int) {
	if target != m.cursor {
		m.pushJump(m.cursor)
	}
	m.setCursor(target)
}

// pushJump records pos — the position the cursor is about to leave — onto
// the shared jumplist (see jl's doc comment).
func (m *ListModel) pushJump(pos int) {
	m.jl.push(jumpPos{view: viewList, listCursor: pos})
}

func (m *ListModel) halfPageSize() int {
	h := m.vp.Height / 2
	if h < 1 {
		h = 1
	}
	return h
}

func (m *ListModel) moveCursor(delta int) {
	m.setCursor(clampBetween(m.cursor+delta, m.topIndex(), len(m.commits)-1))
}

// clampIndex constrains next to the valid index range [0, max], as used by
// [/] section movement in DetailModel (which has no synthetic-row concept
// and so always clamps at 0).
func clampIndex(next, max int) int {
	return clampBetween(next, 0, max)
}

// clampBetween constrains next to the valid range [min, max].
func clampBetween(next, min, max int) int {
	if next < min {
		return min
	}
	if next > max {
		return max
	}
	return next
}

// matchPosition reports pos as a 1-based position within total, or (0, 0)
// when there are no matches — the shared shape behind both ListModel's and
// DetailModel's MatchPosition methods.
func matchPosition(pos, total int) (int, int) {
	if total == 0 {
		return 0, 0
	}
	return pos + 1, total
}

// jumpToBottom moves the cursor to the last loaded commit, or, if history
// isn't fully loaded yet, requests the remaining commits and defers the
// jump until they arrive.
func (m *ListModel) jumpToBottom() (fetchRequest, bool) {
	if len(m.commits) >= m.totalCount {
		m.jumpCursor(len(m.commits) - 1)
		return fetchRequest{}, false
	}
	// The target isn't loaded, so it's necessarily different from the
	// cursor's current (loaded) position — push unconditionally rather than
	// duplicating jumpCursor's now-vs-later comparison here.
	m.pushJump(m.cursor)
	m.pendingCursor = m.totalCount - 1
	return m.RequestLoadUpTo(m.totalCount - 1)
}

// RequestLoadUpTo requests whatever commits are needed so that index target
// (plus a little lookahead) is loaded, unless a fetch is already in flight
// or it's already loaded. This is the same "fetch the rest in one shot"
// mechanism jumpToBottom uses (target = totalCount-1 there), just
// parameterized by an arbitrary position — used by search to load only as
// far as the relevant match rather than the entire remaining history.
// Exported so app.go can trigger it directly for search-driven jumps.
func (m *ListModel) RequestLoadUpTo(target int) (fetchRequest, bool) {
	if m.loadingMore || len(m.commits) >= m.totalCount {
		return fetchRequest{}, false
	}
	need := target + 1 + loadMoreLookahead
	if need > m.totalCount {
		need = m.totalCount
	}
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

// ensureVisible scrolls the viewport so the cursor row stays on screen. The
// comparison is against the cursor's actual rendered row (rowIndex), not
// m.cursor directly, since the cursor itself stays in commit-index space
// and can be negative (see the cursor field's doc comment).
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

// Search records matches (absolute commit positions, ascending, as computed
// by the caller from a git-provided hash set) and jumps to the first match
// at or after the cursor, wrapping to the first match if none qualify. It
// reports needsLoad=true when that position isn't loaded yet, in which
// case the caller should dispatch a fetch via RequestLoadUpTo(target); the
// jump completes automatically once AppendCommits delivers it. See
// SetSearchDisplay for how the search term itself gets here.
func (m *ListModel) Search(matches []int) (needsLoad bool, target int) {
	m.matches = matches
	m.matchPos = -1
	m.pendingCursor = -1
	if len(matches) == 0 {
		m.render()
		return false, 0
	}
	m.matchPos = 0 // wrap to the first match if none qualify at/after the cursor
	for i, idx := range matches {
		if idx >= m.cursor {
			m.matchPos = i
			break
		}
	}
	return m.applyCurrentMatch()
}

// NextMatch and PrevMatch cycle to the following/preceding search match,
// wrapping at both ends. See Search for the needsLoad/target contract.
func (m *ListModel) NextMatch() (needsLoad bool, target int) { return m.stepMatch(1) }
func (m *ListModel) PrevMatch() (needsLoad bool, target int) { return m.stepMatch(-1) }

func (m *ListModel) stepMatch(delta int) (needsLoad bool, target int) {
	if len(m.matches) == 0 {
		return false, 0
	}
	m.matchPos = (m.matchPos + delta + len(m.matches)) % len(m.matches)
	return m.applyCurrentMatch()
}

// applyCurrentMatch moves the cursor to the current match if it's already
// loaded, or marks a jump as pending (resolved by AppendCommits) and
// reports needsLoad so the caller dispatches a targeted fetch. Always ends
// in a full render(), even when deferred: matches/matchPos just changed,
// which can affect dimming/highlighting across arbitrary rows, not just
// the cursor's.
//
// This is the sole path behind Search's initial jump and every later
// NextMatch/PrevMatch step, so every one of them pushes a jumplist entry
// (unlike vim, where only the initial search is jumplisted) — deliberate,
// since a single step here can jump much farther than vim's local
// from-cursor re-search (message match to diff match, or across files).
func (m *ListModel) applyCurrentMatch() (needsLoad bool, target int) {
	target = m.matches[m.matchPos]
	if target != m.cursor {
		m.pushJump(m.cursor)
	}
	needsLoad = m.jumpOrDefer(target)
	m.render()
	return needsLoad, target
}

// JumpTo positions the cursor at absolute commit position target, or, if
// it isn't loaded yet, marks it as pending (resolved by AppendCommits, the
// same mechanism jumpToBottom/applyCurrentMatch use) and reports needsLoad
// so the caller dispatches a fetch via RequestLoadUpTo. Used for a
// CLI-requested jump to a specific commit's position in history.
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
// itself — callers differ in what else changed, so they decide between a
// full render() and a renderCursorMoved patch.
func (m *ListModel) jumpOrDefer(target int) (needsLoad bool) {
	if target >= len(m.commits) {
		m.pendingCursor = target
		return true
	}
	m.cursor = target
	m.ensureVisible()
	return false
}

// ClearSearch discards the active search's matches. The term/dismissed
// display state is cleared separately via SetSearchDisplay.
func (m *ListModel) ClearSearch() {
	m.matches = nil
	m.matchPos = -1
	m.pendingCursor = -1
	m.render()
}

// SetSearchDisplay updates the term to highlight and whether highlighting
// is currently dismissed, then re-renders. The root Model calls this
// whenever the shared search state changes — see the doc comment on the
// displayTerm/searchDismissed fields.
func (m *ListModel) SetSearchDisplay(term string, dismissed bool) {
	m.displayTerm = term
	m.searchDismissed = dismissed
	m.render()
}

// MatchPosition reports the current match position (1-based) and total
// count for status-bar display.
func (m ListModel) MatchPosition() (pos, total int) {
	return matchPosition(m.matchPos, len(m.matches))
}

// rowContext captures the two styling inputs every row in one render
// shares (the effective search term, blanked when dismissed, and which
// commit position is the active n/N match) — hoisted so the full rebuild
// and a single-row patch derive them identically and can't drift apart.
type rowContext struct {
	term        string
	activeMatch int
}

func (m ListModel) rowContext() rowContext {
	term := m.displayTerm
	activeMatch := -1
	if m.searchDismissed {
		term = ""
	} else if m.matchPos >= 0 && m.matchPos < len(m.matches) {
		activeMatch = m.matches[m.matchPos]
	}
	return rowContext{term: term, activeMatch: activeMatch}
}

// isMatch reports whether commit position pos is one of the list's actual
// search matches (m.matches, ascending) — a binary search, since a
// single-row patch needs to query one arbitrary position without
// rescanning from the start.
func (m ListModel) isMatch(pos int) bool {
	_, found := slices.BinarySearch(m.matches, pos)
	return found
}

// renderRow renders the row at cursor-space position pos (negative for a
// leading row) using ctx's shared styling inputs. This is the single
// definition of what a row looks like — both the full rebuild (render)
// and a single-row patch (patchRow) call it, so they cannot drift apart.
func (m ListModel) renderRow(pos int, ctx rowContext) string {
	if pos < 0 {
		e := m.leading[m.rowIndex(pos)]
		return renderWorktreeRow(e, m.vp.Width, pos == m.cursor, ctx.term != "")
	}
	c := m.commits[pos]
	// A synthetic row can never be a search match (matches come from git
	// log queries over real commits only), so it's dimmed whenever a
	// search is active — the correct behavior, not a compromise.
	dimmed := ctx.term != "" && !m.isMatch(pos)
	return renderCommitRow(c, m.vp.Width, pos == m.cursor, dimmed, ctx.term, pos == ctx.activeMatch)
}

// setContent joins the cached rendered rows into the viewport's content.
// The join itself is still O(total content length), forced by
// viewport.SetContent's API (which re-splits it internally) — what the
// cache eliminates is the styling work upstream of the join (fmt.Sprintf,
// several lipgloss Render calls, highlightSpans), which is the expensive
// part.
func (m *ListModel) setContent() {
	m.vp.SetContent(strings.Join(m.renderedRows, "\n"))
}

// patchRow re-renders a single row in place. A no-op if pos maps outside
// the cache's current bounds, which lets callers pass an out-of-range "old
// cursor" (e.g. before any rows were ever rendered) without special-casing.
func (m *ListModel) patchRow(pos int, ctx rowContext) {
	row := m.rowIndex(pos)
	if row < 0 || row >= len(m.renderedRows) {
		return
	}
	m.renderedRows[row] = m.renderRow(pos, ctx)
}

// renderCursorMoved patches only the row the cursor left (from) and the
// row it arrived at (m.cursor), instead of re-rendering every loaded row —
// the fix for a pure cursor move being an O(rows loaded) operation
// regardless of how much actually changed. Falls back to a full render()
// if the cache isn't the size render() would produce, which can only
// happen if some other writer of leading/commits forgot to end in one.
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
// changed. This is the one place the "old cursor" is captured, before it's
// overwritten — every pure cursor-movement path routes through it.
func (m *ListModel) setCursor(pos int) {
	from := m.cursor
	m.cursor = pos
	m.ensureVisible()
	m.renderCursorMoved(from)
}

// render fully rebuilds renderedRows and the viewport content. This is the
// only path that can grow/shrink the row space or repaint arbitrarily many
// rows at once (a width change, new leading rows, or a change to which
// rows are dimmed/highlighted by search) — pure cursor movement instead
// goes through setCursor/renderCursorMoved, which patch only the two rows
// that changed.
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
