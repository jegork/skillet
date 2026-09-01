package consumer_test

import (
	"os"
	"path/filepath"
	"strings"
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

func TestSymlinkDirToggle(t *testing.T) {
	h := testhome.New(t)
	h.Skill("a", "A")
	h.Skill("b", "B")
	dir := filepath.Join(h.Dir, ".codex", "skills")
	c := consumer.NewSymlinkDir("codex", dir, h.SkillsDir())

	// enable creates the dir and a relative symlink
	if err := c.Enable("a"); err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(filepath.Join(dir, "a"))
	if err != nil || target != filepath.Join("..", "..", ".agents", "skills", "a") {
		t.Fatalf("stub target %q err %v", target, err)
	}
	if err := c.Enable("a"); err != nil {
		t.Errorf("enable twice must be a no-op: %v", err)
	}

	// a broken stub is replaced
	h.Stub(".codex/skills", "b", "../../.agents/skills/gone")
	if err := c.Enable("b"); err != nil {
		t.Fatal(err)
	}
	rep, _ := c.Report(skills("a", "b"))
	if !rep.Enabled["a"] || !rep.Enabled["b"] {
		t.Errorf("enabled %v", rep.Enabled)
	}

	// disable removes symlinks only
	if err := c.Disable("a"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "a")); !os.IsNotExist(err) {
		t.Error("stub a still there")
	}
	if err := c.Disable("a"); err != nil {
		t.Errorf("disable of a missing stub must be a no-op: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := c.Disable("real"); err == nil {
		t.Error("disable must refuse to remove a real directory")
	}
	if err := c.Enable("real"); err == nil {
		t.Error("enable must refuse to replace a real directory")
	}
}

func TestOmpToggle(t *testing.T) {
	h := testhome.New(t)
	p := h.OmpIgnore("handoff", "omp-*")
	if err := os.WriteFile(p, []byte("# keep this comment\nskills:\n  ignoredSkills:\n    - handoff\n    - omp-* # trailing\nother: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := consumer.NewOmp(p)
	all := skills("handoff", "omp-review", "tdd")

	if err := c.Disable("tdd"); err != nil {
		t.Fatal(err)
	}
	if err := c.Disable("tdd"); err != nil {
		t.Errorf("disable twice must be a no-op: %v", err)
	}
	rep, _ := c.Report(all)
	if rep.Enabled["tdd"] {
		t.Error("tdd should be hidden")
	}
	if err := c.Enable("handoff"); err != nil {
		t.Fatal(err)
	}
	if err := c.Enable("omp-review"); err == nil {
		t.Error("enable must refuse when a glob pattern hides the skill")
	}
	rep, _ = c.Report(all)
	if !rep.Enabled["handoff"] || rep.Enabled["omp-review"] {
		t.Errorf("enabled %v", rep.Enabled)
	}
	out, _ := os.ReadFile(p)
	got := string(out)
	for _, want := range []string{"# keep this comment", "other: 1", "    - omp-* # trailing", "    - tdd"} {
		if !strings.Contains(got, want) {
			t.Errorf("config lost %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "- handoff") {
		t.Errorf("handoff still ignored:\n%s", got)
	}

	// no config yet: disable creates one
	fresh := consumer.NewOmp(filepath.Join(h.Dir, "nested", "config.yml"))
	if err := fresh.Disable("x"); err != nil {
		t.Fatal(err)
	}
	rep, err := fresh.Report(skills("x", "y"))
	if err != nil || rep.Enabled["x"] || !rep.Enabled["y"] {
		t.Errorf("fresh config: %v %v", rep.Enabled, err)
	}
}

func TestForget(t *testing.T) {
	h := testhome.New(t)
	h.Skill("a", "A")
	h.Stub(".claude/skills", "a", "../../.agents/skills/a")
	claude := consumer.NewSymlinkDir("claude", filepath.Join(h.Dir, ".claude", "skills"), h.SkillsDir())
	if err := claude.Forget("a"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(h.Dir, ".claude", "skills", "a")); !os.IsNotExist(err) {
		t.Error("stub still there")
	}
	if err := claude.Forget("a"); err != nil {
		t.Errorf("forget twice: %v", err)
	}

	omp := consumer.NewOmp(h.OmpIgnore("handoff", "omp-*"))
	if err := omp.Forget("handoff"); err != nil {
		t.Fatal(err)
	}
	if err := omp.Forget("omp-review"); err != nil {
		t.Errorf("forget of a glob-hidden name must not error: %v", err)
	}
	rep, _ := omp.Report(skills("handoff", "omp-review"))
	if !rep.Enabled["handoff"] || rep.Enabled["omp-review"] {
		t.Errorf("enabled %v", rep.Enabled)
	}
}
