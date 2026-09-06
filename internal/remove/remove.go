// Package remove deletes a skill from its scope and cleans up everything
// that pointed at it: consumer stubs, the omp ignore entry, the README row
// and the project lock entry.
package remove

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jegork/skillet/internal/consumer"
	"github.com/jegork/skillet/internal/inventory"
	"github.com/jegork/skillet/internal/readme"
	"github.com/jegork/skillet/internal/skill"
)

type Input struct {
	Home     skill.Paths
	Projects []inventory.Project
	// RemoveVendored deletes a vendored global skill through the pnpx CLI
	// so the lock stays consistent; nil uses pnpx. Tests replace it.
	RemoveVendored func(home, name string) error
}

// Cmd builds `pnpx skills remove` for a vendored global skill; the CLI owns
// the lock file and cleans the agent links.
func Cmd(home, name string) *exec.Cmd {
	cmd := exec.Command("pnpx", "skills", "remove", "-s", name, "-g", "-y")
	cmd.Dir = home
	// the CLI picks the global scope from $HOME, like registry.Add
	cmd.Env = append(os.Environ(), "HOME="+home)
	return cmd
}

// Remove deletes s and forgets it everywhere. Refused when s.Dir is not a
// real directory: a symlink or a misplaced stub is a doctor problem, not a
// delete target.
func Remove(in Input, s skill.Skill) error {
	info, err := os.Lstat(s.Dir)
	if err != nil {
		return err
	}
	if !info.Mode().IsDir() {
		return fmt.Errorf("%s is not a real directory (symlink or stub? run skillet doctor)", s.Dir)
	}
	if s.Origin.Vendored && s.Scope == "" {
		fn := in.RemoveVendored
		if fn == nil {
			fn = func(home, name string) error {
				out, err := Cmd(home, name).CombinedOutput()
				if err != nil {
					return fmt.Errorf("pnpx skills remove %s: %s", name, tail(out))
				}
				return nil
			}
		}
		if err := fn(in.Home.Home, s.Name); err != nil {
			return err
		}
	} else {
		if err := os.RemoveAll(s.Dir); err != nil {
			return err
		}
		if s.Scope != "" {
			if err := dropProjectLockEntry(s); err != nil {
				return err
			}
		}
	}
	return Cleanup(in, s)
}

// Cleanup forgets the skill on every consumer of its scope and drops the
// README row. After a pnpx removal the claude/codex stubs are already gone;
// Forget is a no-op for those.
func Cleanup(in Input, s skill.Skill) error {
	for _, c := range scopeConsumers(in, s) {
		if _, native := c.(*consumer.Native); native {
			continue
		}
		if err := c.Forget(s.Name); err != nil {
			return err
		}
	}
	return readme.Drop(in.Home.Readme(), s.Name)
}

func scopeConsumers(in Input, s skill.Skill) []consumer.Consumer {
	if s.Scope == "" {
		return inventory.Consumers(in.Home.Home)
	}
	for _, p := range in.Projects {
		if p.Root == s.Scope {
			return p.Consumers
		}
	}
	return nil
}

// dropProjectLockEntry removes the skill's entry from the project lock, so
// doctor does not report an orphan.
func dropProjectLockEntry(s skill.Skill) error {
	path := filepath.Join(s.Scope, "skills-lock.json")
	lock, err := skill.ReadProjectLock(path)
	if err != nil {
		return err
	}
	if lock.Missing {
		return nil
	}
	if _, ok := lock.Skills[s.Name]; !ok {
		return nil
	}
	delete(lock.Skills, s.Name)
	b, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// tail is the last non-empty line: the CLI writes progress spinners and
// banners over everything before the actual error.
func tail(out []byte) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if s := strings.TrimSpace(lines[i]); s != "" {
			return s
		}
	}
	return ""
}
