package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFetchCommitDetail(t *testing.T) {
	repo, _ := initFixtureRepo(t, 1)
	ctx := context.Background()

	// Replace the single commit with one that has a multi-line body, so we
	// can verify %B is captured in full.
	dir := repo.Path
	runGitCmd(t, dir, fixtureCommitEnv, "commit", "--amend", "--allow-empty", "-m", "subject line\n\nbody line one\nbody line two")
	amendedHash := headHash(t, dir)

	detail, err := repo.FetchCommitDetail(ctx, amendedHash)
	if err != nil {
		t.Fatalf("FetchCommitDetail: %v", err)
	}

	if detail.Hash != amendedHash {
		t.Errorf("Hash = %s, want %s", detail.Hash, amendedHash)
	}
	if detail.Subject != "subject line" {
		t.Errorf("Subject = %q, want %q", detail.Subject, "subject line")
	}
	wantBody := "subject line\n\nbody line one\nbody line two"
	if detail.Body != wantBody {
		t.Errorf("Body = %q, want %q", detail.Body, wantBody)
	}
	if detail.AuthorName != "Test Author" {
		t.Errorf("AuthorName = %q", detail.AuthorName)
	}
}

func TestFetchCommitDetailMergeCommit(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	runGitCmd(t, dir, fixtureCommitEnv, "init", "-b", "main")
	runGitCmd(t, dir, fixtureCommitEnv, "config", "user.name", "Test Author")
	runGitCmd(t, dir, fixtureCommitEnv, "config", "user.email", "test@example.com")
	runGitCmd(t, dir, fixtureCommitEnv, "commit", "--allow-empty", "-m", "base")
	runGitCmd(t, dir, fixtureCommitEnv, "checkout", "-b", "feature")
	runGitCmd(t, dir, fixtureCommitEnv, "commit", "--allow-empty", "-m", "feature work")
	runGitCmd(t, dir, fixtureCommitEnv, "checkout", "main")
	runGitCmd(t, dir, fixtureCommitEnv, "merge", "--no-ff", "-m", "merge feature", "feature")

	repo, err := OpenRepo(ctx, dir)
	if err != nil {
		t.Fatalf("OpenRepo: %v", err)
	}

	mergeHash := headHash(t, dir)

	detail, err := repo.FetchCommitDetail(ctx, mergeHash)
	if err != nil {
		t.Fatalf("FetchCommitDetail on merge commit: %v", err)
	}
	if len(detail.Parents) != 2 {
		t.Errorf("merge commit Parents = %v, want 2 parents", detail.Parents)
	}
	if detail.Subject != "merge feature" {
		t.Errorf("Subject = %q, want %q", detail.Subject, "merge feature")
	}
	if len(detail.Files) != 0 {
		t.Errorf("merge commit Files = %v, want none fetched (v1 limitation)", detail.Files)
	}
}

func TestFetchCommitDetailRootCommitDiff(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	runGitCmd(t, dir, fixtureCommitEnv, "init", "-b", "main")
	runGitCmd(t, dir, fixtureCommitEnv, "config", "user.name", "Test Author")
	runGitCmd(t, dir, fixtureCommitEnv, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello\nworld\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGitCmd(t, dir, fixtureCommitEnv, "add", "hello.txt")
	runGitCmd(t, dir, fixtureCommitEnv, "commit", "-m", "root commit")

	repo, err := OpenRepo(ctx, dir)
	if err != nil {
		t.Fatalf("OpenRepo: %v", err)
	}
	rootHash := headHash(t, dir)

	detail, err := repo.FetchCommitDetail(ctx, rootHash)
	if err != nil {
		t.Fatalf("FetchCommitDetail on root commit: %v", err)
	}
	if len(detail.Parents) != 0 {
		t.Fatalf("root commit Parents = %v, want none", detail.Parents)
	}
	if len(detail.Files) != 1 {
		t.Fatalf("root commit Files = %v, want 1 file", detail.Files)
	}
	f := detail.Files[0]
	if f.Path != "hello.txt" || f.Status != StatusAdded {
		t.Errorf("file = %+v, want hello.txt added", f)
	}
	if f.Additions != 2 {
		t.Errorf("Additions = %d, want 2", f.Additions)
	}
}

func TestFetchCommitDetailNormalCommitDiff(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	runGitCmd(t, dir, fixtureCommitEnv, "init", "-b", "main")
	runGitCmd(t, dir, fixtureCommitEnv, "config", "user.name", "Test Author")
	runGitCmd(t, dir, fixtureCommitEnv, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGitCmd(t, dir, fixtureCommitEnv, "add", "hello.txt")
	runGitCmd(t, dir, fixtureCommitEnv, "commit", "-m", "first")

	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello\nworld\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGitCmd(t, dir, fixtureCommitEnv, "add", "hello.txt")
	runGitCmd(t, dir, fixtureCommitEnv, "commit", "-m", "second")

	repo, err := OpenRepo(ctx, dir)
	if err != nil {
		t.Fatalf("OpenRepo: %v", err)
	}
	secondHash := headHash(t, dir)

	detail, err := repo.FetchCommitDetail(ctx, secondHash)
	if err != nil {
		t.Fatalf("FetchCommitDetail: %v", err)
	}
	if len(detail.Files) != 1 {
		t.Fatalf("Files = %v, want 1 file", detail.Files)
	}
	f := detail.Files[0]
	if f.Path != "hello.txt" || f.Status != StatusModified {
		t.Errorf("file = %+v, want hello.txt modified", f)
	}
	if f.Additions != 1 || f.Deletions != 0 {
		t.Errorf("Additions=%d Deletions=%d, want 1/0", f.Additions, f.Deletions)
	}
}

// TestFetchCommitDetailShowsACopiedFileAsAdded pins down that a commit diff
// passes no copy-detection flag: copy.txt is a brand-new file whose content
// happens to match an existing one, and it reads as a plain added file with
// all of its lines, not as a near-empty delta against source.txt.
func TestFetchCommitDetailShowsACopiedFileAsAdded(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	runGitCmd(t, dir, fixtureCommitEnv, "init", "-b", "main")
	runGitCmd(t, dir, fixtureCommitEnv, "config", "user.name", "Test Author")
	runGitCmd(t, dir, fixtureCommitEnv, "config", "user.email", "test@example.com")
	content := []byte("line one\nline two\nline three\nline four\n")
	if err := os.WriteFile(filepath.Join(dir, "source.txt"), content, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGitCmd(t, dir, fixtureCommitEnv, "add", "source.txt")
	runGitCmd(t, dir, fixtureCommitEnv, "commit", "-m", "add source.txt")

	if err := os.WriteFile(filepath.Join(dir, "copy.txt"), content, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGitCmd(t, dir, fixtureCommitEnv, "add", "copy.txt")
	runGitCmd(t, dir, fixtureCommitEnv, "commit", "-m", "copy source.txt to copy.txt")

	repo, err := OpenRepo(ctx, dir)
	if err != nil {
		t.Fatalf("OpenRepo: %v", err)
	}
	detail, err := repo.FetchCommitDetail(ctx, headHash(t, dir))
	if err != nil {
		t.Fatalf("FetchCommitDetail: %v", err)
	}
	if len(detail.Files) != 1 {
		t.Fatalf("Files = %v, want 1 file", detail.Files)
	}
	f := detail.Files[0]
	if f.Path != "copy.txt" || f.OldPath != "" || f.Status != StatusAdded {
		t.Errorf("file = %+v, want copy.txt added with no source", f)
	}
	if f.Additions != 4 || f.Deletions != 0 {
		t.Errorf("Additions=%d Deletions=%d, want 4/0 — the whole file", f.Additions, f.Deletions)
	}
}

// TestFetchCommitDetailDetectsRenameWithRenamesOff pins the explicit -M in
// diffArgs: the fixture turns diff.renames off, which runGit inherits, so
// without the flag a moved file would come back as a delete plus an add.
func TestFetchCommitDetailDetectsRenameWithRenamesOff(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	runGitCmd(t, dir, fixtureCommitEnv, "init", "-b", "main")
	runGitCmd(t, dir, fixtureCommitEnv, "config", "user.name", "Test Author")
	runGitCmd(t, dir, fixtureCommitEnv, "config", "user.email", "test@example.com")
	runGitCmd(t, dir, fixtureCommitEnv, "config", "diff.renames", "false")
	if err := os.WriteFile(filepath.Join(dir, "source.txt"), []byte("one\ntwo\nthree\nfour\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGitCmd(t, dir, fixtureCommitEnv, "add", "source.txt")
	runGitCmd(t, dir, fixtureCommitEnv, "commit", "-m", "add source.txt")

	runGitCmd(t, dir, fixtureCommitEnv, "mv", "source.txt", "moved.txt")
	if err := os.WriteFile(filepath.Join(dir, "moved.txt"), []byte("one\ntwo\nthree\nFOUR\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGitCmd(t, dir, fixtureCommitEnv, "add", "moved.txt")
	runGitCmd(t, dir, fixtureCommitEnv, "commit", "-m", "move source.txt to moved.txt")

	repo, err := OpenRepo(ctx, dir)
	if err != nil {
		t.Fatalf("OpenRepo: %v", err)
	}
	detail, err := repo.FetchCommitDetail(ctx, headHash(t, dir))
	if err != nil {
		t.Fatalf("FetchCommitDetail: %v", err)
	}
	if len(detail.Files) != 1 {
		t.Fatalf("Files = %v, want 1 file", detail.Files)
	}
	f := detail.Files[0]
	if f.Path != "moved.txt" || f.OldPath != "source.txt" || f.Status != StatusRenamed {
		t.Errorf("file = %+v, want moved.txt renamed from source.txt", f)
	}
	if f.Additions != 1 || f.Deletions != 1 {
		t.Errorf("Additions=%d Deletions=%d, want 1/1 — only the edit made in passing", f.Additions, f.Deletions)
	}
}
