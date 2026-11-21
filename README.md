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
  view. The status bar shows where you are, right-aligned the way vim does
  it: the file in view (`file 3/12`), the cursor's line (`L812/2040`), and
  `Top`/`42%`/`Bot`/`All`. It stays visible while you search.
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

### As git's pager

Everything beyond the commit list — a commit, a diff against any revision, a
path — is reached through git itself, with gitl as its pager:

```sh
git config --global pager.diff gitl
git config --global pager.show gitl
```

Then git's own commands render in gitl:

```sh
git show HEAD~2                  # a commit: message, stats and diff
git show <rev> -- internal/ui    # the same, limited to a path
git diff origin/main...          # everything your branch changed
git diff --cached                # the staged changes
git diff HEAD~5 -- '*.go'        # any shape git accepts
```

gitl parses none of that grammar itself, so every revision, range and
pathspec git understands works, including ones it has never heard of. Like
any pager, it shows that one screen: `q`/`Esc` quits.

Wire it to `pager.diff`/`pager.show` rather than `core.pager`: gitl renders a
patch, and has nothing to add to `git status` or `git branch -v`.

Piping works the same way, if you'd rather not change your config:

```sh
git show HEAD~2 | gitl
```

There are two other, non-interactive modes:

```sh
gitl -n 20        # print the 20 most recent commits and exit
gitl --version    # print the version and exit
```

Run `gitl --help` (or `-h`) at any time for the full flag reference.

### Keybindings

| Key(s)                | Action                                  |
|------------------------|------------------------------------------|
| `j`/`k`, `↓`/`↑`        | Move down/up                            |
| `ctrl-d` / `ctrl-u`     | Half-page down/up                       |
| `g` / `G`               | Jump to top / bottom                    |
| `Enter`                 | Open commit detail; on a "Files changed" row, jump to that file's diff |
| `[` / `]`               | Jump to previous/next file's diff (detail view) |
| `{` / `}`               | Jump to previous/next blank line (detail view) |
| `/`                     | Search (list: author + message + hash; detail: message + diff) |
| `n` / `N`               | Next / previous search match (wraps)    |
| `ctrl-o` / `ctrl-i`     | Jump back / forward through the positions you left |
| `V`                     | Start/end a linewise selection (detail view); any movement key extends it |
| `Enter` / `y`           | Copy the selection to the clipboard (while selecting) |
| `Esc` / `q`             | Undo one thing per press: cancel the selection, hide the search, then back (detail view) / quit (list view) |
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
- Copying a selection goes out as an OSC 52 escape sequence, so it reaches
  the clipboard of whichever machine your terminal is on — the right one
  over ssh — but only if the terminal allows clipboard writes. There's no
  reply to wait for, so gitl reports the copy as sent either way; a terminal
  with that turned off (or `tmux set-clipboard off`) drops it silently.
- The working-tree/staged rows are a snapshot taken at startup, like the
  rest of the app's data — there's no key to refresh them if you edit files
  in another terminal while gitl is running.
- Only the first 200 untracked files each get their own diffed entry;
  beyond that, remaining untracked files still show up (correctly counted
  and marked as added) but without their own content diffed.
- The commit list itself is never path-filtered, and there's no way to
  change what's on screen from inside the running UI — a different diff
  means a different git command.
- Rename detection through the pager follows your `diff.renames` setting,
  rather than the `-M` gitl passes on its own invocations.
- `git show --stat -p` shows git's stat block *and* gitl's; `git show --stat`
  alone emits no patch, so gitl shows git's text and builds no block of its
  own.

## Development

```sh
go build ./...     # build everything
go vet ./...        # static checks
go test ./...        # run the test suite
go run .             # run against the repo you're standing in
```

See `CLAUDE.md` for more on the project's structure and conventions.
