package git

import (
	"bytes"
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

// CommitDetail holds full commit metadata, the complete message body, and
// the commit's diff. Files is left empty for merge commits: with more than
// one parent there's no single canonical diff, and picking one (e.g.
// first-parent) is deferred to a later stage.
type CommitDetail struct {
	Commit
	Body  string
	Files []File
}

// FetchCommitDetail retrieves full metadata, message body, and diff for
// hash.
func (r *Repo) FetchCommitDetail(ctx context.Context, hash string) (*CommitDetail, error) {
	out, err := runGit(ctx, r.Path, "log", "-1", "--date=iso-strict", "--pretty=format:"+commitDetailFormat, hash)
	if err != nil {
		return nil, err
	}
	detail, err := parseCommitDetail(out)
	if err != nil {
		return nil, err
	}

	// Merge commits intentionally have no Files: with more than one
	// parent there's no single canonical diff to pick.
	if !detail.IsMerge() {
		from := emptyTreeHash
		if len(detail.Parents) == 1 {
			from = detail.Parents[0]
		}
		files, err := r.fetchDiff(ctx, from, detail.Hash)
		if err != nil {
			return nil, err
		}
		detail.Files = files
	}

	return detail, nil
}

func (r *Repo) fetchDiff(ctx context.Context, from, to string) ([]File, error) {
	out, err := runGit(ctx, r.Path, "diff", "--no-color", "-U3", from, to)
	if err != nil {
		return nil, err
	}
	return ParseUnifiedDiff(bytes.NewReader(out))
}

func parseCommitDetail(out []byte) (*CommitDetail, error) {
	text := trimTrailingNewlines(out)
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
