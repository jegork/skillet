// Package skill is the domain model: what skills exist on disk and where they come from.
package skill

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Paths struct{ Home string }

func (p Paths) SkillsDir() string { return filepath.Join(p.Home, ".agents", "skills") }
func (p Paths) LockFile() string  { return filepath.Join(p.Home, ".agents", ".skill-lock.json") }
func (p Paths) Readme() string    { return filepath.Join(p.SkillsDir(), "README.md") }

type Origin struct {
	Vendored bool
	Source   string
}

func (o Origin) String() string {
	if o.Vendored {
		return "vendored (" + o.Source + ")"
	}
	return "own"
}

type Skill struct {
	Name        string
	Dir         string
	Description string
	FMName      string
	Origin      Origin
	ModTime     time.Time
	HasSkillMD  bool
	Markdown    []string
}

type Frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

var ErrNoFrontmatter = errors.New("no frontmatter")

func ParseFrontmatter(b []byte) (Frontmatter, error) {
	var fm Frontmatter
	rest, ok := bytes.CutPrefix(b, []byte("---\n"))
	if !ok {
		return fm, ErrNoFrontmatter
	}
	end := bytes.Index(rest, []byte("\n---"))
	if end < 0 {
		return fm, ErrNoFrontmatter
	}
	if err := yaml.Unmarshal(rest[:end], &fm); err != nil {
		return fm, fmt.Errorf("frontmatter: %w", err)
	}
	fm.Description = strings.TrimSpace(fm.Description)
	return fm, nil
}

// Scan lists every directory under skillsDir as a skill, sorted by name.
func Scan(skillsDir string, lock Lock) ([]Skill, error) {
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil, err
	}
	var skills []Skill
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		s, err := read(filepath.Join(skillsDir, e.Name()))
		if err != nil {
			return nil, err
		}
		if entry, ok := lock.Skills[s.Name]; ok {
			s.Origin = Origin{Vendored: true, Source: entry.Source}
		}
		skills = append(skills, s)
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return skills, nil
}

func read(dir string) (Skill, error) {
	s := Skill{Name: filepath.Base(dir), Dir: dir}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.ModTime().After(s.ModTime) {
			s.ModTime = info.ModTime()
		}
		if strings.HasSuffix(d.Name(), ".md") {
			rel, _ := filepath.Rel(dir, p)
			s.Markdown = append(s.Markdown, rel)
		}
		return nil
	})
	if err != nil {
		return s, err
	}
	sort.Slice(s.Markdown, func(i, j int) bool {
		// SKILL.md first, then the rest alphabetically
		if s.Markdown[i] == "SKILL.md" || s.Markdown[j] == "SKILL.md" {
			return s.Markdown[i] == "SKILL.md"
		}
		return s.Markdown[i] < s.Markdown[j]
	})
	b, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	s.HasSkillMD = true
	if fm, err := ParseFrontmatter(b); err == nil {
		s.FMName, s.Description = fm.Name, fm.Description
	}
	return s, nil
}
