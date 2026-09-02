package consumer_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jegork/skillet/internal/consumer"
	"github.com/jegork/skillet/internal/testhome"
)

func TestNativeReport(t *testing.T) {
	h := testhome.New(t)
	bare := filepath.Join(h.Dir, "proj", ".claude", "skills")
	for _, n := range []string{"alpha", "beta"} {
		if err := os.MkdirAll(filepath.Join(bare, n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(bare, "beta", "SKILL.md"), []byte("---\nname: beta\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bare, ".hidden"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := consumer.NewNative("claude", bare)
	if c.Name() != "claude" {
		t.Errorf("name %q", c.Name())
	}
	rep, err := c.Report(skills("alpha", "beta", "global-only"))
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Enabled["alpha"] || !rep.Enabled["beta"] {
		t.Errorf("enabled: %v", rep.Enabled)
	}
	if rep.Enabled[".hidden"] || rep.Enabled["global-only"] {
		t.Errorf("unexpected entries enabled: %v", rep.Enabled)
	}
	if len(rep.Stubs) != 0 {
		t.Errorf("native dirs have no stubs: %v", rep.Stubs)
	}
}

func TestNativeMissingDir(t *testing.T) {
	c := consumer.NewNative("claude", filepath.Join(t.TempDir(), "nope"))
	rep, err := c.Report(skills("a"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Enabled) != 0 {
		t.Errorf("enabled: %v", rep.Enabled)
	}
}

func TestNativeToggleRefused(t *testing.T) {
	h := testhome.New(t)
	bare := filepath.Join(h.Dir, "proj", ".claude", "skills")
	if err := os.MkdirAll(filepath.Join(bare, "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := consumer.NewNative("claude", bare)
	err := c.Enable("alpha")
	if err == nil || !strings.Contains(err.Error(), "native") {
		t.Errorf("Enable = %v, want a native-dir error", err)
	}
	err = c.Disable("alpha")
	if err == nil || !strings.Contains(err.Error(), "native") {
		t.Errorf("Disable = %v, want a native-dir error", err)
	}
}
