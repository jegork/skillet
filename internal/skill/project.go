package skill

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ProjectLock is the project-level skills-lock.json v1. Missing means the
// file is absent, which is not an error: every project skill is then own.
type ProjectLock struct {
	Version int                  `json:"version"`
	Skills  map[string]LockEntry `json:"skills"`
	Missing bool                 `json:"-"`
}

func ReadProjectLock(path string) (ProjectLock, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return ProjectLock{Missing: true}, nil
	}
	if err != nil {
		return ProjectLock{}, err
	}
	var l ProjectLock
	if err := json.Unmarshal(b, &l); err != nil {
		return ProjectLock{}, fmt.Errorf("%s: %w", path, err)
	}
	return l, nil
}

// ContentHash is the computedHash the pnpx skills project lock records:
// sha256 over the dir's regular files, each as relative path immediately
// followed by its contents, no separators, in the order JavaScript's
// localeCompare sorts the paths (case-insensitive first). .git and
// node_modules are skipped like the CLI does. Verified against a real
// skills-lock.json for every entry.
func ContentHash(dir string) (string, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && p != dir && (d.Name() == ".git" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Slice(paths, func(i, j int) bool {
		a, b := strings.ToLower(paths[i]), strings.ToLower(paths[j])
		if a != b {
			return a < b
		}
		return paths[i] < paths[j]
	})
	h := sha256.New()
	for _, rel := range paths {
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return "", err
		}
		h.Write([]byte(rel))
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
