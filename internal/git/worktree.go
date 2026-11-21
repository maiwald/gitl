package git

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/maiwald/gitl/internal/concurrent"
)

// EntryKind distinguishes a real commit from the synthetic entries gitl can
// show without one behind them: the two working-tree entries above real
// history, and a diff piped in from git while gitl is acting as its pager.
// The zero value is EntryCommit, so an unadorned CommitDetail still means "a
// real commit".
type EntryKind int

const (
	EntryCommit EntryKind = iota
	EntryStaged
	EntryWorktree
	// EntryPiped is a diff read from stdin whose header, if it had one,
	// named no commit gitl could look up — so there is no metadata to show
	// beyond whatever text preceded the diff (see CommitDetail.Preamble).
	EntryPiped
)

// EntrySummary is the file and line counts behind one synthetic entry —
// everything its list row shows, without the diff those counts came from.
type EntrySummary struct {
	Kind      EntryKind
	Files     int
	Additions int
	Deletions int
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

// FetchWorktreeSummaries returns a counts-only summary of every synthetic
// entry that currently has something to show (working tree, then staged),
// omitting empty ones — without building either diff. A list row shows
// nothing but these counts, so the hunks behind them are left to the fetch
// that opens the entry, and this costs only what git spends deciding which
// files differ.
func (r *Repo) FetchWorktreeSummaries(ctx context.Context) ([]EntrySummary, error) {
	var worktree, staged EntrySummary
	err := concurrent.RunConcurrently(
		func() (err error) { worktree, err = r.worktreeSummary(ctx); return },
		func() (err error) { staged, err = r.stagedSummary(ctx); return },
	)
	if err != nil {
		return nil, err
	}

	var summaries []EntrySummary
	if worktree.Files > 0 {
		summaries = append(summaries, worktree)
	}
	if staged.Files > 0 {
		summaries = append(summaries, staged)
	}
	return summaries, nil
}

// stagedDiff diffs the index against HEAD. On an unborn HEAD git diffs
// against the empty tree by itself, so no explicit fallback is needed.
func (r *Repo) stagedDiff(ctx context.Context) ([]File, error) {
	return r.diffFiles(ctx, "--cached")
}

// worktreeDiff diffs tracked files against the index, then appends one
// synthesized added-file entry per untracked file, so the working-tree view
// covers everything `git status` would flag as new, not just what plain `git
// diff` shows. The tracked diff and the untracked-file scan are independent
// git invocations, so they run concurrently.
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
// once. Per-process spawn overhead varies a lot by machine, and serializing
// the calls lets it add up linearly across every untracked file; unbounded
// fan-out would instead spawn up to maxUntrackedDiffs git processes at once
// and could thrash a constrained system.
const untrackedDiffConcurrency = 16

// untrackedFiles returns one synthesized File per untracked path, rendered as
// an added-file diff against /dev/null — the same approach lazygit and tig use
// to show untracked files alongside a real diff.
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
	args := []string{"ls-files", "--others", "--exclude-standard", "-z"}
	out, err := runGit(ctx, r.Path, args...)
	if err != nil {
		return nil, err
	}
	return splitNullTerminated(out), nil
}

// untrackedDiff synthesizes an added-file diff for a single untracked path via
// `git diff --no-index` against /dev/null. Path/OldPath/Status are overwritten
// unconditionally, so the result doesn't depend on how go-gitdiff parses a
// /dev/null header. A diff that can't be run or parsed at all (an unreadable
// path, a symlink, a fifo, or an empty file with no textual delta) degrades to
// a hunk-less added-file entry rather than failing the whole worktree fetch.
func (r *Repo) untrackedDiff(ctx context.Context, path string) File {
	out, err := runGitNoIndexDiff(ctx, r.Path, diffArgs("--no-index", "--", "/dev/null", path)...)
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

// stagedSummary counts the index against HEAD, matching what stagedDiff
// assembles.
func (r *Repo) stagedSummary(ctx context.Context) (EntrySummary, error) {
	s, err := r.numstatSummary(ctx, "--cached")
	s.Kind = EntryStaged
	return s, err
}

// worktreeSummary counts tracked files against the index plus every untracked
// file, mirroring what worktreeDiff assembles. The two are independent, so
// they run concurrently.
func (r *Repo) worktreeSummary(ctx context.Context) (EntrySummary, error) {
	var tracked, untracked EntrySummary
	err := concurrent.RunConcurrently(
		func() (err error) { tracked, err = r.numstatSummary(ctx); return },
		func() (err error) { untracked, err = r.untrackedSummary(ctx); return },
	)
	if err != nil {
		return EntrySummary{}, err
	}
	return EntrySummary{
		Kind:      EntryWorktree,
		Files:     tracked.Files + untracked.Files,
		Additions: tracked.Additions + untracked.Additions,
		Deletions: tracked.Deletions + untracked.Deletions,
	}, nil
}

// numstatSummary totals the records of `git diff --numstat <args...>`.
//
// The -z variant is deliberately not used here. It reports a rename as
// "A\tD\t\0<old>\0<new>\0" — an empty path field followed by two more
// fields — and splitNullTerminated drops empty entries, so a parser built on
// it would silently miscount every rename as two records. Plain --numstat
// quotes any path containing a newline, making it exactly one record per line,
// and the path itself is irrelevant to a count either way.
//
// -M is passed for the same reason diffArgs passes it, and to keep a row's
// counts agreeing with its detail view: without it, a user with
// diff.renames=false would see a rename counted as two files here and shown
// as one file there.
func (r *Repo) numstatSummary(ctx context.Context, args ...string) (EntrySummary, error) {
	out, err := runGit(ctx, r.Path, append([]string{"diff", "--numstat", "-M"}, args...)...)
	if err != nil {
		return EntrySummary{}, err
	}

	var summary EntrySummary
	for _, line := range splitLines(out) {
		additions, rest, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		deletions, _, ok := strings.Cut(rest, "\t")
		if !ok {
			continue
		}
		summary.Files++
		// A binary file's counts are both "-", which fails to parse and so
		// contributes nothing — the same total the hunk-parsing path reaches
		// for one, since go-gitdiff gives it no fragments to count.
		if n, err := strconv.Atoi(additions); err == nil {
			summary.Additions += n
		}
		if n, err := strconv.Atoi(deletions); err == nil {
			summary.Deletions += n
		}
	}
	return summary, nil
}

// untrackedSummary counts the untracked files the working-tree entry shows as
// added, and the lines in them. Counting needs no diff, so unlike
// untrackedFiles this spawns no git process per file and maxUntrackedDiffs'
// subprocess cap doesn't apply — every untracked file is counted, even past
// the point where the detail view stops synthesizing hunks for them.
func (r *Repo) untrackedSummary(ctx context.Context) (EntrySummary, error) {
	paths, err := r.untrackedPaths(ctx)
	if err != nil {
		return EntrySummary{}, err
	}

	summary := EntrySummary{Kind: EntryWorktree, Files: len(paths)}
	for _, path := range paths {
		summary.Additions += countAddedLines(filepath.Join(r.Path, path))
	}
	return summary, nil
}

// binarySniffBytes is how much of a file git inspects for a NUL byte before
// calling it binary (buffer_is_binary in xdiff-interface.c).
const binarySniffBytes = 8000

// countAddedLines returns the additions `git diff --no-index` would report for
// an untracked file: one per newline plus a final unterminated line, and none
// at all for a binary file, which git reports as "-". Anything it can't read
// counts zero, the same way untrackedDiff degrades on a path it can't diff.
//
// Only regular files are opened. A named pipe would otherwise block the read
// until something wrote to it, hanging the fetch; git, being handed the path
// rather than opening it this way, has no such problem.
func countAddedLines(path string) int {
	if fi, err := os.Stat(path); err != nil || !fi.Mode().IsRegular() {
		return 0
	}
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	buf := make([]byte, 32*1024)
	var lines, read int
	var last byte
	for {
		n, err := f.Read(buf)
		chunk := buf[:n]
		if sniff := binarySniffBytes - read; sniff > 0 && bytes.IndexByte(chunk[:min(n, sniff)], 0) >= 0 {
			return 0
		}
		lines += bytes.Count(chunk, []byte{'\n'})
		if n > 0 {
			last = chunk[n-1]
			read += n
		}
		if err != nil {
			break
		}
	}
	if read > 0 && last != '\n' {
		lines++
	}
	return lines
}
