// Package testhome builds a fake $HOME with the layout skillet expects.
package testhome

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jegork/skillet/internal/skill"
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

// ProjectDir creates a project root under the home and returns it.
func (h *Home) ProjectDir(name string) string {
	dir := filepath.Join(h.Dir, name)
	h.mkdir(dir)
	return dir
}

// ProjectSkill writes a skill with frontmatter into a project's canonical
// .agents/skills dir and returns the dir.
func (h *Home) ProjectSkill(project, name, description string) string {
	return h.ProjectRawSkill(project, name, fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n# %s\n", name, description, name))
}

// ProjectRawSkill is ProjectSkill with exact SKILL.md content.
func (h *Home) ProjectRawSkill(project, name, content string) string {
	dir := filepath.Join(project, ".agents", "skills", name)
	h.mkdir(dir)
	h.write(filepath.Join(dir, "SKILL.md"), content)
	return dir
}

// ProjectBareSkill writes a skill into a project's bare .claude/skills dir.
func (h *Home) ProjectBareSkill(project, name, description string) string {
	dir := filepath.Join(project, ".claude", "skills", name)
	h.mkdir(dir)
	h.write(filepath.Join(dir, "SKILL.md"), fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n# %s\n", name, description, name))
	return dir
}

// ProjectFile writes an extra file inside a project skill dir.
func (h *Home) ProjectFile(project, name, rel, content string) string {
	p := filepath.Join(project, ".agents", "skills", name, rel)
	h.mkdir(filepath.Dir(p))
	h.write(p, content)
	return p
}

// ProjectStub creates a symlink <project>/<consumerDir>/<name> -> target.
func (h *Home) ProjectStub(project, consumerDir, name, target string) string {
	dir := filepath.Join(project, consumerDir)
	h.mkdir(dir)
	p := filepath.Join(dir, name)
	if err := os.Symlink(target, p); err != nil {
		h.t.Fatal(err)
	}
	return p
}

// ProjectLock writes <project>/skills-lock.json v1 with each entry's
// computedHash taken from the skill folder as it stands.
func (h *Home) ProjectLock(project string, entries map[string]string) {
	hashes := map[string]string{}
	for name := range entries {
		dir := filepath.Join(canonicalDir(project), name)
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		hash, err := skill.ContentHash(dir)
		if err != nil {
			h.t.Fatal(err)
		}
		hashes[name] = hash
	}
	h.ProjectLockWithHashes(project, entries, hashes)
}

// ProjectLockWithHash is ProjectLock with one explicit computedHash for
// every entry.
func (h *Home) ProjectLockWithHash(project, hash string, entries map[string]string) {
	hashes := map[string]string{}
	for name := range entries {
		hashes[name] = hash
	}
	h.ProjectLockWithHashes(project, entries, hashes)
}

// ProjectLockWithHashes writes the lock with explicit per-entry hashes;
// entries without one get no computedHash.
func (h *Home) ProjectLockWithHashes(project string, entries, hashes map[string]string) {
	skills := map[string]any{}
	for name, source := range entries {
		e := map[string]any{"source": source, "sourceType": "github", "skillPath": "skills/" + name + "/SKILL.md"}
		if hash, ok := hashes[name]; ok {
			e["computedHash"] = hash
		}
		skills[name] = e
	}
	b, err := json.MarshalIndent(map[string]any{"version": 1, "skills": skills}, "", "  ")
	if err != nil {
		h.t.Fatal(err)
	}
	h.write(filepath.Join(project, "skills-lock.json"), string(b))
}

// Config writes the skillet config file.
func (h *Home) Config(content string) {
	p := filepath.Join(h.Dir, ".config", "skillet", "config.yml")
	h.mkdir(filepath.Dir(p))
	h.write(p, content)
}

// canonicalDir is a project's canonical skills dir: .agents/skills when the
// mirror layout is present, .claude/skills otherwise.
func canonicalDir(project string) string {
	agents := filepath.Join(project, ".agents", "skills")
	if info, err := os.Stat(agents); err == nil && info.IsDir() {
		return agents
	}
	return filepath.Join(project, ".claude", "skills")
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
