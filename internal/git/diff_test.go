package git

import (
	"strings"
	"testing"
)

func TestParseUnifiedDiffAddsDeletesContext(t *testing.T) {
	diff := `diff --git a/foo.txt b/foo.txt
index e69de29..0cfbf08 100644
--- a/foo.txt
+++ b/foo.txt
@@ -1,3 +1,4 @@
 line one
-line two
+line two changed
+line three added
 line four
`
	files, err := ParseUnifiedDiff(strings.NewReader(diff))
	if err != nil {
		t.Fatalf("ParseUnifiedDiff: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}
	f := files[0]
	if f.Path != "foo.txt" || f.OldPath != "foo.txt" {
		t.Errorf("Path=%q OldPath=%q, want foo.txt/foo.txt", f.Path, f.OldPath)
	}
	if f.Status != StatusModified {
		t.Errorf("Status = %v, want StatusModified", f.Status)
	}
	if f.Additions != 2 {
		t.Errorf("Additions = %d, want 2", f.Additions)
	}
	if f.Deletions != 1 {
		t.Errorf("Deletions = %d, want 1", f.Deletions)
	}
	if len(f.Hunks) != 1 {
		t.Fatalf("got %d hunks, want 1", len(f.Hunks))
	}
	hunk := f.Hunks[0]
	wantOps := []LineOp{OpContext, OpDelete, OpAdd, OpAdd, OpContext}
	wantText := []string{"line one", "line two", "line two changed", "line three added", "line four"}
	if len(hunk.Lines) != len(wantOps) {
		t.Fatalf("got %d lines, want %d", len(hunk.Lines), len(wantOps))
	}
	for i, l := range hunk.Lines {
		if l.Op != wantOps[i] {
			t.Errorf("line %d: Op = %v, want %v", i, l.Op, wantOps[i])
		}
		if l.Text != wantText[i] {
			t.Errorf("line %d: Text = %q, want %q", i, l.Text, wantText[i])
		}
	}
}

func TestParseUnifiedDiffNewFile(t *testing.T) {
	diff := `diff --git a/new.txt b/new.txt
new file mode 100644
index 0000000..3b18e51
--- /dev/null
+++ b/new.txt
@@ -0,0 +1,2 @@
+hello
+world
`
	files, err := ParseUnifiedDiff(strings.NewReader(diff))
	if err != nil {
		t.Fatalf("ParseUnifiedDiff: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}
	if files[0].Status != StatusAdded {
		t.Errorf("Status = %v, want StatusAdded", files[0].Status)
	}
	if files[0].Additions != 2 || files[0].Deletions != 0 {
		t.Errorf("Additions=%d Deletions=%d, want 2/0", files[0].Additions, files[0].Deletions)
	}
}

func TestParseUnifiedDiffDeletedFile(t *testing.T) {
	diff := `diff --git a/gone.txt b/gone.txt
deleted file mode 100644
index 3b18e51..0000000
--- a/gone.txt
+++ /dev/null
@@ -1,2 +0,0 @@
-hello
-world
`
	files, err := ParseUnifiedDiff(strings.NewReader(diff))
	if err != nil {
		t.Fatalf("ParseUnifiedDiff: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}
	if files[0].Status != StatusDeleted {
		t.Errorf("Status = %v, want StatusDeleted", files[0].Status)
	}
	if files[0].Path != "gone.txt" {
		t.Errorf("Path = %q, want gone.txt (falls back to OldName)", files[0].Path)
	}
}

func TestParseUnifiedDiffRename(t *testing.T) {
	diff := `diff --git a/old_name.txt b/new_name.txt
similarity index 100%
rename from old_name.txt
rename to new_name.txt
`
	files, err := ParseUnifiedDiff(strings.NewReader(diff))
	if err != nil {
		t.Fatalf("ParseUnifiedDiff: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}
	f := files[0]
	if f.Status != StatusRenamed {
		t.Errorf("Status = %v, want StatusRenamed", f.Status)
	}
	if f.OldPath != "old_name.txt" || f.Path != "new_name.txt" {
		t.Errorf("OldPath=%q Path=%q, want old_name.txt/new_name.txt", f.OldPath, f.Path)
	}
}

func TestParseUnifiedDiffBinary(t *testing.T) {
	diff := `diff --git a/image.png b/image.png
index e69de29..3b18e51 100644
Binary files a/image.png and b/image.png differ
`
	files, err := ParseUnifiedDiff(strings.NewReader(diff))
	if err != nil {
		t.Fatalf("ParseUnifiedDiff: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}
	if !files[0].Binary {
		t.Error("Binary = false, want true")
	}
	if len(files[0].Hunks) != 0 {
		t.Errorf("got %d hunks for binary file, want 0", len(files[0].Hunks))
	}
}

func TestParseUnifiedDiffMultipleFiles(t *testing.T) {
	diff := `diff --git a/a.txt b/a.txt
index e69de29..0cfbf08 100644
--- a/a.txt
+++ b/a.txt
@@ -1 +1 @@
-a
+A
diff --git a/b.txt b/b.txt
index e69de29..3b18e51 100644
--- a/b.txt
+++ b/b.txt
@@ -1 +1 @@
-b
+B
`
	files, err := ParseUnifiedDiff(strings.NewReader(diff))
	if err != nil {
		t.Fatalf("ParseUnifiedDiff: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("got %d files, want 2", len(files))
	}
	if files[0].Path != "a.txt" || files[1].Path != "b.txt" {
		t.Errorf("file order = [%s, %s], want [a.txt, b.txt]", files[0].Path, files[1].Path)
	}
}

// TestParsePatchReturnsShowPreamble pins the split gitl's pager mode depends
// on: everything before the first file — for `git show`, the commit header
// and message — comes back separately rather than being discarded.
func TestParsePatchReturnsShowPreamble(t *testing.T) {
	patch := `commit 31cfe2074df3dffbf5903ba3d55f5f653220f045
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
	files, preamble, err := ParsePatch(strings.NewReader(patch))
	if err != nil {
		t.Fatalf("ParsePatch: %v", err)
	}
	if len(files) != 1 || files[0].Path != "f.txt" {
		t.Fatalf("files = %+v, want one entry for f.txt", files)
	}
	if !strings.HasPrefix(preamble, "commit 31cfe207") {
		t.Errorf("preamble = %q, want it to start with the commit header", preamble)
	}
	if strings.Contains(preamble, "diff --git") {
		t.Errorf("preamble = %q, want it to stop before the first file", preamble)
	}
}

// TestParsePatchWithoutADiffIsAllPreamble covers what a pager receives for
// `git show HEAD:file` or `git status`: no diff at all, so everything is
// preamble and gitl has something to show verbatim rather than nothing.
func TestParsePatchWithoutADiffIsAllPreamble(t *testing.T) {
	files, preamble, err := ParsePatch(strings.NewReader("On branch master\nnothing to commit\n"))
	if err != nil {
		t.Fatalf("ParsePatch: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("files = %+v, want none", files)
	}
	if preamble != "On branch master\nnothing to commit\n" {
		t.Errorf("preamble = %q, want the input verbatim", preamble)
	}
}
