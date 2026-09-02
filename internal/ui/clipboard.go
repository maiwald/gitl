package ui

import (
	"fmt"
	"io"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// copiedMsg reports a successful copy, for the status bar to confirm.
type copiedMsg struct{ lines int }

// copyCmd writes text to the terminal's clipboard as an OSC 52 sequence.
//
// OSC 52 rather than pbcopy/xclip because it reaches the clipboard of the
// machine the *terminal* is on, which is the right one over ssh, and because
// it needs no subprocess and no platform detection. It does need the terminal
// to allow it: tmux forwards it outward under its default set-clipboard, but
// a terminal with clipboard writes disabled silently drops it, and there is
// no reply to wait for — a successful write here means the sequence was sent,
// not that the clipboard took it.
//
// The write goes to the terminal alongside bubbletea's own frames, from a
// different goroutine, so it is deliberately a single Write of one
// self-contained sequence: the terminal parses a complete OSC 52 wherever it
// lands, and the keypress that triggered it cleared the selection, so the
// frame after it repaints every row that was highlighted anyway.
func copyCmd(w io.Writer, text string, lines int) tea.Cmd {
	if w == nil {
		w = os.Stdout
	}
	return func() tea.Msg {
		if _, err := io.WriteString(w, ansi.SetSystemClipboard(text)); err != nil {
			return errMsg{err: fmt.Errorf("copy to clipboard: %w", err), severity: nonFatal}
		}
		return copiedMsg{lines: lines}
	}
}
