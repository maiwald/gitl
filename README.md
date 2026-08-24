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
- **Rename detection** — a moved file shows as a rename (`old -> new`) with
  just the edits made along the way, instead of a full delete+add. Copy
  detection is deliberately not enabled: a newly added file that happens to
  resemble an existing one shows as added, with all of its content, rather
  than as a near-empty delta against a file it was never related to.
- **Vim-style keys** throughout — `hjkl`/arrows, `ctrl-d`/`ctrl-u` half-page
  paging, `g`/`G` to jump to top/bottom.
- **Search** — `/` to search, `n`/`N` to cycle matches (wrapping at both
  ends), with all matches highlighted and the current one visually
  distinct. One term is shared by both views: the commit list matches
  author name, full commit message and hash; an open commit matches its
  message (author line and body) and diff content, message matches first.
  Only the view you're in jumps to a match — the other highlights them, so
  `n` picks up where you left off when you get there. `Esc`/`q` hides the
  highlighting without forgetting the term; `n`/`N` brings it back.
  Commits that don't match are dimmed in the list, so a match whose only
  hit is in the message body (nothing visible to highlight in the row)
  still stands out by contrast with the surrounding non-matches.
- **Jumplist** — `ctrl-o`/`ctrl-i` step back and forward through the
  positions you jumped from, across both views: opening a commit, `g`/`G`,
  `[`/`]`, and every search jump are recorded, so `ctrl-o` from inside a
  commit returns to the list row you opened it from.
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
                   # ...); q/Esc quits outright, like --staged/--diff below.

gitl --staged      # open the staged-changes (index vs HEAD) detail view
                   # directly, no list involved — a drop-in replacement for
                   # `git diff --staged | less`: q/Esc quits outright.

gitl --diff        # open the working-tree-changes (index vs working tree)
                   # detail view directly, no list involved — a drop-in
                   # replacement for `git diff | less`: q/Esc quits outright.
```

Each of those takes paths, limiting the diff that opens to them just like
`git diff`/`git show` do:

```sh
gitl --diff internal/ui          # only this directory's unstaged changes
gitl --staged main.go args.go    # only these files' staged changes
gitl HEAD~3 internal/git         # only this directory's part of that commit
gitl HEAD~3 -- main.go           # -- separates paths from the revision
                                  # explicitly, needed for a path that could
                                  # be read as a revision (or as a flag)
```

Without `--`, the first argument is always the revision, even if a file by
that name exists — so a path-only invocation needs the separator, or one of
`--staged`/`--diff`. The paths apply to the diff that opens and nothing
else: the commit list behind it isn't filtered, so a commit opened from
there (or reached with `ctrl-o`) shows its full diff.

`--staged` and `--diff` are mutually exclusive with each other and with a
revision argument.

Run `gitl --help` (or `-h`) at any time for the full flag reference.

### Keybindings

| Key(s)                | Action                                  |
|------------------------|------------------------------------------|
| `j`/`k`, `↓`/`↑`        | Move down/up                            |
| `ctrl-d` / `ctrl-u`     | Half-page down/up                       |
| `g` / `G`               | Jump to top / bottom                    |
| `Enter`                 | Open commit detail; on a "Files changed" row, jump to that file's diff |
| `[` / `]`               | Jump to previous/next file's diff (detail view) |
| `/`                     | Search (list: author + message + hash; detail: message + diff) |
| `n` / `N`               | Next / previous search match (wraps)    |
| `ctrl-o` / `ctrl-i`     | Jump back / forward through the positions you left |
| `Esc` / `q`             | Hide the search, then back (detail view) / quit (list view) |
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
- Paths limit the startup diff only — there's no path-filtered commit list,
  and no way to change or drop the pathspec from inside the running UI.

## Development

```sh
go build ./...     # build everything
go vet ./...        # static checks
go test ./...        # run the test suite
go run .             # run against the repo you're standing in
```

See `CLAUDE.md` for more on the project's structure and conventions.
