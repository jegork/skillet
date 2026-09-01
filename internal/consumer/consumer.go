// Package consumer reports which skills each agent tool can see.
package consumer

import (
	"bytes"
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
	// Enable makes the skill visible; Disable hides it. Both are idempotent.
	Enable(name string) error
	Disable(name string) error
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

func (c *SymlinkDir) Enable(name string) error {
	p := filepath.Join(c.dir, name)
	if info, err := os.Lstat(p); err == nil {
		if info.Mode()&fs.ModeSymlink == 0 {
			return fmt.Errorf("%s: %s is not a symlink, refusing to replace it", c.name, p)
		}
		if _, state := c.inspect(p, fs.FileInfoToDirEntry(info)); state == StubOK {
			return nil
		}
		if err := os.Remove(p); err != nil {
			return err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return err
	}
	target, err := filepath.Rel(c.dir, filepath.Join(c.skillsDir, name))
	if err != nil {
		return err
	}
	return os.Symlink(target, p)
}

func (c *SymlinkDir) Disable(name string) error {
	p := filepath.Join(c.dir, name)
	info, err := os.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		return fmt.Errorf("%s: %s is not a symlink, refusing to remove it", c.name, p)
	}
	return os.Remove(p)
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

func (c *Omp) Enable(name string) error {
	doc, seq, err := c.load()
	if err != nil {
		return err
	}
	kept := seq.Content[:0]
	found := false
	for _, n := range seq.Content {
		if n.Value == name {
			found = true
			continue
		}
		if ok, _ := path.Match(n.Value, name); ok {
			return fmt.Errorf("omp: %q is hidden by pattern %q in %s, edit the config by hand", name, n.Value, c.configPath)
		}
		kept = append(kept, n)
	}
	if !found {
		return nil
	}
	seq.Content = kept
	return c.save(doc)
}

func (c *Omp) Disable(name string) error {
	doc, seq, err := c.load()
	if err != nil {
		return err
	}
	for _, n := range seq.Content {
		if ok, _ := path.Match(n.Value, name); ok {
			return nil
		}
	}
	seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: name})
	return c.save(doc)
}

// load parses the config as a node tree so comments survive a rewrite, and
// returns the skills.ignoredSkills sequence, creating it when absent.
func (c *Omp) load() (*yaml.Node, *yaml.Node, error) {
	doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	b, err := os.ReadFile(c.configPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, nil, err
	}
	if err == nil && len(b) > 0 {
		doc = &yaml.Node{}
		if err := yaml.Unmarshal(b, doc); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", c.configPath, err)
		}
		if len(doc.Content) == 0 {
			doc.Content = []*yaml.Node{{Kind: yaml.MappingNode}}
		}
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, nil, fmt.Errorf("%s: top level is not a mapping", c.configPath)
	}
	skills := mappingChild(root, "skills", yaml.MappingNode)
	seq := mappingChild(skills, "ignoredSkills", yaml.SequenceNode)
	return doc, seq, nil
}

func mappingChild(m *yaml.Node, key string, kind yaml.Kind) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			if m.Content[i+1].Kind != kind {
				m.Content[i+1] = &yaml.Node{Kind: kind}
			}
			return m.Content[i+1]
		}
	}
	child := &yaml.Node{Kind: kind}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, child)
	return child
}

func (c *Omp) save(doc *yaml.Node) error {
	if err := os.MkdirAll(filepath.Dir(c.configPath), 0o700); err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if info, err := os.Stat(c.configPath); err == nil {
		mode = info.Mode().Perm()
	}
	return os.WriteFile(c.configPath, buf.Bytes(), mode)
}

func matchesAny(patterns []string, name string) bool {
	for _, p := range patterns {
		if ok, _ := path.Match(p, name); ok {
			return true
		}
	}
	return false
}
