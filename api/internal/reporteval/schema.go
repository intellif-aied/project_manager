package reporteval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	DatasetSchemaVersion = "daily-report-evaluation-dataset/v1"
	PlanSchemaVersion    = "daily-report-evaluation-plan/v1"
	BundleSchemaVersion  = "daily-report-evaluation-bundle/v1"
	SourceSchemaVersion  = "daily-report-source-evidence/v1"
	MetricsSchemaVersion = "daily-report-run-metrics/v1"
)

var safeIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type DatasetManifest struct {
	SchemaVersion  string           `json:"schema_version"`
	DatasetVersion string           `json:"dataset_version"`
	RubricVersion  string           `json:"rubric_version"`
	ReportType     string           `json:"report_type"`
	Cases          []EvaluationCase `json:"cases"`
}

type EvaluationCase struct {
	CaseID                       string          `json:"case_id"`
	ReportDate                   string          `json:"report_date"`
	SelectedSessionSliceKeys     []string        `json:"selected_session_slice_keys"`
	ExpectedSourceIdentitySHA256 string          `json:"expected_source_identity_sha256,omitempty"`
	EvidenceBaseline             json.RawMessage `json:"evidence_baseline"`
	Tags                         []string        `json:"tags"`
	Source                       string          `json:"source"`
	UsageAuthorized              bool            `json:"usage_authorized"`
}

type ExecutionPlan struct {
	SchemaVersion  string        `json:"schema_version"`
	DatasetFile    string        `json:"dataset_file"`
	Repetitions    int           `json:"repetitions"`
	TimeoutSeconds int           `json:"timeout_seconds"`
	Variants       []VariantSpec `json:"variants"`
}

type VariantSpec struct {
	VariantVersion          string `json:"variant_version"`
	AgentID                 string `json:"agent_id"`
	ModelID                 string `json:"model_id,omitempty"`
	ExpectedVariantSHA256   string `json:"expected_variant_sha256,omitempty"`
	ExpectedPipelineProfile string `json:"expected_pipeline_profile,omitempty"`
}

type RunReceipt struct {
	CaseID         string            `json:"case_id"`
	VariantVersion string            `json:"variant_version"`
	Repetition     int               `json:"repetition"`
	RunID          string            `json:"run_id"`
	Status         string            `json:"status"`
	FailureStage   string            `json:"failure_stage,omitempty"`
	ErrorCode      string            `json:"error_code,omitempty"`
	VariantSHA256  string            `json:"variant_sha256"`
	ArtifactSHA256 map[string]string `json:"artifact_sha256,omitempty"`
}

type BundleManifest struct {
	SchemaVersion  string        `json:"schema_version"`
	DatasetVersion string        `json:"dataset_version"`
	DatasetSHA256  string        `json:"dataset_sha256"`
	RubricVersion  string        `json:"rubric_version"`
	CreatedAt      time.Time     `json:"created_at"`
	Variants       []VariantSpec `json:"variants"`
	Runs           []RunReceipt  `json:"runs"`
}

type SourceEvidence struct {
	SchemaVersion        string               `json:"schema_version"`
	SourceIdentitySHA256 string               `json:"source_identity_set_sha256"`
	SelectionID          string               `json:"selection_id"`
	ReadMode             string               `json:"read_mode"`
	DigestVersion        string               `json:"digest_version"`
	RedactionVersion     string               `json:"redaction_version"`
	DigestSHA256         string               `json:"digest_sha256"`
	DigestPayload        json.RawMessage      `json:"digest_payload"`
	Items                []SourceEvidenceItem `json:"items"`
}

type SourceEvidenceItem struct {
	SessionID             string `json:"session_id"`
	SessionRef            string `json:"session_ref"`
	AgentType             string `json:"agent_type"`
	SessionContentSliceID string `json:"session_content_slice_id"`
	SourceGenerationID    string `json:"source_generation_id"`
	ProjectionRevisionID  string `json:"content_projection_revision_id"`
	ContentEpoch          int64  `json:"content_epoch"`
	StartCursor           int64  `json:"start_cursor"`
	EndCursor             int64  `json:"end_cursor"`
	DigestSHA256          string `json:"digest_sha256"`
	DigestVersion         string `json:"digest_version"`
}

type AvailabilityValue struct {
	Status string `json:"status"`
	Value  *int64 `json:"value,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type RunMetrics struct {
	SchemaVersion         string            `json:"schema_version"`
	Status                string            `json:"status"`
	FailureStage          string            `json:"failure_stage,omitempty"`
	ErrorCode             string            `json:"error_code,omitempty"`
	DurationMS            *int64            `json:"duration_ms,omitempty"`
	BriefInvalidAttempts  int               `json:"brief_invalid_attempts"`
	ResultInvalidAttempts int               `json:"result_invalid_attempts"`
	InputTokens           AvailabilityValue `json:"input_tokens"`
	OutputTokens          AvailabilityValue `json:"output_tokens"`
	CostMicrousd          AvailabilityValue `json:"cost_microusd"`
}

func (dataset DatasetManifest) Validate() error {
	if dataset.SchemaVersion != DatasetSchemaVersion {
		return fmt.Errorf("schema_version must be %q", DatasetSchemaVersion)
	}
	if strings.TrimSpace(dataset.DatasetVersion) == "" || strings.TrimSpace(dataset.RubricVersion) == "" {
		return errors.New("dataset_version and rubric_version are required")
	}
	if dataset.ReportType != "personal_daily" {
		return errors.New("report_type must be personal_daily")
	}
	if len(dataset.Cases) == 0 {
		return errors.New("at least one case is required")
	}
	seen := map[string]bool{}
	for index, item := range dataset.Cases {
		prefix := fmt.Sprintf("cases[%d]", index)
		if !safeIdentifierPattern.MatchString(item.CaseID) || seen[item.CaseID] {
			return fmt.Errorf("%s.case_id must be unique and non-empty", prefix)
		}
		seen[item.CaseID] = true
		if _, err := time.Parse("2006-01-02", item.ReportDate); err != nil {
			return fmt.Errorf("%s.report_date must be YYYY-MM-DD", prefix)
		}
		if len(item.SelectedSessionSliceKeys) == 0 {
			return fmt.Errorf("%s.selected_session_slice_keys is required", prefix)
		}
		for _, sliceKey := range item.SelectedSessionSliceKeys {
			if _, err := uuid.Parse(sliceKey); err != nil {
				return fmt.Errorf("%s.selected_session_slice_keys contains invalid UUID", prefix)
			}
		}
		if !item.UsageAuthorized {
			return fmt.Errorf("%s.usage_authorized must be true", prefix)
		}
		if strings.TrimSpace(item.Source) == "" || len(item.EvidenceBaseline) == 0 || !json.Valid(item.EvidenceBaseline) {
			return fmt.Errorf("%s source and valid evidence_baseline are required", prefix)
		}
		if hash := item.ExpectedSourceIdentitySHA256; hash != "" && !isSHA256(hash) {
			return fmt.Errorf("%s.expected_source_identity_sha256 is invalid", prefix)
		}
	}
	return nil
}

func (plan ExecutionPlan) Validate() error {
	if plan.SchemaVersion != PlanSchemaVersion {
		return fmt.Errorf("schema_version must be %q", PlanSchemaVersion)
	}
	if strings.TrimSpace(plan.DatasetFile) == "" {
		return errors.New("dataset_file is required")
	}
	if plan.Repetitions < 1 || plan.Repetitions > 5 {
		return errors.New("repetitions must be between 1 and 5")
	}
	if plan.TimeoutSeconds < 30 || plan.TimeoutSeconds > 7200 {
		return errors.New("timeout_seconds must be between 30 and 7200")
	}
	if len(plan.Variants) < 2 || len(plan.Variants) > 3 {
		return errors.New("exactly 2 or 3 variants are required")
	}
	seen := map[string]bool{}
	for index, variant := range plan.Variants {
		if !safeIdentifierPattern.MatchString(variant.VariantVersion) || variant.AgentID == "" || seen[variant.VariantVersion] {
			return fmt.Errorf("variants[%d] requires unique variant_version and agent_id", index)
		}
		seen[variant.VariantVersion] = true
		if variant.ExpectedVariantSHA256 != "" && !isSHA256(variant.ExpectedVariantSHA256) {
			return fmt.Errorf("variants[%d].expected_variant_sha256 is invalid", index)
		}
	}
	return nil
}

func CanonicalSHA256(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func NormalizeDataset(dataset *DatasetManifest) {
	for index := range dataset.Cases {
		sort.Strings(dataset.Cases[index].Tags)
		sort.Strings(dataset.Cases[index].SelectedSessionSliceKeys)
	}
	sort.Slice(dataset.Cases, func(i, j int) bool { return dataset.Cases[i].CaseID < dataset.Cases[j].CaseID })
}

func isSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
