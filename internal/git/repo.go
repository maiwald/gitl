package git

import (
	"context"
	"fmt"
)

// Repo represents a git working tree we can query.
type Repo struct {
	Path string
}

// OpenRepo validates that path is inside a git work tree and returns a Repo
// rooted there.
func OpenRepo(ctx context.Context, path string) (*Repo, error) {
	out, err := runGitTrimmed(ctx, path, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		return nil, fmt.Errorf("not a git repository: %w", err)
	}
	if out != "true" {
		return nil, fmt.Errorf("not a git repository: %s", path)
	}
	return &Repo{Path: path}, nil
}

// OpenRepoWithHead opens the repository at path and reports whether it has a
// commit yet — the two questions every startup asks before choosing what to
// show, in the order that makes "not a git repository" win over any
// HasCommits failure.
func OpenRepoWithHead(ctx context.Context, path string) (repo *Repo, hasCommits bool, err error) {
	repo, err = OpenRepo(ctx, path)
	if err != nil {
		return nil, false, err
	}
	hasCommits, err = repo.HasCommits(ctx)
	if err != nil {
		return nil, false, err
	}
	return repo, hasCommits, nil
}

// HasCommits reports whether HEAD resolves to a commit. It returns false,
// not an error, for a freshly initialized repository with no commits yet.
func (r *Repo) HasCommits(ctx context.Context) (bool, error) {
	_, err := runGit(ctx, r.Path, "rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil {
		if isExitCode(err, 1) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
