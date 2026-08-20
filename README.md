# gitl

A terminal UI for browsing git history, with vim-style navigation.

## Features

- **Commit list** — scrollable log of commits showing date, author, ref
  decorations (branches/tags/HEAD), and subject line.
- **Working tree and staged changes** — shown as two rows above the commit
  list (working tree first, then staged; either is hidden when there's
  nothing to show), openable just like a commit. The working-tree row
  includes untracked files as synthesized added-file diffs, so it covers
  everything `git status` would flag as new, not just tracked changes.
- **Commit detail** — full metadata and commit message, plus a highlighted,
  scrollable diff with a sticky header showing which file is currently in
  view.
- **Vim-style keys** throughout — `hjkl`/arrows, `ctrl-d`/`ctrl-u` half-page
  paging, `g`/`G` to jump to top/bottom.
- **Search** — `/` to search, `n`/`N` to cycle matches (wrapping at both
  ends), with all matches highlighted and the current one visually
  distinct. In the commit list, search covers author name and the full
  commit message; in the commit detail view, it covers the commit
  message (author line and body) and the diff content, message matches
  first. Commits that don't match a search are dimmed in the list, so a
  match whose only hit is in the message body (nothing visible to
  highlight in the row) still stands out by contrast with the
  surrounding non-matches.
- Handles large repositories: commits are fetched in lazily-loaded batches
  rather than all at once, so startup and memory use don't scale with the
  size of the history. Searching the list only loads as much history as a
  match actually requires, rather than the whole log up front.

## Installation

Requires Go 1.25+ and `git` on your `PATH`.

```sh
go install github.com/maiwald/gitl@latest
```

Or build from source:

```sh
git clone https://github.com/maiwald/gitl.git
cd gitl
go build -o gitl .
```

Check what's installed with `gitl --version`.

## Usage

Run it inside any git repository:

```sh
gitl
```

It also accepts a startup target, jumping straight to a detail view instead
of the list:

```sh
gitl <rev>        # open a specific commit's detail view directly (any
                   # git-resolvable revision — a sha, branch, tag, HEAD~2,
                   # ...); q/Esc quits outright, like --staged below.

gitl --staged      # open the staged-changes (index vs HEAD) detail view
                   # directly, no list involved — a drop-in replacement for
                   # `git diff --staged | less`: q/Esc quits outright.
```

`--staged` and a revision argument are mutually exclusive.

Run `gitl --help` (or `-h`) at any time for the full flag reference.

### Keybindings

| Key(s)                | Action                                  |
|------------------------|------------------------------------------|
| `j`/`k`, `↓`/`↑`        | Move down/up                            |
| `ctrl-d` / `ctrl-u`     | Half-page down/up                       |
| `g` / `G`               | Jump to top / bottom                    |
| `Enter`                 | Open commit detail                      |
| `[` / `]`               | Jump to previous/next file's diff (detail view) |
| `/`                     | Search (list: author + message; detail: message + diff) |
| `n` / `N`               | Next / previous search match (wraps)    |
| `Esc` / `q`             | Back (detail view) / quit (list view)   |
| `ctrl-c`                | Quit                                     |

## Known limitations

- Merge commits show metadata and message only; their diff isn't rendered
  yet (there's no single canonical diff for a merge, and picking one is
  deferred).
- Diff content doesn't have language-aware syntax highlighting, only
  add/delete/context coloring.
- In the commit list, a commit that matches search only via its full
  message body (not the one-line subject shown in the row) counts toward
  the match total but has nothing visible to highlight in that row —
  distinguishable from a true non-match only by not being dimmed.
- The working-tree/staged rows are a snapshot taken at startup, like the
  rest of the app's data — there's no key to refresh them if you edit files
  in another terminal while gitl is running.
- Only the first 200 untracked files each get their own diffed entry;
  beyond that, remaining untracked files still show up (correctly counted
  and marked as added) but without their own content diffed.
- `gitl <rev>` for a revision that isn't reachable from HEAD (e.g. a commit
  that only exists on another branch) still opens its detail view, but the
  list can't sensibly position a cursor on it, so it's left at the top.

## Development

```sh
go build ./...     # build everything
go vet ./...        # static checks
go test ./...        # run the test suite
go run .             # run against the repo you're standing in
```

See `CLAUDE.md` for more on the project's structure and conventions.
