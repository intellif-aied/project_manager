package handler

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aidashboard/api/internal/autodailyreport"
	"github.com/aidashboard/api/model"
)

func TestBuildAutoDailyReportSubmissionBindsOwnerSourcesAndGuard(t *testing.T) {
	defaults := testManagedAgentDefaults()
	defaults.ReportTwoPassEnabled = true
	handler := NewManagedAgentHandlerWithDefaults(nil, nil, defaults)
	agent := model.ManagedAgent{
		AgentID: "system-report-agent", CurrentVersionID: 7,
		Instructions: "system report instructions", StartPromptTemplate: "/aida-report",
	}
	fingerprint := strings.Repeat("a", 64)
	updatedAt := time.Date(2026, 7, 31, 12, 0, 0, 123000, time.UTC)
	request := autodailyreport.SubmissionRequest{
		UserID: "305", ReportDate: "2026-07-31", SourceFingerprint: fingerprint,
		SourceSliceKeys: []string{"00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002"},
		Guard: autodailyreport.ReportGuard{
			Mode: autodailyreport.GuardModeReplace, ReportID: "report-1", UpdatedAt: &updatedAt,
		},
	}

	got, err := handler.buildAutoDailyReportSubmission(
		agent, request, request.SourceSliceKeys, map[string]struct{}{reportMCPCredentialSlot: {}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.UserID != "305" || got.ReportType != reportTypePersonalDaily || got.BusinessType != reportAgentRunBusinessType || !got.RequireSources {
		t.Fatalf("unexpected report identity: %#v", got)
	}
	if got.AgentID != agent.AgentID || got.AgentVersionID == nil || *got.AgentVersionID != 7 || got.ModelID != defaults.ReportModelID {
		t.Fatalf("unexpected runtime binding: agent=%q version=%v model=%q", got.AgentID, got.AgentVersionID, got.ModelID)
	}
	if got.IdempotencyKey != "auto-daily:2026-07-31:"+fingerprint {
		t.Fatalf("idempotency key=%q", got.IdempotencyKey)
	}
	wantSources := []string{request.SourceSliceKeys[0], request.SourceSliceKeys[1]}
	gotSources := []string{got.Sources[0].SliceKey, got.Sources[1].SliceKey}
	if !reflect.DeepEqual(gotSources, wantSources) {
		t.Fatalf("frozen sources=%#v want=%#v", gotSources, wantSources)
	}
	if got.InputRef["trigger_source"] != autodailyreport.TriggerSource || got.InputRef["auto_report_source_fingerprint"] != fingerprint {
		t.Fatalf("automatic metadata missing: %#v", got.InputRef)
	}
	guard, ok := got.InputRef["auto_report_guard"].(autodailyreport.ReportGuard)
	if !ok || guard.ReportID != "report-1" || guard.UpdatedAt == nil || !guard.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("write guard was not frozen: %#v", got.InputRef["auto_report_guard"])
	}
	target, ok := got.InputRef["target"].(reportTarget)
	if !ok || target.Type != "self" || target.UserID != "305" {
		t.Fatalf("target does not use source owner: %#v", got.InputRef["target"])
	}
	if got.ExecutionInput["system_report_account"] != true || got.ExecutionInput["report_agent_source"] != managedAgentSourceSystem {
		t.Fatalf("automatic run did not use the system report account: %#v", got.ExecutionInput)
	}
	if len(got.VariantManifest) == 0 || got.VariantSHA256 == "" {
		t.Fatal("automatic run did not freeze its variant manifest")
	}
	encoded, err := json.Marshal(got.RequestFingerprintInput)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{autodailyreport.TriggerSource, fingerprint, "report-1"} {
		if !strings.Contains(string(encoded), expected) {
			t.Fatalf("request fingerprint does not bind %q: %s", expected, encoded)
		}
	}
}

func TestSubmitAutoDailyReportValidatesBeforeCallingPlatform(t *testing.T) {
	handler := NewManagedAgentHandlerWithDefaults(nil, nil, testManagedAgentDefaults())
	_, err := handler.SubmitAutoDailyReport(t.Context(), autodailyreport.SubmissionRequest{
		UserID: "305", ReportDate: "2026-07-31", SourceFingerprint: "invalid",
		SourceSliceKeys: []string{"00000000-0000-4000-8000-000000000001"},
		Guard:           autodailyreport.ReportGuard{Mode: autodailyreport.GuardModeAbsent},
	})
	if err == nil {
		t.Fatal("invalid automatic request was accepted")
	}
}
