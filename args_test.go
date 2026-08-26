package main

import (
	"flag"
	"io"
	"testing"
)

// parse runs parseArgs on a quiet FlagSet, so a test for a rejected command
// line doesn't print flag usage into the test output.
func parse(t *testing.T, argv ...string) (options, error) {
	t.Helper()
	fs := flag.NewFlagSet("gitl", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	return parseArgs(fs, argv)
}

func TestParseArgsModes(t *testing.T) {
	opts, err := parse(t, "-n", "5")
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if opts.count != 5 {
		t.Errorf("count = %d, want 5", opts.count)
	}

	// --version wins over everything else, including arguments that would
	// otherwise be rejected.
	opts, err = parse(t, "--version", "HEAD~2")
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if !opts.version {
		t.Error("version = false, want true")
	}
}

// TestParseArgsRejectsPositionals pins the grammar gitl no longer has: a
// revision or a path is git's to parse, and reaches gitl through the pager.
func TestParseArgsRejectsPositionals(t *testing.T) {
	for _, argv := range [][]string{{"HEAD~2"}, {"--", "a.txt"}, {"a.txt"}} {
		if _, err := parse(t, argv...); err == nil {
			t.Errorf("parseArgs(%q) succeeded, want a rejection", argv)
		}
	}
}

// TestPagerMode pins the signature git produces when it runs gitl as a pager:
// no arguments at all, with the command's output on stdin. Everything else is
// a direct invocation, including one whose stdin happens to be redirected.
func TestPagerMode(t *testing.T) {
	tests := []struct {
		name       string
		argv       []string
		stdinIsTTY bool
		want       bool
	}{
		{"git as pager", nil, false, true},
		{"manual pipe", []string{}, false, true},
		{"interactive, no args", nil, true, false},
		{"flags with redirected stdin", []string{"-n", "5"}, false, false},
		{"revision with redirected stdin", []string{"HEAD~1"}, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := pagerMode(tc.argv, tc.stdinIsTTY); got != tc.want {
				t.Errorf("pagerMode(%q, stdinIsTTY=%v) = %v, want %v", tc.argv, tc.stdinIsTTY, got, tc.want)
			}
		})
	}
}
