package skill_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jegork/skillet/internal/skill"
	"github.com/jegork/skillet/internal/testhome"
)

func TestReadProjectLockMissingFile(t *testing.T) {
	h := testhome.New(t)
	lock, err := skill.ReadProjectLock(filepath.Join(h.Dir, "proj", "skills-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !lock.Missing || len(lock.Skills) != 0 {
		t.Errorf("missing lock must load as zero, got %+v", lock)
	}
}

func TestReadProjectLockParses(t *testing.T) {
	h := testhome.New(t)
	p := h.ProjectDir("proj")
	h.ProjectRawSkill(p, "web-perf", "---\nname: web-perf\ndescription: d\n---\n")
	h.ProjectLock(p, map[string]string{"web-perf": "buildfind/agent-skills"})

	lock, err := skill.ReadProjectLock(filepath.Join(p, "skills-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	if lock.Missing {
		t.Fatal("lock reported missing")
	}
	if lock.Version != 1 {
		t.Errorf("version = %d, want 1", lock.Version)
	}
	if lock.Skills["web-perf"].Source != "buildfind/agent-skills" {
		t.Errorf("skills = %v", lock.Skills)
	}
	if lock.Skills["web-perf"].ComputedHash == "" {
		t.Error("computedHash empty")
	}
}

func TestReadProjectLockBadJSON(t *testing.T) {
	h := testhome.New(t)
	p := h.ProjectDir("proj")
	lockPath := filepath.Join(p, "skills-lock.json")
	if err := os.WriteFile(lockPath, []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := skill.ReadProjectLock(lockPath); err == nil {
		t.Error("expected an error for malformed json")
	}
}

func TestContentHashStableAndSensitive(t *testing.T) {
	h := testhome.New(t)
	p := h.ProjectDir("proj")
	h.ProjectRawSkill(p, "alpha", "---\nname: alpha\ndescription: d\n---\nbody\n")
	h.ProjectFile(p, "alpha", "refs.md", "see `beta` skill\n")

	first, err := skill.ContentHash(filepath.Join(p, ".agents", "skills"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := skill.ContentHash(filepath.Join(p, ".agents", "skills"))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("hash not stable: %s vs %s", first, second)
	}

	// a nested file change must move the hash
	h.ProjectFile(p, "alpha", "refs.md", "see `beta` skill, edited\n")
	edited, err := skill.ContentHash(filepath.Join(p, ".agents", "skills"))
	if err != nil {
		t.Fatal(err)
	}
	if edited == first {
		t.Error("hash unchanged after an edit")
	}

	// a new skill moves it too
	h.ProjectSkill(p, "beta", "does beta things")
	added, err := skill.ContentHash(filepath.Join(p, ".agents", "skills"))
	if err != nil {
		t.Fatal(err)
	}
	if added == edited {
		t.Error("hash unchanged after adding a skill")
	}
}

func TestContentHashIgnoresSymlinks(t *testing.T) {
	h := testhome.New(t)
	p := h.ProjectDir("proj")
	h.ProjectRawSkill(p, "alpha", "---\nname: alpha\ndescription: d\n---\n")
	with, err := skill.ContentHash(filepath.Join(p, ".agents", "skills"))
	if err != nil {
		t.Fatal(err)
	}
	h.ProjectStub(p, ".claude/skills", "alpha", "../../.agents/skills/alpha")
	without, err := skill.ContentHash(filepath.Join(p, ".agents", "skills"))
	if err != nil {
		t.Fatal(err)
	}
	if with != without {
		t.Error("symlinks outside the skills dir must not affect the hash")
	}
}

func TestContentHashMissingDir(t *testing.T) {
	if _, err := skill.ContentHash(filepath.Join(os.TempDir(), "skillet-nope")); err == nil {
		t.Error("expected an error for a missing dir")
	}
}

// the vector comes from the algorithm validated against a real
// skills-lock.json: plain concatenation, case-insensitive path order
func TestContentHashMatchesSkillsCLI(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"SKILL.md":            "---\nname: x\n---\n",
		"references/Notes.md": "n",
		"references/a.md":     "a",
		"scripts/run.sh":      "#!/bin/sh\n",
	}
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "node_modules", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "node_modules", "x", "ignored.js"), []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := skill.ContentHash(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := "f7745d966e4e14317ac7fc136676f7f9ba50daa95997697b56c67713daa7fed6"; got != want {
		t.Errorf("ContentHash = %s, want %s", got, want)
	}
}
