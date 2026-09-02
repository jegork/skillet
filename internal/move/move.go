// Package move relocates a skill between the global skills dir and a
// project's skills dir, in both directions, and fixes everything that
// pointed at it: consumer stubs, the README row, the lock entries.
package move

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/jegork/skillet/internal/consumer"
	"github.com/jegork/skillet/internal/inventory"
	"github.com/jegork/skillet/internal/readme"
	"github.com/jegork/skillet/internal/skill"
)

// Input is the world a move happens in: the global home plus every
// discovered project with its skills.
type Input struct {
	Home     skill.Paths
	Skills   []skill.Skill
	Projects []inventory.Project
}

// Move relocates s to target: "" moves it into the global skills dir, a
// project root into that project's canonical skills dir. A refused move
// leaves both sides untouched.
func Move(in Input, s skill.Skill, targetRoot string) error {
	if s.Scope == targetRoot {
		return errors.New("the skill already lives in that scope")
	}
	srcGlobal := s.Scope == ""
	tgtGlobal := targetRoot == ""

	var srcProject, tgtProject *inventory.Project
	if !srcGlobal {
		p, err := findProject(in, s.Scope)
		if err != nil {
			return err
		}
		srcProject = p
	}
	var tgtSkills []skill.Skill
	if tgtGlobal {
		// in.Skills is the flat list across scopes; only home skills clash
		for _, g := range in.Skills {
			if g.Scope == "" {
				tgtSkills = append(tgtSkills, g)
			}
		}
	} else {
		p, err := findProject(in, targetRoot)
		if err != nil {
			return err
		}
		tgtProject = p
		tgtSkills = p.Skills
	}
	tgtDir := in.Home.SkillsDir()
	if tgtGlobal {
		tgtDir = filepath.Join(tgtDir, s.Name)
	} else {
		tgtDir = filepath.Join(tgtProject.SkillsDir, s.Name)
	}
	for _, t := range tgtSkills {
		if t.Name == s.Name {
			return fmt.Errorf("%s already has a skill named %q", scopeName(targetRoot), s.Name)
		}
	}
	// project scope hides global for that project: only a cross-project move
	// can create a shadow, the global copy is what leaves when moving home
	if !tgtGlobal && !srcGlobal {
		for _, g := range in.Skills {
			if g.Scope == "" && g.Name == s.Name {
				return fmt.Errorf("moving %q into %s would shadow the global skill of the same name", s.Name, scopeName(targetRoot))
			}
		}
	}
	if _, err := os.Lstat(tgtDir); err == nil {
		return fmt.Errorf("%s already exists", tgtDir)
	}

	var srcConsumers []consumer.Consumer
	var srcSkills []skill.Skill
	var srcLockPath string
	if srcGlobal {
		srcConsumers = inventory.Consumers(in.Home.Home)
		srcSkills = in.Skills
		srcLockPath = in.Home.LockFile()
	} else {
		srcConsumers = srcProject.Consumers
		srcSkills = srcProject.Skills
		srcLockPath = filepath.Join(srcProject.Root, "skills-lock.json")
	}
	seen := map[string]bool{}
	for _, c := range srcConsumers {
		rep, err := c.Report(srcSkills)
		if err != nil {
			return err
		}
		seen[c.Name()] = rep.Enabled[s.Name]
	}

	// the entry to carry across scopes, read before anything changes
	var entry skill.LockEntry
	var hasEntry bool
	if s.Origin.Vendored {
		lock, err := readScopeLock(srcGlobal, srcLockPath)
		if err != nil {
			return err
		}
		entry, hasEntry = lock.Skills[s.Name]
	}

	if err := relocate(s.Dir, tgtDir); err != nil {
		return err
	}

	for _, c := range srcConsumers {
		if _, native := c.(*consumer.Native); native {
			continue
		}
		if err := c.Forget(s.Name); err != nil {
			return err
		}
	}
	for _, c := range tgtConsumers(tgtGlobal, in.Home.Home, tgtProject) {
		if _, native := c.(*consumer.Native); native {
			continue
		}
		if !seen[c.Name()] {
			continue
		}
		if err := c.Enable(s.Name); err != nil {
			return err
		}
	}

	if hasEntry {
		if err := migrateLock(srcGlobal, srcLockPath, in, tgtGlobal, tgtProject, s.Name, entry, tgtDir); err != nil {
			return err
		}
	}

	readmePath := in.Home.Readme()
	if srcGlobal {
		return readme.Drop(readmePath, s.Name)
	}
	if tgtGlobal {
		return readme.Add(readmePath, s)
	}
	return nil
}

func tgtConsumers(global bool, home string, p *inventory.Project) []consumer.Consumer {
	if global {
		return inventory.Consumers(home)
	}
	return p.Consumers
}

func findProject(in Input, root string) (*inventory.Project, error) {
	for i := range in.Projects {
		if in.Projects[i].Root == root {
			return &in.Projects[i], nil
		}
	}
	return nil, fmt.Errorf("no project at %s", root)
}

func scopeName(root string) string {
	if root == "" {
		return "global"
	}
	return fmt.Sprintf("project %s", filepath.Base(root))
}

// relocate renames the skill dir, falling back to copy + remove across
// filesystems (home and a project may live on different mounts).

func relocate(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	var linkErr *os.LinkError
	if !errors.As(err, &linkErr) || !errors.Is(linkErr.Err, syscall.EXDEV) {
		return err
	}
	if err := copyTree(src, dst); err != nil {
		return err
	}
	return os.RemoveAll(src)
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		t := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(target, t)
		case d.IsDir():
			return os.MkdirAll(t, info.Mode().Perm())
		case info.Mode().IsRegular():
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			return os.WriteFile(t, b, info.Mode().Perm())
		}
		return nil
	})
}

// migrateLock moves the vendored entry to the target scope's lock: the
// global v3 lock tracks the git tree hash, the project v1 lock the
// computed content hash, so origin and drift keep working after the move.
func migrateLock(srcGlobal bool, srcLockPath string, in Input, tgtGlobal bool, tgtProject *inventory.Project, name string, entry skill.LockEntry, tgtDir string) error {
	// forget the entry at the source
	srcLock, err := readScopeLock(srcGlobal, srcLockPath)
	if err != nil {
		return err
	}
	delete(srcLock.Skills, name)
	if err := writeScopeLock(srcGlobal, srcLockPath, srcLock); err != nil {
		return err
	}

	// record it at the target
	tgtLockPath := in.Home.LockFile()
	if !tgtGlobal {
		tgtLockPath = filepath.Join(tgtProject.Root, "skills-lock.json")
	}
	tgtLock, err := readScopeLock(tgtGlobal, tgtLockPath)
	if err != nil {
		return err
	}
	if tgtGlobal {
		hash, err := skill.TreeHash(tgtDir)
		if err != nil {
			return err
		}
		entry.SkillFolderHash = hash
		entry.ComputedHash = ""
	} else {
		hash, err := skill.ContentHash(tgtDir)
		if err != nil {
			return err
		}
		entry.ComputedHash = hash
		entry.SkillFolderHash = ""
	}
	tgtLock.Skills[name] = entry
	return writeScopeLock(tgtGlobal, tgtLockPath, tgtLock)
}

func readScopeLock(global bool, path string) (skill.Lock, error) {
	var lock skill.Lock
	var err error
	if global {
		lock, err = skill.ReadLock(path)
	} else {
		var pl skill.ProjectLock
		pl, err = skill.ReadProjectLock(path)
		lock = skill.Lock{Version: pl.Version, Skills: pl.Skills, Missing: pl.Missing}
	}
	if err != nil {
		return skill.Lock{}, err
	}
	if lock.Skills == nil {
		lock.Skills = map[string]skill.LockEntry{}
	}
	return lock, nil
}

func writeScopeLock(global bool, path string, lock skill.Lock) error {
	if lock.Missing {
		lock.Missing = false
		lock.Version = 1
		if global {
			lock.Version = 3
		}
	}
	b, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
