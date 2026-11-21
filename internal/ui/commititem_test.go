package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/maiwald/gitl/internal/git"
	"github.com/muesli/termenv"
)

// Tests run without a real terminal, where lipgloss's auto-detected color
// profile strips all styling (Render becomes a no-op), making it
// impossible to assert which style was applied to a span. Force a color
// profile so Render always emits distinguishable output.
func init() {
	lipgloss.SetColorProfile(termenv.TrueColor)
}

func TestHighlightSpansStylesMatchedSubstringCaseInsensitively(t *testing.T) {
	base := lipgloss.NewStyle()
	match := lipgloss.NewStyle().Bold(true)

	got := highlightSpans("Hello World", "world", base, match)
	want := base.Render("Hello ") + match.Render("World")
	if got != want {
		t.Errorf("highlightSpans = %q, want %q", got, want)
	}
}

func TestHighlightSpansNoMatchReturnsBaseRender(t *testing.T) {
	base := lipgloss.NewStyle().Bold(true)
	match := lipgloss.NewStyle()

	got := highlightSpans("Hello World", "xyz", base, match)
	want := base.Render("Hello World")
	if got != want {
		t.Errorf("highlightSpans = %q, want %q", got, want)
	}
}

func TestHighlightSpansEmptyTermReturnsBaseRenderUnchanged(t *testing.T) {
	base := lipgloss.NewStyle().Bold(true)
	got := highlightSpans("Hello World", "", base, lipgloss.NewStyle())
	want := base.Render("Hello World")
	if got != want {
		t.Errorf("highlightSpans = %q, want %q", got, want)
	}
}

func fixtureCommitForRow() git.Commit {
	return git.Commit{
		Hash:       "abc123",
		AuthorName: "Alice",
		AuthorDate: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		Subject:    "fix the searchable bug",
	}
}

func TestRenderCommitRowHighlightsAuthorMatch(t *testing.T) {
	row := renderCommitRow(fixtureCommitForRow(), 120, false, false, "alice", false)
	want := searchMatchStyle.Render("Alice")
	if !strings.Contains(row, want) {
		t.Errorf("row = %q, want it to contain the highlighted author %q", row, want)
	}
}

func TestRenderCommitRowHighlightsSubjectMatch(t *testing.T) {
	row := renderCommitRow(fixtureCommitForRow(), 120, false, false, "searchable", false)
	want := searchMatchStyle.Render("searchable")
	if !strings.Contains(row, want) {
		t.Errorf("row = %q, want it to contain the highlighted subject match %q", row, want)
	}
}

// TestRenderCommitRowHighlightsShaMatch pins down that the visible sha
// prefix is search-highlighted like author/subject: list search also
// matches the commit hash (git.HashMatcher), unlike the detail view's
// in-commit search (see headerLinesFor's doc comment in detail.go).
func TestRenderCommitRowHighlightsShaMatch(t *testing.T) {
	c := fixtureCommitForRow()

	shaRow := renderCommitRow(c, 120, false, false, "abc", false)
	want := searchMatchStyle.Render("abc")
	if !strings.Contains(shaRow, want) {
		t.Errorf("row = %q, want it to contain the highlighted sha %q", shaRow, want)
	}
}

// TestRenderCommitRowNeverHighlightsDate pins down that date is rendered as
// an opaque, unsplit span regardless of searchTerm: it's never part of what
// list search actually searches (sha, author name, and commit message
// only), so highlighting it — even when its text happens to contain the
// term — would be misleading about what matched.
func TestRenderCommitRowNeverHighlightsDate(t *testing.T) {
	c := fixtureCommitForRow()

	dateRow := renderCommitRow(c, 120, false, false, "2024", false)
	if strings.Contains(dateRow, searchMatchStyle.Render("2024")) {
		t.Errorf("row = %q, date should never be search-highlighted", dateRow)
	}
}

func TestRenderCommitRowActiveMatchUsesDistinctStyleFromOrdinaryMatch(t *testing.T) {
	c := fixtureCommitForRow()
	ordinary := renderCommitRow(c, 120, false, false, "alice", false)
	active := renderCommitRow(c, 120, false, false, "alice", true)

	if strings.Contains(ordinary, searchActiveMatchStyle.Render("Alice")) {
		t.Error("ordinary match row unexpectedly used the active-match style")
	}
	if !strings.Contains(active, searchActiveMatchStyle.Render("Alice")) {
		t.Errorf("active match row = %q, want it styled with searchActiveMatchStyle", active)
	}
	if ordinary == active {
		t.Error("ordinary and active match rows should render differently")
	}
}

// TestRenderCommitRowHighlightsWhenSelected pins that the cursor row keeps its
// search highlighting: the match style brings its own background, so it stays
// visible over the cursor's, and the row the user is looking at is exactly the
// one where seeing which field matched is worth most.
func TestRenderCommitRowHighlightsWhenSelected(t *testing.T) {
	row := renderCommitRow(fixtureCommitForRow(), 120, true, false, "alice", false)
	if !strings.Contains(row, searchMatchStyle.Render("Alice")) {
		t.Errorf("selected row = %q, want the match rendered with searchMatchStyle", row)
	}

	active := renderCommitRow(fixtureCommitForRow(), 120, true, false, "alice", true)
	if !strings.Contains(active, searchActiveMatchStyle.Render("Alice")) {
		t.Errorf("selected active-match row = %q, want the match rendered with searchActiveMatchStyle", active)
	}
}

// TestRenderCommitRowDimsNonMatch pins down that dimmed replaces a row's
// normal per-field colors with dimStyle, the mechanism that lets a commit
// matched only via its message body (nothing visibly highlighted in the
// row) still read as "a match" by contrast with true non-matches.
func TestRenderCommitRowDimsNonMatch(t *testing.T) {
	c := fixtureCommitForRow()
	normal := renderCommitRow(c, 120, false, false, "", false)
	dimmed := renderCommitRow(c, 120, false, true, "", false)

	if normal == dimmed {
		t.Fatal("dimmed row should render differently from a normal row")
	}
	paddedAuthor := fmt.Sprintf("%-*s", authorColumnWidth, "Alice")
	if !strings.Contains(dimmed, dimStyle.Render(paddedAuthor)) {
		t.Errorf("dimmed row = %q, want the author rendered with dimStyle", dimmed)
	}
	if strings.Contains(dimmed, authorStyle.Render(paddedAuthor)) {
		t.Errorf("dimmed row = %q, should not still use authorStyle's normal color", dimmed)
	}
}

func TestRenderCommitRowSkipsDimmingWhenSelected(t *testing.T) {
	row := renderCommitRow(fixtureCommitForRow(), 120, true, true, "", false)
	paddedAuthor := fmt.Sprintf("%-*s", authorColumnWidth, "Alice")
	if strings.Contains(row, dimStyle.Render(paddedAuthor)) {
		t.Errorf("selected row = %q, should not apply dimming (selection style stays authoritative)", row)
	}
}

func TestRenderCommitListJoinsPlainRows(t *testing.T) {
	commits := []git.Commit{fixtureCommitForRow(), fixtureCommitForRow()}
	got := RenderCommitList(commits, 120)

	want := strings.Join([]string{
		strings.TrimRight(renderCommitRow(commits[0], 120, false, false, "", false), " "),
		strings.TrimRight(renderCommitRow(commits[1], 120, false, false, "", false), " "),
	}, "\n")
	if got != want {
		t.Errorf("RenderCommitList = %q, want %q", got, want)
	}
}

// TestRenderCommitListHasNoTrailingWhitespace pins that printed rows stop at
// their last visible character: padding out to the width they were rendered
// at would wrap every line the moment the window is narrower than that.
func TestRenderCommitListHasNoTrailingWhitespace(t *testing.T) {
	commits := []git.Commit{fixtureCommitForRow(), fixtureCommitForRow()}
	for i, line := range strings.Split(RenderCommitList(commits, 200), "\n") {
		if strings.TrimRight(line, " \t") != line {
			t.Errorf("line %d = %q, want no trailing whitespace", i, line)
		}
	}
}

// TestRenderCommitRowShortensShaTo7Chars pins down that the sha column
// always shows a 7-char abbreviation (git's own default abbreviated-hash
// length) regardless of how long the full hash is, at both narrow and wide
// pane widths — this change is unconditional, unlike the date/author ones.
func TestRenderCommitRowShortensShaTo7Chars(t *testing.T) {
	c := fixtureCommitForRow()
	c.Hash = "1234567890abcdef1234567890abcdef12345678"
	wantSha := shaStyle.Render("1234567")

	for _, width := range []int{80, 120} {
		row := renderCommitRow(c, width, false, false, "", false)
		if !strings.Contains(row, wantSha) {
			t.Errorf("width %d: row = %q, want the 7-char short sha %q", width, row, wantSha)
		}
		if strings.Contains(row, c.Hash) {
			t.Errorf("width %d: row = %q, should never contain the full 40-char hash", width, row)
		}
	}
}

// TestRenderCommitRowWideLayoutUsesSingleSpaceGaps pins down the exact
// wide-width row layout: single-space gaps around the date/clock block
// (down from the former two spaces) and around the author field, while the
// author->subject gap stays two spaces. Reconstructed field-by-field with
// the same helpers renderCommitRow itself uses, then compared through
// padRow so padding/truncation matches exactly.
func TestRenderCommitRowWideLayoutUsesSingleSpaceGaps(t *testing.T) {
	c := fixtureCommitForRow()
	width := 120
	row := renderCommitRow(c, width, false, false, "", false)

	localDate := c.AuthorDate.Local()
	sha := shaStyle.Render(fmt.Sprintf("%-*s", shaColumnWidth, shaPrefix(c.Hash)))
	date := dateStyle.Render(localDate.Format("2006-01-02"))
	clock := dateStyle.Render(localDate.Format("15:04"))
	author := authorStyle.Render(fmt.Sprintf("%-*s", authorColumnWidth, truncate(c.AuthorName, authorColumnWidth)))
	subject := normalRowStyle.Render(c.Subject)

	want := padRow(sha+" "+date+" "+clock+" "+author+"  "+subject, width, false)
	if row != want {
		t.Errorf("row  = %q\nwant = %q", row, want)
	}
}

// TestRenderCommitRowNarrowCollapsesDateAndTime pins down the narrow-width
// (<100 cols) date/clock collapse: today's commits show only the time,
// everything else shows only the date, padded to dateColumnWidth alone
// (not the wider two-field width) — that shrink is the actual point of
// collapsing the columns, so the subject gets the space back.
func TestRenderCommitRowNarrowCollapsesDateAndTime(t *testing.T) {
	width := 80

	t.Run("today shows time only", func(t *testing.T) {
		c := fixtureCommitForRow()
		c.AuthorDate = time.Now()
		row := renderCommitRow(c, width, false, false, "", false)

		localDate := c.AuthorDate.Local()
		sha := shaStyle.Render(fmt.Sprintf("%-*s", shaColumnWidth, shaPrefix(c.Hash)))
		when := dateStyle.Render(fmt.Sprintf("%-*s", dateColumnWidth, localDate.Format("15:04")))
		author := authorStyle.Render(fmt.Sprintf("%-*s", narrowAuthorColumnWidth, truncate(abbreviateName(c.AuthorName), narrowAuthorColumnWidth)))
		subject := normalRowStyle.Render(c.Subject)

		want := padRow(sha+" "+when+" "+author+"  "+subject, width, false)
		if row != want {
			t.Errorf("row  = %q\nwant = %q", row, want)
		}
		if strings.Contains(row, localDate.Format("2006-01-02")) {
			t.Errorf("row = %q, should not show the date for today's commit in narrow layout", row)
		}
	})

	t.Run("older commit shows date only", func(t *testing.T) {
		c := fixtureCommitForRow() // AuthorDate is 2024-01-02, never "today"
		row := renderCommitRow(c, width, false, false, "", false)

		localDate := c.AuthorDate.Local()
		sha := shaStyle.Render(fmt.Sprintf("%-*s", shaColumnWidth, shaPrefix(c.Hash)))
		when := dateStyle.Render(fmt.Sprintf("%-*s", dateColumnWidth, localDate.Format("2006-01-02")))
		author := authorStyle.Render(fmt.Sprintf("%-*s", narrowAuthorColumnWidth, truncate(abbreviateName(c.AuthorName), narrowAuthorColumnWidth)))
		subject := normalRowStyle.Render(c.Subject)

		want := padRow(sha+" "+when+" "+author+"  "+subject, width, false)
		if row != want {
			t.Errorf("row  = %q\nwant = %q", row, want)
		}
		if strings.Contains(row, localDate.Format("15:04")) {
			t.Errorf("row = %q, should not show the time for a non-today commit in narrow layout", row)
		}
	})
}

// TestRenderCommitRowAuthorAbbreviationIsWidthGated pins down that the
// per-word 3-char author abbreviation (and narrower author column) only
// kicks in below the narrow breakpoint — at wide widths the full name is
// shown at the normal column width.
func TestRenderCommitRowAuthorAbbreviationIsWidthGated(t *testing.T) {
	c := fixtureCommitForRow()
	c.AuthorName = "Ada Lovelace"

	narrowRow := renderCommitRow(c, 80, false, false, "", false)
	wideRow := renderCommitRow(c, 120, false, false, "", false)

	wantNarrowAuthor := authorStyle.Render(fmt.Sprintf("%-*s", narrowAuthorColumnWidth, "Ada Lov"))
	wantWideAuthor := authorStyle.Render(fmt.Sprintf("%-*s", authorColumnWidth, "Ada Lovelace"))

	if !strings.Contains(narrowRow, wantNarrowAuthor) {
		t.Errorf("narrow row = %q, want abbreviated author %q", narrowRow, wantNarrowAuthor)
	}
	if strings.Contains(narrowRow, "Lovelace") {
		t.Errorf("narrow row = %q, should not show the full author name", narrowRow)
	}
	if !strings.Contains(wideRow, wantWideAuthor) {
		t.Errorf("wide row = %q, want the full author name %q", wideRow, wantWideAuthor)
	}
}

// TestAbbreviateNameCapsAtTwoGroups pins down that abbreviateName keeps at
// most two 3-letter groups, marking a dropped remainder with a trailing
// "…" — this is what lets narrowAuthorColumnWidth shrink to 8 rather than
// needing to fit an arbitrary number of abbreviated words.
func TestAbbreviateNameCapsAtTwoGroups(t *testing.T) {
	cases := []struct{ name, want string }{
		{"Ada", "Ada"},
		{"Ada Lovelace", "Ada Lov"},
		{"Grace Brewster Murray Hopper", "Gra Bre…"},
	}
	for _, c := range cases {
		if got := abbreviateName(c.name); got != c.want {
			t.Errorf("abbreviateName(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestRenderCommitRowNarrowAuthorEllipsizesThreeOrMoreWords pins down that
// a name with more than two whitespace-delimited parts renders with the
// "…" marker (not just silently dropping the extra words) in the narrow
// list layout.
func TestRenderCommitRowNarrowAuthorEllipsizesThreeOrMoreWords(t *testing.T) {
	c := fixtureCommitForRow()
	c.AuthorName = "Grace Brewster Murray Hopper"

	row := renderCommitRow(c, 80, false, false, "", false)
	wantAuthor := authorStyle.Render(fmt.Sprintf("%-*s", narrowAuthorColumnWidth, "Gra Bre…"))
	if !strings.Contains(row, wantAuthor) {
		t.Errorf("row = %q, want the ellipsized abbreviated author %q", row, wantAuthor)
	}
	if strings.Contains(row, "Murray") || strings.Contains(row, "Hopper") {
		t.Errorf("row = %q, should not show the third/fourth name parts", row)
	}
}
