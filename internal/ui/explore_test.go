package ui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/jegork/skillet/internal/explore"
	"github.com/jegork/skillet/internal/inventory"
	"github.com/jegork/skillet/internal/testhome"
)

// newExploreModel builds a home with one vendored skill and injected vendor
// listing and install functions. The install function really writes the
// skill folder, so the rescan after install sees it.
func newExploreModel(t *testing.T, install func(ctx context.Context, source, name string) error) (Model, *[][]string, *int, *testhome.Home) {
	t.Helper()
	h := testhome.New(t)
	h.Skill("alpha", "does alpha things")
	h.Skill("vend", "vendored one")
	h.Lock(map[string]string{"vend": "acme/skills"})
	h.Readme("| `alpha` | own | does alpha things |", "| `vend` | vendored (acme/skills) | vendored one |")
	inv, err := inventory.Load(h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	listing := []explore.Skill{
		{Vendor: "acme/skills", Path: ".", Name: "skills"},
		{Vendor: "acme/skills", Path: "skills/vend", Name: "vend", Installed: true},
		{Vendor: "acme/skills", Path: "skills/pick", Name: "pick"},
		{Vendor: "other/repo", Path: "skills/their", Name: "their"},
	}
	fetches := 0
	installs := &[][]string{}
	m := New(Config{
		Inventory: inv,
		Load:      func() (inventory.Inventory, error) { return inventory.Load(h.Dir) },
		Consumers: inventory.Consumers(h.Dir),
		Vendors: func() []explore.Skill {
			fetches++
			return listing
		},
		Install: install,
	})
	return m, installs, &fetches, h
}

func openExplore(t *testing.T, m Model) Model {
	t.Helper()
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = press(next.(Model), "a")
	if m.mode != modeExplore {
		t.Fatalf("mode = %v, want explore", m.mode)
	}
	return m
}

func TestExploreKeyListsVendors(t *testing.T) {
	m, _, fetches, _ := newExploreModel(t, nil)
	m = openExplore(t, m)
	if *fetches != 1 {
		t.Errorf("vendors fetched %d times, want 1", *fetches)
	}
	v := view(m)
	for _, want := range []string{"acme/skills", "other/repo", "pick", "their", "installed", "available"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}
	// the installed skill is not offered again
	if strings.Contains(v, "pick-ui") {
		t.Error("unexpected row")
	}
}

func TestExploreKeyWithoutConfig(t *testing.T) {
	m := newTestModel(t)
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m = next.(Model)
	if cmd != nil {
		t.Error("no vendors source must not build a view")
	}
	if m.mode == modeExplore {
		t.Error("mode changed without a vendors source")
	}
	if !strings.Contains(m.flash, "explore") {
		t.Errorf("flash %q", m.flash)
	}
}

func TestExploreFilterNarrows(t *testing.T) {
	m, _, _, _ := newExploreModel(t, nil)
	m = openExplore(t, m)
	m = press(m, "/", "p", "i", "c", "k", "enter")
	if got := len(m.list.VisibleItems()); got != 2 {
		t.Fatalf("visible items %d, want group + match", got)
	}
	v := view(m)
	if !strings.Contains(v, "pick") || strings.Contains(v, "their") {
		t.Errorf("filter should hide other rows:\n%s", v)
	}
}

func TestExploreInstallRunsRegistryPath(t *testing.T) {
	installs := &[][]string{}
	var h *testhome.Home
	m, _, _, gotHome := newExploreModel(t, func(ctx context.Context, source, name string) error {
		*installs = append(*installs, []string{source, name})
		h.Skill(name, "does "+name+" things")
		return nil
	})
	h = gotHome
	m = openExplore(t, m)
	// rows: acme/skills group, ".", vend (installed), pick; pick is third down
	m = press(m, "j", "j", "j")
	if e, ok := m.list.SelectedItem().(exploreItem); !ok || e.skill.Name != "pick" {
		t.Fatalf("selected %v", m.list.SelectedItem())
	}
	m = press(m, "enter")
	if len(*installs) != 1 || (*installs)[0][0] != "acme/skills" || (*installs)[0][1] != "pick" {
		t.Fatalf("installs = %v", *installs)
	}
	if m.mode != modeList {
		t.Fatalf("after install mode = %v, want list", m.mode)
	}
	// the rescan saw the new skill and the row got selected
	if it, ok := m.list.SelectedItem().(item); !ok || it.skill.Name != "pick" {
		t.Fatalf("selected %v, want the new pick row", m.list.SelectedItem())
	}
	if !strings.Contains(view(m), "installed pick") {
		t.Errorf("flash:\n%s", view(m))
	}
}

func TestExploreInstallFailureStaysInView(t *testing.T) {
	m, _, _, _ := newExploreModel(t, func(ctx context.Context, source, name string) error {
		return context.DeadlineExceeded
	})
	m = openExplore(t, m)
	m = press(m, "j", "j", "j", "enter")
	if m.mode != modeExplore {
		t.Fatalf("mode = %v, want explore after failure", m.mode)
	}
	if !strings.Contains(m.flash, "install failed") {
		t.Errorf("flash %q", m.flash)
	}
}

func TestExploreEnterOnInstalledDoesNotInstall(t *testing.T) {
	installs := &[][]string{}
	m, _, _, _ := newExploreModel(t, func(ctx context.Context, source, name string) error {
		*installs = append(*installs, []string{source, name})
		return nil
	})
	m = openExplore(t, m)
	m = press(m, "j", "j") // vend, installed
	if e, ok := m.list.SelectedItem().(exploreItem); !ok || !e.skill.Installed {
		t.Fatalf("selected %v", m.list.SelectedItem())
	}
	m = press(m, "enter")
	if len(*installs) != 0 {
		t.Errorf("installed rows must not reinstall: %v", *installs)
	}
	if !strings.Contains(m.flash, "already installed") {
		t.Errorf("flash %q", m.flash)
	}
}

func TestExploreEnterOnGroupDoesNothing(t *testing.T) {
	installs := &[][]string{}
	m, _, _, _ := newExploreModel(t, func(ctx context.Context, source, name string) error {
		*installs = append(*installs, []string{source, name})
		return nil
	})
	m = openExplore(t, m)
	if _, ok := m.list.SelectedItem().(groupItem); !ok {
		t.Fatalf("selected %v, want the vendor group", m.list.SelectedItem())
	}
	m = press(m, "enter")
	if len(*installs) != 0 {
		t.Errorf("group rows must not install: %v", *installs)
	}
}

func TestExploreEscReturnsToList(t *testing.T) {
	m, _, _, _ := newExploreModel(t, nil)
	m = openExplore(t, m)
	m = press(m, "/", "p", "enter")
	m = press(m, "esc")
	if m.mode != modeList {
		t.Fatalf("mode = %v, want list", m.mode)
	}
	if m.list.IsFiltered() {
		t.Error("leaving explore must clear the filter")
	}
	if got := len(m.list.VisibleItems()); got != 2 {
		t.Errorf("visible items %d, want the 2 inventory skills", got)
	}
}
