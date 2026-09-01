// Package inventory loads everything skillet knows about the skills on this machine.
package inventory

import (
	"fmt"
	"path/filepath"

	"github.com/jegork/skillet/internal/consumer"
	"github.com/jegork/skillet/internal/doctor"
	"github.com/jegork/skillet/internal/skill"
)

type Inventory struct {
	Paths     skill.Paths
	Lock      skill.Lock
	Skills    []skill.Skill
	Consumers []string
	Reports   map[string]consumer.Report
	Findings  []doctor.Finding
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
	for _, c := range Consumers(home) {
		rep, err := c.Report(inv.Skills)
		if err != nil {
			return inv, fmt.Errorf("%s: %w", c.Name(), err)
		}
		inv.Consumers = append(inv.Consumers, c.Name())
		inv.Reports[c.Name()] = rep
	}
	inv.Findings = doctor.Run(doctor.Input{Paths: inv.Paths, Skills: inv.Skills, Lock: inv.Lock, Reports: inv.Reports})
	return inv, nil
}
