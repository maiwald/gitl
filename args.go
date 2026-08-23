package main

import (
	"errors"
	"flag"
	"fmt"

	"github.com/maiwald/gitl/internal/ui"
)

// errFlagParse marks a failure the FlagSet has already reported itself
// (message plus usage), so main can exit without printing it twice. The
// underlying error is wrapped alongside it, keeping flag.ErrHelp recognizable.
var errFlagParse = errors.New("flag parse error")

// options is everything gitl's command line can ask for: the two
// print-and-exit modes, and the startup target for the TUI itself.
type options struct {
	count   int
	version bool
	target  ui.StartTarget
}

// parseArgs registers gitl's flags on fs and parses a full argument vector
// (os.Args[1:] in main, a literal one in tests) into options. fs is a
// parameter so main can install its own usage/error reporting on it while
// tests stay quiet.
//
// Flag parsing sees only the part of argv before a literal "--" (see
// splitPathspecs), so what follows it is never mistaken for a flag, whatever a
// path is named.
func parseArgs(fs *flag.FlagSet, argv []string) (options, error) {
	count := fs.Int("n", 0, "print the N most recent commits non-interactively and exit")
	staged := fs.Bool("staged", false, "open the staged changes (index vs HEAD) directly; q exits")
	diff := fs.Bool("diff", false, "open the working tree changes (index vs working tree) directly; q exits")
	version := fs.Bool("version", false, "print the version and exit")

	head, sepPaths, sepSeen := splitPathspecs(argv)
	if err := fs.Parse(head); err != nil {
		return options{}, fmt.Errorf("%w: %w", errFlagParse, err)
	}
	if *version {
		// Checked before any of the combination rules below: --version
		// prints and exits regardless of what else was typed.
		return options{version: true}, nil
	}

	target, err := startTarget(*staged, *diff, fs.Args(), sepPaths, sepSeen)
	if err != nil {
		return options{}, err
	}
	if *count > 0 && (target.Staged || target.Diff || target.Rev != "") {
		return options{}, errors.New("-n cannot be combined with --staged, --diff, a revision argument, or paths")
	}
	return options{count: *count, target: target}, nil
}

// splitPathspecs splits argv at the first literal "--", git's own separator
// between revisions and paths. Done before flag parsing because Go's flag
// package consumes a leading "--" itself: `gitl -- foo.go` would otherwise
// reach us indistinguishable from `gitl foo.go`, where foo.go means a
// revision instead.
func splitPathspecs(argv []string) (head, paths []string, sepSeen bool) {
	for i, arg := range argv {
		if arg == "--" {
			return argv[:i], argv[i+1:], true
		}
	}
	return argv, nil, false
}

// startTarget assembles the startup target from the parsed flags, the
// non-flag arguments before any "--" (args), and the paths after it
// (sepPaths). Without an explicit separator the first non-flag argument is
// the revision and the rest are paths — mirroring `git show <rev> <path>...`
// — except under --staged/--diff, which take no revision, so every non-flag
// argument there is a path.
func startTarget(staged, diff bool, args, sepPaths []string, sepSeen bool) (ui.StartTarget, error) {
	if staged && diff {
		return ui.StartTarget{}, errors.New("--staged and --diff are mutually exclusive")
	}

	var rev string
	if !staged && !diff && len(args) > 0 {
		rev, args = args[0], args[1:]
		if sepSeen && len(args) > 0 {
			// Everything after "--" is a path, so a second argument still
			// left here is a second revision — the one shape the rule above
			// can't absorb.
			return ui.StartTarget{}, errors.New("expected at most one revision argument before --")
		}
	}

	var paths []string
	paths = append(paths, args...)
	paths = append(paths, sepPaths...)
	if len(paths) > 0 && rev == "" && !staged && !diff {
		// The commit list is never path-filtered (see ui.StartTarget.Paths),
		// so paths on their own have nothing to apply to.
		return ui.StartTarget{}, errors.New("paths require --staged, --diff, or a revision argument")
	}
	return ui.StartTarget{Rev: rev, Staged: staged, Diff: diff, Paths: paths}, nil
}
