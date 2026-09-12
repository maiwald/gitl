package main

import (
	"bytes"
	"testing"
)

const statText = " f.txt | 3 +++\n 1 file changed, 3 insertions(+)\n"

// TestPipeToPagerFeedsTheText pins the handoff itself: the pager receives the
// bytes git produced, not a rendering of them.
func TestPipeToPagerFeedsTheText(t *testing.T) {
	var out bytes.Buffer
	if err := pipeToPager("cat", []byte(statText), &out); err != nil {
		t.Fatalf("pipeToPager: %v", err)
	}
	if out.String() != statText {
		t.Errorf("pager output = %q, want the input verbatim", out.String())
	}
}

// TestPipeToPagerWithoutOnePrintsItself covers a machine with no pager to
// resolve — the text still has to arrive, unpaged.
func TestPipeToPagerWithoutOnePrintsItself(t *testing.T) {
	var out bytes.Buffer
	if err := pipeToPager("", []byte(statText), &out); err != nil {
		t.Fatalf("pipeToPager: %v", err)
	}
	if out.String() != statText {
		t.Errorf("output = %q, want the input verbatim", out.String())
	}
}

// TestPipeToPagerRecoversFromAMissingPager covers a configured pager that
// isn't installed: the shell exits 127 having printed nothing, and gitl must
// not let the git command behind it come out empty.
func TestPipeToPagerRecoversFromAMissingPager(t *testing.T) {
	var out bytes.Buffer
	err := pipeToPager("gitl-no-such-pager 2>/dev/null", []byte(statText), &out)
	if err != nil {
		t.Fatalf("pipeToPager: %v", err)
	}
	if out.String() != statText {
		t.Errorf("output = %q, want the input verbatim", out.String())
	}
}

// TestPipeToPagerReportsOtherFailures pins the other half of that: a pager
// that ran and failed is the pager's business, not something to reprint over.
func TestPipeToPagerReportsOtherFailures(t *testing.T) {
	var out bytes.Buffer
	if err := pipeToPager("exit 3", []byte(statText), &out); err == nil {
		t.Error("pipeToPager succeeded, want the pager's exit status reported")
	}
	if out.Len() != 0 {
		t.Errorf("output = %q, want nothing reprinted", out.String())
	}
}
