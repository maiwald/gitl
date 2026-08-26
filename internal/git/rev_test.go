package git

import (
	"context"
	"testing"
)

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
