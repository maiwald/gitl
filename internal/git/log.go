package git

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// fieldSep separates fields within a single log record. It's the ASCII unit
// separator, chosen because none of the fields we request can contain it.
const fieldSep = "\x1f"

// logFormat requests one record per commit (tformat guarantees a trailing
// newline per record), with fields in the order parsed by parseLogLine.
const logFormat = "%H" + fieldSep + "%h" + fieldSep + "%P" + fieldSep +
	"%an" + fieldSep + "%ae" + fieldSep + "%ad" + fieldSep +
	"%D" + fieldSep + "%s"

// Commit is a single entry from git log.
type Commit struct {
	Hash        string
	ShortHash   string
	Parents     []string
	AuthorName  string
	AuthorEmail string
	AuthorDate  time.Time
	Refs        []string
	Subject     string
}

// FetchLogBatch fetches up to limit commits starting at skip, in the order
// git log would normally produce them (most recent first).
func (r *Repo) FetchLogBatch(ctx context.Context, skip, limit int) ([]Commit, error) {
	args := []string{
		"log",
		fmt.Sprintf("--skip=%d", skip),
		fmt.Sprintf("--max-count=%d", limit),
		"--date=iso-strict",
		"--pretty=tformat:" + logFormat,
	}
	out, err := runGit(ctx, r.Path, args...)
	if err != nil {
		return nil, err
	}
	return parseLogOutput(out)
}

// CommitCount returns the total number of commits reachable from HEAD.
func (r *Repo) CommitCount(ctx context.Context) (int, error) {
	out, err := runGit(ctx, r.Path, "rev-list", "--count", "HEAD")
	if err != nil {
		return 0, err
	}
	count, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, fmt.Errorf("parsing commit count: %w", err)
	}
	return count, nil
}

func parseLogOutput(out []byte) ([]Commit, error) {
	text := string(out)
	lines := strings.Split(text, "\n")

	commits := make([]Commit, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		c, err := parseLogLine(line)
		if err != nil {
			return nil, err
		}
		commits = append(commits, c)
	}
	return commits, nil
}

func parseLogLine(line string) (Commit, error) {
	fields := strings.Split(line, fieldSep)
	if len(fields) != 8 {
		return Commit{}, fmt.Errorf("unexpected log line format (got %d fields): %q", len(fields), line)
	}

	hash, short, parents, authorName, authorEmail, dateStr, refs, subject := fields[0], fields[1], fields[2], fields[3], fields[4], fields[5], fields[6], fields[7]

	date, err := time.Parse(time.RFC3339, dateStr)
	if err != nil {
		return Commit{}, fmt.Errorf("parsing author date %q: %w", dateStr, err)
	}

	var parentList []string
	if parents != "" {
		parentList = strings.Split(parents, " ")
	}

	var refList []string
	if refs != "" {
		refList = strings.Split(refs, ", ")
	}

	return Commit{
		Hash:        hash,
		ShortHash:   short,
		Parents:     parentList,
		AuthorName:  authorName,
		AuthorEmail: authorEmail,
		AuthorDate:  date,
		Refs:        refList,
		Subject:     subject,
	}, nil
}
