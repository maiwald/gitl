package git

import "context"

// SearchCommitHashes returns the set of commit hashes reachable from HEAD
// whose author name or full commit message (subject and body) contains
// term, as a case-insensitive, literal substring match (not a regex).
// Author and message are queried separately and unioned (OR semantics):
// git log's own --author combined with --grep in one invocation applies
// AND semantics, which isn't what we want here. Returns an empty, non-nil
// set without invoking git when term is empty.
func (r *Repo) SearchCommitHashes(ctx context.Context, term string) (map[string]bool, error) {
	if term == "" {
		return map[string]bool{}, nil
	}

	hashes := map[string]bool{}
	for _, matchArg := range []string{"--author=" + term, "--grep=" + term} {
		out, err := runGit(ctx, r.Path, "log", matchArg, "-F", "-i", "--format=%H")
		if err != nil {
			return nil, err
		}
		for _, h := range splitLines(out) {
			hashes[h] = true
		}
	}
	return hashes, nil
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
