package git

import (
	"bytes"
	"context"
	"io"
	"strings"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
)

// diffArgs returns the shared base flags every `git diff` invocation in this
// package uses, plus any trailing arguments. It builds a fresh slice each call
// rather than appending to a package var, since diffFiles and untrackedDiff
// run concurrently and would otherwise share a backing array.
//
// -M is passed explicitly rather than relying on git's default, which a user
// can turn off with diff.renames=false — runGit inherits their config, so
// without it a moved file would show as a delete plus an add for them and as
// a rename for everyone else.
//
// The same goes for the rest: diff.external would replace the unified diff
// this package parses with whatever the program prints, and diff.noprefix
// (or mnemonicPrefix) changes the a/ b/ prefixes go-gitdiff strips from a
// path, so a path would come out missing its first directory.
func diffArgs(extra ...string) []string {
	return append([]string{"diff", "--no-color", "--no-ext-diff", "--src-prefix=a/", "--dst-prefix=b/", "-U3", "-M"}, extra...)
}

// diffFiles runs `git diff --no-color -U3 <args...>` and parses the result.
// untrackedDiff (worktree.go) doesn't use it: `--no-index` reports a real
// difference via exit code 1, which needs runGitNoIndexDiff.
func (r *Repo) diffFiles(ctx context.Context, args ...string) ([]File, error) {
	out, err := runGit(ctx, r.Path, diffArgs(args...)...)
	if err != nil {
		return nil, err
	}
	return ParseUnifiedDiff(bytes.NewReader(out))
}

// LineOp classifies a diff line as unchanged context, an addition, or a
// deletion.
type LineOp int

const (
	OpContext LineOp = iota
	OpAdd
	OpDelete
)

// DiffLine is a single line within a hunk, without its trailing newline.
type DiffLine struct {
	Op   LineOp
	Text string
}

// Hunk is one contiguous block of changed lines within a file, together
// with its unified-diff header (e.g. "@@ -1,3 +1,4 @@").
type Hunk struct {
	Header string
	Lines  []DiffLine
}

// FileStatus describes how a file was affected by a commit.
type FileStatus int

const (
	StatusModified FileStatus = iota
	StatusAdded
	StatusDeleted
	StatusRenamed
	StatusCopied
)

// File describes the changes to a single file within a diff.
type File struct {
	OldPath   string
	Path      string
	Status    FileStatus
	Additions int
	Deletions int
	Binary    bool
	Hunks     []Hunk
}

// ParseUnifiedDiff parses git diff/show output into structured files, hunks,
// and lines, classifying each line as context, addition, or deletion. It
// discards anything preceding the first file — gitl's own invocations ask for
// a bare diff, so there is nothing there. Use ParsePatch where there might be
// (see pipe.go).
func ParseUnifiedDiff(r io.Reader) ([]File, error) {
	files, _, err := ParsePatch(r)
	return files, err
}

// ParsePatch is ParseUnifiedDiff plus everything that preceded the first
// file's diff, which for `git show` output is the commit header and message.
// gitl never asks git for that shape itself; it arrives when gitl is used as
// a pager and git hands it whatever the user's command produced.
func ParsePatch(r io.Reader) (files []File, preamble string, err error) {
	gitFiles, preamble, err := gitdiff.Parse(r)
	if err != nil {
		return nil, "", err
	}

	files = make([]File, 0, len(gitFiles))
	for _, gf := range gitFiles {
		files = append(files, convertFile(gf))
	}
	return files, preamble, nil
}

func convertFile(gf *gitdiff.File) File {
	f := File{
		OldPath: gf.OldName,
		Path:    gf.NewName,
		Binary:  gf.IsBinary,
		Status:  fileStatus(gf),
	}
	if f.Path == "" {
		f.Path = gf.OldName
	}

	if n := len(gf.TextFragments); n > 0 {
		f.Hunks = make([]Hunk, 0, n)
	}
	for _, frag := range gf.TextFragments {
		hunk := Hunk{
			Header: strings.TrimSuffix(frag.Header(), "\n"),
			Lines:  make([]DiffLine, 0, len(frag.Lines)),
		}
		for _, line := range frag.Lines {
			op := OpContext
			switch line.Op {
			case gitdiff.OpAdd:
				op = OpAdd
				f.Additions++
			case gitdiff.OpDelete:
				op = OpDelete
				f.Deletions++
			}
			hunk.Lines = append(hunk.Lines, DiffLine{
				Op:   op,
				Text: strings.TrimSuffix(line.Line, "\n"),
			})
		}
		f.Hunks = append(f.Hunks, hunk)
	}
	return f
}

func fileStatus(gf *gitdiff.File) FileStatus {
	switch {
	case gf.IsNew:
		return StatusAdded
	case gf.IsDelete:
		return StatusDeleted
	case gf.IsCopy:
		return StatusCopied
	case gf.IsRename:
		return StatusRenamed
	default:
		return StatusModified
	}
}
