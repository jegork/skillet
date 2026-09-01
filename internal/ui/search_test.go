package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/jegork/skillet/internal/inventory"
	"github.com/jegork/skillet/internal/registry"
	"github.com/jegork/skillet/internal/testhome"
)

func newSearchModel(t *testing.T, find func(context.Context, string) ([]registry.Result, error), install func(context.Context, string, string) error) (Model, *testhome.Home) {
	t.Helper()
	h := testhome.New(t)
	h.Skill("alpha", "does alpha things")
	inv, err := inventory.Load(h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	m := New(Config{
		Inventory: inv,
		Load:      func() (inventory.Inventory, error) { return inventory.Load(h.Dir) },
		Consumers: inventory.Consumers(h.Dir),
		Find:      find,
		Install:   install,
	})
	return sized(m), h
}

func sized(m Model) Model {
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	return next.(Model)
}
func TestSearchInstallFlow(t *testing.T) {
	var calls []string
	var home *testhome.Home
	m, h := newSearchModel(t,
		func(ctx context.Context, q string) ([]registry.Result, error) {
			calls = append(calls, "find:"+q)
			return []registry.Result{
				{Source: "acme/tools", Skill: "deploy", Installs: "12K", URL: "https://skills.sh/acme/tools/deploy"},
				{Source: "acme/tools", Skill: "release", Installs: "3K"},
			}, nil
		},
		func(ctx context.Context, source, skill string) error {
			calls = append(calls, "install:"+source+"@"+skill)
			home.Skill(skill, "installed from the registry")
			home.Lock(map[string]string{skill: source})
			return nil
		},
	)
	home = h
	m = press(m, "i")
	if m.mode != modeSearch || m.search.phase != searchInput {
		t.Fatalf("mode %v phase %v", m.mode, m.search.phase)
	}
	m = press(m, "enter")
	if m.search.phase != searchInput {
		t.Errorf("empty query must stay in the input, phase %v", m.search.phase)
	}
	m = press(m, "d", "e", "p", "l", "o", "y", "enter")
	if m.search.phase != searchChoosing || len(m.search.results) != 2 {
		t.Fatalf("phase %v results %d", m.search.phase, len(m.search.results))
	}
	if out := view(m); !strings.Contains(out, "acme/tools@deploy") || !strings.Contains(out, "12K installs") {
		t.Errorf("results not rendered:\n%s", out)
	}
	m = press(m, "j")
	if m.search.cursor != 1 {
		t.Errorf("cursor %d after one j", m.search.cursor)
	}
	m = press(m, "k", "enter")
	if m.mode != modeList || m.flash != "installed deploy" {
		t.Fatalf("mode %v flash %q", m.mode, m.flash)
	}
	if m.selectedName() != "deploy" {
		t.Errorf("selected %q, want the new skill", m.selectedName())
	}
	if !m.inv.Reports["claude"].Enabled["deploy"] {
		t.Error("claude stub was not enabled")
	}
	if _, err := os.Stat(filepath.Join(h.Dir, ".claude", "skills", "deploy")); err != nil {
		t.Errorf("claude stub missing: %v", err)
	}
	readme, err := os.ReadFile(filepath.Join(h.SkillsDir(), "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readme), "| `deploy` | vendored (acme/tools) |") {
		t.Errorf("README index not updated:\n%s", readme)
	}
	if len(calls) != 2 || calls[0] != "find:deploy" || calls[1] != "install:acme/tools@deploy" {
		t.Errorf("calls %v", calls)
	}
}

func TestSearchResultsCursorBounds(t *testing.T) {
	m, _ := newSearchModel(t,
		func(ctx context.Context, q string) ([]registry.Result, error) {
			return []registry.Result{{Source: "a/b", Skill: "c", Installs: "1"}}, nil
		}, nil)
	m = press(m, "i", "x", "enter")
	m = press(m, "j", "j", "j")
	if m.search.cursor != 0 {
		t.Errorf("cursor %d, single result must not move", m.search.cursor)
	}
	m = press(m, "k")
	if m.search.cursor != 0 {
		t.Errorf("cursor %d, must not go negative", m.search.cursor)
	}
}

func TestSearchFindError(t *testing.T) {
	m, _ := newSearchModel(t,
		func(ctx context.Context, q string) ([]registry.Result, error) {
			return nil, errors.New("network down")
		}, nil)
	m = press(m, "i", "q", "enter")
	if m.search.phase != searchInput || !strings.Contains(m.flash, "search failed") {
		t.Fatalf("phase %v flash %q", m.search.phase, m.flash)
	}
	m = press(m, "esc")
	if m.mode != modeList {
		t.Errorf("esc should leave search, mode %v", m.mode)
	}
}

func TestInstallErrorStaysInResults(t *testing.T) {
	m, _ := newSearchModel(t,
		func(ctx context.Context, q string) ([]registry.Result, error) {
			return []registry.Result{{Source: "a/b", Skill: "c", Installs: "1"}}, nil
		},
		func(ctx context.Context, source, skill string) error {
			return errors.New("pnpx exploded")
		})
	m = press(m, "i", "x", "enter", "enter")
	if m.mode != modeSearch || m.search.phase != searchChoosing {
		t.Fatalf("mode %v phase %v", m.mode, m.search.phase)
	}
	if !strings.Contains(m.flash, "install failed") {
		t.Errorf("flash %q", m.flash)
	}
}

func TestSearchNotConfigured(t *testing.T) {
	h := testhome.New(t)
	inv, err := inventory.Load(h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	m := sized(New(Config{Inventory: inv}))
	m = press(m, "i")
	if m.mode != modeList || !strings.Contains(m.flash, "not configured") {
		t.Fatalf("mode %v flash %q", m.mode, m.flash)
	}
}

func TestSearchEscCancelsFromInput(t *testing.T) {
	m, _ := newSearchModel(t, nil, nil)
	m = press(m, "i", "a", "esc")
	if m.mode != modeList {
		t.Errorf("mode %v", m.mode)
	}
}
func TestInstallStubFailureStillReloads(t *testing.T) {
	m, h := newSearchModel(t, nil, nil)
	h.Skill("blocked", "target")
	// a plain directory where the stub belongs makes claude Enable refuse
	if err := os.MkdirAll(filepath.Join(h.Dir, ".claude", "skills", "blocked"), 0o755); err != nil {
		t.Fatal(err)
	}
	m = apply(m, installDoneMsg{source: "a/b", skill: "blocked"})
	if m.mode != modeList {
		t.Fatalf("mode %v", m.mode)
	}
	if !strings.Contains(m.flash, "installed blocked, but") {
		t.Errorf("flash %q", m.flash)
	}
}
