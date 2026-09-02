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

// Finding is one problem. Skill and Project are empty for global findings;
// Project set means the finding belongs to a project's skills.
type Finding struct {
	Check    string
	Skill    string
	Project  string
	Severity Severity
	Message  string
}

type Input struct {
	Paths    skill.Paths
	Skills   []skill.Skill
	Lock     skill.Lock
	Reports  map[string]consumer.Report
	Projects []ProjectInput
}

// ProjectInput is one project's slice of the world: its skills, project lock
// and consumer reports.
type ProjectInput struct {
	Root      string
	SkillsDir string
	Skills    []skill.Skill
	Lock      skill.ProjectLock
	Reports   map[string]consumer.Report
}

func Run(in Input) []Finding {
	known := map[string]skill.Skill{}
	for _, s := range in.Skills {
		known[s.Name] = s
	}
	var out []Finding
	out = append(out, skillFiles(in.Skills)...)
	out = append(out, stubs(in.Reports, "")...)
	out = append(out, xrefs(in.Skills, known)...)
	out = append(out, readmeCheck(in.Paths.Readme(), globalSkills(in.Skills))...)
	out = append(out, lockOrphans(in.Lock, known, "")...)
	out = append(out, drift(in.Skills, in.Lock)...)
	out = append(out, provenance(in.Skills)...)
	for _, p := range in.Projects {
		out = append(out, projectRun(in, p)...)
	}
	return out
}

func projectRun(in Input, p ProjectInput) []Finding {
	var out []Finding
	name := filepath.Base(p.Root)
	out = append(out, stubs(p.Reports, name)...)
	for i := range out {
		out[i].Project = p.Root
	}
	out = append(out, projectSkillFiles(p, name)...)
	out = append(out, shadow(in, p)...)
	out = append(out, projectLockOrphans(p, name)...)
	out = append(out, projectDrift(p, name)...)
	return out
}

func projectSkillFiles(p ProjectInput, name string) []Finding {
	var out []Finding
	for _, f := range skillFiles(p.Skills) {
		f.Project = p.Root
		f.Message = name + ": " + f.Message
		out = append(out, f)
	}
	return out
}

// shadow flags project skills whose name is also a global skill: the project
// copy hides the global one from tools that read the project.
func shadow(in Input, p ProjectInput) []Finding {
	var out []Finding
	global := map[string]bool{}
	for _, s := range globalSkills(in.Skills) {
		global[s.Name] = true
	}
	for _, s := range p.Skills {
		if global[s.Name] {
			out = append(out, Finding{"shadow", s.Name, p.Root, Error,
				"shadows the global skill of the same name"})
		}
	}
	return out
}

func projectLockOrphans(p ProjectInput, name string) []Finding {
	var out []Finding
	names := make([]string, 0, len(p.Lock.Skills))
	for n := range p.Lock.Skills {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		found := false
		for _, s := range p.Skills {
			if s.Name == n {
				found = true
				break
			}
		}
		if !found {
			out = append(out, Finding{"lock-orphan", "", p.Root, Warn,
				name + ": lock entry " + n + " (" + p.Lock.Skills[n].Source + ") has no folder"})
		}
	}
	return out
}

// projectDrift compares each project skill folder with the computedHash its
// skills-lock.json entry recorded. Info only, like the global drift check.
func projectDrift(p ProjectInput, name string) []Finding {
	var out []Finding
	if p.Lock.Missing {
		return out
	}
	for _, s := range p.Skills {
		entry, ok := p.Lock.Skills[s.Name]
		if !ok || entry.ComputedHash == "" {
			continue
		}
		hash, err := skill.ContentHash(s.Dir)
		if err != nil {
			out = append(out, Finding{"drift", s.Name, p.Root, Warn, name + ": " + err.Error()})
			continue
		}
		if hash != entry.ComputedHash {
			out = append(out, Finding{"drift", s.Name, p.Root, Info,
				name + ": folder differs from skills-lock.json computedHash: edited locally, or the lock is stale"})
		}
	}
	return out
}

// BySkill groups findings by subject skill, scoping project findings under
// their project root so a project skill and a global twin keep separate keys.
func BySkill(fs []Finding) map[string][]Finding {
	out := map[string][]Finding{}
	for _, f := range fs {
		key := f.Skill
		if f.Project != "" {
			key = f.Project + "\x00" + f.Skill
		}
		out[key] = append(out[key], f)
	}
	return out
}

func skillFiles(skills []skill.Skill) []Finding {
	var out []Finding
	for _, s := range skills {
		switch {
		case !s.HasSkillMD:
			out = append(out, Finding{"skill-md", s.Name, "", Error, "no SKILL.md"})
		case s.Description == "":
			out = append(out, Finding{"description", s.Name, "", Warn, "frontmatter has no description"})
		}
		if s.FMName != "" && s.FMName != s.Name {
			out = append(out, Finding{"name-mismatch", s.Name, "", Warn, fmt.Sprintf("frontmatter name is %q", s.FMName)})
		}
	}
	return out
}

func stubs(reports map[string]consumer.Report, project string) []Finding {
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
			msg := st.State.String()
			if st.Target != "" {
				msg += " -> " + st.Target
			}
			out = append(out, Finding{"stub", st.Name, project, Error, projectPrefix(project, msg, n)})
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
		out = append(out, Finding{"xref", s.Name, "", sev, "references unknown skills: " + strings.Join(names, ", ")})
	}
	return out
}

func readmeCheck(path string, skills []skill.Skill) []Finding {
	idx, err := readme.Parse(path)
	if errors.Is(err, fs.ErrNotExist) {
		return []Finding{{Check: "readme", Severity: Error, Message: "README.md index is missing"}}
	}
	if err != nil {
		return []Finding{{Check: "readme", Severity: Error, Message: err.Error()}}
	}
	listed := idx.Origins
	var out []Finding
	onDisk := map[string]bool{}
	for _, s := range skills {
		onDisk[s.Name] = true
		origin, ok := listed[s.Name]
		switch {
		case !ok:
			out = append(out, Finding{"readme", "", "", Warn, fmt.Sprintf("README missing `%s`", s.Name)})
		case origin != s.Origin.String():
			out = append(out, Finding{"readme", "", "", Warn, fmt.Sprintf("README origin for `%s` is %q, disk says %q", s.Name, origin, s.Origin.String())})
		}
	}
	names := make([]string, 0, len(listed))
	for n := range listed {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if !onDisk[n] {
			out = append(out, Finding{"readme", "", "", Warn, fmt.Sprintf("README lists `%s` which does not exist", n)})
		}
	}
	return out
}

// provenance flags own skills whose frontmatter carries vendor markers: they
// were most likely installed before the lock file tracked them.
func provenance(skills []skill.Skill) []Finding {
	var out []Finding
	for _, s := range globalSkills(skills) {
		if s.Origin.Vendored || (s.Author == "" && s.License == "") {
			continue
		}
		var markers []string
		if s.Author != "" {
			markers = append(markers, "author "+s.Author)
		}
		if s.License != "" {
			markers = append(markers, "license "+s.License)
		}
		out = append(out, Finding{"provenance", s.Name, "", Warn, "looks vendored (" + strings.Join(markers, ", ") + ") but is not in the lock file; adopt it with pnpx skills add"})
	}
	return out
}

// drift compares vendored folders with the git tree hash the lock recorded.
// Info only: pnpx skills rewrites some frontmatter on install, so a mismatch
// is not always a local edit.
func drift(skills []skill.Skill, lock skill.Lock) []Finding {
	var out []Finding
	for _, s := range globalSkills(skills) {
		entry, ok := lock.Skills[s.Name]
		if !ok || len(entry.SkillFolderHash) != 40 {
			continue
		}
		hash, err := skill.TreeHash(s.Dir)
		if err != nil {
			out = append(out, Finding{"drift", s.Name, "", Warn, err.Error()})
			continue
		}
		if hash != entry.SkillFolderHash {
			out = append(out, Finding{"drift", s.Name, "", Info, "folder differs from the lock file: edited locally, or rewritten by pnpx skills on install"})
		}
	}
	return out
}

func lockOrphans(lock skill.Lock, known map[string]skill.Skill, project string) []Finding {
	var out []Finding
	names := make([]string, 0, len(lock.Skills))
	for n := range lock.Skills {
		if _, ok := known[n]; !ok {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		out = append(out, Finding{"lock-orphan", "", "", Warn, fmt.Sprintf("lock entry `%s` (%s) has no folder", n, lock.Skills[n].Source)})
	}
	return out
}

// projectPrefix builds "<project>: <consumer>: <state>" style messages.
func projectPrefix(project, msg, consumer string) string {
	if project == "" {
		return consumer + ": " + msg
	}
	return project + " " + consumer + ": " + msg
}

// globalSkills filters to the home skills; project skills never take part
// in the global README, lock or provenance checks.
func globalSkills(skills []skill.Skill) []skill.Skill {
	var out []skill.Skill
	for _, s := range skills {
		if s.Scope == "" {
			out = append(out, s)
		}
	}
	return out
}
