package handler

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/aidashboard/api/internal/reportbrief"
)

func TestManagedReportToolsetIncludesBriefOnlyWhenEnabled(t *testing.T) {
	disabled := reportMCPToolsForToolsetWithBrief(managedReportMCPToolset, false)
	if len(disabled) != 3 {
		t.Fatalf("disabled managed tools=%d, want 3", len(disabled))
	}
	enabled := reportMCPToolsForToolsetWithBrief(managedReportMCPToolset, true)
	want := []string{toolGetReportContext, toolWriteReportBrief, toolWriteReportResult, toolWriteReportFailure}
	if len(enabled) != len(want) {
		t.Fatalf("enabled managed tools=%d, want %d", len(enabled), len(want))
	}
	for index, tool := range enabled {
		if got := tool["name"]; got != want[index] {
			t.Fatalf("tool[%d]=%v, want %s", index, got, want[index])
		}
	}
	for _, tool := range reportMCPToolsForToolsetWithBrief("", true) {
		if tool["name"] == toolWriteReportBrief {
			t.Fatal("legacy full toolset must not expose write_report_brief")
		}
	}
}

func TestPersonalReportToolsetIsRunBoundWithoutBrief(t *testing.T) {
	tools := reportMCPToolsForToolsetWithBrief(personalReportMCPToolset, true)
	want := []string{toolGetReportContext, toolWriteReportResult, toolWriteReportFailure}
	if len(tools) != len(want) {
		t.Fatalf("personal tools=%d, want %d", len(tools), len(want))
	}
	for index, tool := range tools {
		if got := tool["name"]; got != want[index] {
			t.Fatalf("tool[%d]=%v, want %s", index, got, want[index])
		}
		properties := tool["inputSchema"].(map[string]any)["properties"].(map[string]any)
		if _, exists := properties["run_id"]; exists {
			t.Fatalf("personal tool %s exposes run_id", want[index])
		}
		if want[index] == toolWriteReportResult {
			if _, exists := properties["format_mode"]; exists {
				t.Fatal("personal write_report_result must not expose the system format policy")
			}
		}
	}
}

func TestManagedReportResultToolRequiresStandardFormatMode(t *testing.T) {
	tools := reportMCPToolsForToolsetWithBrief(managedReportMCPToolset, true)
	for _, tool := range tools {
		if tool["name"] != toolWriteReportResult {
			continue
		}
		schema := tool["inputSchema"].(map[string]any)
		properties := schema["properties"].(map[string]any)
		mode, exists := properties["format_mode"]
		if !exists {
			t.Fatal("managed write_report_result does not expose format_mode")
		}
		if !reflect.DeepEqual(mode, map[string]any{"type": "string", "enum": []string{"standard"}}) {
			t.Fatalf("format_mode schema = %#v", mode)
		}
		required := schema["required"].([]string)
		if !reflect.DeepEqual(required, []string{"content", "format_mode"}) {
			t.Fatalf("managed write required = %#v", required)
		}
		return
	}
	t.Fatal("managed write_report_result tool not found")
}

func TestManagedReportBriefToolUsesRunBoundJSONContract(t *testing.T) {
	tool := reportBriefTool()
	schema, ok := tool["inputSchema"].(map[string]any)
	if !ok {
		t.Fatalf("inputSchema type = %T", tool["inputSchema"])
	}
	if got, want := schema["required"], []string{"brief_json"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("required = %#v, want %#v", got, want)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T", schema["properties"])
	}
	if _, exists := properties["run_id"]; exists {
		t.Fatal("managed Brief schema must not ask the model to repeat run_id")
	}
	if got := properties["brief_json"]; !reflect.DeepEqual(got, map[string]any{"type": "string"}) {
		t.Fatalf("brief_json schema = %#v", got)
	}
}

func TestManagedReportToolsDoNotExposeRunIDButLegacyToolsKeepIt(t *testing.T) {
	managed := reportMCPToolsForToolsetWithBrief(managedReportMCPToolset, true)
	for _, tool := range managed {
		name, _ := tool["name"].(string)
		properties := tool["inputSchema"].(map[string]any)["properties"].(map[string]any)
		if _, exists := properties["run_id"]; exists {
			t.Fatalf("managed tool %s exposes run_id", name)
		}
	}
	legacy := reportMCPToolsForToolsetWithBrief("", true)
	for _, tool := range legacy {
		name, _ := tool["name"].(string)
		if name != toolGetReportContext && name != toolWriteReportResult && name != toolWriteReportFailure {
			continue
		}
		properties := tool["inputSchema"].(map[string]any)["properties"].(map[string]any)
		if _, exists := properties["run_id"]; !exists {
			t.Fatalf("legacy tool %s lost run_id compatibility", name)
		}
	}
}

func TestErrorDetailsForMCPRemovesSentinelMessage(t *testing.T) {
	err := fmt.Errorf("%w: content contains forbidden term", reportbrief.ErrInvalid)
	if got := errorDetailsForMCP(err, reportbrief.ErrInvalid); got != "content contains forbidden term" {
		t.Fatalf("message = %q", got)
	}
}
