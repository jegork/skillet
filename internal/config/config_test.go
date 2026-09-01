package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jegork/skillet/internal/testhome"
)

// write creates the config file with exact content.
func write(t *testing.T, h *testhome.Home, content string) {
	t.Helper()
	path := Path(h.Dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPathDefault(t *testing.T) {
	h := testhome.New(t)
	want := filepath.Join(h.Dir, ".config", "skillet", "config.yml")
	if got := Path(h.Dir); got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
}

func TestPathRespectsXDGConfigHome(t *testing.T) {
	h := testhome.New(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h.Dir, "cfg"))
	want := filepath.Join(h.Dir, "cfg", "skillet", "config.yml")
	if got := Path(h.Dir); got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
}

func TestLoadMissingFile(t *testing.T) {
	h := testhome.New(t)
	cfg, err := Load(h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Store != "" || cfg.GitStore.Dir != "" || cfg.GitStore.Remote != "" ||
		len(cfg.Projects.Roots) != 0 || len(cfg.Projects.Paths) != 0 {
		t.Errorf("missing file must load as zero config, got %+v", cfg)
	}
}

func TestLoadParsesKeys(t *testing.T) {
	h := testhome.New(t)
	write(t, h, `# machine settings
store: git            # or chezmoi
git_store:
  dir: ~/.agents/.skillet-store.git
  remote: git@github.com:you/skills-store.git
projects:
  roots: [~/Documents/projects, ~/orca/workspaces/*]   # children are probed
  paths: []
`)
	cfg, err := Load(h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Store != "git" {
		t.Errorf("store = %q", cfg.Store)
	}
	if cfg.GitStore.Dir != filepath.Join(h.Dir, ".agents", ".skillet-store.git") {
		t.Errorf("git_store.dir = %q, want tilde expanded", cfg.GitStore.Dir)
	}
	if cfg.GitStore.Remote != "git@github.com:you/skills-store.git" {
		t.Errorf("git_store.remote = %q", cfg.GitStore.Remote)
	}
	wantRoots := []string{
		filepath.Join(h.Dir, "Documents", "projects"),
		filepath.Join(h.Dir, "orca", "workspaces", "*"),
	}
	if len(cfg.Projects.Roots) != 2 || cfg.Projects.Roots[0] != wantRoots[0] || cfg.Projects.Roots[1] != wantRoots[1] {
		t.Errorf("projects.roots = %q", cfg.Projects.Roots)
	}
	if len(cfg.Projects.Paths) != 0 {
		t.Errorf("projects.paths = %v, want empty", cfg.Projects.Paths)
	}
}

func TestLoadLeavesAbsoluteAndUserTildeAlone(t *testing.T) {
	h := testhome.New(t)
	write(t, h,
		"git_store:\n  dir: /abs/store.git\nprojects:\n  roots: ['~other/thing', '~/mine']\n")
	cfg, err := Load(h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GitStore.Dir != "/abs/store.git" {
		t.Errorf("git_store.dir = %q", cfg.GitStore.Dir)
	}
	if cfg.Projects.Roots[0] != "~other/thing" {
		t.Errorf("~other must stay untouched, got %q", cfg.Projects.Roots[0])
	}
	if cfg.Projects.Roots[1] != filepath.Join(h.Dir, "mine") {
		t.Errorf("projects.roots[1] = %q", cfg.Projects.Roots[1])
	}
}

func TestLoadBareTilde(t *testing.T) {
	h := testhome.New(t)
	write(t, h, "git_store:\n  dir: '~'\n")
	cfg, err := Load(h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GitStore.Dir != h.Dir {
		t.Errorf("git_store.dir = %q, want %q", cfg.GitStore.Dir, h.Dir)
	}
}

func TestLoadBrokenYAML(t *testing.T) {
	h := testhome.New(t)
	write(t, h, "store: [unclosed\n")
	if _, err := Load(h.Dir); err == nil {
		t.Error("broken yaml must fail")
	}
}

func TestLoadEmptyFile(t *testing.T) {
	h := testhome.New(t)
	cfg, err := Load(h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Store != "" || cfg.GitStore.Dir != "" || cfg.GitStore.Remote != "" ||
		len(cfg.Projects.Roots) != 0 || len(cfg.Projects.Paths) != 0 {
		t.Errorf("empty file must load as zero config, got %+v", cfg)
	}
}

func TestLoadNonMappingTopLevel(t *testing.T) {
	h := testhome.New(t)
	write(t, h, "- a\n- b\n")
	if _, err := Load(h.Dir); err == nil {
		t.Error("non-mapping top level must fail")
	}
}

func TestGet(t *testing.T) {
	h := testhome.New(t)
	write(t, h,
		"store: git\nprojects:\n  roots: [~/a, ~/b]\n")
	cases := map[string][]string{
		"store":          {"git"},
		"git_store.dir":  nil, // absent nested key, absent parent too
		"projects.roots": {"~/a", "~/b"},
		"projects.paths": nil, // empty sequence
	}
	for key, want := range cases {
		got, err := Get(h.Dir, key)
		if err != nil {
			t.Fatalf("get %s: %v", key, err)
		}
		if len(got) != len(want) {
			t.Errorf("get %s = %v, want %v", key, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("get %s = %v, want %v", key, got, want)
			}
		}
	}
	if _, err := Get(h.Dir, "nope"); err == nil {
		t.Error("unknown key must error")
	}
}

func TestGetMissingFile(t *testing.T) {
	h := testhome.New(t)
	got, err := Get(h.Dir, "store")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("missing file must read empty, got %v", got)
	}
}

func TestSetCreatesFile(t *testing.T) {
	h := testhome.New(t)
	if err := Set(h.Dir, "store", []string{"git"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(Path(h.Dir))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "store: git\n" {
		t.Errorf("fresh file = %q", b)
	}
}

func TestSetCreatesNestedChain(t *testing.T) {
	h := testhome.New(t)
	if err := Set(h.Dir, "git_store.dir", []string{"~/s.git"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GitStore.Dir != filepath.Join(h.Dir, "s.git") {
		t.Errorf("git_store.dir = %q", cfg.GitStore.Dir)
	}
}

func TestSetListAndClear(t *testing.T) {
	h := testhome.New(t)
	if err := Set(h.Dir, "projects.roots", []string{"~/a", "~/b"}); err != nil {
		t.Fatal(err)
	}
	got, err := Get(h.Dir, "projects.roots")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("roots = %v, want two entries", got)
	}
	if err := Set(h.Dir, "projects.roots", nil); err != nil {
		t.Fatal(err)
	}
	got, err = Get(h.Dir, "projects.roots")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("cleared roots = %v, want empty", got)
	}
}

func TestSetKeepsComments(t *testing.T) {
	h := testhome.New(t)
	write(t, h, `# my skillet settings
store: chezmoi            # or git
git_store:
  # where the history lives
  dir: ~/.agents/.skillet-store.git
`)
	if err := Set(h.Dir, "git_store.remote", []string{"git@github.com:me/store.git"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(Path(h.Dir))
	if err != nil {
		t.Fatal(err)
	}
	out := string(b)
	for _, want := range []string{
		"# my skillet settings",
		"store: chezmoi # or git",
		"# where the history lives",
		"remote: git@github.com:me/store.git",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rewritten config lost %q:\n%s", want, out)
		}
	}
}

func TestSetOverwritesScalar(t *testing.T) {
	h := testhome.New(t)
	write(t, h, "store: chezmoi # or git\n")
	if err := Set(h.Dir, "store", []string{"git"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(Path(h.Dir))
	if !strings.Contains(string(b), "store: git # or git") {
		t.Errorf("value not replaced in place:\n%s", b)
	}
}

func TestSetValidation(t *testing.T) {
	h := testhome.New(t)
	if err := Set(h.Dir, "unknown", []string{"x"}); err == nil {
		t.Error("unknown key must error")
	}
	if err := Set(h.Dir, "store", nil); err == nil {
		t.Error("scalar without a value must error")
	}
	if err := Set(h.Dir, "store", []string{"a", "b"}); err == nil {
		t.Error("scalar with two values must error")
	}
	if err := Set(h.Dir, "store", []string{"banana"}); err == nil {
		t.Error("invalid store backend must error")
	}
	if err := Set(h.Dir, "git_store.dir", nil); err == nil {
		t.Error("nested scalar without a value must error")
	}
	if err := Set(h.Dir, "projects.roots", []string{"~/a"}); err != nil {
		t.Errorf("list with one value: %v", err)
	}
}

func TestSetOnNonMappingFile(t *testing.T) {
	h := testhome.New(t)
	write(t, h, "- a\n")
	if err := Set(h.Dir, "store", []string{"git"}); err == nil {
		t.Error("must refuse to write over a non-mapping document")
	}
}

func TestSetPreservesPerms(t *testing.T) {
	h := testhome.New(t)
	path := Path(h.Dir)
	write(t, h, "store: chezmoi\n")
	os.Chmod(path, 0o640)
	if err := Set(h.Dir, "store", []string{"git"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Errorf("perms = %v, want 0640", info.Mode().Perm())
	}
}

func TestEnsureCreatesMissing(t *testing.T) {
	h := testhome.New(t)
	if err := Ensure(h.Dir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(Path(h.Dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 0 {
		t.Errorf("ensure must create an empty file, got %q", b)
	}
	if err := Ensure(h.Dir); err != nil {
		t.Fatal(err)
	}
}
