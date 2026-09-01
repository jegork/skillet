package doctor_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jegork/skillet/internal/consumer"
	"github.com/jegork/skillet/internal/doctor"
	"github.com/jegork/skillet/internal/skill"
	"github.com/jegork/skillet/internal/testhome"
)

type key struct {
	check, skill string
	sev          doctor.Severity
}

func run(t *testing.T, h *testhome.Home, consumers ...consumer.Consumer) []doctor.Finding {
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
	reports := map[string]consumer.Report{}
	for _, c := range consumers {
		rep, err := c.Report(skills)
		if err != nil {
			t.Fatal(err)
		}
		reports[c.Name()] = rep
	}
	return doctor.Run(doctor.Input{Paths: paths, Skills: skills, Lock: lock, Reports: reports})
}

func keyed(fs []doctor.Finding) map[key]string {
	out := map[key]string{}
	for _, f := range fs {
		out[key{f.Check, f.Skill, f.Severity}] = f.Message
	}
	return out
}

func expect(t *testing.T, found []doctor.Finding, want ...key) map[key]string {
	t.Helper()
	got := keyed(found)
	for _, k := range want {
		if _, ok := got[k]; !ok {
			t.Errorf("missing finding %+v; got %v", k, found)
		}
	}
	if len(found) != len(want) {
		t.Errorf("got %d findings, want %d: %v", len(found), len(want), found)
	}
	return got
}

func TestCleanHomeHasNoFindings(t *testing.T) {
	h := testhome.New(t)
	h.Skill("a", "A")
	h.Skill("v", "V")
	h.Lock(map[string]string{"v": "acme/skills"})
	h.Readme("| `a` | own | A |", "| `v` | vendored (acme/skills) | V |")
	h.Stub(".claude/skills", "a", "../../.agents/skills/a")
	claude := consumer.NewSymlinkDir("claude", filepath.Join(h.Dir, ".claude", "skills"), h.SkillsDir())

	expect(t, run(t, h, claude))
}

func TestSkillFileFindings(t *testing.T) {
	h := testhome.New(t)
	h.EmptySkill("nomd")
	h.RawSkill("nodesc", "---\nname: nodesc\n---\n")
	h.RawSkill("renamed", "---\nname: else\ndescription: d\n---\n")
	h.Readme("| `nomd` | own |  |", "| `nodesc` | own |  |", "| `renamed` | own | d |")

	expect(t, run(t, h),
		key{"skill-md", "nomd", doctor.Error},
		key{"description", "nodesc", doctor.Warn},
		key{"name-mismatch", "renamed", doctor.Warn},
	)
}

func TestStubFindings(t *testing.T) {
	h := testhome.New(t)
	h.Skill("a", "A")
	h.Readme("| `a` | own | A |")
	h.Stub(".codex/skills", "a", "../../.agents/skills/a")
	h.Stub(".codex/skills", "gone", "../../.agents/skills/gone")
	codex := consumer.NewSymlinkDir("codex", filepath.Join(h.Dir, ".codex", "skills"), h.SkillsDir())

	got := expect(t, run(t, h, codex), key{"stub", "gone", doctor.Error})
	if msg := got[key{"stub", "gone", doctor.Error}]; msg != "codex: dangling symlink -> ../../.agents/skills/gone" {
		t.Errorf("message %q", msg)
	}
}

func TestXrefFindings(t *testing.T) {
	h := testhome.New(t)
	h.Skill("a", "A")
	h.File("a", "SKILL.md", "---\nname: a\ndescription: A\n---\nload the `missing` skill, then the `b` skill. Not `Skill` here.\n")
	h.File("a", "docs/more.md", "see the `also-missing` skill\n")
	h.Skill("b", "B")
	h.Skill("v", "V")
	h.File("v", "SKILL.md", "---\nname: v\ndescription: V\n---\nuse the `upstream-sibling` skill\n")
	h.Lock(map[string]string{"v": "acme/skills"})
	h.Readme("| `a` | own | A |", "| `b` | own | B |", "| `v` | vendored (acme/skills) | V |")

	got := expect(t, run(t, h),
		key{"xref", "a", doctor.Warn},
		key{"xref", "v", doctor.Info},
	)
	if msg := got[key{"xref", "a", doctor.Warn}]; msg != "references unknown skills: also-missing, missing" {
		t.Errorf("message %q", msg)
	}
}

func TestReadmeFindings(t *testing.T) {
	h := testhome.New(t)
	h.Skill("a", "A")
	h.Skill("b", "B")
	h.Skill("c", "C")
	h.Lock(map[string]string{"c": "acme/skills"})

	expect(t, run(t, h), key{"readme", "", doctor.Error})

	h.Readme("| `a` | own | A |", "| `c` | own | C |", "| `zz` | own | gone |")
	got := run(t, h)
	if len(got) != 3 {
		t.Fatalf("got %v", got)
	}
	msgs := map[string]bool{}
	for _, f := range got {
		if f.Check != "readme" || f.Skill != "" || f.Severity != doctor.Warn {
			t.Errorf("unexpected finding %+v", f)
		}
		msgs[f.Message] = true
	}
	for _, m := range []string{
		"README missing `b`",
		"README lists `zz` which does not exist",
		"README origin for `c` is \"own\", disk says \"vendored (acme/skills)\"",
	} {
		if !msgs[m] {
			t.Errorf("missing message %q in %v", m, msgs)
		}
	}
}

func TestLockOrphan(t *testing.T) {
	h := testhome.New(t)
	h.Skill("a", "A")
	h.Lock(map[string]string{"a": "acme/skills", "ghost": "acme/ghost"})
	h.Readme("| `a` | vendored (acme/skills) | A |")

	expect(t, run(t, h), key{"lock-orphan", "", doctor.Warn})
}

func TestDriftAgainstLockHash(t *testing.T) {
	h := testhome.New(t)
	h.Skill("same", "S")
	h.Skill("changed", "C")
	h.Skill("own", "O")
	h.Readme("| `same` | vendored (acme/a) | S |", "| `changed` | vendored (acme/b) | C |", "| `own` | own | O |")
	sameHash, err := skill.TreeHash(filepath.Join(h.SkillsDir(), "same"))
	if err != nil {
		t.Fatal(err)
	}
	h.LockWithHashes(map[string]string{"same": "acme/a", "changed": "acme/b"}, map[string]string{
		"same":    sameHash,
		"changed": "0000000000000000000000000000000000000000",
	})

	expect(t, run(t, h), key{"drift", "changed", doctor.Info})
}

func TestProvenanceMarkers(t *testing.T) {
	h := testhome.New(t)
	h.RawSkill("untracked", "---\nname: untracked\ndescription: U\nlicense: MIT\nmetadata:\n  author: vercel\n---\n")
	h.RawSkill("tracked", "---\nname: tracked\ndescription: T\nlicense: MIT\n---\n")
	h.Skill("mine", "M")
	h.Lock(map[string]string{"tracked": "acme/skills"})
	h.Readme("| `untracked` | own | U |", "| `tracked` | vendored (acme/skills) | T |", "| `mine` | own | M |")

	got := expect(t, run(t, h), key{"provenance", "untracked", doctor.Warn})
	if msg := got[key{"provenance", "untracked", doctor.Warn}]; !strings.Contains(msg, "author vercel") || !strings.Contains(msg, "license MIT") {
		t.Errorf("message %q", msg)
	}
}
