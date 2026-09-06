// Package inventory loads everything skillet knows about the skills on this machine.
package inventory

import (
	"fmt"
	"path/filepath"

	"github.com/jegork/skillet/internal/config"
	"github.com/jegork/skillet/internal/consumer"
	"github.com/jegork/skillet/internal/doctor"
	"github.com/jegork/skillet/internal/project"
	"github.com/jegork/skillet/internal/skill"
	"github.com/jegork/skillet/internal/upstream"
)

type Inventory struct {
	Paths     skill.Paths
	Lock      skill.Lock
	Skills    []skill.Skill
	Consumers []string
	Reports   map[string]consumer.Report
	Projects  []Project
	Upstream  map[string]upstream.Info // nil when the cache has no data yet
	Findings  []doctor.Finding
}

// Project is one discovered project's skills, with reports keyed by the
// plain consumer name.
type Project struct {
	Root      string
	SkillsDir string
	Skills    []skill.Skill
	Lock      skill.ProjectLock
	Consumers []consumer.Consumer
	Reports   map[string]consumer.Report
}

func Consumers(home string) []consumer.Consumer {
	skillsDir := skill.Paths{Home: home}.SkillsDir()
	return []consumer.Consumer{
		consumer.NewSymlinkDir("claude", filepath.Join(home, ".claude", "skills"), skillsDir),
		consumer.NewSymlinkDir("codex", filepath.Join(home, ".codex", "skills"), skillsDir),
		consumer.NewOmp(filepath.Join(home, ".omp", "agent", "config.yml")),
	}
}

func Load(home string) (Inventory, error) {
	inv := Inventory{Paths: skill.Paths{Home: home}, Reports: map[string]consumer.Report{}}
	var err error
	if inv.Lock, err = skill.ReadLock(inv.Paths.LockFile()); err != nil {
		return inv, err
	}
	if inv.Skills, err = skill.Scan(inv.Paths.SkillsDir(), inv.Lock); err != nil {
		return inv, err
	}
	// local cache read only: the network check runs separately in the background
	inv.Upstream = upstream.Evaluate(inv.Lock, upstream.ReadCache(upstream.Path(home)))

	for _, c := range Consumers(home) {
		rep, err := c.Report(inv.Skills)
		if err != nil {
			return inv, fmt.Errorf("%s: %w", c.Name(), err)
		}
		inv.Consumers = append(inv.Consumers, c.Name())
		inv.Reports[c.Name()] = rep
	}

	cfg, err := config.Load(home)
	if err != nil {
		return inv, err
	}
	projects, err := project.Discover(home, cfg)
	if err != nil {
		return inv, err
	}
	var projectInputs []doctor.ProjectInput
	for _, p := range projects {
		pj, err := loadProject(p)
		if err != nil {
			return inv, err
		}
		inv.Projects = append(inv.Projects, pj)
		input := doctor.ProjectInput{
			Root: pj.Root, SkillsDir: pj.SkillsDir, Skills: pj.Skills, Lock: p.Lock,
			Reports: map[string]consumer.Report{},
		}
		for name, rep := range pj.Reports {
			input.Reports[name] = rep
		}
		projectInputs = append(projectInputs, input)
		inv.Skills = append(inv.Skills, pj.Skills...)
	}
	inv.Findings = doctor.Run(doctor.Input{Paths: inv.Paths, Skills: inv.Skills, Lock: inv.Lock, Reports: inv.Reports, Upstream: inv.Upstream, Projects: projectInputs})
	return inv, nil
}

func loadProject(p project.Project) (Project, error) {
	out := Project{Root: p.Root, SkillsDir: p.SkillsDir(), Reports: map[string]consumer.Report{}}
	lock, err := skill.ReadProjectLock(p.LockFile())
	if err != nil {
		return out, fmt.Errorf("%s: %w", p.Root, err)
	}
	out.Lock = lock
	if out.Skills, err = skill.Scan(out.SkillsDir, skill.Lock{}); err != nil {
		return out, err
	}
	for i := range out.Skills {
		out.Skills[i].Scope = p.Root
		if entry, ok := lock.Skills[out.Skills[i].Name]; ok {
			out.Skills[i].Origin = skill.Origin{Vendored: true, Source: entry.Source}
		}
	}
	for _, c := range p.Consumers() {
		out.Consumers = append(out.Consumers, c)
		rep, err := c.Report(out.Skills)
		if err != nil {
			return out, fmt.Errorf("%s %s: %w", filepath.Base(p.Root), c.Name(), err)
		}
		out.Reports[c.Name()] = rep
	}
	return out, nil
}
