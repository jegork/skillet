package upstream

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jegork/skillet/internal/skill"
)

const (
	lockSHA  = "1111111111111111111111111111111111111111"
	upSHA    = "2222222222222222222222222222222222222222"
	otherSHA = "3333333333333333333333333333333333333333"
)

// fakeFetcher replaces the network; it counts calls so tests can see what
// Refresh actually fetched.
type fakeFetcher struct {
	trees map[string][]Entry // "owner/repo" -> entries
	fail  map[string]error
	calls map[string]int
}

func newFakeFetcher() *fakeFetcher {
	return &fakeFetcher{
		trees: map[string][]Entry{},
		fail:  map[string]error{},
		calls: map[string]int{},
	}
}

func (f *fakeFetcher) Tree(ctx context.Context, owner, repo string) ([]Entry, error) {
	key := owner + "/" + repo
	f.calls[key]++
	if err := f.fail[key]; err != nil {
		return nil, err
	}
	if ents, ok := f.trees[key]; ok {
		return ents, nil
	}
	return nil, errors.New("no fixture for " + key)
}

func lockWith(entries map[string]skill.LockEntry) skill.Lock {
	return skill.Lock{Version: 3, Skills: entries}
}

func entry(source, skillPath, hash string) skill.LockEntry {
	return skill.LockEntry{
		Source:          source,
		SourceType:      "github",
		SkillPath:       skillPath,
		SkillFolderHash: hash,
	}
}

func TestParseTreeFixture(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "tree.json"))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := ParseTree(b)
	if err != nil {
		t.Fatal(err)
	}
	if entries[0].Path != "." || entries[0].SHA != "d23d7f88a2e21c9e4b1418c7abe420f5c1052ba7" {
		t.Errorf("root entry = %+v", entries[0])
	}
	var sawBlob int
	var folder Entry
	for _, e := range entries {
		if e.Type == "blob" {
			sawBlob++
		}
		if e.Path == "skills/animate-expo" {
			folder = e
		}
	}
	if folder.SHA != "2daf5ab8f4d3db7690e46d3d6d63e7ebae89a090" {
		t.Errorf("skills/animate-expo = %+v", folder)
	}
	if sawBlob == 0 {
		t.Error("fixture has no blobs, parse is too narrow")
	}
}

func TestParseTreeTruncated(t *testing.T) {
	for name, b := range map[string][]byte{
		"truncated": []byte(`{"sha":"abc","truncated":true,"tree":[]}`),
		"garbage":   []byte("not json"),
	} {
		if _, err := ParseTree(b); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

// upstream repo: root "." plus skills/one with two files, skills/two empty.
func oneRepo() *fakeFetcher {
	f := newFakeFetcher()
	f.trees["acme/one"] = []Entry{
		{Path: "skills", Type: "tree", SHA: otherSHA},
		{Path: "skills/one", Type: "tree", SHA: upSHA},
		{Path: "skills/one/SKILL.md", Type: "blob", SHA: "a1"},
		{Path: "skills/two", Type: "tree", SHA: otherSHA},
	}
	return f
}

func TestRefreshAndEvaluate(t *testing.T) {
	h := t.TempDir()
	p := Path(h)
	lock := lockWith(map[string]skill.LockEntry{
		"one": entry("acme/one", "skills/one/SKILL.md", lockSHA),
	})
	f := oneRepo()
	if err := Refresh(t.Context(), p, lock, f, false); err != nil {
		t.Fatal(err)
	}
	if f.calls["acme/one"] != 1 {
		t.Fatalf("calls = %d", f.calls["acme/one"])
	}
	c := ReadCache(p)
	infos := Evaluate(lock, c)
	info := infos["one"]
	if info.State != Outdated || info.Upstream != upSHA || info.Lock != lockSHA {
		t.Errorf("info = %+v", info)
	}
	// a second refresh inside the TTL must not hit the network again
	if err := Refresh(t.Context(), p, lock, f, false); err != nil {
		t.Fatal(err)
	}
	if f.calls["acme/one"] != 1 {
		t.Errorf("fresh cache was refetched: %d calls", f.calls["acme/one"])
	}
}

func TestRefreshForceIgnoresTTL(t *testing.T) {
	h := t.TempDir()
	p := Path(h)
	lock := lockWith(map[string]skill.LockEntry{"one": entry("acme/one", "skills/one/SKILL.md", lockSHA)})
	f := oneRepo()
	if err := Refresh(t.Context(), p, lock, f, false); err != nil {
		t.Fatal(err)
	}
	if err := Refresh(t.Context(), p, lock, f, true); err != nil {
		t.Fatal(err)
	}
	if f.calls["acme/one"] != 2 {
		t.Errorf("force ignored: %d calls", f.calls["acme/one"])
	}
}

func TestRefreshExpiresAfterTTL(t *testing.T) {
	h := t.TempDir()
	p := Path(h)
	lock := lockWith(map[string]skill.LockEntry{"one": entry("acme/one", "skills/one/SKILL.md", lockSHA)})
	f := oneRepo()
	if err := Refresh(t.Context(), p, lock, f, false); err != nil {
		t.Fatal(err)
	}
	c := ReadCache(p)
	rc := c.Repos["acme/one"]
	rc.FetchedAt = time.Now().Add(-TTL - time.Minute)
	c.Repos["acme/one"] = rc
	if err := WriteCache(p, c); err != nil {
		t.Fatal(err)
	}
	if err := Refresh(t.Context(), p, lock, f, false); err != nil {
		t.Fatal(err)
	}
	if f.calls["acme/one"] != 2 {
		t.Errorf("stale cache was kept: %d calls", f.calls["acme/one"])
	}
}

func TestRefreshKeepsCacheWhenFetchFails(t *testing.T) {
	h := t.TempDir()
	p := Path(h)
	lock := lockWith(map[string]skill.LockEntry{
		"one":  entry("acme/one", "skills/one/SKILL.md", lockSHA),
		"down": entry("acme/down", "skills/down/SKILL.md", lockSHA),
	})
	// first pass succeeds for both, second pass fails for acme/down
	f := oneRepo()
	f.trees["acme/down"] = []Entry{{Path: "skills/down", Type: "tree", SHA: upSHA}}
	if err := Refresh(t.Context(), p, lock, f, false); err != nil {
		t.Fatal(err)
	}
	c := ReadCache(p)
	rc := c.Repos["acme/down"]
	rc.FetchedAt = time.Now().Add(-2 * time.Hour)
	c.Repos["acme/down"] = rc
	if err := WriteCache(p, c); err != nil {
		t.Fatal(err)
	}
	f.fail["acme/down"] = errors.New("rate limited")
	if err := Refresh(t.Context(), p, lock, f, false); err != nil {
		t.Fatal(err)
	}
	if got := c.Repos["acme/down"].Trees["skills/down"]; got != upSHA {
		t.Errorf("old entry dropped: %q", got)
	}
	if time.Since(c.Repos["acme/down"].FetchedAt) < time.Hour {
		t.Error("old entry was refreshed despite the failure")
	}
	// the untouched repo still got refreshed... and evaluate sees both
	infos := Evaluate(lock, c)
	if infos["one"].State != Outdated {
		t.Errorf("one = %+v", infos["one"])
	}
	if infos["down"].State != Outdated {
		t.Errorf("down = %+v", infos["down"])
	}
}

func TestEvaluateStates(t *testing.T) {
	lock := lockWith(map[string]skill.LockEntry{
		"current":  entry("acme/one", "skills/current/SKILL.md", upSHA),
		"outdated": entry("acme/one", "skills/one/SKILL.md", lockSHA),
		"nocache":  entry("acme/none", "skills/nocache/SKILL.md", lockSHA),
		"nopath":   entry("acme/one", "skills/gone/SKILL.md", lockSHA),
		"nohash":   entry("acme/one", "skills/one/SKILL.md", ""),
		"root":     entry("acme/one", "SKILL.md", upSHA),
		"nonrepo":  entry("local-folder", "skills/one/SKILL.md", lockSHA),
	})
	c := Cache{Version: cacheVersion, Repos: map[string]Repo{
		"acme/one": {FetchedAt: time.Now(), Trees: map[string]string{
			".":              upSHA,
			"skills/one":     upSHA,
			"skills/current": upSHA,
		}},
	}}
	infos := Evaluate(lock, c)
	cases := map[string]struct {
		state State
		up    string
	}{
		"current":  {Current, ""},
		"outdated": {Outdated, upSHA},
		"nocache":  {Unknown, ""},
		"nopath":   {Unknown, ""},
		"nohash":   {Unknown, ""},
		"root":     {Current, ""},
		"nonrepo":  {Unknown, ""},
	}
	for name, want := range cases {
		if got := infos[name]; got.State != want.state || got.Upstream != want.up {
			t.Errorf("%s = %+v, want state %d upstream %q", name, got, want.state, want.up)
		}
	}
	if _, ok := infos["missing"]; ok {
		t.Error("skills outside the lock must not appear")
	}
}

func TestPathRespectsXDGCacheHome(t *testing.T) {
	real, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no user home")
	}
	xdg := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", xdg)
	if got, want := Path(real), filepath.Join(xdg, "skillet", "upstream.json"); got != want {
		t.Errorf("real home: Path = %q, want %q", got, want)
	}
	// any other home keeps its own .cache, so --home and temp homes are never
	// redirected to the user's cache
	other := t.TempDir()
	if got, want := Path(other), filepath.Join(other, ".cache", "skillet", "upstream.json"); got != want {
		t.Errorf("other home: Path = %q, want %q", got, want)
	}
}

func TestReadCacheDegrades(t *testing.T) {
	h := t.TempDir()
	p := Path(h)
	if c := ReadCache(p); c.Repos != nil {
		t.Error("missing file must read as an empty cache")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("{bogus"), 0o644); err != nil {
		t.Fatal(err)
	}
	if c := ReadCache(p); c.Repos != nil {
		t.Error("corrupt file must read as an empty cache")
	}
	if err := os.WriteFile(p, []byte(`{"version":99,"repos":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if c := ReadCache(p); c.Repos != nil {
		t.Error("wrong version must read as an empty cache")
	}
}

// the fixture carries SKILL.md blobs at several depths: repo root, the
// usual skills/ folder, a nested skills/.curated/x path and a deep one
func TestRefreshRecordsSkillFolders(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "tree.json"))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := ParseTree(b)
	if err != nil {
		t.Fatal(err)
	}
	f := newFakeFetcher()
	f.trees["emil/skills"] = entries
	h := t.TempDir()
	lock := lockWith(map[string]skill.LockEntry{
		"animate": entry("emil/skills", "skills/animate/SKILL.md", lockSHA),
	})
	if err := Refresh(t.Context(), Path(h), lock, f, false); err != nil {
		t.Fatal(err)
	}
	got := ReadCache(Path(h)).Repos["emil/skills"].Skills
	want := []string{
		".",
		"skills/.curated/x",
		"skills/animate",
		"skills/animate-expo",
		"skills/animation-vocabulary",
		"skills/apple-design",
		"skills/ask-sonner",
		"skills/emil-design-eng",
		"skills/find-animation-opportunities",
		"skills/improve-animations",
		"skills/pick-ui-library",
		"skills/prototype",
		"skills/review-animations",
		"skills/write-swift",
		"tools/deep/digger",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Skills = %v, want %v", got, want)
	}
}

func TestCacheRoundTripKeepsSkillFolders(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cache", "upstream.json")
	c := Cache{Version: cacheVersion, Repos: map[string]Repo{
		"acme/one": {FetchedAt: time.Now(), Trees: map[string]string{"skills/one": upSHA}, Skills: []string{".", "skills/one"}},
	}}
	if err := WriteCache(p, c); err != nil {
		t.Fatal(err)
	}
	got := ReadCache(p).Repos["acme/one"].Skills
	if !reflect.DeepEqual(got, []string{".", "skills/one"}) {
		t.Errorf("Skills after round trip = %v", got)
	}
}
