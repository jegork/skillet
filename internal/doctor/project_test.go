package doctor_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jegork/skillet/internal/consumer"
	"github.com/jegork/skillet/internal/doctor"
	"github.com/jegork/skillet/internal/project"
	"github.com/jegork/skillet/internal/skill"
	"github.com/jegork/skillet/internal/testhome"
)

// projectInput scans a project home-style: skills, lock, consumer reports.
func projectInput(t *testing.T, h *testhome.Home, root string) doctor.ProjectInput {
	t.Helper()
	lock, err := skill.ReadProjectLock(root + "/skills-lock.json")
	if err != nil {
		t.Fatal(err)
	}
	proj := project.Project{Root: root}
	skills, err := skill.Scan(proj.SkillsDir(), skill.Lock{})
	if err != nil {
		t.Fatal(err)
	}
	for i := range skills {
		skills[i].Scope = root
	}
	reports := map[string]consumer.Report{}
	for _, c := range proj.Consumers() {
		rep, err := c.Report(skills)
		if err != nil {
			t.Fatal(err)
		}
		reports[c.Name()] = rep
	}
	return doctor.ProjectInput{
		Root: root, SkillsDir: proj.SkillsDir(), Skills: skills, Lock: lock, Reports: reports,
	}
}

func runWith(t *testing.T, h *testhome.Home, projects ...doctor.ProjectInput) []doctor.Finding {
	t.Helper()
	paths := skill.Paths{Home: h.Dir}
	lock, err := skill.ReadLock(paths.LockFile())
	if err != nil {
		t.Fatal(err)
	}
	skills, err := skill.Scan(paths.SkillsDir(), lock)
	if err != nil {
		t.Fatal(err)
	}
	return doctor.Run(doctor.Input{Paths: paths, Skills: skills, Lock: lock, Projects: projects})
}

func TestCleanProjectNoFindings(t *testing.T) {
	h := testhome.New(t)
	h.Readme()
	mirror := h.ProjectDir("src/mirror")
	h.ProjectSkill(mirror, "a-skill", "does a")
	h.ProjectLock(mirror, map[string]string{"a-skill": "acme/repo"})
	h.ProjectStub(mirror, ".claude/skills", "a-skill", "../../.agents/skills/a-skill")

	expect(t, runWith(t, h, projectInput(t, h, mirror)))
}

func TestProjectSkillShadowsGlobal(t *testing.T) {
	h := testhome.New(t)
	h.Skill("web-perf", "global one")
	h.Readme("| `web-perf` | own | global one |")
	mirror := h.ProjectDir("src/proj")
	h.ProjectSkill(mirror, "web-perf", "project one")
	h.ProjectLock(mirror, map[string]string{"web-perf": "buildfind/agent-skills"})

	fs := runWith(t, h, projectInput(t, h, mirror))
	got := expect(t, fs, key{"shadow", "web-perf", doctor.Error})
	f := fs[0]
	if f.Project != mirror {
		t.Errorf("finding not scoped to the project: %+v", f)
	}
	if msg := got[key{"shadow", "web-perf", doctor.Error}]; msg == "" {
		t.Error("empty message")
	}
}

func TestProjectStubFindings(t *testing.T) {
	h := testhome.New(t)
	h.Readme()
	mirror := h.ProjectDir("src/proj")
	h.ProjectSkill(mirror, "a-skill", "does a")
	h.ProjectStub(mirror, ".claude/skills", "dangling", "../../.agents/skills/dangling")
	h.ProjectStub(mirror, ".claude/skills", "foreign", "../../.agents/skills/other")

	fs := runWith(t, h, projectInput(t, h, mirror))
	got := expect(t, fs, key{"stub", "dangling", doctor.Error}, key{"stub", "foreign", doctor.Error})
	for _, k := range []key{{"stub", "dangling", doctor.Error}, {"stub", "foreign", doctor.Error}} {
		if msg := got[k]; !strings.Contains(msg, filepath.Base(mirror)) {
			t.Errorf("message %q does not name the project", msg)
		}
	}
}

func TestProjectLockOrphanAndDrift(t *testing.T) {
	h := testhome.New(t)
	h.Readme()
	mirror := h.ProjectDir("src/proj")
	h.ProjectSkill(mirror, "a-skill", "does a")
	h.ProjectLock(mirror, map[string]string{"a-skill": "acme/repo", "ghost": "acme/ghost"})
	// stale hash: rewrite the skill after locking
	h.ProjectFile(mirror, "a-skill", "extra.md", "more\n")

	fs := runWith(t, h, projectInput(t, h, mirror))
	expect(t, fs,
		key{"lock-orphan", "", doctor.Warn},
		key{"drift", "", doctor.Warn},
	)
}

func TestProjectDriftQuietWhenHashMatches(t *testing.T) {
	h := testhome.New(t)
	h.Readme()
	mirror := h.ProjectDir("src/proj")
	h.ProjectSkill(mirror, "a-skill", "does a")
	h.ProjectLock(mirror, map[string]string{"a-skill": "acme/repo"})
	h.ProjectFile(mirror, "a-skill", "extra.md", "more\n")
	// re-lock at current content
	hash, err := skill.ContentHash(filepath.Join(mirror, ".agents", "skills"))
	if err != nil {
		t.Fatal(err)
	}
	h.ProjectLockWithHash(mirror, hash, map[string]string{"a-skill": "acme/repo"})

	expect(t, runWith(t, h, projectInput(t, h, mirror)))
}
