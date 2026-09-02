package explore

import (
	"reflect"
	"testing"
	"time"

	"github.com/jegork/skillet/internal/inventory"
	"github.com/jegork/skillet/internal/skill"
	"github.com/jegork/skillet/internal/upstream"
)

func cache() upstream.Cache {
	return upstream.Cache{Version: 1, Repos: map[string]upstream.Repo{
		"acme/skills": {
			FetchedAt: time.Now(),
			Trees:     map[string]string{"skills/animate": "a", "skills/.curated/x": "b", ".": "c"},
			Skills:    []string{".", "skills/.curated/x", "skills/animate", "skills/unused"},
		},
		"other/repo": {FetchedAt: time.Now(), Skills: []string{"skills/their"}},
	}}
}

func inv(home, project map[string]skill.LockEntry) inventory.Inventory {
	out := inventory.Inventory{Lock: skill.Lock{Version: 3, Skills: home}}
	if project != nil {
		out.Projects = []inventory.Project{{Root: "/proj", Lock: skill.ProjectLock{Version: 1, Skills: project}}}
	}
	return out
}

func entry(source string) skill.LockEntry {
	return skill.LockEntry{Source: source, SourceType: "github", SkillPath: "skills/x/SKILL.md"}
}

func TestListMarksInstalledAndAvailable(t *testing.T) {
	l := inv(map[string]skill.LockEntry{
		"animate": entry("acme/skills"),
	}, nil)
	got := List(cache(), l)
	want := []Skill{
		{Vendor: "acme/skills", Path: ".", Name: "skills"},
		{Vendor: "acme/skills", Path: "skills/.curated/x", Name: "x"},
		{Vendor: "acme/skills", Path: "skills/animate", Name: "animate", Installed: true},
		{Vendor: "acme/skills", Path: "skills/unused", Name: "unused"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("List = %+v, want %+v", got, want)
	}
}

// a root-level SKILL.md installs the whole repo, so the folder gets the
// repo's base name
func TestListNamesRootFolderAfterRepo(t *testing.T) {
	l := inv(map[string]skill.LockEntry{"deep": entry("vendor/deep-digger")}, nil)
	c := upstream.Cache{Version: 1, Repos: map[string]upstream.Repo{
		"vendor/deep-digger": {FetchedAt: time.Now(), Skills: []string{"."}},
	}}
	got := List(c, l)
	want := []Skill{{Vendor: "vendor/deep-digger", Path: ".", Name: "deep-digger"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("List = %+v, want %+v", got, want)
	}
}

// project locks contribute vendors and installed names too; vendors without
// cache data (never fetched) stay out of the listing
func TestListMergesProjectLocks(t *testing.T) {
	l := inv(map[string]skill.LockEntry{
		"animate": entry("acme/skills"),
	}, map[string]skill.LockEntry{
		"their":   entry("other/repo"),
		"missing": entry("never/fetched"),
	})
	got := List(cache(), l)
	want := []Skill{
		{Vendor: "acme/skills", Path: ".", Name: "skills"},
		{Vendor: "acme/skills", Path: "skills/.curated/x", Name: "x"},
		{Vendor: "acme/skills", Path: "skills/animate", Name: "animate", Installed: true},
		{Vendor: "acme/skills", Path: "skills/unused", Name: "unused"},
		{Vendor: "other/repo", Path: "skills/their", Name: "their", Installed: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("List = %+v, want %+v", got, want)
	}
}

func TestListEmptyWithoutCacheOrLocks(t *testing.T) {
	if got := List(upstream.Cache{}, inv(nil, nil)); got != nil {
		t.Errorf("empty input: List = %+v", got)
	}
	// a vendor that is locked but has no cached tree yields no rows
	l := inv(map[string]skill.LockEntry{"x": entry("ghost/repo")}, nil)
	if got := List(cache(), l); got != nil {
		t.Errorf("uncached vendor: List = %+v", got)
	}
}

func TestLocksMergesHomeAndProjectLocks(t *testing.T) {
	l := inv(map[string]skill.LockEntry{"home": entry("acme/skills")},
		map[string]skill.LockEntry{"proj": entry("other/repo")})
	merged := Locks(l)
	if len(merged.Skills) != 2 {
		t.Fatalf("merged skills = %+v", merged.Skills)
	}
	if merged.Skills["home"].Source != "acme/skills" || merged.Skills["proj"].Source != "other/repo" {
		t.Errorf("merged skills = %+v", merged.Skills)
	}
	// the home lock must not be mutated
	if _, ok := l.Lock.Skills["proj"]; ok {
		t.Error("Locks mutated the home lock")
	}
}

func TestLocksWithoutLocks(t *testing.T) {
	l := inv(nil, nil)
	merged := Locks(l)
	if merged.Skills == nil {
		t.Error("merged skills map must not be nil: Refresh iterates it")
	}
}
