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
)

// ProjectLock is the project-level skills-lock.json v1. Missing means the
// file is absent, which is not an error: every project skill is then own.
type ProjectLock struct {
	Version int `json:"version"`
	// sha256 over the skills dir's files in sorted relative path order,
	// as computed by ContentHash.
	ComputedHash string               `json:"computedHash"`
	Skills       map[string]LockEntry `json:"skills"`
	Missing      bool                 `json:"-"`
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

// ContentHash hashes a skill dir as sha256 over its regular files in sorted
// relative path order: path, NUL, contents, newline per file. This is the
// computedHash the project lock records.
func ContentHash(dir string) (string, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, rel := range paths {
		b, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			return "", err
		}
		h.Write([]byte(rel))
		h.Write([]byte{0})
		h.Write(b)
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
