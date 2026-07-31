package handler

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/aidashboard/api/internal/autodailyreport"
	"github.com/aidashboard/api/internal/biztime"
	"github.com/aidashboard/api/internal/reportcontext"
	"github.com/aidashboard/api/internal/reportsource"
	"github.com/aidashboard/api/internal/sessiondigestv2"
	"github.com/aidashboard/api/model"
)

func (h *ManagedAgentHandler) SubmitAutoDailyReport(
	ctx context.Context, request autodailyreport.SubmissionRequest,
) (string, error) {
	if h == nil {
		return "", managedAgentConfigError("managed Agent handler is not configured")
	}
	userID := strings.TrimSpace(request.UserID)
	reportDate := strings.TrimSpace(request.ReportDate)
	if userID == "" {
		return "", errors.New("automatic report user is required")
	}
	if _, err := biztime.ParseDate(reportDate); err != nil {
		return "", fmt.Errorf("invalid automatic report date: %w", err)
	}
	if request.Guard.Mode != autodailyreport.GuardModeAbsent && request.Guard.Mode != autodailyreport.GuardModeReplace {
		return "", errors.New("invalid automatic report write guard")
	}
	if request.Guard.Mode == autodailyreport.GuardModeReplace &&
		(strings.TrimSpace(request.Guard.ReportID) == "" || request.Guard.UpdatedAt == nil) {
		return "", errors.New("automatic report replace guard is incomplete")
	}
	fingerprint := strings.TrimSpace(request.SourceFingerprint)
	decodedFingerprint, decodeErr := hex.DecodeString(fingerprint)
	if decodeErr != nil || len(decodedFingerprint) != 32 || fingerprint != strings.ToLower(fingerprint) {
		return "", errors.New("automatic report source fingerprint must be a lowercase SHA-256")
	}
	sourceSliceKeys := make([]string, 0, len(request.SourceSliceKeys))
	seen := map[string]struct{}{}
	for _, raw := range request.SourceSliceKeys {
		key := strings.TrimSpace(raw)
		if key == "" || !isValidUUID(key) {
			return "", errors.New("automatic report source keys must be slice UUIDs")
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		sourceSliceKeys = append(sourceSliceKeys, key)
	}
	if len(sourceSliceKeys) == 0 {
		return "", reportsource.ErrSourceUnavailable
	}
	sort.Strings(sourceSliceKeys)
	if h.client == nil || !h.client.Configured() {
		return "", managedAgentConfigError("managed Agent service is not configured")
	}
	if h.defaults.AIDAPublicBaseURL == "" {
		return "", managedAgentConfigError("AIDA_PUBLIC_BASE_URL is required for Report Agent")
	}
	if h.reportSource == nil {
		return "", errors.New("report source service is not configured")
	}

	agent, err := h.systemReportAgent(ctx)
	if err != nil {
		return "", err
	}
	if agent == nil {
		return "", managedAgentConfigError("default system Report Agent is not configured")
	}
	if !containsString(reportTypesForAgent(*agent), reportTypePersonalDaily) {
		return "", managedAgentConfigError("default system Report Agent does not support personal_daily")
	}
	if err := h.resolveAndRepairReportAgent(
		ctx, h.client, agent, managedAgentSourceSystem, true,
	); err != nil {
		return "", err
	}
	if !h.hasRunnableReportMCP(*agent) {
		return "", managedAgentConfigError("Report Agent must bind Aida Report MCP")
	}
	reportSlots := map[string]struct{}{}
	for _, slot := range h.reportMCPCredentialSlots(*agent) {
		reportSlots[slot] = struct{}{}
	}
	if len(reportSlots) == 0 {
		reportSlots[h.defaults.ReportMCPCredentialSlot] = struct{}{}
	}
	if missing := h.missingRequiredCredentialSlots(*agent, reportSlots, nil); len(missing) > 0 {
		return "", managedAgentConfigError(
			"required credential slots are not bound: " + strings.Join(missing, ","),
		)
	}

	submission, err := h.buildAutoDailyReportSubmission(*agent, request, sourceSliceKeys, reportSlots)
	if err != nil {
		return "", err
	}
	result, err := h.reportSource.CreateReportRun(ctx, submission)
	if err != nil {
		return "", err
	}
	return result.RunID, nil
}

func (h *ManagedAgentHandler) buildAutoDailyReportSubmission(
	agent model.ManagedAgent,
	request autodailyreport.SubmissionRequest,
	sourceSliceKeys []string,
	reportSlots map[string]struct{},
) (reportsource.RunSubmissionRequest, error) {
	userID := strings.TrimSpace(request.UserID)
	reportDate := strings.TrimSpace(request.ReportDate)
	modelID := h.defaults.ReportModelID
	target := reportTarget{Type: "self", UserID: userID}
	periodRef := reportPeriodInputRef(reportTypePersonalDaily, reportDate, "", "")
	inputRef := map[string]any{
		"trigger_source":                 autodailyreport.TriggerSource,
		"report_type":                    reportTypePersonalDaily,
		"period":                         periodRef,
		"target":                         target,
		"model_id":                       modelID,
		"mcp_server":                     h.defaults.ReportMCPSlug,
		"report_mcp_version":             h.defaults.ReportMCPVersion,
		"digest_version":                 sessiondigestv2.Version,
		"redaction_version":              sessiondigestv2.RedactionVersion,
		"report_context_schema_version":  reportcontext.SchemaVersion,
		"credential_slot":                h.defaults.ReportMCPCredentialSlot,
		"report_skill_slug":              h.defaults.ReportSkillSlug,
		"report_skill_version":           h.defaults.ReportSkillVersion,
		"selected_session_slice_keys":    sourceSliceKeys,
		"auto_report_source_fingerprint": strings.TrimSpace(request.SourceFingerprint),
		"auto_report_guard":              request.Guard,
		"code_revision":                  strings.TrimSpace(h.defaults.BuildRevision),
	}
	promptMaterial := strings.TrimSpace(agent.Instructions) + "\n" + strings.TrimSpace(agent.StartPromptTemplate)
	if strings.TrimSpace(promptMaterial) != "" {
		inputRef["report_prompt_sha256"] = sha256Hex(promptMaterial)
	}
	if skillMarkdown := strings.TrimSpace(h.reportSkillMarkdown()); skillMarkdown != "" {
		inputRef["report_skill_sha256"] = sha256Hex(skillMarkdown)
	}

	frozenScope := map[string]any{
		"user_id": userID, "business_type": reportAgentRunBusinessType,
		"report_type": reportTypePersonalDaily, "target": target, "period": periodRef,
		"timezone": biztime.Zone, "agent_id": agent.AgentID,
		"agent_version_id": agent.CurrentVersionID, "model_id": modelID,
		"initial_message": "", "trigger_source": autodailyreport.TriggerSource,
		"source_fingerprint": strings.TrimSpace(request.SourceFingerprint),
		"source_slice_keys":  sourceSliceKeys, "write_guard": request.Guard,
		"digest_version":                sessiondigestv2.Version,
		"redaction_version":             sessiondigestv2.RedactionVersion,
		"report_context_schema_version": reportcontext.SchemaVersion,
		"report_context_representation": reportcontext.RepresentationWorkEvidence,
	}
	dedupeScope := reportRunDedupeScope(frozenScope, map[string]string{}, sortedStringKeys(reportSlots))
	sources := make([]reportsource.SourceInput, 0, len(sourceSliceKeys))
	for _, key := range sourceSliceKeys {
		sources = append(sources, reportsource.SourceInput{SliceKey: key})
	}
	var agentVersionID *int
	if agent.CurrentVersionID > 0 {
		value := agent.CurrentVersionID
		agentVersionID = &value
	}
	variantManifest, variantSHA256, err := buildSubmittedReportVariant(
		agent.AgentID, agentVersionID, modelID, inputRef,
		managedAgentSourceSystem, h.defaults.ReportTwoPassEnabled,
	)
	if err != nil {
		return reportsource.RunSubmissionRequest{}, err
	}
	return reportsource.RunSubmissionRequest{
		UserID: userID, ReportType: reportTypePersonalDaily,
		Period:  reportContextPeriod(reportTypePersonalDaily, reportDate, "", ""),
		Sources: sources, RequireSources: true,
		BusinessType: reportAgentRunBusinessType, AgentID: agent.AgentID,
		AgentVersionID: agentVersionID, ModelID: modelID,
		IdempotencyKey:          "auto-daily:" + reportDate + ":" + strings.TrimSpace(request.SourceFingerprint),
		RequestFingerprintInput: dedupeScope, ActiveDedupeInput: dedupeScope,
		InputRef: inputRef,
		ExecutionInput: map[string]any{
			"timezone": biztime.Zone, "initial_message": "",
			"credential_overrides":          map[string]string{},
			"report_mcp_slots":              sortedStringKeys(reportSlots),
			"report_context_representation": reportcontext.RepresentationWorkEvidence,
			"report_agent_source":           managedAgentSourceSystem,
			"system_report_account":         true,
		},
		VariantManifest: variantManifest, VariantSHA256: variantSHA256,
	}, nil
}
