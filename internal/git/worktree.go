package git

import (
	"bytes"
	"context"
	"fmt"
)

// EntryKind distinguishes a real commit from the two synthetic entries gitl
// can show above real history. The zero value is EntryCommit, so an
// unadorned CommitDetail still means "a real commit".
type EntryKind int

const (
	EntryCommit EntryKind = iota
	EntryStaged
	EntryWorktree
)

// WorktreeEntry is one synthetic list entry (working tree or staged
// changes) together with the diff behind it. It carries no commit metadata
// because there is none.
type WorktreeEntry struct {
	Kind  EntryKind
	Files []File
}

// HasStagedChanges reports whether the index differs from HEAD, via `git
// diff --cached --quiet`'s exit code. Note the polarity is the opposite of
// HasCommits' rev-parse check: here exit 1 means "yes, there are changes".
func (r *Repo) HasStagedChanges(ctx context.Context) (bool, error) {
	_, differs, err := runGitDiffExit(ctx, r.Path, "diff", "--cached", "--quiet")
	return differs, err
}

// FetchWorktreeEntry returns the diff behind a single synthetic entry kind
// (EntryStaged or EntryWorktree). Returns nil files when there is nothing to
// show.
func (r *Repo) FetchWorktreeEntry(ctx context.Context, kind EntryKind) ([]File, error) {
	switch kind {
	case EntryStaged:
		return r.stagedDiff(ctx)
	case EntryWorktree:
		return r.worktreeDiff(ctx)
	default:
		return nil, fmt.Errorf("FetchWorktreeEntry: not a synthetic entry kind: %v", kind)
	}
}

// FetchWorktreeEntries returns every synthetic entry that currently has
// something to show, working tree first (furthest from HEAD) then staged.
// Entries with no changes are omitted entirely, mirroring how `git status`
// omits empty sections.
func (r *Repo) FetchWorktreeEntries(ctx context.Context) ([]WorktreeEntry, error) {
	var entries []WorktreeEntry

	worktreeFiles, err := r.worktreeDiff(ctx)
	if err != nil {
		return nil, err
	}
	if len(worktreeFiles) > 0 {
		entries = append(entries, WorktreeEntry{Kind: EntryWorktree, Files: worktreeFiles})
	}

	stagedFiles, err := r.stagedDiff(ctx)
	if err != nil {
		return nil, err
	}
	if len(stagedFiles) > 0 {
		entries = append(entries, WorktreeEntry{Kind: EntryStaged, Files: stagedFiles})
	}

	return entries, nil
}

// EntryDetail wraps a synthetic entry's files as a CommitDetail so the
// detail view renders it with exactly the same machinery a real commit
// uses; the commit metadata fields are deliberately left zero.
func EntryDetail(e WorktreeEntry) *CommitDetail {
	return &CommitDetail{Kind: e.Kind, Files: e.Files}
}

// stagedDiff diffs the index against HEAD. On an unborn HEAD (no commits
// yet) git already treats this as a diff against the empty tree, so no
// explicit fallback is needed.
func (r *Repo) stagedDiff(ctx context.Context) ([]File, error) {
	out, err := runGit(ctx, r.Path, "diff", "--cached", "--no-color", "-U3")
	if err != nil {
		return nil, err
	}
	return ParseUnifiedDiff(bytes.NewReader(out))
}

// worktreeDiff diffs tracked files against the index. Untracked files are
// added separately (see untracked.go).
func (r *Repo) worktreeDiff(ctx context.Context) ([]File, error) {
	out, err := runGit(ctx, r.Path, "diff", "--no-color", "-U3")
	if err != nil {
		return nil, err
	}
	return ParseUnifiedDiff(bytes.NewReader(out))
}
