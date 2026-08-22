package git

import (
	"context"
	"fmt"
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

func TestHasWorktreeChangesFalseWhenClean(t *testing.T) {
	repo := initFixtureRepoWithFile(t)
	has, err := repo.HasWorktreeChanges(context.Background())
	if err != nil {
		t.Fatalf("HasWorktreeChanges: %v", err)
	}
	if has {
		t.Error("HasWorktreeChanges = true on a clean repo, want false")
	}
}

func TestHasWorktreeChangesTrueAfterUnstagedEdit(t *testing.T) {
	repo := initFixtureRepoWithFile(t)
	dir := repo.Path
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("line one\nline two\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	has, err := repo.HasWorktreeChanges(context.Background())
	if err != nil {
		t.Fatalf("HasWorktreeChanges: %v", err)
	}
	if !has {
		t.Error("HasWorktreeChanges = false after an unstaged edit, want true")
	}
}

func TestHasWorktreeChangesTrueForUntrackedFileOnly(t *testing.T) {
	repo := initFixtureRepoWithFile(t)
	dir := repo.Path
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	has, err := repo.HasWorktreeChanges(context.Background())
	if err != nil {
		t.Fatalf("HasWorktreeChanges: %v", err)
	}
	if !has {
		t.Error("HasWorktreeChanges = false with only an untracked file present, want true")
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

// TestFetchWorktreeEntryStagedDetectsCopyFromUntouchedSource pins down that
// stagedDiff uses --find-copies-harder specifically (not plain
// --find-copies): source.txt isn't itself staged, so only "-harder" scans
// it as a candidate copy source. See stagedDiff's doc comment for the cost
// tradeoff this flag accepts.
func TestFetchWorktreeEntryStagedDetectsCopyFromUntouchedSource(t *testing.T) {
	repo := initFixtureRepoWithFile(t)
	dir := repo.Path
	content := []byte("line one\nline two\nline three\nline four\n")
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), content, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGitCmd(t, dir, fixtureCommitEnv, "add", "tracked.txt")
	runGitCmd(t, dir, fixtureCommitEnv, "commit", "-m", "give tracked.txt real content")

	if err := os.WriteFile(filepath.Join(dir, "copy.txt"), content, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGitCmd(t, dir, fixtureCommitEnv, "add", "copy.txt")

	files, err := repo.FetchWorktreeEntry(context.Background(), EntryStaged)
	if err != nil {
		t.Fatalf("FetchWorktreeEntry(EntryStaged): %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got files = %+v, want one file", files)
	}
	f := files[0]
	if f.Path != "copy.txt" || f.OldPath != "tracked.txt" || f.Status != StatusCopied {
		t.Errorf("file = %+v, want copy.txt copied from tracked.txt", f)
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

func TestWorktreeDiffIncludesUntrackedFile(t *testing.T) {
	repo := initFixtureRepoWithFile(t)
	dir := repo.Path
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("brand new\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	files, err := repo.FetchWorktreeEntry(context.Background(), EntryWorktree)
	if err != nil {
		t.Fatalf("FetchWorktreeEntry(EntryWorktree): %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1: %+v", len(files), files)
	}
	f := files[0]
	if f.Path != "new.txt" {
		t.Errorf("Path = %q, want new.txt", f.Path)
	}
	if f.OldPath != "" {
		t.Errorf("OldPath = %q, want empty", f.OldPath)
	}
	if f.Status != StatusAdded {
		t.Errorf("Status = %v, want StatusAdded", f.Status)
	}
	if f.Additions != 1 {
		t.Errorf("Additions = %d, want 1", f.Additions)
	}
}

func TestWorktreeDiffIncludesNestedUntrackedFile(t *testing.T) {
	repo := initFixtureRepoWithFile(t)
	dir := repo.Path
	if err := os.MkdirAll(filepath.Join(dir, "sub", "dir"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "dir", "nested.txt"), []byte("deep\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	files, err := repo.FetchWorktreeEntry(context.Background(), EntryWorktree)
	if err != nil {
		t.Fatalf("FetchWorktreeEntry(EntryWorktree): %v", err)
	}
	if len(files) != 1 || files[0].Path != "sub/dir/nested.txt" {
		t.Fatalf("got files = %+v, want one file sub/dir/nested.txt", files)
	}
}

func TestWorktreeDiffExcludesGitignoredFile(t *testing.T) {
	repo := initFixtureRepoWithFile(t)
	dir := repo.Path
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored.txt\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGitCmd(t, dir, fixtureCommitEnv, "add", ".gitignore")
	runGitCmd(t, dir, fixtureCommitEnv, "commit", "-m", "add gitignore")
	if err := os.WriteFile(filepath.Join(dir, "ignored.txt"), []byte("should not appear\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	files, err := repo.FetchWorktreeEntry(context.Background(), EntryWorktree)
	if err != nil {
		t.Fatalf("FetchWorktreeEntry(EntryWorktree): %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("got files = %+v, want none (ignored.txt should be excluded)", files)
	}
}

func TestWorktreeDiffUntrackedFileBeyondCapDegradesGracefully(t *testing.T) {
	repo := initFixtureRepoWithFile(t)
	dir := repo.Path

	// Names sort so that files past maxUntrackedDiffs are the ones without
	// individually-diffed content, matching ls-files -z's lexical order.
	total := maxUntrackedDiffs + 1
	for i := 0; i < total; i++ {
		name := fmt.Sprintf("f%04d.txt", i)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(fmt.Sprintf("content %d\n", i)), 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", name, err)
		}
	}

	files, err := repo.FetchWorktreeEntry(context.Background(), EntryWorktree)
	if err != nil {
		t.Fatalf("FetchWorktreeEntry(EntryWorktree): %v", err)
	}
	if len(files) != total {
		t.Fatalf("got %d files, want %d", len(files), total)
	}

	last := files[total-1]
	if last.Path != fmt.Sprintf("f%04d.txt", total-1) {
		t.Fatalf("last file = %q, want f%04d.txt", last.Path, total-1)
	}
	if last.Status != StatusAdded {
		t.Errorf("last file Status = %v, want StatusAdded", last.Status)
	}
	if len(last.Hunks) != 0 || last.Additions != 0 {
		t.Errorf("last file beyond the cap should be hunk-less, got %+v", last)
	}

	first := files[0]
	if len(first.Hunks) == 0 {
		t.Errorf("first file within the cap should have hunks, got %+v", first)
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

// dirtyTwoFileRepo returns a repo with two tracked files, both edited in the
// working tree and one of them also staged, plus an untracked file — enough
// for the pathspec tests below to tell "limited to a.txt" apart from
// "everything".
func dirtyTwoFileRepo(t *testing.T) *Repo {
	t.Helper()
	dir := t.TempDir()
	ctx := context.Background()

	runGitCmd(t, dir, fixtureCommitEnv, "init", "-b", "main")
	runGitCmd(t, dir, fixtureCommitEnv, "config", "user.name", "Test Author")
	runGitCmd(t, dir, fixtureCommitEnv, "config", "user.email", "test@example.com")
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("one\n"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	runGitCmd(t, dir, fixtureCommitEnv, "add", "a.txt", "b.txt")
	runGitCmd(t, dir, fixtureCommitEnv, "commit", "-m", "add a.txt and b.txt")

	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("one\ntwo\n"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	runGitCmd(t, dir, fixtureCommitEnv, "add", "b.txt")
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	repo, err := OpenRepo(ctx, dir)
	if err != nil {
		t.Fatalf("OpenRepo: %v", err)
	}
	return repo
}

func TestFetchWorktreeEntryLimitedToPaths(t *testing.T) {
	repo := dirtyTwoFileRepo(t)
	ctx := context.Background()

	files, err := repo.FetchWorktreeEntry(ctx, EntryWorktree, "a.txt")
	if err != nil {
		t.Fatalf("FetchWorktreeEntry(EntryWorktree): %v", err)
	}
	if len(files) != 1 || files[0].Path != "a.txt" {
		t.Fatalf("worktree files = %+v, want only a.txt", files)
	}

	files, err = repo.FetchWorktreeEntry(ctx, EntryStaged, "a.txt")
	if err != nil {
		t.Fatalf("FetchWorktreeEntry(EntryStaged): %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("staged files = %+v, want none (only b.txt is staged)", files)
	}
}

// TestFetchWorktreeEntryPathspecLimitsUntrackedFiles pins down that the
// pathspec reaches the untracked-file half of the worktree diff too, not just
// the tracked `git diff` it's appended to.
func TestFetchWorktreeEntryPathspecLimitsUntrackedFiles(t *testing.T) {
	repo := dirtyTwoFileRepo(t)
	ctx := context.Background()

	files, err := repo.FetchWorktreeEntry(ctx, EntryWorktree, "untracked.txt")
	if err != nil {
		t.Fatalf("FetchWorktreeEntry(EntryWorktree): %v", err)
	}
	if len(files) != 1 || files[0].Path != "untracked.txt" {
		t.Fatalf("files = %+v, want only untracked.txt", files)
	}

	files, err = repo.FetchWorktreeEntry(ctx, EntryWorktree, "b.txt")
	if err != nil {
		t.Fatalf("FetchWorktreeEntry(EntryWorktree): %v", err)
	}
	for _, f := range files {
		if f.Path == "untracked.txt" {
			t.Fatalf("files = %+v, want untracked.txt excluded by the b.txt pathspec", files)
		}
	}
}

func TestHasChangesLimitedToPaths(t *testing.T) {
	repo := dirtyTwoFileRepo(t)
	ctx := context.Background()

	tests := []struct {
		name  string
		check func(ctx context.Context, paths ...string) (bool, error)
		path  string
		want  bool
	}{
		{"staged, path is staged", repo.HasStagedChanges, "b.txt", true},
		{"staged, path is not staged", repo.HasStagedChanges, "a.txt", false},
		{"worktree, path is dirty", repo.HasWorktreeChanges, "a.txt", true},
		{"worktree, path is clean", repo.HasWorktreeChanges, "clean-nonexistent.txt", false},
		{"worktree, path is untracked", repo.HasWorktreeChanges, "untracked.txt", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			has, err := tt.check(ctx, tt.path)
			if err != nil {
				t.Fatalf("check(%q): %v", tt.path, err)
			}
			if has != tt.want {
				t.Errorf("check(%q) = %v, want %v", tt.path, has, tt.want)
			}
		})
	}
}
