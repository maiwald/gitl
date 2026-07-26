# gitl

A terminal UI for browsing git history, with vim-style navigation.

## Features

- **Commit list** — scrollable log of commits showing date, author, ref
  decorations (branches/tags/HEAD), and subject line.
- **Commit detail** — full metadata and commit message, plus a two-pane
  diff view: a highlighted, scrollable diff on the left and the list of
  changed files on the right, kept in sync as you scroll.
- **Vim-style keys** throughout — `hjkl`/arrows, `ctrl-d`/`ctrl-u` half-page
  paging, `g`/`G` to jump to top/bottom, `Tab` to switch panes.
- **Search** — `/` to search, `n`/`N` to cycle matches (wrapping at both
  ends), with all matches highlighted and the current one visually
  distinct. In the commit list, search covers author name and the full
  commit message; in the commit detail view, it covers the diff content.
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

## Usage

Run it inside any git repository:

```sh
gitl
```

### Keybindings

| Key(s)                | Action                                  |
|------------------------|------------------------------------------|
| `j`/`k`, `↓`/`↑`        | Move down/up                            |
| `h`/`l`, `←`/`→`        | (reserved for pane focus in detail view) |
| `ctrl-d` / `ctrl-u`     | Half-page down/up                       |
| `g` / `G`               | Jump to top / bottom                    |
| `Enter`                 | Open commit detail / jump to file       |
| `Tab`                   | Switch focus between diff and file list |
| `/`                     | Search (author + message, or diff content) |
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
  the match total but has nothing visible to highlight in that row.
- Detail-view search covers diff content only, not the commit message.

## Development

```sh
go build ./...     # build everything
go vet ./...        # static checks
go test ./...        # run the test suite
go run .             # run against the repo you're standing in
```

See `CLAUDE.md` for more on the project's structure and conventions.
