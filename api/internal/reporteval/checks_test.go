package reporteval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCollectDeterministicMetricsChecksFactConservation(t *testing.T) {
	directory := t.TempDir()
	contextPayload := json.RawMessage(`{
		"schema_version":"report-context/v1",
		"work_evidence":{"mode":"work_evidence","period":{"start_date":"2026-06-11","end_date":"2026-06-11"},"facts":[
			{"fact_ref":"fact-001","kind":"result","text":"完成开发","observations":[]},
			{"fact_ref":"fact-002","kind":"trace","text":"过程沟通","observations":[]}
		]}
	}`)
	briefPayload := json.RawMessage(`{
		"schema_version":"report-brief/v1","report_type":"personal_daily",
		"period":{"start":"2026-06-11","end":"2026-06-11"},
		"workstreams":[{"title":"日报","objective":"交付","deliverables":[{
			"result":"完成开发","state":"validated","environment":"test","fact_refs":["fact-001"]
		}]}],
		"excluded_facts":[{"fact_ref":"fact-002","reason":"trace"}],"no_reportable_work":false
	}`)
	if _, err := writeJSON(filepath.Join(directory, "context.json"), contextPayload); err != nil {
		t.Fatal(err)
	}
	if _, err := writeJSON(filepath.Join(directory, "brief.json"), briefPayload); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "generated-draft.md"), []byte("完成开发\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	metrics := collectDeterministicMetrics(directory, "run-1")
	if metrics.FactTotal != 2 || metrics.FactReferencedCount != 1 || metrics.FactExcludedCount != 1 || metrics.FactUnaccountedCount != 0 || len(metrics.Anomalies) != 0 {
		t.Fatalf("metrics = %#v", metrics)
	}
	briefPayload = json.RawMessage(`{
		"schema_version":"report-brief/v1","report_type":"personal_daily","period":{"start":"2026-06-11","end":"2026-06-11"},
		"workstreams":[{"title":"日报","objective":"交付","deliverables":[{"result":"完成开发","state":"validated","environment":"test","fact_refs":["fact-001"]}]}],
		"excluded_facts":[],"no_reportable_work":false
	}`)
	if _, err := writeJSON(filepath.Join(directory, "brief.json"), briefPayload); err != nil {
		t.Fatal(err)
	}
	metrics = collectDeterministicMetrics(directory, "run-1")
	if metrics.FactUnaccountedCount != 1 || len(metrics.Anomalies) == 0 {
		t.Fatalf("missing conservation failure: %#v", metrics)
	}
}
