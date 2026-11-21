package git

import (
	"context"
	"fmt"
	"strings"
)

// commitDetailFormat reuses logFormat's field order (so parseLogLine can be
// reused as-is) and appends the full commit message body last, since %B may
// contain embedded newlines that would otherwise disrupt record framing.
const commitDetailFormat = logFormat + fieldSep + "%B"

// emptyTreeHash is git's well-known hash for the empty tree, used as the
// diff base for root commits (which have no parent to diff against).
const emptyTreeHash = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

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

// CommitDetail holds full commit metadata, the complete message body, and the
// commit's diff. Files is left empty for merge commits: with more than one
// parent there's no single canonical diff, and picking one is deferred.
//
// Kind is EntryCommit for every CommitDetail FetchCommitDetail builds. The
// other kinds are built directly from their files, with Commit and Body left
// zero.
type CommitDetail struct {
	Kind EntryKind
	Commit
	Body  string
	Files []File

	// Preamble is the text that preceded the diff when it was piped in
	// (EntryPiped), shown verbatim in place of the commit header gitl would
	// otherwise render. Empty for every other kind.
	Preamble string
}

// IsPseudo reports whether d describes a synthetic working-tree/staged entry
// rather than a real commit, in which case its Commit metadata and Body are
// meaningless.
func (d CommitDetail) IsPseudo() bool { return d.Kind != EntryCommit }

// FetchCommitDetail retrieves full metadata, message body, and diff for hash.
func (r *Repo) FetchCommitDetail(ctx context.Context, hash string) (*CommitDetail, error) {
	detail, err := r.FetchCommitMeta(ctx, hash)
	if err != nil {
		return nil, err
	}

	if !detail.IsMerge() {
		from := emptyTreeHash
		if len(detail.Parents) == 1 {
			from = detail.Parents[0]
		}
		files, err := r.diffFiles(ctx, from, detail.Hash)
		if err != nil {
			return nil, err
		}
		detail.Files = files
	}

	return detail, nil
}

// FetchCommitMeta retrieves a commit's metadata and message body without its
// diff, in gitl's own machine-readable format. Split out of FetchCommitDetail
// for pager mode, which has the diff already — git piped it in — but needs the
// header, and reads only the commit hash out of that pipe rather than trying
// to parse git's human-formatted one (see pipe.go).
func (r *Repo) FetchCommitMeta(ctx context.Context, hash string) (*CommitDetail, error) {
	out, err := runGit(ctx, r.Path, "log", noShowSignature, "-1", "--date=iso-strict", "--pretty=format:"+commitDetailFormat, hash)
	if err != nil {
		return nil, err
	}
	return parseCommitDetail(out)
}

func parseCommitDetail(out []byte) (*CommitDetail, error) {
	text := strings.TrimRight(string(out), "\n")
	fields := strings.SplitN(text, fieldSep, numLogFields+1)
	if len(fields) != numLogFields+1 {
		return nil, fmt.Errorf("unexpected commit detail format (got %d fields)", len(fields))
	}

	commit, err := parseLogLine(fields[:numLogFields])
	if err != nil {
		return nil, err
	}

	return &CommitDetail{
		Commit: commit,
		Body:   fields[numLogFields],
	}, nil
}
