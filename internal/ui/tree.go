package ui

import (
	"sort"
	"strings"

	"charm.land/bubbles/v2/list"

	"github.com/jegork/skillet/internal/skill"
)

// groups are encoded into FilterValue with this marker so treeFilter can tell
// group rows from skill rows and match a group through any of its children.
const groupMarker = "\x00"

type skillGroup struct {
	key    string
	skills []skill.Skill
}

// groupSkills orders skills into tree groups: own first, then one group per
// vendored source, groups and children in name order.
func groupSkills(skills []skill.Skill) []skillGroup {
	var own []skill.Skill
	bySource := map[string][]skill.Skill{}
	var sources []string
	for _, s := range skills {
		if !s.Origin.Vendored {
			own = append(own, s)
			continue
		}
		if _, ok := bySource[s.Origin.Source]; !ok {
			sources = append(sources, s.Origin.Source)
		}
		bySource[s.Origin.Source] = append(bySource[s.Origin.Source], s)
	}
	sort.Strings(sources)
	groups := make([]skillGroup, 0, len(sources)+1)
	if len(own) > 0 {
		groups = append(groups, skillGroup{key: "own", skills: own})
	}
	for _, src := range sources {
		groups = append(groups, skillGroup{key: src, skills: bySource[src]})
	}
	return groups
}

type groupItem struct {
	key      string
	children []string
}

func (g groupItem) FilterValue() string {
	return groupMarker + strings.Join(g.children, groupMarker)
}

// treeFilter matches skills with the default fuzzy filter; a group matches
// when any of its skills does, so filtering never leaves empty groups behind.
func treeFilter(term string, targets []string) []list.Rank {
	var vtargets []string
	var vidx []int
	for i, t := range targets {
		for _, p := range strings.Split(strings.TrimPrefix(t, groupMarker), groupMarker) {
			vtargets = append(vtargets, p)
			vidx = append(vidx, i)
		}
	}
	seen := map[int]bool{}
	var ranks []list.Rank
	for _, r := range list.DefaultFilter(term, vtargets) {
		i := vidx[r.Index]
		if seen[i] {
			continue
		}
		seen[i] = true
		ranks = append(ranks, list.Rank{Index: i, MatchedIndexes: r.MatchedIndexes})
	}
	return ranks
}

// visibleSkills counts skill rows, leaving group rows out of the status count.
func visibleSkills(l list.Model) int {
	n := 0
	for _, it := range l.VisibleItems() {
		if _, ok := it.(item); ok {
			n++
		}
	}
	return n
}
