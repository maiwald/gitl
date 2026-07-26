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

// CommitDetail holds full commit metadata plus the complete message body
// (subject and description).
type CommitDetail struct {
	Commit
	Body string
}

// FetchCommitDetail retrieves full metadata and the message body for hash.
func (r *Repo) FetchCommitDetail(ctx context.Context, hash string) (*CommitDetail, error) {
	out, err := runGit(ctx, r.Path, "log", "-1", "--date=iso-strict", "--pretty=format:"+commitDetailFormat, hash)
	if err != nil {
		return nil, err
	}
	return parseCommitDetail(out)
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
