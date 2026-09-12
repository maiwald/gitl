package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"

	"github.com/maiwald/gitl/internal/git"
)

// shellNotFound is the exit status a shell uses for a command it couldn't
// find — the one pager failure that printed nothing and can still be
// recovered from.
const shellNotFound = 127

// handOffToPager gives raw to the pager git would have used had gitl not been
// configured for this command (git.FallbackPager), for input gitl has nothing
// to add to: anything carrying no patch it could parse. That text is already
// exactly what the user asked git for, colors included — gitl's own rendering
// of it could only be a plainer copy.
//
// The pager inherits gitl's environment, which is the one git built for a
// pager: LESS=FRX and LV=-c are already in it, so less behaves as it would
// have under git directly.
func handOffToPager(raw []byte) error {
	return pipeToPager(git.FallbackPager(context.Background(), cwd), raw, os.Stdout)
}

// pipeToPager runs pager with raw on its stdin, falling back to writing raw to
// out whenever no pager ran: none was resolvable, or the shell couldn't find
// the one that was. A pager that fails is a git command that fails, and the
// text it was given is right there.
func pipeToPager(pager string, raw []byte, out io.Writer) error {
	if pager == "" {
		_, err := out.Write(raw)
		return err
	}
	// Through a shell, like git: a pager is a command line ("less -FRX",
	// "delta | less"), not a program name.
	cmd := exec.Command("sh", "-c", pager)
	cmd.Stdin = bytes.NewReader(raw)
	cmd.Stdout = out
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == shellNotFound {
		_, err = out.Write(raw)
	}
	return err
}
