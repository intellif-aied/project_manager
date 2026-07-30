package reporteval

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const PatternStatisticsSchemaVersion = "production-report-pattern-statistics/v1"

type PatternRange struct {
	Min int `json:"min,omitempty"`
	P50 int `json:"p50"`
	P90 int `json:"p90"`
	P95 int `json:"p95,omitempty"`
	Max int `json:"max"`
}

type PatternDateRange struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// PatternStatistics is descriptive production-shape evidence. It is never a
// fact ground truth and must not change a factual review grade.
type PatternStatistics struct {
	SchemaVersion               string           `json:"schema_version"`
	ReportCount                 int              `json:"report_count"`
	AuthorCount                 int              `json:"author_count"`
	DateCount                   int              `json:"date_count"`
	DateRange                   PatternDateRange `json:"date_range"`
	WithNextDayPlan             int              `json:"with_next_day_plan"`
	FormatClassCounts           map[string]int   `json:"format_class_counts"`
	CharacterCount              PatternRange     `json:"character_count"`
	LineCount                   PatternRange     `json:"line_count"`
	OrderedItemCount            PatternRange     `json:"ordered_item_count"`
	HeadingCount                PatternRange     `json:"heading_count"`
	ReportsPerDate              map[string]int   `json:"reports_per_date"`
	ReportsPerAuthor            PatternRange     `json:"reports_per_author"`
	ExactDuplicateExtraRows     int              `json:"exact_duplicate_extra_rows"`
	LiteralNewlineEscapeReports int              `json:"literal_newline_escape_reports"`
	RedactionCounts             map[string]int   `json:"redaction_counts"`
}

func (statistics PatternStatistics) Validate() error {
	if statistics.SchemaVersion != PatternStatisticsSchemaVersion {
		return fmt.Errorf("pattern statistics schema_version must be %q", PatternStatisticsSchemaVersion)
	}
	if statistics.ReportCount <= 0 || statistics.AuthorCount <= 0 || statistics.DateCount <= 0 {
		return errors.New("pattern statistics counts must be positive")
	}
	start, startErr := time.Parse("2006-01-02", statistics.DateRange.Start)
	end, endErr := time.Parse("2006-01-02", statistics.DateRange.End)
	if startErr != nil || endErr != nil || end.Before(start) {
		return errors.New("pattern statistics date_range is invalid")
	}
	if statistics.WithNextDayPlan < 0 || statistics.WithNextDayPlan > statistics.ReportCount ||
		statistics.ExactDuplicateExtraRows < 0 || statistics.LiteralNewlineEscapeReports < 0 {
		return errors.New("pattern statistics secondary counts are invalid")
	}
	formatTotal := 0
	for name, count := range statistics.FormatClassCounts {
		if strings.TrimSpace(name) == "" || count < 0 {
			return errors.New("pattern format_class_counts is invalid")
		}
		formatTotal += count
	}
	if formatTotal != statistics.ReportCount {
		return fmt.Errorf("pattern format class total %d does not match report_count %d", formatTotal, statistics.ReportCount)
	}
	dateTotal := 0
	if len(statistics.ReportsPerDate) != statistics.DateCount {
		return errors.New("pattern reports_per_date does not match date_count")
	}
	for date, count := range statistics.ReportsPerDate {
		parsed, err := time.Parse("2006-01-02", date)
		if err != nil || parsed.Before(start) || parsed.After(end) || count < 0 {
			return errors.New("pattern reports_per_date is invalid")
		}
		dateTotal += count
	}
	if dateTotal != statistics.ReportCount {
		return errors.New("pattern reports_per_date does not match report_count")
	}
	for _, count := range statistics.RedactionCounts {
		if count < 0 {
			return errors.New("pattern redaction_counts is invalid")
		}
	}
	for name, distribution := range map[string]PatternRange{
		"character_count": statistics.CharacterCount, "line_count": statistics.LineCount,
		"ordered_item_count": statistics.OrderedItemCount, "heading_count": statistics.HeadingCount,
		"reports_per_author": statistics.ReportsPerAuthor,
	} {
		if distribution.Min < 0 || distribution.Min > distribution.P50 || distribution.P50 > distribution.P90 ||
			distribution.P90 > distribution.Max || distribution.P95 != 0 && (distribution.P90 > distribution.P95 || distribution.P95 > distribution.Max) {
			return fmt.Errorf("pattern %s distribution is invalid", name)
		}
	}
	return nil
}

type FrozenDataset struct {
	Manifest       DatasetManifest
	ManifestPath   string
	BaseDir        string
	DatasetSHA256  string
	Sources        map[string]SourceEvidence
	SourcePayloads map[string][]byte
	Pattern        PatternStatistics
	PatternPayload []byte
}

func LoadFrozenDataset(manifestPath string) (FrozenDataset, error) {
	manifestPath = strings.TrimSpace(manifestPath)
	if manifestPath == "" {
		return FrozenDataset{}, errors.New("dataset manifest path is required")
	}
	manifestPath, err := filepath.Abs(manifestPath)
	if err != nil {
		return FrozenDataset{}, err
	}
	var manifest DatasetManifest
	if err := decodeStrictJSONFile(manifestPath, &manifest); err != nil {
		return FrozenDataset{}, err
	}
	NormalizeDataset(&manifest)
	if err := manifest.Validate(); err != nil {
		return FrozenDataset{}, fmt.Errorf("invalid dataset manifest: %w", err)
	}
	datasetHash, err := CanonicalSHA256(manifest)
	if err != nil {
		return FrozenDataset{}, err
	}
	result := FrozenDataset{
		Manifest: manifest, ManifestPath: manifestPath, BaseDir: filepath.Dir(manifestPath), DatasetSHA256: datasetHash,
		Sources: map[string]SourceEvidence{}, SourcePayloads: map[string][]byte{},
	}
	seenSourceHashes := map[string]string{}
	for _, item := range manifest.Cases {
		payload, err := result.readReferencedFile(item.SourceEvidence)
		if err != nil {
			return FrozenDataset{}, fmt.Errorf("case %s source evidence: %w", item.CaseID, err)
		}
		var source SourceEvidence
		if err := decodeStrictJSON(payload, &source); err != nil {
			return FrozenDataset{}, fmt.Errorf("case %s source evidence: %w", item.CaseID, err)
		}
		if err := source.Validate(); err != nil {
			return FrozenDataset{}, fmt.Errorf("case %s source evidence: %w", item.CaseID, err)
		}
		if previous, duplicate := seenSourceHashes[source.SourceIdentitySHA256]; duplicate {
			return FrozenDataset{}, fmt.Errorf("cases %s and %s use the same source identity", previous, item.CaseID)
		}
		seenSourceHashes[source.SourceIdentitySHA256] = item.CaseID
		if err := ValidateBaselineSourceRefs(item.EvidenceBaseline, source, item.ReportDate); err != nil {
			return FrozenDataset{}, fmt.Errorf("case %s baseline: %w", item.CaseID, err)
		}
		result.Sources[item.CaseID] = source
		result.SourcePayloads[item.CaseID] = payload
	}
	result.PatternPayload, err = result.readReferencedFile(manifest.PatternBaseline.Statistics)
	if err != nil {
		return FrozenDataset{}, fmt.Errorf("pattern baseline: %w", err)
	}
	if err := decodeStrictJSON(result.PatternPayload, &result.Pattern); err != nil {
		return FrozenDataset{}, fmt.Errorf("pattern baseline: %w", err)
	}
	if err := result.Pattern.Validate(); err != nil {
		return FrozenDataset{}, err
	}
	return result, nil
}

func (dataset FrozenDataset) readReferencedFile(reference FileReference) ([]byte, error) {
	if err := reference.Validate("file_reference"); err != nil {
		return nil, err
	}
	filePath := filepath.Join(dataset.BaseDir, filepath.FromSlash(reference.Path))
	payload, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	if actual := CanonicalBytesSHA256(payload); actual != reference.SHA256 {
		return nil, fmt.Errorf("sha256 mismatch for %s: got %s", reference.Path, actual)
	}
	return payload, nil
}

func decodeStrictJSONFile(path string, output any) error {
	payload, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := decodeStrictJSON(payload, output); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func decodeStrictJSON(payload []byte, output any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("JSON contains multiple values")
		}
		return err
	}
	return nil
}

func sortedCaseIDs(values map[string]SourceEvidence) []string {
	result := make([]string, 0, len(values))
	for caseID := range values {
		result = append(result, caseID)
	}
	sort.Strings(result)
	return result
}
