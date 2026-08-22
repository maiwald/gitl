package main

import (
	"context"
	"errors"
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
	// ContinueOnError rather than ExitOnError so a bad argument *combination*
	// (which the FlagSet knows nothing about) can be reported from the same
	// place as a bad flag. The FlagSet keeps printing its own parse failures
	// and usage, so those arrive here already reported — see errFlagParse.
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

	target := opts.target
	if target.Staged && !hasChanges((*git.Repo).HasStagedChanges, target.Paths) {
		return
	}
	if target.Diff && !hasChanges((*git.Repo).HasWorktreeChanges, target.Paths) {
		return
	}

	p := tea.NewProgram(ui.NewModel(target), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		exitWithError(err)
	}
}

// exitWithError reports a failure that leaves gitl nothing to show and exits
// nonzero — every runtime (as opposed to command-line) failure in main.
func exitWithError(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

// printUsage is installed as fs.Usage, so -h/--help (and flag-parse errors)
// print gitl's own synopsis instead of just the flag list.
func printUsage(fs *flag.FlagSet) {
	fmt.Fprint(os.Stderr, `gitl - a terminal UI for browsing git history

Usage:
  gitl                       open the commit list for the current repository
  gitl <rev> [<path>...]     open a commit's detail view directly (sha,
                              branch, tag, HEAD~2, ...); q/Esc quits outright
  gitl --staged [<path>...]  open the staged changes (index vs HEAD)
                              directly; q/Esc quits outright
  gitl --diff [<path>...]    open the working tree changes (index vs working
                              tree) directly; q/Esc quits outright
  gitl -n <count>            print the N most recent commits and exit
  gitl --version             print the version and exit

Paths limit the diff that opens to those files, like git's own. Use -- to
separate them from a revision explicitly (gitl <rev> -- <path>...).

Flags:
`)
	fs.PrintDefaults()
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

// hasChanges is the pre-flight for --staged/--diff: an empty index/worktree
// exits quietly, the way `git diff` prints nothing and succeeds, instead of
// flashing an alt-screen TUI just to immediately say there's nothing to
// show. check is passed as a method expression, e.g.
// (*git.Repo).HasStagedChanges, and is asked only about paths when the
// command line gave any — so `gitl --diff untouched.go` is as silent as
// `git diff -- untouched.go` is. Only a failure to answer at all is
// reported, and exits nonzero.
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
		// --staged/--diff pre-flight (see hasChanges). The guard itself stays:
		// FetchLogBatch on an unborn HEAD would fail rather than print
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
