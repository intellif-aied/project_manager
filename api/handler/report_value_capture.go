package handler

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

const reportVariantManifestSchemaVersion = "report-generation-variant/v2"

type reportVariantManifest struct {
	SchemaVersion         string            `json:"schema_version"`
	PipelineProfile       string            `json:"pipeline_profile"`
	Stages                []string          `json:"stages"`
	AgentID               string            `json:"agent_id"`
	AgentVersionID        *int              `json:"agent_version_id"`
	ModelID               *string           `json:"model_id"`
	DigestVersion         string            `json:"digest_version"`
	RedactionVersion      string            `json:"redaction_version"`
	ContextSchemaVersion  string            `json:"context_schema_version"`
	ContextRepresentation string            `json:"context_representation"`
	BriefSchemaVersion    string            `json:"brief_schema_version"`
	ReportAgentSource     string            `json:"report_agent_source"`
	ReportSkillSlug       string            `json:"report_skill_slug"`
	ReportSkillVersion    string            `json:"report_skill_version"`
	ReportMCPSlug         string            `json:"report_mcp_slug"`
	ReportMCPVersion      string            `json:"report_mcp_version"`
	CodeRevision          string            `json:"code_revision"`
	PromptSHA256          string            `json:"prompt_sha256"`
	SkillSHA256           string            `json:"skill_sha256"`
	StageVersions         map[string]string `json:"stage_versions"`
	ValidationRules       []string          `json:"validation_rules"`
	ConfigSHA256          string            `json:"config_sha256"`
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func buildReportVariantManifest(run *reportAIRun, briefSchemaVersion string) ([]byte, string, error) {
	stages := []string{"digest", "context", "final"}
	pipelineProfile := "digest_context_final"
	if strings.TrimSpace(briefSchemaVersion) != "" {
		stages = []string{"digest", "context", "brief", "final"}
		pipelineProfile = "digest_context_brief_final"
	}
	reportAgentSource := strings.TrimSpace(run.ReportAgentSource)
	if reportAgentSource == "" {
		reportAgentSource = managedAgentSourceSystem
	}
	manifest := reportVariantManifest{
		SchemaVersion:         reportVariantManifestSchemaVersion,
		PipelineProfile:       pipelineProfile,
		Stages:                stages,
		AgentID:               strings.TrimSpace(run.AgentID),
		AgentVersionID:        run.AgentVersionID,
		ModelID:               run.ModelID,
		DigestVersion:         strings.TrimSpace(stringFromAny(run.InputRef["digest_version"])),
		RedactionVersion:      strings.TrimSpace(stringFromAny(run.InputRef["redaction_version"])),
		ContextSchemaVersion:  strings.TrimSpace(stringFromAny(run.InputRef["report_context_schema_version"])),
		ContextRepresentation: strings.TrimSpace(run.ContextRepresentation),
		BriefSchemaVersion:    strings.TrimSpace(briefSchemaVersion),
		ReportAgentSource:     reportAgentSource,
		ReportMCPSlug:         strings.TrimSpace(stringFromAny(run.InputRef["mcp_server"])),
		ReportMCPVersion:      strings.TrimSpace(stringFromAny(run.InputRef["report_mcp_version"])),
		CodeRevision:          manifestValue(run.InputRef, "code_revision"),
		PromptSHA256:          manifestValue(run.InputRef, "report_prompt_sha256"),
		SkillSHA256:           manifestValue(run.InputRef, "report_skill_sha256"),
		StageVersions: map[string]string{
			"digest":  manifestVersion(run.InputRef, "digest_version"),
			"context": manifestVersion(run.InputRef, "report_context_schema_version"),
			"final":   manifestVersion(run.InputRef, "report_skill_version"),
		},
		ValidationRules: []string{"source_identity_hash", "context_contract", "final_result_contract"},
	}
	if briefSchemaVersion != "" {
		manifest.StageVersions["brief"] = briefSchemaVersion
		manifest.ValidationRules = append(manifest.ValidationRules, "brief_fact_reference_contract")
	}
	if manifest.StageVersions["final"] == "not_applicable" && run.AgentVersionID != nil {
		manifest.StageVersions["final"] = fmt.Sprintf("managed-agent-version/%d", *run.AgentVersionID)
	}
	if reportAgentSource == managedAgentSourceSystem {
		manifest.ReportSkillSlug = strings.TrimSpace(stringFromAny(run.InputRef["report_skill_slug"]))
		manifest.ReportSkillVersion = strings.TrimSpace(stringFromAny(run.InputRef["report_skill_version"]))
	}
	configPayload, err := json.Marshal(manifest)
	if err != nil {
		return nil, "", err
	}
	manifest.ConfigSHA256 = sha256Hex(string(configPayload))
	payload, err := json.Marshal(manifest)
	if err != nil {
		return nil, "", err
	}
	var canonical any
	if err := json.Unmarshal(payload, &canonical); err != nil {
		return nil, "", err
	}
	payload, err = json.Marshal(canonical)
	if err != nil {
		return nil, "", err
	}
	return payload, sha256Hex(string(payload)), nil
}

func manifestValue(values map[string]any, key string) string {
	value := strings.TrimSpace(stringFromAny(values[key]))
	if value == "" {
		return "not_available"
	}
	return value
}

func manifestVersion(values map[string]any, key string) string {
	value := strings.TrimSpace(stringFromAny(values[key]))
	if value == "" {
		return "not_applicable"
	}
	return value
}

func insertReportGenerationSnapshot(
	ctx context.Context,
	tx *sql.Tx,
	run *reportAIRun,
	reportID string,
	userID string,
	reportDate string,
	content string,
	summary string,
	briefSchemaVersion string,
) error {
	manifest, variantHash, err := buildReportVariantManifest(run, briefSchemaVersion)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO report_generation_snapshots (
			run_id, report_id, user_id, report_date,
			generated_content, generated_content_sha256,
			summary_content, summary_sha256,
			variant_manifest_json, variant_sha256
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		run.ID, reportID, userID, reportDate,
		content, sha256Hex(strings.TrimSpace(content)),
		summary, sha256Hex(strings.TrimSpace(summary)),
		manifest, variantHash,
	)
	return err
}

func insertReportUserOutcome(
	ctx context.Context,
	tx *sql.Tx,
	reportID string,
	userID string,
	reportDate string,
	runID sql.NullString,
	action string,
	content *string,
) error {
	var contentValue any
	var contentHash any
	if content != nil {
		contentValue = *content
		contentHash = sha256Hex(strings.TrimSpace(*content))
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO report_user_outcome_events (
			report_id, user_id, report_date, managed_agent_run_id,
			action, content, content_sha256, action_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, now())`,
		reportID, userID, reportDate, nullableOutcomeRunID(runID), action, contentValue, contentHash,
	)
	return err
}

func nullableOutcomeRunID(value sql.NullString) any {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil
	}
	return strings.TrimSpace(value.String)
}
