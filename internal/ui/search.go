package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/jegork/skillet/internal/inventory"
	"github.com/jegork/skillet/internal/readme"
	"github.com/jegork/skillet/internal/registry"
)

type searchPhase int

const (
	searchInput searchPhase = iota
	searchSearching
	searchChoosing
	searchInstalling
)

type searchState struct {
	input   textinput.Model
	phase   searchPhase
	query   string
	results []registry.Result
	cursor  int
}

type searchDoneMsg struct {
	results []registry.Result
	err     error
}
type installDoneMsg struct {
	err    error
	source string
	skill  string
}

// installedMsg arrives after the stub + rescan + README pass: the inventory
// already contains the new skill.
type installedMsg struct {
	inv  inventory.Inventory
	err  error
	name string
}

func newSearchState() searchState {
	ti := textinput.New()
	ti.Prompt = "find skills: "
	ti.Placeholder = "query"
	ti.CursorEnd()
	return searchState{input: ti}
}

func findSkills(fn func(context.Context, string) ([]registry.Result, error), query string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		res, err := fn(ctx, query)
		return searchDoneMsg{results: res, err: err}
	}
}

func installSkill(fn func(context.Context, string, string) error, source, skill string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		return installDoneMsg{err: fn(ctx, source, skill), source: source, skill: skill}
	}
}

func (m Model) startSearch() (tea.Model, tea.Cmd) {
	if m.cfg.Find == nil {
		m.flash = "registry search not configured"
		return m, nil
	}
	m.mode = modeSearch
	m.search = newSearchState()
	return m, m.search.input.Focus()
}

func (m Model) updateSearch(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		m.mode = modeList
		m.search.input.Blur()
		return m, nil
	case key.Matches(msg, m.keys.Quit) && msg.String() == "ctrl+c":
		return m, tea.Quit
	}
	switch m.search.phase {
	case searchInput:
		if key.Matches(msg, m.keys.Confirm) {
			query := strings.TrimSpace(m.search.input.Value())
			if query == "" {
				return m, nil
			}
			m.search.query = query
			m.search.phase = searchSearching
			return m, findSkills(m.cfg.Find, query)
		}
		var cmd tea.Cmd
		m.search.input, cmd = m.search.input.Update(msg)
		return m, cmd
	case searchChoosing:
		if key.Matches(msg, m.keys.Up) {
			m.search.cursor = max(m.search.cursor-1, 0)
			return m, nil
		}
		if key.Matches(msg, m.keys.Down) {
			m.search.cursor = min(m.search.cursor+1, len(m.search.results)-1)
			return m, nil
		}
		if key.Matches(msg, m.keys.Confirm) {
			if len(m.search.results) == 0 {
				return m, nil
			}
			if m.cfg.Install == nil {
				m.flash = "registry install not configured"
				return m, nil
			}
			r := m.search.results[m.search.cursor]
			m.search.phase = searchInstalling
			return m, installSkill(m.cfg.Install, r.Source, r.Skill)
		}
	}
	return m, nil
}

func (m Model) searchDone(msg searchDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.search.phase = searchInput
		m.flash = "search failed: " + msg.err.Error()
		return m, nil
	}
	m.search.results = msg.results
	m.search.cursor = 0
	m.search.phase = searchChoosing
	return m, nil
}

func (m Model) installDone(msg installDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.search.phase = searchChoosing
		m.flash = "install failed: " + msg.err.Error()
		return m, nil
	}
	return m, m.finishInstall(msg.skill)
}

// finishInstall makes the claude stub, rescans and regenerates the README
// index over the fresh scan, then hands the inventory back.
func (m Model) finishInstall(name string) tea.Cmd {
	load := m.cfg.Load
	consumers := m.cfg.Consumers
	paths := m.inv.Paths
	return func() tea.Msg {
		for _, c := range consumers {
			if c.Name() != "claude" {
				continue
			}
			if err := c.Enable(name); err != nil {
				return installedMsg{err: fmt.Errorf("claude stub: %w", err), name: name}
			}
		}
		inv, err := load()
		if err != nil {
			return installedMsg{err: err, name: name}
		}
		if _, err := readme.Regenerate(paths.Readme(), inv.Skills); err != nil {
			return installedMsg{inv: inv, err: fmt.Errorf("README: %w", err), name: name}
		}
		return installedMsg{inv: inv, name: name}
	}
}

func (m Model) installed(msg installedMsg) (tea.Model, tea.Cmd) {
	m.mode = modeList
	m.search = searchState{}
	if msg.err != nil {
		m.flash = "installed " + msg.name + ", but: " + msg.err.Error()
		return m, m.reload()
	}
	m.selectNext = msg.name
	m.setInventory(msg.inv)
	m.flash = "installed " + msg.name
	return m, nil
}

func (m Model) renderSearch() string {
	s := m.styles
	var head string
	switch m.search.phase {
	case searchInput:
		head = m.search.input.View()
	case searchSearching:
		head = "searching skills.sh for " + s.accent.Render(m.search.query) + "…"
	case searchChoosing:
		head = s.title.Render("results for "+m.search.query) + "  " +
			s.faint.Render("↑↓ move · enter install · esc cancel")
	case searchInstalling:
		r := m.search.results[m.search.cursor]
		head = "installing " + r.Source + "@" + r.Skill + "…"
	}
	lines := []string{head}
	if m.search.phase == searchChoosing {
		if len(m.search.results) == 0 {
			lines = append(lines, s.faint.Render("no results"))
		}
		for i, r := range m.search.results {
			text := r.Source + "@" + r.Skill + "  " + r.Installs + " installs"
			if r.URL != "" {
				text += "  " + r.URL
			}
			if i == m.search.cursor {
				lines = append(lines, s.selected.Render("▸ "+text))
			} else {
				lines = append(lines, "  "+text)
			}
		}
	}
	return lipgloss.NewStyle().MaxWidth(max(m.width, 1)).Render(strings.Join(lines, "\n"))
}
