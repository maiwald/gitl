package ui

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestMain stubs out the local clipboard tools for the whole package. No test
// wants to shell out to pbcopy/tmux and overwrite the clipboard of whoever is
// running go test; the ones that care about that half of a copy record what it
// was handed instead (see recordLocalCopies).
func TestMain(m *testing.M) {
	copyLocally = func(string) {}
	os.Exit(m.Run())
}

// recordLocalCopies makes the local-tool half of a copy observable, restoring
// the package-wide stub afterwards.
func recordLocalCopies(t *testing.T) *[]string {
	t.Helper()
	var got []string
	copyLocally = func(text string) { got = append(got, text) }
	t.Cleanup(func() { copyLocally = func(string) {} })
	return &got
}

// clipboardPayload decodes the text an OSC 52 sequence carries, failing the
// test if what was written isn't one.
func clipboardPayload(t *testing.T, written string) string {
	t.Helper()
	const prefix = "\x1b]52;c;"
	if !strings.HasPrefix(written, prefix) || !strings.HasSuffix(written, "\a") {
		t.Fatalf("not an OSC 52 system-clipboard sequence: %q", written)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSuffix(strings.TrimPrefix(written, prefix), "\a"))
	if err != nil {
		t.Fatalf("payload is not base64: %v", err)
	}
	return string(decoded)
}

func TestCopyCmdWritesOSC52(t *testing.T) {
	var buf bytes.Buffer
	local := recordLocalCopies(t)
	msg := copyCmd(&buf, "+added\n-removed\n", 2)()

	// The same text goes to the terminal and to the local tools: they are
	// alternative routes to one clipboard, not different content.
	if want := []string{"+added\n-removed\n"}; !slices.Equal(*local, want) {
		t.Errorf("local clipboard tools got %q, want %q", *local, want)
	}

	if got, ok := msg.(copiedMsg); !ok || got.lines != 2 {
		t.Fatalf("msg = %#v, want copiedMsg{lines: 2}", msg)
	}
	if got := clipboardPayload(t, buf.String()); got != "+added\n-removed\n" {
		t.Errorf("clipboard payload = %q", got)
	}
}

// failingWriter stands in for a terminal that has gone away mid-run.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("no terminal") }

func TestCopyCmdReportsWriteFailure(t *testing.T) {
	msg, ok := copyCmd(failingWriter{}, "text", 1)().(errMsg)
	if !ok {
		t.Fatalf("msg = %#v, want errMsg", msg)
	}
	if msg.severity != nonFatal {
		t.Errorf("severity = %v, want nonFatal — a failed copy leaves the view intact", msg.severity)
	}
}

// TestEnterCopiesTheSelection drives the whole path a user takes: V, extend,
// Enter, and the sequence lands on the model's own writer.
func TestEnterCopiesTheSelection(t *testing.T) {
	for _, key := range []rune{'\r', 'y'} {
		t.Run(string(key), func(t *testing.T) {
			var buf bytes.Buffer
			m := newSearchableDetailModel()
			m.out = &buf
			m.detail.setCursor(0)

			newModel, _ := m.Update(keyRune('V'))
			m = newModel.(Model)
			newModel, _ = m.Update(keyRune('j'))
			m = newModel.(Model)

			var cmd tea.Cmd
			if key == '\r' {
				newModel, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			} else {
				newModel, cmd = m.Update(keyRune(key))
			}
			m = newModel.(Model)

			if cmd == nil {
				t.Fatal("expected a copy command")
			}
			if m.detail.selecting {
				t.Error("selecting = true after copying, want the selection ended")
			}
			msg := cmd()
			if got, ok := msg.(copiedMsg); !ok || got.lines != 2 {
				t.Fatalf("msg = %#v, want copiedMsg{lines: 2}", msg)
			}
			if got := clipboardPayload(t, buf.String()); strings.Count(got, "\n") != 2 {
				t.Errorf("clipboard payload = %q, want two lines", got)
			}
		})
	}
}

func TestStatusBarConfirmsACopyUntilTheNextKey(t *testing.T) {
	m := newSearchableDetailModel()
	newModel, _ := m.Update(copiedMsg{lines: 3})
	m = newModel.(Model)
	if got := m.renderStatusBar(); !strings.Contains(got, "copied 3 lines") {
		t.Errorf("status bar = %q, want a copy confirmation", got)
	}

	newModel, _ = m.Update(keyRune('j'))
	m = newModel.(Model)
	if got := m.renderStatusBar(); strings.Contains(got, "copied") {
		t.Errorf("status bar = %q, want the confirmation gone after a key press", got)
	}
}

// TestFailedTerminalWriteSkipsTheLocalTools pins the one case where a copy
// gives up early: the tty is gone, which is a real failure to report rather
// than a clipboard that merely might not have taken.
func TestFailedTerminalWriteSkipsTheLocalTools(t *testing.T) {
	local := recordLocalCopies(t)
	if _, ok := copyCmd(failingWriter{}, "text", 1)().(errMsg); !ok {
		t.Fatal("want errMsg from a failed terminal write")
	}
	if len(*local) != 0 {
		t.Errorf("local clipboard tools ran %d times after a failed write, want 0", len(*local))
	}
}

// TestClipboardToolsLeadWithTmuxInsideTmux covers the reason the tmux entry
// exists at all: inside tmux the OSC 52 sequence is swallowed, so `tmux
// load-buffer -w` is the only route that needs no tmux.conf change.
func TestClipboardToolsLeadWithTmuxInsideTmux(t *testing.T) {
	t.Setenv("TMUX", "/private/tmp/tmux-501/default,1234,0")
	tools := clipboardTools()
	if len(tools) == 0 {
		t.Fatal("no clipboard tools")
	}
	if want := []string{"tmux", "load-buffer", "-w", "-"}; !slices.Equal(tools[0], want) {
		t.Errorf("first tool = %q, want %q", tools[0], want)
	}
	if len(tools) < 2 {
		t.Error("tmux is the only tool listed, want the platform's own as well — a local copy shouldn't stop at the tmux buffer")
	}
}

func TestClipboardToolsOmitTmuxOutsideIt(t *testing.T) {
	t.Setenv("TMUX", "")
	for _, tool := range clipboardTools() {
		if tool[0] == "tmux" {
			t.Errorf("tmux listed outside a tmux session: %q", tool)
		}
	}
}

// TestRunClipboardToolsFeedsTheToolOnStdin exercises the real exec path — the
// half TestMain stubs out everywhere else — against a fake tool standing in
// for whatever this platform's first choice is, with PATH narrowed to it so
// the rest of the list finds nothing.
func TestRunClipboardToolsFeedsTheToolOnStdin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake tool is a shell script")
	}
	t.Setenv("TMUX", "")
	tools := clipboardTools()
	if len(tools) == 0 {
		t.Skip("no clipboard tools on this platform")
	}

	dir := t.TempDir()
	out := filepath.Join(dir, "copied")
	// /bin/cat by absolute path: PATH is about to be narrowed to dir, so the
	// script can't look a command up any more than the tools it shadows can.
	script := "#!/bin/sh\n/bin/cat > " + out + "\n"
	if err := os.WriteFile(filepath.Join(dir, tools[0][0]), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	runClipboardTools("+added\n-removed\n")

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("%s was never run: %v", tools[0][0], err)
	}
	if string(got) != "+added\n-removed\n" {
		t.Errorf("tool received %q", got)
	}
}

// TestRunClipboardToolsSurvivesAFailingTool covers the ordinary case on a
// machine with a tool installed but no clipboard behind it (xclip with no X
// server): it must not stop the tools after it from being tried.
func TestRunClipboardToolsSurvivesAFailingTool(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	runClipboardTools("text")
}
