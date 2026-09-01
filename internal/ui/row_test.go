package ui

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/jegork/skillet/internal/doctor"
	"github.com/jegork/skillet/internal/skill"
)

func TestRelTime(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	cases := map[string]time.Time{
		"-":   {},
		"now": now.Add(-30 * time.Second),
		"5m":  now.Add(-5 * time.Minute),
		"3h":  now.Add(-3 * time.Hour),
		"13d": now.Add(-13 * 24 * time.Hour),
		"2w":  now.Add(-14 * 24 * time.Hour),
		"2mo": now.Add(-61 * 24 * time.Hour),
		"1y":  now.Add(-400 * 24 * time.Hour),
	}
	for want, at := range cases {
		if got := relTime(at, now); got != want {
			t.Errorf("relTime(%v) = %q, want %q", at, got, want)
		}
	}
}

func TestPad(t *testing.T) {
	cases := []struct {
		in    string
		width int
		want  string
	}{
		{"abc", 5, "abc  "},
		{"abcdef", 4, "abc…"},
		{"héllo wörld", 6, "héllo…"},
		{"x", 1, "x"},
		{"xy", 1, "…"},
		{"x", 0, ""},
	}
	for _, c := range cases {
		if got := pad(c.in, c.width); got != c.want {
			t.Errorf("pad(%q,%d) = %q, want %q", c.in, c.width, got, c.want)
		}
	}
}

func TestLayoutFitsWidth(t *testing.T) {
	for _, w := range []int{40, 60, 80, 120, 200} {
		c := layout(w, 3)
		total := 2 + c.name + 1 + c.origin + 1 + c.consumers + 1 + c.doctor + 1 + c.modified
		if c.description > 0 {
			total += 1 + c.description
		}
		if total > w {
			t.Errorf("width %d: columns need %d", w, total)
		}
		if w >= 120 && c.description == 0 {
			t.Errorf("width %d: description column dropped", w)
		}
	}
}

func TestRowRendersWithoutOverflow(t *testing.T) {
	d := delegate{styles: newStyles(true), consumers: []string{"claude", "codex", "omp"}, now: func() time.Time { return time.Now() }}
	it := item{
		skill:    skill.Skill{Name: "a-rather-long-skill-name-that-overflows", Description: strings.Repeat("d", 200), Origin: skill.Origin{Vendored: true, Source: "acme/very-long-repository-name"}, ModTime: time.Now()},
		enabled:  map[string]bool{"claude": true},
		findings: []doctor.Finding{{Severity: doctor.Warn}, {Severity: doctor.Error}},
	}
	for _, w := range []int{50, 100, 160} {
		row := d.row(it, layout(w, 3), true)
		if got := lipgloss.Width(row); got > w {
			t.Errorf("width %d: row is %d cells", w, got)
		}
		if !strings.Contains(row, "2") {
			t.Errorf("width %d: doctor count missing in %q", w, row)
		}
	}
}
