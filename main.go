package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime/debug"

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
	diff := flag.Bool("diff", false, "open the working tree changes (index vs working tree) directly; q exits")
	version := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *version {
		fmt.Println("gitl " + versionString())
		return
	}

	args := flag.Args()
	if len(args) > 1 {
		fmt.Fprintln(os.Stderr, "error: expected at most one revision argument")
		os.Exit(2)
	}
	var rev string
	if len(args) == 1 {
		rev = args[0]
	}

	if *staged && *diff {
		fmt.Fprintln(os.Stderr, "error: --staged and --diff are mutually exclusive")
		os.Exit(2)
	}
	if (*staged || *diff) && rev != "" {
		fmt.Fprintln(os.Stderr, "error: --staged/--diff and a revision argument are mutually exclusive")
		os.Exit(2)
	}
	if *n > 0 && (*staged || *diff || rev != "") {
		fmt.Fprintln(os.Stderr, "error: -n cannot be combined with --staged, --diff, or a revision argument")
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
		hasStaged, err := checkHasChanges((*git.Repo).HasStagedChanges)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		if !hasStaged {
			fmt.Fprintln(os.Stderr, "no staged changes")
			return
		}
	}
	if *diff {
		hasWorktreeChanges, err := checkHasChanges((*git.Repo).HasWorktreeChanges)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		if !hasWorktreeChanges {
			fmt.Fprintln(os.Stderr, "no working tree changes")
			return
		}
	}

	p := tea.NewProgram(ui.NewModel(ui.StartTarget{Rev: rev, Staged: *staged, Diff: *diff}), tea.WithAltScreen())
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
  gitl --diff         open the working tree changes (index vs working
                       tree) directly; q/Esc quits outright
  gitl -n <count>     print the N most recent commits and exit
  gitl --version      print the version and exit

Flags:
`)
	flag.PrintDefaults()
}

// versionString reports the module version go install embedded (a tag like
// "v1.3.0", or a pseudo-version on an untagged commit). A plain `go build`
// from a local checkout has no such metadata, so that case falls back to
// "dev" rather than printing Go's uninformative "(devel)".
func versionString() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return "dev"
	}
	return info.Main.Version
}

// openCWDRepo opens the repository rooted at the current working directory,
// the only place gitl's CLI needs a hardcoded "." — internal/git itself
// stays path-agnostic.
func openCWDRepo(ctx context.Context) (*git.Repo, error) {
	return git.OpenRepo(ctx, ".")
}

// checkHasChanges is a pre-flight for --staged/--diff, so an empty
// index/worktree prints a one-line message and exits instead of flashing an
// alt-screen TUI just to immediately say there's nothing to show. check is
// passed as a method expression, e.g. (*git.Repo).HasStagedChanges.
func checkHasChanges(check func(*git.Repo, context.Context) (bool, error)) (bool, error) {
	ctx := context.Background()
	repo, err := openCWDRepo(ctx)
	if err != nil {
		return false, err
	}
	return check(repo, ctx)
}

func printCommits(n int) error {
	ctx := context.Background()
	repo, err := openCWDRepo(ctx)
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
