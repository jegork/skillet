package ui

import (
	"os"
	"testing"
)

// the CI runner exports XDG_CONFIG_HOME, which would point config loading at
// the runner's real config path instead of each test's temp home
func TestMain(m *testing.M) {
	os.Unsetenv("XDG_CONFIG_HOME")
	os.Exit(m.Run())
}
