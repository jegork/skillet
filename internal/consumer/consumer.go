// Package consumer reports which skills each agent tool can see.
package consumer

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/jegork/skillet/internal/skill"
)

type StubState int

const (
	StubOK StubState = iota
	StubDangling
	StubForeign
	StubNotSymlink
)

func (s StubState) String() string {
	switch s {
	case StubOK:
		return "ok"
	case StubDangling:
		return "dangling symlink"
	case StubForeign:
		return "symlink points outside the skills dir"
	case StubNotSymlink:
		return "not a symlink"
	}
	return fmt.Sprintf("StubState(%d)", int(s))
}

type Stub struct {
	Name   string
	Path   string
	Target string
	State  StubState
}

type Report struct {
	Enabled map[string]bool
	Stubs   []Stub
}

type Consumer interface {
	Name() string
	Report(skills []skill.Skill) (Report, error)
}

// SymlinkDir is a consumer that sees a skill when <dir>/<name> is a symlink
// into the skills dir. Claude Code and Codex work this way.
type SymlinkDir struct {
	name, dir, skillsDir string
}

func NewSymlinkDir(name, dir, skillsDir string) *SymlinkDir {
	return &SymlinkDir{name: name, dir: dir, skillsDir: skillsDir}
}

func (c *SymlinkDir) Name() string { return c.name }

func (c *SymlinkDir) Report(skills []skill.Skill) (Report, error) {
	rep := Report{Enabled: map[string]bool{}}
	entries, err := os.ReadDir(c.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return rep, nil
	}
	if err != nil {
		return rep, err
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		stub := Stub{Name: e.Name(), Path: filepath.Join(c.dir, e.Name())}
		stub.Target, stub.State = c.inspect(stub.Path, e)
		rep.Stubs = append(rep.Stubs, stub)
		if stub.State == StubOK {
			rep.Enabled[stub.Name] = true
		}
	}
	return rep, nil
}

func (c *SymlinkDir) inspect(p string, e fs.DirEntry) (string, StubState) {
	if e.Type()&fs.ModeSymlink == 0 {
		return "", StubNotSymlink
	}
	target, err := os.Readlink(p)
	if err != nil {
		return "", StubDangling
	}
	resolved := target
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(c.dir, resolved)
	}
	if filepath.Clean(resolved) != filepath.Join(c.skillsDir, e.Name()) {
		return target, StubForeign
	}
	if _, err := os.Stat(p); err != nil {
		return target, StubDangling
	}
	return target, StubOK
}

// Omp reads the skills dir directly and hides names matching the
// skills.ignoredSkills globs in its config.
type Omp struct {
	configPath string
}

func NewOmp(configPath string) *Omp { return &Omp{configPath: configPath} }

func (c *Omp) Name() string { return "omp" }

func (c *Omp) Report(skills []skill.Skill) (Report, error) {
	rep := Report{Enabled: map[string]bool{}}
	patterns, err := c.ignored()
	if err != nil {
		return rep, err
	}
	for _, s := range skills {
		rep.Enabled[s.Name] = !matchesAny(patterns, s.Name)
	}
	return rep, nil
}

func (c *Omp) ignored() ([]string, error) {
	b, err := os.ReadFile(c.configPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg struct {
		Skills struct {
			IgnoredSkills []string `yaml:"ignoredSkills"`
		} `yaml:"skills"`
	}
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", c.configPath, err)
	}
	return cfg.Skills.IgnoredSkills, nil
}

func matchesAny(patterns []string, name string) bool {
	for _, p := range patterns {
		if ok, _ := path.Match(p, name); ok {
			return true
		}
	}
	return false
}
