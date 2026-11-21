package git

import (
	"context"
	"testing"
)

// fixtureCommitEnv is the fixed author/committer identity and timestamp
// used by initFixtureRepo, so commit hashes and dates are deterministic.
var fixtureCommitEnv = []string{
	"GIT_AUTHOR_NAME=Test Author",
	"GIT_AUTHOR_EMAIL=test@example.com",
	"GIT_COMMITTER_NAME=Test Author",
	"GIT_COMMITTER_EMAIL=test@example.com",
	"GIT_AUTHOR_DATE=2024-01-01T12:00:00+00:00",
	"GIT_COMMITTER_DATE=2024-01-01T12:00:00+00:00",
}

// initFixtureRepo creates a temp git repo with a small, deterministic
// history: n commits, each with a distinct subject line.
func initFixtureRepo(t *testing.T, n int) (*Repo, []string) {
	t.Helper()
	dir := t.TempDir()
	ctx := context.Background()

	runGitCmd(t, dir, fixtureCommitEnv, "init", "-b", "main")
	runGitCmd(t, dir, fixtureCommitEnv, "config", "user.name", "Test Author")
	runGitCmd(t, dir, fixtureCommitEnv, "config", "user.email", "test@example.com")

	var hashes []string
	for i := 0; i < n; i++ {
		subject := "commit " + string(rune('A'+i))
		runGitCmd(t, dir, fixtureCommitEnv, "commit", "--allow-empty", "-m", subject)
		hashes = append(hashes, headHash(t, dir))
	}
	// hashes[0] is the oldest commit (first created); git log lists newest
	// first, so the expected order is the reverse of creation order.
	reversed := make([]string, len(hashes))
	for i, h := range hashes {
		reversed[len(hashes)-1-i] = h
	}

	repo, err := OpenRepo(ctx, dir)
	if err != nil {
		t.Fatalf("OpenRepo: %v", err)
	}
	return repo, reversed
}

func TestOpenRepoRejectsNonRepo(t *testing.T) {
	dir := t.TempDir()
	if _, err := OpenRepo(context.Background(), dir); err == nil {
		t.Fatal("expected error opening non-repo directory")
	}
}

func TestHasCommitsOnEmptyRepo(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	runGitCmd(t, dir, nil, "init", "-b", "main")

	repo, err := OpenRepo(ctx, dir)
	if err != nil {
		t.Fatalf("OpenRepo: %v", err)
	}

	has, err := repo.HasCommits(ctx)
	if err != nil {
		t.Fatalf("HasCommits: %v", err)
	}
	if has {
		t.Error("HasCommits = true for a freshly initialized repo, want false")
	}
}

func TestHasCommitsAfterFirstCommit(t *testing.T) {
	repo, _ := initFixtureRepo(t, 1)
	has, err := repo.HasCommits(context.Background())
	if err != nil {
		t.Fatalf("HasCommits: %v", err)
	}
	if !has {
		t.Error("HasCommits = false after a commit exists, want true")
	}
}

func TestOpenRepoWithHead(t *testing.T) {
	ctx := context.Background()

	if _, _, err := OpenRepoWithHead(ctx, t.TempDir()); err == nil {
		t.Error("expected an error for a non-repo directory")
	}

	empty := t.TempDir()
	runGitCmd(t, empty, nil, "init", "-b", "main")
	if _, has, err := OpenRepoWithHead(ctx, empty); err != nil || has {
		t.Errorf("empty repo: has = %v, err = %v, want false, nil", has, err)
	}

	repo, _ := initFixtureRepo(t, 1)
	if _, has, err := OpenRepoWithHead(ctx, repo.Path); err != nil || !has {
		t.Errorf("repo with a commit: has = %v, err = %v, want true, nil", has, err)
	}
}

func TestFetchLogBatch(t *testing.T) {
	repo, wantHashesNewestFirst := initFixtureRepo(t, 5)
	ctx := context.Background()

	commits, err := repo.FetchLogBatch(ctx, 0, 10)
	if err != nil {
		t.Fatalf("FetchLogBatch: %v", err)
	}
	if len(commits) != 5 {
		t.Fatalf("got %d commits, want 5", len(commits))
	}

	for i, c := range commits {
		if c.Hash != wantHashesNewestFirst[i] {
			t.Errorf("commit %d: hash = %s, want %s", i, c.Hash, wantHashesNewestFirst[i])
		}
		if c.AuthorName != "Test Author" {
			t.Errorf("commit %d: author name = %q, want %q", i, c.AuthorName, "Test Author")
		}
		if c.AuthorEmail != "test@example.com" {
			t.Errorf("commit %d: author email = %q", i, c.AuthorEmail)
		}
		if c.AuthorDate.IsZero() {
			t.Errorf("commit %d: author date not parsed", i)
		}
	}

	// Subjects are newest-first: commit E, D, C, B, A.
	wantSubjects := []string{"commit E", "commit D", "commit C", "commit B", "commit A"}
	for i, c := range commits {
		if c.Subject != wantSubjects[i] {
			t.Errorf("commit %d: subject = %q, want %q", i, c.Subject, wantSubjects[i])
		}
	}

	// Parent linkage: newest commit's parent is the next one down.
	for i := 0; i < len(commits)-1; i++ {
		if len(commits[i].Parents) != 1 || commits[i].Parents[0] != commits[i+1].Hash {
			t.Errorf("commit %d: parents = %v, want [%s]", i, commits[i].Parents, commits[i+1].Hash)
		}
	}
	if len(commits[len(commits)-1].Parents) != 0 {
		t.Errorf("root commit should have no parents, got %v", commits[len(commits)-1].Parents)
	}
}

func TestFetchLogBatchPaging(t *testing.T) {
	repo, wantHashesNewestFirst := initFixtureRepo(t, 5)
	ctx := context.Background()

	first, err := repo.FetchLogBatch(ctx, 0, 2)
	if err != nil {
		t.Fatalf("FetchLogBatch(0,2): %v", err)
	}
	if len(first) != 2 {
		t.Fatalf("got %d commits, want 2", len(first))
	}

	second, err := repo.FetchLogBatch(ctx, 2, 2)
	if err != nil {
		t.Fatalf("FetchLogBatch(2,2): %v", err)
	}
	if len(second) != 2 {
		t.Fatalf("got %d commits, want 2", len(second))
	}

	got := []string{first[0].Hash, first[1].Hash, second[0].Hash, second[1].Hash}
	want := wantHashesNewestFirst[:4]
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("batch position %d: hash = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestCommitCount(t *testing.T) {
	repo, _ := initFixtureRepo(t, 7)
	count, err := repo.CommitCount(context.Background())
	if err != nil {
		t.Fatalf("CommitCount: %v", err)
	}
	if count != 7 {
		t.Errorf("CommitCount = %d, want 7", count)
	}
}

func TestParseLogLineRefs(t *testing.T) {
	repo, wantHashesNewestFirst := initFixtureRepo(t, 1)
	ctx := context.Background()

	commits, err := repo.FetchLogBatch(ctx, 0, 1)
	if err != nil {
		t.Fatalf("FetchLogBatch: %v", err)
	}
	if len(commits) != 1 {
		t.Fatalf("got %d commits, want 1", len(commits))
	}
	c := commits[0]
	if c.Hash != wantHashesNewestFirst[0] {
		t.Fatalf("hash mismatch")
	}
	found := false
	for _, ref := range c.Refs {
		if ref == "HEAD -> main" || ref == "main" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected refs to include main/HEAD, got %v", c.Refs)
	}
}
