package ui

import "charm.land/bubbles/v2/key"

type keyMap struct {
	Up, Down, Filter, Edit, Refine, Sync, Doctor, Reload, Readme, Focus, Help, Quit, Back key.Binding
	Confirm, TogglePush, Install, Config                                                  key.Binding
	Toggle, Rename, Move                                                                  key.Binding
	Upstream, Update                                                                      key.Binding
	Tree, Collapse, Expand                                                                key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Up:         key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("↑/k", "up")),
		Down:       key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("↓/j", "down")),
		Filter:     key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		Edit:       key.NewBinding(key.WithKeys("e", "enter"), key.WithHelp("e", "edit")),
		Rename:     key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "rename (own only)")),
		Sync:       key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sync")),
		Move:       key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "move between scopes")),
		Install:    key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "install from registry")),
		Doctor:     key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "doctor")),
		Reload:     key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reload")),
		Readme:     key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "regenerate README index")),
		Config:     key.NewBinding(key.WithKeys("E"), key.WithHelp("E", "edit config")),
		Refine:     key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "refine (own only)")),
		Upstream:   key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "check upstream")),
		Update:     key.NewBinding(key.WithKeys("U"), key.WithHelp("U", "update outdated skill")),
		Toggle:     key.NewBinding(key.WithKeys("c", "x", "o"), key.WithHelp("c/x/o", "toggle claude/codex/omp")),
		Focus:      key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "focus preview")),
		Help:       key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:       key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Back:       key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Confirm:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "commit")),
		TogglePush: key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "toggle push")),
		Tree:       key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "tree view")),
		Collapse:   key.NewBinding(key.WithKeys("left"), key.WithHelp("←", "collapse group")),
		Expand:     key.NewBinding(key.WithKeys("right"), key.WithHelp("→", "expand group")),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Edit, k.Refine, k.Toggle, k.Sync, k.Install, k.Filter, k.Tree, k.Config, k.Doctor, k.Upstream, k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Filter, k.Tree, k.Collapse, k.Expand, k.Focus},
		{k.Edit, k.Refine, k.Toggle, k.Rename, k.Move, k.Sync, k.Install, k.Doctor, k.Reload, k.Readme, k.Upstream, k.Update},
		{k.Config, k.Help, k.Back, k.Quit},
	}
}
