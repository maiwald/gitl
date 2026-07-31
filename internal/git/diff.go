package git

import (
	"bytes"
	"context"
	"io"
	"strings"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
)

// diffArgs returns the shared base flags every `git diff` invocation in
// this package uses, plus any trailing arguments. Returns a fresh slice
// each call (not a shared package var) so concurrent callers — diffFiles
// and untrackedDiff run concurrently via RunConcurrently — can never
// observe or corrupt each other's args through a shared backing array.
func diffArgs(extra ...string) []string {
	return append([]string{"diff", "--no-color", "-U3"}, extra...)
}

// diffFiles runs `git diff --no-color -U3 <args...>` and parses the result
// — the one bridge from a git diff invocation to parsed Files, shared by
// every caller that just needs "diff, then parse" with different trailing
// arguments (a revision range, --cached, no arguments at all for the plain
// worktree-vs-index diff). untrackedDiff (worktree.go) is the deliberate
// exception: --no-index has different error semantics (a real difference
// is reported via exit code 1, handled by runGitDiffExit), so it doesn't
// go through this helper.
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
// and lines, classifying each line as context, addition, or deletion.
func ParseUnifiedDiff(r io.Reader) ([]File, error) {
	gitFiles, _, err := gitdiff.Parse(r)
	if err != nil {
		return nil, err
	}

	files := make([]File, 0, len(gitFiles))
	for _, gf := range gitFiles {
		files = append(files, convertFile(gf))
	}
	return files, nil
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

	for _, frag := range gf.TextFragments {
		hunk := Hunk{Header: strings.TrimSuffix(frag.Header(), "\n")}
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
