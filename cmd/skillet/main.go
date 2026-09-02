package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/jegork/skillet/internal/config"
	"github.com/jegork/skillet/internal/doctor"
	"github.com/jegork/skillet/internal/inventory"
	"github.com/jegork/skillet/internal/readme"
	"github.com/jegork/skillet/internal/skill"
	"github.com/jegork/skillet/internal/store"
	"github.com/jegork/skillet/internal/store/chezmoi"
	"github.com/jegork/skillet/internal/store/gitrepo"
	"github.com/jegork/skillet/internal/ui"
)

var version = "dev"

var roots = []store.Root{
	{Rel: ".agents/skills"},
	{Rel: ".agents/.skill-lock.json"},
	{Rel: ".claude/skills"},
	{Rel: ".codex/skills", Exclude: []string{".system"}},
	{Rel: ".omp/agent/config.yml"},
}

func main() {
	home, _ := os.UserHomeDir()
	storeName := "chezmoi"
	storeSet := false
	fs := flag.NewFlagSet("skillet", flag.ExitOnError)
	fs.StringVar(&home, "home", home, "home directory to manage")
	fs.Func("store", "store backend: chezmoi or git", func(s string) error {
		if s != "chezmoi" && s != "git" {
			return fmt.Errorf("unknown store %q (want chezmoi or git)", s)
		}
		storeName, storeSet = s, true
		return nil
	})
	showVersion := fs.Bool("version", false, "print version")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: skillet [--home DIR] [--store chezmoi|git] [config|doctor|status|readme|store init]")
		fs.PrintDefaults()
	}
	_ = fs.Parse(os.Args[1:])
	if *showVersion {
		fmt.Println("skillet", version)
		return
	}
	cfg, err := config.Load(home)
	if err != nil {
		fmt.Fprintln(os.Stderr, "skillet:", err)
		os.Exit(1)
	}
	// precedence: flag, then config, then the chezmoi default
	if !storeSet && cfg.Store != "" {
		storeName = cfg.Store
	}
	switch fs.Arg(0) {
	case "config":
		err = runConfig(home, fs.Args()[1:])
	case "doctor":
		err = runDoctor(home)
	case "status":
		err = runStatus(home, newStore(storeName, home, cfg.GitStore.Dir))
	case "readme":
		err = runReadme(home)
	case "store":
		err = runStore(home, cfg, fs.Args()[1:])
	case "":
		err = runTUI(home, newStore(storeName, home, cfg.GitStore.Dir), config.Path(home))
	default:
		fs.Usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "skillet:", err)
		os.Exit(1)
	}
}

// newStore builds the selected backend; gitDir empty means the default
// ~/.agents/.skillet-store.git.
func newStore(name, home, gitDir string) store.Store {
	if name == "git" {
		if gitDir == "" {
			gitDir = filepath.Join(home, ".agents", ".skillet-store.git")
		}
		return gitrepo.New(home, gitDir, roots)
	}
	return chezmoi.New(home, roots)
}

func runTUI(home string, st store.Store, cfgPath string) error {
	inv, err := inventory.Load(home)
	if err != nil {
		return err
	}
	m := ui.New(ui.Config{
		Inventory:  inv,
		Load:       func() (inventory.Inventory, error) { return inventory.Load(home) },
		Store:      st,
		Consumers:  inventory.Consumers(home),
		ConfigPath: cfgPath,
	})
	_, err = tea.NewProgram(m).Run()
	return err
}

// runConfig handles skillet config path|get|set|edit.
func runConfig(home string, args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: skillet config path|get [key]|set key value...|edit")
		os.Exit(2)
	}
	switch args[0] {
	case "path":
		fmt.Println(config.Path(home))
	case "get":
		if len(args) > 2 {
			fmt.Fprintln(os.Stderr, "usage: skillet config get [key]")
			os.Exit(2)
		}
		if len(args) == 1 {
			return printAll(home)
		}
		values, err := config.Get(home, args[1])
		if err != nil {
			return err
		}
		for _, v := range values {
			fmt.Println(v)
		}
	case "set":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: skillet config set key value...")
			os.Exit(2)
		}
		return config.Set(home, args[1], args[2:])
	case "edit":
		if err := config.Ensure(home); err != nil {
			return err
		}
		editor := os.Getenv("VISUAL")
		if editor == "" {
			editor = os.Getenv("EDITOR")
		}
		if editor == "" {
			editor = "vi"
		}
		c := exec.Command("sh", "-c", editor+` "$1"`, "sh", config.Path(home))
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
		return c.Run()
	default:
		fmt.Fprintf(os.Stderr, "skillet: unknown config command %q\n", args[0])
		os.Exit(2)
	}
	return nil
}

// printAll prints every settable key with its stored values.
func printAll(home string) error {
	for _, key := range config.KnownKeys {
		values, err := config.Get(home, key)
		if err != nil {
			return err
		}
		if len(values) == 0 {
			fmt.Printf("%s:\n", key)
			continue
		}
		for _, v := range values {
			fmt.Printf("%s: %s\n", key, v)
		}
	}
	return nil
}

func runDoctor(home string) error {
	inv, err := inventory.Load(home)
	if err != nil {
		return err
	}
	vendored, projects := 0, 0
	for _, s := range inv.Skills {
		if s.Origin.Vendored {
			vendored++
		}
		if s.Scope != "" {
			projects++
		}
	}
	fmt.Printf("%d skills (%d own, %d vendored, %d project)\n", len(inv.Skills), len(inv.Skills)-vendored, vendored, projects)
	for _, c := range inv.Consumers {
		n := 0
		for _, on := range inv.Reports[c].Enabled {
			if on {
				n++
			}
		}
		fmt.Printf("  %-6s %d enabled\n", c, n)
	}
	for _, p := range inv.Projects {
		unit := "skills"
		if len(p.Skills) == 1 {
			unit = "skill"
		}
		fmt.Printf("  project %s (%d %s)\n", filepath.Base(p.Root), len(p.Skills), unit)
	}
	if len(inv.Findings) == 0 {
		fmt.Println("doctor: no findings")
		return nil
	}
	fmt.Println()
	failed := false
	for _, f := range inv.Findings {
		if f.Severity == doctor.Error {
			failed = true
		}
		subject := f.Skill
		if f.Project != "" {
			subject = filepath.Base(f.Project)
			if f.Skill != "" {
				subject += "/" + f.Skill
			}
		} else if subject == "" {
			subject = "(global)"
		}
		fmt.Printf("%-5s %-14s %-28s %s\n", f.Severity, f.Check, subject, f.Message)
	}
	if failed {
		return fmt.Errorf("doctor found errors")
	}
	return nil
}

func runReadme(home string) error {
	inv, err := inventory.Load(home)
	if err != nil {
		return err
	}
	var global []skill.Skill
	for _, s := range inv.Skills {
		if s.Scope == "" {
			global = append(global, s)
		}
	}
	res, err := readme.Regenerate(inv.Paths.Readme(), global)
	if err != nil {
		return err
	}
	fmt.Printf("README index regenerated: +%d -%d\n", res.Added, res.Removed)
	return nil
}

func runStatus(home string, st store.Store) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	status, err := st.Status(ctx)
	if err != nil {
		return err
	}
	ahead := "no upstream"
	if status.Ahead >= 0 {
		ahead = fmt.Sprintf("+%d", status.Ahead)
	}
	fmt.Printf("branch %s (%s)\n", status.Branch, ahead)
	fmt.Printf("uncaptured: %d\n", len(status.Uncaptured))
	for _, c := range status.Uncaptured {
		fmt.Printf("  %s %s\n", c.Kind, c.Path)
	}
	fmt.Printf("uncommitted: %d\n", len(status.Uncommitted))
	if len(status.Uncommitted) > 0 {
		fmt.Println("  " + strings.Join(status.Uncommitted, "\n  "))
	}
	return nil
}

func runStore(home string, cfg config.Config, args []string) error {
	if len(args) == 0 || args[0] != "init" {
		fmt.Fprintln(os.Stderr, "usage: skillet store init [--git-dir DIR] [--remote URL]")
		os.Exit(2)
	}
	fs := flag.NewFlagSet("skillet store init", flag.ExitOnError)
	gitDir := fs.String("git-dir", "", "git dir for the store repo (default from git_store.dir or ~/.agents/.skillet-store.git)")
	remote := fs.String("remote", "", "origin remote to add")
	_ = fs.Parse(args[1:])
	dir := *gitDir
	if dir == "" {
		dir = cfg.GitStore.Dir
	}
	if dir == "" {
		dir = filepath.Join(home, ".agents", ".skillet-store.git")
	}
	if err := gitrepo.Init(home, dir, *remote); err != nil {
		return err
	}
	if err := config.Set(home, "git_store.dir", []string{dir}); err != nil {
		return err
	}
	if *remote != "" {
		if err := config.Set(home, "git_store.remote", []string{*remote}); err != nil {
			return err
		}
	}
	fmt.Printf("git store initialized at %s\n", dir)
	return nil
}
