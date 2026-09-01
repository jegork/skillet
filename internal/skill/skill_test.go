package skill_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jegork/skillet/internal/skill"
	"github.com/jegork/skillet/internal/testhome"
)

func scan(t *testing.T, h *testhome.Home) map[string]skill.Skill {
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
	byName := map[string]skill.Skill{}
	for _, s := range skills {
		byName[s.Name] = s
	}
	return byName
}

func TestScanReadsFrontmatter(t *testing.T) {
	h := testhome.New(t)
	h.Skill("alpha", "does alpha things")
	h.RawSkill("folded", "---\nname: folded\ndescription: >-\n  first line\n  second line\n---\nbody\n")
	h.RawSkill("nodesc", "---\nname: nodesc\n---\nbody\n")
	h.RawSkill("renamed", "---\nname: other-name\ndescription: d\n---\n")
	h.RawSkill("plain", "no frontmatter here\n")
	h.EmptySkill("empty")
	h.Readme("| `alpha` | own | does alpha things |")

	got := scan(t, h)

	if _, ok := got["README.md"]; ok {
		t.Error("README.md listed as a skill")
	}
	if len(got) != 6 {
		t.Fatalf("got %d skills, want 6", len(got))
	}
	cases := []struct {
		name, description, fmName string
		hasSkillMD                bool
	}{
		{"alpha", "does alpha things", "alpha", true},
		{"folded", "first line second line", "folded", true},
		{"nodesc", "", "nodesc", true},
		{"renamed", "d", "other-name", true},
		{"plain", "", "", true},
		{"empty", "", "", false},
	}
	for _, c := range cases {
		s := got[c.name]
		if s.Description != c.description || s.FMName != c.fmName || s.HasSkillMD != c.hasSkillMD {
			t.Errorf("%s: got %+v", c.name, s)
		}
		if s.Origin.Vendored {
			t.Errorf("%s: vendored without a lock file", c.name)
		}
		if s.Dir != filepath.Join(h.SkillsDir(), c.name) {
			t.Errorf("%s: dir %q", c.name, s.Dir)
		}
	}
}

func TestScanAssignsOriginFromLock(t *testing.T) {
	h := testhome.New(t)
	h.Skill("own", "mine")
	h.Skill("vend", "theirs")
	h.Lock(map[string]string{"vend": "acme/skills", "orphan": "acme/gone"})

	paths := skill.Paths{Home: h.Dir}
	lock, err := skill.ReadLock(paths.LockFile())
	if err != nil {
		t.Fatal(err)
	}
	if lock.Missing || lock.Version != 3 || len(lock.Skills) != 2 {
		t.Fatalf("lock: %+v", lock)
	}
	if lock.Skills["orphan"].Source != "acme/gone" {
		t.Errorf("orphan entry: %+v", lock.Skills["orphan"])
	}

	got := scan(t, h)
	if o := got["own"].Origin; o.Vendored || o.String() != "own" {
		t.Errorf("own origin: %+v", o)
	}
	if o := got["vend"].Origin; !o.Vendored || o.Source != "acme/skills" || o.String() != "vendored (acme/skills)" {
		t.Errorf("vend origin: %+v", o)
	}
	if _, ok := got["orphan"]; ok {
		t.Error("lock orphan listed as a skill")
	}
}

func TestReadLockMissingFile(t *testing.T) {
	lock, err := skill.ReadLock(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !lock.Missing || len(lock.Skills) != 0 {
		t.Errorf("got %+v", lock)
	}
}

func TestScanModTimeAndMarkdown(t *testing.T) {
	h := testhome.New(t)
	dir := h.Skill("deep", "d")
	nested := h.File("deep", "docs/notes.md", "see the `other` skill")
	script := h.File("deep", "scripts/run.sh", "#!/bin/sh\n")
	old := time.Now().Add(-48 * time.Hour)
	for _, p := range []string{filepath.Join(dir, "SKILL.md"), script} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	newest := time.Now().Add(-1 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(nested, newest, newest); err != nil {
		t.Fatal(err)
	}

	s := scan(t, h)["deep"]
	if !s.ModTime.Equal(newest) {
		t.Errorf("modtime %v, want %v", s.ModTime, newest)
	}
	want := []string{"SKILL.md", "docs/notes.md"}
	if len(s.Markdown) != len(want) || s.Markdown[0] != want[0] || s.Markdown[1] != want[1] {
		t.Errorf("markdown %v, want %v", s.Markdown, want)
	}
}

func TestScanMissingDir(t *testing.T) {
	_, err := skill.Scan(filepath.Join(t.TempDir(), "missing"), skill.Lock{Missing: true})
	if err == nil {
		t.Fatal("expected error for missing skills dir")
	}
}
