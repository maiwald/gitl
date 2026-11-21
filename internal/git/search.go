package git

import (
	"context"
	"strings"

	"github.com/maiwald/gitl/internal/concurrent"
)

// SearchCommitHashes returns the set of commit hashes reachable from HEAD
// whose author (name or email — git matches the whole "Name <email>" header)
// or full commit message contains term, as a case-insensitive literal
// substring match (not a regex). Author and message
// are queried separately and unioned: git log's own --author plus --grep in
// one invocation would AND them instead. Returns an empty, non-nil set without
// invoking git when term is empty; otherwise the two queries, being
// independent subprocesses, run concurrently.
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
	out, err := runGit(ctx, r.Path, "log", noShowSignature, matchArg, "-F", "-i", "--format=%H")
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

// AllCommitHashes returns every commit hash reachable from HEAD, in the same
// order FetchLogBatch uses, without formatting author/date/ref-decoration for
// each one. It maps a matched commit's hash to its position in history.
func (r *Repo) AllCommitHashes(ctx context.Context) ([]string, error) {
	out, err := runGit(ctx, r.Path, "log", noShowSignature, "--format=%H")
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

// HashMatcher returns a predicate reporting whether a hash contains term as a
// case-insensitive substring — the same match semantics SearchCommitHashes
// applies to author/message. It's an in-memory test rather than a third case
// in SearchCommitHashes because callers already hold the full hash order (see
// allHashes in app.go), so searching by hash costs no git process at all. A
// predicate rather than a set of the matching hashes, since a short term
// matches most of them and the set would be the size of the whole history.
// The predicate matches nothing when term is empty.
func HashMatcher(term string) func(hash string) bool {
	if term == "" {
		return func(string) bool { return false }
	}
	term = strings.ToLower(term)
	return func(hash string) bool { return strings.Contains(hash, term) }
}
