package remove_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jegork/skillet/internal/inventory"
	"github.com/jegork/skillet/internal/remove"
	"github.com/jegork/skillet/internal/skill"
	"github.com/jegork/skillet/internal/testhome"
)

func load(t *testing.T, h *testhome.Home) remove.Input {
	t.Helper()
	inv, err := inventory.Load(h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	return remove.Input{Home: inv.Paths, Projects: inv.Projects}
}

func globalSkill(inv inventory.Inventory, name string) (skill.Skill, bool) {
	for _, s := range inv.Skills {
		if s.Scope == "" && s.Name == name {
			return s, true
		}
	}
	return skill.Skill{}, false
}

func projectSkill(inv inventory.Inventory, root, name string) (skill.Skill, bool) {
	for _, p := range inv.Projects {
		if p.Root != root {
			continue
		}
		for _, s := range p.Skills {
			if s.Name == name {
				return s, true
			}
		}
	}
	return skill.Skill{}, false
}

func assertGone(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("%s still there: %v", path, err)
	}
}

func assertOmpConfig(t *testing.T, h *testhome.Home, wantAbsent ...string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(h.Dir, ".omp", "agent", "config.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range wantAbsent {
		if strings.Contains(string(b), w) {
			t.Errorf("omp config still mentions %q:\n%s", w, b)
		}
	}
}

// fakeCLI mimics what pnpx skills remove does for a vendored global skill:
// it deletes the folder and drops the lock entry, leaving the rest of the
// lock file alone.
func fakeCLI(h *testhome.Home) func(home, name string) error {
	return func(home, name string) error {
		if err := os.RemoveAll(filepath.Join(h.SkillsDir(), name)); err != nil {
			return err
		}
		lock, err := readLock(filepath.Join(h.Dir, ".agents", ".skill-lock.json"))
		if err != nil {
			return err
		}
		delete(lock.Skills, name)
		return writeLock(filepath.Join(h.Dir, ".agents", ".skill-lock.json"), lock)
	}
}

func TestRemoveOwnGlobal(t *testing.T) {
	h := testhome.New(t)
	h.Skill("alpha", "does alpha things")
	h.Skill("beta", "does beta things")
	h.Stub(".claude/skills", "alpha", "../../.agents/skills/alpha")
	h.OmpIgnore("alpha")
	h.Readme("| `alpha` | own | does alpha things |", "| `beta` | own | does beta things |")
	s, ok := globalSkill(mustLoad(t, h), "alpha")
	if !ok {
		t.Fatal("skill not scanned")
	}
	if err := remove.Remove(load(t, h), s); err != nil {
		t.Fatal(err)
	}
	assertGone(t, filepath.Join(h.SkillsDir(), "alpha"))
	assertGone(t, filepath.Join(h.Dir, ".claude", "skills", "alpha"))
	assertOmpConfig(t, h, "alpha")
	if rows := readmeRows(t, h); strings.Contains(rows, "`alpha`") {
		t.Errorf("README row still there:\n%s", rows)
	}
	if _, err := os.Stat(filepath.Join(h.SkillsDir(), "beta")); err != nil {
		t.Errorf("other skill touched: %v", err)
	}
}

func TestRemoveVendoredGlobal(t *testing.T) {
	h := testhome.New(t)
	h.Skill("vend", "vendored one")
	h.Skill("other", "another vendored one")
	h.Lock(map[string]string{"vend": "acme/skills", "other": "acme/skills"})
	h.Stub(".claude/skills", "vend", "../../.agents/skills/vend")
	h.Stub(".codex/skills", "vend", "../../.agents/skills/vend")
	h.OmpIgnore("vend")
	h.Readme("| `vend` | vendored (acme/skills) | vendored one |")
	s, ok := globalSkill(mustLoad(t, h), "vend")
	if !ok {
		t.Fatal("skill not scanned")
	}
	in := load(t, h)
	in.RemoveVendored = fakeCLI(h)
	if err := remove.Remove(in, s); err != nil {
		t.Fatal(err)
	}
	assertGone(t, filepath.Join(h.SkillsDir(), "vend"))
	lock, err := readLock(filepath.Join(h.Dir, ".agents", ".skill-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := lock.Skills["vend"]; ok {
		t.Error("lock entry still there")
	}
	if _, ok := lock.Skills["other"]; !ok {
		t.Error("unrelated lock entry lost")
	}
	assertGone(t, filepath.Join(h.Dir, ".claude", "skills", "vend"))
	assertGone(t, filepath.Join(h.Dir, ".codex", "skills", "vend"))
	assertOmpConfig(t, h, "vend")
	if rows := readmeRows(t, h); strings.Contains(rows, "`vend`") {
		t.Errorf("README row still there:\n%s", rows)
	}
}

func TestRemoveProjectOwn(t *testing.T) {
	h := testhome.New(t)
	h.Config("projects:\n  roots: [" + h.Dir + "/src]\n")
	root := h.ProjectDir("src/proj")
	h.ProjectSkill(root, "alpha", "does alpha things")
	h.ProjectSkill(root, "beta", "does beta things")
	h.ProjectLock(root, map[string]string{"alpha": "own", "beta": "own"})
	h.ProjectStub(root, ".claude/skills", "alpha", "../../.agents/skills/alpha")
	s, ok := projectSkill(mustLoad(t, h), root, "alpha")
	if !ok {
		t.Fatal("project skill not scanned")
	}
	if err := remove.Remove(load(t, h), s); err != nil {
		t.Fatal(err)
	}
	assertGone(t, filepath.Join(root, ".agents", "skills", "alpha"))
	lock, err := readProjectLock(filepath.Join(root, "skills-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := lock.Skills["alpha"]; ok {
		t.Error("project lock entry still there")
	}
	if _, ok := lock.Skills["beta"]; !ok {
		t.Error("unrelated project lock entry lost")
	}
	assertGone(t, filepath.Join(root, ".claude", "skills", "alpha"))
}

func TestRemoveProjectVendored(t *testing.T) {
	h := testhome.New(t)
	h.Config("projects:\n  roots: [" + h.Dir + "/src]\n")
	root := h.ProjectDir("src/proj")
	h.ProjectSkill(root, "vend", "vendored one")
	h.ProjectLock(root, map[string]string{"vend": "acme/skills"})
	s, ok := projectSkill(mustLoad(t, h), root, "vend")
	if !ok {
		t.Fatal("project skill not scanned")
	}
	if err := remove.Remove(load(t, h), s); err != nil {
		t.Fatal(err)
	}
	assertGone(t, filepath.Join(root, ".agents", "skills", "vend"))
	lock, err := readProjectLock(filepath.Join(root, "skills-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := lock.Skills["vend"]; ok {
		t.Error("project lock entry still there")
	}
}

func TestRemoveProjectWithoutLockFile(t *testing.T) {
	h := testhome.New(t)
	h.Config("projects:\n  roots: [" + h.Dir + "/src]\n")
	root := h.ProjectDir("src/proj")
	h.ProjectSkill(root, "alpha", "does alpha things")
	s, ok := projectSkill(mustLoad(t, h), root, "alpha")
	if !ok {
		t.Fatal("project skill not scanned")
	}
	if err := remove.Remove(load(t, h), s); err != nil {
		t.Fatal(err)
	}
	assertGone(t, filepath.Join(root, ".agents", "skills", "alpha"))
}

func TestRemoveRefusesSymlink(t *testing.T) {
	h := testhome.New(t)
	h.Skill("alpha", "does alpha things")
	h.Skill("beta", "does beta things")
	real := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.Rename(filepath.Join(h.SkillsDir(), "alpha"), real); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(h.SkillsDir(), "alpha")); err != nil {
		t.Fatal(err)
	}
	h.Stub(".claude/skills", "alpha", "../../.agents/skills/alpha")
	s := skill.Skill{Name: "alpha", Dir: filepath.Join(h.SkillsDir(), "alpha")}
	err := remove.Remove(load(t, h), s)
	if err == nil || !strings.Contains(err.Error(), "not a real directory") {
		t.Fatalf("want refusal, got %v", err)
	}
	if _, err := os.Lstat(real); err != nil {
		t.Errorf("the symlink target was deleted: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(h.Dir, ".claude", "skills", "alpha")); err != nil {
		t.Errorf("stub was touched on refusal: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.SkillsDir(), "beta")); err != nil {
		t.Errorf("other skill touched: %v", err)
	}
}

func TestRemoveRefusesPlainFile(t *testing.T) {
	h := testhome.New(t)
	p := filepath.Join(h.SkillsDir(), "alpha")
	if err := os.WriteFile(p, []byte("not a skill"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := remove.Remove(load(t, h), skill.Skill{Name: "alpha", Dir: p})
	if err == nil || !strings.Contains(err.Error(), "not a real directory") {
		t.Fatalf("want refusal, got %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("the file was deleted: %v", err)
	}
}

func TestCmdArgs(t *testing.T) {
	cmd := remove.Cmd("/tmp/home", "alpha")
	if got := strings.Join(cmd.Args, " "); got != "pnpx skills remove -s alpha -g -y" {
		t.Errorf("args %q", got)
	}
	if cmd.Dir != "/tmp/home" {
		t.Errorf("dir %q", cmd.Dir)
	}
	var home string
	for _, e := range cmd.Env {
		if strings.HasPrefix(e, "HOME=") {
			home = e
		}
	}
	if home != "HOME=/tmp/home" {
		t.Errorf("env %q", home)
	}
}

func mustLoad(t *testing.T, h *testhome.Home) inventory.Inventory {
	t.Helper()
	inv, err := inventory.Load(h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	return inv
}

func readmeRows(t *testing.T, h *testhome.Home) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(h.SkillsDir(), "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func readLock(path string) (skill.Lock, error) {
	var l skill.Lock
	b, err := os.ReadFile(path)
	if err != nil {
		return l, err
	}
	return l, json.Unmarshal(b, &l)
}

func readProjectLock(path string) (skill.ProjectLock, error) {
	var l skill.ProjectLock
	b, err := os.ReadFile(path)
	if err != nil {
		return l, err
	}
	return l, json.Unmarshal(b, &l)
}

func writeLock(path string, lock skill.Lock) error {
	b, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
