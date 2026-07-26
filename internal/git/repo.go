package git

import (
	"context"
	"fmt"
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
