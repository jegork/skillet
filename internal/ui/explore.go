package ui

import (
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

	"github.com/jegork/skillet/internal/explore"
)

// exploreState holds the vendor listing read when the view opened; it is
// refetched after every u upstream check lands so fresh cache data shows up.
type exploreState struct {
	skills []explore.Skill
}

type exploreItem struct {
	skill explore.Skill
}

func (e exploreItem) FilterValue() string { return e.skill.Name }

// key identifies the row across rebuilds: names repeat across vendors.
func (e exploreItem) key() string { return e.skill.Vendor + "\x00" + e.skill.Path }

func (m Model) startExplore() (tea.Model, tea.Cmd) {
	if m.cfg.Vendors == nil {
		m.flash = "explore not configured"
		return m, nil
	}
	m.mode = modeExplore
	m.list.ResetFilter()
	m.explore.skills = m.cfg.Vendors()
	return m, m.rebuildItems()
}

func (m Model) updateExplore(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Back):
		m.mode = modeList
		m.list.ResetFilter()
		return m, m.rebuildItems()
	case key.Matches(msg, m.keys.Upstream):
		return m, m.checkUpstream(true)
	case key.Matches(msg, m.keys.Confirm):
		return m.installExplore()
	}
	return m.forward(msg)
}

func (m Model) installExplore() (tea.Model, tea.Cmd) {
	e, ok := m.list.SelectedItem().(exploreItem)
	if !ok {
		return m, nil // a vendor group row
	}
	if e.skill.Installed {
		m.flash = e.skill.Name + " is already installed"
		return m, nil
	}
	if m.cfg.Install == nil {
		m.flash = "registry install not configured"
		return m, nil
	}
	return m, installSkill(m.cfg.Install, e.skill.Vendor, e.skill.Name)
}

// exploreItems builds the vendor-grouped rows: one group per vendor, its
// skill folders underneath, all in name order.
func (m *Model) exploreItems() []list.Item {
	byVendor := map[string][]explore.Skill{}
	var vendors []string
	for _, s := range m.explore.skills {
		if _, ok := byVendor[s.Vendor]; !ok {
			vendors = append(vendors, s.Vendor)
		}
		byVendor[s.Vendor] = append(byVendor[s.Vendor], s)
	}
	sort.Strings(vendors)
	var items []list.Item
	for _, v := range vendors {
		skills := byVendor[v]
		names := make([]string, len(skills))
		for i, s := range skills {
			names[i] = s.Name
		}
		items = append(items, groupItem{key: v, label: v, children: names})
		for _, s := range skills {
			items = append(items, exploreItem{skill: s})
		}
	}
	return items
}

func (m Model) explorePreview(e exploreItem) string {
	s := m.styles
	var b strings.Builder
	b.WriteString(s.title.Render(e.skill.Name) + "  " + s.faint.Render(e.skill.Vendor) + "\n")
	b.WriteString(s.faint.Render(e.skill.Path) + "\n")
	if e.skill.Installed {
		b.WriteString("\n" + s.ok.Render("installed"))
	} else {
		b.WriteString("\n" + s.ok.Render("available") + s.faint.Render(" · enter installs"))
	}
	return b.String()
}
