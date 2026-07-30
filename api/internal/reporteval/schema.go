package reporteval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	DatasetSchemaVersion          = "daily-report-evaluation-dataset/v2"
	PlanSchemaVersion             = "daily-report-evaluation-plan/v2"
	BundleSchemaVersion           = "daily-report-evaluation-bundle/v2"
	SourceSchemaVersion           = "daily-report-source-evidence/v2"
	EvidenceBaselineSchemaVersion = "daily-report-evidence-baseline/v1"
	MetricsSchemaVersion          = "daily-report-run-metrics/v1"
	RuntimeAttestationVersion     = "daily-report-evaluation-runtime/v1"
)

var (
	safeIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	environmentKeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,127}$`)
)

type FileReference struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type PatternBaselineReference struct {
	DatasetVersion string        `json:"dataset_version"`
	Statistics     FileReference `json:"statistics"`
}

type DatasetManifest struct {
	SchemaVersion   string                   `json:"schema_version"`
	DatasetVersion  string                   `json:"dataset_version"`
	RubricVersion   string                   `json:"rubric_version"`
	ReportType      string                   `json:"report_type"`
	PatternBaseline PatternBaselineReference `json:"pattern_baseline"`
	Cases           []EvaluationCase         `json:"cases"`
}

type EvaluationCase struct {
	CaseID                   string           `json:"case_id"`
	ReportDate               string           `json:"report_date"`
	SelectedSessionSliceKeys []string         `json:"selected_session_slice_keys"`
	SourceEvidence           FileReference    `json:"source_evidence"`
	EvidenceBaseline         EvidenceBaseline `json:"evidence_baseline"`
	Tags                     []string         `json:"tags"`
	Source                   string           `json:"source"`
	UsageAuthorized          bool             `json:"usage_authorized"`
}

type EvidenceBaseline struct {
	SchemaVersion      string          `json:"schema_version"`
	Items              []EvidenceItem  `json:"items"`
	TopicRelations     []TopicRelation `json:"topic_relations"`
	ForbiddenAdditions []string        `json:"forbidden_additions"`
	NoReportableWork   bool            `json:"no_reportable_work"`
}

type EvidenceItem struct {
	EvidenceID  string   `json:"evidence_id"`
	Disposition string   `json:"disposition"`
	Statement   string   `json:"statement"`
	SourceRefs  []string `json:"source_refs"`
	State       string   `json:"state"`
	Environment string   `json:"environment"`
}

type TopicRelation struct {
	Relation     string   `json:"relation"`
	EvidenceRefs []string `json:"evidence_refs"`
}

type ExecutionPlan struct {
	SchemaVersion  string        `json:"schema_version"`
	DatasetFile    string        `json:"dataset_file"`
	Repetitions    int           `json:"repetitions"`
	TimeoutSeconds int           `json:"timeout_seconds"`
	Variants       []VariantSpec `json:"variants"`
}

type RuntimeSpec struct {
	BaseURL       string            `json:"base_url"`
	TokenEnv      string            `json:"token_env,omitempty"`
	CaseTokenEnvs map[string]string `json:"case_token_envs,omitempty"`
}

type VariantSpec struct {
	VariantVersion          string      `json:"variant_version"`
	AgentID                 string      `json:"agent_id"`
	ModelID                 string      `json:"model_id,omitempty"`
	ExpectedVariantSHA256   string      `json:"expected_variant_sha256,omitempty"`
	ExpectedPipelineProfile string      `json:"expected_pipeline_profile,omitempty"`
	Runtime                 RuntimeSpec `json:"runtime"`
}

type VariantDescriptor struct {
	VariantVersion          string `json:"variant_version"`
	AgentID                 string `json:"agent_id"`
	ModelID                 string `json:"model_id,omitempty"`
	ExpectedVariantSHA256   string `json:"expected_variant_sha256,omitempty"`
	ExpectedPipelineProfile string `json:"expected_pipeline_profile,omitempty"`
}

func (variant VariantSpec) Descriptor() VariantDescriptor {
	return VariantDescriptor{
		VariantVersion: variant.VariantVersion, AgentID: variant.AgentID, ModelID: variant.ModelID,
		ExpectedVariantSHA256:   variant.ExpectedVariantSHA256,
		ExpectedPipelineProfile: variant.ExpectedPipelineProfile,
	}
}

type RuntimeAttestation struct {
	SchemaVersion string `json:"schema_version"`
	Enabled       bool   `json:"enabled"`
	Environment   string `json:"environment"`
	BuildRevision string `json:"build_revision"`
	InstanceID    string `json:"instance_id"`
}

type RunReceipt struct {
	CaseID         string             `json:"case_id"`
	VariantVersion string             `json:"variant_version"`
	Repetition     int                `json:"repetition"`
	RunID          string             `json:"run_id"`
	Status         string             `json:"status"`
	FailureStage   string             `json:"failure_stage,omitempty"`
	ErrorCode      string             `json:"error_code,omitempty"`
	VariantSHA256  string             `json:"variant_sha256"`
	Runtime        RuntimeAttestation `json:"runtime"`
	ArtifactSHA256 map[string]string  `json:"artifact_sha256,omitempty"`
}

type BundleManifest struct {
	SchemaVersion  string              `json:"schema_version"`
	DatasetVersion string              `json:"dataset_version"`
	DatasetSHA256  string              `json:"dataset_sha256"`
	PlanSHA256     string              `json:"plan_sha256"`
	RubricVersion  string              `json:"rubric_version"`
	Repetitions    int                 `json:"repetitions"`
	CreatedAt      time.Time           `json:"created_at"`
	Variants       []VariantDescriptor `json:"variants"`
	Runs           []RunReceipt        `json:"runs"`
}

type SourceEvidence struct {
	SchemaVersion        string               `json:"schema_version"`
	SourceIdentitySHA256 string               `json:"source_identity_set_sha256"`
	RedactionVersion     string               `json:"redaction_version"`
	Items                []SourceEvidenceItem `json:"items"`
}

type SourceEvidenceItem struct {
	EvidenceSourceID string                `json:"evidence_source_id"`
	AgentType        string                `json:"agent_type"`
	Events           []SourceEvidenceEvent `json:"events"`
}

type SourceEvidenceEvent struct {
	EvidenceRef string          `json:"evidence_ref"`
	OccurredAt  time.Time       `json:"occurred_at"`
	EventType   string          `json:"event_type"`
	Summary     string          `json:"summary,omitempty"`
	Excerpt     string          `json:"excerpt,omitempty"`
	Payload     json.RawMessage `json:"payload"`
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
	if strings.TrimSpace(dataset.PatternBaseline.DatasetVersion) == "" {
		return errors.New("pattern_baseline.dataset_version is required")
	}
	if err := dataset.PatternBaseline.Statistics.Validate("pattern_baseline.statistics"); err != nil {
		return err
	}
	referencedPaths := map[string]string{}
	if err := registerDatasetFileReference("pattern_baseline.statistics", dataset.PatternBaseline.Statistics, referencedPaths); err != nil {
		return err
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
		seenSliceKeys := map[string]bool{}
		for _, sliceKey := range item.SelectedSessionSliceKeys {
			if sliceKey != strings.TrimSpace(sliceKey) || seenSliceKeys[sliceKey] {
				return fmt.Errorf("%s.selected_session_slice_keys must be normalized and unique", prefix)
			}
			if _, err := uuid.Parse(sliceKey); err != nil {
				return fmt.Errorf("%s.selected_session_slice_keys contains invalid UUID", prefix)
			}
			seenSliceKeys[sliceKey] = true
		}
		if err := item.SourceEvidence.Validate(prefix + ".source_evidence"); err != nil {
			return err
		}
		if err := registerDatasetFileReference(prefix+".source_evidence", item.SourceEvidence, referencedPaths); err != nil {
			return err
		}
		if err := item.EvidenceBaseline.Validate(prefix + ".evidence_baseline"); err != nil {
			return err
		}
		if !item.UsageAuthorized {
			return fmt.Errorf("%s.usage_authorized must be true", prefix)
		}
		if strings.TrimSpace(item.Source) == "" {
			return fmt.Errorf("%s.source is required", prefix)
		}
		if len(uniqueNonEmpty(item.Tags)) != len(item.Tags) {
			return fmt.Errorf("%s.tags must be non-empty and unique", prefix)
		}
	}
	return nil
}

func registerDatasetFileReference(field string, reference FileReference, seen map[string]string) error {
	root := strings.SplitN(reference.Path, "/", 2)[0]
	switch root {
	case "manifest.json", "execution-plan.json", "dataset-manifest.json", "cases":
		return fmt.Errorf("%s.path uses reserved bundle path %s", field, reference.Path)
	}
	if previous, exists := seen[reference.Path]; exists {
		return fmt.Errorf("%s.path %s is already used by %s", field, reference.Path, previous)
	}
	seen[reference.Path] = field
	return nil
}

func (reference FileReference) Validate(field string) error {
	value := strings.TrimSpace(reference.Path)
	clean := path.Clean(value)
	if value == "" || clean == "." || path.IsAbs(value) || clean != value || strings.HasPrefix(clean, "../") || strings.Contains(value, `\`) {
		return fmt.Errorf("%s.path must be a normalized relative path", field)
	}
	if !isSHA256(reference.SHA256) {
		return fmt.Errorf("%s.sha256 is invalid", field)
	}
	return nil
}

func (baseline EvidenceBaseline) Validate(field string) error {
	if baseline.SchemaVersion != EvidenceBaselineSchemaVersion {
		return fmt.Errorf("%s.schema_version must be %q", field, EvidenceBaselineSchemaVersion)
	}
	if len(baseline.Items) == 0 && !baseline.NoReportableWork {
		return fmt.Errorf("%s requires items or no_reportable_work", field)
	}
	seen := map[string]bool{}
	hasReportable := false
	for index, item := range baseline.Items {
		prefix := fmt.Sprintf("%s.items[%d]", field, index)
		if !safeIdentifierPattern.MatchString(item.EvidenceID) || seen[item.EvidenceID] {
			return fmt.Errorf("%s.evidence_id must be unique and non-empty", prefix)
		}
		seen[item.EvidenceID] = true
		if item.Disposition != "required" && item.Disposition != "optional" && item.Disposition != "exclude" {
			return fmt.Errorf("%s.disposition is invalid", prefix)
		}
		if item.Disposition != "exclude" {
			hasReportable = true
		}
		if strings.TrimSpace(item.Statement) == "" || len(uniqueNonEmpty(item.SourceRefs)) != len(item.SourceRefs) || len(item.SourceRefs) == 0 {
			return fmt.Errorf("%s requires statement and unique source_refs", prefix)
		}
		if !validEvidenceState(item.State) || !validEvidenceEnvironment(item.Environment) {
			return fmt.Errorf("%s state or environment is invalid", prefix)
		}
	}
	if baseline.NoReportableWork && hasReportable {
		return fmt.Errorf("%s.no_reportable_work conflicts with required or optional items", field)
	}
	for index, relation := range baseline.TopicRelations {
		prefix := fmt.Sprintf("%s.topic_relations[%d]", field, index)
		if relation.Relation != "same_workstream" && relation.Relation != "separate_workstream" {
			return fmt.Errorf("%s.relation is invalid", prefix)
		}
		refs := uniqueNonEmpty(relation.EvidenceRefs)
		if len(refs) < 2 || len(refs) != len(relation.EvidenceRefs) {
			return fmt.Errorf("%s requires at least two unique evidence_refs", prefix)
		}
		for _, ref := range refs {
			if !seen[ref] {
				return fmt.Errorf("%s references unknown evidence item %s", prefix, ref)
			}
		}
	}
	if len(uniqueNonEmpty(baseline.ForbiddenAdditions)) != len(baseline.ForbiddenAdditions) {
		return fmt.Errorf("%s.forbidden_additions must be non-empty and unique", field)
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
		if !safeIdentifierPattern.MatchString(variant.VariantVersion) || strings.TrimSpace(variant.AgentID) == "" || seen[variant.VariantVersion] {
			return fmt.Errorf("variants[%d] requires unique variant_version and agent_id", index)
		}
		seen[variant.VariantVersion] = true
		if variant.ExpectedVariantSHA256 != "" && !isSHA256(variant.ExpectedVariantSHA256) {
			return fmt.Errorf("variants[%d].expected_variant_sha256 is invalid", index)
		}
		parsed, err := url.Parse(strings.TrimSpace(variant.Runtime.BaseURL))
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil ||
			(parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("variants[%d].runtime.base_url must be an HTTP(S) origin", index)
		}
		hasDefaultToken := strings.TrimSpace(variant.Runtime.TokenEnv) != ""
		hasCaseTokens := len(variant.Runtime.CaseTokenEnvs) > 0
		if hasDefaultToken == hasCaseTokens {
			return fmt.Errorf("variants[%d].runtime requires exactly one of token_env or case_token_envs", index)
		}
		if hasDefaultToken && !environmentKeyPattern.MatchString(strings.TrimSpace(variant.Runtime.TokenEnv)) {
			return fmt.Errorf("variants[%d].runtime.token_env is invalid", index)
		}
		for caseID, tokenEnv := range variant.Runtime.CaseTokenEnvs {
			if !safeIdentifierPattern.MatchString(caseID) || !environmentKeyPattern.MatchString(strings.TrimSpace(tokenEnv)) {
				return fmt.Errorf("variants[%d].runtime.case_token_envs contains an invalid case or environment key", index)
			}
		}
	}
	return nil
}

func (plan ExecutionPlan) ValidateForDataset(dataset DatasetManifest) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	if err := dataset.Validate(); err != nil {
		return fmt.Errorf("invalid dataset for plan: %w", err)
	}
	cases := make(map[string]bool, len(dataset.Cases))
	for _, item := range dataset.Cases {
		cases[item.CaseID] = true
	}
	for variantIndex, variant := range plan.Variants {
		if len(variant.Runtime.CaseTokenEnvs) > 0 {
			if len(variant.Runtime.CaseTokenEnvs) != len(cases) {
				return fmt.Errorf("variants[%d].runtime.case_token_envs must cover every dataset case exactly once", variantIndex)
			}
			for caseID := range variant.Runtime.CaseTokenEnvs {
				if !cases[caseID] {
					return fmt.Errorf("variants[%d].runtime.case_token_envs references unknown case %s", variantIndex, caseID)
				}
			}
		}
		for _, item := range dataset.Cases {
			if _, err := variant.Runtime.TokenEnvironment(item.CaseID); err != nil {
				return fmt.Errorf("variants[%d]: %w", variantIndex, err)
			}
		}
	}
	return nil
}

func (runtime RuntimeSpec) TokenEnvironment(caseID string) (string, error) {
	if tokenEnv := strings.TrimSpace(runtime.TokenEnv); tokenEnv != "" {
		return tokenEnv, nil
	}
	tokenEnv := strings.TrimSpace(runtime.CaseTokenEnvs[caseID])
	if tokenEnv == "" {
		return "", fmt.Errorf("runtime credential is missing for case %s", caseID)
	}
	return tokenEnv, nil
}

func (attestation RuntimeAttestation) Validate() error {
	if attestation.SchemaVersion != RuntimeAttestationVersion || !attestation.Enabled || attestation.Environment != "test" {
		return errors.New("runtime is not an enabled test evaluation runtime")
	}
	buildRevision := strings.TrimSpace(attestation.BuildRevision)
	if buildRevision == "" || buildRevision == "not_available" || buildRevision == "unknown" || strings.TrimSpace(attestation.InstanceID) == "" {
		return errors.New("runtime attestation is incomplete")
	}
	return nil
}

func validateVariantIdentityConsistency(runs []RunReceipt) error {
	versionHashes := map[string]string{}
	hashVersions := map[string]string{}
	for _, run := range runs {
		if !safeIdentifierPattern.MatchString(run.VariantVersion) || !isSHA256(run.VariantSHA256) {
			return fmt.Errorf("run %s has invalid variant identity", run.RunID)
		}
		if previousHash, exists := versionHashes[run.VariantVersion]; exists && previousHash != run.VariantSHA256 {
			return fmt.Errorf("variant %s maps to multiple actual manifests", run.VariantVersion)
		}
		if previousVersion, exists := hashVersions[run.VariantSHA256]; exists && previousVersion != run.VariantVersion {
			return fmt.Errorf("variants %s and %s share the same actual manifest", previousVersion, run.VariantVersion)
		}
		versionHashes[run.VariantVersion] = run.VariantSHA256
		hashVersions[run.VariantSHA256] = run.VariantVersion
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
		for itemIndex := range dataset.Cases[index].EvidenceBaseline.Items {
			sort.Strings(dataset.Cases[index].EvidenceBaseline.Items[itemIndex].SourceRefs)
		}
		for relationIndex := range dataset.Cases[index].EvidenceBaseline.TopicRelations {
			sort.Strings(dataset.Cases[index].EvidenceBaseline.TopicRelations[relationIndex].EvidenceRefs)
		}
		sort.Strings(dataset.Cases[index].EvidenceBaseline.ForbiddenAdditions)
	}
	sort.Slice(dataset.Cases, func(i, j int) bool { return dataset.Cases[i].CaseID < dataset.Cases[j].CaseID })
}

func validEvidenceState(value string) bool {
	switch value {
	case "released", "validated", "completed", "in_progress", "blocked", "none":
		return true
	default:
		return false
	}
}

func validEvidenceEnvironment(value string) bool {
	switch value {
	case "production", "test", "development", "none":
		return true
	default:
		return false
	}
}

func uniqueNonEmpty(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func isSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
