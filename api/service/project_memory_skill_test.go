package service

import (
	"strings"
	"testing"
)

func TestProjectMemorySkillUsesValidatedParentAndAliasContract(t *testing.T) {
	if ProjectMemorySkillVersion != "project-memory-v5" {
		t.Fatalf("version = %q", ProjectMemorySkillVersion)
	}
	markdown := ProjectMemorySkillMarkdown()
	required := []string{
		"First compare all current_theme items and build a parent map",
		"workspace_refs are opaque continuity evidence",
		"create_new: create a stable project",
		"include both the complete child name and the remaining child phrase",
		"芯片验证平台使用手册 and 使用手册",
		`"schema_version":"project-memory-proposal/v1","decisions"`,
		"Do not use proposals or decision fields in place of decisions and action",
	}
	for _, expected := range required {
		if !strings.Contains(markdown, expected) {
			t.Fatalf("skill is missing %q", expected)
		}
	}
	if strings.Contains(markdown, "create_project") {
		t.Fatal("skill contains unsupported create_project action")
	}
}
