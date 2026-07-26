package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestFetchCommitDetail(t *testing.T) {
	repo, hashes := initFixtureRepo(t, 1)
	ctx := context.Background()

	// Replace the single commit with one that has a multi-line body, so we
	// can verify %B is captured in full.
	dir := repo.Path
	cmd := exec.CommandContext(ctx, "git", "commit", "--amend", "--allow-empty", "-m", "subject line\n\nbody line one\nbody line two")
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(),
		"GIT_AUTHOR_NAME=Test Author",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test Author",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit --amend: %v\n%s", err, out)
	}

	headCmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	headCmd.Dir = dir
	headOut, err := headCmd.Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	amendedHash := string(headOut[:40])
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

	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		cmd.Env = append(cmd.Environ(),
			"GIT_AUTHOR_NAME=Test Author",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test Author",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	run("init", "-b", "main")
	run("config", "user.name", "Test Author")
	run("config", "user.email", "test@example.com")
	run("commit", "--allow-empty", "-m", "base")
	run("checkout", "-b", "feature")
	run("commit", "--allow-empty", "-m", "feature work")
	run("checkout", "main")
	run("merge", "--no-ff", "-m", "merge feature", "feature")

	repo, err := OpenRepo(ctx, dir)
	if err != nil {
		t.Fatalf("OpenRepo: %v", err)
	}

	headCmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	headCmd.Dir = dir
	headOut, err := headCmd.Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	mergeHash := string(headOut[:40])

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

	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		cmd.Env = append(cmd.Environ(),
			"GIT_AUTHOR_NAME=Test Author", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test Author", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	run("init", "-b", "main")
	run("config", "user.name", "Test Author")
	run("config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello\nworld\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	run("add", "hello.txt")
	run("commit", "-m", "root commit")

	repo, err := OpenRepo(ctx, dir)
	if err != nil {
		t.Fatalf("OpenRepo: %v", err)
	}
	headOut, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	rootHash := string(headOut[:40])

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

	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		cmd.Env = append(cmd.Environ(),
			"GIT_AUTHOR_NAME=Test Author", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test Author", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	run("init", "-b", "main")
	run("config", "user.name", "Test Author")
	run("config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	run("add", "hello.txt")
	run("commit", "-m", "first")

	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello\nworld\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	run("add", "hello.txt")
	run("commit", "-m", "second")

	repo, err := OpenRepo(ctx, dir)
	if err != nil {
		t.Fatalf("OpenRepo: %v", err)
	}
	headOut, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	secondHash := string(headOut[:40])

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
