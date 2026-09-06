package ui

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

	"github.com/jegork/skillet/internal/doctor"
	"github.com/jegork/skillet/internal/skill"
	"github.com/jegork/skillet/internal/upstream"
)

type item struct {
	skill    skill.Skill
	enabled  map[string]bool // consumer name -> sees this skill
	findings []doctor.Finding
	upstream upstream.State // Unknown until the background check lands
}

func (i item) FilterValue() string { return i.skill.Name }

type columns struct {
	name, origin, upd, consumers, doctor, modified, description int
}

// layout splits a row width into columns; description absorbs the rest and
// disappears on narrow panes.
func layout(width int, consumers int) columns {
	c := columns{name: 26, origin: 22, upd: 3, consumers: consumers + 1, doctor: 3, modified: 4}
	base := func() int {
		return 2 + c.name + 1 + c.origin + 1 + c.upd + 1 + c.consumers + 1 + c.doctor + 1 + c.modified
	}
	for base() > width && c.upd > 0 {
		c.upd-- // on very narrow panes the upstream marker gives way first
	}
	for base() > width && c.origin > 8 {
		c.origin--
	}
	for base() > width && c.name > 12 {
		c.name--
	}
	if rest := width - base() - 1; rest >= 12 {
		c.description = rest
	}
	return c
}

type delegate struct {
	styles    styles
	consumers []string
	now       func() time.Time
	expanded  func(string) bool // nil in flat mode: no group rows to render
}

func (d delegate) Height() int                         { return 1 }
func (d delegate) Spacing() int                        { return 0 }
func (d delegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d delegate) Render(w io.Writer, m list.Model, index int, li list.Item) {
	c := layout(m.Width(), len(d.consumers))
	if g, ok := li.(groupItem); ok {
		fmt.Fprint(w, d.groupRow(g, index == m.Index()))
		return
	}
	if e, ok := li.(exploreItem); ok {
		fmt.Fprint(w, d.exploreRow(e, index == m.Index()))
		return
	}
	it, ok := li.(item)
	if !ok {
		return
	}
	fmt.Fprint(w, d.row(it, c, index == m.Index()))
}

func (d delegate) groupRow(g groupItem, selected bool) string {
	marker, name := "▸", g.label
	if d.expanded != nil && d.expanded(g.key) {
		marker = "▾"
	}
	cursor, style := "  ", d.styles.accent
	if selected {
		cursor = d.styles.selected.Render("> ")
		style = d.styles.selected
	} else if g.key != "own" && !isProjectKey(g.key) {
		style = d.styles.vendored
	}
	return cursor + style.Render(marker+" "+name) + d.styles.faint.Render(fmt.Sprintf(" (%d)", len(g.children)))
}

func (d delegate) exploreRow(e exploreItem, selected bool) string {
	cursor, style := "  ", d.styles.accent
	if selected {
		cursor, style = "> ", d.styles.selected
	}
	state := d.styles.ok.Render("available")
	if e.skill.Installed {
		state = d.styles.faint.Render("installed")
	}
	return cursor + style.Render(e.skill.Name) + "  " +
		d.styles.faint.Render(e.skill.Path) + "  " + state
}

// isProjectKey reports whether a group key is a project root path.
func isProjectKey(key string) bool { return strings.HasPrefix(key, string(os.PathSeparator)) }

func (d delegate) header(width int) string {
	c := layout(width, len(d.consumers))
	badges := ""
	for _, n := range d.consumers {
		badges += badge(n)
	}
	cells := []string{
		"  ",
		pad("skill", c.name), pad("origin", c.origin), pad("upd", c.upd), pad(badges, c.consumers),
		pad("dr", c.doctor), pad("mod", c.modified),
	}
	return d.styles.header.Render(strings.Join(cells, " "))
}

func (d delegate) row(it item, c columns, selected bool) string {
	s := d.styles
	cursor := "  "
	name := s.faint.Render("")
	if selected {
		cursor = s.selected.Render("> ")
		name = s.selected.Render(pad(it.skill.Name, c.name))
	} else {
		name = pad(it.skill.Name, c.name)
	}
	origin := pad("own", c.origin)
	switch {
	case it.skill.Origin.Vendored:
		origin = s.vendored.Render(pad("vend "+it.skill.Origin.Source, c.origin))
	case it.skill.Scope != "":
		origin = s.vendored.Render(pad("@"+filepath.Base(it.skill.Scope), c.origin))
	}
	var badges strings.Builder
	for _, n := range d.consumers {
		if it.enabled[n] {
			badges.WriteString(s.ok.Render(badge(n)))
		} else {
			badges.WriteString(s.faint.Render("·"))
		}
	}
	badges.WriteString(strings.Repeat(" ", c.consumers-len(d.consumers)))
	dr := s.faint.Render(pad("·", c.doctor))
	if n := len(it.findings); n > 0 {
		dr = d.severityStyle(worst(it.findings)).Render(pad(fmt.Sprint(n), c.doctor))
	}
	upd := s.faint.Render(pad("·", c.upd))
	if it.upstream == upstream.Outdated {
		upd = s.warn.Render(pad("↑", c.upd))
	}
	mod := s.faint.Render(pad(relTime(it.skill.ModTime, d.now()), c.modified))
	cells := []string{cursor + name, origin, upd, badges.String(), dr, mod}
	if c.description > 0 {
		cells = append(cells, s.faint.Render(pad(it.skill.Description, c.description)))
	}
	return strings.Join(cells, " ")
}

func (d delegate) severityStyle(sev doctor.Severity) lipglossStyle {
	switch sev {
	case doctor.Error:
		return d.styles.err
	case doctor.Warn:
		return d.styles.warn
	}
	return d.styles.faint
}

var badges = map[string]string{"claude": "C", "codex": "X", "omp": "O"}

func badge(consumer string) string {
	if b, ok := badges[consumer]; ok {
		return b
	}
	return strings.ToUpper(consumer[:1])
}

func worst(fs []doctor.Finding) doctor.Severity {
	w := doctor.Info
	for _, f := range fs {
		if f.Severity > w {
			w = f.Severity
		}
	}
	return w
}

// pad truncates with an ellipsis or right-pads with spaces to exactly width cells.
func pad(s string, width int) string {
	if width <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) > width {
		if width == 1 {
			return "…"
		}
		return string(r[:width-1]) + "…"
	}
	return s + strings.Repeat(" ", width-len(r))
}

func relTime(t time.Time, now time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 14*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d.Hours()/(24*30)))
	}
	return fmt.Sprintf("%dy", int(d.Hours()/(24*365)))
}
