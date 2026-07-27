package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFetchCommitDetail(t *testing.T) {
	repo, hashes := initFixtureRepo(t, 1)
	ctx := context.Background()

	// Replace the single commit with one that has a multi-line body, so we
	// can verify %B is captured in full.
	dir := repo.Path
	runGitCmd(t, dir, fixtureCommitEnv, "commit", "--amend", "--allow-empty", "-m", "subject line\n\nbody line one\nbody line two")
	amendedHash := headHash(t, dir)
	_ = hashes // original hash is stale after amend; not used further

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
