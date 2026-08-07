package service

import (
	"strings"
	"testing"
)

func TestProjectMemorySkillUsesValidatedParentAndAliasContract(t *testing.T) {
	if ProjectMemorySkillVersion != "project-memory-v10" {
		t.Fatalf("version = %q", ProjectMemorySkillVersion)
	}
	markdown := ProjectMemorySkillMarkdown()
	required := []string{
		"First compare all current_theme items and build a parent map",
		"workspace_refs are opaque continuity evidence",
		"create_new: create a stable project",
		"confidence >= 0.88",
		"up to 20 prior report snapshots",
		"up to 10 recent report overviews",
		"up to 10 older, name-only anchors",
		"at most 20 historical reports in total",
		"Never replace an established parent with a narrower daily task",
		"matched_theme_refs lists current themes matched",
		"An exact child-title match alone does not beat this parent evidence",
		"extract workstream_cues",
		"调用执行、ctp CLI、版本流、调度器 or 用例筛选工作台",
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
