# gitl — Project Guidance

## Overview

`gitl` is a terminal UI for browsing git history: a scrollable commit list
with vim-style navigation, and a commit detail view with a highlighted,
scrollable diff and a sticky file header that tracks scroll position.
Written in Go, using
[Bubble Tea](https://github.com/charmbracelet/bubbletea) for the TUI
runtime, [Lip Gloss](https://github.com/charmbracelet/lipgloss) for
styling, and [go-gitdiff](https://github.com/bluekeyes/go-gitdiff) for
unified-diff parsing. Git data comes from shelling out to the `git` CLI
(`os/exec`), not a Go git library — this keeps log/diff formatting exactly
matching real git output and avoids the performance cost of a pure-Go
packfile implementation.

Module path: `github.com/maiwald/gitl`. Requires Go 1.25+ and `git` on
`PATH`.

## Project layout

```
main.go                    entry point: builds and runs the root tea.Program,
                           plus the -n printer and the --staged/--diff
                           pre-flight
args.go                    command-line parsing: flags, the revision/pathspec
                           split (including git's "--" separator) and every
                           combination rule, as a unit-testable options value
internal/concurrent/       RunConcurrently: generic "run these fns in parallel,
                           return the first error in call order" helper, with no
                           git- or TUI-specific logic, shared by both packages below
internal/git/              data access layer, no TUI dependencies
  repo.go                  Repo, OpenRepo, HasCommits
  exec.go                  runGit: shells out, wraps stderr into errors
  log.go                   Commit type, FetchLogBatch, CommitCount (paged log)
  show.go                  CommitDetail (+ EntryKind), FetchCommitDetail (metadata + diff)
  diff.go                  File/Hunk/DiffLine, ParseUnifiedDiff (go-gitdiff wrapper)
  search.go                SearchCommitHashes (author+message, OR), AllCommitHashes,
                           HashesMatching (in-memory hash-substring filter, list search only)
  rev.go                   ResolveRevision, CommitPosition (CLI revision jump support)
  worktree.go              EntryKind/WorktreeEntry, EntrySummary/FetchWorktreeSummaries
                           (numstat counts for the list's leading rows),
                           FetchWorktreeEntry (the diff behind one, on open: staged +
                           working-tree diffs, untracked files as synthesized
                           added-file diffs), HasStagedChanges/HasWorktreeChanges, EntryDetail
internal/ui/               Bubble Tea models, no direct git shelling out
  app.go                   root Model: view routing (list <-> detail), async fetch
                           commands, canonical search state + prompt overlay +
                           status bar, StartTarget dispatch (plain / --staged /
                           --diff / <rev>, each optionally pathspec-limited)
  keys.go                  shared vim KeyMap
  styles.go                lipgloss style definitions
  viewport.go              rowViewport: the scrolling window both views display
                           their rendered rows through
  render.go                primitives every view renders through (clipping/padding
                           a row to width, compositing styled spans into a line,
                           cursor + search-match styling), the lineRenderer/
                           renderRows layout the detail view's blocks are built
                           from, plus the cursor/match arithmetic both view
                           models share
  list.go, commititem.go   commit list: cursor, viewport, lazy pagination, search
  worktreerow.go           renderWorktreeRow: the two synthetic leading rows
  entry.go                 entryTitle/entryDescription: display strings shared by
                           worktreerow.go and detail.go's pseudo-entry branch
  detail.go                commit detail: header + sticky file header, scroll sync, search
  diffview.go              RenderDiff (flatten+style diff), FindDiffMatches
                           (diff content search), intralineSpans/intralineDiff
                           (per-line word-diff highlighting within a hunk)
  statsview.go             statLinesFor: git-show---stat-style summary (per-file +N/-M,
                           totals line), spliced between message and diff in detail.go,
                           plus FindStatMatches (search over its file rows)
```

`internal/git` and `internal/ui` are cleanly separated: the UI layer only
ever calls exported `git` package functions and never shells out directly,
and `internal/git` has no knowledge of Bubble Tea. `internal/concurrent`
exists so a fully generic helper isn't homed in one of those two packages
just because it happened to be needed there first — if it has no git- or
TUI-specific logic, it belongs there rather than in `internal/git` or
`internal/ui`, and rather than a general-purpose `util`-style package that
would invite unrelated helpers to accumulate in it over time.

## Development workflow

```sh
go build ./...      # build
go vet ./...         # static checks
go test ./...         # unit tests
go run .              # run against the repo you're standing in (dogfooding)
gofmt -l -w .         # format (run before every commit)
```

Commit style used in this repo: one commit per coherent feature/stage
(see git log for the incremental history — app skeleton, then git data
layer, then list rendering, then navigation, then detail view, then diff
parsing, then two-pane layout, then scroll sync, then polish). Prefer
continuing that granularity for new work rather than large bundled
commits. This applies to refactoring/cleanup passes too, not just new
features: a pass that touches several unrelated spots (e.g. a dedup here,
an efficiency fix there, an extracted helper elsewhere) should land as one
commit per independent change, not one bundled commit — even though
they're discovered and applied together. Run the full build/vet/test gate
before every commit — don't commit red.

## Testing approach

- **`internal/git`**: tests spin up real, scripted git repos under
  `t.TempDir()` (see `initFixtureRepo` in `log_test.go`) and exercise the
  actual `git` binary — no mocking of git itself. Diff parsing
  (`diff_test.go`) is tested against canned unified-diff text instead,
  since that doesn't need a real repo.
- **`internal/ui`**: most model logic (cursor movement, pagination
  triggers, scroll sync) is tested by calling `Update` directly on a
  model built from fixture data — no real terminal needed. A few tests use
  `github.com/charmbracelet/x/exp/teatest` for full quit/lifecycle
  behavior.
- **Manual/visual verification**: Bubble Tea programs need a real TTY, so
  piping input via plain shell redirection doesn't work for interactive
  checks. When you need to eyeball rendered output (colors, layout,
  scrolling), drive the built binary through a `pty.fork()`-based Python
  script (see conversation history / PR descriptions for examples) that
  sets the window size via `TIOCSWINSZ` and answers the terminal's startup
  capability queries (OSC 11 background
  color, cursor position report) before sending keys — without a reply to
  those queries, Bubble Tea's terminal detection can stall waiting for a
  response that never comes.

## Conventions

- No comments except where they explain non-obvious *why* (a git quirk, a
  workaround, an invariant) — not what the code does.
- Prefer plain functions and small structs over interfaces/abstractions
  until a second concrete need actually appears.
- Field/record parsing from `git log`/`git diff` output uses the ASCII
  unit separator (`\x1f`) as a delimiter precisely because none of the
  fields requested can contain it — don't switch to a delimiter that could
  collide with commit messages or paths.
- Diff line wrapping: each wrapped visual row is its own entry in the rows
  `RenderDiff`'s lineRenderers produce (not a single multi-line string), so
  `FileRange` line-index bookkeeping stays correct regardless of terminal
  width.
- Search-match highlighting always styles plain-text spans *before* any
  `.Render()` call touches them, then concatenates the already-rendered
  pieces (see `highlightSpans` and `renderLineWithBaseSpans` in
  `render.go`) — this codebase has no facility for splicing a style
  into an already-ANSI'd string, and doesn't need one as long as styling
  decisions are made on the underlying plain text first.
- List search loads only as much history as the relevant match needs
  (`RequestLoadUpTo` in `list.go`), not the whole remaining log — matching
  which commits a search matches is cheap (`git log --grep`/`--author`,
  independent of what's loaded), but jumping to a match still has to load
  up to it, reusing the same lazy-pagination mechanism scrolling uses.
- List rows outside the active search's real match set (`ListModel.matches`,
  from the actual `git log --grep`/`--author` query — not from
  re-scanning a row's own visible text) are dimmed via `dimStyle`
  (`commititem.go`), applied per-field rather than as one outer style
  wrapped around the whole row — see `rowFieldStyle`'s doc comment
  (`commititem.go`) for why: every field is independently `.Render()`'d,
  and an outer style would get cancelled by the first inner ANSI reset.
- `ListModel.cursor` stays an index into `commits` and goes **negative** to
  address the synthetic leading rows (`leading[i]` ⟺
  `cursor == i-len(leading)`, see the field's doc comment in `list.go`).
  This is deliberate: every other index computation in that file (`matches`,
  `fetchRequest` skip/limit, `totalCount` comparisons, `maybeLoadMore`'s
  lookahead check) is keyed to real positions in HEAD's history and stays
  completely unaware synthetic rows exist — don't "fix" the negative
  values by switching to a combined row-space index; that would require
  touching all of those instead of none of them. The cursor always stays
  within `[topIndex(), lastIndex()]`, the rows that actually exist:
  `lastIndex()` floors `len(commits)-1` at `topIndex()` so a list with
  nothing loaded yet (every run's first frames, and the whole of a
  `--staged`/`--diff` run) can't put the cursor on a row that isn't there.
- A synthetic leading row (working tree/staged) is represented as a
  `git.CommitDetail` with `Kind != EntryCommit` and the embedded `Commit`
  left zero-valued (`IsPseudo()`). `detail.go`'s diff/stats/sections/search
  machinery all operate on `[]git.File` and need no knowledge of this; only
  `headerLinesFor`, `noDiffNotice`, and `renderStickyHeader` branch on it.
- `ListModel` caches its rendered rows (`renderedRows`, `list.go`) and
  patches just the cursor's old/new row on a pure cursor move
  (`setCursor`/`renderCursorMoved`) instead of re-rendering everything.
  Any change to *what* a row shows, not just which row is the cursor (a
  new field on `Commit`, a new dim/highlight condition, appended commits,
  an updated `matches` set) must go through `render()`'s full rebuild —
  otherwise the cache silently keeps showing stale content.
- Both views display their rows through `rowViewport` (`viewport.go`), not
  `bubbles/viewport`, and that is the whole point of the row caches above.
  bubbles' viewport takes its content as one string: every `SetContent`
  re-split it and then measured every line with `ansi.StringWidth` to size a
  horizontal scroll this app never uses — putting the entire commit, or the
  entire loaded history, back onto the cost of a single cursor move (~90% of
  a keypress, megabytes of garbage per keystroke). Bubble Tea runs a full
  `Update` *and* `View` per message with no key coalescing, so anything
  per-keypress that scales with content shows up as autorepeat piling into
  the tty buffer and draining after the key is released. `SetRows` therefore
  **borrows** the caller's slice rather than copying it — which is also why a
  row patched in place needs no handoff, while a `render()`/`reflow()` that
  builds a fresh slice does. `View()` emits rows verbatim, padding only the
  row *count* out to `Height` (the status bar sits directly below it): every
  row is already exactly one display line at full width (`padRow`), and the
  ones that aren't are blank by construction, where trailing spaces and an
  empty line are the same picture once the terminal erases to end of line.
- An async fetch's own in-flight guard (`ListModel.loadingMore`,
  `Model.searchingTerm`) is cleared by that fetch's own message type
  (`moreCommitsFailedMsg`, `searchFailedMsg` in `app.go`), never by the
  generic `errMsg` — an unrelated later failure must not unlatch a guard
  some other in-flight fetch still owns, nor leave one latched that no
  message will ever clear. Any new async command with its own in-flight
  state should follow this pattern. A deferred navigation waiting on such
  a fetch (`ListModel.pendingCursor`) has to be re-offered when the fetch
  that blocked it lands (`moreCommitsLoadedMsg`), since `RequestLoadUpTo`
  refuses while another fetch is in flight and nothing else would ask
  again.
- `DetailModel.cursorLine` is a tig-style line cursor (index into the same
  flattened line space `leftVP`'s scroll offset lives in), decoupled from
  `leftVP.YOffset` the same way `ListModel.cursor` is decoupled from
  `vp.YOffset` — movement moves the cursor, then the viewport follows it
  (`setCursor`'s `ensureCursorVisible`), never the reverse. Its highlight
  is part of the rendered content, not something spliced onto the viewport
  afterward — but it's applied by `renderRows` (`render.go`) once the lines
  have been laid out, not threaded into the blocks that produce them, which
  is what lets a cursor move repaint just the two lines that changed.
- A `reflow()` is the detail view's only way to change *what its lines say*,
  so every writer of that (`SetSearchDisplay`, `SetSize`, `setMatchPos`)
  ends in one. Which line the cursor is on is the sole exception: it goes
  through `repaintCursor`, which re-renders only the line being left and the
  line being taken up and splices them into the cached rows. That invariant
  is what lets `jumpToMatch` read `messageMatchLines`/`diffMatchLines` out of
  the *previous* reflow instead of forcing a fresh one first: those depend
  only on the match set and the wrap width, both of which already ended in a
  reflow when they changed. The restyle a match step needs is `landOnLine`'s
  own repaint, which has to run regardless to move the cursor highlight onto
  the line it lands on — so a reflow before it is pure duplicated work, and
  on a large commit that's a whole re-render of every diff line per `n`/`N`
  press.
- A block of detail-view content (`renderHeaderLines`, `statLinesFor`,
  `RenderDiff`) returns `[]lineRenderer` (`render.go`) — one closure per
  *logical* line, each able to render itself plain or with the cursor row's
  background — and knows nothing about where its lines will land or which
  one the cursor is on. `reflow` concatenates the blocks and `renderRows`
  lays them out, returning the flattened rows plus the `lineStart` index and
  applying the cursor to whichever line contains it. So the line indices a
  block reports (`FileRange`s, match lines) are in *its own* logical lines,
  counted from its own origin, and `reflow` rebases each one onto a row via
  `rowOf` — anything new that a block reports has to be rebased there too, or
  it will silently point into a different block.
- `DetailModel` keeps the `lines` it last assembled next to the `content`
  they rendered into, and `repaintCursor` patches the two logical lines whose
  highlight changed rather than reflowing (`ListModel.renderedRows` /
  `renderCursorMoved` is the same bargain, with the same obligation: a change
  to what a line *says* must go through `reflow`, which rebuilds all three of
  `lines`/`content`/`lineStart` together). The splice depends on a line
  rendering to the same number of rows with and without the cursor
  background — that's the wrap-on-plain-text invariant below, and
  `TestDetailModelCursorPatchMatchesFullReflow` pins the whole arrangement by
  comparing patched content against a full reflow after every kind of jump.
  `BenchmarkDetailCursorMove`/`BenchmarkDetailReflow` are the reason it
  exists (~11 µs against ~30 ms on an 8000-line diff); if they ever
  converge, something has started reflowing on every keypress. They only
  compare a cursor move against a reflow of the *same* commit, though, so
  they cannot see work that merely *re-reads* the content —
  `TestDetailCursorMoveCostIsIndependentOfCommitSize` and its list
  counterpart are the tripwire for that, comparing bytes allocated per
  keypress across two fixture sizes (bytes, not allocation count: the
  regression they were written for moved a megabyte per keystroke in a
  constant 77 allocations).
- `wrapRanges` and `padRow` (`render.go`) each skip a lipgloss `Render` on
  the common path — a line that already fits isn't wrapped, and a plain
  (non-cursor) fill neither truncates nor styles its padding. Those are
  claims about lipgloss's behavior rather than ours, and they're on the
  hottest path in the app (a reflow pays both once per line of the whole
  commit), so `render_test.go` compares each fast path against what lipgloss
  itself produces: an upgrade that changes either fails there instead of
  silently changing every rendered row.
- Word-wrapping happens on **plain text**, before any styling
  (`wrapRanges`/`wrapStyledRows` in `render.go`, used by `renderDiffLine`
  and `renderHeaderLines`): the wrap is computed as byte ranges into the
  unstyled line, and each resulting row is styled and padded separately
  (spans/matches clipped and rebased onto it). This is an invariant, not a
  preference — lipgloss's wrapper (`cellbuf.Wrap`) only treats a space as a
  word boundary while the style in effect has *no background*, so wrapping a
  line that already carries the cursor row's background turns it into one
  unbreakable word and lands it on a different number of rows than the same
  line without it. Since `renderRows` renders each line plain to learn its
  row count and then overwrites one of them in place with its cursor
  rendering, a mismatch there would silently shift every line index below it
  (`FileRange`s, match positions, `cursorLine`'s own bound) — and
  `repaintCursor` patches lines into the cached rows on that same
  assumption. Wrapping first
  also removes the need to strip lipgloss's plain padding back off before
  re-padding: nothing is padded until after styling.
- Every entry in the flattened content is exactly **one display line**.
  `renderClipped` sets `Inline(true)` alongside `Width`/`MaxWidth` for
  precisely this reason: `Width` alone word-wraps, so an over-long hunk
  header (git puts the enclosing function on it) or file path would come
  back as two display lines inside what the model counts as one, and the
  viewport would then scroll further than the cursor can reach. Anything new
  that produces a content row must truncate, never wrap — or return its rows
  separately, the way `wrapStyledRows` does.
- The detail cursor moves by **logical line, not visual row**: `reflow`
  records `DetailModel.lineStart` (the row each logical line begins at, from
  `renderRows`; `logicalAt` searches it backwards, from a row to the line
  containing it), `setCursor` snaps whatever it's given back to `blockStart`,
  and downward motion is floored at `blockEnd`.
  The highlight covers a wrapped line's whole block, so stepping row by row
  through it would make presses look like dead keys.
- A CLI pathspec (`gitl --diff <path>...`, `gitl <rev> [--] <path>...`)
  reaches git as its own `-- <path>...` tail (`pathspecArgs` in
  `internal/git/diff.go`) and is scoped to the **startup target only**
  (`StartTarget.Paths`): `fetchDetailCmd`'s `paths` is passed exclusively by
  the startup dispatch, never by Enter on a list row — including Enter on a
  synthetic row, whose `FetchWorktreeEntry` is called with no paths — and
  `FetchWorktreeSummaries` (the list's leading rows) is unfiltered. The commit
  list can't be path-filtered at all, so filtering anything reached *through*
  it would answer a question the list never asked. The pathspec is carried on
  `CommitDetail.Paths`/`WorktreeEntry.Paths` for one purpose only — letting
  `noDiffNotice` (`detail.go`) say *which* paths came up empty; nothing
  re-runs a fetch from it.
- The non-interactive CLI paths say nothing when there's nothing to show:
  `--staged`/`--diff` with an empty index/worktree (`hasChanges`) and `-n` in
  a repo with no commits (`printCommits`) both just return, exit 0, and print
  no explanation — mirroring `git diff`, which prints nothing and succeeds.
  Only failures are printed. The TUI is the exception, and deliberately so:
  `noDiffNotice` and `app.go`'s "no commits yet" error are what a screen that
  would otherwise be blank shows instead.
- Command-line parsing lives in `args.go`, not `main.go`, and takes its
  `*flag.FlagSet` as a parameter so tests can parse a literal argv quietly
  (`args_test.go`) while main installs usage/error reporting on it. The
  `--` separator is split off (`splitPathspecs`) *before* `fs.Parse`,
  because Go's flag package consumes a leading `--` itself — without the
  pre-split, `gitl -- foo.go` would be indistinguishable from `gitl foo.go`,
  where the argument means a revision instead. `flag.ContinueOnError` (not
  `ExitOnError`) is what lets a bad flag and a bad argument *combination*
  exit through the same path in main; `errFlagParse` marks the failures the
  FlagSet has already printed itself, so they aren't reported twice.
- `Model` (`app.go`) owns a single cross-view jumplist (`jl *jumplist`,
  vim's ctrl-o/ctrl-i): a two-stack, browser-back/forward shape capped at
  `jumplistCap`, holding tagged `jumpPos` entries (a list cursor position,
  or a `*git.CommitDetail` plus a `cursorLine` within it). `ListModel` and
  `DetailModel` each hold a pointer to the same shared `jl` and only ever
  *push* onto it (`pushJump`, before `g`/`G`, `[`/`]` (detail view only),
  and every search-match jump — the *initial* `/` jump and every later
  `n`/`N` step, unlike vim, which only jumplists the initial search, since
  a single step here can jump much farther than vim's local from-cursor
  re-search); actual JumpBack/JumpForward traversal (`doJumpBack`/
  `doJumpForward`) lives solely on `Model`, since popping an entry can
  require switching the active view — something neither child model can
  do on its own. Opening a commit/entry (Enter) is itself pushed as a jump
  (in `handleListViewKey`, before the view switches), so ctrl-o from a
  freshly opened detail view returns to the list position it was opened
  from, not just to a seeded position within that one commit's diff — this
  is what makes ctrl-o/ctrl-i correctly cross the list/detail boundary
  (previously two independent per-view jumplists couldn't). `jl` is seeded
  with a single entry at the list's position 0 in `NewModel`, so ctrl-o can
  always reach the very top even before any real jump has happened — but
  only when the run actually has a list (`StartTarget.opensList`), since
  `--staged`/`--diff` never load one and ctrl-o would otherwise strand the
  user on a permanently empty list view.
  `ctrl+i` (not the literal string `"ctrl+i"`) is bound to bubbletea's
  `"tab"` key string — the two are the same byte (0x09) at the terminal
  protocol level, and bubbletea's own decoder reports it as tab (see
  `KeyTab`'s doc comment in bubbletea's `key.go`). Jumping back/forward
  into a detail view that isn't the currently active one reconstructs it
  from the entry's stored `*git.CommitDetail` (no re-fetch) via
  `gotoJumpPos`/`newDetailModel`, which installs the search term the same
  non-jumping way opening a commit does — a jump to the first match would
  fight the `cursorLine` this restore is specifically trying to land on.
- There is **one search term**, owned by the root `Model` (`searchTerm`/
  `searchDismissed`/`listMatches`, written only by `setSearchDisplay`), and
  **both views answer to it** — but each is searched only when its answer is
  actually needed. The detail view resolves synchronously (its whole commit
  is in memory). The list's half is two repo-wide `git log` scans, so it
  waits for `ensureListSearch`: the list becoming visible with the search
  still showing, or n/N reviving it there. A search that stays inside one
  commit never pays for it, and neither does the usual way out — the first
  q/esc dismisses the search, so the second arrives at a list that displays
  no search at all.
- What a deferred list search does when it lands is decided when it's
  *started* (`Model.jumpOnResolve`), never read off the active view when it
  resolves: a search started merely to light up the list's rows must not
  also move its cursor, while one started by `/` from the list, or by n/N,
  lands on a match (`jumpToFirstMatch`/`jumpToLastMatch` — n takes the first
  match ahead of the cursor, N the last one behind it). The jump isn't
  consumed until there are commits to land it on, since a search can resolve
  before the first batch of them does. A view with matches but none active
  reports `[0/N]`; `stepMatchPos` makes n take the first from there and N
  the last.
- The detail view searches three blocks — the header/message, the stats
  block, and the diff — and concatenates their matches, in that on-screen
  order, into the single sequence `matchPos` indexes
  (`matchBlock`/`matchSegment` in `detail.go`), so n/N sweeps a commit top to
  bottom. A file name is therefore found once per place it is *displayed*:
  once in its stats row and again in its diff file header. Only rows that
  name something the user searched for participate — the stats block's
  "Files changed" heading and its "N files changed" totals row are not
  searched, the same way `FindDiffMatches` skips hunk headers. Adding a
  fourth block means extending `matchBlock`, `totalMatches`, `matchSegment`,
  `jumpToMatch` and `reflow`'s match-line rebasing together; a block that
  reports match lines but isn't rebased in `reflow` will silently point into
  another block.
- A match set must never outlive the term it was computed for. Each view
  keeps its own `displayTerm` next to its matches and drops (`ListModel`,
  whose matches take a git query) or recomputes (`DetailModel`) them the
  moment `SetSearchDisplay` hands it a different term — including the empty
  term, which is how clearing a search is expressed, so neither view needs
  a separate `ClearSearch`. Same rule one level up: changing the canonical
  term retires `listMatches` and forgets `searchingTerm`, so an in-flight
  search's results are discarded when they land.
- The list model is constructed in `NewModel`, not left a zero value until
  `commitsLoadedMsg` — keys reach it from the first frame, and a zero
  `ListModel` has no shared jumplist to push onto. `commitsLoadedMsg` still
  replaces it wholesale, so anything the root `Model` holds canonically for
  it (search display state, `listMatches`) must be re-applied there;
  anything that *isn't* held canonically is silently lost by that swap.

## Known limitations (intentional, not bugs)

- Merge commits show metadata/message only — no diff is fetched, since
  there's no single canonical diff for a merge (picking one, e.g.
  first-parent, is deferred).
- No language-aware syntax highlighting inside diffs, only diff-status
  coloring (add/delete/context).
- List search matching a commit's message body (not its one-line subject),
  or matching the commit hash beyond the visible `shaColumnWidth`-character
  prefix, has nothing to visibly highlight in that row, since the row
  doesn't show the full body or the full hash — it still counts correctly
  toward the match total, and is distinguishable from a true non-match by
  not being dimmed.
- List search matches the commit hash (`git.HashesMatching`), but the
  detail view's in-commit search deliberately does not — see
  `headerLinesFor`'s doc comment in `detail.go`. Searching for a commit's
  own hash while already viewing that commit isn't useful; the hash field
  is only a search target when picking a commit out of the list.
- The working-tree/staged rows' counts are fetched once at startup and their
  diffs once each on open, like the rest of the app's data — there's no
  refresh key, so edits made in another terminal while gitl is running aren't
  reflected.
- A leading row's counts and the stats shown by opening it come from different
  git invocations, and are allowed to disagree in one place — pinned by a test
  in `worktree_test.go`, since restoring the agreement means putting the cost
  this split removed back on every startup. Only the first `maxUntrackedDiffs`
  (200) untracked files get their own `git diff --no-index` call, so beyond
  that an untracked file appears in the opened diff marked `StatusAdded` but
  without hunks (and contributing no lines to that view's stats), while the
  row's totals, which spawn no subprocess to count, include every one of
  them.
- Returning to the list with a search still showing can leave it briefly
  reporting `searching…` while the queries it deferred run — the cost of not
  running them for every search made inside a commit. The hash order they
  need is fetched once per run and cached (`Model.allHashes`).
- A CLI pathspec limits the startup diff only: there's no path-filtered
  commit list, and no way to change or drop the pathspec from inside the
  running UI. With no `--`, the first non-flag argument is always read as a
  revision even when a file by that name exists (git guesses between the two;
  gitl doesn't, since a path-only invocation has no list mode to fall back
  to) — `--`, `--staged`, or `--diff` is how a bare path is expressed.
- `gitl <rev>` for a revision not reachable from HEAD still opens its
  detail view (via `FetchCommitDetail`, which accepts any git-resolvable
  revision), but `CommitPosition` has no list position to offer, so the
  list is simply left at the top rather than erroring.
- `DetailModel.cursorLine` and jumplist entries are raw indices into the
  flattened, already-word-wrapped line space, which shifts on a terminal
  resize (rewrapping changes how many flattened lines the same content
  occupies). A resize re-clamps `cursorLine` back in range so it's never
  dangling, but doesn't try to preserve *which* logical line it was
  pointing at — same for any jumplist entry recorded before the resize.
  (A restored index is at least snapped back to a logical line's start, see
  `setCursor`, so it can't land mid-block.) The cursor also highlights a
  wrapped line's whole block rather than the one visual row within it —
  deliberate, and what makes moving by logical line the natural step.
