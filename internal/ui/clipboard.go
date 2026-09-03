package ui

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// copiedMsg reports a successful copy, for the status bar to confirm.
type copiedMsg struct{ lines int }

// copyCmd puts text on the clipboard by every route available at once: an
// OSC 52 sequence written to the terminal, plus whichever local clipboard
// commands this machine turns out to have (see clipboardTools).
//
// Every one of these is fire-and-forget — OSC 52 has no reply to wait for, and
// a tool that exits 0 only proves *a* clipboard took the text, not the one the
// user is about to paste into. So rather than pick one and hope, all of them
// run: they are idempotent and carry identical text, so a machine where
// several work just sets the same clipboard twice, and there is no environment
// left where the copy quietly does nothing. The status bar's confirmation is
// therefore about the attempt, not the outcome; nothing here can do better.
//
// OSC 52 stays the one that always runs because it is the only one that
// reaches the clipboard of the machine the *terminal* is on, which is the
// right one over ssh, where every local tool would set a clipboard nobody can
// see. Its write goes to the terminal alongside bubbletea's own frames, from a
// different goroutine, so it is deliberately a single Write of one
// self-contained sequence: the terminal parses a complete OSC 52 wherever it
// lands, and the keypress that triggered it cleared the selection, so the
// frame after it repaints every row that was highlighted anyway.
func copyCmd(w io.Writer, text string, lines int) tea.Cmd {
	if w == nil {
		w = os.Stdout
	}
	return func() tea.Msg {
		// Only the terminal write is reported: it failing means the tty
		// itself has gone, while a missing or failing local tool is the
		// ordinary case on most machines and says nothing about whether the
		// copy worked.
		if _, err := io.WriteString(w, ansi.SetSystemClipboard(text)); err != nil {
			return errMsg{err: fmt.Errorf("copy to clipboard: %w", err), severity: nonFatal}
		}
		copyLocally(text)
		return copiedMsg{lines: lines}
	}
}

// copyLocally runs the local clipboard tools, a var so tests can stub out the
// subprocesses — the real ones would clobber the clipboard of whoever is
// running `go test`.
var copyLocally = runClipboardTools

// clipboardToolTimeout bounds a clipboard tool that neither exits nor reads
// its stdin. Well over what any of them need; it exists so a wedged tool
// can't hold the command goroutine forever.
const clipboardToolTimeout = 2 * time.Second

// clipboardTools is the local clipboard commands worth trying on this machine,
// each taking the text on stdin.
//
// tmux comes first and is what makes a copy inside tmux work at all: tmux's
// default set-clipboard is `external`, under which it refuses to let an
// application set the clipboard and swallows the OSC 52 above rather than
// forwarding it. `load-buffer -w` is tmux setting the clipboard itself, which
// is precisely what `external` does allow, so it needs no tmux.conf change
// (the -w flag needs tmux 3.2+; older tmux fails here and sets nothing, which
// is no worse than the swallowed sequence).
func clipboardTools() [][]string {
	var tools [][]string
	if os.Getenv("TMUX") != "" {
		tools = append(tools, []string{"tmux", "load-buffer", "-w", "-"})
	}
	switch runtime.GOOS {
	case "darwin":
		tools = append(tools, []string{"pbcopy"})
	case "windows":
		tools = append(tools, []string{"clip"})
	default:
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			tools = append(tools, []string{"wl-copy"})
		}
		if os.Getenv("DISPLAY") != "" {
			tools = append(tools,
				[]string{"xclip", "-selection", "clipboard"},
				[]string{"xsel", "--clipboard", "--input"},
			)
		}
		// WSL has neither, but the Windows clipboard is one exec away.
		tools = append(tools, []string{"clip.exe"})
	}
	return tools
}

// runClipboardTools runs every clipboard tool this machine has, ignoring the
// ones it doesn't and the ones that fail. Not first-success-wins: which tool
// owns the clipboard the user will paste from isn't knowable from in here —
// on a remote machine it's tmux and not pbcopy, and nothing distinguishes that
// from the local case reliably — so they all get the text.
func runClipboardTools(text string) {
	for _, tool := range clipboardTools() {
		path, err := exec.LookPath(tool[0])
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), clipboardToolTimeout)
		cmd := exec.CommandContext(ctx, path, tool[1:]...)
		cmd.Stdin = strings.NewReader(text)
		_ = cmd.Run()
		cancel()
	}
}
