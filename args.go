package main

import (
	"errors"
	"flag"
	"fmt"
)

// errFlagParse marks a failure the FlagSet has already reported itself
// (message plus usage), so main can exit without printing it twice. The
// underlying error is wrapped alongside it, keeping flag.ErrHelp recognizable.
var errFlagParse = errors.New("flag parse error")

// options is everything gitl's command line can ask for: the two
// print-and-exit modes. Everything else gitl can show — a commit, a diff
// against any revision, a pathspec — arrives through git itself, which runs
// gitl as its pager (see pagerMode) and has already parsed its own grammar.
type options struct {
	count   int
	version bool
}

// parseArgs registers gitl's flags on fs and parses a full argument vector
// (os.Args[1:] in main, a literal one in tests) into options. fs is a
// parameter so main can install its own usage/error reporting on it while
// tests stay quiet.
func parseArgs(fs *flag.FlagSet, argv []string) (options, error) {
	count := fs.Int("n", 0, "print the N most recent commits non-interactively and exit")
	version := fs.Bool("version", false, "print the version and exit")

	if err := fs.Parse(argv); err != nil {
		return options{}, fmt.Errorf("%w: %w", errFlagParse, err)
	}
	if *version {
		// Checked first: --version prints and exits regardless of what else
		// was typed.
		return options{version: true}, nil
	}
	if args := fs.Args(); len(args) > 0 {
		return options{}, fmt.Errorf("unexpected argument %q: to view a commit or a diff, point git at gitl as its pager (gitl --help)", args[0])
	}
	return options{count: *count}, nil
}

// pagerMode reports whether this invocation is git running gitl as its pager.
// That is exactly git's signature: it execs the pager with no arguments at
// all and the command's output on stdin, so no-args-and-stdin-is-not-a-tty
// identifies it without gitl having to be told. A direct `gitl -n 5` in a
// script keeps working, since it has arguments.
func pagerMode(argv []string, stdinIsTTY bool) bool {
	return len(argv) == 0 && !stdinIsTTY
}
