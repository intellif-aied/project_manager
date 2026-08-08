package service

import (
	"strings"
	"testing"
)

func TestProjectMemorySkillUsesIncrementalOperationContract(t *testing.T) {
	if ProjectMemorySkillVersion != "project-memory-v11" {
		t.Fatalf("version = %q", ProjectMemorySkillVersion)
	}
	markdown := ProjectMemorySkillMarkdown()
	required := []string{
		"Return only useful changes",
		"operations: []",
		"create_project",
		"confidence >= 0.88",
		"upsert_signal / retire_signal",
		"Every operation must include evidence_refs",
		"Human-edited and manual-report names are authoritative",
		"temp_ref",
		`"schema_version":"project-memory-maintenance/v2","operations"`,
	}
	for _, expected := range required {
		if !strings.Contains(markdown, expected) {
			t.Fatalf("skill is missing %q", expected)
		}
	}
	if strings.Contains(markdown, "one forced decision per theme") == false {
		t.Fatal("skill must explicitly reject forced per-theme decisions")
	}
}
