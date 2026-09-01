// Package testhome builds a fake $HOME with the layout skillet expects.
package testhome

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type Home struct {
	Dir string
	t   *testing.T
}

func New(t *testing.T) *Home {
	t.Helper()
	h := &Home{Dir: t.TempDir(), t: t}
	h.mkdir(h.SkillsDir())
	return h
}

func (h *Home) SkillsDir() string { return filepath.Join(h.Dir, ".agents", "skills") }

// Skill writes a skill dir with a frontmatter SKILL.md and returns the dir.
func (h *Home) Skill(name, description string) string {
	return h.RawSkill(name, fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n# %s\n", name, description, name))
}

// RawSkill writes SKILL.md with exact content and returns the dir.
func (h *Home) RawSkill(name, content string) string {
	dir := h.EmptySkill(name)
	h.write(filepath.Join(dir, "SKILL.md"), content)
	return dir
}

// EmptySkill creates a skill dir without SKILL.md and returns it.
func (h *Home) EmptySkill(name string) string {
	dir := filepath.Join(h.SkillsDir(), name)
	h.mkdir(dir)
	return dir
}

// File writes an extra file inside a skill dir.
func (h *Home) File(name, rel, content string) string {
	p := filepath.Join(h.SkillsDir(), name, rel)
	h.mkdir(filepath.Dir(p))
	h.write(p, content)
	return p
}

// Lock writes .skill-lock.json v3 with name -> "owner/repo" entries.
func (h *Home) Lock(entries map[string]string) { h.LockWithHashes(entries, nil) }

// LockWithHashes is Lock with an explicit skillFolderHash per name.
func (h *Home) LockWithHashes(entries map[string]string, hashes map[string]string) {
	skills := map[string]any{}
	for name, source := range entries {
		// empty by default so the drift check stays quiet unless a test asks for it
		hash := ""
		if v, ok := hashes[name]; ok {
			hash = v
		}
		skills[name] = map[string]any{
			"source":          source,
			"sourceType":      "github",
			"sourceUrl":       "https://github.com/" + source + ".git",
			"skillPath":       "skills/" + name + "/SKILL.md",
			"skillFolderHash": hash,
			"installedAt":     "2026-04-02T19:20:46.521Z",
			"updatedAt":       "2026-08-03T07:56:20.074Z",
		}
	}
	b, err := json.MarshalIndent(map[string]any{"version": 3, "skills": skills}, "", "  ")
	if err != nil {
		h.t.Fatal(err)
	}
	h.write(filepath.Join(h.Dir, ".agents", ".skill-lock.json"), string(b))
}

// Stub creates a symlink <home>/<consumerDir>/<name> -> target.
func (h *Home) Stub(consumerDir, name, target string) string {
	dir := filepath.Join(h.Dir, consumerDir)
	h.mkdir(dir)
	p := filepath.Join(dir, name)
	if err := os.Symlink(target, p); err != nil {
		h.t.Fatal(err)
	}
	return p
}

// OmpIgnore writes .omp/agent/config.yml with the given ignoredSkills patterns.
func (h *Home) OmpIgnore(patterns ...string) string {
	var sb strings.Builder
	sb.WriteString("skills:\n  ignoredSkills:\n")
	for _, p := range patterns {
		sb.WriteString("    - " + p + "\n")
	}
	p := filepath.Join(h.Dir, ".omp", "agent", "config.yml")
	h.mkdir(filepath.Dir(p))
	h.write(p, sb.String())
	return p
}

// Readme writes ~/.agents/skills/README.md with a header and the given table rows.
func (h *Home) Readme(rows ...string) string {
	var sb strings.Builder
	sb.WriteString("# Skills index\n\n| Skill | Origin | What it does |\n|---|---|---|\n")
	for _, r := range rows {
		sb.WriteString(r + "\n")
	}
	p := filepath.Join(h.SkillsDir(), "README.md")
	h.write(p, sb.String())
	return p
}

func (h *Home) mkdir(dir string) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		h.t.Fatal(err)
	}
}

func (h *Home) write(path, content string) {
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		h.t.Fatal(err)
	}
}
