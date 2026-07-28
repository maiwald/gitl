package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// initFixtureRepoWithFile is like initFixtureRepo, but its single commit
// adds a tracked file so tests can dirty the working tree/index afterward.
func initFixtureRepoWithFile(t *testing.T) *Repo {
	t.Helper()
	dir := t.TempDir()
	ctx := context.Background()

	runGitCmd(t, dir, fixtureCommitEnv, "init", "-b", "main")
	runGitCmd(t, dir, fixtureCommitEnv, "config", "user.name", "Test Author")
	runGitCmd(t, dir, fixtureCommitEnv, "config", "user.email", "test@example.com")

	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("line one\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGitCmd(t, dir, fixtureCommitEnv, "add", "tracked.txt")
	runGitCmd(t, dir, fixtureCommitEnv, "commit", "-m", "add tracked.txt")

	repo, err := OpenRepo(ctx, dir)
	if err != nil {
		t.Fatalf("OpenRepo: %v", err)
	}
	return repo
}

func TestHasStagedChangesFalseWhenClean(t *testing.T) {
	repo := initFixtureRepoWithFile(t)
	has, err := repo.HasStagedChanges(context.Background())
	if err != nil {
		t.Fatalf("HasStagedChanges: %v", err)
	}
	if has {
		t.Error("HasStagedChanges = true on a clean repo, want false")
	}
}

func TestHasStagedChangesTrueAfterAdd(t *testing.T) {
	repo := initFixtureRepoWithFile(t)
	dir := repo.Path
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("line one\nline two\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGitCmd(t, dir, fixtureCommitEnv, "add", "tracked.txt")

	has, err := repo.HasStagedChanges(context.Background())
	if err != nil {
		t.Fatalf("HasStagedChanges: %v", err)
	}
	if !has {
		t.Error("HasStagedChanges = false after staging a change, want true")
	}
}

func TestFetchWorktreeEntryStaged(t *testing.T) {
	repo := initFixtureRepoWithFile(t)
	dir := repo.Path
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("line one\nline two\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGitCmd(t, dir, fixtureCommitEnv, "add", "tracked.txt")

	files, err := repo.FetchWorktreeEntry(context.Background(), EntryStaged)
	if err != nil {
		t.Fatalf("FetchWorktreeEntry(EntryStaged): %v", err)
	}
	if len(files) != 1 || files[0].Path != "tracked.txt" {
		t.Fatalf("got files = %+v, want one file tracked.txt", files)
	}
	if files[0].Additions != 1 {
		t.Errorf("Additions = %d, want 1", files[0].Additions)
	}
}

func TestFetchWorktreeEntryWorktree(t *testing.T) {
	repo := initFixtureRepoWithFile(t)
	dir := repo.Path
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("line one\nline two\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	files, err := repo.FetchWorktreeEntry(context.Background(), EntryWorktree)
	if err != nil {
		t.Fatalf("FetchWorktreeEntry(EntryWorktree): %v", err)
	}
	if len(files) != 1 || files[0].Path != "tracked.txt" {
		t.Fatalf("got files = %+v, want one file tracked.txt", files)
	}
}

func TestFetchWorktreeEntriesOmitsEmptyAndOrdersWorktreeFirst(t *testing.T) {
	repo := initFixtureRepoWithFile(t)
	ctx := context.Background()

	entries, err := repo.FetchWorktreeEntries(ctx)
	if err != nil {
		t.Fatalf("FetchWorktreeEntries: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("got %d entries on a clean repo, want 0", len(entries))
	}

	dir := repo.Path
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("line one\nline two\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.txt"), []byte("staged\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGitCmd(t, dir, fixtureCommitEnv, "add", "other.txt")

	entries, err = repo.FetchWorktreeEntries(ctx)
	if err != nil {
		t.Fatalf("FetchWorktreeEntries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2 (worktree + staged): %+v", len(entries), entries)
	}
	if entries[0].Kind != EntryWorktree {
		t.Errorf("entries[0].Kind = %v, want EntryWorktree", entries[0].Kind)
	}
	if entries[1].Kind != EntryStaged {
		t.Errorf("entries[1].Kind = %v, want EntryStaged", entries[1].Kind)
	}
}

func TestEntryDetail(t *testing.T) {
	entry := WorktreeEntry{Kind: EntryStaged, Files: []File{{Path: "a.txt"}}}
	detail := EntryDetail(entry)
	if detail.Kind != EntryStaged {
		t.Errorf("Kind = %v, want EntryStaged", detail.Kind)
	}
	if !detail.IsPseudo() {
		t.Error("IsPseudo() = false, want true")
	}
	if len(detail.Files) != 1 || detail.Files[0].Path != "a.txt" {
		t.Errorf("Files = %+v", detail.Files)
	}
	if detail.Hash != "" {
		t.Errorf("Hash = %q, want empty (no commit metadata)", detail.Hash)
	}
}

func TestFetchCommitDetailIsNotPseudo(t *testing.T) {
	repo, hashes := initFixtureRepo(t, 1)
	detail, err := repo.FetchCommitDetail(context.Background(), hashes[0])
	if err != nil {
		t.Fatalf("FetchCommitDetail: %v", err)
	}
	if detail.IsPseudo() {
		t.Error("IsPseudo() = true for a real commit, want false")
	}
	if detail.Kind != EntryCommit {
		t.Errorf("Kind = %v, want EntryCommit", detail.Kind)
	}
}
