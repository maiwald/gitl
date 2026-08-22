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
                           parses -n / --staged / --diff / --version / a
                           positional revision arg
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
  worktree.go              EntryKind/WorktreeEntry, FetchWorktreeEntries/FetchWorktreeEntry
                           (staged + working-tree diffs, untracked files as synthesized
                           added-file diffs), HasStagedChanges/HasWorktreeChanges, EntryDetail
internal/ui/               Bubble Tea models, no direct git shelling out
  app.go                   root Model: view routing (list <-> detail), async fetch
                           commands, canonical search state + prompt overlay +
                           status bar, StartTarget dispatch (plain / --staged /
                           --diff / <rev>)
  keys.go                  shared vim KeyMap
  styles.go                lipgloss style definitions
  render.go                primitives every view renders through (clipping/padding
                           a row to width, compositing styled spans into a line,
                           cursor + search-match styling) plus the cursor/match
                           arithmetic both view models share
  list.go, commititem.go   commit list: cursor, viewport, lazy pagination, search
  worktreerow.go           renderWorktreeRow: the two synthetic leading rows
  entry.go                 entryTitle/entryDescription: display strings shared by
                           worktreerow.go and detail.go's pseudo-entry branch
  detail.go                commit detail: header + sticky file header, scroll sync, search
  diffview.go              RenderDiff (flatten+style diff), FindDiffMatches
                           (diff content search), intralineSpans/intralineDiff
                           (per-line word-diff highlighting within a hunk)
  statsview.go             statLinesFor: git-show---stat-style summary (per-file +N/-M,
                           totals line), spliced between message and diff in detail.go
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
- Diff line wrapping: each wrapped visual row is its own entry in the
  flattened `RenderDiff` output (not a single multi-line string), so
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
  is baked into content at `reflow()` time (threaded into `RenderDiff`/
  `renderHeaderLines`/`statLinesFor` as a line-space coordinate, the same
  way an active search match is), not spliced onto the rendered viewport
  afterward.
- Padding a cursor-highlighted row to full width needs *two* Render()
  passes to come out fully colored: the first (colorless) `Width()` wrap
  that produces word-wrapped rows already pads short ones with **plain**
  spaces, and re-wrapping that padded, already-ANSI'd string in a further
  `Style.Render()` call to try to recolor it doesn't work — a style
  wrapped around content that contains its own embedded reset only
  colors up to that reset, not past it (the same "no facility for
  splicing a style into an already-ANSI'd string" hazard as search-match
  highlighting, one layer removed). `renderDiffLine`/`renderHeaderLines`
  strip that plain padding back off (`strings.TrimRight(row, " ")`)
  before re-padding through `padRow` with the cursor background as its
  fill, so the fill becomes a fresh, independently rendered span instead
  of a doomed re-wrap. A single-`Style.Render()`-call site with
  `Width`+`Background` set on genuinely plain (not pre-ANSI'd) text —
  `renderFileHeader`/hunk headers via `cursorMerged`, `renderClipped` —
  doesn't have this problem and needs no such strip.
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
  **both views match against it**: the detail view synchronously (its whole
  commit is in memory), the list via the git queries `startListSearch`
  dispatches. Only the view that is *active* when `/` is submitted jumps to
  its first match (`DetailModel.jumpToFirstMatch`, `applyListMatches`'s
  `viewList` gate) — moving a cursor the user isn't looking at is the same
  surprise as landing mid-diff the moment a commit opens, and n/N reach
  those matches from either view anyway. A view with matches but none
  active reports `[0/N]`; `stepMatchPos` makes n take the first from there
  and N the last.
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
- The working-tree/staged rows and their diffs are fetched once at startup,
  like the rest of the app's data — there's no refresh key, so edits made
  in another terminal while gitl is running aren't reflected.
- Only the first `maxUntrackedDiffs` (200) untracked files get their own
  `git diff --no-index` call in the working-tree view; beyond that, an
  untracked file still appears (correctly counted, marked `StatusAdded`)
  but without its own hunks, so a repo with a huge untracked directory
  doesn't spawn hundreds of subprocesses.
- A search submitted from the detail view also runs the list's git queries
  (one term, both views), so searching inside a commit costs a `git log
  --grep`/`--author` pair even if the user never returns to the list. The
  hash order those queries need is fetched once per run and cached
  (`Model.allHashes`).
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
  Also, the cursor highlights a whole logical line's entire wrapped block
  when it lands on one that word-wrapped into multiple visual rows,
  rather than just the specific visual row — line-wrapping a single diff/
  message line to several rows is rare enough in practice not to be worth
  the bookkeeping a visual-row-exact cursor would need.
