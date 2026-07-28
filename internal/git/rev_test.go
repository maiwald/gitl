package git

import (
	"context"
	"testing"
)

func TestResolveRevision(t *testing.T) {
	repo, hashesNewestFirst := initFixtureRepo(t, 5)
	ctx := context.Background()
	dir := repo.Path

	runGitCmd(t, dir, fixtureCommitEnv, "tag", "v1", hashesNewestFirst[2])

	cases := []struct {
		name string
		rev  string
		want string
	}{
		{"full hash", hashesNewestFirst[0], hashesNewestFirst[0]},
		{"abbreviated hash", hashesNewestFirst[0][:8], hashesNewestFirst[0]},
		{"branch", "main", hashesNewestFirst[0]},
		{"tag", "v1", hashesNewestFirst[2]},
		{"relative", "HEAD~2", hashesNewestFirst[2]},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := repo.ResolveRevision(ctx, c.rev)
			if err != nil {
				t.Fatalf("ResolveRevision(%q): %v", c.rev, err)
			}
			if got != c.want {
				t.Errorf("ResolveRevision(%q) = %s, want %s", c.rev, got, c.want)
			}
		})
	}
}

func TestResolveRevisionUnknown(t *testing.T) {
	repo, _ := initFixtureRepo(t, 1)
	_, err := repo.ResolveRevision(context.Background(), "does-not-exist")
	if err == nil {
		t.Fatal("expected error resolving an unknown revision")
	}
}

func TestCommitPosition(t *testing.T) {
	repo, hashesNewestFirst := initFixtureRepo(t, 5)
	ctx := context.Background()

	for i, h := range hashesNewestFirst {
		pos, ok, err := repo.CommitPosition(ctx, h)
		if err != nil {
			t.Fatalf("CommitPosition(%s): %v", h, err)
		}
		if !ok {
			t.Fatalf("CommitPosition(%s): ok = false, want true", h)
		}
		if pos != i {
			t.Errorf("CommitPosition(%s) = %d, want %d", h, pos, i)
		}
	}
}

func TestCommitPositionUnreachable(t *testing.T) {
	repo, _ := initFixtureRepo(t, 3)
	dir := repo.Path
	ctx := context.Background()

	runGitCmd(t, dir, fixtureCommitEnv, "checkout", "-b", "side", "HEAD~1")
	runGitCmd(t, dir, fixtureCommitEnv, "commit", "--allow-empty", "-m", "side commit")
	sideHash := headHash(t, dir)
	runGitCmd(t, dir, fixtureCommitEnv, "checkout", "main")

	_, ok, err := repo.CommitPosition(ctx, sideHash)
	if err != nil {
		t.Fatalf("CommitPosition(%s): %v", sideHash, err)
	}
	if ok {
		t.Errorf("CommitPosition(%s): ok = true, want false (unreachable from HEAD)", sideHash)
	}
}
