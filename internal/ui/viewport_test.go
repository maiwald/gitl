package ui

import (
	"strings"
	"testing"
)

func numberedRows(n int) []string {
	rows := make([]string, n)
	for i := range rows {
		rows[i] = string(rune('a' + i))
	}
	return rows
}

// TestRowViewportViewIsAlwaysHeightLines pins what Model.View depends on: the
// status bar is concatenated directly below this block, so a window that
// rendered fewer lines than its height would drag the status bar up with it.
func TestRowViewportViewIsAlwaysHeightLines(t *testing.T) {
	for _, tc := range []struct {
		name   string
		rows   int
		height int
	}{
		{"empty content", 0, 4},
		{"shorter than the window", 2, 4},
		{"exactly the window", 4, 4},
		{"taller than the window", 10, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := rowViewport{Height: tc.height}
			v.SetRows(numberedRows(tc.rows))
			if got := strings.Count(v.View(), "\n") + 1; got != tc.height {
				t.Fatalf("View() rendered %d lines, want %d: %q", got, tc.height, v.View())
			}
		})
	}
}

func TestRowViewportViewIsEmptyWithoutHeight(t *testing.T) {
	v := rowViewport{Height: 0}
	v.SetRows(numberedRows(10))
	if got := v.View(); got != "" {
		t.Fatalf("View() = %q on a zero-height window, want the empty string", got)
	}
}

func TestRowViewportViewShowsTheWindowAtYOffset(t *testing.T) {
	v := rowViewport{Height: 3}
	v.SetRows(numberedRows(10))
	v.SetYOffset(4)
	if got, want := v.View(), "e\nf\ng"; got != want {
		t.Fatalf("View() = %q, want %q", got, want)
	}
}

// TestRowViewportSetYOffsetClampsToTheContent pins the clamp DetailModel.reflow
// is written against: it pads its content with blank rows precisely to widen
// maxYOffset, so a short trailing file's first line can still be scrolled to
// the top. A different clamp shape silently changes what [ / ] can reach.
func TestRowViewportSetYOffsetClampsToTheContent(t *testing.T) {
	cases := []struct {
		name   string
		rows   int
		height int
		set    int
		want   int
	}{
		{"negative", 10, 3, -5, 0},
		{"past the end", 10, 3, 99, 7},
		{"content shorter than the window", 2, 3, 99, 0},
		{"in range", 10, 3, 4, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := rowViewport{Height: tc.height}
			v.SetRows(numberedRows(tc.rows))
			v.SetYOffset(tc.set)
			if v.YOffset != tc.want {
				t.Fatalf("SetYOffset(%d) left YOffset=%d, want %d", tc.set, v.YOffset, tc.want)
			}
		})
	}
}

// TestRowViewportSetRowsPinsAnOffsetPastTheEndToTheBottom covers a resize that
// rewraps a diff onto fewer rows than the window is currently scrolled past.
func TestRowViewportSetRowsPinsAnOffsetPastTheEndToTheBottom(t *testing.T) {
	v := rowViewport{Height: 3}
	v.SetRows(numberedRows(20))
	v.SetYOffset(17)

	v.SetRows(numberedRows(6))
	if v.YOffset != 3 {
		t.Fatalf("YOffset = %d after the content shrank to 6 rows, want 3 (its new bottom)", v.YOffset)
	}
}

// TestRowViewportViewSurvivesAnOffsetPastTheEnd guards the case ListModel's
// direct YOffset write makes reachable: bubbles/viewport sliced from an
// unbounded top and would have panicked here.
func TestRowViewportViewSurvivesAnOffsetPastTheEnd(t *testing.T) {
	v := rowViewport{Height: 3}
	v.SetRows(numberedRows(4))
	v.YOffset = 99
	if got, want := v.View(), "\n\n"; got != want {
		t.Fatalf("View() = %q past the end of the content, want %q (a blank window)", got, want)
	}
}

// TestRowViewportSetRowsBorrowsItsRows documents why renderCursorMoved and
// repaintCursor may patch a row in place: the viewport holds the caller's
// slice, not a copy of it. Turning the borrow into a copy would put an
// O(content) step back onto every keypress — the whole reason this type exists.
func TestRowViewportSetRowsBorrowsItsRows(t *testing.T) {
	rows := numberedRows(4)
	v := rowViewport{Height: 4}
	v.SetRows(rows)

	rows[1] = "patched"
	if !strings.Contains(v.View(), "patched") {
		t.Fatalf("View() = %q, want it to show a row patched in place after SetRows", v.View())
	}
}
