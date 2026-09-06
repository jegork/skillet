// Package explore lists the skills the locked vendors ship, read from the
// upstream cache: what the trusted repos offer beyond what is installed.
package explore

import (
	"maps"
	"path"
	"sort"
	"strings"

	"github.com/jegork/skillet/internal/inventory"
	"github.com/jegork/skillet/internal/skill"
	"github.com/jegork/skillet/internal/upstream"
)

// Skill is one skill folder found in a vendor's tree.
type Skill struct {
	Vendor    string // owner/repo
	Path      string // skill folder inside the vendor repo
	Name      string // folder name; the repo name for a root-level SKILL.md
	Installed bool
}

// List merges the vendors of the home lock and every scanned project lock
// with the skill folders the upstream cache recorded for them. Vendors the
// cache knows nothing about are left out; refresh first for the full view.
func List(c upstream.Cache, inv inventory.Inventory) []Skill {
	installed := map[string]map[string]bool{}
	var vendors []string
	add := func(skills map[string]skill.LockEntry) {
		for name, e := range skills {
			if !strings.Contains(e.Source, "/") {
				continue
			}
			if installed[e.Source] == nil {
				installed[e.Source] = map[string]bool{}
				vendors = append(vendors, e.Source)
			}
			installed[e.Source][name] = true
		}
	}
	add(inv.Lock.Skills)
	for _, p := range inv.Projects {
		add(p.Lock.Skills)
	}
	sort.Strings(vendors)
	var out []Skill
	for _, v := range vendors {
		for _, folder := range c.Repos[v].Skills {
			name := path.Base(folder)
			if folder == "." {
				name = path.Base(v)
			}
			out = append(out, Skill{Vendor: v, Path: folder, Name: name, Installed: installed[v][name]})
		}
	}
	return out
}

// Locks merges the home lock with every scanned project lock, so an
// upstream refresh covers the project vendors too.
func Locks(inv inventory.Inventory) skill.Lock {
	merged := inv.Lock
	merged.Skills = maps.Clone(inv.Lock.Skills)
	if merged.Skills == nil {
		merged.Skills = map[string]skill.LockEntry{}
	}
	for _, p := range inv.Projects {
		for name, e := range p.Lock.Skills {
			merged.Skills[name] = e
		}
	}
	return merged
}
