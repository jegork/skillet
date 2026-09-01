// Package doctor finds inconsistencies between skills, their consumers and the README index.
package doctor

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/jegork/skillet/internal/consumer"
	"github.com/jegork/skillet/internal/readme"
	"github.com/jegork/skillet/internal/skill"
)

type Severity int

const (
	Info Severity = iota
	Warn
	Error
)

func (s Severity) String() string {
	switch s {
	case Info:
		return "info"
	case Warn:
		return "warn"
	case Error:
		return "error"
	}
	return fmt.Sprintf("Severity(%d)", int(s))
}

// Finding is one problem. Skill is empty for global findings.
type Finding struct {
	Check    string
	Skill    string
	Severity Severity
	Message  string
}

type Input struct {
	Paths   skill.Paths
	Skills  []skill.Skill
	Lock    skill.Lock
	Reports map[string]consumer.Report
}

func Run(in Input) []Finding {
	known := map[string]skill.Skill{}
	for _, s := range in.Skills {
		known[s.Name] = s
	}
	var out []Finding
	out = append(out, skillFiles(in.Skills)...)
	out = append(out, stubs(in.Reports)...)
	out = append(out, xrefs(in.Skills, known)...)
	out = append(out, readmeCheck(in.Paths.Readme(), in.Skills)...)
	out = append(out, lockOrphans(in.Lock, known)...)
	out = append(out, drift(in.Skills, in.Lock)...)
	return out
}

func BySkill(fs []Finding) map[string][]Finding {
	out := map[string][]Finding{}
	for _, f := range fs {
		out[f.Skill] = append(out[f.Skill], f)
	}
	return out
}

func skillFiles(skills []skill.Skill) []Finding {
	var out []Finding
	for _, s := range skills {
		switch {
		case !s.HasSkillMD:
			out = append(out, Finding{"skill-md", s.Name, Error, "no SKILL.md"})
		case s.Description == "":
			out = append(out, Finding{"description", s.Name, Warn, "frontmatter has no description"})
		}
		if s.FMName != "" && s.FMName != s.Name {
			out = append(out, Finding{"name-mismatch", s.Name, Warn, fmt.Sprintf("frontmatter name is %q", s.FMName)})
		}
	}
	return out
}

func stubs(reports map[string]consumer.Report) []Finding {
	var out []Finding
	names := make([]string, 0, len(reports))
	for n := range reports {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		for _, st := range reports[n].Stubs {
			if st.State == consumer.StubOK {
				continue
			}
			msg := n + ": " + st.State.String()
			if st.Target != "" {
				msg += " -> " + st.Target
			}
			out = append(out, Finding{"stub", st.Name, Error, msg})
		}
	}
	return out
}

var xrefRe = regexp.MustCompile("(?i)`([a-z0-9][a-z0-9._-]*)` skill\\b")

func xrefs(skills []skill.Skill, known map[string]skill.Skill) []Finding {
	var out []Finding
	for _, s := range skills {
		missing := map[string]bool{}
		for _, rel := range s.Markdown {
			b, err := os.ReadFile(filepath.Join(s.Dir, rel))
			if err != nil {
				continue
			}
			for _, m := range xrefRe.FindAllSubmatch(b, -1) {
				name := string(m[1])
				if _, ok := known[name]; !ok {
					missing[name] = true
				}
			}
		}
		if len(missing) == 0 {
			continue
		}
		names := make([]string, 0, len(missing))
		for n := range missing {
			names = append(names, n)
		}
		sort.Strings(names)
		sev := Warn
		if s.Origin.Vendored {
			sev = Info
		}
		out = append(out, Finding{"xref", s.Name, sev, "references unknown skills: " + strings.Join(names, ", ")})
	}
	return out
}

func readmeCheck(path string, skills []skill.Skill) []Finding {
	idx, err := readme.Parse(path)
	if errors.Is(err, fs.ErrNotExist) {
		return []Finding{{"readme", "", Error, "README.md index is missing"}}
	}
	if err != nil {
		return []Finding{{"readme", "", Error, err.Error()}}
	}
	listed := idx.Origins
	var out []Finding
	onDisk := map[string]bool{}
	for _, s := range skills {
		onDisk[s.Name] = true
		origin, ok := listed[s.Name]
		switch {
		case !ok:
			out = append(out, Finding{"readme", "", Warn, fmt.Sprintf("README missing `%s`", s.Name)})
		case origin != s.Origin.String():
			out = append(out, Finding{"readme", "", Warn, fmt.Sprintf("README origin for `%s` is %q, disk says %q", s.Name, origin, s.Origin.String())})
		}
	}
	names := make([]string, 0, len(listed))
	for n := range listed {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if !onDisk[n] {
			out = append(out, Finding{"readme", "", Warn, fmt.Sprintf("README lists `%s` which does not exist", n)})
		}
	}
	return out
}

// drift compares vendored folders with the git tree hash the lock recorded.
// Info only: pnpx skills rewrites some frontmatter on install, so a mismatch
// is not always a local edit.
func drift(skills []skill.Skill, lock skill.Lock) []Finding {
	var out []Finding
	for _, s := range skills {
		entry, ok := lock.Skills[s.Name]
		if !ok || len(entry.SkillFolderHash) != 40 {
			continue
		}
		hash, err := skill.TreeHash(s.Dir)
		if err != nil {
			out = append(out, Finding{"drift", s.Name, Warn, err.Error()})
			continue
		}
		if hash != entry.SkillFolderHash {
			out = append(out, Finding{"drift", s.Name, Info, "folder differs from the lock file: edited locally, or rewritten by pnpx skills on install"})
		}
	}
	return out
}

func lockOrphans(lock skill.Lock, known map[string]skill.Skill) []Finding {
	var out []Finding
	names := make([]string, 0, len(lock.Skills))
	for n := range lock.Skills {
		if _, ok := known[n]; !ok {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		out = append(out, Finding{"lock-orphan", "", Warn, fmt.Sprintf("lock entry `%s` (%s) has no folder", n, lock.Skills[n].Source)})
	}
	return out
}
