package git

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
)

// ResolveRevision resolves a user-supplied revision (a full or abbreviated
// hash, a branch, a tag, HEAD~2, etc.) to a full commit hash. --end-of-options
// keeps a revision beginning with "-" from being read as a git option, and
// the ^{commit} suffix peels annotated tags and rejects revisions that name
// a blob or tree rather than a commit.
func (r *Repo) ResolveRevision(ctx context.Context, rev string) (string, error) {
	out, err := runGitTrimmed(ctx, r.Path, "rev-parse", "--verify", "--quiet", "--end-of-options", rev+"^{commit}")
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return "", fmt.Errorf("unknown revision: %s", rev)
		}
		return "", err
	}
	return out, nil
}

// CommitPosition returns the zero-based position of hash within HEAD's
// history, in the same order FetchLogBatch produces — the order ListModel's
// indices are keyed to. ok is false when hash isn't reachable from HEAD at
// all (e.g. a commit that only exists on another branch), in which case
// there is no list position to place a cursor on.
func (r *Repo) CommitPosition(ctx context.Context, hash string) (pos int, ok bool, err error) {
	hashes, err := r.AllCommitHashes(ctx)
	if err != nil {
		return 0, false, err
	}
	for i, h := range hashes {
		if h == hash {
			return i, true, nil
		}
	}
	return 0, false, nil
}
