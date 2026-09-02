package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jegork/skillet/internal/config"
	"github.com/jegork/skillet/internal/skill"
	"github.com/jegork/skillet/internal/testhome"
	"github.com/jegork/skillet/internal/upstream"
)

// captureStdout reroutes os.Stdout while fn runs.
func captureStdout(t *testing.T, fn func()) *bytes.Buffer {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()
	fn()
	w.Close()
	var buf bytes.Buffer
	buf.ReadFrom(r)
	return &buf
}

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

type fakeFetcher struct {
	trees map[string][]upstream.Entry
	fail  map[string]error
}

func (f fakeFetcher) Tree(ctx context.Context, owner, repo string) ([]upstream.Entry, error) {
	if err := f.fail[owner+"/"+repo]; err != nil {
		return nil, err
	}
	return f.trees[owner+"/"+repo], nil
}

func outdatedHome(t *testing.T) *testhome.Home {
	h := testhome.New(t)
	h.Skill("vend", "vendored one")
	h.Skill("own", "own one")
	stale, err := skill.TreeHash(h.SkillsDir() + "/vend")
	if err != nil {
		t.Fatal(err)
	}
	h.LockWithHashes(map[string]string{"vend": "acme/skills"}, map[string]string{"vend": stale})
	h.Readme("| `vend` | vendored (acme/skills) | vendored one |", "| `own` | own | own one |")
	return h
}

func TestRunOutdatedPrintsStaleSkills(t *testing.T) {
	h := outdatedHome(t)
	f := fakeFetcher{trees: map[string][]upstream.Entry{
		"acme/skills": {{Path: "skills/vend", Type: "tree", SHA: "2222222222222222222222222222222222222222"}},
	}}
	out := captureStdout(t, func() {
		if err := runOutdated(h.Dir, f); err != nil {
			t.Fatal(err)
		}
	})
	if got := out.String(); !strings.Contains(got, "vend\tacme/skills") {
		t.Errorf("output %q", got)
	}
	// the fetch wrote the cache, so a second run reuses it
	out2 := captureStdout(t, func() {
		if err := runOutdated(h.Dir, fakeFetcher{fail: map[string]error{"acme/skills": errors.New("rate limited")}}); err != nil {
			t.Fatal(err)
		}
	})
	if got, out2 := out.String(), out2.String(); got != out2 {
		t.Errorf("fresh %q vs cached %q", got, out2)
	}
}

func TestRunOutdatedQuietWhenUpToDateOrOffline(t *testing.T) {
	h := outdatedHome(t)
	stale, _ := skill.TreeHash(h.SkillsDir() + "/vend")
	upToDate := fakeFetcher{trees: map[string][]upstream.Entry{
		"acme/skills": {{Path: "skills/vend", Type: "tree", SHA: stale}},
	}}
	out := captureStdout(t, func() {
		if err := runOutdated(h.Dir, upToDate); err != nil {
			t.Fatal(err)
		}
	})
	if out.Len() != 0 {
		t.Errorf("up-to-date home printed %q", out.String())
	}
	offline := fakeFetcher{fail: map[string]error{"acme/skills": errors.New("offline")}}
	out = captureStdout(t, func() {
		if err := runOutdated(h.Dir, offline); err != nil {
			t.Fatal(err)
		}
	})
	if out.Len() != 0 {
		t.Errorf("offline home printed %q", out.String())
	}
}

func exploreHome(t *testing.T) *testhome.Home {
	h := testhome.New(t)
	h.Skill("vend", "vendored one")
	h.Lock(map[string]string{"vend": "acme/skills"})
	h.Readme("| `vend` | vendored (acme/skills) | vendored one |")
	return h
}

func TestRunExplorePrintsVendors(t *testing.T) {
	h := exploreHome(t)
	f := fakeFetcher{trees: map[string][]upstream.Entry{
		"acme/skills": {
			{Path: "skills/vend", Type: "tree", SHA: "2222222222222222222222222222222222222222"},
			{Path: "skills/vend/SKILL.md", Type: "blob", SHA: "a1"},
			{Path: "skills/animate/SKILL.md", Type: "blob", SHA: "a2"},
			{Path: "skills/.curated/x/SKILL.md", Type: "blob", SHA: "a3"},
		},
	}}
	out := captureStdout(t, func() {
		if err := runExplore(h.Dir, nil, f); err != nil {
			t.Fatal(err)
		}
	})
	got := out.String()
	for _, want := range []string{
		"vend\tacme/skills\tinstalled\n",
		"animate\tacme/skills\tavailable\n",
		"x\tacme/skills\tavailable\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q: %q", want, got)
		}
	}
	// offline with a cache still lists from the cache: refresh keeps the old entry
	out2 := captureStdout(t, func() {
		offline := fakeFetcher{fail: map[string]error{"acme/skills": errors.New("offline")}}
		if err := runExplore(h.Dir, nil, offline); err != nil {
			t.Fatal(err)
		}
	})
	if out2.String() != got {
		t.Errorf("cached run %q vs fresh %q", out2.String(), got)
	}
	// narrowing to one vendor
	out3 := captureStdout(t, func() {
		if err := runExplore(h.Dir, []string{"other/repo"}, f); err != nil {
			t.Fatal(err)
		}
	})
	if out3.Len() != 0 {
		t.Errorf("vendor filter printed %q", out3.String())
	}
}
