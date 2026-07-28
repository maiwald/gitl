package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	xterm "github.com/charmbracelet/x/term"
	"github.com/maiwald/gitl/internal/git"
	"github.com/maiwald/gitl/internal/ui"
	"github.com/mattn/go-isatty"
)

// fallbackListWidth is used when stdout isn't a real terminal (piped or
// redirected), so -n output stays stable/scriptable instead of depending
// on whatever width a non-TTY happens to report.
const fallbackListWidth = 120

func main() {
	n := flag.Int("n", 0, "print the N most recent commits non-interactively and exit")
	flag.Parse()

	if *n > 0 {
		if err := printCommits(*n); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	p := tea.NewProgram(ui.NewModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func printCommits(n int) error {
	ctx := context.Background()
	repo, err := git.OpenRepo(ctx, ".")
	if err != nil {
		return err
	}
	hasCommits, err := repo.HasCommits(ctx)
	if err != nil {
		return err
	}
	if !hasCommits {
		fmt.Println("this repository has no commits yet")
		return nil
	}
	commits, err := repo.FetchLogBatch(ctx, 0, n)
	if err != nil {
		return err
	}
	fmt.Println(ui.RenderCommitList(commits, listWidth()))
	return nil
}

func listWidth() int {
	fd := os.Stdout.Fd()
	if isatty.IsTerminal(fd) {
		if w, _, err := xterm.GetSize(fd); err == nil && w > 0 {
			return w
		}
	}
	return fallbackListWidth
}
