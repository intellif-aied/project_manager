package handler

import (
	"fmt"
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

func TestErrorDetailsForMCPRemovesSentinelMessage(t *testing.T) {
	err := fmt.Errorf("%w: content contains forbidden term", reportbrief.ErrInvalid)
	if got := errorDetailsForMCP(err, reportbrief.ErrInvalid); got != "content contains forbidden term" {
		t.Fatalf("message = %q", got)
	}
}
