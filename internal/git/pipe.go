package git

import (
	"io"
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// commitHeaderPattern matches the first line of git's default commit header,
// the one shape gitl reads out of a piped patch. Only the hash is taken from
// it: everything else the header shows is fetched again in gitl's own format
// (FetchCommitMeta), which --pretty, log.date and locale can't reshape.
var commitHeaderPattern = regexp.MustCompile(`^commit ([0-9a-f]{7,64})\b`)

// PipedPatch is a patch read from somewhere other than gitl's own git
// invocations — in practice git itself, running gitl as its pager.
type PipedPatch struct {
	Files []File
	// Preamble is whatever preceded the first file's diff: a commit header
	// and message for `git show`, nothing for `git diff`, and for anything
	// else (`git status`, `git show HEAD:file`) the entire input.
	Preamble string
	// Hash is the commit Preamble named, empty when it named none.
	Hash string
}

// ReadPipedPatch reads a patch and splits it into the diff, the text in front
// of it, and the commit that text names, if any.
//
// ANSI is stripped first: git colors its output when a pager is in use, and
// gitl re-colors from the parsed structure anyway. That also strips a literal
// escape sequence out of a diffed file's own content, which is a fair trade —
// git would have colored that line too, leaving it unparseable either way.
func ReadPipedPatch(r io.Reader) (*PipedPatch, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	text := ansi.Strip(string(raw))

	files, preamble, err := ParsePatch(strings.NewReader(text))
	if err != nil {
		return nil, err
	}

	p := &PipedPatch{Files: files, Preamble: preamble}
	if m := commitHeaderPattern.FindStringSubmatch(preamble); m != nil {
		p.Hash = m[1]
	}
	return p, nil
}

// Detail wraps the patch as a CommitDetail with no commit behind it, for the
// case where the preamble named no commit or naming one led nowhere. Where it
// did resolve, FetchCommitMeta supplies the header instead and this is unused.
func (p *PipedPatch) Detail() *CommitDetail {
	return &CommitDetail{Kind: EntryPiped, Files: p.Files, Preamble: p.Preamble}
}
