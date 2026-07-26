package git

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Repo represents a git working tree we can query.
type Repo struct {
	Path string
}

// OpenRepo validates that path is inside a git work tree and returns a Repo
// rooted there.
func OpenRepo(ctx context.Context, path string) (*Repo, error) {
	out, err := runGit(ctx, path, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		return nil, fmt.Errorf("not a git repository: %w", err)
	}
	if strings.TrimSpace(string(out)) != "true" {
		return nil, fmt.Errorf("not a git repository: %s", path)
	}
	return &Repo{Path: path}, nil
}

// HasCommits reports whether HEAD resolves to a commit. It returns false,
// not an error, for a freshly initialized repository with no commits yet.
func (r *Repo) HasCommits(ctx context.Context) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--verify", "--quiet", "HEAD")
	cmd.Dir = r.Path
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("git rev-parse --verify --quiet HEAD: %w", err)
	}
	return true, nil
}
