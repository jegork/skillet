package move_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jegork/skillet/internal/inventory"
	"github.com/jegork/skillet/internal/move"
	"github.com/jegork/skillet/internal/skill"
	"github.com/jegork/skillet/internal/testhome"
)

func load(t *testing.T, h *testhome.Home) (move.Input, inventory.Inventory) {
	t.Helper()
	inv, err := inventory.Load(h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	return move.Input{Home: inv.Paths, Skills: inv.Skills, Projects: inv.Projects}, inv
}

func projectSkill(inv inventory.Inventory, root, name string) skill.Skill {
	for _, p := range inv.Projects {
		if p.Root != root {
			continue
		}
		for _, s := range p.Skills {
			if s.Name == name {
				return s
			}
		}
	}
	return skill.Skill{}
}

func globalSkill(inv inventory.Inventory, name string) (skill.Skill, bool) {
	for _, s := range inv.Skills {
		if s.Name == name {
			return s, true
		}
	}
	return skill.Skill{}, false
}

func TestMoveGlobalToProjectOwn(t *testing.T) {
	h := testhome.New(t)
	h.Config("projects:\n  roots: [" + h.Dir + "/src]\n")
	h.Skill("mine", "does mine things")
	h.Skill("stay", "stays home")
	h.Stub(".claude/skills", "mine", "../../.agents/skills/mine")
	h.OmpIgnore("mine", "omp-*")
	h.Readme("| `mine` | own | does mine things |", "| `stay` | own | stays home |")
	root := h.ProjectDir("src/mirror")
	h.ProjectSkill(root, "proj", "project one")
	h.ProjectLock(root, map[string]string{"proj": "acme/repo"})
	h.ProjectStub(root, ".claude/skills", "proj", "../../.agents/skills/proj")

	in, inv := load(t, h)
	s, ok := globalSkill(inv, "mine")
	if !ok {
		t.Fatal("skill not scanned")
	}
	if err := move.Move(in, s, root); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(h.SkillsDir(), "mine")); !os.IsNotExist(err) {
		t.Error("home dir still exists")
	}
	moved, err := os.ReadFile(filepath.Join(root, ".agents", "skills", "mine", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(moved), "---\nname: mine\n") {
		t.Errorf("moved SKILL.md:\n%s", moved)
	}
	// the home claude stub is forgotten, the project one is created
	if _, err := os.Lstat(filepath.Join(h.Dir, ".claude", "skills", "mine")); !os.IsNotExist(err) {
		t.Error("home claude stub still exists")
	}
	link, err := os.Readlink(filepath.Join(root, ".claude", "skills", "mine"))
	if err != nil {
		t.Fatal(err)
	}
	if link != "../../.agents/skills/mine" {
		t.Errorf("project claude stub -> %q", link)
	}
	// no home codex stub, so no project codex stub either
	if _, err := os.Lstat(filepath.Join(root, ".codex", "skills", "mine")); !os.IsNotExist(err) {
		t.Error("project codex stub created without a source twin")
	}
	// the omp ignore entry for the name is dropped with the skill
	omp, _ := os.ReadFile(filepath.Join(h.Dir, ".omp", "agent", "config.yml"))
	if strings.Contains(string(omp), "- mine") || !strings.Contains(string(omp), "- omp-*") {
		t.Errorf("omp config:\n%s", omp)
	}
	// the README row is dropped, other rows kept
	rm, _ := os.ReadFile(inv.Paths.Readme())
	if strings.Contains(string(rm), "`mine`") || !strings.Contains(string(rm), "| `stay` | own | stays home |") {
		t.Errorf("readme:\n%s", rm)
	}
	// an own skill gains no lock entry: the project lock still lists only proj
	pl, err := skill.ReadProjectLock(filepath.Join(root, "skills-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := pl.Skills["mine"]; ok {
		t.Error("own skill gained a project lock entry")
	}

	after, err := inventory.Load(h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if after.Reports["claude"].Enabled["mine"] || len(after.Reports["claude"].Stubs) != 0 {
		t.Errorf("home claude after move: %+v", after.Reports["claude"])
	}
	for _, p := range after.Projects {
		if p.Root != root {
			continue
		}
		if !p.Reports["claude"].Enabled["mine"] || p.Reports["codex"].Enabled["mine"] {
			t.Errorf("project reports: %+v", p.Reports["claude"])
		}
	}
	for _, f := range after.Findings {
		if f.Skill == "mine" || f.Skill == "proj" {
			t.Errorf("finding after move: %+v", f)
		}
	}
}

func TestMoveProjectToGlobalOwn(t *testing.T) {
	h := testhome.New(t)
	h.Config("projects:\n  roots: [" + h.Dir + "/src]\n")
	root := h.ProjectDir("src/mirror")
	h.ProjectSkill(root, "mine", "does mine things")
	h.ProjectStub(root, ".claude/skills", "mine", "../../.agents/skills/mine")
	h.ProjectStub(root, ".codex/skills", "mine", "../../../.codex/skills/x")

	in, inv := load(t, h)
	s := projectSkill(inv, root, "mine")
	if err := move.Move(in, s, ""); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(root, ".agents", "skills", "mine")); !os.IsNotExist(err) {
		t.Error("project dir still exists")
	}
	for _, dir := range []string{".claude", ".codex"} {
		if _, err := os.Lstat(filepath.Join(root, dir, "skills", "mine")); !os.IsNotExist(err) {
			t.Errorf("project %s stub still exists", dir)
		}
	}
	link, err := os.Readlink(filepath.Join(h.Dir, ".claude", "skills", "mine"))
	if err != nil {
		t.Fatal(err)
	}
	if link != "../../.agents/skills/mine" {
		t.Errorf("home claude stub -> %q", link)
	}
	rm, _ := os.ReadFile(inv.Paths.Readme())
	if !strings.Contains(string(rm), "| `mine` | own | does mine things |") {
		t.Errorf("readme row not added:\n%s", rm)
	}
}

func TestMoveVendoredToProjectCarriesLockEntry(t *testing.T) {
	h := testhome.New(t)
	h.Config("projects:\n  roots: [" + h.Dir + "/src]\n")
	h.Skill("vend", "vendored one")
	h.Skill("other", "other vendored")
	h.Lock(map[string]string{"vend": "acme/skills", "other": "acme/other"})
	h.Stub(".claude/skills", "vend", "../../.agents/skills/vend")
	root := h.ProjectDir("src/mirror")
	h.ProjectSkill(root, "proj", "project one")
	h.ProjectLock(root, map[string]string{"proj": "acme/repo"})

	in, inv := load(t, h)
	s, ok := globalSkill(inv, "vend")
	if !ok {
		t.Fatal("skill not scanned")
	}
	if err := move.Move(in, s, root); err != nil {
		t.Fatal(err)
	}

	homeLock, err := skill.ReadLock(inv.Paths.LockFile())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := homeLock.Skills["vend"]; ok {
		t.Error("home lock kept the entry")
	}
	if homeLock.Skills["other"].Source != "acme/other" {
		t.Errorf("home lock lost its other entry: %+v", homeLock.Skills)
	}

	pl, err := skill.ReadProjectLock(filepath.Join(root, "skills-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	if pl.Version != 1 || len(pl.Skills) != 2 {
		t.Errorf("project lock: version %d skills %v", pl.Version, pl.Skills)
	}
	entry, ok := pl.Skills["vend"]
	if !ok {
		t.Fatal("project lock has no vend entry")
	}
	if entry.Source != "acme/skills" || entry.SourceType != "github" || entry.SkillPath != "skills/vend/SKILL.md" {
		t.Errorf("entry not carried over: %+v", entry)
	}
	want, err := skill.ContentHash(filepath.Join(root, ".agents", "skills", "vend"))
	if err != nil {
		t.Fatal(err)
	}
	if entry.ComputedHash != want {
		t.Errorf("computedHash %q, want %q", entry.ComputedHash, want)
	}
	if entry.SkillFolderHash != "" {
		t.Errorf("project entry carries a global hash: %q", entry.SkillFolderHash)
	}
	if _, ok := pl.Skills["proj"]; !ok {
		t.Error("project lock lost its other entry")
	}
}

func TestMoveVendoredToGlobalCarriesLockEntry(t *testing.T) {
	h := testhome.New(t)
	h.Config("projects:\n  roots: [" + h.Dir + "/src]\n")
	root := h.ProjectDir("src/mirror")
	h.ProjectSkill(root, "vend", "vendored one")
	h.ProjectSkill(root, "stay", "stays vendored")
	h.ProjectLock(root, map[string]string{"vend": "acme/skills", "stay": "acme/other"})

	in, inv := load(t, h)
	s := projectSkill(inv, root, "vend")
	if err := move.Move(in, s, ""); err != nil {
		t.Fatal(err)
	}

	homeLock, err := skill.ReadLock(inv.Paths.LockFile())
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := homeLock.Skills["vend"]
	if !ok {
		t.Fatal("home lock has no vend entry")
	}
	if entry.Source != "acme/skills" || entry.SourceType != "github" {
		t.Errorf("entry not carried over: %+v", entry)
	}
	want, err := skill.TreeHash(filepath.Join(h.SkillsDir(), "vend"))
	if err != nil {
		t.Fatal(err)
	}
	if entry.SkillFolderHash != want {
		t.Errorf("skillFolderHash %q, want %q", entry.SkillFolderHash, want)
	}
	if entry.ComputedHash != "" {
		t.Errorf("global entry carries a project hash: %q", entry.ComputedHash)
	}

	pl, err := skill.ReadProjectLock(filepath.Join(root, "skills-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := pl.Skills["vend"]; ok {
		t.Error("project lock kept the entry")
	}
	if pl.Skills["stay"].Source != "acme/other" {
		t.Errorf("project lock lost its other entry: %v", pl.Skills)
	}

	after, err := inventory.Load(h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range after.Findings {
		if f.Skill == "vend" || f.Skill == "stay" {
			t.Errorf("finding after move: %+v", f)
		}
	}
}

func TestMoveIntoBareProject(t *testing.T) {
	h := testhome.New(t)
	h.Config("projects:\n  roots: [" + h.Dir + "/src]\n")
	h.Skill("mine", "does mine things")
	h.Stub(".claude/skills", "mine", "../../.agents/skills/mine")
	h.Stub(".codex/skills", "mine", "../../.agents/skills/mine")
	root := h.ProjectDir("src/bare")
	h.ProjectBareSkill(root, "proj", "project one")

	in, inv := load(t, h)
	s, ok := globalSkill(inv, "mine")
	if !ok {
		t.Fatal("skill not scanned")
	}
	if err := move.Move(in, s, root); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(root, ".claude", "skills", "mine", "SKILL.md")); err != nil {
		t.Errorf("skill not in the bare claude dir: %v", err)
	}
	// codex saw it at home, so it gets a project stub pointing at the bare dir
	link, err := os.Readlink(filepath.Join(root, ".codex", "skills", "mine"))
	if err != nil {
		t.Fatal(err)
	}
	if link != "../../.claude/skills/mine" {
		t.Errorf("project codex stub -> %q", link)
	}
}

func TestMoveRefusals(t *testing.T) {
	h := testhome.New(t)
	h.Config("projects:\n  roots: [" + h.Dir + "/src]\n")
	h.Skill("mine", "home one")
	h.Skill("twin", "home twin")
	h.Readme("| `mine` | own | home one |", "| `twin` | own | home twin |")
	a := h.ProjectDir("src/a")
	b := h.ProjectDir("src/b")
	h.ProjectSkill(a, "twin", "project twin")
	h.ProjectSkill(a, "clash", "already in b")
	h.ProjectSkill(b, "clash", "already in b")

	in, inv := load(t, h)
	before, err := inventory.Load(h.Dir)
	if err != nil {
		t.Fatal(err)
	}

	clash := projectSkill(inv, a, "clash")
	err = move.Move(in, clash, b)
	if err == nil || !strings.Contains(err.Error(), "clash") {
		t.Errorf("target clash not refused: %v", err)
	}
	twin := projectSkill(inv, a, "twin")
	err = move.Move(in, twin, b)
	if err == nil || !strings.Contains(err.Error(), "shadow") || !strings.Contains(err.Error(), "twin") {
		t.Errorf("shadow not refused: %v", err)
	}
	err = move.Move(in, twin, a)
	if err == nil {
		t.Error("same scope not refused")
	}
	err = move.Move(in, twin, filepath.Join(h.Dir, "src", "unknown"))
	if err == nil {
		t.Error("unknown project not refused")
	}

	after, err := inventory.Load(h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Skills) != len(before.Skills) || len(after.Projects) != len(before.Projects) {
		t.Error("a refused move changed the inventory")
	}
	for _, p := range after.Projects {
		for _, q := range before.Projects {
			if p.Root == q.Root && (len(p.Skills) != len(q.Skills) || len(p.Reports["claude"].Stubs) != len(q.Reports["claude"].Stubs)) {
				t.Errorf("project %s changed: %d -> %d skills", filepath.Base(p.Root), len(q.Skills), len(p.Skills))
			}
		}
	}
	rm, _ := os.ReadFile(inv.Paths.Readme())
	if !strings.Contains(string(rm), "| `mine` | own | home one |") {
		t.Errorf("readme changed:\n%s", rm)
	}
}

func TestMoveOwnIntoProjectKeepsBothConsumers(t *testing.T) {
	h := testhome.New(t)
	h.Config("projects:\n  roots: [" + h.Dir + "/src]\n")
	h.Skill("mine", "does mine things")
	h.Stub(".claude/skills", "mine", "../../.agents/skills/mine")
	h.Stub(".codex/skills", "mine", "../../.agents/skills/mine")
	root := h.ProjectDir("src/mirror")
	h.ProjectSkill(root, "proj", "project one")

	in, inv := load(t, h)
	s, ok := globalSkill(inv, "mine")
	if !ok {
		t.Fatal("skill not scanned")
	}
	if err := move.Move(in, s, root); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{".claude", ".codex"} {
		link, err := os.Readlink(filepath.Join(root, dir, "skills", "mine"))
		if err != nil {
			t.Fatal(err)
		}
		if link != "../../.agents/skills/mine" && link != "../../../.agents/skills/mine" {
			t.Errorf("project %s stub -> %q", dir, link)
		}
	}
	after, err := inventory.Load(h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range after.Projects {
		if p.Root != root {
			continue
		}
		if !p.Reports["claude"].Enabled["mine"] || !p.Reports["codex"].Enabled["mine"] {
			t.Errorf("project stubs after move: %+v / %+v", p.Reports["claude"], p.Reports["codex"])
		}
	}
}
