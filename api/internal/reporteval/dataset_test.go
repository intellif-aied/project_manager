package reporteval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFrozenDatasetFixture(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.MkdirAll(filepath.Join(directory, "sources"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(directory, "pattern"), 0o750); err != nil {
		t.Fatal(err)
	}
	source := SourceEvidence{
		SchemaVersion: SourceSchemaVersion, SourceIdentitySHA256: strings.Repeat("c", 64), RedactionVersion: EvidenceRedactionVersion,
		Items: []SourceEvidenceItem{{EvidenceSourceID: "source-001", AgentType: "codex", Events: []SourceEvidenceEvent{{
			EvidenceRef: "source-001/event-000001", OccurredAt: time.Date(2026, 6, 11, 1, 0, 0, 0, time.UTC),
			EventType: "assistant", Summary: "完成协议设计", Payload: json.RawMessage(`{"text":"done"}`),
		}}}},
	}
	sourcePayload, _ := json.MarshalIndent(source, "", "  ")
	sourcePayload = append(sourcePayload, '\n')
	sourcePath := filepath.Join(directory, "sources", "case-001.json")
	if err := os.WriteFile(sourcePath, sourcePayload, 0o640); err != nil {
		t.Fatal(err)
	}
	pattern := PatternStatistics{
		SchemaVersion: PatternStatisticsSchemaVersion, ReportCount: 2, AuthorCount: 2, DateCount: 1,
		DateRange:         PatternDateRange{Start: "2026-07-29", End: "2026-07-29"},
		FormatClassCounts: map[string]int{"ordered_list": 2}, CharacterCount: PatternRange{Min: 10, P50: 20, P90: 30, Max: 40},
		LineCount: PatternRange{Min: 1, P50: 2, P90: 3, Max: 4}, OrderedItemCount: PatternRange{P50: 1, P90: 2, Max: 3},
		HeadingCount: PatternRange{P50: 0, P90: 0, Max: 1}, ReportsPerAuthor: PatternRange{Min: 1, P50: 1, P90: 1, Max: 1},
		ReportsPerDate: map[string]int{"2026-07-29": 2}, RedactionCounts: map[string]int{},
	}
	patternPayload, _ := json.MarshalIndent(pattern, "", "  ")
	patternPayload = append(patternPayload, '\n')
	if err := os.WriteFile(filepath.Join(directory, "pattern", "statistics.json"), patternPayload, 0o640); err != nil {
		t.Fatal(err)
	}
	dataset := validDataset()
	dataset.Cases[0].SourceEvidence.SHA256 = CanonicalBytesSHA256(sourcePayload)
	dataset.PatternBaseline.Statistics.SHA256 = CanonicalBytesSHA256(patternPayload)
	manifestPayload, _ := json.MarshalIndent(dataset, "", "  ")
	manifestPath := filepath.Join(directory, "dataset.json")
	if err := os.WriteFile(manifestPath, append(manifestPayload, '\n'), 0o640); err != nil {
		t.Fatal(err)
	}
	return manifestPath
}

func TestLoadFrozenDatasetVerifiesContentAndReferences(t *testing.T) {
	path := writeFrozenDatasetFixture(t)
	dataset, err := LoadFrozenDataset(path)
	if err != nil {
		t.Fatal(err)
	}
	if dataset.DatasetSHA256 == "" || len(dataset.Sources) != 1 || dataset.Pattern.ReportCount != 2 {
		t.Fatalf("dataset = %#v", dataset)
	}
}

func TestLoadFrozenDatasetRejectsChangedReferencedFile(t *testing.T) {
	path := writeFrozenDatasetFixture(t)
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "sources", "case-001.json"), []byte(`{}`), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFrozenDataset(path); err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("expected hash mismatch, got %v", err)
	}
}

func TestLoadFrozenDatasetRejectsUnknownBaselineSourceRef(t *testing.T) {
	path := writeFrozenDatasetFixture(t)
	var dataset DatasetManifest
	if err := decodeStrictJSONFile(path, &dataset); err != nil {
		t.Fatal(err)
	}
	dataset.Cases[0].EvidenceBaseline.Items[0].SourceRefs[0] = "source-001/event-999999"
	payload, _ := json.MarshalIndent(dataset, "", "  ")
	if err := os.WriteFile(path, append(payload, '\n'), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFrozenDataset(path); err == nil || !strings.Contains(err.Error(), "unknown source") {
		t.Fatalf("expected source reference failure, got %v", err)
	}
}
