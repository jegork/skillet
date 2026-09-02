package consumer

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/jegork/skillet/internal/skill"
)

// Native is a consumer that reads a skills dir directly, with no stub layer:
// every skill folder in the dir is visible. Visibility cannot be toggled.
type Native struct {
	name, dir string
}

func NewNative(name, dir string) *Native { return &Native{name: name, dir: dir} }

func (c *Native) Name() string { return c.name }

func (c *Native) Report(_ []skill.Skill) (Report, error) {
	rep := Report{Enabled: map[string]bool{}}
	entries, err := os.ReadDir(c.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return rep, nil
	}
	if err != nil {
		return rep, err
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		rep.Enabled[e.Name()] = true
	}
	return rep, nil
}

func (c *Native) Enable(string) error {
	return fmt.Errorf("%s: %s is a native dir, toggling is not possible", c.name, c.dir)
}

func (c *Native) Disable(string) error {
	return fmt.Errorf("%s: %s is a native dir, toggling is not possible", c.name, c.dir)
}

func (c *Native) Forget(name string) error { return c.Disable(name) }
