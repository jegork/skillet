package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jegork/skillet/internal/config"
	"github.com/jegork/skillet/internal/testhome"
)

func TestStoreInitWritesConfigBack(t *testing.T) {
	h := testhome.New(t)
	cfg := config.Config{}
	if err := runStore(h.Dir, cfg, []string{"init", "--remote", "git@github.com:me/store.git"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GitStore.Dir != filepath.Join(h.Dir, ".agents", ".skillet-store.git") {
		t.Errorf("git_store.dir = %q", cfg.GitStore.Dir)
	}
	if cfg.GitStore.Remote != "git@github.com:me/store.git" {
		t.Errorf("git_store.remote = %q", cfg.GitStore.Remote)
	}
}

func TestStoreInitKeepsConfigComments(t *testing.T) {
	h := testhome.New(t)
	path := config.Path(h.Dir)
	os.MkdirAll(filepath.Dir(path), 0o700)
	os.WriteFile(path, []byte("# my settings\ngit_store:\n  dir: ~/.store.git # custom\n"), 0o600)
	cfg, err := config.Load(h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := runStore(h.Dir, cfg, []string{"init", "--remote", "git@github.com:me/store.git"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	out := string(b)
	for _, want := range []string{
		"# my settings",
		"# custom",
		"remote: git@github.com:me/store.git",
		"dir: " + filepath.Join(h.Dir, ".store.git"),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("config lost %q:\n%s", want, out)
		}
	}
}

func TestRunConfigSetAndThenPrecedence(t *testing.T) {
	h := testhome.New(t)
	if err := runConfig(h.Dir, []string{"set", "store", "git"}); err != nil {
		t.Fatal(err)
	}
	if err := runConfig(h.Dir, []string{"set", "projects.roots", "~/a", "~/b"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Store != "git" {
		t.Errorf("store = %q", cfg.Store)
	}
	if len(cfg.Projects.Roots) != 2 || cfg.Projects.Roots[0] != filepath.Join(h.Dir, "a") {
		t.Errorf("projects.roots = %q", cfg.Projects.Roots)
	}
	// a bad store value must fail loudly
	if err := runConfig(h.Dir, []string{"set", "store", "banana"}); err == nil {
		t.Error("store banana must be rejected")
	}
	if err := runConfig(h.Dir, []string{"get", "store"}); err != nil {
		t.Errorf("get store: %v", err)
	}
	if err := runConfig(h.Dir, []string{"get", "nonsense"}); err == nil {
		t.Error("get nonsense must fail")
	}
}

func TestRunConfigSetKeepsComments(t *testing.T) {
	h := testhome.New(t)
	path := config.Path(h.Dir)
	os.MkdirAll(filepath.Dir(path), 0o700)
	os.WriteFile(path, []byte("# machine\nstore: chezmoi # or git\n"), 0o600)
	if err := runConfig(h.Dir, []string{"set", "store", "git"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	out := string(b)
	for _, want := range []string{"# machine", "store: git # or git"} {
		if !strings.Contains(out, want) {
			t.Errorf("config lost %q:\n%s", want, out)
		}
	}
}
