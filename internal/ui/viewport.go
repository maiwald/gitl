package ui

import "strings"

// rowViewport is a scrolling window onto content that is already laid out as
// one string per display row.
//
// It exists instead of bubbles/viewport because that one takes its content as
// a single string: every SetContent re-splits it and then measures every line
// with ansi.StringWidth to size a horizontal scroll this app never uses. That
// put the whole commit — or the whole loaded history — back onto the cost of
// one cursor move, which is the exact work ListModel.renderedRows and
// DetailModel.content exist to avoid.
type rowViewport struct {
	Height int

	// YOffset is the content row shown on the window's top line. A caller that
	// has already bounded it writes it directly — ListModel.ensureVisible
	// bounds it against rows it hasn't handed over yet, so it can't use the
	// clamp below; SetYOffset is the clamping way in.
	YOffset int

	rows []string
}

// SetRows hands the viewport the rows to display. They are borrowed, not
// copied: both callers keep a long-lived row cache that they patch in place
// and hand back, and copying would put an O(content) step back onto every
// keypress. A patch in place therefore needs no handoff — but anything that
// regrows or replaces the cache does, since the append may have moved the
// backing array.
func (v *rowViewport) SetRows(rows []string) {
	v.rows = rows
	// Content that shrank out from under the scroll position — a resize
	// rewrapping a diff onto fewer rows — pins the window to the new bottom
	// instead of leaving it parked past the end.
	if v.YOffset > len(v.rows)-1 {
		v.YOffset = v.maxYOffset()
	}
}

// maxYOffset is as far as the window can scroll: the offset that puts the last
// row on the window's last line.
func (v rowViewport) maxYOffset() int {
	return max(0, len(v.rows)-v.Height)
}

// SetYOffset scrolls to n, clamped to the content. DetailModel.reflow pads its
// content with blank rows specifically to widen this clamp, so a short trailing
// file's first line can still be brought to the top of the window.
func (v *rowViewport) SetYOffset(n int) {
	v.YOffset = clamp(n, 0, v.maxYOffset())
}

// View renders the visible window as exactly Height lines.
//
// Rows are emitted as they are. Every row a view produces is already padded to
// the full width (padRow), and the ones that aren't are blank by construction —
// a separator row, or the fill below the last line — where trailing spaces and
// an empty line are the same picture once the terminal erases to end of line.
// The row *count* is load-bearing though: Model.View concatenates the status
// bar directly below this block, which would float up under a short one.
func (v rowViewport) View() string {
	if v.Height <= 0 {
		return ""
	}
	top := clamp(v.YOffset, 0, len(v.rows))
	visible := v.rows[top:min(top+v.Height, len(v.rows))]

	var b strings.Builder
	for i := range v.Height {
		if i > 0 {
			b.WriteByte('\n')
		}
		if i < len(visible) {
			b.WriteString(visible[i])
		}
	}
	return b.String()
}
