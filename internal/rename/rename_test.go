package rename_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jegork/skillet/internal/consumer"
	"github.com/jegork/skillet/internal/inventory"
	"github.com/jegork/skillet/internal/rename"
	"github.com/jegork/skillet/internal/testhome"
)

func TestRenameMovesEverything(t *testing.T) {
	h := testhome.New(t)
	h.RawSkill("old", "---\nname: old\ndescription: O\n---\nsee the `old` skill and the `other` skill\n")
	h.RawSkill("other", "---\nname: other\ndescription: X\n---\nuse the `old` Skill here\n")
	h.File("other", "docs/more.md", "`old` skill again\n")
	h.RawSkill("vend", "---\nname: vend\ndescription: V\n---\nthe `old` skill is upstream\n")
	h.Lock(map[string]string{"vend": "acme/skills"})
	h.Stub(".claude/skills", "old", "../../.agents/skills/old")
	h.Stub(".claude/skills", "other", "../../.agents/skills/other")
	h.OmpIgnore("old", "omp-*")
	h.Readme("| `old` | own | O |", "| `other` | own | X |", "| `vend` | vendored (acme/skills) | V |")

	inv, err := inventory.Load(h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := rename.Rename(inv.Paths, inv.Skills, inventory.Consumers(h.Dir), "old", "fresh")
	if err != nil {
		t.Fatal(err)
	}
	if rep.RewrittenFiles != 3 || len(rep.VendoredRefs) != 1 || rep.VendoredRefs[0] != "vend" {
		t.Errorf("report %+v", rep)
	}

	if _, err := os.Stat(filepath.Join(h.SkillsDir(), "old")); !os.IsNotExist(err) {
		t.Error("old dir still exists")
	}
	read := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(h.SkillsDir(), rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if got := read("fresh/SKILL.md"); !strings.HasPrefix(got, "---\nname: fresh\n") || !strings.Contains(got, "see the `fresh` skill and the `other` skill") {
		t.Errorf("renamed SKILL.md:\n%s", got)
	}
	if got := read("other/SKILL.md"); !strings.Contains(got, "use the `fresh` Skill here") {
		t.Errorf("xref not rewritten:\n%s", got)
	}
	if got := read("other/docs/more.md"); !strings.Contains(got, "`fresh` skill again") {
		t.Errorf("nested xref not rewritten:\n%s", got)
	}
	if got := read("vend/SKILL.md"); !strings.Contains(got, "the `old` skill is upstream") {
		t.Errorf("vendored file was edited:\n%s", got)
	}

	after, err := inventory.Load(h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if !after.Reports["claude"].Enabled["fresh"] || after.Reports["claude"].Enabled["old"] || len(after.Reports["claude"].Stubs) != 2 {
		t.Errorf("claude stubs: %+v", after.Reports["claude"])
	}
	if after.Reports["omp"].Enabled["fresh"] || !after.Reports["omp"].Enabled["other"] {
		t.Errorf("omp visibility: %v", after.Reports["omp"].Enabled)
	}
	cfg, _ := os.ReadFile(filepath.Join(h.Dir, ".omp", "agent", "config.yml"))
	if strings.Contains(string(cfg), "- old") || !strings.Contains(string(cfg), "- fresh") || !strings.Contains(string(cfg), "- omp-*") {
		t.Errorf("omp config:\n%s", cfg)
	}
	rm, _ := os.ReadFile(inv.Paths.Readme())
	if !strings.Contains(string(rm), "| `fresh` | own | O |") || strings.Contains(string(rm), "`old`") {
		t.Errorf("readme:\n%s", rm)
	}
	for _, f := range after.Findings {
		if f.Check == "stub" || f.Check == "readme" || (f.Check == "xref" && f.Skill != "vend") {
			t.Errorf("unexpected finding after rename: %+v", f)
		}
	}
}

func TestRenameRefusals(t *testing.T) {
	h := testhome.New(t)
	h.Skill("own", "O")
	h.Skill("taken", "T")
	h.Skill("vend", "V")
	h.Lock(map[string]string{"vend": "acme/skills"})
	h.Readme()
	inv, _ := inventory.Load(h.Dir)
	var cs []consumer.Consumer
	cases := map[string][2]string{
		"vendored":  {"vend", "x"},
		"missing":   {"nope", "x"},
		"taken":     {"own", "taken"},
		"same":      {"own", "own"},
		"uppercase": {"own", "Bad"},
		"space":     {"own", "a b"},
	}
	for label, c := range cases {
		if _, err := rename.Rename(inv.Paths, inv.Skills, cs, c[0], c[1]); err == nil {
			t.Errorf("%s: expected error", label)
		}
	}
	if _, err := os.Stat(filepath.Join(h.SkillsDir(), "own")); err != nil {
		t.Error("refused rename must not touch the dir")
	}
}
