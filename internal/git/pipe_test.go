package git

import (
	"strings"
	"testing"
)

const showPatch = `commit 31cfe2074df3dffbf5903ba3d55f5f653220f045
Author: T <t@t>
Date:   Wed Aug 26 18:55:00 2026 +0200

    branch moves

diff --git a/f.txt b/f.txt
index df967b9..1f2cd5a 100644
--- a/f.txt
+++ b/f.txt
@@ -1 +1,2 @@
 base
+onbranch
`

// TestParsePipedPatchTakesOnlyTheHash pins the narrow contract with git's
// human-readable header: gitl reads the commit hash and nothing else, because
// the hash is the one part --pretty, log.date and locale can't reshape.
func TestParsePipedPatchTakesOnlyTheHash(t *testing.T) {
	p := ParsePipedPatch([]byte(showPatch))
	if p.Hash != "31cfe2074df3dffbf5903ba3d55f5f653220f045" {
		t.Errorf("Hash = %q, want the header's commit hash", p.Hash)
	}
	if len(p.Files) != 1 || p.Files[0].Path != "f.txt" {
		t.Errorf("Files = %+v, want one entry for f.txt", p.Files)
	}
}

// TestParsePipedPatchStripsGitsColor covers what git actually sends a pager:
// it colors its output when one is in use, and gitl re-colors from the parsed
// structure anyway.
func TestParsePipedPatchStripsGitsColor(t *testing.T) {
	colored := "\x1b[1mdiff --git a/f.txt b/f.txt\x1b[m\n" +
		"\x1b[1mindex df967b9..1f2cd5a 100644\x1b[m\n" +
		"\x1b[1m--- a/f.txt\x1b[m\n\x1b[1m+++ b/f.txt\x1b[m\n" +
		"\x1b[36m@@ -1 +1,2 @@\x1b[m\n base\x1b[m\n\x1b[32m+onbranch\x1b[m\n"

	p := ParsePipedPatch([]byte(colored))
	if len(p.Files) != 1 {
		t.Fatalf("Files = %+v, want the colored diff parsed as one file", p.Files)
	}
	if len(p.Files[0].Hunks) != 1 || len(p.Files[0].Hunks[0].Lines) != 2 {
		t.Fatalf("hunks = %+v, want one hunk of two lines", p.Files[0].Hunks)
	}
	for _, l := range p.Files[0].Hunks[0].Lines {
		if strings.Contains(l.Text, "\x1b") {
			t.Errorf("line %q still carries an escape sequence", l.Text)
		}
	}
}

// TestParsePipedPatchWithoutACommitHeader covers plain `git diff`, which pipes
// a bare diff, and `git status`, which pipes no diff at all.
func TestParsePipedPatchWithoutACommitHeader(t *testing.T) {
	bare := ParsePipedPatch([]byte(showPatch[strings.Index(showPatch, "diff --git"):]))
	if bare.Hash != "" || bare.Preamble != "" || len(bare.Files) != 1 {
		t.Errorf("got hash=%q preamble=%q files=%d, want a bare one-file diff", bare.Hash, bare.Preamble, len(bare.Files))
	}
	if d := bare.Detail(); d.Kind != EntryPiped || !d.IsPseudo() {
		t.Errorf("Detail() kind = %v, want a pseudo EntryPiped", d.Kind)
	}

	status := ParsePipedPatch([]byte("On branch master\nnothing to commit\n"))
	if status.Hash != "" || len(status.Files) != 0 || status.Preamble == "" {
		t.Errorf("got hash=%q preamble=%q files=%d, want everything as preamble", status.Hash, status.Preamble, len(status.Files))
	}
}

// TestParsePipedPatchOnUnparsablePatchKeepsTheText covers --word-diff, whose
// markers the unified-diff parser rejects. gitl is git's pager here: an error
// would leave the user's `git show --word-diff` showing nothing and failing,
// so the text stands in for the diff gitl couldn't parse.
func TestParsePipedPatchOnUnparsablePatchKeepsTheText(t *testing.T) {
	wordDiff := showPatch[:strings.Index(showPatch, "@@ -1 +1,2 @@")] +
		"@@ -1 +1,2 @@\nbase\n{+onbranch+}\n"

	p := ParsePipedPatch([]byte(wordDiff))
	if len(p.Files) != 0 {
		t.Errorf("Files = %+v, want none — the patch didn't parse", p.Files)
	}
	if !strings.Contains(p.Preamble, "{+onbranch+}") {
		t.Errorf("Preamble = %q, want the whole input verbatim", p.Preamble)
	}
	if p.Hash != "31cfe2074df3dffbf5903ba3d55f5f653220f045" {
		t.Errorf("Hash = %q, want the header's commit hash even so", p.Hash)
	}
}
