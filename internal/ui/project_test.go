package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/jegork/skillet/internal/inventory"
	"github.com/jegork/skillet/internal/testhome"
)

// newProjectModel builds a home with two projects (mirror + bare) and a
// global skill, plus the project config.
func newProjectModel(t *testing.T) (Model, *testhome.Home) {
	t.Helper()
	h := testhome.New(t)
	h.Config("projects:\n  roots: [" + h.Dir + "/src]\n")
	h.Skill("global-skill", "the global one")
	h.Readme("| `global-skill` | own | the global one |")

	mirror := h.ProjectDir("src/mirror")
	h.ProjectSkill(mirror, "proj-a", "project one")
	h.ProjectSkill(mirror, "shared", "own project skill")
	h.ProjectSkill(mirror, "twin", "project copy of a global name")
	h.ProjectSkill(mirror, "shared", "project own skill")
	h.Skill("twin", "global original")
	h.ProjectLock(mirror, map[string]string{"proj-a": "acme/repo"})
	h.ProjectStub(mirror, ".claude/skills", "proj-a", "../../.agents/skills/proj-a")
	h.ProjectStub(mirror, ".claude/skills", "shared", "../../.agents/skills/shared")

	bare := h.ProjectDir("src/bare")
	h.ProjectBareSkill(bare, "proj-b", "project two")

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
	return next.(Model), h
}

func TestProjectGroupsInTree(t *testing.T) {
	m, _ := newProjectModel(t)
	m = press(m, "t")
	v := view(m)
	if !strings.Contains(v, "proj mirror") || !strings.Contains(v, "proj bare") {
		t.Errorf("tree missing project groups:\n%s", v)
	}
}

func TestFlatListScopeMarker(t *testing.T) {
	m, _ := newProjectModel(t)
	v := view(m)
	if !strings.Contains(v, "@mirror") {
		t.Errorf("flat list missing project marker:\n%s", v)
	}
}

func TestProjectPreviewShowsScope(t *testing.T) {
	m, _ := newProjectModel(t)
	m = press(m, "/", "p", "r", "o", "j", "-", "a", "enter")
	if _, ok := m.list.SelectedItem().(item); !ok {
		t.Fatalf("expected a skill selected, got %T", m.list.SelectedItem())
	}
	v := view(m)
	if !strings.Contains(v, "project") || !strings.Contains(v, "mirror") {
		t.Errorf("preview missing project scope:\n%s", v)
	}
}

func TestToggleProjectConsumers(t *testing.T) {
	m, h := newProjectModel(t)
	mirror := filepath.Join(h.Dir, "src", "mirror")
	stub := filepath.Join(mirror, ".claude", "skills", "proj-a")

	m = press(m, "/", "p", "r", "o", "j", "-", "a", "enter")
	if _, ok := m.list.SelectedItem().(item); !ok {
		t.Fatalf("expected a skill selected, got %T", m.list.SelectedItem())
	}

	// enabled by the existing stub: c disables
	m = press(m, "c")
	if _, err := os.Lstat(stub); !os.IsNotExist(err) {
		t.Fatalf("stub still present after disable: %v", err)
	}
	// and re-enables
	m = press(m, "c")
	if _, err := os.Lstat(stub); err != nil {
		t.Errorf("stub not recreated: %v", err)
	}
	target, err := os.Readlink(stub)
	if err != nil {
		t.Fatal(err)
	}
	if want := "../../.agents/skills/proj-a"; target != want {
		t.Errorf("stub target %q, want %q", target, want)
	}
}

func TestToggleNativeRefused(t *testing.T) {
	m, _ := newProjectModel(t)
	m = press(m, "/", "p", "r", "o", "j", "-", "b", "enter")
	m = press(m, "c")
	if !strings.Contains(m.flash, "native") {
		t.Errorf("flash %q, want a native-dir refusal", m.flash)
	}
}

func TestProjectSkillFindingsAttachToRow(t *testing.T) {
	m, _ := newProjectModel(t)
	// the project's twin shadows the global skill of the same name; the
	// global row must not inherit the finding
	var globalFindings, projectFindings int
	for _, li := range m.list.Items() {
		it, ok := li.(item)
		if !ok || it.skill.Name != "twin" {
			continue
		}
		if it.skill.Scope == "" {
			globalFindings = len(it.findings)
		} else {
			projectFindings = len(it.findings)
		}
	}
	if globalFindings != 0 || projectFindings == 0 {
		t.Errorf("global %d findings, project %d findings", globalFindings, projectFindings)
	}
}

func TestRenameProjectSkill(t *testing.T) {
	m, h := newProjectModel(t)
	mirror := filepath.Join(h.Dir, "src", "mirror")
	oldStub := filepath.Join(mirror, ".claude", "skills", "shared")
	newStub := filepath.Join(mirror, ".claude", "skills", "shared2")

	// flat order: global-skill, twin, proj-b, proj-a, shared, twin(project)
	m = press(m, "j", "j", "j", "j")
	if it := m.list.SelectedItem().(item); it.skill.Name != "shared" {
		t.Fatalf("selected %s, want shared", it.skill.Name)
	}
	m = press(m, "n")
	if m.mode != modeRename {
		t.Fatalf("rename mode not entered")
	}
	m = press(m, "2", "enter") // input is prefilled with "shared"

	if _, err := os.Stat(filepath.Join(mirror, ".agents", "skills", "shared2")); err != nil {
		t.Errorf("dir not renamed: %v", err)
	}
	if _, err := os.Lstat(oldStub); !os.IsNotExist(err) {
		t.Errorf("old stub still there: %v", err)
	}
	if target, err := os.Readlink(newStub); err != nil || target != "../../.agents/skills/shared2" {
		t.Errorf("new stub %q %v", target, err)
	}
}
