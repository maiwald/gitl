# gitl — Project Guidance

## Overview

`gitl` is a terminal UI for browsing git history: a scrollable commit list
with vim-style navigation, and a commit detail view with a two-pane,
highlighted diff synced to a file list. Written in Go, using
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
main.go                    entry point: builds and runs the root tea.Program
internal/git/              data access layer, no TUI dependencies
  repo.go                  Repo, OpenRepo, HasCommits
  exec.go                  runGit: shells out, wraps stderr into errors
  log.go                   Commit type, FetchLogBatch, CommitCount (paged log)
  show.go                  CommitDetail, FetchCommitDetail (metadata + diff)
  diff.go                  File/Hunk/DiffLine, ParseUnifiedDiff (go-gitdiff wrapper)
internal/ui/               Bubble Tea models, no direct git shelling out
  app.go                   root Model: view routing (list <-> detail), async fetch commands
  keys.go                  shared vim KeyMap
  styles.go                lipgloss style definitions
  list.go, commititem.go   commit list: cursor, viewport, lazy pagination
  detail.go                commit detail: header + two-pane layout, focus/scroll sync
  diffview.go              RenderDiff (flatten+style diff), FileAt (scroll->file lookup)
  filelist.go              right-pane file list
```

`internal/git` and `internal/ui` are cleanly separated: the UI layer only
ever calls exported `git` package functions and never shells out directly,
and `internal/git` has no knowledge of Bubble Tea.

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
commits. Run the full build/vet/test gate before every commit — don't
commit red.

## Testing approach

- **`internal/git`**: tests spin up real, scripted git repos under
  `t.TempDir()` (see `initFixtureRepo` in `log_test.go`) and exercise the
  actual `git` binary — no mocking of git itself. Diff parsing
  (`diff_test.go`) is tested against canned unified-diff text instead,
  since that doesn't need a real repo.
- **`internal/ui`**: most model logic (cursor movement, pagination
  triggers, focus/scroll sync) is tested by calling `Update` directly on a
  model built from fixture data — no real terminal needed. A few tests use
  `github.com/charmbracelet/x/exp/teatest` for full quit/lifecycle
  behavior.
- **Manual/visual verification**: Bubble Tea programs need a real TTY, so
  piping input via plain shell redirection doesn't work for interactive
  checks. When you need to eyeball rendered output (colors, layout,
  scrolling), drive the built binary through a `pty.fork()`-based Python
  script (see conversation history / PR descriptions for examples) that
  answers the terminal's startup capability queries (OSC 11 background
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
  `FileRange`/`FileAt` line-index bookkeeping stays correct regardless of
  terminal width.

## Known limitations (intentional, not bugs)

- Merge commits show metadata/message only — no diff is fetched, since
  there's no single canonical diff for a merge (picking one, e.g.
  first-parent, is deferred).
- No language-aware syntax highlighting inside diffs, only diff-status
  coloring (add/delete/context).
