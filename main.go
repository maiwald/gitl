package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	xterm "github.com/charmbracelet/x/term"
	"github.com/maiwald/gitl/internal/git"
	"github.com/maiwald/gitl/internal/ui"
	"github.com/mattn/go-isatty"
)

// fallbackListWidth is used when stdout isn't a real terminal (piped or
// redirected), so -n output stays stable/scriptable instead of depending
// on whatever width a non-TTY happens to report.
const fallbackListWidth = 120

func main() {
	flag.Usage = printUsage
	n := flag.Int("n", 0, "print the N most recent commits non-interactively and exit")
	staged := flag.Bool("staged", false, "open the staged changes (index vs HEAD) directly; q exits")
	flag.Parse()

	args := flag.Args()
	if len(args) > 1 {
		fmt.Fprintln(os.Stderr, "error: expected at most one revision argument")
		os.Exit(2)
	}
	var rev string
	if len(args) == 1 {
		rev = args[0]
	}

	if *staged && rev != "" {
		fmt.Fprintln(os.Stderr, "error: --staged and a revision argument are mutually exclusive")
		os.Exit(2)
	}
	if *n > 0 && (*staged || rev != "") {
		fmt.Fprintln(os.Stderr, "error: -n cannot be combined with --staged or a revision argument")
		os.Exit(2)
	}

	if *n > 0 {
		if err := printCommits(*n); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	if *staged {
		hasStaged, err := checkHasStagedChanges()
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		if !hasStaged {
			fmt.Fprintln(os.Stderr, "no staged changes")
			return
		}
	}

	p := tea.NewProgram(ui.NewModel(ui.StartTarget{Rev: rev, Staged: *staged}), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// printUsage is installed as flag.Usage, so -h/--help (and flag-parse
// errors) print gitl's own synopsis instead of just the flag list.
func printUsage() {
	fmt.Fprint(os.Stderr, `gitl - a terminal UI for browsing git history

Usage:
  gitl                open the commit list for the current repository
  gitl <rev>          open a commit's detail view directly (sha, branch,
                       tag, HEAD~2, ...); q/Esc quits outright
  gitl --staged       open the staged changes (index vs HEAD) directly;
                       q/Esc quits outright
  gitl -n <count>     print the N most recent commits and exit

Flags:
`)
	flag.PrintDefaults()
}

// checkHasStagedChanges is a pre-flight for --staged, so an empty index
// prints a one-line message and exits instead of flashing an alt-screen TUI
// just to immediately say there's nothing to show.
func checkHasStagedChanges() (bool, error) {
	ctx := context.Background()
	repo, err := git.OpenRepo(ctx, ".")
	if err != nil {
		return false, err
	}
	return repo.HasStagedChanges(ctx)
}

func printCommits(n int) error {
	ctx := context.Background()
	repo, err := git.OpenRepo(ctx, ".")
	if err != nil {
		return err
	}
	hasCommits, err := repo.HasCommits(ctx)
	if err != nil {
		return err
	}
	if !hasCommits {
		fmt.Println("this repository has no commits yet")
		return nil
	}
	commits, err := repo.FetchLogBatch(ctx, 0, n)
	if err != nil {
		return err
	}
	fmt.Println(ui.RenderCommitList(commits, listWidth()))
	return nil
}

func listWidth() int {
	fd := os.Stdout.Fd()
	if isatty.IsTerminal(fd) {
		if w, _, err := xterm.GetSize(fd); err == nil && w > 0 {
			return w
		}
	}
	return fallbackListWidth
}
