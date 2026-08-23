package ui

import "github.com/maiwald/gitl/internal/git"

// entryTitle is the pseudo-entry's title in the detail view header, in its
// list row, and in the sticky header, where it stands in for the literal
// "Commit" a real commit shows.
func entryTitle(k git.EntryKind) string {
	switch k {
	case git.EntryStaged:
		return "Staged changes"
	case git.EntryWorktree:
		return "Working tree"
	default:
		return ""
	}
}

// entryDescription is the dim one-line subtitle shown under a pseudo
// entry's title in the detail view header.
func entryDescription(k git.EntryKind) string {
	switch k {
	case git.EntryStaged:
		return "Changes staged for commit (index vs HEAD)."
	case git.EntryWorktree:
		return "Uncommitted changes in the working tree, including untracked files."
	default:
		return ""
	}
}
