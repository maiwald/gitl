package git

import (
	"context"
	"strings"

	"github.com/maiwald/gitl/internal/concurrent"
)

// SearchCommitHashes returns the set of commit hashes reachable from HEAD
// whose author name or full commit message (subject and body) contains
// term, as a case-insensitive, literal substring match (not a regex).
// Author and message are queried separately and unioned (OR semantics):
// git log's own --author combined with --grep in one invocation applies
// AND semantics, which isn't what we want here. Returns an empty, non-nil
// set without invoking git when term is empty. The two queries are
// independent subprocesses, so they run concurrently rather than one
// after another.
func (r *Repo) SearchCommitHashes(ctx context.Context, term string) (map[string]bool, error) {
	if term == "" {
		return map[string]bool{}, nil
	}

	var authorHashes, messageHashes []string
	err := concurrent.RunConcurrently(
		func() (err error) { authorHashes, err = r.logHashes(ctx, "--author="+term); return },
		func() (err error) { messageHashes, err = r.logHashes(ctx, "--grep="+term); return },
	)
	if err != nil {
		return nil, err
	}

	hashes := make(map[string]bool, len(authorHashes)+len(messageHashes))
	for _, h := range authorHashes {
		hashes[h] = true
	}
	for _, h := range messageHashes {
		hashes[h] = true
	}
	return hashes, nil
}

// logHashes runs `git log <matchArg> -F -i --format=%H`, returning the
// hashes of every matching commit.
func (r *Repo) logHashes(ctx context.Context, matchArg string) ([]string, error) {
	out, err := runGit(ctx, r.Path, "log", matchArg, "-F", "-i", "--format=%H")
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

// AllCommitHashes returns every commit hash reachable from HEAD, in the
// same order FetchLogBatch uses, without the cost of formatting
// author/date/ref-decoration for each one. It's used to map a matched
// commit's hash to its absolute position in history.
func (r *Repo) AllCommitHashes(ctx context.Context) ([]string, error) {
	out, err := runGit(ctx, r.Path, "log", "--format=%H")
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

// HashesMatching filters hashes to those containing term as a
// case-insensitive substring — the same match semantics
// SearchCommitHashes applies to author/message. It's a pure function (no
// git subprocess) rather than a third case in SearchCommitHashes because
// callers already have the full hash order in hand (fetched once via
// AllCommitHashes and reused across searches, see allHashes's doc comment
// in app.go) for mapping matches to list positions, so filtering it
// in-memory avoids spawning a second git process just to search by hash.
// Returns an empty, non-nil set when term is empty.
func HashesMatching(hashes []string, term string) map[string]bool {
	matches := map[string]bool{}
	if term == "" {
		return matches
	}
	term = strings.ToLower(term)
	for _, h := range hashes {
		if strings.Contains(h, term) {
			matches[h] = true
		}
	}
	return matches
}
