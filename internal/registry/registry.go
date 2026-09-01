// Package registry searches skills.sh and installs skills through the
// pnpx skills CLI, which owns ~/.agents/.skill-lock.json.
package registry

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// findTimeout covers one skills.sh query; addTimeout covers a clone plus
// install of a whole repository.
const (
	findTimeout = time.Minute
	addTimeout  = 5 * time.Minute
)

// Result is one skills.sh search hit. The find output carries no
// description, only an install count and a skills.sh URL.
type Result struct {
	Source   string // owner/repo
	Skill    string // the name after the @
	Installs string // e.g. "331.5K"
	URL      string
}

// findLine matches "owner/repo@skill 331.5K installs"; the header line
// ("Install with npx skills add <owner/repo@skill>") has no "installs".
var findLine = regexp.MustCompile(`^([^@\s]+)@([^@\s]+)\s+(\S+) installs$`)

// the CLI colours its output even when stdout is a pipe
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// ParseFind reads the text output of `pnpx skills find`. Non-TTY output is
// plain text, one hit per pair of lines:
//
//	owner/repo@skill 331.5K installs
//	└ https://skills.sh/owner/repo/skill
//
// Anything that does not match is skipped, so a CLI formatting change
// degrades to fewer results instead of an error.
func ParseFind(out string) []Result {
	var results []Result
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		line := ansiRe.ReplaceAllString(line, "")
		m := findLine.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		r := Result{Source: m[1], Skill: m[2], Installs: m[3]}
		if i+1 < len(lines) {
			next := ansiRe.ReplaceAllString(lines[i+1], "")
			if u, ok := strings.CutPrefix(strings.TrimSpace(next), "└"); ok {
				r.URL = strings.TrimSpace(u)
			}
		}
		results = append(results, r)
	}
	return results
}

// Find runs `pnpx skills find <query>` and parses the hits.
func Find(ctx context.Context, query string) ([]Result, error) {
	ctx, cancel := context.WithTimeout(ctx, findTimeout)
	defer cancel()
	out, err := run(ctx, "find", query)
	if err != nil {
		return nil, err
	}
	return ParseFind(out), nil
}

// Add installs one skill, or every skill in the source when name is empty,
// into the global skills dir of home. The lock file stays with the CLI.
func Add(ctx context.Context, home, source, name string) error {
	if name == "" {
		name = "*"
	}
	ctx, cancel := context.WithTimeout(ctx, addTimeout)
	defer cancel()
	args := []string{"skills", "add", source, "--skill", name, "-g", "-y"}
	cmd := exec.CommandContext(ctx, "pnpx", args...)
	cmd.Dir = home
	// the CLI picks the global scope from $HOME
	cmd.Env = append(os.Environ(), "HOME="+home)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("pnpx skills add %s: %s", source, tail(out))
	}
	return nil
}

func run(ctx context.Context, args ...string) (string, error) {
	b, err := exec.CommandContext(ctx, "pnpx", append([]string{"skills"}, args...)...).Output()
	if err == nil {
		return string(b), nil
	}
	msg := ""
	var ee *exec.ExitError
	if errors.As(err, &ee) && len(ee.Stderr) > 0 {
		msg = ": " + tail(ee.Stderr)
	}
	if ctx.Err() != nil {
		return "", fmt.Errorf("pnpx skills %s: timed out", args[0])
	}
	return "", fmt.Errorf("pnpx skills %s: %w%s", args[0], err, msg)
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
