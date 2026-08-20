package git

import (
	"bytes"
	"context"
	"fmt"
	"sync"

	"github.com/maiwald/gitl/internal/concurrent"
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
// changes) together with the diff behind it.
type WorktreeEntry struct {
	Kind  EntryKind
	Files []File
}

// HasStagedChanges reports whether the index differs from HEAD, via `git
// diff --cached --quiet`'s exit code.
func (r *Repo) HasStagedChanges(ctx context.Context) (bool, error) {
	_, differs, err := runGitDiffExit(ctx, r.Path, "diff", "--cached", "--quiet")
	return differs, err
}

// HasWorktreeChanges reports whether worktreeDiff would have anything to
// show: tracked files differing from the index, or untracked files. Checked
// separately (rather than just calling worktreeDiff and looking at the
// result) so a CLI `--diff` pre-flight can answer without paying for a full
// untracked-file diff synthesis just to find out the answer is "no".
func (r *Repo) HasWorktreeChanges(ctx context.Context) (bool, error) {
	var trackedDiffers bool
	var untracked []string
	err := concurrent.RunConcurrently(
		func() (err error) { _, trackedDiffers, err = runGitDiffExit(ctx, r.Path, "diff", "--quiet"); return },
		func() (err error) { untracked, err = r.untrackedPaths(ctx); return },
	)
	if err != nil {
		return false, err
	}
	return trackedDiffers || len(untracked) > 0, nil
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
		return nil, fmt.Errorf("not a synthetic entry kind: %v", kind)
	}
}

// FetchWorktreeEntries returns every synthetic entry that currently has
// something to show, working tree first (furthest from HEAD) then staged.
// Entries with no changes are omitted entirely, mirroring how `git status`
// omits empty sections. The two diffs are independent git invocations, so
// they run concurrently rather than one after another.
func (r *Repo) FetchWorktreeEntries(ctx context.Context) ([]WorktreeEntry, error) {
	var worktreeFiles, stagedFiles []File
	err := concurrent.RunConcurrently(
		func() (err error) { worktreeFiles, err = r.worktreeDiff(ctx); return },
		func() (err error) { stagedFiles, err = r.stagedDiff(ctx); return },
	)
	if err != nil {
		return nil, err
	}

	var entries []WorktreeEntry
	if len(worktreeFiles) > 0 {
		entries = append(entries, WorktreeEntry{Kind: EntryWorktree, Files: worktreeFiles})
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

// stagedDiff diffs the index against HEAD with copy detection
// (`--find-copies-harder`, matching `git diff --cached --find-copies-harder`)
// — unlike the worktree diff, a newly staged file genuinely can appear as
// "added" here, so there's something for copy detection to pair against.
// The "-harder" variant (vs. plain --find-copies) also catches a copy whose
// source file wasn't itself touched, at the cost of scanning every
// unmodified file in the tree as a candidate source rather than just the
// files already in the diff — acceptable here since this fetches once per
// detail-view open, not on every keystroke. On an unborn HEAD (no commits
// yet) git already treats this as a diff against the empty tree, so no
// explicit fallback is needed.
func (r *Repo) stagedDiff(ctx context.Context) ([]File, error) {
	return r.diffFiles(ctx, "--cached", "--find-copies-harder")
}

// worktreeDiff diffs tracked files against the index, then appends one
// synthesized added-file entry per untracked file, so the working-tree view
// covers everything `git status` would flag as new (not just what plain
// `git diff` shows). The tracked diff and the untracked-file scan are
// independent git invocations, so they run concurrently.
//
// Unlike stagedDiff/FetchCommitDetail, this diff has no copy-detection flag:
// both sides (index and working tree) are restricted to already-tracked
// paths, so a path can never appear as "added" for copy detection to pair
// against — a genuinely new path is untracked, which never reaches this git
// invocation at all (it's handled by untrackedFiles below instead). Passing
// --find-copies here would be a pure no-op.
func (r *Repo) worktreeDiff(ctx context.Context) ([]File, error) {
	var files, untracked []File
	err := concurrent.RunConcurrently(
		func() (err error) { files, err = r.diffFiles(ctx); return },
		func() (err error) { untracked, err = r.untrackedFiles(ctx); return },
	)
	if err != nil {
		return nil, err
	}
	return append(files, untracked...), nil
}

// maxUntrackedDiffs caps how many untracked files get their own `git diff
// --no-index` subprocess call; beyond it, remaining untracked files fall
// back to a hunk-less added-file entry, so a repo with a huge untracked
// build directory can't spawn hundreds of git invocations.
const maxUntrackedDiffs = 200

// untrackedDiffConcurrency bounds how many of those subprocess calls run at
// once. Per-process spawn overhead varies a lot by machine (antivirus/
// sandboxing can make it far slower than on a typical dev box), and running
// them one at a time lets that overhead add up linearly across every
// untracked file — which is what made the working-tree view visibly slower
// to appear than the commit list in a repo with more than a few untracked
// files. Unbounded fan-out isn't the fix either: it would spawn as many
// concurrent git processes as there are untracked files (up to
// maxUntrackedDiffs), which could thrash on a constrained system.
const untrackedDiffConcurrency = 16

// untrackedFiles returns one synthesized File per untracked path, rendered
// as an added-file diff against /dev/null — the same approach lazygit/tig
// use for showing untracked files alongside a real diff.
func (r *Repo) untrackedFiles(ctx context.Context) ([]File, error) {
	paths, err := r.untrackedPaths(ctx)
	if err != nil {
		return nil, err
	}

	files := make([]File, len(paths))
	sem := make(chan struct{}, untrackedDiffConcurrency)
	var wg sync.WaitGroup
	for i, path := range paths {
		if i >= maxUntrackedDiffs {
			files[i] = File{Path: path, Status: StatusAdded}
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, path string) {
			defer wg.Done()
			defer func() { <-sem }()
			files[i] = r.untrackedDiff(ctx, path)
		}(i, path)
	}
	wg.Wait()
	return files, nil
}

func (r *Repo) untrackedPaths(ctx context.Context) ([]string, error) {
	out, err := runGit(ctx, r.Path, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	return splitNullTerminated(out), nil
}

// untrackedDiff synthesizes an added-file diff for a single untracked path
// via `git diff --no-index` against /dev/null. Path/OldPath/Status are
// overwritten unconditionally regardless of what the parser produced, so
// the result can't be affected by however go-gitdiff happens to parse a
// /dev/null header. If the diff can't be run or parsed at all (an
// unreadable path, a symlink, a fifo) — or the path is empty, which
// produces no textual delta to parse — this degrades to a hunk-less
// added-file entry rather than failing the whole worktree fetch.
func (r *Repo) untrackedDiff(ctx context.Context, path string) File {
	out, _, err := runGitDiffExit(ctx, r.Path, diffArgs("--no-index", "--", "/dev/null", path)...)
	if err != nil {
		return File{Path: path, Status: StatusAdded}
	}
	parsed, err := ParseUnifiedDiff(bytes.NewReader(out))
	if err != nil || len(parsed) == 0 {
		return File{Path: path, Status: StatusAdded}
	}
	f := parsed[0]
	f.Path = path
	f.OldPath = ""
	f.Status = StatusAdded
	return f
}
