package readme_test

import (
	"os"
	"strings"
	"testing"

	"github.com/jegork/skillet/internal/readme"
	"github.com/jegork/skillet/internal/skill"
	"github.com/jegork/skillet/internal/testhome"
)

func scan(t *testing.T, h *testhome.Home) []skill.Skill {
	t.Helper()
	paths := skill.Paths{Home: h.Dir}
	lock, err := skill.ReadLock(paths.LockFile())
	if err != nil {
		t.Fatal(err)
	}
	skills, err := skill.Scan(paths.SkillsDir(), lock)
	if err != nil {
		t.Fatal(err)
	}
	return skills
}

func TestParseRows(t *testing.T) {
	h := testhome.New(t)
	p := h.Readme("| `a` | own | A |", "| `v` | vendored (acme/skills) | V |")
	idx, err := readme.Parse(p)
	if err != nil {
		t.Fatal(err)
	}
	if idx.Origins["a"] != "own" || idx.Origins["v"] != "vendored (acme/skills)" || len(idx.Origins) != 2 {
		t.Errorf("origins %v", idx.Origins)
	}
	if _, err := readme.Parse(p + ".missing"); err == nil {
		t.Error("missing file must error")
	}
}

func TestRegenerateKeepsSectionsAndAppendsUnknown(t *testing.T) {
	h := testhome.New(t)
	h.Skill("a", "A does things. More detail here.")
	h.Skill("b", strings.Repeat("b", 200))
	h.Skill("newbie", "Fresh skill")
	h.Skill("v", "V")
	h.Lock(map[string]string{"v": "acme/skills", "b": "acme/other"})
	p := h.Readme()
	fixture := strings.ReplaceAll(`# Skills index

Intro prose stays.

## Review

| Skill | Origin | What it does |
|---|---|---|
| 'a' | own | old text |
| 'gone' | own | removed skill |

## Tools

| Skill | Origin | What it does |
|---|---|---|
| 'v' | own | V |
| 'b' | own | B |
`, "'", "`")
	if err := os.WriteFile(p, []byte(fixture), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := readme.Regenerate(p, scan(t, h))
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 1 || res.Removed != 1 {
		t.Errorf("added %d removed %d", res.Added, res.Removed)
	}
	out, _ := os.ReadFile(p)
	got := string(out)
	for _, want := range []string{
		"Intro prose stays.",
		"## Review\n\n| Skill | Origin | What it does |\n|---|---|---|\n| `a` | own | A does things. |\n",
		"## Tools\n\n| Skill | Origin | What it does |\n|---|---|---|\n| `v` | vendored (acme/skills) | V |\n| `b` | vendored (acme/other) | " + strings.Repeat("b", 157) + "... |\n",
		"## Uncategorized\n\n| Skill | Origin | What it does |\n|---|---|---|\n| `newbie` | own | Fresh skill |\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "gone") {
		t.Error("removed skill still listed")
	}

	// second run is a no-op and keeps newbie where it is
	res, err = readme.Regenerate(p, scan(t, h))
	if err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(p)
	if res.Added != 0 || res.Removed != 0 || string(again) != got {
		t.Errorf("regenerate is not idempotent: %+v", res)
	}
}

func TestRegenerateCreatesMissingReadme(t *testing.T) {
	h := testhome.New(t)
	h.Skill("a", "A")
	p := skill.Paths{Home: h.Dir}.Readme()
	res, err := readme.Regenerate(p, scan(t, h))
	if err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(p)
	if res.Added != 1 || !strings.HasPrefix(string(out), "# Skills index\n") || !strings.Contains(string(out), "## Uncategorized\n") || !strings.Contains(string(out), "| `a` | own | A |") {
		t.Errorf("res %+v\n%s", res, out)
	}
}

func TestRename(t *testing.T) {
	h := testhome.New(t)
	p := h.Readme("| `old` | own | O |", "| `other` | own | X |")
	if err := readme.Rename(p, "old", "new"); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(p)
	if !strings.Contains(string(out), "| `new` | own | O |") || strings.Contains(string(out), "`old`") {
		t.Errorf("rename failed:\n%s", out)
	}
	if err := readme.Rename(p+".missing", "a", "b"); err != nil {
		t.Errorf("missing readme should be a no-op, got %v", err)
	}
}
