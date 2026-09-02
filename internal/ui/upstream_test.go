package ui

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jegork/skillet/internal/inventory"
	"github.com/jegork/skillet/internal/skill"
	"github.com/jegork/skillet/internal/testhome"
	"github.com/jegork/skillet/internal/upstream"
)

const (
	lockSHA = "1111111111111111111111111111111111111111"
	upSHA   = "2222222222222222222222222222222222222222"
)

// upstreamModel builds the test home with a vendored skill whose lock hash is
// real, an injected upstream check and update command. The caller can seed
// m.inv.Upstream with the states it needs; rebuildItems picks them up.
func newUpstreamModel(t *testing.T) (Model, *[]bool, *[]bool) {
	t.Helper()
	h := testhome.New(t)
	h.Skill("alpha", "does alpha things")
	h.Skill("beta", "does beta things")
	h.Skill("vend", "vendored one")
	staleHash, err := skill.TreeHash(h.SkillsDir() + "/vend")
	if err != nil {
		t.Fatal(err)
	}
	h.LockWithHashes(map[string]string{"vend": "acme/skills"}, map[string]string{"vend": staleHash})
	h.Stub(".claude/skills", "alpha", "../../.agents/skills/alpha")
	h.Readme("| `alpha` | own | does alpha things |", "| `beta` | own | does beta things |", "| `vend` | vendored (acme/skills) | vendored one |")
	inv, err := inventory.Load(h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	inv.Upstream = map[string]upstream.Info{
		"vend": {State: upstream.Outdated, Upstream: upSHA, Lock: staleHash},
		"beta": {State: upstream.Current},
	}
	var updated, checks []bool
	m := New(Config{
		Inventory: inv,
		Load:      func() (inventory.Inventory, error) { return inventory.Load(h.Dir) },
		Consumers: inventory.Consumers(h.Dir),
		Upstream: func(ctx context.Context, force bool) error {
			checks = append(checks, force)
			return nil
		},
		UpdateCmd: func(name string) *exec.Cmd {
			updated = append(updated, true)
			return exec.Command("true")
		},
	})
	return m, &updated, &checks
}

func selectVend(t *testing.T, m Model) Model {
	t.Helper()
	for range 3 {
		if it, ok := m.list.SelectedItem().(item); ok && it.skill.Name == "vend" {
			return m
		}
		next, _ := m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
		m = next.(Model)
	}
	t.Fatal("vend not found in list")
	return m
}

func TestUpstreamKeyForcesCheck(t *testing.T) {
	m, _, checks := newUpstreamModel(t)
	m = press(m, "u")
	if len(*checks) != 1 || !(*checks)[0] {
		t.Fatalf("u must force a check, checks %v", *checks)
	}
	if m.flash != "upstream checked" {
		t.Errorf("flash %q", m.flash)
	}
	if m.upstreamPending {
		t.Error("pending flag stuck")
	}
}

func TestInitChecksUpstreamInBackground(t *testing.T) {
	m, _, checks := newUpstreamModel(t)
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init returned no command")
	}
	out := cmd()
	batch, ok := out.(tea.BatchMsg)
	if !ok {
		t.Fatalf("Init cmd yielded %T", out)
	}
	for _, c := range batch {
		c()
	}
	if len(*checks) != 1 || (*checks)[0] {
		t.Fatalf("background check must not force, checks %v", *checks)
	}
}

func TestUpstreamKeyWithoutConfig(t *testing.T) {
	m := newTestModel(t)
	m = press(m, "u")
	// not configured: the press is ignored, no pending check left behind
	if m.upstreamPending {
		t.Error("pending check started without a hook")
	}
}

func TestUpdateRefusesOwnSkill(t *testing.T) {
	m, updated, _ := newUpstreamModel(t)
	m = press(m, "U")
	if len(*updated) != 0 {
		t.Fatal("update ran for an own skill")
	}
	if !strings.Contains(m.flash, "only global vendored") {
		t.Errorf("flash %q", m.flash)
	}
}

func TestUpdateRefusesUnknownState(t *testing.T) {
	m, updated, _ := newUpstreamModel(t)
	m = selectVend(t, m)
	m.inv.Upstream["vend"] = upstream.Info{State: upstream.Unknown}
	m = press(m, "U")
	if len(*updated) != 0 {
		t.Fatal("update ran while upstream was unknown")
	}
	if !strings.Contains(m.flash, "not outdated") {
		t.Errorf("flash %q", m.flash)
	}
}

func TestUpdateRunsUpdateCmd(t *testing.T) {
	m, updated, _ := newUpstreamModel(t)
	m = selectVend(t, m)
	m = press(m, "U")
	if len(*updated) != 1 {
		t.Fatalf("update command not run")
	}
	if m.mode != modeList {
		t.Errorf("mode %v, want list during exec", m.mode)
	}
}

func TestUpdateDoneReloadsAndFlashes(t *testing.T) {
	m, _, _ := newUpstreamModel(t)
	m = selectVend(t, m)
	// the ExecProcess callback feeds its result straight into Update
	m = apply(m, updateDoneMsg{err: nil, name: "vend"})
	if m.flash != "updated vend" {
		t.Errorf("flash %q", m.flash)
	}
	if it, ok := m.list.SelectedItem().(item); !ok || it.skill.Name != "vend" {
		t.Errorf("selected %T %v", m.list.SelectedItem(), it)
	}
}

func TestUpdateFailureFlashesAndReloads(t *testing.T) {
	m, _, _ := newUpstreamModel(t)
	m = apply(m, updateDoneMsg{err: exec.ErrNotFound, name: "vend"})
	if !strings.Contains(m.flash, "update vend") {
		t.Errorf("flash %q", m.flash)
	}
}

func TestRowMarksOutdated(t *testing.T) {
	d := delegate{styles: newStyles(true), consumers: []string{"claude", "codex", "omp"}, now: func() time.Time { return time.Now() }}
	it := item{
		skill:    skill.Skill{Name: "vend", Origin: skill.Origin{Vendored: true, Source: "acme/skills"}, ModTime: time.Now()},
		enabled:  map[string]bool{},
		upstream: upstream.Outdated,
	}
	row := d.row(it, layout(160, 3), false)
	if got := ansi.StringWidth(row); got > 160 {
		t.Errorf("row is %d cells", got)
	}
	if !strings.Contains(row, "↑") {
		t.Errorf("outdated marker missing in %q", ansi.Strip(row))
	}
	fresh := it
	fresh.upstream = upstream.Current
	if strings.Contains(d.row(fresh, layout(160, 3), false), "↑") {
		t.Error("current skill must not be marked")
	}
}

func TestHeaderHasUpdColumn(t *testing.T) {
	d := delegate{styles: newStyles(true), consumers: []string{"claude", "codex", "omp"}}
	if h := d.header(160); !strings.Contains(ansi.Strip(h), "upd") {
		t.Errorf("header %q", h)
	}
}

func TestPreviewShowsUpstreamHashes(t *testing.T) {
	m, _, _ := newUpstreamModel(t)
	m = selectVend(t, m)
	it, ok := m.list.SelectedItem().(item)
	if !ok {
		t.Fatalf("selected %T", m.list.SelectedItem())
	}
	out := m.skillPreview(it)
	for _, want := range []string{"upstream has changes", "upstream " + upSHA, "lock "} {
		if !strings.Contains(ansi.Strip(out), want) {
			t.Errorf("preview missing %q:\n%s", want, ansi.Strip(out))
		}
	}
}
