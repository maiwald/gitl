package main

import (
	"flag"
	"io"
	"reflect"
	"testing"

	"github.com/maiwald/gitl/internal/ui"
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

func TestParseArgsTargets(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want ui.StartTarget
	}{
		{"no arguments", nil, ui.StartTarget{}},
		{"revision only", []string{"HEAD~2"}, ui.StartTarget{Rev: "HEAD~2"}},
		{
			"revision with paths",
			[]string{"HEAD~2", "a.txt", "sub/b.txt"},
			ui.StartTarget{Rev: "HEAD~2", Paths: []string{"a.txt", "sub/b.txt"}},
		},
		{
			"revision with paths after a separator",
			[]string{"HEAD~2", "--", "a.txt"},
			ui.StartTarget{Rev: "HEAD~2", Paths: []string{"a.txt"}},
		},
		{"staged only", []string{"--staged"}, ui.StartTarget{Staged: true}},
		{
			"staged with paths",
			[]string{"--staged", "a.txt"},
			ui.StartTarget{Staged: true, Paths: []string{"a.txt"}},
		},
		{
			"diff with paths after a separator",
			[]string{"--diff", "--", "a.txt", "b.txt"},
			ui.StartTarget{Diff: true, Paths: []string{"a.txt", "b.txt"}},
		},
		{
			// The separator is what makes a path that looks like a flag (or
			// like a revision) reach us as a path at all.
			"separator protects a flag-like path",
			[]string{"--diff", "--", "--staged"},
			ui.StartTarget{Diff: true, Paths: []string{"--staged"}},
		},
		{
			// --staged/--diff take no revision, so every argument is a path,
			// whichever side of the separator it's on.
			"staged with paths on both sides of the separator",
			[]string{"--staged", "a.txt", "--", "b.txt"},
			ui.StartTarget{Staged: true, Paths: []string{"a.txt", "b.txt"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, err := parse(t, tt.argv...)
			if err != nil {
				t.Fatalf("parseArgs(%q): %v", tt.argv, err)
			}
			if !reflect.DeepEqual(opts.target, tt.want) {
				t.Errorf("target = %+v, want %+v", opts.target, tt.want)
			}
		})
	}
}

func TestParseArgsRejects(t *testing.T) {
	tests := []struct {
		name string
		argv []string
	}{
		{"staged and diff together", []string{"--staged", "--diff"}},
		{"two revisions", []string{"HEAD", "HEAD~1", "--", "a.txt"}},
		{"paths without a target", []string{"--", "a.txt"}},
		{"-n with a revision", []string{"-n", "5", "HEAD"}},
		{"-n with paths", []string{"-n", "5", "--staged", "a.txt"}},
		{"unknown flag", []string{"--nope"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parse(t, tt.argv...); err == nil {
				t.Errorf("parseArgs(%q) = nil error, want a rejection", tt.argv)
			}
		})
	}
}

// TestParseArgsSeparatorlessPathIsARevision pins down the one place gitl
// doesn't guess like git does: with no "--", the first argument is always
// taken as a revision, even when a file by that name exists, since the
// commit list can't be path-filtered anyway.
func TestParseArgsSeparatorlessPathIsARevision(t *testing.T) {
	opts, err := parse(t, "main.go")
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if opts.target.Rev != "main.go" || len(opts.target.Paths) != 0 {
		t.Errorf("target = %+v, want it read as a revision", opts.target)
	}
}

func TestParseArgsModes(t *testing.T) {
	opts, err := parse(t, "-n", "5")
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if opts.count != 5 {
		t.Errorf("count = %d, want 5", opts.count)
	}

	// --version wins over everything else, including combinations that would
	// otherwise be rejected.
	opts, err = parse(t, "--version", "--staged", "--diff")
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if !opts.version {
		t.Error("version = false, want true")
	}
}

func TestSplitPathspecs(t *testing.T) {
	tests := []struct {
		name      string
		argv      []string
		wantHead  []string
		wantPaths []string
		wantSeen  bool
	}{
		{"no separator", []string{"HEAD", "a.txt"}, []string{"HEAD", "a.txt"}, nil, false},
		{"leading separator", []string{"--", "a.txt"}, []string{}, []string{"a.txt"}, true},
		{"trailing separator", []string{"--diff", "--"}, []string{"--diff"}, []string{}, true},
		{
			// Only the first "--" separates; later ones are paths named "--".
			"second separator is a path",
			[]string{"HEAD", "--", "a.txt", "--"},
			[]string{"HEAD"}, []string{"a.txt", "--"}, true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			head, paths, seen := splitPathspecs(tt.argv)
			if !reflect.DeepEqual(head, tt.wantHead) || !reflect.DeepEqual(paths, tt.wantPaths) || seen != tt.wantSeen {
				t.Errorf("splitPathspecs(%q) = %q, %q, %v; want %q, %q, %v",
					tt.argv, head, paths, seen, tt.wantHead, tt.wantPaths, tt.wantSeen)
			}
		})
	}
}
