package ui

import (
	"strings"
	"testing"

	"github.com/maiwald/gitl/internal/git"
)

// TestPipedCommitWithoutADiffShowsGitsOwnText covers the commands that name a
// commit but print something other than a patch — `--stat`, `--name-only`,
// `--raw`, a merge's combined diff. gitl's own header over an empty diff would
// show strictly less than git just printed, so the preamble is shown verbatim
// instead. The nil repo is half the assertion: the header fetch isn't merely
// unused, it's never reached.
func TestPipedCommitWithoutADiffShowsGitsOwnText(t *testing.T) {
	statLine := " f.txt | 2 +-"
	p := &git.PipedPatch{
		Hash: "31cfe2074df3dffbf5903ba3d55f5f653220f045",
		Preamble: "commit 31cfe2074df3dffbf5903ba3d55f5f653220f045\n" +
			"Author: T <t@t>\n\n    branch moves\n\n" +
			statLine + "\n 1 file changed, 1 insertion(+), 1 deletion(-)\n",
	}

	msg, ok := fetchPipedCommitCmd(nil, p)().(commitDetailLoadedMsg)
	if !ok {
		t.Fatalf("fetchPipedCommitCmd produced %T, want commitDetailLoadedMsg", msg)
	}
	if msg.detail.Kind != git.EntryPiped {
		t.Errorf("Kind = %v, want EntryPiped", msg.detail.Kind)
	}

	lines, _, _, _ := headerLinesFor(msg.detail)
	if !slicesContain(lines, statLine) {
		t.Errorf("header lines = %q, want git's stat block among them", lines)
	}
}

func slicesContain(lines []string, want string) bool {
	for _, l := range lines {
		if strings.TrimRight(l, " ") == want {
			return true
		}
	}
	return false
}
