// Package rename moves an own skill to a new name and fixes everything that
// pointed at the old one: frontmatter, cross-references, consumer stubs, the
// README row.
package rename

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/jegork/skillet/internal/consumer"
	"github.com/jegork/skillet/internal/readme"
	"github.com/jegork/skillet/internal/skill"
)

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

type Report struct {
	RewrittenFiles int
	// VendoredRefs lists vendored skills that still mention the old name;
	// those files are never edited.
	VendoredRefs []string
}

func Rename(paths skill.Paths, skills []skill.Skill, consumers []consumer.Consumer, oldName, newName string) (Report, error) {
	var rep Report
	var old *skill.Skill
	for i := range skills {
		if skills[i].Name == oldName {
			old = &skills[i]
		}
		if skills[i].Name == newName {
			return rep, fmt.Errorf("a skill named %q already exists", newName)
		}
	}
	switch {
	case old == nil:
		return rep, fmt.Errorf("no skill named %q", oldName)
	case old.Origin.Vendored:
		return rep, fmt.Errorf("%s is %s, pnpx skills owns its name", oldName, old.Origin)
	case oldName == newName:
		return rep, errors.New("same name")
	case !nameRe.MatchString(newName):
		return rep, fmt.Errorf("%q is not a valid skill name", newName)
	}
	newDir := filepath.Join(filepath.Dir(old.Dir), newName)
	if _, err := os.Lstat(newDir); err == nil {
		return rep, fmt.Errorf("%s already exists", newDir)
	}

	enabled := map[string]bool{}
	for _, c := range consumers {
		r, err := c.Report(skills)
		if err != nil {
			return rep, err
		}
		enabled[c.Name()] = r.Enabled[oldName]
	}

	if err := os.Rename(old.Dir, newDir); err != nil {
		return rep, err
	}
	if err := rewriteFrontmatter(filepath.Join(newDir, "SKILL.md"), oldName, newName); err != nil {
		return rep, err
	}

	xref := regexp.MustCompile("`" + regexp.QuoteMeta(oldName) + "` ([sS][kK][iI][lL][lL]\\b)")
	for _, s := range skills {
		dir := s.Dir
		if s.Name == oldName {
			dir = newDir
		}
		for _, rel := range s.Markdown {
			p := filepath.Join(dir, rel)
			b, err := os.ReadFile(p)
			if err != nil || !xref.Match(b) {
				continue
			}
			if s.Origin.Vendored {
				rep.VendoredRefs = append(rep.VendoredRefs, s.Name)
				break
			}
			out := xref.ReplaceAll(b, []byte("`"+newName+"` $1"))
			if err := os.WriteFile(p, out, 0o644); err != nil {
				return rep, err
			}
			rep.RewrittenFiles++
		}
	}

	for _, c := range consumers {
		if err := c.Forget(oldName); err != nil {
			return rep, err
		}
		var err error
		if enabled[c.Name()] {
			err = c.Enable(newName)
		} else {
			err = c.Disable(newName)
		}
		if err != nil {
			return rep, err
		}
	}
	return rep, readme.Rename(paths.Readme(), oldName, newName)
}

func rewriteFrontmatter(path, oldName, newName string) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	re := regexp.MustCompile("(?m)^name:[ \\t]*" + regexp.QuoteMeta(oldName) + "[ \\t]*$")
	loc := re.FindIndex(b)
	if loc == nil {
		return nil
	}
	out := append([]byte{}, b[:loc[0]]...)
	out = append(out, []byte("name: "+newName)...)
	out = append(out, b[loc[1]:]...)
	return os.WriteFile(path, out, 0o644)
}
