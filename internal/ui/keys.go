package ui

import "github.com/charmbracelet/bubbles/key"

// KeyMap defines the vim-style key bindings shared across views.
type KeyMap struct {
	Up           key.Binding
	Down         key.Binding
	Left         key.Binding
	Right        key.Binding
	HalfPageUp   key.Binding
	HalfPageDown key.Binding
	Top          key.Binding
	Bottom       key.Binding
	Enter        key.Binding
	Back         key.Binding
	Quit         key.Binding
	Tab          key.Binding
	Search       key.Binding
	NextMatch    key.Binding
	PrevMatch    key.Binding
}

// DefaultKeyMap is the vim-style binding set used throughout the app.
var DefaultKeyMap = KeyMap{
	Up:           key.NewBinding(key.WithKeys("up", "k")),
	Down:         key.NewBinding(key.WithKeys("down", "j")),
	Left:         key.NewBinding(key.WithKeys("left", "h")),
	Right:        key.NewBinding(key.WithKeys("right", "l")),
	HalfPageUp:   key.NewBinding(key.WithKeys("ctrl+u")),
	HalfPageDown: key.NewBinding(key.WithKeys("ctrl+d")),
	Top:          key.NewBinding(key.WithKeys("g")),
	Bottom:       key.NewBinding(key.WithKeys("G")),
	Enter:        key.NewBinding(key.WithKeys("enter")),
	Back:         key.NewBinding(key.WithKeys("esc", "q")),
	Quit:         key.NewBinding(key.WithKeys("ctrl+c")),
	Tab:          key.NewBinding(key.WithKeys("tab", "shift+tab")),
	Search:       key.NewBinding(key.WithKeys("/")),
	NextMatch:    key.NewBinding(key.WithKeys("n")),
	PrevMatch:    key.NewBinding(key.WithKeys("N")),
}
