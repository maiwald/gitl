package git

import (
	"context"
	"testing"
)

// commitSpec describes one commit to create in a search fixture repo, with
// per-commit author/subject/body so author-only and body-only matches can
// be tested precisely.
type commitSpec struct {
	author  string
	subject string
	body    string
}

// initSearchFixtureRepo creates a temp repo with one commit per spec, in
// order, and returns the repo plus the resulting hashes newest-first
// (matching git log's default order).
func initSearchFixtureRepo(t *testing.T, specs []commitSpec) (*Repo, []string) {
	t.Helper()
	dir := t.TempDir()
	ctx := context.Background()

	runGitCmd(t, dir, nil, "init", "-b", "main")
	runGitCmd(t, dir, nil, "config", "user.name", "Test Author")
	runGitCmd(t, dir, nil, "config", "user.email", "test@example.com")

	var hashes []string
	for _, spec := range specs {
		env := []string{
			"GIT_AUTHOR_NAME=" + spec.author,
			"GIT_AUTHOR_EMAIL=author@example.com",
			"GIT_COMMITTER_NAME=" + spec.author,
			"GIT_COMMITTER_EMAIL=author@example.com",
			"GIT_AUTHOR_DATE=2024-01-01T12:00:00+00:00",
			"GIT_COMMITTER_DATE=2024-01-01T12:00:00+00:00",
		}
		args := []string{"commit", "--allow-empty", "-m", spec.subject}
		if spec.body != "" {
			args = append(args, "-m", spec.body)
		}
		runGitCmd(t, dir, env, args...)
		hashes = append(hashes, headHash(t, dir))
	}

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

func TestSearchCommitHashesMatchesAuthor(t *testing.T) {
	repo, hashes := initSearchFixtureRepo(t, []commitSpec{
		{author: "Alice", subject: "first commit"},
		{author: "Bob", subject: "second commit"},
	})
	got, err := repo.SearchCommitHashes(context.Background(), "Alice")
	if err != nil {
		t.Fatalf("SearchCommitHashes: %v", err)
	}
	if len(got) != 1 || !got[hashes[1]] {
		t.Errorf("got %v, want only %s (Alice's commit)", got, hashes[1])
	}
}

func TestSearchCommitHashesMatchesMessageBody(t *testing.T) {
	repo, hashes := initSearchFixtureRepo(t, []commitSpec{
		{author: "Alice", subject: "subject one", body: "body with UNIQUEMARKER inside"},
		{author: "Alice", subject: "subject two"},
	})
	got, err := repo.SearchCommitHashes(context.Background(), "UNIQUEMARKER")
	if err != nil {
		t.Fatalf("SearchCommitHashes: %v", err)
	}
	if len(got) != 1 || !got[hashes[1]] {
		t.Errorf("got %v, want only %s (matched via body, not subject)", got, hashes[1])
	}
}

func TestSearchCommitHashesUnionsAuthorAndMessage(t *testing.T) {
	repo, hashes := initSearchFixtureRepo(t, []commitSpec{
		{author: "TARGETNAME", subject: "unrelated subject"},
		{author: "Someone Else", subject: "contains TARGETNAME in subject"},
		{author: "Someone Else", subject: "irrelevant"},
	})
	got, err := repo.SearchCommitHashes(context.Background(), "TARGETNAME")
	if err != nil {
		t.Fatalf("SearchCommitHashes: %v", err)
	}
	want := map[string]bool{hashes[2]: true, hashes[1]: true}
	if len(got) != len(want) || !got[hashes[2]] || !got[hashes[1]] {
		t.Errorf("got %v, want union %v (author-only match and message-only match, not just their AND)", got, want)
	}
}

func TestSearchCommitHashesCaseInsensitive(t *testing.T) {
	repo, hashes := initSearchFixtureRepo(t, []commitSpec{
		{author: "Alice", subject: "Fixes THE Bug"},
	})
	got, err := repo.SearchCommitHashes(context.Background(), "the bug")
	if err != nil {
		t.Fatalf("SearchCommitHashes: %v", err)
	}
	if len(got) != 1 || !got[hashes[0]] {
		t.Errorf("got %v, want case-insensitive match on %s", got, hashes[0])
	}
}

func TestSearchCommitHashesLiteralNotRegex(t *testing.T) {
	repo, _ := initSearchFixtureRepo(t, []commitSpec{
		{author: "Alice", subject: "contains axb but not the literal pattern"},
	})
	got, err := repo.SearchCommitHashes(context.Background(), "a.b")
	if err != nil {
		t.Fatalf("SearchCommitHashes: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want no matches (literal \"a.b\" is not present, regex wildcard must not apply)", got)
	}
}

func TestSearchCommitHashesEmptyTermReturnsEmptySet(t *testing.T) {
	repo, _ := initSearchFixtureRepo(t, []commitSpec{{author: "Alice", subject: "subject"}})
	got, err := repo.SearchCommitHashes(context.Background(), "")
	if err != nil {
		t.Fatalf("SearchCommitHashes: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("got %v, want empty non-nil set", got)
	}
}

func TestSearchCommitHashesNoMatches(t *testing.T) {
	repo, _ := initSearchFixtureRepo(t, []commitSpec{{author: "Alice", subject: "subject"}})
	got, err := repo.SearchCommitHashes(context.Background(), "NoSuchTermAnywhere")
	if err != nil {
		t.Fatalf("SearchCommitHashes: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want no matches", got)
	}
}

func TestAllCommitHashesMatchesLogOrder(t *testing.T) {
	repo, wantHashesNewestFirst := initSearchFixtureRepo(t, []commitSpec{
		{author: "Alice", subject: "first"},
		{author: "Alice", subject: "second"},
		{author: "Alice", subject: "third"},
	})
	got, err := repo.AllCommitHashes(context.Background())
	if err != nil {
		t.Fatalf("AllCommitHashes: %v", err)
	}
	if len(got) != len(wantHashesNewestFirst) {
		t.Fatalf("got %d hashes, want %d", len(got), len(wantHashesNewestFirst))
	}
	for i, h := range got {
		if h != wantHashesNewestFirst[i] {
			t.Errorf("hash %d = %s, want %s", i, h, wantHashesNewestFirst[i])
		}
	}
}
