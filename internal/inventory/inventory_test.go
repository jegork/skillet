package inventory_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/jegork/skillet/internal/doctor"
	"github.com/jegork/skillet/internal/inventory"
	"github.com/jegork/skillet/internal/skill"
	"github.com/jegork/skillet/internal/testhome"
	"github.com/jegork/skillet/internal/upstream"
)

func TestLoadGlobalOnly(t *testing.T) {
	h := testhome.New(t)
	h.Skill("alpha", "does alpha")
	h.Lock(map[string]string{"alpha": "acme/repo"})
	h.Readme("| `alpha` | vendored (acme/repo) | does alpha |")

	inv, err := inventory.Load(h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Skills) != 1 || inv.Skills[0].Name != "alpha" {
		t.Fatalf("skills %v", inv.Skills)
	}
	if inv.Skills[0].Scope != "" {
		t.Errorf("global skill scope %q", inv.Skills[0].Scope)
	}
	if len(inv.Projects) != 0 {
		t.Errorf("projects %v", inv.Projects)
	}
}

func TestLoadWithProjects(t *testing.T) {
	h := testhome.New(t)
	h.Config("projects:\n  roots: [" + h.Dir + "/src]\n")
	h.Skill("alpha", "does alpha")
	h.Readme("| `alpha` | own | does alpha |")

	mirror := h.ProjectDir("src/mirror")
	h.ProjectSkill(mirror, "proj-a", "project one")
	h.ProjectSkill(mirror, "shared", "project copy")
	h.ProjectLock(mirror, map[string]string{"proj-a": "acme/repo"})
	h.ProjectStub(mirror, ".claude/skills", "proj-a", "../../.agents/skills/proj-a")

	bare := h.ProjectDir("src/bare")
	h.ProjectBareSkill(bare, "proj-b", "project two")

	inv, err := inventory.Load(h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Projects) != 2 {
		t.Fatalf("projects %v", inv.Projects)
	}

	byRoot := map[string]inventory.Project{}
	for _, p := range inv.Projects {
		byRoot[p.Root] = p
		if len(p.Skills) == 0 {
			t.Errorf("project %s has no skills", p.Root)
		}
		if p.SkillsDir == "" {
			t.Errorf("project %s has no skills dir", p.Root)
		}
	}

	m, ok := byRoot[mirror]
	if !ok {
		t.Fatal("mirror project missing")
	}
	if m.SkillsDir != filepath.Join(mirror, ".agents", "skills") {
		t.Errorf("mirror skills dir %q", m.SkillsDir)
	}
	var origin *skill.Origin
	for i := range m.Skills {
		if m.Skills[i].Name == "proj-a" {
			origin = &m.Skills[i].Origin
		}
	}
	if origin == nil {
		t.Fatal("proj-a missing from mirror project")
	}
	if !origin.Vendored || origin.Source != "acme/repo" {
		t.Errorf("proj-a origin %+v", *origin)
	}
	if !m.Reports["claude"].Enabled["proj-a"] {
		t.Errorf("mirror claude report %v", m.Reports)
	}

	b, ok := byRoot[bare]
	if !ok {
		t.Fatal("bare project missing")
	}
	if b.SkillsDir != filepath.Join(bare, ".claude", "skills") {
		t.Errorf("bare skills dir %q", b.SkillsDir)
	}
	if !b.Reports["claude"].Enabled["proj-b"] {
		t.Errorf("bare claude report %v", b.Reports)
	}

	// a shadow finding exists for the project skill named like a global one
	h.ProjectSkill(mirror, "alpha", "shadow copy")
	inv, err = inventory.Load(h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	var shadow bool
	for _, f := range inv.Findings {
		if f.Check == "shadow" && f.Skill == "alpha" {
			shadow = true
		}
	}
	if !shadow {
		t.Errorf("no shadow finding: %v", inv.Findings)
	}
}

func TestLoadConfiguredPath(t *testing.T) {
	h := testhome.New(t)
	p := h.ProjectDir("somewhere/else")
	h.ProjectBareSkill(p, "proj-b", "project two")
	h.Config("projects:\n  paths: [" + p + "]\n")

	inv, err := inventory.Load(h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Projects) != 1 {
		t.Fatalf("projects %v", inv.Projects)
	}
}

func TestLoadEvaluatesUpstreamCache(t *testing.T) {
	h := testhome.New(t)
	h.Skill("alpha", "does alpha")
	h.LockWithHashes(map[string]string{"alpha": "acme/repo"}, map[string]string{"alpha": "1111111111111111111111111111111111111111"})
	cache := `{"version":1,"repos":{"acme/repo":{"fetchedAt":"2026-09-01T12:00:00Z","trees":{"skills/alpha":"2222222222222222222222222222222222222222"}}}}`
	os.MkdirAll(filepath.Join(h.Dir, ".cache", "skillet"), 0o755)
	os.WriteFile(filepath.Join(h.Dir, ".cache", "skillet", "upstream.json"), []byte(cache), 0o644)

	inv, err := inventory.Load(h.Dir)
	if err != nil {
		t.Fatal(err)
	}

	if !slices.ContainsFunc(inv.Findings, func(f doctor.Finding) bool {
		return f.Check == "outdated" && f.Skill == "alpha"
	}) {
		t.Errorf("findings %v", inv.Findings)
	}
	info := inv.Upstream["alpha"]
	if info.State != upstream.Outdated || info.Upstream != "2222222222222222222222222222222222222222" {
		t.Errorf("upstream info %+v", info)
	}
}
