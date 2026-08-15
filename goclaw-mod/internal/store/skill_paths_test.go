package store

import "testing"

func TestSkillBaseDirAcceptsDirectoryPath(t *testing.T) {
	got := SkillBaseDir("/var/lib/goclaw/data/skills-store/demo/3")
	if got != "/var/lib/goclaw/data/skills-store/demo/3" {
		t.Fatalf("SkillBaseDir() = %q", got)
	}
}

func TestSkillBaseDirAcceptsSkillMarkdownPath(t *testing.T) {
	got := SkillBaseDir("/var/lib/goclaw/data/skills-store/demo/3/SKILL.md")
	if got != "/var/lib/goclaw/data/skills-store/demo/3" {
		t.Fatalf("SkillBaseDir() = %q", got)
	}
}

func TestSkillMarkdownPath(t *testing.T) {
	got := SkillMarkdownPath("/var/lib/goclaw/data/skills-store/demo/3/SKILL.md")
	if got != "/var/lib/goclaw/data/skills-store/demo/3/SKILL.md" {
		t.Fatalf("SkillMarkdownPath() = %q", got)
	}
}

func TestSkillSlugDir(t *testing.T) {
	got := SkillSlugDir("/var/lib/goclaw/data/skills-store/demo/3/SKILL.md")
	if got != "/var/lib/goclaw/data/skills-store/demo" {
		t.Fatalf("SkillSlugDir() = %q", got)
	}
}
