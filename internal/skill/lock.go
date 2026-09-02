package skill

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"
)

type LockEntry struct {
	Source          string    `json:"source"`
	SourceType      string    `json:"sourceType"`
	SourceURL       string    `json:"sourceUrl"`
	SkillPath       string    `json:"skillPath"`
	SkillFolderHash string    `json:"skillFolderHash"`
	ComputedHash    string    `json:"computedHash"` // project lock v1: sha256 of the skill folder, see ContentHash
	InstalledAt     time.Time `json:"installedAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// Lock is the pnpx skills lock file. Missing means the file is absent, which
// is not an error: every skill is then own.
type Lock struct {
	Version int                  `json:"version"`
	Skills  map[string]LockEntry `json:"skills"`
	Missing bool                 `json:"-"`
}

func ReadLock(path string) (Lock, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Lock{Missing: true}, nil
	}
	if err != nil {
		return Lock{}, err
	}
	var l Lock
	if err := json.Unmarshal(b, &l); err != nil {
		return Lock{}, fmt.Errorf("%s: %w", path, err)
	}
	return l, nil
}
