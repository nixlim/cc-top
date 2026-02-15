// Package tui implements the Bubble Tea TUI for cc-top.
package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap defines all key bindings for the cc-top TUI.
type KeyMap struct {
	Quit       key.Binding
	Tab        key.Binding
	Up         key.Binding
	Down       key.Binding
	Enter      key.Binding
	Escape     key.Binding
	Filter     key.Binding
	KillSwitch key.Binding
	ScrollUp   key.Binding
	ScrollDown key.Binding
	Enable     key.Binding
	Fix        key.Binding
	Rescan     key.Binding
	Confirm    key.Binding
	Deny       key.Binding
}

// DefaultKeyMap returns the default key bindings for cc-top.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Quit: key.NewBinding(
			key.WithKeys("q"),
			key.WithHelp("q", "quit"),
		),
		Tab: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "toggle view"),
		),
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("up/k", "navigate up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("down/j", "navigate down"),
		),
		Enter: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "select/continue"),
		),
		Escape: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "back/cancel"),
		),
		Filter: key.NewBinding(
			key.WithKeys("f"),
			key.WithHelp("f", "filter events"),
		),
		KillSwitch: key.NewBinding(
			key.WithKeys("ctrl+k"),
			key.WithHelp("ctrl+k", "kill switch"),
		),
		ScrollUp: key.NewBinding(
			key.WithKeys("k"),
			key.WithHelp("k", "scroll up"),
		),
		ScrollDown: key.NewBinding(
			key.WithKeys("j"),
			key.WithHelp("j", "scroll down"),
		),
		Enable: key.NewBinding(
			key.WithKeys("e", "E"),
			key.WithHelp("E", "enable telemetry"),
		),
		Fix: key.NewBinding(
			key.WithKeys("F"),
			key.WithHelp("F", "fix misconfigured"),
		),
		Rescan: key.NewBinding(
			key.WithKeys("r", "R"),
			key.WithHelp("R", "rescan"),
		),
		Confirm: key.NewBinding(
			key.WithKeys("y", "Y"),
			key.WithHelp("Y", "confirm"),
		),
		Deny: key.NewBinding(
			key.WithKeys("n", "N"),
			key.WithHelp("n", "deny/cancel"),
		),
	}
}
