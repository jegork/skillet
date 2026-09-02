package registry

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const fixture = `
Install with npx skills add <owner/repo@skill>

juliusbrussee/caveman@caveman-commit 331.5K installs
└ https://skills.sh/juliusbrussee/caveman/caveman-commit

mattpocock/skills@setup-pre-commit 294.4K installs
└ https://skills.sh/mattpocock/skills/setup-pre-commit

github/awesome-copilot@git-commit 43.9K installs
└ https://skills.sh/github/awesome-copilot/git-commit

diodeinc/pcb@git-hunk 106 installs
`

func TestParseFind(t *testing.T) {
	got := ParseFind(fixture)
	if len(got) != 4 {
		t.Fatalf("got %d results, want 4: %+v", len(got), got)
	}
	first := got[0]
	if first.Source != "juliusbrussee/caveman" || first.Skill != "caveman-commit" ||
		first.Installs != "331.5K" || first.URL != "https://skills.sh/juliusbrussee/caveman/caveman-commit" {
		t.Errorf("first result wrong: %+v", first)
	}
	if got[3].URL != "" {
		t.Errorf("hit without a url line must have an empty URL, got %q", got[3].URL)
	}
	if got[3].Installs != "106" {
		t.Errorf("plain install count mangled: %+v", got[3])
	}
}

func TestParseFindDefensive(t *testing.T) {
	for name, out := range map[string]string{
		"empty":         "",
		"header only":   "\nInstall with npx skills add <owner/repo@skill>\n\n",
		"no at sign":    "some-skill 12 installs\n",
		"no count":      "a/b@c installs\n└ https://skills.sh/a/b/c\n",
		"two at signs":  "a/b@c@d 12 installs\n",
		"spaces in sfx": "a/b@c 12  installs\n",
	} {
		if got := ParseFind(out); len(got) != 0 {
			t.Errorf("%s: expected no results, got %+v", name, got)
		}
	}
}

func TestParseFindANSI(t *testing.T) {
	out := "\n\x1b[38;5;102mInstall with\x1b[0m npx skills add <owner/repo@skill>\n\n" +
		"\x1b[38;5;145mjuliusbrussee/caveman@caveman-commit\x1b[0m \x1b[36m331.7K installs\x1b[0m\n" +
		"\x1b[38;5;102m└ https://skills.sh/juliusbrussee/caveman/caveman-commit\x1b[0m\n"
	got := ParseFind(out)
	if len(got) != 1 || got[0].Skill != "caveman-commit" ||
		got[0].URL != "https://skills.sh/juliusbrussee/caveman/caveman-commit" {
		t.Errorf("colored output mangled: %+v", got)
	}
}

func TestParseFindCRLF(t *testing.T) {
	got := ParseFind("a/b@c 12 installs\r\n└ https://skills.sh/a/b/c\r\n")
	if len(got) != 1 || got[0].URL != "https://skills.sh/a/b/c" {
		t.Errorf("CRLF output mangled: %+v", got)
	}
}

func TestFindLive(t *testing.T) {
	// hits the network and pnpx; opt in explicitly so task test stays fast
	if os.Getenv("SKILLET_LIVE_TESTS") == "" {
		t.Skip("set SKILLET_LIVE_TESTS=1 to run live registry tests")
	}
	skipIfNoPnpx(t)
	ctx, cancel := context.WithTimeout(context.Background(), findTimeout)
	defer cancel()
	res, err := Find(ctx, "commit")
	if err != nil {
		t.Fatal(err)
	}
	if len(res) == 0 {
		t.Fatal("live search returned no hits")
	}
	if !strings.Contains(res[0].Source, "/") || res[0].Skill == "" {
		t.Errorf("malformed live result: %+v", res[0])
	}
}

func TestAddLive(t *testing.T) {
	// hits the network and pnpx; opt in explicitly so task test stays fast
	if os.Getenv("SKILLET_LIVE_TESTS") == "" {
		t.Skip("set SKILLET_LIVE_TESTS=1 to run live registry tests")
	}
	skipIfNoPnpx(t)
	home := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), addTimeout)
	defer cancel()
	if err := Add(ctx, home, "getsentry/skills", "commit"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "commit", "SKILL.md")); err != nil {
		t.Errorf("skill not installed into HOME: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", ".skill-lock.json")); err != nil {
		t.Errorf("lock file not written: %v", err)
	}
}

func skipIfNoPnpx(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping live registry test in -short mode")
	}
	if _, err := exec.LookPath("pnpx"); err != nil {
		t.Skip("pnpx not on PATH")
	}
}

func TestUpdateCmd(t *testing.T) {
	cmd := UpdateCmd("/tmp/home", "alpha")
	if got := strings.Join(cmd.Args, " "); got != "pnpx skills update alpha -g -y" {
		t.Errorf("args %q", got)
	}
	if cmd.Dir != "/tmp/home" {
		t.Errorf("dir %q", cmd.Dir)
	}
	var home string
	for _, e := range cmd.Env {
		if strings.HasPrefix(e, "HOME=") {
			home = e
		}
	}
	if home != "HOME=/tmp/home" {
		t.Errorf("env %q", home)
	}
}
