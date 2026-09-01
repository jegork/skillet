package gitrepo_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jegork/skillet/internal/store"
	"github.com/jegork/skillet/internal/store/gitrepo"
)

var roots = []store.Root{
	{Rel: ".agents/skills"},
	{Rel: ".agents/.skill-lock.json"},
	{Rel: ".claude/skills"},
	{Rel: ".codex/skills", Exclude: []string{".system"}},
}

type fixture struct {
	t     *testing.T
	home  string
	dir   string
	store *gitrepo.Store
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, home: t.TempDir(), dir: t.TempDir()}
	f.store = &gitrepo.Store{Bin: "git", Home: f.home, Dir: f.dir, Roots: roots, Env: gitEnv()}
	if err := gitrepo.Init(f.home, f.dir, ""); err != nil {
		t.Fatal(err)
	}

	f.homeFile(".agents/skills/a/SKILL.md", "---\nname: a\ndescription: A\n---\n")
	f.homeStub(".claude/skills", "a")
	f.homeStub(".codex/skills", "a")
	f.homeFile(".codex/skills/.system/marker", "system\n")
	f.homeFile(".zshrc", "# zsh\n")
	f.git("add", "-A", "--", ".agents/skills", ".claude/skills", ".codex/skills", ":(exclude).codex/skills/.system")
	f.git("commit", "-m", "init")
	remote := t.TempDir()
	f.gitIn(remote, "init", "--bare", "-b", "main", ".")
	f.git("remote", "add", "origin", remote)
	f.git("push", "-u", "origin", "main")
	return f
}

func gitEnv() []string {
	return []string{
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
	}
}

// git runs git against the store repo (its git dir, worktree $HOME).
func (f *fixture) git(args ...string) string {
	return f.gitIn(f.home, append([]string{"--git-dir", f.dir}, args...)...)
}

func (f *fixture) gitIn(dir string, args ...string) string {
	f.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), gitEnv()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func (f *fixture) homeFile(rel, content string) { f.write(filepath.Join(f.home, rel), content) }

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
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.Symlink("../../.agents/skills/"+name, p); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) tracked(rel string) bool {
	out := f.git("ls-files", "--", rel)
	return strings.TrimSpace(out) != ""
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

	// only the store roots are tracked: the unrelated .zshrc must stay invisible
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
		".agents/skills/b/SKILL.md",
		".agents/.skill-lock.json",
		".claude/skills/b",
		".codex/skills/b",
	} {
		if !f.tracked(rel) {
			t.Errorf("not tracked after capture: %s", rel)
		}
	}
	if f.tracked(".codex/skills/.system/marker") {
		t.Error("excluded .system must not be tracked")
	}
	st = f.status()
	if len(st.Uncaptured) != 0 {
		t.Errorf("uncaptured after capture: %v", st.Uncaptured)
	}
	if len(st.Uncommitted) == 0 {
		t.Error("expected uncommitted changes after capture")
	}

	// unrelated changes in $HOME stay out of diff and commit, even when staged
	f.homeFile(".zshrc", "# changed\n")
	f.git("add", ".zshrc")
	diff, err := f.store.Diff(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "skills/b") || strings.Contains(diff, "zshrc") {
		t.Errorf("diff scope wrong:\n%s", diff)
	}
	if err := f.store.Commit(ctx, "chore(skills): add b"); err != nil {
		t.Fatal(err)
	}
	// the staged .zshrc and the untracked excluded .system dir are the only
	// worktree-wide leftovers; nothing tracked outside the roots changed
	if out := f.git("status", "--porcelain"); strings.TrimRight(out, "\n") != "A  .zshrc\n?? .codex/skills/.system/" {
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

	// deleting a skill and its stubs is captured
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
	for _, rel := range []string{".agents/skills/a/SKILL.md", ".claude/skills/a", ".codex/skills/a"} {
		if f.tracked(rel) {
			t.Errorf("still tracked after forget: %s", rel)
		}
	}
	if st = f.status(); len(st.Uncaptured) != 0 {
		t.Errorf("uncaptured after forget: %v", st.Uncaptured)
	}
}

func TestInit(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".agents", ".skillet-store.git")
	remote := t.TempDir()
	if err := gitrepo.Init(home, dir, remote); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("git", "--git-dir", dir, "remote", "get-url", "origin").CombinedOutput()
	if err != nil {
		t.Fatalf("remote get-url: %v\n%s", err, out)
	}
	if strings.TrimSpace(string(out)) != remote {
		t.Errorf("remote = %q want %q", strings.TrimSpace(string(out)), remote)
	}
	st, err := gitrepo.New(home, dir, roots).Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Branch != "main" || st.Ahead != -1 {
		t.Errorf("fresh store: branch %q ahead %d", st.Branch, st.Ahead)
	}
}

func TestBrokenBinaryReturnsError(t *testing.T) {
	s := gitrepo.New(t.TempDir(), t.TempDir(), roots)
	s.Bin = "/nonexistent/git-binary"
	if _, err := s.Status(context.Background()); err == nil {
		t.Fatal("expected error from broken git binary")
	}
}
