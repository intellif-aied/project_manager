package handler

import (
	"encoding/json"
	"testing"
)

func TestBuildReportVariantManifestWithOptionalBrief(t *testing.T) {
	modelID := "MiniMax-M3"
	agentVersion := 12
	run := &reportAIRun{
		AgentID: "report-agent", AgentVersionID: &agentVersion, ModelID: &modelID,
		ContextRepresentation: "work_evidence",
		InputRef: map[string]any{
			"digest_version": "digest-v2", "redaction_version": "redaction-v1",
			"report_context_schema_version": "context-v1", "report_skill_slug": "report-skill",
			"report_skill_version": "3", "mcp_server": "report-mcp", "report_mcp_version": "4",
		},
	}
	payload, hash, err := buildReportVariantManifest(run, "brief-v1")
	if err != nil {
		t.Fatal(err)
	}
	var manifest reportVariantManifest
	if err := json.Unmarshal(payload, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.PipelineProfile != "digest_context_brief_final" || len(manifest.Stages) != 4 || manifest.BriefSchemaVersion != "brief-v1" {
		t.Fatalf("manifest = %#v", manifest)
	}
	if hash != sha256Hex(string(payload)) {
		t.Fatalf("variant hash does not match payload")
	}

	directPayload, directHash, err := buildReportVariantManifest(run, "")
	if err != nil {
		t.Fatal(err)
	}
	if directHash == hash {
		t.Fatalf("brief and direct variants must not share a hash")
	}
	if err := json.Unmarshal(directPayload, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.PipelineProfile != "digest_context_final" || len(manifest.Stages) != 3 || manifest.BriefSchemaVersion != "" {
		t.Fatalf("direct manifest = %#v", manifest)
	}
}

func TestBuildReportVariantManifestIsDeterministic(t *testing.T) {
	run := &reportAIRun{AgentID: "report-agent", InputRef: map[string]any{"digest_version": "v2"}}
	first, firstHash, err := buildReportVariantManifest(run, "")
	if err != nil {
		t.Fatal(err)
	}
	second, secondHash, err := buildReportVariantManifest(run, "")
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) || firstHash != secondHash {
		t.Fatalf("manifest is not deterministic")
	}
}
