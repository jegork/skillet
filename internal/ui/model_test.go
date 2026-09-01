package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

	"github.com/jegork/skillet/internal/inventory"
	"github.com/jegork/skillet/internal/testhome"
)

func newTestModel(t *testing.T) Model {
	t.Helper()
	h := testhome.New(t)
	h.Skill("alpha", "does alpha things")
	h.Skill("beta", "does beta things")
	h.Skill("vend", "vendored one")
	h.Lock(map[string]string{"vend": "acme/skills"})
	h.Stub(".claude/skills", "alpha", "../../.agents/skills/alpha")
	h.Stub(".claude/skills", "gone", "../../.agents/skills/gone")
	h.Readme("| `alpha` | own | does alpha things |", "| `beta` | own | does beta things |", "| `vend` | vendored (acme/skills) | vendored one |")
	inv, err := inventory.Load(h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	m := New(Config{Inventory: inv, Load: func() (inventory.Inventory, error) { return inventory.Load(h.Dir) }})
	return m
}

func press(m Model, keys ...string) Model {
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		default:
			msg = tea.KeyPressMsg{Code: rune(k[0]), Text: k}
		}
		m = apply(m, msg)
	}
	return m
}

// apply runs Update and feeds back the data messages produced by returned
// commands. Cursor blink ticks are dropped: they would loop forever.
func apply(m Model, msg tea.Msg) Model {
	next, cmd := m.Update(msg)
	m = next.(Model)
	if cmd == nil {
		return m
	}
	var feed func(tea.Msg)
	feed = func(out tea.Msg) {
		switch out := out.(type) {
		case list.FilterMatchesMsg, inventoryMsg, statusMsg:
			m = apply(m, out)
		case tea.BatchMsg:
			for _, c := range out {
				if c != nil {
					feed(c())
				}
			}
		}
	}
	feed(cmd())
	return m
}

func view(m Model) string { return m.View().Content }

func TestListRendersSkillsAndStatus(t *testing.T) {
	m := newTestModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = next.(Model)
	out := view(m)
	for _, want := range []string{"alpha", "beta", "vend", "acme/skills", "store: none", "doctor:", "does alpha things", filepath.Join(".agents", "skills", "alpha")} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}
	lines := strings.Split(out, "\n")
	if len(lines) != 40 {
		t.Errorf("view has %d lines, want 40", len(lines))
	}
	if !strings.Contains(lines[len(lines)-1], "store: none") {
		t.Errorf("status bar must be the last line, got %q", lines[len(lines)-1])
	}
}

func TestFilterNarrowsList(t *testing.T) {
	m := newTestModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = press(next.(Model), "/", "b", "e", "t", "enter")
	if got := len(m.list.VisibleItems()); got != 1 {
		t.Fatalf("visible items %d, want 1", got)
	}
	if !strings.Contains(view(m), "1/3") {
		t.Errorf("status bar should show 1/3:\n%s", view(m))
	}
	if !strings.Contains(view(m), "does beta things") || m.lastSelected != "beta" {
		t.Errorf("preview should follow the filtered selection, last=%q", m.lastSelected)
	}
	m = press(m, "esc")
	if got := len(m.list.VisibleItems()); got != 3 {
		t.Errorf("after esc visible %d, want 3", got)
	}
}

func TestEditRefusesVendored(t *testing.T) {
	m := newTestModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = press(next.(Model), "j", "j") // alpha, beta, vend
	if it := m.list.SelectedItem().(item); it.skill.Name != "vend" {
		t.Fatalf("selected %s", it.skill.Name)
	}
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	m = next.(Model)
	if cmd != nil {
		t.Error("vendored edit must not launch an editor")
	}
	if !strings.Contains(m.flash, "edit disabled") {
		t.Errorf("flash %q", m.flash)
	}
}

func TestDoctorModeShowsFindings(t *testing.T) {
	m := newTestModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = press(next.(Model), "d")
	if out := view(m); !strings.Contains(out, "dangling symlink") || !strings.Contains(out, "gone") {
		t.Errorf("doctor view missing stub finding:\n%s", out)
	}
	m = press(m, "d")
	if out := view(m); strings.Contains(out, "dangling symlink") {
		t.Error("doctor view should close on second d")
	}
}

func TestSyncWithoutStore(t *testing.T) {
	m := newTestModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = press(next.(Model), "s")
	if m.mode != modeList || !strings.Contains(m.flash, "no store") {
		t.Errorf("mode %v flash %q", m.mode, m.flash)
	}
}
