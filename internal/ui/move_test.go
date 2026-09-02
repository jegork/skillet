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

// newMoveModel builds a home with one global skill, one vendored skill and a
// mirror project with one skill.
func newMoveModel(t *testing.T) (Model, *testhome.Home) {
	t.Helper()
	h := testhome.New(t)
	h.Config("projects:\n  roots: [" + h.Dir + "/src]\n")
	h.Skill("home-skill", "lives at home")
	h.Skill("vend", "vendored one")
	h.Lock(map[string]string{"vend": "acme/skills"})
	h.Readme("| `home-skill` | own | lives at home |", "| `vend` | vendored (acme/skills) | vendored one |")
	root := h.ProjectDir("src/mirror")
	h.ProjectSkill(root, "proj-skill", "project one")

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

func TestMovePickerListsOtherScopes(t *testing.T) {
	m, h := newMoveModel(t)
	m = press(m, "m")
	if m.mode != modeMove {
		t.Fatalf("mode %v", m.mode)
	}
	line := m.flashLine()
	if !strings.Contains(line, "1 mirror") {
		t.Errorf("picker missing the project: %q", line)
	}
	if strings.Contains(line, "global") {
		t.Errorf("current scope must be excluded: %q", line)
	}
	m = press(m, "esc")
	if m.mode != modeList {
		t.Errorf("esc should cancel, mode %v", m.mode)
	}
	_ = h
}

func TestMoveGlobalSkillIntoProject(t *testing.T) {
	m, h := newMoveModel(t)
	m = press(m, "m", "1")
	if m.mode != modeList || !strings.Contains(m.flash, "moved home-skill") {
		t.Fatalf("mode %v flash %q", m.mode, m.flash)
	}
	if _, err := os.Stat(filepath.Join(h.Dir, "src", "mirror", ".agents", "skills", "home-skill")); err != nil {
		t.Errorf("skill not in the project: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.SkillsDir(), "home-skill")); !os.IsNotExist(err) {
		t.Error("home dir still exists")
	}
	rm, _ := os.ReadFile(filepath.Join(h.SkillsDir(), "README.md"))
	if strings.Contains(string(rm), "`home-skill`") {
		t.Errorf("readme row not dropped:\n%s", rm)
	}
}

func TestMoveProjectSkillToGlobal(t *testing.T) {
	m, h := newMoveModel(t)
	// proj-skill is the last row in the flat list
	m = press(m, "j", "j", "j", "m")
	if m.mode != modeMove {
		t.Fatalf("mode %v", m.mode)
	}
	if !strings.Contains(m.flashLine(), "1 global") || strings.Contains(m.flashLine(), "mirror") {
		t.Errorf("picker should offer only global: %q", m.flashLine())
	}
	m = press(m, "1")
	if !strings.Contains(m.flash, "moved proj-skill") {
		t.Fatalf("flash %q", m.flash)
	}
	if _, err := os.Stat(filepath.Join(h.SkillsDir(), "proj-skill")); err != nil {
		t.Errorf("skill not at home: %v", err)
	}
	rm, _ := os.ReadFile(filepath.Join(h.SkillsDir(), "README.md"))
	if !strings.Contains(string(rm), "| `proj-skill` | own | project one |") {
		t.Errorf("readme row not added:\n%s", rm)
	}
}

func TestMoveRefusalNamesClash(t *testing.T) {
	m, h := newMoveModel(t)
	// a project skill clashing with the home name
	root := filepath.Join(h.Dir, "src", "mirror")
	h.ProjectSkill(root, "home-skill", "clashing project copy")
	inv, err := inventory.Load(h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	next, _ := m.Update(inventoryMsg{inv: inv})
	m = next.(Model)
	// rows: home-skill (global), vend, home-skill (project twin), proj-skill
	m.list.Select(2)
	m = press(m, "m", "1")
	if !strings.Contains(m.flash, "already has a skill named") {
		t.Errorf("clash not refused: %q", m.flash)
	}
	if _, err := os.Stat(filepath.Join(root, ".agents", "skills", "home-skill")); err != nil {
		t.Error("refused move removed the source dir")
	}
}

func TestMoveVendoredIntoProjectKeepsLock(t *testing.T) {
	m, h := newMoveModel(t)
	m = press(m, "j", "m", "1")
	if !strings.Contains(m.flash, "moved vend") {
		t.Fatalf("flash %q", m.flash)
	}
	if _, err := os.Stat(filepath.Join(h.Dir, "src", "mirror", ".agents", "skills", "vend")); err != nil {
		t.Errorf("vend not in the project: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.Dir, "src", "mirror", "skills-lock.json")); err != nil {
		t.Errorf("lock entry not migrated: %v", err)
	}
}
