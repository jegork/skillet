package project_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jegork/skillet/internal/config"
	"github.com/jegork/skillet/internal/consumer"
	"github.com/jegork/skillet/internal/project"
	"github.com/jegork/skillet/internal/testhome"
)

func cfgWith(roots, paths []string) config.Config {
	return config.Config{Projects: config.Projects{Roots: roots, Paths: paths}}
}

func discover(t *testing.T, h *testhome.Home, cfg config.Config) []project.Project {
	t.Helper()
	ps, err := project.Discover(h.Dir, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return ps
}

func keys(ps []project.Project) []string {
	var out []string
	for _, p := range ps {
		out = append(out, filepath.Base(p.Root))
	}
	return out
}

func TestDiscoverChildrenOfRoot(t *testing.T) {
	h := testhome.New(t)
	h.ProjectSkill(h.ProjectDir("src/web-perf"), "a-skill", "does a")
	h.ProjectBareSkill(h.ProjectDir("src/cli-helper"), "b-skill", "does b")
	h.ProjectDir("src/empty")

	ps := discover(t, h, cfgWith([]string{h.Dir + "/src"}, nil))

	if got := keys(ps); len(got) != 2 || got[0] != "cli-helper" || got[1] != "web-perf" {
		t.Fatalf("got %v, want the two projects with skills, sorted", got)
	}
}

func TestDiscoverGlobRootMatchesProjects(t *testing.T) {
	h := testhome.New(t)
	h.ProjectSkill(h.ProjectDir("ws/alpha"), "a-skill", "does a")
	h.ProjectSkill(h.ProjectDir("ws/beta"), "b-skill", "does b")
	h.ProjectDir("ws/missing")

	ps := discover(t, h, cfgWith([]string{h.Dir + "/ws/al*"}, nil))

	if len(ps) != 1 || filepath.Base(ps[0].Root) != "alpha" {
		t.Fatalf("got %v, want only the alpha worktree", keys(ps))
	}
}

func TestDiscoverPathsAndDedup(t *testing.T) {
	h := testhome.New(t)
	one := h.ProjectDir("p/one")
	two := h.ProjectDir("p/two")
	h.ProjectSkill(one, "a-skill", "does a")
	h.ProjectSkill(two, "b-skill", "does b")

	cfg := cfgWith([]string{h.Dir + "/p"}, []string{h.Dir + "/p/two", h.Dir + "/p/two"})
	ps := discover(t, h, cfg)

	if len(ps) != 2 {
		t.Fatalf("got %v, want one entry per project", keys(ps))
	}
}

func TestDiscoverMissingRoot(t *testing.T) {
	h := testhome.New(t)
	ps := discover(t, h, cfgWith([]string{h.Dir + "/gone", h.Dir + "/gone/*"}, nil))
	if len(ps) != 0 {
		t.Errorf("got %v, want none", keys(ps))
	}
}

func TestProjectLayouts(t *testing.T) {
	h := testhome.New(t)
	mirror := h.ProjectDir("src/mirror")
	h.ProjectSkill(mirror, "a-skill", "does a")
	h.ProjectLock(mirror, map[string]string{"a-skill": "acme/repo"})
	h.ProjectStub(mirror, ".claude/skills", "a-skill", "../../.agents/skills/a-skill")

	bare := h.ProjectDir("src/bare")
	h.ProjectBareSkill(bare, "b-skill", "does b")

	ps := discover(t, h, cfgWith([]string{h.Dir + "/src"}, nil))
	if len(ps) != 2 {
		t.Fatalf("got %v", keys(ps))
	}
	byBase := map[string]project.Project{}
	for _, p := range ps {
		byBase[filepath.Base(p.Root)] = p
	}

	m, ok := byBase["mirror"]
	if !ok {
		t.Fatal("mirror project missing")
	}
	if m.SkillsDir() != filepath.Join(mirror, ".agents", "skills") {
		t.Errorf("mirror canonical dir %q", m.SkillsDir())
	}
	if !m.Mirror() {
		t.Error("mirror layout not detected")
	}
	kinds := consumerKinds(m)
	if kinds["claude"] != "symlink" || kinds["codex"] != "symlink" {
		t.Errorf("mirror consumers %v", kinds)
	}

	b, ok := byBase["bare"]
	if !ok {
		t.Fatal("bare project missing")
	}
	if b.SkillsDir() != filepath.Join(bare, ".claude", "skills") {
		t.Errorf("bare canonical dir %q", b.SkillsDir())
	}
	if b.Mirror() {
		t.Error("bare project detected as mirror")
	}
	kinds = consumerKinds(b)
	if kinds["claude"] != "native" {
		t.Errorf("bare claude consumer = %q", kinds["claude"])
	}
}

func TestConsumersSeeProjectStubs(t *testing.T) {
	h := testhome.New(t)
	p := h.ProjectDir("src/mirror")
	h.ProjectSkill(p, "a-skill", "does a")
	h.ProjectStub(p, ".claude/skills", "dangling", "../../.agents/skills/dangling")

	ps := discover(t, h, cfgWith([]string{h.Dir + "/src"}, nil))
	if len(ps) != 1 {
		t.Fatalf("got %v", keys(ps))
	}
	var claude consumer.Consumer
	for _, c := range ps[0].Consumers() {
		if c.Name() == "claude" {
			claude = c
		}
	}
	if claude == nil {
		t.Fatal("no claude consumer")
	}
	rep, err := claude.Report(nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Enabled["dangling"] {
		t.Error("broken project stub counted as enabled")
	}
	found := false
	for _, s := range rep.Stubs {
		if s.Name == "dangling" && s.State == consumer.StubDangling {
			found = true
		}
	}
	if !found {
		t.Errorf("broken stub not reported: %+v", rep.Stubs)
	}
}

func TestNoSkillsMeansNoProject(t *testing.T) {
	h := testhome.New(t)
	h.ProjectDir("src/plain")
	if err := os.MkdirAll(filepath.Join(h.Dir, "src", "plain", "skills-lock.json"), 0o755); err != nil {
		t.Fatal(err)
	}

	ps := discover(t, h, cfgWith([]string{h.Dir + "/src"}, nil))
	if len(ps) != 0 {
		t.Errorf("got %v, want no projects", keys(ps))
	}
}

func TestSkillsDirMissingLayout(t *testing.T) {
	h := testhome.New(t)
	p := project.Project{Root: filepath.Join(h.Dir, "nothing")}
	if _, err := os.Stat(p.SkillsDir()); !os.IsNotExist(err) {
		t.Errorf("SkillsDir %q should not exist (err %v)", p.SkillsDir(), err)
	}
}

func consumerKinds(p project.Project) map[string]string {
	out := map[string]string{}
	for _, c := range p.Consumers() {
		switch c.(type) {
		case *consumer.SymlinkDir:
			out[c.Name()] = "symlink"
		case *consumer.Native:
			out[c.Name()] = "native"
		default:
			out[c.Name()] = "unknown"
		}
	}
	return out
}
