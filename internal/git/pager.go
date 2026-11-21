package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// defaultPager is git's own DEFAULT_PAGER, used when nothing else names one.
const defaultPager = "less"

// FallbackPager reports the pager command git would have run for a command it
// is currently paging through gitl: GIT_PAGER, then core.pager, then PAGER,
// then less. That is git's own precedence with one entry removed — the
// pager.<cmd> setting that pointed at gitl in the first place, which is
// exactly the one the caller is trying to step around.
//
// Any candidate naming gitl is skipped rather than returned: gitl piping its
// input to gitl would never terminate.
//
// It reports "" when nothing usable is left, which the caller takes as "print
// the text yourself" — a missing pager must not turn into a git command that
// shows nothing.
func FallbackPager(ctx context.Context, dir string) string {
	// Each candidate is looked up only once the ones before it have been
	// passed over, so the git subprocess core.pager costs is skipped whenever
	// GIT_PAGER already names a usable pager.
	candidates := []func() string{
		func() string { return os.Getenv("GIT_PAGER") },
		func() string {
			// A repo with no core.pager set exits nonzero here, which is an
			// answer, not a failure.
			v, _ := runGitTrimmed(ctx, dir, "config", "--get", "core.pager")
			return v
		},
		func() string { return os.Getenv("PAGER") },
		func() string { return defaultPager },
	}

	for _, candidate := range candidates {
		if c := strings.TrimSpace(candidate()); c != "" && !namesGitl(c) {
			return c
		}
	}
	return ""
}

// namesGitl reports whether a pager command runs gitl itself. It reads only
// the command's first word, so a pager that reaches gitl some other way (a
// shell pipeline, a wrapper script) isn't caught — nothing short of running it
// would catch those, and a pager configured to page through gitl is already
// asking for what it gets.
func namesGitl(cmd string) bool {
	fields := strings.Fields(cmd)
	return len(fields) > 0 && filepath.Base(fields[0]) == "gitl"
}
