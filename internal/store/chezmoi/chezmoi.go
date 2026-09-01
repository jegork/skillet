// Package chezmoi implements store.Store on top of the chezmoi CLI: $HOME is
// the destination, the chezmoi source dir is the working copy, its git repo
// is the history.
package chezmoi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
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
	Roots []store.Root
	// Args are extra global flags, e.g. --source/--destination for tests.
	Args []string
	// Env is appended to the inherited environment.
	Env []string
}

func New(home string, roots []store.Root) *Store {
	return &Store{Bin: "chezmoi", Home: home, Roots: roots}
}

var _ store.Store = (*Store)(nil)

func (s *Store) Status(ctx context.Context) (store.Status, error) {
	var st store.Status
	managed, err := s.managed(ctx)
	if err != nil {
		return st, err
	}
	var managedRoots []string
	for _, r := range s.Roots {
		if managed[s.abs(r)] {
			managedRoots = append(managedRoots, s.abs(r))
		}
	}
	if len(managedRoots) > 0 {
		out, err := s.run(ctx, append([]string{"status", "--path-style=relative"}, managedRoots...)...)
		if err != nil {
			return st, err
		}
		for _, line := range lines(out) {
			if len(line) < 4 {
				continue
			}
			var kind store.ChangeKind
			switch line[1] {
			case 'M':
				kind = store.Modified
			case 'A':
				kind = store.Deleted
			case 'D':
				kind = store.Added
			default:
				continue
			}
			st.Uncaptured = append(st.Uncaptured, store.Change{Path: line[3:], Kind: kind})
		}
	}
	if present := s.presentRoots(); len(present) > 0 {
		out, err := s.run(ctx, append([]string{"unmanaged", "--path-style=relative"}, present...)...)
		if err != nil {
			return st, err
		}
		for _, p := range lines(out) {
			if !s.excluded(p) {
				st.Uncaptured = append(st.Uncaptured, store.Change{Path: p, Kind: store.Added})
			}
		}
	}

	st.Uncaptured = collapse(st.Uncaptured)

	src, err := s.sourcePaths(ctx, managedRoots)
	if err != nil {
		return st, err
	}
	if len(src) > 0 {
		out, err := s.git(ctx, append([]string{"status", "--porcelain", "--"}, src...)...)
		if err != nil {
			return st, err
		}
		for _, line := range lines(out) {
			if len(line) > 3 {
				st.Uncommitted = append(st.Uncommitted, line[3:])
			}
		}
	}
	branch, err := s.git(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return st, err
	}
	st.Branch = strings.TrimSpace(branch)
	st.Ahead = -1
	if out, err := s.git(ctx, "rev-list", "--count", "@{u}..HEAD"); err == nil {
		st.Ahead, _ = strconv.Atoi(strings.TrimSpace(out))
	}
	return st, nil
}

func (s *Store) Capture(ctx context.Context) error {
	for _, r := range s.Roots {
		abs := s.abs(r)
		info, err := os.Lstat(abs)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		targets := []string{abs}
		if info.IsDir() {
			// never re-add the root dir itself: chezmoi 2.72 drops its exact_
			// attribute on re-add, adding children keeps the parent as is
			entries, err := os.ReadDir(abs)
			if err != nil {
				return err
			}
			targets = targets[:0]
			for _, e := range entries {
				if !contains(r.Exclude, e.Name()) {
					targets = append(targets, filepath.Join(abs, e.Name()))
				}
			}
		}
		if len(targets) > 0 {
			if _, err := s.run(ctx, append([]string{"add"}, targets...)...); err != nil {
				return err
			}
		}
	}
	managed, err := s.managed(ctx)
	if err != nil {
		return err
	}
	var gone []string
	for p := range managed {
		if !s.underRoot(p) {
			continue
		}
		if _, err := os.Lstat(p); errors.Is(err, fs.ErrNotExist) {
			gone = append(gone, p)
		}
	}
	if len(gone) > 0 {
		// deepest first so a dir is forgotten after its children
		sort.Sort(sort.Reverse(sort.StringSlice(gone)))
		if _, err := s.run(ctx, append([]string{"forget", "--force"}, gone...)...); err != nil {
			return err
		}
	}
	src, err := s.currentSourcePaths(ctx)
	if err != nil {
		return err
	}
	if len(src) == 0 {
		return nil
	}
	_, err = s.git(ctx, append([]string{"add", "-A", "--"}, src...)...)
	return err
}

func (s *Store) Diff(ctx context.Context) (string, error) {
	src, err := s.currentSourcePaths(ctx)
	if err != nil || len(src) == 0 {
		return "", err
	}
	stat, err := s.git(ctx, append([]string{"diff", "HEAD", "--stat", "--"}, src...)...)
	if err != nil {
		return "", err
	}
	full, err := s.git(ctx, append([]string{"diff", "HEAD", "--"}, src...)...)
	if err != nil {
		return "", err
	}
	if stat == "" {
		return "", nil
	}
	return stat + "\n" + full, nil
}

func (s *Store) Commit(ctx context.Context, message string) error {
	src, err := s.currentSourcePaths(ctx)
	if err != nil {
		return err
	}
	if len(src) == 0 {
		return errors.New("nothing is managed yet")
	}
	_, err = s.git(ctx, append([]string{"commit", "-m", message, "--"}, src...)...)
	return err
}

func (s *Store) Push(ctx context.Context) error {
	_, err := s.git(ctx, "push")
	return err
}

func (s *Store) abs(r store.Root) string { return filepath.Join(s.Home, r.Rel) }

// presentRoots returns the absolute roots that exist in $HOME right now.
func (s *Store) presentRoots() []string {
	var out []string
	for _, r := range s.Roots {
		if _, err := os.Lstat(s.abs(r)); err == nil {
			out = append(out, s.abs(r))
		}
	}
	return out
}

func (s *Store) underRoot(abs string) bool {
	for _, r := range s.Roots {
		root := s.abs(r)
		if abs == root || strings.HasPrefix(abs, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// excluded reports whether a $HOME-relative path is an excluded child of a root.
func (s *Store) excluded(rel string) bool {
	for _, r := range s.Roots {
		for _, ex := range r.Exclude {
			if rel == filepath.Join(r.Rel, ex) {
				return true
			}
		}
	}
	return false
}

// managed returns every absolute path chezmoi currently manages.
func (s *Store) managed(ctx context.Context) (map[string]bool, error) {
	out, err := s.run(ctx, "managed", "--path-style=absolute")
	if err != nil {
		return nil, err
	}
	m := map[string]bool{}
	for _, p := range lines(out) {
		m[p] = true
	}
	return m, nil
}

// currentSourcePaths resolves the source-relative path of every root that is
// managed right now.
func (s *Store) currentSourcePaths(ctx context.Context) ([]string, error) {
	managed, err := s.managed(ctx)
	if err != nil {
		return nil, err
	}
	var roots []string
	for _, r := range s.Roots {
		if managed[s.abs(r)] {
			roots = append(roots, s.abs(r))
		}
	}
	return s.sourcePaths(ctx, roots)
}

func (s *Store) sourcePaths(ctx context.Context, absRoots []string) ([]string, error) {
	if len(absRoots) == 0 {
		return nil, nil
	}
	srcDir, err := s.run(ctx, "source-path")
	if err != nil {
		return nil, err
	}
	srcDir = strings.TrimSpace(srcDir)
	out, err := s.run(ctx, append([]string{"source-path"}, absRoots...)...)
	if err != nil {
		return nil, err
	}
	var rel []string
	for _, p := range lines(out) {
		r, err := filepath.Rel(srcDir, p)
		if err != nil {
			return nil, err
		}
		rel = append(rel, r)
	}
	return rel, nil
}

func (s *Store) git(ctx context.Context, args ...string) (string, error) {
	return s.run(ctx, append([]string{"git", "--"}, args...)...)
}

func (s *Store) run(ctx context.Context, args ...string) (string, error) {
	full := append([]string{"--no-pager", "--no-tty", "--color=false"}, s.Args...)
	full = append(full, args...)
	cmd := exec.CommandContext(ctx, s.Bin, full...)
	cmd.Env = append(os.Environ(), s.Env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("chezmoi %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.String(), nil
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

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
