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

	switch len(detail.Parents) {
	case 1:
		files, err := r.fetchDiff(ctx, detail.Parents[0], detail.Hash)
		if err != nil {
			return nil, err
		}
		detail.Files = files
	case 0:
		files, err := r.fetchDiff(ctx, emptyTreeHash, detail.Hash)
		if err != nil {
			return nil, err
		}
		detail.Files = files
	}
	// Merge commits (len(Parents) > 1) intentionally have no Files.

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
	text := strings.TrimRight(string(out), "\n")
	fields := strings.SplitN(text, fieldSep, 9)
	if len(fields) != 9 {
		return nil, fmt.Errorf("unexpected commit detail format (got %d fields)", len(fields))
	}

	commit, err := parseLogLine(strings.Join(fields[:8], fieldSep))
	if err != nil {
		return nil, err
	}

	return &CommitDetail{
		Commit: commit,
		Body:   fields[8],
	}, nil
}
