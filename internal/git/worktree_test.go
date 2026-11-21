package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// initFixtureRepoWithFile is like initFixtureRepo, but its single commit
// adds a tracked file so tests can dirty the working tree/index afterward.
func initFixtureRepoWithFile(t *testing.T) *Repo {
	t.Helper()
	dir := t.TempDir()
	ctx := context.Background()

	runGitCmd(t, dir, fixtureCommitEnv, "init", "-b", "main")
	runGitCmd(t, dir, fixtureCommitEnv, "config", "user.name", "Test Author")
	runGitCmd(t, dir, fixtureCommitEnv, "config", "user.email", "test@example.com")

	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("line one\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGitCmd(t, dir, fixtureCommitEnv, "add", "tracked.txt")
	runGitCmd(t, dir, fixtureCommitEnv, "commit", "-m", "add tracked.txt")

	repo, err := OpenRepo(ctx, dir)
	if err != nil {
		t.Fatalf("OpenRepo: %v", err)
	}
	return repo
}

func TestFetchWorktreeEntryStaged(t *testing.T) {
	repo := initFixtureRepoWithFile(t)
	dir := repo.Path
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("line one\nline two\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGitCmd(t, dir, fixtureCommitEnv, "add", "tracked.txt")

	files, err := repo.FetchWorktreeEntry(context.Background(), EntryStaged)
	if err != nil {
		t.Fatalf("FetchWorktreeEntry(EntryStaged): %v", err)
	}
	if len(files) != 1 || files[0].Path != "tracked.txt" {
		t.Fatalf("got files = %+v, want one file tracked.txt", files)
	}
	if files[0].Additions != 1 {
		t.Errorf("Additions = %d, want 1", files[0].Additions)
	}
}

// TestFetchWorktreeEntryStagedShowsACopiedFileAsAdded pins down that
// stagedDiff passes no copy-detection flag: copy.txt is a brand-new staged
// file whose content happens to match tracked.txt, and it reads as a plain
// added file with all of its lines, not as an empty delta against
// tracked.txt.
func TestFetchWorktreeEntryStagedShowsACopiedFileAsAdded(t *testing.T) {
	repo := initFixtureRepoWithFile(t)
	dir := repo.Path
	content := []byte("line one\nline two\nline three\nline four\n")
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), content, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGitCmd(t, dir, fixtureCommitEnv, "add", "tracked.txt")
	runGitCmd(t, dir, fixtureCommitEnv, "commit", "-m", "give tracked.txt real content")

	if err := os.WriteFile(filepath.Join(dir, "copy.txt"), content, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGitCmd(t, dir, fixtureCommitEnv, "add", "copy.txt")

	files, err := repo.FetchWorktreeEntry(context.Background(), EntryStaged)
	if err != nil {
		t.Fatalf("FetchWorktreeEntry(EntryStaged): %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got files = %+v, want one file", files)
	}
	f := files[0]
	if f.Path != "copy.txt" || f.OldPath != "" || f.Status != StatusAdded {
		t.Errorf("file = %+v, want copy.txt added with no source", f)
	}
	if f.Additions != 4 || f.Deletions != 0 {
		t.Errorf("Additions=%d Deletions=%d, want 4/0 — the whole file", f.Additions, f.Deletions)
	}
}

// TestFetchWorktreeEntryStagedDetectsRenameWithRenamesOff pins the explicit
// -M in diffArgs and numstatSummary: the fixture turns diff.renames off, which
// runGit inherits, so without the flag a staged move would come back as a
// delete plus an add in the diff and as two files in the row's counts.
func TestFetchWorktreeEntryStagedDetectsRenameWithRenamesOff(t *testing.T) {
	repo := initFixtureRepoWithFile(t)
	ctx := context.Background()
	dir := repo.Path
	runGitCmd(t, dir, fixtureCommitEnv, "config", "diff.renames", "false")
	content := []byte("line one\nline two\nline three\nline four\n")
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), content, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGitCmd(t, dir, fixtureCommitEnv, "add", "tracked.txt")
	runGitCmd(t, dir, fixtureCommitEnv, "commit", "-m", "give tracked.txt real content")
	runGitCmd(t, dir, fixtureCommitEnv, "mv", "tracked.txt", "moved.txt")

	files, err := repo.FetchWorktreeEntry(ctx, EntryStaged)
	if err != nil {
		t.Fatalf("FetchWorktreeEntry(EntryStaged): %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got files = %+v, want one file", files)
	}
	f := files[0]
	if f.Path != "moved.txt" || f.OldPath != "tracked.txt" || f.Status != StatusRenamed {
		t.Errorf("file = %+v, want moved.txt renamed from tracked.txt", f)
	}

	summary, err := repo.stagedSummary(ctx)
	if err != nil {
		t.Fatalf("stagedSummary: %v", err)
	}
	want := EntrySummary{Kind: EntryStaged, Files: 1}
	if summary != want {
		t.Errorf("stagedSummary = %+v, want %+v — the rename counts as one file", summary, want)
	}
}

func TestFetchWorktreeEntryWorktree(t *testing.T) {
	repo := initFixtureRepoWithFile(t)
	dir := repo.Path
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("line one\nline two\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	files, err := repo.FetchWorktreeEntry(context.Background(), EntryWorktree)
	if err != nil {
		t.Fatalf("FetchWorktreeEntry(EntryWorktree): %v", err)
	}
	if len(files) != 1 || files[0].Path != "tracked.txt" {
		t.Fatalf("got files = %+v, want one file tracked.txt", files)
	}
}

func TestIsPseudo(t *testing.T) {
	for _, k := range []EntryKind{EntryStaged, EntryWorktree, EntryPiped} {
		if !(CommitDetail{Kind: k}).IsPseudo() {
			t.Errorf("IsPseudo() = false for kind %v, want true", k)
		}
	}
	if (CommitDetail{}).IsPseudo() {
		t.Error("IsPseudo() = true for a real commit, want false")
	}
}

func TestWorktreeDiffIncludesUntrackedFile(t *testing.T) {
	repo := initFixtureRepoWithFile(t)
	dir := repo.Path
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("brand new\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	files, err := repo.FetchWorktreeEntry(context.Background(), EntryWorktree)
	if err != nil {
		t.Fatalf("FetchWorktreeEntry(EntryWorktree): %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1: %+v", len(files), files)
	}
	f := files[0]
	if f.Path != "new.txt" {
		t.Errorf("Path = %q, want new.txt", f.Path)
	}
	if f.OldPath != "" {
		t.Errorf("OldPath = %q, want empty", f.OldPath)
	}
	if f.Status != StatusAdded {
		t.Errorf("Status = %v, want StatusAdded", f.Status)
	}
	if f.Additions != 1 {
		t.Errorf("Additions = %d, want 1", f.Additions)
	}
}

func TestWorktreeDiffIncludesNestedUntrackedFile(t *testing.T) {
	repo := initFixtureRepoWithFile(t)
	dir := repo.Path
	if err := os.MkdirAll(filepath.Join(dir, "sub", "dir"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "dir", "nested.txt"), []byte("deep\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	files, err := repo.FetchWorktreeEntry(context.Background(), EntryWorktree)
	if err != nil {
		t.Fatalf("FetchWorktreeEntry(EntryWorktree): %v", err)
	}
	if len(files) != 1 || files[0].Path != "sub/dir/nested.txt" {
		t.Fatalf("got files = %+v, want one file sub/dir/nested.txt", files)
	}
}

func TestWorktreeDiffExcludesGitignoredFile(t *testing.T) {
	repo := initFixtureRepoWithFile(t)
	dir := repo.Path
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored.txt\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGitCmd(t, dir, fixtureCommitEnv, "add", ".gitignore")
	runGitCmd(t, dir, fixtureCommitEnv, "commit", "-m", "add gitignore")
	if err := os.WriteFile(filepath.Join(dir, "ignored.txt"), []byte("should not appear\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	files, err := repo.FetchWorktreeEntry(context.Background(), EntryWorktree)
	if err != nil {
		t.Fatalf("FetchWorktreeEntry(EntryWorktree): %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("got files = %+v, want none (ignored.txt should be excluded)", files)
	}
}

func TestWorktreeDiffUntrackedFileBeyondCapDegradesGracefully(t *testing.T) {
	repo := initFixtureRepoWithFile(t)
	dir := repo.Path

	// Names sort so that files past maxUntrackedDiffs are the ones without
	// individually-diffed content, matching ls-files -z's lexical order.
	total := maxUntrackedDiffs + 1
	for i := 0; i < total; i++ {
		name := fmt.Sprintf("f%04d.txt", i)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(fmt.Sprintf("content %d\n", i)), 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", name, err)
		}
	}

	files, err := repo.FetchWorktreeEntry(context.Background(), EntryWorktree)
	if err != nil {
		t.Fatalf("FetchWorktreeEntry(EntryWorktree): %v", err)
	}
	if len(files) != total {
		t.Fatalf("got %d files, want %d", len(files), total)
	}

	last := files[total-1]
	if last.Path != fmt.Sprintf("f%04d.txt", total-1) {
		t.Fatalf("last file = %q, want f%04d.txt", last.Path, total-1)
	}
	if last.Status != StatusAdded {
		t.Errorf("last file Status = %v, want StatusAdded", last.Status)
	}
	if len(last.Hunks) != 0 || last.Additions != 0 {
		t.Errorf("last file beyond the cap should be hunk-less, got %+v", last)
	}

	first := files[0]
	if len(first.Hunks) == 0 {
		t.Errorf("first file within the cap should have hunks, got %+v", first)
	}
}

func TestFetchCommitDetailIsNotPseudo(t *testing.T) {
	repo, hashes := initFixtureRepo(t, 1)
	detail, err := repo.FetchCommitDetail(context.Background(), hashes[0])
	if err != nil {
		t.Fatalf("FetchCommitDetail: %v", err)
	}
	if detail.IsPseudo() {
		t.Error("IsPseudo() = true for a real commit, want false")
	}
	if detail.Kind != EntryCommit {
		t.Errorf("Kind = %v, want EntryCommit", detail.Kind)
	}
}

func TestFetchWorktreeSummariesOmitsEmptyAndOrdersWorktreeFirst(t *testing.T) {
	repo := initFixtureRepoWithFile(t)
	ctx := context.Background()

	summaries, err := repo.FetchWorktreeSummaries(ctx)
	if err != nil {
		t.Fatalf("FetchWorktreeSummaries: %v", err)
	}
	if len(summaries) != 0 {
		t.Fatalf("got %d summaries on a clean repo, want 0", len(summaries))
	}

	dir := repo.Path
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("line one\nline two\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.txt"), []byte("staged\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGitCmd(t, dir, fixtureCommitEnv, "add", "other.txt")

	summaries, err = repo.FetchWorktreeSummaries(ctx)
	if err != nil {
		t.Fatalf("FetchWorktreeSummaries: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("got %d summaries, want 2 (worktree + staged): %+v", len(summaries), summaries)
	}
	if summaries[0].Kind != EntryWorktree {
		t.Errorf("summaries[0].Kind = %v, want EntryWorktree", summaries[0].Kind)
	}
	if summaries[1].Kind != EntryStaged {
		t.Errorf("summaries[1].Kind = %v, want EntryStaged", summaries[1].Kind)
	}
}

// TestFetchWorktreeSummariesMatchFullDiff is the anti-drift test between the
// counts a list row shows and the diff its detail view opens: the two come
// from different git invocations now, and only agree as long as neither grows
// a flag the other lacks. The fixture deliberately contains no copy or rename,
// the one case where they're expected to disagree (see stagedSummary).
func TestFetchWorktreeSummariesMatchFullDiff(t *testing.T) {
	repo := initFixtureRepoWithFile(t)
	ctx := context.Background()
	dir := repo.Path

	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("line one\nline two\nline three\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "staged.txt"), []byte("x\ny\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGitCmd(t, dir, fixtureCommitEnv, "add", "staged.txt")

	summaries, err := repo.FetchWorktreeSummaries(ctx)
	if err != nil {
		t.Fatalf("FetchWorktreeSummaries: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("got %d summaries, want 2: %+v", len(summaries), summaries)
	}

	for _, summary := range summaries {
		files, err := repo.FetchWorktreeEntry(ctx, summary.Kind)
		if err != nil {
			t.Fatalf("FetchWorktreeEntry(%v): %v", summary.Kind, err)
		}
		want := EntrySummary{Kind: summary.Kind, Files: len(files)}
		for _, f := range files {
			want.Additions += f.Additions
			want.Deletions += f.Deletions
		}
		if summary != want {
			t.Errorf("summary for %v = %+v, want %+v (the full diff's own totals)", summary.Kind, summary, want)
		}
	}
}

// TestUntrackedSummaryLineCounts checks countAddedLines against git itself:
// the summary reimplements in Go what `git diff --no-index -- /dev/null
// <path>` used to be spawned per file to answer, so git is the oracle it has
// to keep agreeing with. The shapes that make the two diverge if it doesn't:
// a final line missing its newline still counts, an empty file counts
// nothing, and a binary file reports "-" rather than any count at all.
func TestUntrackedSummaryLineCounts(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
		want    int
	}{
		{"trailing newline", []byte("a\nb\n"), 2},
		{"no trailing newline", []byte("a\nb\nc"), 3},
		{"empty", []byte(""), 0},
		{"binary", []byte("a\x00b\nc\n"), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := initFixtureRepoWithFile(t)
			ctx := context.Background()
			if err := os.WriteFile(filepath.Join(repo.Path, "untracked.txt"), tt.content, 0o644); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}

			summary, err := repo.untrackedSummary(ctx)
			if err != nil {
				t.Fatalf("untrackedSummary: %v", err)
			}
			if summary.Files != 1 {
				t.Fatalf("Files = %d, want 1", summary.Files)
			}
			if summary.Additions != tt.want {
				t.Errorf("Additions = %d, want %d", summary.Additions, tt.want)
			}
			if got := gitNoIndexAdditions(t, repo.Path, "untracked.txt"); got != tt.want {
				t.Errorf("git itself reports %d additions, want %d — the fixture, not the code, is wrong", got, tt.want)
			}
		})
	}
}

// gitNoIndexAdditions asks git how many lines it would report as added for
// path, the way untrackedDiff still does for the diff a row opens. It can't
// use runGitCmd: --no-index exits 1 precisely when it found a difference.
func gitNoIndexAdditions(t *testing.T, dir, path string) int {
	t.Helper()
	out, err := runGitNoIndexDiff(context.Background(), dir, "diff", "--numstat", "--no-index", "--", "/dev/null", path)
	if err != nil {
		t.Fatalf("git diff --numstat --no-index: %v", err)
	}
	lines := splitLines(out)
	if len(lines) != 1 {
		t.Fatalf("git diff --numstat --no-index = %q, want one record", lines)
	}
	additions, _, _ := strings.Cut(lines[0], "\t")
	if additions == "-" {
		return 0
	}
	n, err := strconv.Atoi(additions)
	if err != nil {
		t.Fatalf("unparseable numstat count %q: %v", additions, err)
	}
	return n
}

// TestUntrackedSummarySkipsUnreadable covers the path countAddedLines refuses
// to open: a named pipe, where the read would block until something wrote to
// it. It still counts as a file, since git lists it as untracked.
func TestUntrackedSummarySkipsUnreadable(t *testing.T) {
	repo := initFixtureRepoWithFile(t)
	fifo := filepath.Join(repo.Path, "pipe")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("Mkfifo: %v", err)
	}

	summary, err := repo.untrackedSummary(context.Background())
	if err != nil {
		t.Fatalf("untrackedSummary: %v", err)
	}
	if summary.Additions != 0 {
		t.Errorf("Additions = %d, want 0 for a fifo", summary.Additions)
	}
}

// TestNumstatSummaryRecordShapes covers the three numstat records that aren't
// a plain "N\tM\tpath": a rename (one record carrying two paths, which the -z
// form would split into what looks like two), a binary change ("-\t-"), and a
// mode-only change ("0\t0"), which still counts as a changed file.
func TestNumstatSummaryRecordShapes(t *testing.T) {
	repo := initFixtureRepoWithFile(t)
	ctx := context.Background()
	dir := repo.Path
	runGitCmd(t, dir, fixtureCommitEnv, "config", "core.fileMode", "true")

	if err := os.WriteFile(filepath.Join(dir, "bin.dat"), []byte("\x00\x01binary\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mode.txt"), []byte("unchanged\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGitCmd(t, dir, fixtureCommitEnv, "add", "bin.dat", "mode.txt")
	runGitCmd(t, dir, fixtureCommitEnv, "commit", "-m", "add bin.dat and mode.txt")

	runGitCmd(t, dir, fixtureCommitEnv, "mv", "tracked.txt", "renamed.txt")
	if err := os.WriteFile(filepath.Join(dir, "bin.dat"), []byte("\x00\x02other\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chmod(filepath.Join(dir, "mode.txt"), 0o755); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	runGitCmd(t, dir, fixtureCommitEnv, "add", "-A")

	summary, err := repo.numstatSummary(ctx, "--cached")
	if err != nil {
		t.Fatalf("numstatSummary: %v", err)
	}
	if summary.Files != 3 {
		t.Errorf("Files = %d, want 3 (rename + binary + mode change): %+v", summary.Files, summary)
	}
	if summary.Additions != 0 || summary.Deletions != 0 {
		t.Errorf("counts = +%d -%d, want +0 -0: none of these records carries line counts", summary.Additions, summary.Deletions)
	}
}

// TestStagedSummaryOnUnbornHead pins that a repo with no commits needs no
// special case: `git diff --cached --numstat` diffs the index against the
// empty tree by itself, exactly as stagedDiff relies on.
func TestStagedSummaryOnUnbornHead(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	runGitCmd(t, dir, fixtureCommitEnv, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGitCmd(t, dir, fixtureCommitEnv, "add", "new.txt")

	repo, err := OpenRepo(ctx, dir)
	if err != nil {
		t.Fatalf("OpenRepo: %v", err)
	}
	summary, err := repo.stagedSummary(ctx)
	if err != nil {
		t.Fatalf("stagedSummary: %v", err)
	}
	want := EntrySummary{Kind: EntryStaged, Files: 1, Additions: 2}
	if summary != want {
		t.Errorf("stagedSummary = %+v, want %+v", summary, want)
	}
}

// TestStagedSummaryCountsACopyAsAnAddition is the row-counts half of
// TestFetchWorktreeEntryStagedShowsACopiedFileAsAdded, on the same fixture:
// neither the row nor the diff behind it does copy detection, so both read a
// copied file as a whole added file and report the same totals. Adding
// --find-copies-harder to either one alone is what would split them apart
// again.
func TestStagedSummaryCountsACopyAsAnAddition(t *testing.T) {
	repo := initFixtureRepoWithFile(t)
	ctx := context.Background()
	dir := repo.Path
	content := []byte("line one\nline two\nline three\nline four\n")
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), content, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGitCmd(t, dir, fixtureCommitEnv, "add", "tracked.txt")
	runGitCmd(t, dir, fixtureCommitEnv, "commit", "-m", "give tracked.txt real content")

	if err := os.WriteFile(filepath.Join(dir, "copy.txt"), content, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGitCmd(t, dir, fixtureCommitEnv, "add", "copy.txt")

	summary, err := repo.stagedSummary(ctx)
	if err != nil {
		t.Fatalf("stagedSummary: %v", err)
	}
	want := EntrySummary{Kind: EntryStaged, Files: 1, Additions: 4}
	if summary != want {
		t.Errorf("stagedSummary = %+v, want %+v (the copy read as a plain added file)", summary, want)
	}
}

// TestWorktreeSummaryCountsEveryUntrackedFile pins the second deliberate
// divergence: maxUntrackedDiffs caps how many untracked files get a
// synthesized diff (see
// TestWorktreeDiffUntrackedFileBeyondCapDegradesGracefully), because each one
// costs a subprocess. Counting costs none, so the row's totals climb past
// the cap instead of silently flattening there.
func TestWorktreeSummaryCountsEveryUntrackedFile(t *testing.T) {
	repo := initFixtureRepoWithFile(t)
	total := maxUntrackedDiffs + 1
	for i := 0; i < total; i++ {
		name := fmt.Sprintf("untracked-%04d.txt", i)
		if err := os.WriteFile(filepath.Join(repo.Path, name), []byte("one line\n"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	summary, err := repo.worktreeSummary(context.Background())
	if err != nil {
		t.Fatalf("worktreeSummary: %v", err)
	}
	if summary.Files != total {
		t.Errorf("Files = %d, want %d", summary.Files, total)
	}
	if summary.Additions != total {
		t.Errorf("Additions = %d, want %d — every untracked file counts, cap or no cap", summary.Additions, total)
	}
}
