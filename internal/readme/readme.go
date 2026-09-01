// Package readme reads and regenerates the hand-categorized index at
// ~/.agents/skills/README.md.
package readme

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"

	"github.com/jegork/skillet/internal/skill"
)

const (
	tableHeader    = "| Skill | Origin | What it does |\n|---|---|---|\n"
	uncategorized  = "## Uncategorized"
	defaultIntro   = "# Skills index\n\nGenerated overview of `~/.agents/skills`. **own** = authored in this repo, rename/edit freely. **vendored** = installed and re-synced by `pnpx skills` (tracked in `~/.agents/.skill-lock.json`) — do not rename or edit.\n"
	maxDescription = 160
)

var rowRe = regexp.MustCompile("^\\| `([^`]+)` \\| ([^|]*?) \\|")

// Index is what the README claims: skill name -> origin text.
type Index struct {
	Origins map[string]string
}

func Parse(path string) (Index, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Index{}, err
	}
	idx := Index{Origins: map[string]string{}}
	for _, line := range strings.Split(string(b), "\n") {
		if m := rowRe.FindStringSubmatch(line); m != nil {
			idx.Origins[m[1]] = m[2]
		}
	}
	return idx, nil
}

type Result struct {
	Added, Removed int
}

// Regenerate rewrites every row from the scanned skills, keeps each skill in
// the section it already sits in, drops rows for skills that are gone and
// appends unknown skills under "Uncategorized". Prose outside tables is kept.
func Regenerate(path string, skills []skill.Skill) (Result, error) {
	var res Result
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		b = []byte(defaultIntro)
	} else if err != nil {
		return res, err
	}
	byName := map[string]skill.Skill{}
	for _, s := range skills {
		byName[s.Name] = s
	}
	seen := map[string]bool{}
	var out []string
	inTable := false
	for _, line := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		m := rowRe.FindStringSubmatch(line)
		switch {
		case m != nil:
			inTable = true
			s, ok := byName[m[1]]
			if !ok || seen[m[1]] {
				res.Removed++
				continue
			}
			seen[m[1]] = true
			out = append(out, row(s))
		case inTable && strings.HasPrefix(line, "|"):
			out = append(out, line)
		default:
			inTable = false
			out = append(out, line)
		}
	}
	var missing []skill.Skill
	for _, s := range skills {
		if !seen[s.Name] {
			missing = append(missing, s)
		}
	}
	res.Added = len(missing)
	if len(missing) > 0 {
		out = appendUncategorized(out, missing)
	}
	text := strings.TrimRight(strings.Join(out, "\n"), "\n") + "\n"
	return res, os.WriteFile(path, []byte(text), 0o644)
}

func appendUncategorized(lines []string, skills []skill.Skill) []string {
	for i, line := range lines {
		if strings.TrimSpace(line) != uncategorized {
			continue
		}
		// insert after the section's table, before the next heading
		end := len(lines)
		for j := i + 1; j < len(lines); j++ {
			if strings.HasPrefix(lines[j], "## ") {
				end = j
				break
			}
		}
		for end > i+1 && strings.TrimSpace(lines[end-1]) == "" {
			end--
		}
		var rows []string
		for _, s := range skills {
			rows = append(rows, row(s))
		}
		return append(lines[:end], append(rows, lines[end:]...)...)
	}
	lines = append(lines, "", uncategorized, "", strings.TrimRight(tableHeader, "\n"))
	for _, s := range skills {
		lines = append(lines, row(s))
	}
	return lines
}

func row(s skill.Skill) string {
	return fmt.Sprintf("| `%s` | %s | %s |", s.Name, s.Origin, summary(s.Description))
}

// summary is the first sentence, or a truncated description when it has none
// short enough to fit.
func summary(desc string) string {
	desc = strings.Join(strings.Fields(desc), " ")
	if i := strings.Index(desc, ". "); i > 0 && i+1 <= maxDescription {
		return desc[:i+1]
	}
	r := []rune(desc)
	if len(r) > maxDescription {
		return string(r[:maxDescription-3]) + "..."
	}
	return desc
}

// Rename moves a row to a new name in place. A missing README is fine.
func Rename(path, oldName, newName string) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	lines := strings.Split(string(b), "\n")
	for i, line := range lines {
		if m := rowRe.FindStringSubmatch(line); m != nil && m[1] == oldName {
			lines[i] = "| `" + newName + "`" + line[len("| `"+oldName+"`"):]
		}
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
}
