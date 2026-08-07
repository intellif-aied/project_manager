package handler

import "testing"

func TestReportReviewMCPExposesOnlyReviewTools(t *testing.T) {
	tools := reportReviewTools()
	if len(tools) != 2 || tools[0]["name"] != toolGetReportReviewContext || tools[1]["name"] != toolWriteReportReview {
		t.Fatalf("review tools = %#v", tools)
	}
}

func TestReportReviewPatchSchemaUsesExclusiveOperationShapes(t *testing.T) {
	tools := reportReviewTools()
	input := tools[1]["inputSchema"].(map[string]any)
	properties := input["properties"].(map[string]any)
	patches := properties["patches"].(map[string]any)
	if _, ok := properties["project_attachments"]; !ok {
		t.Fatal("review schema must require explicit project attachments")
	}
	items := patches["items"].(map[string]any)
	variants, ok := items["oneOf"].([]map[string]any)
	if !ok || len(variants) != 10 {
		t.Fatalf("patch schema must expose exclusive shapes: %#v", items)
	}
	for _, variant := range variants {
		if variant["additionalProperties"] != false {
			t.Fatalf("patch variant allows ambiguous fields: %#v", variant)
		}
	}
}
