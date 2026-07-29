package git

import (
	"context"
	"fmt"
)

// ResolveRevision resolves a user-supplied revision (a full or abbreviated
// hash, a branch, a tag, HEAD~2, etc.) to a full commit hash. --end-of-options
// keeps a revision beginning with "-" from being read as a git option, and
// the ^{commit} suffix peels annotated tags and rejects revisions that name
// a blob or tree rather than a commit.
func (r *Repo) ResolveRevision(ctx context.Context, rev string) (string, error) {
	out, err := runGitTrimmed(ctx, r.Path, "rev-parse", "--verify", "--quiet", "--end-of-options", rev+"^{commit}")
	if err != nil {
		if isExitCode(err, 1) {
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
//
// This deliberately scans AllCommitHashes rather than computing
// `git rev-list --count hash..HEAD`, which would be a single git
// invocation instead of a full-history fetch plus a linear scan.
// rev-list --count measures reachability distance, not git log's
// traversal order, and the two diverge once merges are involved — using
// it here would silently place the cursor on the wrong commit in any
// history with merges, rather than fail loudly. The cost of the scan is
// the same `git log --format=%H` query AllCommitHashes already serves
// list search with, and this only runs once per CLI `gitl <rev>` jump.
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
