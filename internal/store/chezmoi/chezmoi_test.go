package chezmoi_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jegork/skillet/internal/store"
	"github.com/jegork/skillet/internal/store/chezmoi"
)

var roots = []store.Root{
	{Rel: ".agents/skills"},
	{Rel: ".agents/.skill-lock.json"},
	{Rel: ".claude/skills"},
	{Rel: ".codex/skills", Exclude: []string{".system"}},
}

type fixture struct {
	t      *testing.T
	home   string
	source string
	remote string
	store  *chezmoi.Store
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	if _, err := exec.LookPath("chezmoi"); err != nil {
		t.Skip("chezmoi not on PATH")
	}
	// chezmoi refuses to add anything under the dir holding its config, so
	// every piece lives in its own temp dir
	f := &fixture{t: t, home: t.TempDir(), source: t.TempDir(), remote: t.TempDir()}
	base := t.TempDir()
	cfg := filepath.Join(base, "chezmoi.toml")
	if err := os.WriteFile(cfg, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	f.store = &chezmoi.Store{
		Bin: "chezmoi", Home: f.home, Roots: roots,
		Args: []string{
			"--source", f.source, "--destination", f.home, "--config", cfg,
			"--persistent-state", filepath.Join(base, "state.boltdb"), "--cache", filepath.Join(base, "cache"),
		},
		Env: gitEnv(),
	}

	f.sourceFile("dot_agents/exact_skills/a/SKILL.md", "---\nname: a\ndescription: A\n---\n")
	f.sourceFile("dot_claude/exact_skills/symlink_a", "../../.agents/skills/a\n")
	f.sourceFile("dot_codex/skills/symlink_a", "../../.agents/skills/a\n")
	f.sourceFile("dot_zshrc", "# zsh\n")
	f.git("init", "-b", "main")
	f.git("add", "-A")
	f.git("commit", "-m", "init")
	f.git("-C", f.remote, "init", "--bare", "-b", "main")
	f.git("remote", "add", "origin", f.remote)
	f.git("push", "-u", "origin", "main")

	f.chezmoi("apply")
	f.homeFile(".codex/skills/.system/marker", "system\n")
	return f
}

func gitEnv() []string {
	return []string{
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
	}
}

func (f *fixture) git(args ...string) string {
	f.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = f.source
	cmd.Env = append(os.Environ(), gitEnv()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func (f *fixture) chezmoi(args ...string) {
	f.t.Helper()
	cmd := exec.Command("chezmoi", append(append([]string{"--no-pager", "--no-tty"}, f.store.Args...), args...)...)
	cmd.Env = append(os.Environ(), gitEnv()...)
	if out, err := cmd.CombinedOutput(); err != nil {
		f.t.Fatalf("chezmoi %v: %v\n%s", args, err, out)
	}
}

func (f *fixture) sourceFile(rel, content string) { f.write(filepath.Join(f.source, rel), content) }
func (f *fixture) homeFile(rel, content string)   { f.write(filepath.Join(f.home, rel), content) }

func (f *fixture) write(p, content string) {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) homeStub(consumerDir, name string) {
	f.t.Helper()
	p := filepath.Join(f.home, consumerDir, name)
	if err := os.Symlink("../../.agents/skills/"+name, p); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) sourceExists(rel string) bool {
	_, err := os.Lstat(filepath.Join(f.source, rel))
	return err == nil
}

func (f *fixture) status() store.Status {
	f.t.Helper()
	st, err := f.store.Status(context.Background())
	if err != nil {
		f.t.Fatal(err)
	}
	return st
}

func (f *fixture) capture() {
	f.t.Helper()
	if err := f.store.Capture(context.Background()); err != nil {
		f.t.Fatal(err)
	}
}

func changes(st store.Status) map[string]store.ChangeKind {
	out := map[string]store.ChangeKind{}
	for _, c := range st.Uncaptured {
		out[c.Path] = c.Kind
	}
	return out
}

func TestRoundTrip(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// the lock file is a root but not managed yet: only that shows up
	st := f.status()
	if got := changes(st); len(got) != 0 || len(st.Uncommitted) != 0 {
		t.Fatalf("fresh fixture not clean: %+v", st)
	}
	if st.Branch != "main" || st.Ahead != 0 {
		t.Errorf("branch %q ahead %d", st.Branch, st.Ahead)
	}

	f.homeFile(".agents/skills/a/SKILL.md", "---\nname: a\ndescription: A2\n---\n")
	f.homeFile(".agents/.skill-lock.json", "{\"version\":3,\"skills\":{}}\n")
	f.homeFile(".agents/skills/b/SKILL.md", "---\nname: b\ndescription: B\n---\n")
	f.homeStub(".claude/skills", "b")
	f.homeStub(".codex/skills", "b")

	got := changes(f.status())
	want := map[string]store.ChangeKind{
		".agents/skills/a/SKILL.md": store.Modified,
		".agents/.skill-lock.json":  store.Added,
		".agents/skills/b":          store.Added,
		".claude/skills/b":          store.Added,
		".codex/skills/b":           store.Added,
	}
	for p, k := range want {
		if got[p] != k {
			t.Errorf("%s: got %v want %v (all: %v)", p, got[p], k, got)
		}
	}
	if _, ok := got[".codex/skills/.system"]; ok {
		t.Error("excluded .system reported as uncaptured")
	}
	if _, ok := got[".agents/skills/b/SKILL.md"]; ok {
		t.Error("child of a new dir should be collapsed into the dir")
	}

	f.capture()
	for _, rel := range []string{
		"dot_agents/exact_skills/b/SKILL.md",
		"dot_agents/dot_skill-lock.json",
		"dot_claude/exact_skills/symlink_b",
		"dot_codex/skills/symlink_b",
	} {
		if !f.sourceExists(rel) {
			t.Errorf("missing in source after capture: %s", rel)
		}
	}
	for _, rel := range []string{"dot_agents/skills", "dot_codex/skills/dot_system"} {
		if f.sourceExists(rel) {
			t.Errorf("must not exist in source: %s", rel)
		}
	}
	st = f.status()
	if len(st.Uncaptured) != 0 {
		t.Errorf("uncaptured after capture: %v", st.Uncaptured)
	}
	if len(st.Uncommitted) == 0 {
		t.Error("expected uncommitted changes after capture")
	}

	// unrelated dirty file in the source must stay out of diff and commit
	f.sourceFile("dot_zshrc", "# changed\n")
	diff, err := f.store.Diff(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "symlink_b") || strings.Contains(diff, "dot_zshrc") {
		t.Errorf("diff scope wrong:\n%s", diff)
	}
	if err := f.store.Commit(ctx, "chore(skills): add b"); err != nil {
		t.Fatal(err)
	}
	if out := f.git("status", "--porcelain"); strings.TrimRight(out, "\n") != " M dot_zshrc" {
		t.Errorf("unexpected git status after commit:\n%s", out)
	}
	st = f.status()
	if st.Ahead != 1 || len(st.Uncommitted) != 0 {
		t.Errorf("after commit: %+v", st)
	}
	if err := f.store.Push(ctx); err != nil {
		t.Fatal(err)
	}
	if st = f.status(); st.Ahead != 0 {
		t.Errorf("after push ahead=%d", st.Ahead)
	}

	// deleting a skill and its stubs is captured as a forget
	for _, rel := range []string{".agents/skills/a", ".claude/skills/a", ".codex/skills/a"} {
		if err := os.RemoveAll(filepath.Join(f.home, rel)); err != nil {
			t.Fatal(err)
		}
	}
	got = changes(f.status())
	if got[".agents/skills/a"] != store.Deleted || got[".claude/skills/a"] != store.Deleted || got[".codex/skills/a"] != store.Deleted {
		t.Errorf("deletions not reported: %v", got)
	}
	f.capture()
	for _, rel := range []string{"dot_agents/exact_skills/a", "dot_claude/exact_skills/symlink_a", "dot_codex/skills/symlink_a"} {
		if f.sourceExists(rel) {
			t.Errorf("still in source after forget: %s", rel)
		}
	}
	if st = f.status(); len(st.Uncaptured) != 0 {
		t.Errorf("uncaptured after forget: %v", st.Uncaptured)
	}
}

func TestBrokenBinaryReturnsError(t *testing.T) {
	s := chezmoi.New(t.TempDir(), roots)
	s.Bin = "definitely-not-chezmoi"
	if _, err := s.Status(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}
