package ui

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jegork/skillet/internal/inventory"
	"github.com/jegork/skillet/internal/skill"
	"github.com/jegork/skillet/internal/testhome"
)

func newTestModel(t *testing.T) Model {
	t.Helper()
	h := testhome.New(t)
	h.Skill("alpha", "does alpha things")
	h.Skill("beta", "does beta things")
	h.Skill("vend", "vendored one")
	h.Lock(map[string]string{"vend": "acme/skills"})
	h.Stub(".claude/skills", "alpha", "../../.agents/skills/alpha")
	h.Stub(".claude/skills", "gone", "../../.agents/skills/gone")
	h.Readme("| `alpha` | own | does alpha things |", "| `beta` | own | does beta things |", "| `vend` | vendored (acme/skills) | vendored one |")
	inv, err := inventory.Load(h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	m := New(Config{
		Inventory: inv,
		Load:      func() (inventory.Inventory, error) { return inventory.Load(h.Dir) },
		Consumers: inventory.Consumers(h.Dir),
	})
	return m
}

func press(m Model, keys ...string) Model {
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		default:
			msg = tea.KeyPressMsg{Code: rune(k[0]), Text: k}
		}
		m = apply(m, msg)
	}
	return m
}

func TestEditConfigKey(t *testing.T) {
	m := newTestModel(t)
	m.cfg.ConfigPath = ""
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'E', Text: "E"})
	m = next.(Model)
	if cmd != nil {
		t.Error("missing config path must not launch an editor")
	}
	if !strings.Contains(m.flash, "no file") {
		t.Errorf("flash %q", m.flash)
	}
}

func TestEditConfigLaunchesEditor(t *testing.T) {
	m := newTestModel(t)
	m.cfg.ConfigPath = filepath.Join(t.TempDir(), "config.yml")
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'E', Text: "E"})
	if cmd == nil {
		t.Fatal("edit config must launch an editor")
	}
	if m.flash != "" {
		t.Errorf("flash %q", m.flash)
	}
	_ = next
}

// apply runs Update and feeds back the data messages produced by returned
// commands. Cursor blink ticks are dropped: they would loop forever.
func apply(m Model, msg tea.Msg) Model {
	next, cmd := m.Update(msg)
	m = next.(Model)
	if cmd == nil {
		return m
	}
	var feed func(tea.Msg)
	feed = func(out tea.Msg) {
		switch out := out.(type) {
		case list.FilterMatchesMsg, inventoryMsg, statusMsg,
			searchDoneMsg, installDoneMsg, installedMsg, upstreamDoneMsg, updateDoneMsg, updatedMsg, removedMsg:
			m = apply(m, out)
		case tea.BatchMsg:
			for _, c := range out {
				if c != nil {
					feed(c())
				}
			}
		}
	}
	feed(cmd())
	return m
}

func view(m Model) string { return m.View().Content }

func TestListRendersSkillsAndStatus(t *testing.T) {
	m := newTestModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = next.(Model)
	out := view(m)
	for _, want := range []string{"alpha", "beta", "vend", "acme/skills", "store: none", "doctor:", "does alpha things"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}
	// the dir path may wrap anywhere inside the preview pane, so check the
	// pane's own content without whitespace or styling
	preview := strings.NewReplacer("\n", "", " ", "").Replace(ansi.Strip(m.preview.GetContent()))
	if want := filepath.Join(".agents", "skills", "alpha"); !strings.Contains(preview, want) {
		t.Errorf("preview missing dir %q:\n%s", want, m.preview.GetContent())
	}
	lines := strings.Split(out, "\n")
	if len(lines) != 40 {
		t.Errorf("view has %d lines, want 40", len(lines))
	}
	if !strings.Contains(lines[len(lines)-1], "store: none") {
		t.Errorf("status bar must be the last line, got %q", lines[len(lines)-1])
	}
}

func TestFilterNarrowsList(t *testing.T) {
	m := newTestModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = press(next.(Model), "/", "b", "e", "t", "enter")
	if got := len(m.list.VisibleItems()); got != 1 {
		t.Fatalf("visible items %d, want 1", got)
	}
	if !strings.Contains(view(m), "1/3") {
		t.Errorf("status bar should show 1/3:\n%s", view(m))
	}
	if !strings.Contains(view(m), "does beta things") || m.lastSelected != "beta" {
		t.Errorf("preview should follow the filtered selection, last=%q", m.lastSelected)
	}
	m = press(m, "esc")
	if got := len(m.list.VisibleItems()); got != 3 {
		t.Errorf("after esc visible %d, want 3", got)
	}
}

func TestEditRefusesVendored(t *testing.T) {
	m := newTestModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = press(next.(Model), "j", "j") // alpha, beta, vend
	if it := m.list.SelectedItem().(item); it.skill.Name != "vend" {
		t.Fatalf("selected %s", it.skill.Name)
	}
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	m = next.(Model)
	if cmd != nil {
		t.Error("vendored edit must not launch an editor")
	}
	if !strings.Contains(m.flash, "edit disabled") {
		t.Errorf("flash %q", m.flash)
	}
}

func TestDoctorModeShowsFindings(t *testing.T) {
	m := newTestModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = press(next.(Model), "d")
	if out := view(m); !strings.Contains(out, "dangling symlink") || !strings.Contains(out, "gone") {
		t.Errorf("doctor view missing stub finding:\n%s", out)
	}
	m = press(m, "d")
	if out := view(m); strings.Contains(out, "dangling symlink") {
		t.Error("doctor view should close on second d")
	}
}

func TestSyncWithoutStore(t *testing.T) {
	m := newTestModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = press(next.(Model), "s")
	if m.mode != modeList || !strings.Contains(m.flash, "no store") {
		t.Errorf("mode %v flash %q", m.mode, m.flash)
	}
}

func TestToggleConsumers(t *testing.T) {
	m := newTestModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = next.(Model) // alpha selected: claude on, codex off, omp on

	m = press(m, "x")
	it := m.list.SelectedItem().(item)
	if !it.enabled["codex"] || !strings.Contains(m.flash, "codex enabled") {
		t.Errorf("codex not enabled: %v flash %q", it.enabled, m.flash)
	}
	m = press(m, "o", "c")
	it = m.list.SelectedItem().(item)
	if it.enabled["omp"] || it.enabled["claude"] {
		t.Errorf("omp/claude still enabled: %v", it.enabled)
	}
	m = press(m, "o")
	if it = m.list.SelectedItem().(item); !it.enabled["omp"] {
		t.Errorf("omp not re-enabled: %v", it.enabled)
	}
}

func TestRenameFlow(t *testing.T) {
	m := newTestModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = press(next.(Model), "n")
	if m.mode != modeRename {
		t.Fatalf("mode %v", m.mode)
	}
	// input is prefilled with "alpha"; append "2" and confirm
	m = press(m, "2", "enter")
	if m.mode != modeList || !strings.HasPrefix(m.flash, "renamed alpha -> alpha2") {
		t.Fatalf("mode %v flash %q", m.mode, m.flash)
	}
	if it := m.list.SelectedItem().(item); it.skill.Name != "alpha2" {
		t.Errorf("selected %q after rename", it.skill.Name)
	}
	m = press(m, "j", "j", "n") // vend
	if m.mode != modeList || !strings.Contains(m.flash, "rename disabled") {
		t.Errorf("vendored rename: mode %v flash %q", m.mode, m.flash)
	}
	m = press(m, "k", "n", "esc")
	if m.mode != modeList {
		t.Errorf("esc should cancel rename, mode %v", m.mode)
	}
}

func TestRefineRefusesVendored(t *testing.T) {
	m := newTestModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = press(next.(Model), "j", "j", "p")
	if m.mode != modeList || !strings.Contains(m.flash, "refine disabled") {
		t.Errorf("vendored refine: mode %v flash %q", m.mode, m.flash)
	}
}

func TestRefineAgentPicker(t *testing.T) {
	orig := lookPath
	t.Cleanup(func() { lookPath = orig })
	lookPath = func(name string) (string, error) {
		if name == "claude" || name == "omp" {
			return "/bin/" + name, nil
		}
		return "", exec.ErrNotFound
	}
	m := newTestModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = press(next.(Model), "p")
	if m.mode != modeRefine {
		t.Fatalf("mode %v, want refine picker", m.mode)
	}
	if line := m.flashLine(); !strings.Contains(line, "1 claude") || !strings.Contains(line, "2 omp") || strings.Contains(line, "codex") {
		t.Errorf("picker should offer only agents on PATH: %q", line)
	}
	m = press(m, "esc")
	if m.mode != modeList {
		t.Errorf("esc should cancel picker, mode %v", m.mode)
	}
}

func TestRefineRunsOnPick(t *testing.T) {
	orig := lookPath
	t.Cleanup(func() { lookPath = orig })
	lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	m := newTestModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = press(next.(Model), "p")
	if m.mode != modeList || !strings.Contains(m.flash, "no agent") {
		t.Errorf("no agents on PATH: mode %v flash %q", m.mode, m.flash)
	}
}

func TestRefineDoneReloads(t *testing.T) {
	m := newTestModel(t)
	m = apply(m, refineDoneMsg{errors.New("exit status 1")})
	if !strings.Contains(m.flash, "refine: exit status 1") {
		t.Errorf("flash %q, want refine error", m.flash)
	}
}

func TestRefineCommand(t *testing.T) {
	dir := "/home/u/.agents/skills/alpha"
	for _, agent := range []string{"claude", "omp", "codex"} {
		c := refineCmd(agent, dir)
		if c.Dir != dir {
			t.Errorf("%s: Dir %q, want %q", agent, c.Dir, dir)
		}
		if c.Args[0] != agent {
			t.Errorf("%s: Args[0] %q", agent, c.Args[0])
		}
		if len(c.Args) != 2 {
			t.Fatalf("%s: args %v, want agent + prompt", agent, c.Args)
		}
		want := "Refine the skill at " + filepath.Join(dir, "SKILL.md") +
			". Read it fully first, then tighten the description trigger line, " +
			"fix unclear steps, and keep frontmatter valid. Do not rename the skill."
		if c.Args[1] != want {
			t.Errorf("%s: prompt %q, want %q", agent, c.Args[1], want)
		}
	}
}

func TestDeleteConfirmFlow(t *testing.T) {
	m := newTestModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = press(next.(Model), "D")
	if !m.confirmDelete {
		t.Fatalf("D must arm the confirm, flash %q", m.flash)
	}
	if !strings.Contains(view(m), "delete alpha?") {
		t.Errorf("flash line %q, want the prompt", view(m))
	}
	m = press(m, "n")
	if m.confirmDelete {
		t.Error("n must cancel the confirm")
	}
	if _, err := os.Stat(filepath.Join(m.inv.Paths.SkillsDir(), "alpha")); err != nil {
		t.Errorf("alpha deleted by cancelled confirm: %v", err)
	}
	// re-arm and cancel with esc, then confirm with y
	m = press(m, "D", "esc")
	if m.confirmDelete {
		t.Error("esc must cancel the confirm")
	}
	m = press(m, "D", "y")
	if m.flash != "deleted alpha" {
		t.Fatalf("flash %q, want deleted alpha", m.flash)
	}
	if _, err := os.Stat(filepath.Join(m.inv.Paths.SkillsDir(), "alpha")); !os.IsNotExist(err) {
		t.Errorf("alpha still there: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(m.inv.Paths.Home, ".claude", "skills", "alpha")); !os.IsNotExist(err) {
		t.Errorf("claude stub still there: %v", err)
	}
	if it := m.list.SelectedItem().(item); it.skill.Name != "beta" {
		t.Errorf("selected %q, want the neighbour beta", it.skill.Name)
	}
}

func TestDeleteKeepsSelectionOnNeighbour(t *testing.T) {
	m := newTestModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = press(next.(Model), "j", "D", "y") // beta, the middle skill
	if m.flash != "deleted beta" {
		t.Fatalf("flash %q", m.flash)
	}
	if it := m.list.SelectedItem().(item); it.skill.Name != "vend" {
		t.Errorf("selected %q, want the next neighbour vend", it.skill.Name)
	}
}

func TestDeleteVendoredRunsRemoveCmd(t *testing.T) {
	m := newTestModel(t)
	var removed []string
	m.cfg.RemoveCmd = func(name string) *exec.Cmd {
		removed = append(removed, name)
		// mimic the CLI: drop the folder; Cleanup drops omp + README row
		os.RemoveAll(filepath.Join(m.inv.Paths.SkillsDir(), name))
		lock, _ := skill.ReadLock(filepath.Join(m.inv.Paths.Home, ".agents", ".skill-lock.json"))
		delete(lock.Skills, name)
		b, _ := json.MarshalIndent(lock, "", "  ")
		os.WriteFile(filepath.Join(m.inv.Paths.Home, ".agents", ".skill-lock.json"), append(b, '\n'), 0o644)
		return exec.Command("true")
	}
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = press(next.(Model), "j", "j", "D", "y") // vend
	if got := len(removed); got != 1 || removed[0] != "vend" {
		t.Fatalf("RemoveCmd calls %v", removed)
	}
	if _, ok := m.list.SelectedItem().(item); !ok {
		t.Fatalf("selection lost during exec")
	}
	// the ExecProcess callback feeds its result straight into Update
	m = apply(m, removedMsg{err: nil, name: "vend"})
	if m.flash != "deleted vend" {
		t.Fatalf("flash %q", m.flash)
	}
	if _, err := os.Stat(filepath.Join(m.inv.Paths.SkillsDir(), "vend")); !os.IsNotExist(err) {
		t.Error("vend still there")
	}
	lock, err := skill.ReadLock(filepath.Join(m.inv.Paths.Home, ".agents", ".skill-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := lock.Skills["vend"]; ok {
		t.Error("lock entry still there")
	}
}

func TestDeleteDoneReloadsAndFlashes(t *testing.T) {
	m := newTestModel(t)
	m = apply(m, removedMsg{err: nil, name: "vend"})
	if m.flash != "deleted vend" {
		t.Errorf("flash %q", m.flash)
	}
	m = apply(m, removedMsg{err: exec.ErrNotFound, name: "vend"})
	if !strings.Contains(m.flash, "remove vend") {
		t.Errorf("flash %q, want remove error", m.flash)
	}
}

func TestDeleteVendoredWithoutRemoveCmd(t *testing.T) {
	m := newTestModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = press(next.(Model), "j", "j", "D") // vend, no RemoveCmd
	if m.flash != "delete not configured" || m.confirmDelete {
		t.Errorf("flash %q confirmDelete %v", m.flash, m.confirmDelete)
	}
}

func TestQuitKey(t *testing.T) {
	m := newTestModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = next.(Model)
	for _, k := range []tea.KeyPressMsg{{Code: 'q', Text: "q"}, {Code: 'c', Mod: tea.ModCtrl}} {
		_, cmd := m.Update(k)
		if cmd == nil {
			t.Fatalf("%s: no command returned", k)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("%s: expected tea.QuitMsg", k)
		}
	}
}
