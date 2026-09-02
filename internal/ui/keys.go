package ui

import "github.com/charmbracelet/bubbles/key"

// KeyMap defines the vim-style key bindings shared across views.
type KeyMap struct {
	Up           key.Binding
	Down         key.Binding
	HalfPageUp   key.Binding
	HalfPageDown key.Binding
	Top          key.Binding
	Bottom       key.Binding
	Enter        key.Binding
	Back         key.Binding
	Quit         key.Binding
	PrevFile     key.Binding
	NextFile     key.Binding
	Search       key.Binding
	NextMatch    key.Binding
	PrevMatch    key.Binding
	// Select starts/ends a linewise selection in the detail view (vim's
	// shift-V), and Yank copies one, alongside Enter.
	Select key.Binding
	Yank   key.Binding
	// JumpBack and JumpForward step through the shared jumplist (see
	// jumplist in app.go), vim's ctrl+o/ctrl+i. JumpForward is bound to
	// "tab", not "ctrl+i": the two are the same byte (0x09) at the terminal
	// protocol level, and bubbletea's decoder reports it as tab (see its
	// KeyTab comment, "also ctrl+i"), so the literal string "ctrl+i" would
	// never match an actual keypress.
	JumpBack    key.Binding
	JumpForward key.Binding
}

// DefaultKeyMap is the vim-style binding set used throughout the app.
var DefaultKeyMap = KeyMap{
	Up:           key.NewBinding(key.WithKeys("up", "k")),
	Down:         key.NewBinding(key.WithKeys("down", "j")),
	HalfPageUp:   key.NewBinding(key.WithKeys("ctrl+u")),
	HalfPageDown: key.NewBinding(key.WithKeys("ctrl+d")),
	Top:          key.NewBinding(key.WithKeys("g")),
	Bottom:       key.NewBinding(key.WithKeys("G")),
	Enter:        key.NewBinding(key.WithKeys("enter")),
	Back:         key.NewBinding(key.WithKeys("esc", "q")),
	Quit:         key.NewBinding(key.WithKeys("ctrl+c")),
	PrevFile:     key.NewBinding(key.WithKeys("[")),
	NextFile:     key.NewBinding(key.WithKeys("]")),
	Search:       key.NewBinding(key.WithKeys("/")),
	NextMatch:    key.NewBinding(key.WithKeys("n")),
	PrevMatch:    key.NewBinding(key.WithKeys("N")),
	Select:       key.NewBinding(key.WithKeys("V")),
	Yank:         key.NewBinding(key.WithKeys("y")),
	JumpBack:     key.NewBinding(key.WithKeys("ctrl+o")),
	JumpForward:  key.NewBinding(key.WithKeys("tab")),
}
