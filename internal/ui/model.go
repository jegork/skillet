// Package ui is the bubbletea front end: one model, a list on the left, a
// preview on the right, a status bar at the bottom.
package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/jegork/skillet/internal/consumer"
	"github.com/jegork/skillet/internal/doctor"
	"github.com/jegork/skillet/internal/inventory"
	"github.com/jegork/skillet/internal/readme"
	"github.com/jegork/skillet/internal/rename"
	"github.com/jegork/skillet/internal/skill"
	"github.com/jegork/skillet/internal/store"
)

type lipglossStyle = lipgloss.Style

type mode int

const (
	modeList mode = iota
	modeDoctor
	modeHelp
	modeSync
	modeRename
	modeRefine
)

type pane int

const (
	paneList pane = iota
	panePreview
)

type Config struct {
	Inventory  inventory.Inventory
	Load       func() (inventory.Inventory, error)
	Store      store.Store // nil disables sync
	Consumers  []consumer.Consumer
	ConfigPath string // editable with E; empty disables it
}
type inventoryMsg struct {
	inv inventory.Inventory
	err error
}
type editorDoneMsg struct{ err error }
type refineDoneMsg struct{ err error }

type Model struct {
	cfg    Config
	inv    inventory.Inventory
	styles styles
	keys   keyMap

	list    list.Model
	preview viewport.Model
	help    help.Model
	dg      delegate

	width, height int
	mode          mode
	focus         pane
	sync          syncState

	status        store.Status
	statusErr     error
	statusPending bool
	flash         string
	lastSelected  string
	renameInput   textinput.Model
	refineAgents  []string
	selectNext    string // skill to select after the next reload
	selectGroup   string // group to select after the next rebuild
	tree          bool   // group skills by origin instead of a flat list
	collapsed     map[string]bool
}

func New(cfg Config) Model {
	m := Model{cfg: cfg, inv: cfg.Inventory, styles: newStyles(true), keys: newKeyMap(), collapsed: map[string]bool{}}
	m.dg = delegate{styles: m.styles, consumers: m.inv.Consumers, now: time.Now, expanded: m.groupExpanded}
	m.list = list.New(nil, m.dg, 0, 0)
	m.list.Filter = treeFilter
	m.list.SetShowTitle(false)
	m.list.SetShowStatusBar(false)
	m.list.SetShowPagination(false)
	m.list.SetShowHelp(false)
	m.list.DisableQuitKeybindings()
	m.list.SetStatusBarItemName("skill", "skills")
	m.preview = viewport.New()
	m.help = help.New()
	m.help.ShowAll = true
	m.renameInput = textinput.New()
	m.renameInput.Prompt = "rename to: "
	m.setInventory(m.inv)
	return m
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{tea.RequestBackgroundColor}
	if m.cfg.Store != nil {
		cmds = append(cmds, loadStatus(m.cfg.Store))
	}
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		return m, nil
	case tea.BackgroundColorMsg:
		m.styles = newStyles(msg.IsDark())
		m.dg.styles = m.styles
		m.list.SetDelegate(m.dg)
		return m, nil
	case inventoryMsg:
		if msg.err != nil {
			m.flash = "reload failed: " + msg.err.Error()
			return m, nil
		}
		cmd := m.setInventory(msg.inv)
		return m, cmd
	case statusMsg:
		m.statusPending = false
		m.status, m.statusErr = msg.status, msg.err
		return m, nil
	case editorDoneMsg:
		if msg.err != nil {
			m.flash = "editor: " + msg.err.Error()
		}
		return m, m.reload()
	case refineDoneMsg:
		if msg.err != nil {
			m.flash = "refine: " + msg.err.Error()
		}
		return m, m.reload()
	case capturedMsg, committedMsg, pushedMsg:
		return m.updateSync(msg)
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m.forward(msg)
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.flash = ""
	if m.mode == modeSync {
		return m.updateSync(msg)
	}
	if m.mode == modeRename {
		return m.updateRename(msg)
	}
	if m.mode == modeRefine {
		return m.updateRefine(msg)
	}
	if m.list.SettingFilter() {
		return m.forward(msg)
	}
	if m.tree && m.mode == modeList && m.focus == paneList {
		if _, ok := m.list.SelectedItem().(groupItem); ok {
			switch {
			case key.Matches(msg, m.keys.Confirm) || msg.String() == "space":
				g := m.list.SelectedItem().(groupItem)
				if m.collapsed[g.key] {
					delete(m.collapsed, g.key)
				} else {
					m.collapsed[g.key] = true
				}
				return m, m.rebuildItems()
			case key.Matches(msg, m.keys.Collapse):
				g := m.list.SelectedItem().(groupItem)
				m.collapsed[g.key] = true
				return m, m.rebuildItems()
			case key.Matches(msg, m.keys.Expand):
				g := m.list.SelectedItem().(groupItem)
				delete(m.collapsed, g.key)
				return m, m.rebuildItems()
			}
		}
		switch {
		case key.Matches(msg, m.keys.Collapse):
			// on a skill row, left collapses the group that owns it
			if it, ok := m.list.SelectedItem().(item); ok {
				m.collapsed[owningGroup(it.skill)] = true
				m.selectGroup = owningGroup(it.skill)
				return m, m.rebuildItems()
			}
		}
	}
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Back):
		if m.mode != modeList {
			m.mode = modeList
			m.refreshPreview()
			return m, nil
		}
		if m.list.IsFiltered() {
			m.list.ResetFilter()
			return m, m.rebuildItems()
		}
		return m, nil
	case key.Matches(msg, m.keys.Help):
		m.toggleMode(modeHelp)
		return m, nil
	case key.Matches(msg, m.keys.Doctor):
		m.toggleMode(modeDoctor)
		return m, nil
	case key.Matches(msg, m.keys.Reload):
		return m, m.reload()
	case key.Matches(msg, m.keys.Readme):
		res, err := readme.Regenerate(m.inv.Paths.Readme(), m.inv.Skills)
		if err != nil {
			m.flash = "README: " + err.Error()
			return m, nil
		}
		m.flash = fmt.Sprintf("README index regenerated: +%d -%d", res.Added, res.Removed)
		return m, m.reload()
	case key.Matches(msg, m.keys.Filter):
		if !m.tree {
			return m.forward(msg)
		}
		// collapsed children must be matchable, so briefly expand everything
		saved := m.collapsed
		m.collapsed = map[string]bool{}
		cmd := m.rebuildItems()
		m.collapsed = saved
		next, fcmd := m.forward(msg)
		return next, tea.Batch(cmd, fcmd)
	case key.Matches(msg, m.keys.Tree):
		m.tree = !m.tree
		return m, m.rebuildItems()
	case key.Matches(msg, m.keys.Focus):
		if m.focus == paneList {
			m.focus = panePreview
		} else {
			m.focus = paneList
		}
		return m, nil
	case key.Matches(msg, m.keys.Edit):
		return m.edit()
	case key.Matches(msg, m.keys.Config):
		return m.editConfig()
	case key.Matches(msg, m.keys.Toggle):
		return m.toggle(msg.String())
	case key.Matches(msg, m.keys.Refine):
		return m.startRefine()
	case key.Matches(msg, m.keys.Rename):
		return m.startRename()
	case key.Matches(msg, m.keys.Sync):
		return m.startSync()
	}
	return m.forward(msg)
}

// forward hands a message to whichever component owns the keyboard.
func (m Model) forward(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if m.focus == panePreview || m.mode != modeList {
		m.preview, cmd = m.preview.Update(msg)
		return m, cmd
	}
	m.list, cmd = m.list.Update(msg)
	if m.selectedName() != m.lastSelected {
		m.refreshPreview()
	}
	return m, cmd
}

func (m *Model) toggleMode(target mode) {
	if m.mode == target {
		m.mode = modeList
	} else {
		m.mode = target
	}
	m.refreshPreview()
}

func (m Model) reload() tea.Cmd {
	load := m.cfg.Load
	cmds := []tea.Cmd{func() tea.Msg {
		inv, err := load()
		return inventoryMsg{inv, err}
	}}
	if m.cfg.Store != nil {
		cmds = append(cmds, loadStatus(m.cfg.Store))
	}
	return tea.Batch(cmds...)
}

func (m *Model) setInventory(inv inventory.Inventory) tea.Cmd {
	m.inv = inv
	return m.rebuildItems()
}

// rebuildItems regenerates the list from m.inv, honouring tree mode and
// collapsed groups, and restores the selection across the rebuild.
func (m *Model) rebuildItems() tea.Cmd {
	selected := m.selectNext
	m.selectNext = ""
	if selected == "" {
		switch sel := m.list.SelectedItem().(type) {
		case item:
			selected = sel.skill.Name
		case groupItem:
			selected = groupMarker + sel.key
		}
	}
	bySkill := doctor.BySkill(m.inv.Findings)
	var items []list.Item
	if !m.tree {
		for _, s := range m.inv.Skills {
			items = append(items, m.makeItem(s, bySkill))
		}
	} else {
		for _, g := range groupSkills(m.inv.Skills) {
			items = append(items, groupItem{key: g.key, children: childNames(g.skills)})
			// collapsed children stay in the list while a filter is active
			if !m.collapsed[g.key] || m.list.SettingFilter() || m.list.IsFiltered() {
				for _, s := range g.skills {
					items = append(items, m.makeItem(s, bySkill))
				}
			}
		}
	}
	cmd := m.list.SetItems(items)
	if !m.list.IsFiltered() {
		if m.selectGroup != "" {
			for i, li := range items {
				if g, ok := li.(groupItem); ok && g.key == m.selectGroup {
					m.list.Select(i)
					break
				}
			}
		} else {
			for i, li := range items {
				switch sel := li.(type) {
				case item:
					if sel.skill.Name == selected {
						m.list.Select(i)
					}
				case groupItem:
					if groupMarker+sel.key == selected {
						m.list.Select(i)
					}
				}
			}
		}
	}
	m.selectGroup = ""
	m.refreshPreview()
	return cmd
}

func (m *Model) makeItem(s skill.Skill, bySkill map[string][]doctor.Finding) item {
	enabled := map[string]bool{}
	for name, rep := range m.inv.Reports {
		enabled[name] = rep.Enabled[s.Name]
	}
	return item{skill: s, enabled: enabled, findings: bySkill[s.Name]}
}

func childNames(skills []skill.Skill) []string {
	names := make([]string, len(skills))
	for i, s := range skills {
		names[i] = s.Name
	}
	return names
}

func owningGroup(s skill.Skill) string {
	if s.Origin.Vendored {
		return s.Origin.Source
	}
	return "own"
}

func (m Model) selectedName() string {
	switch sel := m.list.SelectedItem().(type) {
	case item:
		return sel.skill.Name
	case groupItem:
		return groupMarker + sel.key
	}
	return ""
}

// groupExpanded answers the delegate's collapse marker.
func (m Model) groupExpanded(key string) bool { return !m.collapsed[key] }

func (m *Model) resize() {
	listW := m.width * 55 / 100
	if m.width < 100 {
		listW = m.width
	}
	// rows: column header, list, flash line, status bar
	bodyH := max(m.height-3, 1)
	m.list.SetSize(listW, bodyH)
	m.preview.SetWidth(max(m.width-listW-3, 0))
	m.preview.SetHeight(bodyH + 1)
	m.sync.diff.SetWidth(m.width)
	m.sync.diff.SetHeight(max(m.height-4, 1))
	m.sync.msg.SetWidth(max(m.width-12, 10))
	m.renameInput.SetWidth(max(m.width-14, 10))
	m.help.SetWidth(m.preview.Width())
	m.refreshPreview()
}

func (m *Model) refreshPreview() {
	m.lastSelected = m.selectedName()
	switch m.mode {
	case modeHelp:
		m.preview.SetContent(m.styles.title.Render("keys") + "\n\n" + m.help.View(m.keys))
	case modeDoctor:
		m.preview.SetContent(m.doctorReport())
	default:
		switch sel := m.list.SelectedItem().(type) {
		case item:
			m.preview.SetContent(m.wrap(m.skillPreview(sel)))
		case groupItem:
			m.preview.SetContent(m.wrap(m.groupPreview(sel)))
		default:
			m.preview.SetContent(m.styles.faint.Render("no skill selected"))
		}
	}
	m.preview.GotoTop()
}

func (m Model) groupPreview(g groupItem) string {
	s := m.styles
	var b strings.Builder
	b.WriteString(s.title.Render(g.key) + "\n")
	if g.key == "own" {
		b.WriteString(s.faint.Render("your own skills") + "\n")
	} else {
		b.WriteString(s.faint.Render("vendored from "+g.key) + "\n")
	}
	b.WriteString(fmt.Sprintf("%d skills: %s", len(g.children), strings.Join(g.children, ", ")))
	return b.String()
}

// wrap word-wraps prose to the preview width; the viewport itself only
// breaks lines at the cell boundary.
func (m Model) wrap(s string) string {
	if m.preview.Width() <= 0 {
		return s
	}
	return lipgloss.NewStyle().Width(m.preview.Width()).Render(s)
}

func (m Model) skillPreview(it item) string {
	s := m.styles
	var b strings.Builder
	b.WriteString(s.title.Render(it.skill.Name) + "  " + s.faint.Render(it.skill.Origin.String()) + "\n")
	b.WriteString(s.faint.Render(it.skill.Dir) + "\n")
	var seen, hidden []string
	for _, c := range m.inv.Consumers {
		if it.enabled[c] {
			seen = append(seen, c)
		} else {
			hidden = append(hidden, c)
		}
	}
	b.WriteString("visible to " + s.ok.Render(strings.Join(seen, ", ")))
	if len(hidden) > 0 {
		b.WriteString(s.faint.Render("  hidden from " + strings.Join(hidden, ", ")))
	}
	b.WriteString("\n")
	if len(it.findings) > 0 {
		b.WriteString("\n")
		for _, f := range it.findings {
			b.WriteString(m.dg.severityStyle(f.Severity).Render(f.Severity.String()) + " " + f.Check + ": " + f.Message + "\n")
		}
	}
	b.WriteString("\n" + s.separator.Render(strings.Repeat("─", max(m.preview.Width(), 1))) + "\n")
	body, err := os.ReadFile(filepath.Join(it.skill.Dir, "SKILL.md"))
	if err != nil {
		b.WriteString(s.err.Render("no SKILL.md"))
	} else {
		b.Write(body)
	}
	return b.String()
}

func (m Model) doctorReport() string {
	if len(m.inv.Findings) == 0 {
		return m.styles.ok.Render("doctor: no findings")
	}
	var b strings.Builder
	b.WriteString(m.styles.title.Render("doctor") + "\n\n")
	for _, f := range m.inv.Findings {
		subject := f.Skill
		if subject == "" {
			subject = "global"
		}
		b.WriteString(m.dg.severityStyle(f.Severity).Render(pad(f.Severity.String(), 5)) + " " +
			m.styles.accent.Render(subject) + " " + m.styles.faint.Render(f.Check) + "\n    " + f.Message + "\n")
	}
	return b.String()
}

func (m Model) edit() (tea.Model, tea.Cmd) {
	it, ok := m.list.SelectedItem().(item)
	if !ok {
		return m, nil
	}
	if it.skill.Origin.Vendored {
		m.flash = it.skill.Origin.String() + ": edit disabled, pnpx skills owns this one"
		return m, nil
	}
	return m, tea.ExecProcess(editorCmd(filepath.Join(it.skill.Dir, "SKILL.md")), func(err error) tea.Msg { return editorDoneMsg{err} })
}

// editConfig opens the config file in $VISUAL/$EDITOR.
func (m Model) editConfig() (tea.Model, tea.Cmd) {
	if m.cfg.ConfigPath == "" {
		m.flash = "config: no file to edit"
		return m, nil
	}
	return m, tea.ExecProcess(editorCmd(m.cfg.ConfigPath), func(err error) tea.Msg { return editorDoneMsg{err} })
}

// editorCmd runs the editor through sh so multi-word values like "code -w" work.
func editorCmd(path string) *exec.Cmd {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	return exec.Command("sh", "-c", editor+` "$1"`, "sh", path)
}

// toggle flips the selected skill's visibility for the consumer whose badge
// letter was pressed.
func (m Model) toggle(letter string) (tea.Model, tea.Cmd) {
	it, ok := m.list.SelectedItem().(item)
	if !ok {
		return m, nil
	}
	for _, c := range m.cfg.Consumers {
		if !strings.EqualFold(badge(c.Name()), letter) {
			continue
		}
		var err error
		verb := "enabled"
		if it.enabled[c.Name()] {
			verb = "disabled"
			err = c.Disable(it.skill.Name)
		} else {
			err = c.Enable(it.skill.Name)
		}
		if err != nil {
			m.flash = err.Error()
			return m, nil
		}
		m.flash = fmt.Sprintf("%s: %s %s", it.skill.Name, c.Name(), verb)
		return m, m.reload()
	}
	return m, nil
}

func (m Model) startRename() (tea.Model, tea.Cmd) {
	it, ok := m.list.SelectedItem().(item)
	if !ok {
		return m, nil
	}
	if it.skill.Origin.Vendored {
		m.flash = it.skill.Origin.String() + ": rename disabled, pnpx skills owns this one"
		return m, nil
	}
	m.mode = modeRename
	m.renameInput.SetValue(it.skill.Name)
	m.renameInput.CursorEnd()
	return m, m.renameInput.Focus()
}

func (m Model) updateRename(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		m.mode = modeList
		m.renameInput.Blur()
		return m, nil
	case key.Matches(msg, m.keys.Confirm):
		it, ok := m.list.SelectedItem().(item)
		if !ok {
			m.mode = modeList
			return m, nil
		}
		newName := strings.TrimSpace(m.renameInput.Value())
		m.mode = modeList
		m.renameInput.Blur()
		rep, err := rename.Rename(m.inv.Paths, m.inv.Skills, m.cfg.Consumers, it.skill.Name, newName)
		if err != nil {
			m.flash = "rename: " + err.Error()
			return m, nil
		}
		m.flash = fmt.Sprintf("renamed %s -> %s, %d files rewritten", it.skill.Name, newName, rep.RewrittenFiles)
		if len(rep.VendoredRefs) > 0 {
			m.flash += ", still referenced by vendored " + strings.Join(rep.VendoredRefs, ", ")
		}
		m.selectNext = newName
		return m, m.reload()
	}
	var cmd tea.Cmd
	m.renameInput, cmd = m.renameInput.Update(msg)
	return m, cmd
}

// refineAgentNames are the CLIs refine can launch, probed with lookPath in
// this order.
var refineAgentNames = []string{"claude", "omp", "codex"}

var lookPath = exec.LookPath

func refineAgentChoices() []string {
	var found []string
	for _, name := range refineAgentNames {
		if _, err := lookPath(name); err == nil {
			found = append(found, name)
		}
	}
	return found
}

func (m Model) startRefine() (tea.Model, tea.Cmd) {
	it, ok := m.list.SelectedItem().(item)
	if !ok {
		return m, nil
	}
	if it.skill.Origin.Vendored {
		m.flash = it.skill.Origin.String() + ": refine disabled, pnpx skills owns this one"
		return m, nil
	}
	agents := refineAgentChoices()
	if len(agents) == 0 {
		m.flash = fmt.Sprintf("no agent on PATH (looked for %s)", strings.Join(refineAgentNames, ", "))
		return m, nil
	}
	m.mode = modeRefine
	m.refineAgents = agents
	return m, nil
}

func (m Model) updateRefine(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Back) {
		m.mode = modeList
		return m, nil
	}
	s := msg.String()
	if len(s) != 1 || s[0] < '1' || int(s[0]-'1') >= len(m.refineAgents) {
		return m, nil
	}
	agent := m.refineAgents[s[0]-'1']
	m.mode = modeList
	return m, tea.ExecProcess(refineCmd(agent, m.list.SelectedItem().(item).skill.Dir),
		func(err error) tea.Msg { return refineDoneMsg{err} })
}

// refinePrompt is the message prefilled for the agent, kept in one place.
func refinePrompt(dir string) string {
	return fmt.Sprintf("Refine the skill at %s. Read it fully first, then tighten the "+
		"description trigger line, fix unclear steps, and keep frontmatter valid. "+
		"Do not rename the skill.", filepath.Join(dir, "SKILL.md"))
}

// refineCmd builds the launch command; all three CLIs take the prompt as a
// positional argument.
func refineCmd(agent, dir string) *exec.Cmd {
	c := exec.Command(agent, refinePrompt(dir))
	c.Dir = dir
	return c
}

func (m Model) startSync() (tea.Model, tea.Cmd) {
	if m.cfg.Store == nil {
		m.flash = "no store configured"
		return m, nil
	}
	m.mode = modeSync
	m.sync = newSyncState()
	m.resize()
	return m, captureAndDiff(m.cfg.Store)
}

func (m Model) updateSync(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case capturedMsg:
		if msg.err != nil {
			return m.endSync("capture failed: " + msg.err.Error())
		}
		if strings.TrimSpace(msg.diff) == "" {
			return m.endSync("nothing to commit")
		}
		m.sync.step = syncReview
		m.sync.diff.SetContent(msg.diff)
		m.sync.diff.GotoTop()
		return m, m.sync.msg.Focus()
	case committedMsg:
		if msg.err != nil {
			return m.endSync("commit failed: " + msg.err.Error())
		}
		if !m.sync.push {
			return m.endSync("committed")
		}
		m.sync.step = syncPushing
		return m, push(m.cfg.Store)
	case pushedMsg:
		if msg.err != nil {
			return m.endSync("committed, push failed: " + msg.err.Error())
		}
		return m.endSync("committed and pushed")
	case tea.KeyPressMsg:
		if m.sync.step != syncReview {
			if key.Matches(msg, m.keys.Quit) && msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			return m, nil
		}
		switch {
		case key.Matches(msg, m.keys.Back):
			return m.endSync("sync cancelled, captured changes stay staged")
		case key.Matches(msg, m.keys.TogglePush):
			m.sync.push = !m.sync.push
			return m, nil
		case key.Matches(msg, m.keys.Confirm):
			message := strings.TrimSpace(m.sync.msg.Value())
			if message == "" {
				m.flash = "commit message is empty"
				return m, nil
			}
			m.sync.step = syncCommitting
			return m, commit(m.cfg.Store, message)
		case msg.String() == "up", msg.String() == "down", msg.String() == "pgup", msg.String() == "pgdown", msg.String() == "ctrl+u", msg.String() == "ctrl+d":
			var cmd tea.Cmd
			m.sync.diff, cmd = m.sync.diff.Update(msg)
			return m, cmd
		}
		var cmd tea.Cmd
		m.sync.msg, cmd = m.sync.msg.Update(msg)
		return m, cmd
	}
	var cmd tea.Cmd
	m.sync.msg, cmd = m.sync.msg.Update(msg)
	return m, cmd
}

func (m Model) endSync(flash string) (tea.Model, tea.Cmd) {
	m.mode = modeList
	m.flash = flash
	m.sync.msg.Blur()
	m.refreshPreview()
	return m, m.reload()
}

func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m Model) render() string {
	if m.width == 0 {
		return ""
	}
	if m.mode == modeSync {
		return m.renderSync()
	}
	listView := m.dg.header(m.list.Width()) + "\n" + strings.TrimRight(m.list.View(), "\n")
	body := listView
	if m.preview.Width() > 0 {
		sep := m.styles.separator.Render(strings.TrimRight(strings.Repeat("│\n", m.preview.Height()), "\n"))
		body = lipgloss.JoinHorizontal(lipgloss.Top, listView, " "+sep+" ", m.preview.View())
	}
	body = lipgloss.NewStyle().MaxHeight(max(m.height-2, 1)).Render(body)
	return lipgloss.JoinVertical(lipgloss.Left, body, m.flashLine(), m.statusBar())
}

func (m Model) renderSync() string {
	s := m.styles
	var head string
	switch m.sync.step {
	case syncCapturing:
		head = "capturing changes into the store…"
	case syncReview:
		push := s.faint.Render("push off")
		if m.sync.push {
			push = s.ok.Render("push on")
		}
		head = s.title.Render("review") + "  " + push + "  " + s.faint.Render("enter commit · ctrl+p toggle push · esc cancel · ↑↓ scroll")
	case syncCommitting:
		head = "committing…"
	case syncPushing:
		head = "pushing…"
	}
	return lipgloss.JoinVertical(lipgloss.Left, head, m.sync.diff.View(), m.sync.msg.View(), m.flashLine())
}

func (m Model) flashLine() string {
	if m.mode == modeRename {
		return m.renameInput.View()
	}
	if m.mode == modeRefine {
		parts := []string{"refine with:"}
		for i, a := range m.refineAgents {
			parts = append(parts, fmt.Sprintf("%d %s", i+1, a))
		}
		return m.styles.flash.Render(pad(strings.Join(parts, " · ")+" · esc cancel", m.width))
	}
	if m.flash == "" {
		return ""
	}
	return m.styles.flash.Render(pad(m.flash, m.width))
}

func (m Model) statusBar() string {
	s := m.styles
	var parts []string
	switch {
	case m.cfg.Store == nil:
		parts = append(parts, "store: none")
	case m.statusPending:
		parts = append(parts, "store: …")
	case m.statusErr != nil:
		parts = append(parts, s.err.Render("store: "+firstLine(m.statusErr.Error())))
	default:
		st := m.status
		cap := s.ok.Render("clean")
		if !st.Clean() {
			cap = s.warn.Render(fmt.Sprintf("%d uncaptured · %d uncommitted", len(st.Uncaptured), len(st.Uncommitted)))
		}
		ahead := ""
		if st.Ahead > 0 {
			ahead = fmt.Sprintf(" +%d", st.Ahead)
		} else if st.Ahead < 0 {
			ahead = " (no upstream)"
		}
		parts = append(parts, "store: "+cap+" · "+st.Branch+ahead)
	}
	warn, errs := 0, 0
	for _, f := range m.inv.Findings {
		switch f.Severity {
		case doctor.Warn:
			warn++
		case doctor.Error:
			errs++
		}
	}
	dr := s.ok.Render("ok")
	if warn+errs > 0 {
		dr = fmt.Sprintf("%s %s", s.warn.Render(fmt.Sprintf("%d warn", warn)), s.err.Render(fmt.Sprintf("%d err", errs)))
	}
	parts = append(parts, "doctor: "+dr)
	parts = append(parts, fmt.Sprintf("%d/%d", visibleSkills(m.list), len(m.inv.Skills)))
	parts = append(parts, m.help.ShortHelpView(m.keys.ShortHelp()))
	return s.statusBar.MaxWidth(max(m.width, 1)).Render(strings.Join(parts, "  │  "))
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
