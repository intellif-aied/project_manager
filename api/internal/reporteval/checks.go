package reporteval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/aidashboard/api/internal/reportbrief"
	"github.com/aidashboard/api/internal/reportcontext"
)

type DeterministicMetrics struct {
	RunID                string   `json:"run_id"`
	FactTotal            int      `json:"fact_total"`
	FactReferencedCount  int      `json:"fact_referenced_count"`
	FactExcludedCount    int      `json:"fact_excluded_count"`
	FactUnaccountedCount int      `json:"fact_unaccounted_count"`
	WorkstreamCount      int      `json:"workstream_count"`
	DeliverableCount     int      `json:"deliverable_count"`
	Anomalies            []string `json:"anomalies"`
}

var internalLeakagePattern = regexp.MustCompile("(?i)(?:[0-9a-f]{8}-[0-9a-f-]{27,}|bearer\\s+|https?://|/api/|(?:10|127|192\\.168)\\.[0-9.]+)")

func collectDeterministicMetrics(runDir, runID string) DeterministicMetrics {
	metrics := DeterministicMetrics{RunID: runID, Anomalies: []string{}}
	var contextPayload reportcontext.Payload
	if err := decodeJSONFile(filepath.Join(runDir, "context.json"), &contextPayload); err != nil {
		return metrics
	}
	available := map[string]bool{}
	if contextPayload.WorkEvidence != nil {
		for _, fact := range contextPayload.WorkEvidence.Facts {
			if fact.FactRef != "" {
				available[fact.FactRef] = true
			}
		}
	}
	metrics.FactTotal = len(available)
	var brief reportbrief.Payload
	if err := decodeJSONFile(filepath.Join(runDir, "brief.json"), &brief); err == nil {
		referenced := map[string]bool{}
		excluded := map[string]bool{}
		metrics.WorkstreamCount = len(brief.Workstreams)
		for _, workstream := range brief.Workstreams {
			metrics.DeliverableCount += len(workstream.Deliverables)
			for _, deliverable := range workstream.Deliverables {
				if len(deliverable.FactRefs) == 0 {
					metrics.Anomalies = append(metrics.Anomalies, "deliverable_without_fact_refs")
				}
				if deliverable.State == "released" && deliverable.Environment != "production" ||
					deliverable.State == "validated" && deliverable.Environment != "test" {
					metrics.Anomalies = append(metrics.Anomalies, "state_environment_mismatch")
				}
				for _, ref := range deliverable.FactRefs {
					if available[ref] {
						referenced[ref] = true
					} else {
						metrics.Anomalies = append(metrics.Anomalies, "unknown_fact_ref")
					}
				}
			}
		}
		for _, item := range brief.ExcludedFacts {
			if available[item.FactRef] {
				excluded[item.FactRef] = true
			} else {
				metrics.Anomalies = append(metrics.Anomalies, "unknown_excluded_fact_ref")
			}
			if referenced[item.FactRef] {
				metrics.Anomalies = append(metrics.Anomalies, "fact_referenced_and_excluded")
			}
		}
		metrics.FactReferencedCount = len(referenced)
		metrics.FactExcludedCount = len(excluded)
		for ref := range available {
			if !referenced[ref] && !excluded[ref] {
				metrics.FactUnaccountedCount++
			}
		}
		if metrics.FactReferencedCount+metrics.FactExcludedCount != metrics.FactTotal {
			metrics.Anomalies = append(metrics.Anomalies, "fact_conservation_failed")
		}
	}
	for _, name := range []string{"brief.json", "generated-draft.md"} {
		payload, err := os.ReadFile(filepath.Join(runDir, name))
		if err == nil && internalLeakagePattern.Match(payload) {
			metrics.Anomalies = append(metrics.Anomalies, strings.TrimSuffix(name, filepath.Ext(name))+"_internal_leakage")
		}
	}
	metrics.Anomalies = uniqueStrings(metrics.Anomalies)
	return metrics
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func encodeDeterministicMetrics(value DeterministicMetrics) json.RawMessage {
	payload, _ := json.Marshal(value)
	return payload
}
