package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
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
	if pagerMode(os.Args[1:], isatty.IsTerminal(os.Stdin.Fd())) {
		if err := runAsPager(); err != nil {
			exitWithError(err)
		}
		return
	}

	// ContinueOnError rather than ExitOnError so a bad argument *combination*
	// (which the FlagSet knows nothing about) exits through the same path as
	// a bad flag. The FlagSet still prints its own parse failures, so those
	// arrive here already reported — see errFlagParse.
	fs := flag.NewFlagSet("gitl", flag.ContinueOnError)
	fs.Usage = func() { printUsage(fs) }
	opts, err := parseArgs(fs, os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			// -h/--help: the usage text is the requested output, not an error.
			return
		}
		if !errors.Is(err, errFlagParse) {
			fmt.Fprintln(os.Stderr, "error:", err)
		}
		os.Exit(2)
	}

	if opts.version {
		fmt.Println("gitl " + versionString())
		return
	}

	if opts.count > 0 {
		if err := printCommits(opts.count); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	p := tea.NewProgram(ui.NewModel(ui.StartTarget{}), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		exitWithError(err)
	}
}

// runAsPager renders a patch handed to gitl on stdin, the way git invokes a
// pager (see pagerMode).
//
// Keyboard input can't come from stdin — that's the patch — so it comes from
// the controlling terminal instead. Where there isn't one to read from, or
// nowhere to draw, gitl copies its input through untouched and succeeds: a
// pager that fails is a git command that fails, and `git diff | gitl | cat`
// should behave like `cat`.
func runAsPager() error {
	if !isatty.IsTerminal(os.Stdout.Fd()) {
		return passThrough()
	}
	tty, err := os.Open("/dev/tty")
	if err != nil {
		return passThrough()
	}
	defer tty.Close()

	patch, err := git.ReadPipedPatch(os.Stdin)
	if err != nil {
		return err
	}
	if len(patch.Files) == 0 && patch.Preamble == "" {
		// git runs its pager even for a command that produced nothing, so
		// `git diff` with a clean tree arrives here as an empty pipe. Exit
		// rather than open a full screen showing it — the same "say nothing
		// when there's nothing to show" the non-interactive paths follow.
		return nil
	}
	p := tea.NewProgram(
		ui.NewModel(ui.StartTarget{Piped: patch}),
		tea.WithAltScreen(),
		tea.WithInput(tty),
	)
	_, err = p.Run()
	return err
}

func passThrough() error {
	_, err := io.Copy(os.Stdout, os.Stdin)
	return err
}

// exitWithError reports a failure that leaves gitl nothing to show and exits
// nonzero — every runtime, as opposed to command-line, failure in main.
func exitWithError(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

// printUsage is installed as fs.Usage, so -h/--help (and flag-parse errors)
// print gitl's own synopsis instead of just the flag list.
func printUsage(fs *flag.FlagSet) {
	fmt.Fprint(os.Stderr, `gitl - a terminal UI for browsing git history

Usage:
  gitl              open the commit list for the current repository
  gitl -n <count>   print the N most recent commits and exit
  gitl --version    print the version and exit

To view a commit or a diff, point git at gitl as its pager and use git's own
commands — every revision, range and pathspec git accepts then works:

  git config --global pager.diff gitl
  git config --global pager.show gitl

  git show HEAD~2            a commit, with its message, stats and diff
  git show <rev> -- <path>   the same, limited to a path
  git diff origin/main...    everything your branch changed
  git diff --cached          the staged changes

Flags:
`)
	fs.PrintDefaults()
}

// versionString reports the module version go install embedded (a tag like
// "v1.3.0", or a pseudo-version on an untagged commit). A plain `go build`
// from a local checkout has no such metadata, and reports "dev" rather than
// Go's uninformative "(devel)".
func versionString() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return "dev"
	}
	return info.Main.Version
}

// openCWDRepo opens the repository rooted at the current working directory —
// the only place gitl's CLI hardcodes ".", keeping internal/git path-agnostic.
func openCWDRepo(ctx context.Context) (*git.Repo, error) {
	return git.OpenRepo(ctx, ".")
}

// hasChanges is the pre-flight for --staged/--diff: an empty index or worktree
// exits quietly, the way `git diff` prints nothing and succeeds, instead of
// flashing an alt-screen TUI just to say there's nothing to show. check is a
// method expression, e.g. (*git.Repo).HasStagedChanges, asked only about paths
// when the command line gave any. Only a failure to answer at all is reported,
// and exits nonzero.
func hasChanges(check func(*git.Repo, context.Context, ...string) (bool, error), paths []string) bool {
	ctx := context.Background()
	repo, err := openCWDRepo(ctx)
	if err != nil {
		exitWithError(err)
	}
	has, err := check(repo, ctx, paths...)
	if err != nil {
		exitWithError(err)
	}
	return has
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
		// Nothing to print, and nothing to say about it — same as the
		// --staged/--diff pre-flight (see hasChanges). The guard is still
		// needed: FetchLogBatch on an unborn HEAD fails rather than printing
		// nothing.
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
