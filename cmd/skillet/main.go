package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/jegork/skillet/internal/doctor"
	"github.com/jegork/skillet/internal/inventory"
	"github.com/jegork/skillet/internal/readme"
	"github.com/jegork/skillet/internal/registry"
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
	fs := flag.NewFlagSet("skillet", flag.ExitOnError)
	fs.StringVar(&home, "home", home, "home directory to manage")
	storeName := fs.String("store", "chezmoi", "store backend: chezmoi or git")
	showVersion := fs.Bool("version", false, "print version")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: skillet [--home DIR] [--store chezmoi|git] [doctor|status|readme|install|store init]")
		fs.PrintDefaults()
	}
	_ = fs.Parse(os.Args[1:])
	if *showVersion {
		fmt.Println("skillet", version)
		return
	}
	if *storeName != "chezmoi" && *storeName != "git" {
		fmt.Fprintln(os.Stderr, "skillet: unknown --store", *storeName, "(want chezmoi or git)")
		os.Exit(2)
	}
	var err error
	switch fs.Arg(0) {
	case "doctor":
		err = runDoctor(home)
	case "status":
		err = runStatus(home, newStore(*storeName, home, ""))
	case "readme":
		err = runReadme(home)
	case "install":
		err = runInstall(home, fs.Args()[1:])
	case "store":
		err = runStore(home, fs.Args()[1:])
	case "":
		err = runTUI(home, newStore(*storeName, home, ""))
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

func runTUI(home string, st store.Store) error {
	inv, err := inventory.Load(home)
	if err != nil {
		return err
	}
	m := ui.New(ui.Config{
		Inventory: inv,
		Load:      func() (inventory.Inventory, error) { return inventory.Load(home) },
		Store:     st,
		Consumers: inventory.Consumers(home),
		Find:      registry.Find,
		Install: func(ctx context.Context, source, skill string) error {
			return registry.Add(ctx, home, source, skill)
		},
	})
	_, err = tea.NewProgram(m).Run()
	return err
}

func runDoctor(home string) error {
	inv, err := inventory.Load(home)
	if err != nil {
		return err
	}
	vendored := 0
	for _, s := range inv.Skills {
		if s.Origin.Vendored {
			vendored++
		}
	}
	fmt.Printf("%d skills (%d own, %d vendored)\n", len(inv.Skills), len(inv.Skills)-vendored, vendored)
	for _, c := range inv.Consumers {
		n := 0
		for _, on := range inv.Reports[c].Enabled {
			if on {
				n++
			}
		}
		fmt.Printf("  %-6s %d enabled\n", c, n)
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
		if subject == "" {
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
	res, err := readme.Regenerate(inv.Paths.Readme(), inv.Skills)
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

func runInstall(home string, args []string) error {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	name := fs.String("skill", "", "skill to install (default: every skill in the source)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: skillet [--home DIR] install <source> [--skill NAME]")
	}
	// flag parsing stops at the source positional, so flags after it are
	// split out by hand
	var source string
	var rest []string
	for _, a := range args {
		if source == "" && !strings.HasPrefix(a, "-") {
			source = a
			continue
		}
		rest = append(rest, a)
	}
	_ = fs.Parse(rest)
	if source == "" {
		fs.Usage()
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := registry.Add(ctx, home, source, *name); err != nil {
		return err
	}
	inv, err := inventory.Load(home)
	if err != nil {
		return err
	}
	if *name != "" && *name != "*" {
		for _, c := range inventory.Consumers(home) {
			if c.Name() == "claude" {
				if err := c.Enable(*name); err != nil {
					return fmt.Errorf("claude stub: %w", err)
				}
			}
		}
	}
	var installed []string
	for _, s := range inv.Skills {
		if s.Origin.Vendored && s.Origin.Source == source {
			installed = append(installed, s.Name)
		}
	}
	if _, err := readme.Regenerate(inv.Paths.Readme(), inv.Skills); err != nil {
		return err
	}
	fmt.Printf("installed %s from %s\n", strings.Join(installed, ", "), source)
func runStore(home string, args []string) error {
	if len(args) == 0 || args[0] != "init" {
		fmt.Fprintln(os.Stderr, "usage: skillet store init [--git-dir DIR] [--remote URL]")
		os.Exit(2)
	}
	fs := flag.NewFlagSet("skillet store init", flag.ExitOnError)
	gitDir := fs.String("git-dir", "", "git dir for the store repo (default ~/.agents/.skillet-store.git)")
	remote := fs.String("remote", "", "origin remote to add")
	_ = fs.Parse(args[1:])
	dir := *gitDir
	if dir == "" {
		dir = filepath.Join(home, ".agents", ".skillet-store.git")
	}
	if err := gitrepo.Init(home, dir, *remote); err != nil {
		return err
	}
	fmt.Printf("git store initialized at %s\n", dir)
	return nil
}
