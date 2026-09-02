package ui

import (
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jegork/skillet/internal/inventory"
	"github.com/jegork/skillet/internal/testhome"
)

func treeModel(t *testing.T) Model {
	t.Helper()
	m := newTestModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	return press(next.(Model), "t")
}

func order(m Model) []string {
	var out []string
	for _, it := range m.list.Items() {
		switch v := it.(type) {
		case groupItem:
			out = append(out, "group:"+v.key)
		case item:
			out = append(out, v.skill.Name)
		}
	}
	return out
}

func visibleOrder(m Model) []string {
	var out []string
	for _, it := range m.list.VisibleItems() {
		switch v := it.(type) {
		case groupItem:
			out = append(out, "group:"+v.key)
		case item:
			out = append(out, v.skill.Name)
		}
	}
	return out
}

func TestTreeGroupsOwnFirstThenSources(t *testing.T) {
	m := treeModel(t)
	want := []string{"group:own", "alpha", "beta", "group:acme/skills", "vend"}
	if got := order(m); !slices.Equal(got, want) {
		t.Errorf("tree order %v, want %v", got, want)
	}
	if !strings.Contains(view(m), "3/3") {
		t.Errorf("status bar must count skills, not groups:\n%s", view(m))
	}
}

func TestTreeEnterAndSpaceToggleCollapse(t *testing.T) {
	m := press(treeModel(t), "k") // up from alpha onto the own group
	m = press(m, "enter")
	if got := order(m); !slices.Equal(got, []string{"group:own", "group:acme/skills", "vend"}) {
		t.Errorf("after enter (collapse own): %v", got)
	}
	m = press(m, " ")
	if got := order(m); len(got) != 5 {
		t.Errorf("after space (expand own): %v", got)
	}
}

func TestTreeArrowsCollapseAndSelectGroup(t *testing.T) {
	m := press(treeModel(t), "j", "j")
	if _, ok := m.list.SelectedItem().(groupItem); !ok {
		t.Fatalf("expected the acme group selected, got %#v", m.list.SelectedItem())
	}
	m = press(m, "left")
	if got := order(m); !slices.Equal(got, []string{"group:own", "alpha", "beta", "group:acme/skills"}) {
		t.Errorf("left on a group should collapse it: %v", got)
	}
	m = press(m, "right")
	if got := order(m); len(got) != 5 {
		t.Errorf("right should expand the group: %v", got)
	}
	if _, ok := m.list.SelectedItem().(groupItem); !ok {
		t.Errorf("selection should stay on the group after expand")
	}
}

func TestTreeFilterHidesEmptyGroups(t *testing.T) {
	m := press(treeModel(t), "/", "b", "e", "t", "enter")
	got := visibleOrder(m)
	if !slices.Contains(got, "group:own") || !slices.Contains(got, "beta") {
		t.Errorf("filtered tree should keep the matching group and skill, got %v", got)
	}
	if slices.Contains(got, "group:acme/skills") {
		t.Errorf("group without matches must be hidden, got %v", got)
	}
	if !strings.Contains(view(m), "1/3") {
		t.Errorf("status bar should count 1 visible skill:\n%s", view(m))
	}
}

func TestTreeFilterShowsCollapsedChildren(t *testing.T) {
	m := press(treeModel(t), "enter", "/", "a", "l", "p", "enter")
	if !slices.Contains(visibleOrder(m), "alpha") {
		t.Errorf("filter must match children of collapsed groups, got %v", visibleOrder(m))
	}
}

func TestTreeVerbsOnSkillRows(t *testing.T) {
	m := press(treeModel(t), "j", "j", "j", "j")
	if m.selectedName() != "vend" {
		t.Fatalf("expected vend selected, got %q", m.selectedName())
	}
	m = press(m, "e")
	if !strings.Contains(m.flash, "edit disabled") {
		t.Errorf("edit must stay disabled for vendored skills, flash %q", m.flash)
	}
	m = press(m, "c")
	if !strings.Contains(m.flash, "vend: claude enabled") {
		t.Errorf("consumer toggle must work from the tree, flash %q", m.flash)
	}
	if m.selectedName() != "vend" {
		t.Errorf("selection should survive the reload, got %q", m.selectedName())
	}
}

func TestTreePreviewFollowsGroup(t *testing.T) {
	m := press(treeModel(t), "j", "j")
	if _, ok := m.list.SelectedItem().(groupItem); !ok {
		t.Fatalf("expected a group selected, got %#v", m.list.SelectedItem())
	}
	if !strings.Contains(m.preview.GetContent(), "acme/skills") {
		t.Errorf("preview should describe the group:\n%s", m.preview.GetContent())
	}
}

func TestTreeToggleBackToFlatKeepsSelection(t *testing.T) {
	m := press(treeModel(t), "j")
	if m.selectedName() != "beta" {
		t.Fatalf("expected beta selected, got %q", m.selectedName())
	}
	m = press(m, "t")
	if got := order(m); !slices.Equal(got, []string{"alpha", "beta", "vend"}) {
		t.Errorf("flat order after toggle: %v", got)
	}
	if m.selectedName() != "beta" {
		t.Errorf("selection should survive the mode switch, got %q", m.selectedName())
	}
}

func TestTreeRenameReloadsInTreeMode(t *testing.T) {
	m := press(treeModel(t), "n", "2", "enter")
	if m.selectedName() != "alpha2" {
		t.Errorf("renamed skill should stay selected in tree mode, got %q", m.selectedName())
	}
	if got := order(m); !slices.Contains(got, "alpha2") {
		t.Errorf("tree should contain the renamed skill: %v", got)
	}
}

func TestTreeEmptyInventory(t *testing.T) {
	h := testhome.New(t)
	inv, err := inventory.Load(h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	m := New(Config{
		Inventory: inv,
		Load:      func() (inventory.Inventory, error) { return inventory.Load(h.Dir) },
		Consumers: inventory.Consumers(h.Dir),
	})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = press(next.(Model), "t")
	if got := order(m); len(got) != 0 {
		t.Errorf("empty inventory should render no tree rows, got %v", got)
	}
	if !strings.Contains(view(m), "0/0") {
		t.Errorf("status bar should show 0/0:\n%s", view(m))
	}
}

func TestGroupRowRendering(t *testing.T) {
	d := delegate{styles: newStyles(true), consumers: []string{"claude", "codex", "omp"}, now: func() time.Time { return time.Now() }}
	g := groupItem{key: "own", label: "own", children: []string{"alpha", "beta"}}
	d.expanded = func(string) bool { return true }
	if row := ansi.Strip(d.groupRow(g, false)); !strings.Contains(row, "▾ own (2)") {
		t.Errorf("expanded group row %q", row)
	}
	d.expanded = func(string) bool { return false }
	if row := ansi.Strip(d.groupRow(g, false)); !strings.Contains(row, "▸ own (2)") {
		t.Errorf("collapsed group row %q", row)
	}
	d.expanded = func(string) bool { return true }
	if row := ansi.Strip(d.groupRow(g, true)); !strings.Contains(row, "> ▾ own") {
		t.Errorf("selected group row %q", row)
	}
}
