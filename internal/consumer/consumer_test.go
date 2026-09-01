package consumer_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jegork/skillet/internal/consumer"
	"github.com/jegork/skillet/internal/skill"
	"github.com/jegork/skillet/internal/testhome"
)

func skills(names ...string) []skill.Skill {
	var out []skill.Skill
	for _, n := range names {
		out = append(out, skill.Skill{Name: n})
	}
	return out
}

func TestSymlinkDirReport(t *testing.T) {
	h := testhome.New(t)
	for _, n := range []string{"ok", "foreign", "other"} {
		h.Skill(n, "d")
	}
	h.Stub(".claude/skills", "ok", "../../.agents/skills/ok")
	h.Stub(".claude/skills", "dangling", "../../.agents/skills/dangling")
	h.Stub(".claude/skills", "foreign", "../../.agents/skills/other")
	h.Stub(".claude/skills", "abs", filepath.Join(t.TempDir(), "elsewhere"))
	h.Stub(".claude/skills", ".system", "../../.agents/skills/ok")
	if err := os.MkdirAll(filepath.Join(h.Dir, ".claude", "skills", "realdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	c := consumer.NewSymlinkDir("claude", filepath.Join(h.Dir, ".claude", "skills"), h.SkillsDir())
	if c.Name() != "claude" {
		t.Errorf("name %q", c.Name())
	}
	rep, err := c.Report(skills("ok", "foreign", "other"))
	if err != nil {
		t.Fatal(err)
	}

	if !rep.Enabled["ok"] || rep.Enabled["other"] || rep.Enabled["foreign"] || rep.Enabled[".system"] {
		t.Errorf("enabled: %v", rep.Enabled)
	}
	want := map[string]consumer.StubState{
		"ok":       consumer.StubOK,
		"dangling": consumer.StubDangling,
		"foreign":  consumer.StubForeign,
		"abs":      consumer.StubForeign,
		"realdir":  consumer.StubNotSymlink,
	}
	got := map[string]consumer.StubState{}
	for _, s := range rep.Stubs {
		got[s.Name] = s.State
		if s.Path != filepath.Join(h.Dir, ".claude", "skills", s.Name) {
			t.Errorf("%s: path %q", s.Name, s.Path)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("stubs %v, want %v", got, want)
	}
	for n, st := range want {
		if got[n] != st {
			t.Errorf("%s: state %v, want %v", n, got[n], st)
		}
	}
}

func TestSymlinkDirMissing(t *testing.T) {
	h := testhome.New(t)
	c := consumer.NewSymlinkDir("codex", filepath.Join(h.Dir, ".codex", "skills"), h.SkillsDir())
	rep, err := c.Report(skills("a"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Stubs) != 0 || rep.Enabled["a"] {
		t.Errorf("got %+v", rep)
	}
}

func TestOmpReport(t *testing.T) {
	h := testhome.New(t)
	all := skills("handoff", "omp-review", "omp-runtime", "tdd", "omphalos")

	c := consumer.NewOmp(filepath.Join(h.Dir, ".omp", "agent", "config.yml"))
	rep, err := c.Report(all)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range all {
		if !rep.Enabled[s.Name] {
			t.Errorf("no config: %s should be enabled", s.Name)
		}
	}

	h.OmpIgnore("handoff", "omp-*")
	rep, err = c.Report(all)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"handoff": false, "omp-review": false, "omp-runtime": false, "tdd": true, "omphalos": true}
	for n, en := range want {
		if rep.Enabled[n] != en {
			t.Errorf("%s: enabled=%v, want %v", n, rep.Enabled[n], en)
		}
	}
	if len(rep.Stubs) != 0 {
		t.Errorf("omp has no stubs, got %v", rep.Stubs)
	}
}

func TestOmpMalformedConfig(t *testing.T) {
	h := testhome.New(t)
	p := h.OmpIgnore("x")
	if err := os.WriteFile(p, []byte("skills: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := consumer.NewOmp(p).Report(skills("x")); err == nil {
		t.Fatal("expected error for malformed yaml")
	}
}
