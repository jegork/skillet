// Package gitrepo implements store.Store on a plain git repository whose
// worktree is $HOME: the git dir lives elsewhere (core.worktree points back
// at $HOME) and every operation is pathspec-scoped to the store roots, so
// nothing else in $HOME is ever staged, diffed, committed or pushed.
package gitrepo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/jegork/skillet/internal/store"
)

type Store struct {
	Bin   string
	Home  string
	Dir   string // git dir holding the history
	Roots []store.Root
	// Env is appended to the inherited environment.
	Env []string
}

func New(home, dir string, roots []store.Root) *Store {
	return &Store{Bin: "git", Home: home, Dir: dir, Roots: roots}
}

var _ store.Store = (*Store)(nil)

// Init creates a git dir for $HOME at dir and optionally adds an origin.
func Init(home, dir, remote string) error {
	s := Store{Bin: "git", Home: home, Dir: dir}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	if _, err := s.run(context.Background(), "init", "-b", "main"); err != nil {
		return err
	}
	// git init --git-dir creates a bare repo; the worktree comes back via config
	if _, err := s.run(context.Background(), "config", "core.bare", "false"); err != nil {
		return err
	}
	if _, err := s.run(context.Background(), "config", "core.worktree", home); err != nil {
		return err
	}
	if remote != "" {
		if _, err := s.run(context.Background(), "remote", "add", "origin", remote); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Status(ctx context.Context) (store.Status, error) {
	var st store.Status
	if specs := s.specs(); len(specs) > 0 {
		args := append([]string{"status", "--porcelain", "--"}, specs...)
		out, err := s.run(ctx, args...)
		if err != nil {
			return st, err
		}
		for _, line := range lines(out) {
			if len(line) < 4 {
				continue
			}
			path := renameSplit(strings.TrimSuffix(line[3:], "/"))
			switch line[1] {
			case ' ':
				// staged only: the worktree matches the index
			case '?':
				st.Uncaptured = append(st.Uncaptured, store.Change{Path: path, Kind: store.Added})
			case 'D':
				st.Uncaptured = append(st.Uncaptured, store.Change{Path: s.deletedDir(path), Kind: store.Deleted})
			default:
				st.Uncaptured = append(st.Uncaptured, store.Change{Path: path, Kind: store.Modified})
			}
			if line[0] != ' ' && line[0] != '?' {
				st.Uncommitted = append(st.Uncommitted, path)
			}
		}
	}
	st.Uncaptured = collapse(st.Uncaptured)

	branch, err := s.run(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		// unborn branch: no commit yet, so read the branch name directly
		name, err2 := s.run(ctx, "symbolic-ref", "--short", "HEAD")
		if err2 != nil {
			return st, err
		}
		st.Branch, st.Ahead = strings.TrimSpace(name), -1
		return st, nil
	}
	st.Branch = strings.TrimSpace(branch)
	st.Ahead = -1
	if out, err := s.run(ctx, "rev-list", "--count", "@{u}..HEAD"); err == nil {
		st.Ahead, _ = strconv.Atoi(strings.TrimSpace(out))
	}
	return st, nil
}

func (s *Store) Capture(ctx context.Context) error {
	specs, err := s.activeSpecs(ctx)
	if err != nil || len(specs) == 0 {
		return err
	}
	_, err = s.run(ctx, append([]string{"add", "-A", "--"}, specs...)...)
	return err
}

func (s *Store) Diff(ctx context.Context) (string, error) {
	specs := s.specs()
	if len(specs) == 0 {
		return "", nil
	}
	target, err := s.diffTarget(ctx)
	if err != nil {
		return "", err
	}
	stat, err := s.run(ctx, append([]string{"diff", target, "--stat", "--"}, specs...)...)
	if err != nil {
		return "", err
	}
	full, err := s.run(ctx, append([]string{"diff", target, "--"}, specs...)...)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(stat) == "" {
		return "", nil
	}
	return stat + "\n" + full, nil
}

func (s *Store) Commit(ctx context.Context, message string) error {
	specs, err := s.activeSpecs(ctx)
	if err != nil {
		return err
	}
	if len(specs) == 0 {
		return errors.New("no store roots exist yet")
	}
	_, err = s.run(ctx, append([]string{"commit", "-m", message, "--"}, specs...)...)
	return err
}

func (s *Store) Push(ctx context.Context) error {
	_, err := s.run(ctx, "push")
	return err
}

// specs lists every root with its exclude pathspecs. status and diff accept
// pathspecs that match nothing, so no filtering is needed here.
func (s *Store) specs() []string {
	var out []string
	for _, r := range s.Roots {
		out = append(out, r.Rel)
		for _, ex := range r.Exclude {
			out = append(out, ":(exclude)"+filepath.Join(r.Rel, ex))
		}
	}
	return out
}

// activeSpecs drops roots that neither exist in $HOME nor have tracked files:
// git errors on add and commit when a pathspec matches nothing.
func (s *Store) activeSpecs(ctx context.Context) ([]string, error) {
	if len(s.Roots) == 0 {
		return nil, nil
	}
	rels := make([]string, len(s.Roots))
	for i, r := range s.Roots {
		rels[i] = r.Rel
	}
	out, err := s.run(ctx, append([]string{"ls-files", "--"}, rels...)...)
	if err != nil {
		return nil, err
	}
	tracked := lines(out)
	var specs []string
	for _, r := range s.Roots {
		active := false
		if _, err := os.Lstat(filepath.Join(s.Home, r.Rel)); err == nil {
			active = true
		}
		for _, p := range tracked {
			if strings.HasPrefix(p, r.Rel+"/") {
				active = true
				break
			}
		}
		if !active {
			continue
		}
		specs = append(specs, r.Rel)
		for _, ex := range r.Exclude {
			specs = append(specs, ":(exclude)"+filepath.Join(r.Rel, ex))
		}
	}
	return specs, nil
}

// diffTarget is HEAD, or the empty tree while the branch is still unborn.
func (s *Store) diffTarget(ctx context.Context) (string, error) {
	if _, err := s.run(ctx, "rev-parse", "--verify", "HEAD"); err == nil {
		return "HEAD", nil
	}
	return s.run(ctx, "mktree")
}

// deletedDir bubbles a deleted path up to its highest missing ancestor, so
// removing a whole skill folder is one change instead of one per file.
func (s *Store) deletedDir(rel string) string {
	dir := filepath.Dir(rel)
	if dir == "." {
		return rel
	}
	if _, err := os.Lstat(filepath.Join(s.Home, dir)); err != nil {
		return s.deletedDir(dir)
	}
	return rel
}

func (s *Store) run(ctx context.Context, args ...string) (string, error) {
	full := append([]string{"--git-dir", s.Dir}, args...)
	cmd := exec.CommandContext(ctx, s.Bin, full...)
	cmd.Dir = s.Home
	cmd.Env = append(os.Environ(), s.Env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.String(), nil
}

// renameSplit reduces a porcelain rename entry `old -> new` to new.
func renameSplit(path string) string {
	if i := strings.Index(path, " -> "); i >= 0 {
		return path[i+4:]
	}
	return path
}

// collapse drops changes nested under another reported change, so a new
// directory is one line instead of one per file.
func collapse(changes []store.Change) []store.Change {
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	var out []store.Change
	for _, c := range changes {
		if n := len(out); n > 0 && strings.HasPrefix(c.Path, out[n-1].Path+"/") {
			continue
		}
		out = append(out, c)
	}
	return out
}

func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimRight(l, "\r"); l != "" {
			out = append(out, l)
		}
	}
	return out
}
