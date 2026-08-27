package git

import (
	"context"
)

// CommitPosition returns the zero-based position of hash within HEAD's
// history, in the same order FetchLogBatch produces — the order ListModel's
// indices are keyed to. ok is false when hash isn't reachable from HEAD at
// all, in which case there is no list position to place a cursor on.
//
// It scans AllCommitHashes rather than taking the single-invocation
// `git rev-list --count hash..HEAD`, which measures reachability distance
// rather than git log's traversal order: the two diverge once merges are
// involved, and it would silently place the cursor on the wrong commit instead
// of failing loudly. The scan runs once per piped patch that named a commit.
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
