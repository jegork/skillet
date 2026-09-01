package skill_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jegork/skillet/internal/skill"
)

// git itself is the oracle: the hash must equal what write-tree produces
// for the same files.
func TestTreeHashMatchesGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	write := func(rel, content string, mode os.FileMode) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	write("SKILL.md", "---\nname: a\n---\n", 0o644)
	write("scripts/run.sh", "#!/bin/sh\n", 0o755)
	write("docs/b.md", "b", 0o644)
	write("docs-extra", "sorted after docs/ as a file", 0o644)
	write("z.txt", "", 0o644)
	if err := os.Symlink("SKILL.md", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}

	git := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("config", "core.filemode", "true")
	git("add", "-A")
	want := git("write-tree")

	got, err := skill.TreeHash(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("TreeHash = %s, git write-tree = %s", got, want)
	}
}
