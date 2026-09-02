package ui

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

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
	msg := copyCmd(&buf, "+added\n-removed\n", 2)()

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
