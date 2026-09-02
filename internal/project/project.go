// Package project discovers project-level skills: fixed probes in the
// directories the config's projects.roots and projects.paths point at.
package project

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jegork/skillet/internal/config"
	"github.com/jegork/skillet/internal/consumer"
	"github.com/jegork/skillet/internal/skill"
)

type Project struct {
	Root string
	Lock skill.ProjectLock
}

func (p Project) Mirror() bool {
	info, err := os.Stat(filepath.Join(p.Root, ".agents", "skills"))
	return err == nil && info.IsDir()
}

// SkillsDir is the project's canonical skills dir: .agents/skills in the
// mirror layout, else the bare .claude/skills.
func (p Project) SkillsDir() string {
	if p.Mirror() {
		return filepath.Join(p.Root, ".agents", "skills")
	}
	return filepath.Join(p.Root, ".claude", "skills")
}

// LockFile is the project's skills-lock.json path.
func (p Project) LockFile() string {
	return filepath.Join(p.Root, "skills-lock.json")
}

// Consumers builds the adapters for the project's consumer dirs: claude as
// symlink stubs in the mirror layout or a native dir otherwise, codex as a
// symlink dir in both.
func (p Project) Consumers() []consumer.Consumer {
	skillsDir := p.SkillsDir()
	claudeDir := filepath.Join(p.Root, ".claude", "skills")
	var claude consumer.Consumer
	if p.Mirror() {
		claude = consumer.NewSymlinkDir("claude", claudeDir, skillsDir)
	} else {
		claude = consumer.NewNative("claude", claudeDir)
	}
	return []consumer.Consumer{
		claude,
		consumer.NewSymlinkDir("codex", filepath.Join(p.Root, ".codex", "skills"), skillsDir),
	}
}

// hasSkillsDir reports whether the project keeps skills in any layout.
func (p Project) hasSkills() bool {
	for _, dir := range []string{".agents", ".claude", ".codex"} {
		info, err := os.Stat(filepath.Join(p.Root, dir, "skills"))
		if err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

// Discover resolves the config's project roots and paths into the projects
// that contain skills. A plain root contributes its children; a root with
// glob metacharacters matches the projects directly. projects.paths entries
// are the projects themselves, glob or not.
func Discover(home string, cfg config.Config) ([]Project, error) {
	seen := map[string]bool{}
	var out []Project
	add := func(root string) {
		root = filepath.Clean(root)
		if root == home || seen[root] {
			return
		}
		seen[root] = true
		out = append(out, Project{Root: root})
	}
	for _, pattern := range cfg.Projects.Roots {
		if !hasMeta(pattern) {
			children(pattern, add)
			continue
		}
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			add(m)
		}
	}
	for _, pattern := range cfg.Projects.Paths {
		matches := []string{pattern}
		if hasMeta(pattern) {
			var err error
			if matches, err = filepath.Glob(pattern); err != nil {
				return nil, err
			}
		}
		for _, m := range matches {
			add(m)
		}
	}
	var kept []Project
	for _, p := range out {
		if p.hasSkills() {
			kept = append(kept, p)
		}
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].Root < kept[j].Root })
	return kept, nil
}

// children probes the immediate subdirectories of a plain root.
func children(root string, add func(string)) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			add(filepath.Join(root, e.Name()))
		}
	}
}

func hasMeta(pattern string) bool {
	return strings.ContainsAny(pattern, "*?[")
}
