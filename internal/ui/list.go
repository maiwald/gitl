package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
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
	// cursor is a position in commits. Negative values address the
	// synthetic leading rows instead: leading[i] corresponds to
	// cursor == i-len(leading), so -1 is the row directly above commits[0].
	// Keeping the cursor in commit-index space rather than a combined row
	// space is what lets every other index computation in this file
	// (matches, fetchRequest skip/limit, totalCount comparisons,
	// maybeLoadMore's lookahead check) stay exactly as it was before
	// synthetic rows existed — none of them are meant to know leading rows
	// exist at all.
	cursor int
	vp     viewport.Model

	// leading holds the synthetic, non-commit rows rendered above real
	// history (working tree, then staged), in display order. They take no
	// part in the skip/limit/totalCount/match index arithmetic below, all
	// of which is keyed to real positions in HEAD's history; they're
	// layered on at render time only.
	leading []git.WorktreeEntry

	totalCount       int
	loadingMore      bool
	jumpToEndPending bool

	// displayTerm and searchDismissed mirror the root Model's canonical
	// search state (searchTerm/searchDismissed in app.go), pushed down via
	// SetSearchDisplay. They exist here purely so render() — called
	// internally by cursor movement, scrolling, and resizing — doesn't need
	// them threaded through as parameters everywhere; the root Model is the
	// only writer.
	displayTerm     string
	searchDismissed bool

	matches          []int // absolute commit positions, ascending
	matchPos         int   // index into matches; -1 when there are none
	matchJumpPending bool
}

// NewListModel constructs a list model over the given commits.
func NewListModel(commits []git.Commit) ListModel {
	return ListModel{
		commits:    commits,
		vp:         viewport.New(0, 0),
		totalCount: len(commits),
		matchPos:   -1,
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
// any pending jump-to-bottom or jump-to-search-match once the target
// position is in view.
func (m *ListModel) AppendCommits(commits []git.Commit) {
	m.commits = append(m.commits, commits...)
	m.loadingMore = false
	if m.jumpToEndPending && len(m.commits) >= m.totalCount {
		m.jumpToEndPending = false
		m.cursor = len(m.commits) - 1
		m.ensureVisible()
	}
	if m.matchJumpPending && m.matchPos >= 0 && m.matches[m.matchPos] < len(m.commits) {
		m.matchJumpPending = false
		m.cursor = m.matches[m.matchPos]
		m.ensureVisible()
	}
	m.render()
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
	return m.leading[m.cursor+len(m.leading)], true
}

// topIndex is the cursor position of the topmost row, whether that's a
// synthetic leading row or (with none loaded) commits[0].
func (m ListModel) topIndex() int {
	return -len(m.leading)
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
		m.cursor = m.topIndex()
		m.ensureVisible()
		m.render()
	case key.Matches(msg, DefaultKeyMap.Bottom):
		return m.jumpToBottom()
	}
	return m.maybeLoadMore()
}

func (m *ListModel) halfPageSize() int {
	h := m.vp.Height / 2
	if h < 1 {
		h = 1
	}
	return h
}

func (m *ListModel) moveCursor(delta int) {
	m.cursor = clampBetween(m.cursor+delta, m.topIndex(), len(m.commits)-1)
	m.ensureVisible()
	m.render()
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
		m.cursor = len(m.commits) - 1
		m.ensureVisible()
		m.render()
		return fetchRequest{}, false
	}
	m.jumpToEndPending = true
	return m.beginLoadingUpTo(m.totalCount - 1)
}

// beginLoadingUpTo requests whatever commits are needed so that index
// target (plus a little lookahead) is loaded, unless a fetch is already in
// flight or it's already loaded. This is the same "fetch the rest in one
// shot" mechanism jumpToBottom uses (target = totalCount-1 there), just
// parameterized by an arbitrary position — used by search to load only as
// far as the relevant match rather than the entire remaining history.
func (m *ListModel) beginLoadingUpTo(target int) (fetchRequest, bool) {
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

// RequestLoadUpTo dispatches (or reuses an already-in-flight) fetch to
// bring position target into the loaded range. Exported so app.go can
// trigger it directly for search-driven jumps.
func (m *ListModel) RequestLoadUpTo(target int) (fetchRequest, bool) {
	return m.beginLoadingUpTo(target)
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
// comparison is against the cursor's actual rendered row (row, offset by
// len(leading)), not m.cursor directly, since the cursor itself stays in
// commit-index space and can be negative (see the cursor field's doc
// comment).
func (m *ListModel) ensureVisible() {
	if m.vp.Height <= 0 {
		return
	}
	row := m.cursor + len(m.leading)
	if row < m.vp.YOffset {
		m.vp.YOffset = row
	} else if row >= m.vp.YOffset+m.vp.Height {
		m.vp.YOffset = row - m.vp.Height + 1
	}
	if m.vp.YOffset < 0 {
		m.vp.YOffset = 0
	}
}

// Search records matches (absolute commit positions, ascending, as computed
// by the caller from a git-provided hash set) and jumps to the first match
// at or after the cursor, wrapping to the first match if none qualify. It
// reports needsLoad=true when that position isn't loaded yet, in which
// case the caller should dispatch a fetch via RequestLoadUpTo(target); the
// jump completes automatically once AppendCommits delivers it. The search
// term itself is set separately via SetSearchDisplay, since it's shared
// with the detail view and owned by the root Model.
func (m *ListModel) Search(matches []int) (needsLoad bool, target int) {
	m.matches = matches
	m.matchPos = -1
	m.matchJumpPending = false
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
// reports needsLoad so the caller dispatches a targeted fetch.
func (m *ListModel) applyCurrentMatch() (needsLoad bool, target int) {
	target = m.matches[m.matchPos]
	if target >= len(m.commits) {
		m.matchJumpPending = true
		return true, target
	}
	m.cursor = target
	m.ensureVisible()
	m.render()
	return false, target
}

// ClearSearch discards the active search's matches. The term/dismissed
// display state is cleared separately via SetSearchDisplay.
func (m *ListModel) ClearSearch() {
	m.matches = nil
	m.matchPos = -1
	m.matchJumpPending = false
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

func (m *ListModel) render() {
	term := m.displayTerm
	activeMatch := -1
	if m.searchDismissed {
		term = ""
	} else if m.matchPos >= 0 && m.matchPos < len(m.matches) {
		activeMatch = m.matches[m.matchPos]
	}
	lines := make([]string, len(m.leading)+len(m.commits))
	for i, e := range m.leading {
		selected := i-len(m.leading) == m.cursor
		// A synthetic row can never be a search match (matches come from
		// git log queries over real commits only), so it's dimmed whenever
		// a search is active — the correct behavior, not a compromise.
		lines[i] = renderWorktreeRow(e, m.vp.Width, selected, term != "")
	}
	mi := 0 // walks m.matches (ascending) alongside i, since both are sorted
	for i, c := range m.commits {
		for mi < len(m.matches) && m.matches[mi] < i {
			mi++
		}
		isMatch := mi < len(m.matches) && m.matches[mi] == i
		dimmed := term != "" && !isMatch
		lines[len(m.leading)+i] = renderCommitRow(c, m.vp.Width, i == m.cursor, dimmed, term, i == activeMatch)
	}
	m.vp.SetContent(strings.Join(lines, "\n"))
}

func (m ListModel) View() string {
	return m.vp.View()
}
