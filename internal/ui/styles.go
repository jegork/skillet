package ui

import "charm.land/lipgloss/v2"

type styles struct {
	header    lipgloss.Style
	selected  lipgloss.Style
	faint     lipgloss.Style
	accent    lipgloss.Style
	vendored  lipgloss.Style
	ok        lipgloss.Style
	warn      lipgloss.Style
	err       lipgloss.Style
	statusBar lipgloss.Style
	flash     lipgloss.Style
	title     lipgloss.Style
	separator lipgloss.Style
}

func newStyles(isDark bool) styles {
	ld := lipgloss.LightDark(isDark)
	faint := ld(lipgloss.Color("#8a8a8a"), lipgloss.Color("#6c6c6c"))
	accent := ld(lipgloss.Color("#005f87"), lipgloss.Color("#5fafff"))
	return styles{
		header:    lipgloss.NewStyle().Bold(true).Foreground(faint),
		selected:  lipgloss.NewStyle().Bold(true).Foreground(accent),
		faint:     lipgloss.NewStyle().Foreground(faint),
		accent:    lipgloss.NewStyle().Foreground(accent),
		vendored:  lipgloss.NewStyle().Foreground(ld(lipgloss.Color("#875f00"), lipgloss.Color("#d7af5f"))),
		ok:        lipgloss.NewStyle().Foreground(ld(lipgloss.Color("#008700"), lipgloss.Color("#87d787"))),
		warn:      lipgloss.NewStyle().Foreground(ld(lipgloss.Color("#af8700"), lipgloss.Color("#ffd75f"))),
		err:       lipgloss.NewStyle().Foreground(ld(lipgloss.Color("#d70000"), lipgloss.Color("#ff5f5f"))),
		statusBar: lipgloss.NewStyle().Foreground(faint),
		flash:     lipgloss.NewStyle().Foreground(accent).Bold(true),
		title:     lipgloss.NewStyle().Bold(true),
		separator: lipgloss.NewStyle().Foreground(faint),
	}
}
