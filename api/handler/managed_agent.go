package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aidashboard/api/internal/biztime"
	"github.com/aidashboard/api/internal/reportsource"
	"github.com/aidashboard/api/model"
	"github.com/aidashboard/api/service"
	"github.com/go-chi/chi/v5"
)

type ManagedAgentHandler struct {
	db           *sql.DB
	client       *service.ManagedAgentClient
	defaults     ManagedAgentDefaults
	reportSource *reportsource.Service
}

const (
	defaultReportAgentName        = "报告生成 Agent"
	defaultReportAgentDescription = "默认报告生成 Agent。"
	defaultReportAgentMarker      = "AIDA_REPORT_AGENT:default"
	defaultReportAgentTypesPrefix = "AIDA_REPORT_AGENT_TYPES:"
	defaultManagedAgentMarker     = "AIDA_MANAGED_DEFAULT_AGENT:true"
	defaultReportAssetsMarker     = "AIDA_REPORT_DEFAULT:true"
	reportMCPCredentialSlot       = "AIDA_REPORT_MCP_AUTH"
	managedAgentConfigInvalidCode = "MANAGED_AGENT_CONFIG_INVALID"
	reportMCPProtectedCode        = "REPORT_MCP_PROTECTED"
	reportSkillProtectedCode      = "REPORT_SKILL_PROTECTED"
	reportAgentRunBusinessType    = "report_agent_run"
	scheduledAgentRunBusinessType = "scheduled_agent_run"
	managedAgentBusinessGeneric   = "generic"
	managedAgentBusinessReport    = "report"
	scheduleRunKindGeneric        = "generic_agent"
	scheduleRunKindReport         = "report_agent"
	defaultScheduleTimezone       = "Asia/Shanghai"
	reservedPromptValueCode       = "RESERVED_PROMPT_VALUE"
)

var reportSystemPromptKeys = map[string]struct{}{
	"report_type":                      {},
	"period_json":                      {},
	"calendar_context_json":            {},
	"target_json":                      {},
	"selected_session_slice_keys":      {},
	"selected_session_slice_keys_json": {},
	"report_source_selection_id":       {},
	"period_start":                     {},
	"period_end":                       {},
	"scheduled_trigger_at":             {},
	"run_id":                           {},
	"mcp_url":                          {},
	"credential":                       {},
	"credential_slot":                  {},
	reportMCPCredentialSlot:            {},
}

type ManagedAgentDefaults struct {
	Engine                         string
	ModelID                        string
	ReportSkillOwner               string
	ReportSkillSlug                string
	ReportSkillVersion             string
	ReportSkillName                string
	ReportSkillDescription         string
	ReportSkillMarkdown            string
	ReportMCPSlug                  string
	ReportMCPVersion               string
	ReportMCPName                  string
	ReportMCPDescription           string
	ReportMCPURL                   string
	ReportMCPCredentialSlot        string
	ReportAgentName                string
	ReportAgentDescription         string
	ReportAgentInstructions        string
	ReportAgentStartPromptTemplate string
	ReportAssetRepair              bool
	ReportAssetRepairConfigured    bool
	AIDAPublicBaseURL              string
	AIHubSecret                    string
}

func NewManagedAgentHandler(db *sql.DB, client *service.ManagedAgentClient) *ManagedAgentHandler {
	return NewManagedAgentHandlerWithDefaults(db, client, ManagedAgentDefaults{})
}

func NewManagedAgentHandlerWithDefaults(db *sql.DB, client *service.ManagedAgentClient, defaults ManagedAgentDefaults) *ManagedAgentHandler {
	return &ManagedAgentHandler{db: db, client: client, defaults: normalizeManagedAgentDefaults(defaults)}
}

func (h *ManagedAgentHandler) ConfigureReportSourceSelection(service *reportsource.Service) {
	h.reportSource = service
}

func normalizeManagedAgentDefaults(defaults ManagedAgentDefaults) ManagedAgentDefaults {
	defaults.Engine = strings.TrimSpace(defaults.Engine)
	if defaults.Engine == "" {
		defaults.Engine = "claude-code"
	}
	defaults.ModelID = strings.TrimSpace(defaults.ModelID)
	if defaults.ModelID == "" {
		defaults.ModelID = "MiniMax-M2.5"
	}
	defaults.ReportSkillOwner = strings.TrimSpace(defaults.ReportSkillOwner)
	defaults.ReportSkillSlug = strings.TrimSpace(defaults.ReportSkillSlug)
	if defaults.ReportSkillSlug == "" {
		defaults.ReportSkillSlug = service.ReportSkillSlug
	}
	defaults.ReportSkillVersion = strings.TrimSpace(defaults.ReportSkillVersion)
	if defaults.ReportSkillVersion == "" {
		defaults.ReportSkillVersion = service.ReportSkillVersion
	}
	defaults.ReportSkillName = strings.TrimSpace(defaults.ReportSkillName)
	if defaults.ReportSkillName == "" {
		defaults.ReportSkillName = service.ReportSkillName
	}
	defaults.ReportSkillDescription = strings.TrimSpace(defaults.ReportSkillDescription)
	if defaults.ReportSkillDescription == "" {
		defaults.ReportSkillDescription = "Aida shared Report Skill.\n" + defaultReportAssetsMarker
	}
	defaults.ReportSkillMarkdown = strings.TrimSpace(defaults.ReportSkillMarkdown)
	defaults.ReportMCPSlug = strings.TrimSpace(defaults.ReportMCPSlug)
	if defaults.ReportMCPSlug == "" {
		defaults.ReportMCPSlug = service.ReportMCPSlug
	}
	defaults.ReportMCPVersion = strings.TrimSpace(defaults.ReportMCPVersion)
	if defaults.ReportMCPVersion == "" {
		defaults.ReportMCPVersion = service.ReportMCPVersion
	}
	defaults.ReportMCPName = strings.TrimSpace(defaults.ReportMCPName)
	if defaults.ReportMCPName == "" {
		defaults.ReportMCPName = "Aida Report MCP"
	}
	defaults.ReportMCPDescription = strings.TrimSpace(defaults.ReportMCPDescription)
	if defaults.ReportMCPDescription == "" {
		defaults.ReportMCPDescription = "Aida generic Report MCP endpoint.\n" + defaultReportAssetsMarker
	}
	defaults.ReportMCPURL = strings.TrimRight(strings.TrimSpace(defaults.ReportMCPURL), "/")
	defaults.ReportMCPCredentialSlot = strings.TrimSpace(defaults.ReportMCPCredentialSlot)
	if defaults.ReportMCPCredentialSlot == "" {
		defaults.ReportMCPCredentialSlot = reportMCPCredentialSlot
	}
	defaults.ReportAgentName = strings.TrimSpace(defaults.ReportAgentName)
	if defaults.ReportAgentName == "" {
		defaults.ReportAgentName = defaultReportAgentName
	}
	defaults.ReportAgentDescription = strings.TrimSpace(defaults.ReportAgentDescription)
	if defaults.ReportAgentDescription == "" {
		defaults.ReportAgentDescription = defaultReportAgentDescription
	}
	defaults.ReportAgentInstructions = strings.TrimSpace(defaults.ReportAgentInstructions)
	if defaults.ReportAgentInstructions == "" {
		defaults.ReportAgentInstructions = defaultReportAgentInstructions(defaults.ReportMCPCredentialSlot)
	}
	defaults.ReportAgentStartPromptTemplate = strings.TrimSpace(defaults.ReportAgentStartPromptTemplate)
	if defaults.ReportAgentStartPromptTemplate == "" {
		defaults.ReportAgentStartPromptTemplate = defaultReportAgentStartPromptTemplate(defaults.ReportMCPCredentialSlot)
	}
	if !defaults.ReportAssetRepairConfigured {
		defaults.ReportAssetRepair = true
	}
	defaults.AIDAPublicBaseURL = strings.TrimRight(strings.TrimSpace(defaults.AIDAPublicBaseURL), "/")
	defaults.AIHubSecret = strings.TrimSpace(defaults.AIHubSecret)
	if defaults.AIHubSecret == "" {
		defaults.AIHubSecret = "dev-jwt-secret"
	}
	return defaults
}

func (h *ManagedAgentHandler) clientForRequest(r *http.Request) *service.ManagedAgentClient {
	if h.client == nil {
		return nil
	}
	return h.client.WithToken(bearerTokenFromRequest(r))
}

func bearerTokenFromRequest(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token == authHeader {
		return ""
	}
	return strings.TrimSpace(token)
}

func (h *ManagedAgentHandler) clientForUser(user *model.User, token string) (*service.ManagedAgentClient, string, error) {
	if h == nil || h.client == nil {
		return nil, strings.TrimSpace(token), nil
	}
	resolvedToken := strings.TrimSpace(token)
	if resolvedToken == "" && user != nil {
		var err error
		resolvedToken, err = MintAIHubCompatibleToken(user, h.defaults.AIHubSecret)
		if err != nil {
			return nil, "", err
		}
	}
	if resolvedToken == "" {
		return h.client, "", nil
	}
	return h.client.WithToken(resolvedToken), resolvedToken, nil
}

func generateManagedAgentID(name string) string {
	base := strings.Builder{}
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			base.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && base.Len() > 0 {
			base.WriteByte('-')
			lastDash = true
		}
	}
	baseString := strings.Trim(base.String(), "-")
	if baseString == "" {
		baseString = "agent"
	}
	return "aida-" + baseString + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

func (h *ManagedAgentHandler) ensureConfigured(w http.ResponseWriter) bool {
	if h.client == nil || !h.client.Configured() {
		writeManagedAgentError(w, &service.ManagedAgentError{
			Code:    service.ManagedAgentNotConfiguredCode,
			Message: "managed agent platform is not configured",
		})
		return false
	}
	return true
}

func writeManagedAgentError(w http.ResponseWriter, err error) {
	var managedErr *service.ManagedAgentError
	if errors.As(err, &managedErr) {
		status := http.StatusBadGateway
		if managedErr.Code == service.ManagedAgentNotConfiguredCode || managedErr.Code == managedAgentConfigInvalidCode {
			status = http.StatusServiceUnavailable
		}
		writeJSON(w, status, map[string]any{
			"code":  managedErr.Code,
			"error": managedErr.Message,
		})
		return
	}
	writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
}

func writeManagedAgentConfigError(w http.ResponseWriter, message string) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{
		"code":  managedAgentConfigInvalidCode,
		"error": message,
	})
}

func managedAgentConfigError(message string) error {
	return &service.ManagedAgentError{Code: managedAgentConfigInvalidCode, Message: message}
}

func writeReportSkillProtected(w http.ResponseWriter) {
	writeJSON(w, http.StatusConflict, map[string]any{
		"code":  reportSkillProtectedCode,
		"error": "Aida Report Skill 是系统内置资源，不可修改、删除或归档",
	})
}

func writeReportMCPProtected(w http.ResponseWriter) {
	writeJSON(w, http.StatusConflict, map[string]any{
		"code":  reportMCPProtectedCode,
		"error": "Aida Report MCP 是系统内置资源，不可修改、删除或归档",
	})
}

func (h *ManagedAgentHandler) isReportSkillRef(slug, version string) bool {
	return strings.TrimSpace(slug) == h.defaults.ReportSkillSlug && strings.TrimSpace(version) == h.defaults.ReportSkillVersion
}

func (h *ManagedAgentHandler) isReportMCPRef(slug, version string) bool {
	return strings.TrimSpace(slug) == h.defaults.ReportMCPSlug && strings.TrimSpace(version) == h.defaults.ReportMCPVersion
}

func (h *ManagedAgentHandler) filterReportSystemSkills(skills []model.ManagedSkill) []model.ManagedSkill {
	filtered := make([]model.ManagedSkill, 0, len(skills))
	for _, skill := range skills {
		if strings.TrimSpace(skill.Slug) == h.defaults.ReportSkillSlug && (h.isReportSkillRef(skill.Slug, skill.Version) || strings.Contains(skill.Description, defaultReportAssetsMarker)) {
			continue
		}
		filtered = append(filtered, skill)
	}
	return filtered
}

func (h *ManagedAgentHandler) filterReportSystemMCPEntries(entries []model.ManagedMCPEntry) []model.ManagedMCPEntry {
	filtered := make([]model.ManagedMCPEntry, 0, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry.Slug) == h.defaults.ReportMCPSlug && (h.isReportMCPRef(entry.Slug, entry.Version) || strings.Contains(entry.Description, defaultReportAssetsMarker)) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func includeSystemManagedAssets(r *http.Request) bool {
	value := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("include_system")))
	return value == "true" || value == "1" || value == "yes"
}

func includeArchivedManagedAssets(r *http.Request) bool {
	value := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("include_archived")))
	return value == "true" || value == "1" || value == "yes"
}

func filterArchivedManagedSkills(skills []model.ManagedSkill) []model.ManagedSkill {
	filtered := make([]model.ManagedSkill, 0, len(skills))
	for _, skill := range skills {
		if skill.Archived {
			continue
		}
		filtered = append(filtered, skill)
	}
	return filtered
}

func filterArchivedManagedMCPEntries(entries []model.ManagedMCPEntry) []model.ManagedMCPEntry {
	filtered := make([]model.ManagedMCPEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Archived {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func filterArchivedManagedAgents(agents []model.ManagedAgent) []model.ManagedAgent {
	filtered := make([]model.ManagedAgent, 0, len(agents))
	for _, agent := range agents {
		if agent.Archived {
			continue
		}
		filtered = append(filtered, agent)
	}
	return filtered
}

// proxyJSON runs the ensureConfigured + call + standard error/writeJSON sequence
// shared by every pass-through managed-agent endpoint.
func (h *ManagedAgentHandler) proxyJSON(w http.ResponseWriter, call func() (any, error)) {
	if !h.ensureConfigured(w) {
		return
	}
	resp, err := call()
	if err != nil {
		writeManagedAgentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *ManagedAgentHandler) ListSkills(w http.ResponseWriter, r *http.Request) {
	scope := r.URL.Query().Get("scope")
	includeSystem := includeSystemManagedAssets(r)
	includeArchived := includeArchivedManagedAssets(r)
	client := h.clientForRequest(r)
	h.proxyJSON(w, func() (any, error) {
		resp, err := client.ListSkills(r.Context(), scope)
		if err != nil {
			return nil, err
		}
		if includeSystem {
			systemSkill, err := h.resolveSystemReportSkill(r.Context(), client)
			if err != nil {
				return nil, err
			}
			resp.Skills = h.filterReportSystemSkills(resp.Skills)
			resp.Skills = append([]model.ManagedSkill{systemSkill}, resp.Skills...)
		} else {
			resp.Skills = h.filterReportSystemSkills(resp.Skills)
		}
		if !includeArchived {
			resp.Skills = filterArchivedManagedSkills(resp.Skills)
		}
		return resp, nil
	})
}

func (h *ManagedAgentHandler) CreateSkill(w http.ResponseWriter, r *http.Request) {
	var req service.CreateManagedSkillRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if strings.TrimSpace(req.Slug) == "" || strings.TrimSpace(req.Version) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "slug and version are required"})
		return
	}
	if h.isReportSkillRef(req.Slug, req.Version) {
		writeReportSkillProtected(w)
		return
	}
	if strings.TrimSpace(req.SkillMD) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "skill_md is required"})
		return
	}
	client := h.clientForRequest(r)
	h.proxyJSON(w, func() (any, error) { return client.CreateSkill(r.Context(), req) })
}

func (h *ManagedAgentHandler) ArchiveSkill(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	version := chi.URLParam(r, "version")
	var req service.ArchiveManagedSkillRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if strings.TrimSpace(slug) == "" || strings.TrimSpace(version) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "slug and version are required"})
		return
	}
	if h.isReportSkillRef(slug, version) {
		writeReportSkillProtected(w)
		return
	}
	client := h.clientForRequest(r)
	h.proxyJSON(w, func() (any, error) { return client.ArchiveSkill(r.Context(), slug, version, req.Archived) })
}

func (h *ManagedAgentHandler) DeleteSkill(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	version := chi.URLParam(r, "version")
	if strings.TrimSpace(slug) == "" || strings.TrimSpace(version) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "slug and version are required"})
		return
	}
	if h.isReportSkillRef(slug, version) {
		writeReportSkillProtected(w)
		return
	}
	client := h.clientForRequest(r)
	h.proxyJSON(w, func() (any, error) { return client.DeleteSkill(r.Context(), slug, version) })
}

func (h *ManagedAgentHandler) GetSkillMarkdown(w http.ResponseWriter, r *http.Request) {
	owner := strings.TrimSpace(chi.URLParam(r, "owner"))
	slug := chi.URLParam(r, "slug")
	version := chi.URLParam(r, "version")
	if owner == "_mine" {
		owner = currentManagedOwner(getUser(r))
	}
	if strings.TrimSpace(owner) == "" || strings.TrimSpace(slug) == "" || strings.TrimSpace(version) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "owner, slug and version are required"})
		return
	}
	client := h.clientForRequest(r)
	h.proxyJSON(w, func() (any, error) {
		content, err := client.GetSkillFile(r.Context(), owner, slug, version, "SKILL.md")
		if err != nil {
			return nil, err
		}
		return map[string]string{"content": string(content)}, nil
	})
}

func (h *ManagedAgentHandler) ListMCPEntries(w http.ResponseWriter, r *http.Request) {
	scope := r.URL.Query().Get("scope")
	includeSystem := includeSystemManagedAssets(r)
	includeArchived := includeArchivedManagedAssets(r)
	client := h.clientForRequest(r)
	h.proxyJSON(w, func() (any, error) {
		resp, err := client.ListMCPEntries(r.Context(), scope)
		if err != nil {
			return nil, err
		}
		if !includeSystem {
			resp.Entries = h.filterReportSystemMCPEntries(resp.Entries)
		}
		if !includeArchived {
			resp.Entries = filterArchivedManagedMCPEntries(resp.Entries)
		}
		return resp, nil
	})
}

func (h *ManagedAgentHandler) CreateMCPEntry(w http.ResponseWriter, r *http.Request) {
	var req model.CreateManagedMCPEntryRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if strings.TrimSpace(req.Slug) == "" || strings.TrimSpace(req.Version) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "slug and version are required"})
		return
	}
	if h.isReportMCPRef(req.Slug, req.Version) {
		writeReportMCPProtected(w)
		return
	}
	client := h.clientForRequest(r)
	h.proxyJSON(w, func() (any, error) { return client.CreateMCPEntry(r.Context(), req) })
}

func (h *ManagedAgentHandler) ArchiveMCPEntry(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	version := chi.URLParam(r, "version")
	var req service.ArchiveManagedMCPEntryRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if strings.TrimSpace(slug) == "" || strings.TrimSpace(version) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "slug and version are required"})
		return
	}
	if h.isReportMCPRef(slug, version) {
		writeReportMCPProtected(w)
		return
	}
	client := h.clientForRequest(r)
	h.proxyJSON(w, func() (any, error) { return client.ArchiveMCPEntry(r.Context(), slug, version, req.Archived) })
}

func (h *ManagedAgentHandler) DeleteMCPEntry(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	version := chi.URLParam(r, "version")
	if strings.TrimSpace(slug) == "" || strings.TrimSpace(version) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "slug and version are required"})
		return
	}
	if h.isReportMCPRef(slug, version) {
		writeReportMCPProtected(w)
		return
	}
	client := h.clientForRequest(r)
	h.proxyJSON(w, func() (any, error) { return client.DeleteMCPEntry(r.Context(), slug, version) })
}

func (h *ManagedAgentHandler) ListCredentials(w http.ResponseWriter, r *http.Request) {
	h.proxyJSON(w, func() (any, error) {
		return h.clientForRequest(r).ListCredentials(r.Context())
	})
}

func (h *ManagedAgentHandler) CreateCredential(w http.ResponseWriter, r *http.Request) {
	var req service.CreateManagedCredentialRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	h.proxyJSON(w, func() (any, error) {
		return h.clientForRequest(r).CreateCredential(r.Context(), req)
	})
}

func (h *ManagedAgentHandler) DeleteCredential(w http.ResponseWriter, r *http.Request) {
	credentialID := chi.URLParam(r, "credentialId")
	if strings.TrimSpace(credentialID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "credential_id is required"})
		return
	}
	h.proxyJSON(w, func() (any, error) {
		return h.clientForRequest(r).DeleteCredential(r.Context(), credentialID)
	})
}

type managedAgentProfile struct {
	AgentID         string
	BusinessType    string
	ReportTypes     []string
	IsDefaultReport bool
}

func normalizeManagedAgentBusinessType(value string) string {
	switch strings.TrimSpace(value) {
	case managedAgentBusinessReport:
		return managedAgentBusinessReport
	default:
		return managedAgentBusinessGeneric
	}
}

func normalizeManagedAgentReportTypes(types []string) []string {
	normalized := []string{}
	for _, item := range types {
		item = strings.TrimSpace(item)
		if validateReportType(item) == nil && !containsString(normalized, item) {
			normalized = append(normalized, item)
		}
	}
	if len(normalized) == 0 {
		return append([]string{}, supportedReportTypes...)
	}
	return normalized
}

func platformManagedAgentRequest(req model.UpsertManagedAgentRequest) model.UpsertManagedAgentRequest {
	req.BusinessType = ""
	req.ReportTypes = nil
	return req
}

func (h *ManagedAgentHandler) mergeManagedAgentProfiles(ctx context.Context, userID string, agents []model.ManagedAgent) []model.ManagedAgent {
	if h.db == nil || strings.TrimSpace(userID) == "" || len(agents) == 0 {
		return agents
	}
	agentIDs := []string{}
	for _, agent := range agents {
		if strings.TrimSpace(agent.AgentID) != "" {
			agentIDs = append(agentIDs, agent.AgentID)
		}
	}
	profiles, err := h.loadManagedAgentProfiles(ctx, userID, agentIDs)
	if err != nil {
		return agents
	}
	for idx := range agents {
		profile, ok := profiles[agents[idx].AgentID]
		if !ok {
			continue
		}
		agents[idx].BusinessType = profile.BusinessType
		agents[idx].ReportTypes = append([]string{}, profile.ReportTypes...)
		agents[idx].IsDefaultReport = profile.IsDefaultReport
	}
	return agents
}

func (h *ManagedAgentHandler) filterOtherDeploymentDefaultReportAgents(agents []model.ManagedAgent) []model.ManagedAgent {
	if len(agents) == 0 {
		return agents
	}
	filtered := make([]model.ManagedAgent, 0, len(agents))
	for _, agent := range agents {
		if isMarkedDefaultReportAgent(agent) && !h.isCurrentDeploymentDefaultReportAgent(agent) {
			continue
		}
		filtered = append(filtered, agent)
	}
	return filtered
}

func (h *ManagedAgentHandler) loadManagedAgentProfiles(ctx context.Context, userID string, agentIDs []string) (map[string]managedAgentProfile, error) {
	out := map[string]managedAgentProfile{}
	if h.db == nil || strings.TrimSpace(userID) == "" || len(agentIDs) == 0 {
		return out, nil
	}
	placeholders := make([]string, 0, len(agentIDs))
	args := []any{userID}
	for _, agentID := range agentIDs {
		if strings.TrimSpace(agentID) == "" {
			continue
		}
		args = append(args, agentID)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
	}
	if len(placeholders) == 0 {
		return out, nil
	}
	rows, err := h.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT agent_id, business_type, report_types, COALESCE(is_default_report, false)
		FROM managed_agent_profiles
		WHERE user_id = $1 AND agent_id IN (%s)
	`, strings.Join(placeholders, ",")), args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var profile managedAgentProfile
		var raw []byte
		if err := rows.Scan(&profile.AgentID, &profile.BusinessType, &raw, &profile.IsDefaultReport); err != nil {
			return out, err
		}
		_ = json.Unmarshal(raw, &profile.ReportTypes)
		profile.BusinessType = normalizeManagedAgentBusinessType(profile.BusinessType)
		if profile.BusinessType == managedAgentBusinessReport {
			profile.ReportTypes = normalizeManagedAgentReportTypes(profile.ReportTypes)
		}
		out[profile.AgentID] = profile
	}
	return out, rows.Err()
}

func (h *ManagedAgentHandler) upsertManagedAgentProfile(ctx context.Context, userID, agentID, businessType string, reportTypes []string) error {
	if h.db == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(agentID) == "" {
		return nil
	}
	businessType = normalizeManagedAgentBusinessType(businessType)
	if businessType == managedAgentBusinessReport {
		reportTypes = normalizeManagedAgentReportTypes(reportTypes)
	} else {
		reportTypes = []string{}
	}
	raw, err := json.Marshal(reportTypes)
	if err != nil {
		return err
	}
	_, err = h.db.ExecContext(ctx, `
		INSERT INTO managed_agent_profiles (agent_id, user_id, business_type, report_types, updated_at)
		VALUES ($1, $2, $3, $4::jsonb, now())
		ON CONFLICT (agent_id, user_id) DO UPDATE SET
			business_type = EXCLUDED.business_type,
			report_types = EXCLUDED.report_types,
			updated_at = now()
	`, agentID, userID, businessType, string(raw))
	return err
}

func (h *ManagedAgentHandler) setDefaultReportAgentProfile(ctx context.Context, userID, agentID string, reportTypes []string) error {
	if h.db == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(agentID) == "" {
		return nil
	}
	reportTypes = normalizeManagedAgentReportTypes(reportTypes)
	raw, err := json.Marshal(reportTypes)
	if err != nil {
		return err
	}
	if _, err := h.db.ExecContext(ctx, `
		UPDATE managed_agent_profiles
		SET is_default_report = false, updated_at = now()
		WHERE user_id = $1 AND is_default_report = true AND agent_id <> $2
	`, userID, agentID); err != nil {
		return err
	}
	_, err = h.db.ExecContext(ctx, `
		INSERT INTO managed_agent_profiles (agent_id, user_id, business_type, report_types, is_default_report, updated_at)
		VALUES ($1, $2, $3, $4::jsonb, true, now())
		ON CONFLICT (agent_id, user_id) DO UPDATE SET
			business_type = EXCLUDED.business_type,
			report_types = EXCLUDED.report_types,
			is_default_report = true,
			updated_at = now()
	`, agentID, userID, managedAgentBusinessReport, string(raw))
	return err
}

func (h *ManagedAgentHandler) loadManagedAgentProfile(ctx context.Context, userID, agentID string) (*managedAgentProfile, error) {
	if h.db == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(agentID) == "" {
		return nil, nil
	}
	var profile managedAgentProfile
	var raw []byte
	err := h.db.QueryRowContext(ctx, `
		SELECT agent_id, business_type, report_types, COALESCE(is_default_report, false)
		FROM managed_agent_profiles
		WHERE user_id = $1 AND agent_id = $2
	`, userID, agentID).Scan(&profile.AgentID, &profile.BusinessType, &raw, &profile.IsDefaultReport)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(raw, &profile.ReportTypes)
	profile.BusinessType = normalizeManagedAgentBusinessType(profile.BusinessType)
	if profile.BusinessType == managedAgentBusinessReport {
		profile.ReportTypes = normalizeManagedAgentReportTypes(profile.ReportTypes)
	}
	return &profile, nil
}

func (h *ManagedAgentHandler) ListMyAgents(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	includeArchived := includeArchivedManagedAssets(r)
	client := h.clientForRequest(r)
	h.proxyJSON(w, func() (any, error) {
		resp, err := client.ListMyAgents(r.Context())
		if err != nil {
			return nil, err
		}
		if resp.Agents == nil {
			resp.Agents = []model.ManagedAgent{}
		}
		if u != nil {
			resp.Agents = h.mergeManagedAgentProfiles(r.Context(), u.ID, resp.Agents)
		}
		resp.Agents = h.filterOtherDeploymentDefaultReportAgents(resp.Agents)
		if !includeArchived {
			resp.Agents = filterArchivedManagedAgents(resp.Agents)
		}
		return resp, nil
	})
}

func (h *ManagedAgentHandler) CreateMyAgent(w http.ResponseWriter, r *http.Request) {
	var req model.UpsertManagedAgentRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Engine) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and engine are required"})
		return
	}
	if strings.TrimSpace(req.AgentID) == "" {
		req.AgentID = generateManagedAgentID(req.Name)
	}
	u := getUser(r)
	client := h.clientForRequest(r)
	h.proxyJSON(w, func() (any, error) {
		businessType := normalizeManagedAgentBusinessType(req.BusinessType)
		resp, err := client.CreateMyAgent(r.Context(), platformManagedAgentRequest(req))
		if err != nil {
			return nil, err
		}
		if u != nil {
			if err := h.upsertManagedAgentProfile(r.Context(), u.ID, req.AgentID, businessType, req.ReportTypes); err != nil {
				return nil, err
			}
		}
		return resp, nil
	})
}

func (h *ManagedAgentHandler) UpdateMyAgent(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agentId")
	var req model.UpsertManagedAgentRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	u := getUser(r)
	client := h.clientForRequest(r)
	h.proxyJSON(w, func() (any, error) {
		businessType := normalizeManagedAgentBusinessType(req.BusinessType)
		if u != nil {
			profile, err := h.loadManagedAgentProfile(r.Context(), u.ID, agentID)
			if err != nil {
				return nil, err
			}
			if profile != nil && profile.BusinessType == managedAgentBusinessReport {
				businessType = managedAgentBusinessReport
				req.BusinessType = managedAgentBusinessReport
				req.ReportTypes = profile.ReportTypes
			}
		}
		resp, err := client.UpdateMyAgent(r.Context(), agentID, platformManagedAgentRequest(req))
		if err != nil {
			return nil, err
		}
		if u != nil {
			if err := h.upsertManagedAgentProfile(r.Context(), u.ID, agentID, businessType, req.ReportTypes); err != nil {
				return nil, err
			}
		}
		return resp, nil
	})
}

func (h *ManagedAgentHandler) ArchiveMyAgent(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agentId")
	var req service.ArchiveManagedAgentRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if strings.TrimSpace(agentID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "agent_id is required"})
		return
	}
	client := h.clientForRequest(r)
	h.proxyJSON(w, func() (any, error) { return client.ArchiveMyAgent(r.Context(), agentID, req.Archived) })
}

func (h *ManagedAgentHandler) ensureReportMCPEntry(r *http.Request, client *service.ManagedAgentClient) error {
	_, _, _, err := h.ensureUserReportMCPEntry(r.Context(), client)
	return err
}

func (h *ManagedAgentHandler) ensureUserReportMCPEntry(ctx context.Context, client *service.ManagedAgentClient) (bool, model.ManagedMCPEntry, int, error) {
	expectedURL := h.reportMCPURL()
	resp, err := client.ListMCPEntries(ctx, string(model.ManagedScopeMine))
	if err != nil {
		return false, model.ManagedMCPEntry{}, 0, err
	}
	count := 0
	var first model.ManagedMCPEntry
	for _, entry := range resp.Entries {
		if entry.Slug != h.defaults.ReportMCPSlug || entry.Version != h.defaults.ReportMCPVersion {
			continue
		}
		if entry.Archived {
			continue
		}
		if strings.TrimSpace(expectedURL) != "" && strings.TrimSpace(entry.URL) != "" && strings.TrimSpace(entry.URL) != expectedURL {
			return false, model.ManagedMCPEntry{}, count, fmt.Errorf("Aida Report MCP %s@%s points to %s, expected %s; publish a new MCP asset version in code or fix the platform registry", h.defaults.ReportMCPSlug, h.defaults.ReportMCPVersion, entry.URL, expectedURL)
		}
		count++
		if first.EntryID == "" {
			first = entry
		}
	}
	if count > 0 {
		return false, first, count, nil
	}
	created, err := client.CreateMCPEntry(ctx, model.CreateManagedMCPEntryRequest{
		Slug:               h.defaults.ReportMCPSlug,
		Version:            h.defaults.ReportMCPVersion,
		Name:               h.defaults.ReportMCPName,
		Description:        h.defaults.ReportMCPDescription,
		Transport:          "http",
		URL:                expectedURL,
		AuthHeader:         "Authorization",
		AuthScheme:         "Bearer",
		RequiresCredential: true,
		CredentialEnv:      h.defaults.ReportMCPCredentialSlot,
	})
	if err != nil {
		return false, model.ManagedMCPEntry{}, count, err
	}
	return true, *created, count + 1, nil
}

func (h *ManagedAgentHandler) resolveSystemReportSkill(ctx context.Context, client *service.ManagedAgentClient) (model.ManagedSkill, error) {
	expectedOwner := strings.TrimSpace(h.defaults.ReportSkillOwner)
	if expectedOwner == "" {
		return model.ManagedSkill{}, managedAgentConfigError("MANAGED_AGENT_REPORT_SKILL_OWNER is required for Report Agent")
	}
	if client == nil || !client.Configured() {
		return model.ManagedSkill{}, managedAgentConfigError("managed agent platform is not configured")
	}
	resp, err := client.ListSkills(ctx, string(model.ManagedScopePublic))
	if err != nil {
		return model.ManagedSkill{}, err
	}
	matches := make([]model.ManagedSkill, 0, 1)
	ownerlessMatch := false
	for _, skill := range resp.Skills {
		if skill.Archived || !h.isReportSkillRef(skill.Slug, skill.Version) {
			continue
		}
		owner := strings.TrimSpace(skill.Owner)
		if owner == "" {
			ownerlessMatch = true
			continue
		}
		if owner == expectedOwner {
			matches = append(matches, skill)
		}
	}
	identity := expectedOwner + "/" + h.defaults.ReportSkillSlug + "@" + h.defaults.ReportSkillVersion
	if len(matches) == 0 {
		if ownerlessMatch {
			return model.ManagedSkill{}, managedAgentConfigError("managed agent platform returned an owner-less Report Skill; refusing owner fallback for " + identity)
		}
		return model.ManagedSkill{}, managedAgentConfigError("system Report Skill " + identity + " was not found in the public registry")
	}
	if len(matches) != 1 {
		return model.ManagedSkill{}, managedAgentConfigError("system Report Skill " + identity + " is not unique in the public registry")
	}
	resolved := matches[0]
	resolved.Owner = strings.TrimSpace(resolved.Owner)
	resolved.Slug = strings.TrimSpace(resolved.Slug)
	resolved.Version = strings.TrimSpace(resolved.Version)
	if resolved.SkillID == "" {
		return model.ManagedSkill{}, managedAgentConfigError("system Report Skill " + identity + " has no skill_id")
	}
	return resolved, nil
}

func managedSkillRef(skill model.ManagedSkill) model.ManagedSkillRef {
	return model.ManagedSkillRef{
		Owner:   strings.TrimSpace(skill.Owner),
		Slug:    strings.TrimSpace(skill.Slug),
		Version: strings.TrimSpace(skill.Version),
	}
}

func (h *ManagedAgentHandler) CreateDefaultReportAgent(w http.ResponseWriter, r *http.Request) {
	if !h.ensureConfigured(w) {
		return
	}
	if h.defaults.AIDAPublicBaseURL == "" {
		writeManagedAgentConfigError(w, "AIDA_PUBLIC_BASE_URL is required for Report Agent")
		return
	}
	u := getUser(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	client := h.clientForRequest(r)
	systemSkill, err := h.resolveSystemReportSkill(r.Context(), client)
	if err != nil {
		writeManagedAgentError(w, err)
		return
	}
	systemSkillRef := managedSkillRef(systemSkill)

	agentsResp, err := client.ListMyAgents(r.Context())
	if err != nil {
		writeManagedAgentError(w, err)
		return
	}
	existing, found, err := h.selectReportAgentForUser(r.Context(), u.ID, agentsResp.Agents)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if found {
		if h.defaults.ReportAssetRepair {
			var patch model.UpsertManagedAgentRequest
			var needsRepair bool
			if isMarkedDefaultReportAgent(existing) {
				patch, needsRepair = h.repairedDefaultReportAgentRequest(existing, systemSkillRef)
			} else {
				patch, needsRepair = h.repairedReportAgentDependencyRequest(existing, systemSkillRef)
			}
			if needsRepair {
				if _, err := client.UpdateMyAgent(r.Context(), existing.AgentID, platformManagedAgentRequest(patch)); err != nil {
					writeManagedAgentError(w, err)
					return
				}
				existing = managedAgentFromUpsertRequest(patch)
			}
		}
		if !hasExactSkillRef(existing.Skills, systemSkillRef) {
			writeManagedAgentError(w, managedAgentConfigError("Report Agent does not reference the configured system Report Skill"))
			return
		}
		if err := h.setDefaultReportAgentProfile(r.Context(), u.ID, existing.AgentID, supportedReportTypes); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		existing.BusinessType = managedAgentBusinessReport
		existing.ReportTypes = append([]string{}, supportedReportTypes...)
		existing.IsDefaultReport = true
		writeJSON(w, http.StatusOK, existing)
		return
	}

	req := h.defaultReportAgentRequest(systemSkillRef)
	req.AgentID = generateManagedAgentID(h.defaults.ReportAgentName)
	created, err := client.CreateMyAgent(r.Context(), req)
	if err != nil {
		writeManagedAgentError(w, err)
		return
	}
	if err := h.setDefaultReportAgentProfile(r.Context(), u.ID, created.AgentID, supportedReportTypes); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	agent := managedAgentFromUpsertRequest(req)
	agent.AgentID = created.AgentID
	agent.ManagedVersion = created.ManagedVersion
	agent.BusinessType = managedAgentBusinessReport
	agent.ReportTypes = append([]string{}, supportedReportTypes...)
	agent.IsDefaultReport = true
	writeJSON(w, http.StatusOK, agent)
}

func (h *ManagedAgentHandler) SetDefaultReportAgent(w http.ResponseWriter, r *http.Request) {
	if !h.ensureConfigured(w) {
		return
	}
	u := getUser(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	agentID := strings.TrimSpace(chi.URLParam(r, "agentId"))
	if agentID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "agent_id is required"})
		return
	}
	client := h.clientForRequest(r)
	agent, err := findMyManagedAgent(r, client, agentID)
	if err != nil {
		writeManagedAgentError(w, err)
		return
	}
	if agent == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent not found"})
		return
	}
	profile, err := h.loadManagedAgentProfile(r.Context(), u.ID, agentID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	supported := []string{}
	if profile != nil && profile.BusinessType == managedAgentBusinessReport {
		supported = normalizeManagedAgentReportTypes(profile.ReportTypes)
	} else {
		supported = reportTypesForAgent(*agent)
	}
	if len(supported) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "NOT_REPORT_AGENT", "error": "agent is not a Report Agent"})
		return
	}
	if err := h.setDefaultReportAgentProfile(r.Context(), u.ID, agentID, supported); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	agent.BusinessType = managedAgentBusinessReport
	agent.ReportTypes = append([]string{}, supported...)
	agent.IsDefaultReport = true
	writeJSON(w, http.StatusOK, agent)
}

func managedAgentFromUpsertRequest(req model.UpsertManagedAgentRequest) model.ManagedAgent {
	return model.ManagedAgent{
		AgentID:             req.AgentID,
		Name:                req.Name,
		Description:         req.Description,
		Engine:              req.Engine,
		Instructions:        req.Instructions,
		DefaultModelID:      req.DefaultModelID,
		StartPromptTemplate: req.StartPromptTemplate,
		CredentialSlots:     req.CredentialSlots,
		DefaultBindings:     req.DefaultBindings,
		Skills:              req.Skills,
		MCPServers:          req.MCPServers,
		MCPBindings:         req.MCPBindings,
	}
}

func (h *ManagedAgentHandler) reportMCPURL() string {
	if h.defaults.ReportMCPURL != "" {
		return h.defaults.ReportMCPURL
	}
	return h.defaults.AIDAPublicBaseURL + "/api/v1/mcp/reports"
}

func (h *ManagedAgentHandler) reportTemplateVars() map[string]string {
	return map[string]string{
		"mcp_url":                h.reportMCPURL(),
		"mcp_slug":               h.defaults.ReportMCPSlug,
		"mcp_version":            h.defaults.ReportMCPVersion,
		"credential_slot":        h.defaults.ReportMCPCredentialSlot,
		"supported_report_types": strings.Join(supportedReportTypes, ","),
		"skill_slug":             h.defaults.ReportSkillSlug,
		"skill_version":          h.defaults.ReportSkillVersion,
		"skill_name":             h.defaults.ReportSkillName,
		"agent_name":             h.defaults.ReportAgentName,
		"aida_deployment":        h.reportDeploymentValue(),
	}
}

func renderReportAssetTemplate(template string, values map[string]string) string {
	replacements := make([]string, 0, len(values)*2)
	for key, value := range values {
		replacements = append(replacements, "{{"+key+"}}", value)
	}
	return strings.NewReplacer(replacements...).Replace(template)
}

func (h *ManagedAgentHandler) reportSkillMarkdown() string {
	if h.defaults.ReportSkillMarkdown != "" {
		return renderReportAssetTemplate(h.defaults.ReportSkillMarkdown, h.reportTemplateVars())
	}
	return service.ReportSkillMarkdownWithConfig(service.ReportSkillTemplateData{
		MCPURL:               h.reportMCPURL(),
		MCPSlug:              h.defaults.ReportMCPSlug,
		MCPVersion:           h.defaults.ReportMCPVersion,
		CredentialSlot:       h.defaults.ReportMCPCredentialSlot,
		SupportedReportTypes: supportedReportTypes,
	})
}

func (h *ManagedAgentHandler) reportAgentInstructions() string {
	return renderReportAssetTemplate(h.defaults.ReportAgentInstructions, h.reportTemplateVars())
}

func (h *ManagedAgentHandler) reportAgentStartPromptTemplate() string {
	return renderReportAssetTemplate(h.defaults.ReportAgentStartPromptTemplate, h.reportTemplateVars())
}

func currentManagedOwner(user *model.User) string {
	if user == nil {
		return ""
	}
	owner := strings.TrimSpace(user.Username)
	if owner == "" {
		owner = strings.TrimSpace(user.ID)
	}
	return owner
}

func reportSkillRefOwner(skill model.ManagedSkill, fallbackOwner string) string {
	owner := strings.TrimSpace(skill.Owner)
	if owner != "" {
		return owner
	}
	if strings.TrimSpace(skill.SkillID) != "" {
		return ""
	}
	return strings.TrimSpace(fallbackOwner)
}

func (h *ManagedAgentHandler) defaultReportMCPBinding(owner string) model.ManagedMCPBinding {
	return model.ManagedMCPBinding{
		Owner:          owner,
		Slug:           h.defaults.ReportMCPSlug,
		Version:        h.defaults.ReportMCPVersion,
		CredentialSlot: h.defaults.ReportMCPCredentialSlot,
	}
}

func (h *ManagedAgentHandler) defaultReportMCPServer() model.ManagedMCPServer {
	return model.ManagedMCPServer{
		Name:           h.defaults.ReportMCPSlug,
		URL:            h.reportMCPURL(),
		CredentialSlot: h.defaults.ReportMCPCredentialSlot,
		AuthHeader:     "Authorization",
		AuthScheme:     "Bearer",
	}
}

func (h *ManagedAgentHandler) defaultReportAgentRequest(reportSkill model.ManagedSkillRef) model.UpsertManagedAgentRequest {
	return model.UpsertManagedAgentRequest{
		Name:                h.defaults.ReportAgentName,
		Description:         h.defaults.ReportAgentDescription,
		Engine:              h.defaults.Engine,
		DefaultModelID:      h.defaults.ModelID,
		Instructions:        h.reportAgentInstructions(),
		StartPromptTemplate: h.reportAgentStartPromptTemplate(),
		CredentialSlots: []model.ManagedCredentialSlot{{
			Name:     h.defaults.ReportMCPCredentialSlot,
			Required: true,
		}},
		Skills:      []model.ManagedSkillRef{reportSkill},
		MCPServers:  []model.ManagedMCPServer{h.defaultReportMCPServer()},
		MCPBindings: []model.ManagedMCPBinding{},
	}
}

func defaultReportAgentInstructions(credentialSlot string) string {
	return strings.Join([]string{
		defaultReportAssetsMarker,
		defaultReportAgentMarker,
		defaultReportAgentTypesPrefix + strings.Join(supportedReportTypes, ","),
		defaultManagedAgentMarker,
		"AIDA_REPORT_DEPLOYMENT:{{aida_deployment}}",
		"你是 Aida 报告执行 Agent。报告内容、结构和写作风格由当前绑定的 Skill 决定；本 Prompt 只定义 Aida 运行参数和 MCP 读写协议。",
		"运行参数由 Aida 后端注入，包含 run_id、report_type、period、calendar_context、target，个人报告还可能包含 report_source_selection_id。不要要求用户提供 session IDs、URLs、token 或 credential。",
		"Aida Report MCP 已通过 " + credentialSlot + " 凭据槽注入当前用户 Authorization。使用当前用户身份调用已绑定的 MCP tools，不要手工拼接管理员 token。",
		"个人报告存在 report_source_selection_id 时，必须携带相同的 run_id、report_type、period 和 report_source_selection_id 调用 get_sessions；该 ID 只能放入 report_source_selection_id，不得放入 selected_session_slice_keys，也不得同时传 date_range。digest_v1 和 digest_v2 必须确认 coverage.complete=true 且 has_more=false；digest_v2 还必须确认各日 outcome_coverage.complete=true。仅 legacy full 按 next_cursor 读取到 has_more=false。",
		"固定工具范围：个人报告使用 self；小组报告读取个人报告时使用 team + report_scope=personal；部门报告读取小组报告时使用 department + report_scope=team。不要越权扩大或缩小目标范围。",
		"依据当前绑定 Skill 生成报告后，必须调用 write_report_result，传入相同的 run_id、report_type、period、target 和 content；不要用最终对话回复代替 MCP 回写。生成失败时调用 write_report_failure。",
	}, "\n")
}

func defaultReportAgentStartPromptTemplate(credentialSlot string) string {
	return strings.Join([]string{
		"请根据以下业务参数生成 Aida 报告。",
		"report_type={{ report_type }}",
		"period={{ period_json }}",
		"calendar_context={{ calendar_context_json }}",
		"target={{ target_json }}",
		"report_source_selection_id={{ report_source_selection_id }}",
		"当 report_source_selection_id 非空时，使用该快照和 run_id 完整调用 get_sessions。快照 ID 只能传 report_source_selection_id，不得传 selected_session_slice_keys 或 date_range。digest_v1 和 digest_v2 确认完整单页 coverage；digest_v2 同时确认各日 outcome_coverage；仅 legacy full 使用 next_cursor 读取全部页面。",
		"报告内容与格式遵循当前绑定 Skill，本启动提示不增加额外内容限制。",
		"固定工具范围：个人报告使用 self；小组报告使用 team + report_scope=personal；部门报告使用 department + report_scope=team。禁止将小组或部门报告改用 self 或 all。",
		"run_id={{ run_id }}",
		"当前用户凭据已通过 " + credentialSlot + " credential slot 注入；优先调用已绑定的 Aida Report MCP tools 获取上下文并回写生成结果，不要手工拼接 Authorization。",
		"最终必须调用 write_report_result 完成回写；生成失败时调用 write_report_failure。",
	}, "\n")
}

func (h *ManagedAgentHandler) selectDefaultReportAgent(agents []model.ManagedAgent) (model.ManagedAgent, bool) {
	var marked []model.ManagedAgent
	for _, agent := range agents {
		if agent.Archived {
			continue
		}
		if h.isCurrentDeploymentDefaultReportAgent(agent) {
			marked = append(marked, agent)
		}
	}
	if len(marked) > 0 {
		return bestReportAgent(marked, h), true
	}
	return model.ManagedAgent{}, false
}

func (h *ManagedAgentHandler) selectReportAgentForUser(ctx context.Context, userID string, agents []model.ManagedAgent) (model.ManagedAgent, bool, error) {
	if len(agents) == 0 {
		return model.ManagedAgent{}, false, nil
	}
	agentIDs := make([]string, 0, len(agents))
	for _, agent := range agents {
		if agent.Archived || strings.TrimSpace(agent.AgentID) == "" {
			continue
		}
		agentIDs = append(agentIDs, agent.AgentID)
	}
	profiles, err := h.loadManagedAgentProfiles(ctx, userID, agentIDs)
	if err != nil {
		return model.ManagedAgent{}, false, err
	}
	profileAgents := []model.ManagedAgent{}
	defaultProfileAgents := []model.ManagedAgent{}
	for _, agent := range agents {
		if agent.Archived {
			continue
		}
		profile, ok := profiles[agent.AgentID]
		if !ok || profile.BusinessType != managedAgentBusinessReport {
			continue
		}
		agent.BusinessType = managedAgentBusinessReport
		agent.ReportTypes = normalizeManagedAgentReportTypes(profile.ReportTypes)
		agent.IsDefaultReport = profile.IsDefaultReport
		if profile.IsDefaultReport {
			defaultProfileAgents = append(defaultProfileAgents, agent)
		}
		profileAgents = append(profileAgents, agent)
	}
	if len(defaultProfileAgents) > 0 {
		return bestReportAgent(defaultProfileAgents, h), true, nil
	}
	if len(profileAgents) > 0 {
		return bestReportAgent(profileAgents, h), true, nil
	}
	selected, found := h.selectDefaultReportAgent(agents)
	if !found {
		return model.ManagedAgent{}, false, nil
	}
	selected.BusinessType = managedAgentBusinessReport
	selected.ReportTypes = append([]string{}, supportedReportTypes...)
	return selected, true, nil
}

func bestReportAgent(agents []model.ManagedAgent, h *ManagedAgentHandler) model.ManagedAgent {
	best := agents[0]
	bestScore := h.reportAgentScore(best)
	for _, agent := range agents[1:] {
		score := h.reportAgentScore(agent)
		if score > bestScore || score == bestScore && agent.CreatedAt > best.CreatedAt {
			best = agent
			bestScore = score
		}
	}
	return best
}

func (h *ManagedAgentHandler) reportAgentScore(agent model.ManagedAgent) int {
	score := 0
	if isMarkedDefaultReportAgent(agent) {
		score += 100
	}
	if strings.TrimSpace(agent.DefaultModelID) != "" {
		score += 10
	}
	if h.hasReportMCPBinding(agent.MCPBindings) || h.hasReportMCPServer(agent.MCPServers) {
		score += 10
	}
	if strings.TrimSpace(agent.Instructions) != "" {
		score += 5
	}
	if strings.TrimSpace(agent.StartPromptTemplate) != "" {
		score += 5
	}
	if h.hasReportSkillRef(agent.Skills) {
		score += 5
	}
	return score
}

func isMarkedDefaultReportAgent(agent model.ManagedAgent) bool {
	text := strings.Join([]string{agent.Description, agent.Instructions, agent.StartPromptTemplate}, "\n")
	return strings.Contains(text, defaultReportAgentMarker) && strings.Contains(text, defaultManagedAgentMarker)
}

func (h *ManagedAgentHandler) reportDeploymentValue() string {
	value := strings.TrimSpace(h.defaults.AIDAPublicBaseURL)
	if value == "" {
		value = strings.TrimSpace(h.reportMCPURL())
	}
	return value
}

func (h *ManagedAgentHandler) reportDeploymentMarker() string {
	return "AIDA_REPORT_DEPLOYMENT:" + h.reportDeploymentValue()
}

func (h *ManagedAgentHandler) isCurrentDeploymentDefaultReportAgent(agent model.ManagedAgent) bool {
	if !isMarkedDefaultReportAgent(agent) {
		return false
	}
	text := strings.Join([]string{agent.Description, agent.Instructions, agent.StartPromptTemplate}, "\n")
	marker := h.reportDeploymentMarker()
	if strings.Contains(text, marker) {
		return true
	}
	return !strings.Contains(text, "AIDA_REPORT_DEPLOYMENT:")
}

func (h *ManagedAgentHandler) repairedDefaultReportAgentRequest(agent model.ManagedAgent, reportSkill model.ManagedSkillRef) (model.UpsertManagedAgentRequest, bool) {
	req := model.UpsertManagedAgentRequest{
		AgentID:             agent.AgentID,
		Name:                agent.Name,
		Description:         agent.Description,
		Engine:              agent.Engine,
		Instructions:        agent.Instructions,
		DefaultModelID:      agent.DefaultModelID,
		StartPromptTemplate: agent.StartPromptTemplate,
		CredentialSlots:     agent.CredentialSlots,
		DefaultBindings:     agent.DefaultBindings,
		Skills:              agent.Skills,
		MCPServers:          agent.MCPServers,
		MCPBindings:         agent.MCPBindings,
	}
	changed := false
	if strings.TrimSpace(req.Name) == "" {
		req.Name = h.defaults.ReportAgentName
		changed = true
	}
	if strings.TrimSpace(req.Description) == "" {
		req.Description = h.defaults.ReportAgentDescription
		changed = true
	}
	if strings.TrimSpace(req.Engine) == "" {
		req.Engine = h.defaults.Engine
		changed = true
	}
	if strings.TrimSpace(req.DefaultModelID) == "" {
		req.DefaultModelID = h.defaults.ModelID
		changed = true
	}
	if !hasCredentialSlot(req.CredentialSlots, h.defaults.ReportMCPCredentialSlot) {
		req.CredentialSlots = append(req.CredentialSlots, model.ManagedCredentialSlot{
			Name:     h.defaults.ReportMCPCredentialSlot,
			Required: true,
		})
		changed = true
	}
	if servers, ok := h.ensureCurrentReportMCPServer(req.MCPServers); ok {
		req.MCPServers = servers
		changed = true
	}
	if bindings, ok := h.removeReportMCPBindings(req.MCPBindings); ok {
		req.MCPBindings = bindings
		changed = true
	}
	if skills, ok := h.ensureCurrentReportSkillRef(req.Skills, reportSkill); ok {
		req.Skills = skills
		changed = true
	}
	instructions := strings.TrimSpace(req.Instructions)
	if instructions == "" || containsDefaultMarkers(instructions) && isDefaultLikeInstructions(instructions) {
		defaultInstructions := h.reportAgentInstructions()
		if req.Instructions != defaultInstructions {
			req.Instructions = defaultInstructions
			changed = true
		}
	}
	startPromptTemplate := strings.TrimSpace(req.StartPromptTemplate)
	if startPromptTemplate == "" || isDefaultLikeStartPromptTemplate(startPromptTemplate) {
		defaultStartPromptTemplate := h.reportAgentStartPromptTemplate()
		if req.StartPromptTemplate != defaultStartPromptTemplate {
			req.StartPromptTemplate = defaultStartPromptTemplate
			changed = true
		}
	}
	return req, changed
}

func isDefaultLikeStartPromptTemplate(text string) bool {
	return strings.Contains(text, "report_type={{ report_type }}") &&
		strings.Contains(text, "period={{ period_json }}") &&
		strings.Contains(text, "target={{ target_json }}") &&
		strings.Contains(text, "run_id={{ run_id }}") &&
		strings.Contains(text, "Aida Report MCP")
}

func (h *ManagedAgentHandler) repairedReportAgentDependencyRequest(agent model.ManagedAgent, reportSkill model.ManagedSkillRef) (model.UpsertManagedAgentRequest, bool) {
	req := model.UpsertManagedAgentRequest{
		AgentID:             agent.AgentID,
		Name:                agent.Name,
		Description:         agent.Description,
		Engine:              agent.Engine,
		Instructions:        agent.Instructions,
		DefaultModelID:      agent.DefaultModelID,
		StartPromptTemplate: agent.StartPromptTemplate,
		CredentialSlots:     agent.CredentialSlots,
		DefaultBindings:     agent.DefaultBindings,
		Skills:              agent.Skills,
		MCPServers:          agent.MCPServers,
		MCPBindings:         agent.MCPBindings,
	}
	changed := false
	if !hasCredentialSlot(req.CredentialSlots, h.defaults.ReportMCPCredentialSlot) {
		req.CredentialSlots = append(req.CredentialSlots, model.ManagedCredentialSlot{
			Name:     h.defaults.ReportMCPCredentialSlot,
			Required: true,
		})
		changed = true
	}
	if servers, ok := h.ensureCurrentReportMCPServer(req.MCPServers); ok {
		req.MCPServers = servers
		changed = true
	}
	if bindings, ok := h.removeReportMCPBindings(req.MCPBindings); ok {
		req.MCPBindings = bindings
		changed = true
	}
	if skills, ok := h.ensureCurrentReportSkillRef(req.Skills, reportSkill); ok {
		req.Skills = skills
		changed = true
	}
	return req, changed
}

func (h *ManagedAgentHandler) resolveAndRepairReportAgentDependencies(ctx context.Context, client *service.ManagedAgentClient, agent *model.ManagedAgent) error {
	return h.resolveAndRepairReportAgent(ctx, client, agent, false)
}

func (h *ManagedAgentHandler) resolveAndRepairReportAgent(ctx context.Context, client *service.ManagedAgentClient, agent *model.ManagedAgent, repairDefault bool) error {
	if agent == nil {
		return managedAgentConfigError("Report Agent is required")
	}
	systemSkill, err := h.resolveSystemReportSkill(ctx, client)
	if err != nil {
		return err
	}
	expected := managedSkillRef(systemSkill)
	if h.defaults.ReportAssetRepair {
		patch, changed := h.repairedReportAgentDependencyRequest(*agent, expected)
		if repairDefault {
			patch, changed = h.repairedDefaultReportAgentRequest(*agent, expected)
		}
		if changed {
			if _, err := client.UpdateMyAgent(ctx, agent.AgentID, platformManagedAgentRequest(patch)); err != nil {
				return err
			}
			*agent = managedAgentFromUpsertRequest(patch)
		}
	}
	if !hasExactSkillRef(agent.Skills, expected) {
		return managedAgentConfigError("Report Agent does not reference the configured system Report Skill")
	}
	return nil
}

func hasExactSkillRef(skills []model.ManagedSkillRef, expected model.ManagedSkillRef) bool {
	for _, skill := range skills {
		if strings.TrimSpace(skill.Owner) == strings.TrimSpace(expected.Owner) &&
			strings.TrimSpace(skill.Slug) == strings.TrimSpace(expected.Slug) &&
			strings.TrimSpace(skill.Version) == strings.TrimSpace(expected.Version) {
			return true
		}
	}
	return false
}

func hasSkillRef(skills []model.ManagedSkillRef, slug, version string) bool {
	for _, skill := range skills {
		if skill.Slug == slug && skill.Version == version {
			return true
		}
	}
	return false
}

func (h *ManagedAgentHandler) hasReportSkillRef(skills []model.ManagedSkillRef) bool {
	return hasSkillRef(skills, h.defaults.ReportSkillSlug, h.defaults.ReportSkillVersion)
}

func hasCredentialSlot(slots []model.ManagedCredentialSlot, name string) bool {
	for _, slot := range slots {
		if slot.Name == name {
			return true
		}
	}
	return false
}

func ensureReportMCPBindingCredentialSlot(bindings []model.ManagedMCPBinding, slug, version, slot string) bool {
	changed := false
	for i := range bindings {
		if bindings[i].Slug == slug && bindings[i].Version == version && bindings[i].CredentialSlot == "" {
			bindings[i].CredentialSlot = slot
			changed = true
		}
	}
	return changed
}

func (h *ManagedAgentHandler) ensureCurrentReportMCPBinding(bindings []model.ManagedMCPBinding, owner string) ([]model.ManagedMCPBinding, bool) {
	changed := false
	foundCurrent := false
	out := make([]model.ManagedMCPBinding, 0, len(bindings)+1)
	for _, binding := range bindings {
		if binding.Slug != h.defaults.ReportMCPSlug {
			out = append(out, binding)
			continue
		}
		if binding.Version != h.defaults.ReportMCPVersion {
			changed = true
			continue
		}
		if foundCurrent {
			changed = true
			continue
		}
		if binding.CredentialSlot != h.defaults.ReportMCPCredentialSlot {
			binding.CredentialSlot = h.defaults.ReportMCPCredentialSlot
			changed = true
		}
		if strings.TrimSpace(binding.Owner) == "" && strings.TrimSpace(owner) != "" {
			binding.Owner = owner
			changed = true
		}
		foundCurrent = true
		out = append(out, binding)
	}
	if !foundCurrent {
		out = append(out, h.defaultReportMCPBinding(owner))
		changed = true
	}
	return out, changed
}

func (h *ManagedAgentHandler) ensureCurrentReportMCPServer(servers []model.ManagedMCPServer) ([]model.ManagedMCPServer, bool) {
	changed := false
	found := false
	expected := h.defaultReportMCPServer()
	out := make([]model.ManagedMCPServer, 0, len(servers)+1)
	for _, server := range servers {
		if server.Name != h.defaults.ReportMCPSlug {
			out = append(out, server)
			continue
		}
		if found {
			changed = true
			continue
		}
		if server.URL != expected.URL || server.CredentialSlot != expected.CredentialSlot || server.AuthHeader != expected.AuthHeader || server.AuthScheme != expected.AuthScheme {
			server = expected
			changed = true
		}
		found = true
		out = append(out, server)
	}
	if !found {
		out = append(out, expected)
		changed = true
	}
	return out, changed
}

func (h *ManagedAgentHandler) removeReportMCPBindings(bindings []model.ManagedMCPBinding) ([]model.ManagedMCPBinding, bool) {
	changed := false
	out := make([]model.ManagedMCPBinding, 0, len(bindings))
	for _, binding := range bindings {
		if binding.Slug == h.defaults.ReportMCPSlug {
			changed = true
			continue
		}
		out = append(out, binding)
	}
	return out, changed
}

func (h *ManagedAgentHandler) ensureCurrentReportSkillRef(skills []model.ManagedSkillRef, expected model.ManagedSkillRef) ([]model.ManagedSkillRef, bool) {
	changed := false
	foundCurrent := false
	expected.Owner = strings.TrimSpace(expected.Owner)
	expected.Slug = strings.TrimSpace(expected.Slug)
	expected.Version = strings.TrimSpace(expected.Version)
	out := make([]model.ManagedSkillRef, 0, len(skills)+1)
	for _, skill := range skills {
		if strings.TrimSpace(skill.Slug) != h.defaults.ReportSkillSlug {
			out = append(out, skill)
			continue
		}
		if strings.TrimSpace(skill.Version) != expected.Version {
			changed = true
			continue
		}
		if foundCurrent {
			changed = true
			continue
		}
		if strings.TrimSpace(skill.Owner) != expected.Owner || strings.TrimSpace(skill.Slug) != expected.Slug {
			skill = expected
			changed = true
		}
		foundCurrent = true
		out = append(out, skill)
	}
	if !foundCurrent {
		out = append(out, expected)
		changed = true
	}
	return out, changed
}

func containsDefaultMarkers(text string) bool {
	return strings.Contains(text, defaultReportAgentMarker) && strings.Contains(text, defaultManagedAgentMarker)
}

func isDefaultLikeInstructions(text string) bool {
	return strings.Contains(text, "write_report_result") && (strings.Contains(text, "personal_daily") || strings.Contains(text, defaultReportAgentMarker))
}

func (h *ManagedAgentHandler) hasReportMCPBinding(bindings []model.ManagedMCPBinding) bool {
	for _, binding := range bindings {
		if binding.Slug == h.defaults.ReportMCPSlug && binding.Version == h.defaults.ReportMCPVersion {
			return true
		}
	}
	return false
}

func (h *ManagedAgentHandler) hasReportMCPServer(servers []model.ManagedMCPServer) bool {
	for _, server := range servers {
		if server.Name == h.defaults.ReportMCPSlug {
			return true
		}
	}
	return false
}

func (h *ManagedAgentHandler) hasRunnableReportMCP(agent model.ManagedAgent) bool {
	for _, server := range agent.MCPServers {
		if server.Name == h.defaults.ReportMCPSlug && server.CredentialSlot == h.defaults.ReportMCPCredentialSlot {
			return true
		}
	}
	for _, binding := range agent.MCPBindings {
		if binding.Slug == h.defaults.ReportMCPSlug && binding.Version == h.defaults.ReportMCPVersion && binding.CredentialSlot == h.defaults.ReportMCPCredentialSlot {
			return true
		}
	}
	return false
}

func findManagedAgent(ctx context.Context, client *service.ManagedAgentClient, agentID string) (*model.ManagedAgent, error) {
	resp, err := client.ListMyAgents(ctx)
	if err != nil {
		return nil, err
	}
	for _, agent := range resp.Agents {
		if agent.AgentID == agentID && !agent.Archived {
			return &agent, nil
		}
	}
	return nil, nil
}

func findMyManagedAgent(r *http.Request, client *service.ManagedAgentClient, agentID string) (*model.ManagedAgent, error) {
	resp, err := client.ListMyAgents(r.Context())
	if err != nil {
		return nil, err
	}
	for _, agent := range resp.Agents {
		if agent.AgentID == agentID && !agent.Archived {
			return &agent, nil
		}
	}
	return nil, nil
}

func sortedStringKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedStringMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func cleanCredentialOverrides(values map[string]string) map[string]string {
	cleaned := map[string]string{}
	for slot, credentialID := range values {
		slot = strings.TrimSpace(slot)
		credentialID = strings.TrimSpace(credentialID)
		if slot == "" || credentialID == "" {
			continue
		}
		cleaned[slot] = credentialID
	}
	return cleaned
}

func reportTypesForAgent(agent model.ManagedAgent) []string {
	text := strings.Join([]string{agent.Description, agent.Instructions, agent.StartPromptTemplate}, "\n")
	if !strings.Contains(text, defaultReportAgentMarker) {
		return nil
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, defaultReportAgentTypesPrefix) {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, defaultReportAgentTypesPrefix))
		types := []string{}
		for _, item := range strings.Split(raw, ",") {
			item = strings.TrimSpace(item)
			if validateReportType(item) == nil && !containsString(types, item) {
				types = append(types, item)
			}
		}
		if len(types) > 0 {
			return types
		}
	}
	return append([]string{}, supportedReportTypes...)
}

func validateResolvedReportTarget(reportType string, target reportTarget) error {
	switch reportType {
	case reportTypePersonalDaily, reportTypePersonalWeekly:
		if target.UserID == "" {
			return fmt.Errorf("target user is required")
		}
	case reportTypeTeamDaily, reportTypeTeamWeekly:
		if target.TeamID == "" {
			return fmt.Errorf("target team is required")
		}
	case reportTypeDepartmentDaily, reportTypeDepartmentWeekly:
		if target.DepartmentID == "" {
			return fmt.Errorf("target department is required")
		}
	}
	return nil
}

func reportPeriodInputRef(reportType, date, weekStart, weekEnd string) map[string]string {
	if reportType == reportTypePersonalWeekly || reportType == reportTypeTeamWeekly || reportType == reportTypeDepartmentWeekly {
		return map[string]string{"week_start": weekStart, "week_end": weekEnd}
	}
	return map[string]string{"date": date}
}

func reportWeekdayLabel(day time.Time) string {
	labels := [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}
	return labels[day.Weekday()]
}

func reportCalendarContextJSON(reportType, date, weekStart, weekEnd string) string {
	dayPayload := func(day time.Time) map[string]string {
		dateValue := day.Format("2006-01-02")
		weekday := reportWeekdayLabel(day)
		return map[string]string{
			"date":       dateValue,
			"weekday":    weekday,
			"date_label": dateValue + "（" + weekday + "）",
		}
	}

	payload := map[string]any{}
	if reportType == reportTypePersonalWeekly || reportType == reportTypeTeamWeekly || reportType == reportTypeDepartmentWeekly {
		start, startErr := time.Parse("2006-01-02", weekStart)
		end, endErr := time.Parse("2006-01-02", weekEnd)
		if startErr != nil || endErr != nil || end.Before(start) {
			return "{}"
		}
		days := make([]map[string]string, 0, 7)
		for day := start; !day.After(end) && len(days) < 31; day = day.AddDate(0, 0, 1) {
			days = append(days, dayPayload(day))
		}
		payload = map[string]any{
			"type":       "weekly",
			"timezone":   biztime.Zone,
			"week_start": weekStart,
			"week_end":   weekEnd,
			"days":       days,
		}
	} else {
		day, err := time.Parse("2006-01-02", date)
		if err != nil {
			return "{}"
		}
		payload = map[string]any{
			"type":        "daily",
			"timezone":    biztime.Zone,
			"report_date": date,
			"today_means": "period.date",
			"day":         dayPayload(day),
		}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func reportAgentStartPromptValues(runID, reportType, date, weekStart, weekEnd string, target reportTarget, selectedSessionSliceKeys []string, reportSourceSelectionID, mcpURL string) map[string]string {
	_ = mcpURL
	if selectedSessionSliceKeys == nil {
		selectedSessionSliceKeys = []string{}
	}
	periodJSON, _ := json.Marshal(reportPeriodInputRef(reportType, date, weekStart, weekEnd))
	// The MCP resolves the concrete target from the authenticated run. Prompting
	// with database IDs invites the model to copy them into user-facing reports.
	targetJSON, _ := json.Marshal(reportTarget{Type: target.Type})
	selectedKeysJSON, _ := json.Marshal(selectedSessionSliceKeys)
	values := map[string]string{
		"run_id":                           runID,
		"report_type":                      reportType,
		"period_json":                      string(periodJSON),
		"calendar_context_json":            reportCalendarContextJSON(reportType, date, weekStart, weekEnd),
		"target_json":                      string(targetJSON),
		"selected_session_slice_keys_json": string(selectedKeysJSON),
		"report_source_selection_id":       strings.TrimSpace(reportSourceSelectionID),
	}
	if date != "" {
		values["report_date"] = date
	}
	if weekStart != "" {
		values["week_start"] = weekStart
	}
	if weekEnd != "" {
		values["week_end"] = weekEnd
	}
	return values
}

func isReportSystemPromptKey(key, credentialSlot string) bool {
	key = strings.TrimSpace(key)
	if key == strings.TrimSpace(credentialSlot) {
		return true
	}
	_, ok := reportSystemPromptKeys[key]
	return ok
}

func mergeReportStartPromptValues(systemValues map[string]string, userValues map[string]string, message string, credentialSlot string) (map[string]string, string, bool) {
	merged := copyStringMap(systemValues)
	for key, value := range userValues {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if isReportSystemPromptKey(key, credentialSlot) {
			return nil, key, false
		}
		merged[key] = strings.TrimSpace(value)
	}
	message = strings.TrimSpace(message)
	if message != "" {
		if isReportSystemPromptKey("message", credentialSlot) {
			return nil, "message", false
		}
		merged["message"] = message
	}
	return merged, "", true
}

func buildReportRunMessage(startPromptValues map[string]string, message string, credentialSlot string) string {
	parts := []string{
		"请根据以下业务参数生成 Aida 报告。",
		"report_type=" + strings.TrimSpace(startPromptValues["report_type"]),
		"period=" + strings.TrimSpace(startPromptValues["period_json"]),
		"calendar_context=" + strings.TrimSpace(startPromptValues["calendar_context_json"]),
		"target=" + strings.TrimSpace(startPromptValues["target_json"]),
		"report_source_selection_id=" + strings.TrimSpace(startPromptValues["report_source_selection_id"]),
		"run_id=" + strings.TrimSpace(startPromptValues["run_id"]),
		"当前用户凭据已通过 " + strings.TrimSpace(credentialSlot) + " credential slot 注入；优先调用已绑定的 Aida Report MCP tools 获取上下文并回写生成结果，不要手工拼接 Authorization。",
	}
	if instruction := reportRunScopeInstruction(startPromptValues["report_type"]); instruction != "" {
		parts = append(parts, instruction)
	}
	if selectionID := strings.TrimSpace(startPromptValues["report_source_selection_id"]); selectionID != "" {
		parts = append(parts,
			"来源协议：report_source_selection_id 是本次个人报告的不可变来源快照。携带相同的 run_id、report_type、period 和该 ID 调用 get_sessions；快照 ID 只能放在 report_source_selection_id，不得放入 selected_session_slice_keys，也不得同时传 date_range。digest_v1 和 digest_v2 确认 coverage.complete=true、has_more=false；digest_v2 同时确认各日 outcome_coverage.complete=true。仅 legacy full 按 next_cursor 读取到 has_more=false。报告内容与格式遵循当前绑定 Skill，本消息不增加额外内容限制。",
		)
	}
	message = strings.TrimSpace(message)
	if message != "" {
		parts = append(parts, "", "用户补充说明：", message)
	}
	parts = append(parts, "", "最终动作：必须调用 write_report_result 回写；生成失败时调用 write_report_failure。")
	return strings.Join(parts, "\n")
}

func reportRunScopeInstruction(reportType string) string {
	switch strings.TrimSpace(reportType) {
	case reportTypePersonalDaily, reportTypePersonalWeekly:
		return "强制工具范围：个人报告使用 scope.type=self。"
	case reportTypeTeamDaily, reportTypeTeamWeekly:
		return "强制工具范围：小组报告读取下级报告时必须使用 scope.type=team、report_scope=personal；禁止改用 self 或 all。"
	case reportTypeDepartmentDaily, reportTypeDepartmentWeekly:
		return "强制工具范围：部门报告读取下级报告时必须使用 scope.type=department、report_scope=team；禁止改用 self 或 all。"
	default:
		return ""
	}
}

func fallbackReportRunMessage(reportType, date, weekStart, weekEnd string, target reportTarget) string {
	parts := []string{
		"请生成 Aida 报告。",
		"report_type=" + strings.TrimSpace(reportType),
	}
	if date != "" {
		parts = append(parts, "date="+date)
	}
	if weekStart != "" {
		parts = append(parts, "week_start="+weekStart)
	}
	if weekEnd != "" {
		parts = append(parts, "week_end="+weekEnd)
	}
	if target.Type != "" {
		parts = append(parts, "target_type="+target.Type)
	}
	return strings.Join(parts, "\n")
}

func (h *ManagedAgentHandler) StartAgentRun(w http.ResponseWriter, r *http.Request) {
	if !h.ensureConfigured(w) {
		return
	}
	u := getUser(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	agentID := chi.URLParam(r, "agentId")
	var req model.ManagedAgentManualRunRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	req.Message = strings.TrimSpace(req.Message)
	if agentID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "agent_id is required"})
		return
	}
	req.ModelID = strings.TrimSpace(req.ModelID)

	params := map[string]string{}
	for key, value := range req.Params {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		params[key] = strings.TrimSpace(value)
	}
	if req.Message != "" {
		params["message"] = req.Message
	}
	if len(params) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "message or params is required"})
		return
	}

	client := h.clientForRequest(r)
	agent, err := findManagedAgent(r.Context(), client, agentID)
	if err != nil {
		writeManagedAgentError(w, err)
		return
	}
	if agent == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent not found"})
		return
	}

	if h.agentRequiresCredentialedSession(*agent) {
		h.startCredentialedGenericAgentSession(w, r, u, client, *agent, req, params)
		return
	}

	submitResp, err := client.SubmitTask(r.Context(), service.SubmitManagedTaskRequest{
		AgentID: agentID,
		ModelID: req.ModelID,
		Params:  params,
	})
	if err != nil {
		writeManagedAgentError(w, err)
		return
	}

	inputRef := map[string]any{
		"message":        req.Message,
		"params":         req.Params,
		"trigger_source": "manual",
	}
	runID, err := h.insertAIRun(u.ID, "manual_agent_run", agentID, submitResp, req.ModelID, inputRef)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	run, err := h.loadAIRun(runID, u.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (h *ManagedAgentHandler) agentRequiresCredentialedSession(agent model.ManagedAgent) bool {
	for _, slot := range agent.CredentialSlots {
		if slot.Required && strings.TrimSpace(slot.Name) != "" {
			return true
		}
	}
	return false
}

func (h *ManagedAgentHandler) reportMCPCredentialSlots(agent model.ManagedAgent) []string {
	slots := []string{}
	for _, server := range agent.MCPServers {
		if server.Name != h.defaults.ReportMCPSlug {
			continue
		}
		slot := strings.TrimSpace(server.CredentialSlot)
		if slot != "" && !containsString(slots, slot) {
			slots = append(slots, slot)
		}
	}
	for _, binding := range agent.MCPBindings {
		if binding.Slug != h.defaults.ReportMCPSlug || binding.Version != h.defaults.ReportMCPVersion {
			continue
		}
		slot := strings.TrimSpace(binding.CredentialSlot)
		if slot == "" {
			continue
		}
		if !containsString(slots, slot) {
			slots = append(slots, slot)
		}
	}
	return slots
}

func (h *ManagedAgentHandler) missingRequiredCredentialSlots(agent model.ManagedAgent, reportSlots map[string]struct{}, credentialOverrides map[string]string) []string {
	missing := []string{}
	for _, slot := range agent.CredentialSlots {
		name := strings.TrimSpace(slot.Name)
		if !slot.Required || name == "" {
			continue
		}
		if _, ok := reportSlots[name]; ok {
			continue
		}
		if agent.DefaultBindings != nil && strings.TrimSpace(agent.DefaultBindings[name]) != "" {
			continue
		}
		if credentialOverrides != nil && strings.TrimSpace(credentialOverrides[name]) != "" {
			continue
		}
		missing = append(missing, name)
	}
	return missing
}

func (h *ManagedAgentHandler) startCredentialedGenericAgentSession(w http.ResponseWriter, r *http.Request, u *model.User, client *service.ManagedAgentClient, agent model.ManagedAgent, req model.ManagedAgentManualRunRequest, params map[string]string) {
	reportSlots := map[string]struct{}{}
	for _, slot := range h.reportMCPCredentialSlots(agent) {
		reportSlots[slot] = struct{}{}
	}
	runtimeOverrides := cleanCredentialOverrides(req.CredentialOverrides)
	for slot := range reportSlots {
		delete(runtimeOverrides, slot)
	}
	missing := h.missingRequiredCredentialSlots(agent, reportSlots, runtimeOverrides)
	if len(missing) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"code":  "CREDENTIAL_REQUIRED",
			"error": "required credential slots are not bound to usable default credentials",
			"slots": missing,
		})
		return
	}

	token := bearerTokenFromRequest(r)
	if len(reportSlots) > 0 && token == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "current user token is required"})
		return
	}

	inputRef := map[string]any{
		"message":        req.Message,
		"params":         req.Params,
		"trigger_source": "manual",
	}
	if len(reportSlots) > 0 {
		inputRef["credential_slots"] = sortedStringKeys(reportSlots)
		inputRef["credential_override"] = "redacted"
	}
	if len(runtimeOverrides) > 0 {
		inputRef["credential_override_slots"] = sortedStringMapKeys(runtimeOverrides)
		inputRef["credential_override"] = "redacted"
	}
	runID, err := h.insertPendingManagedSessionAIRun(u.ID, "manual_agent_run", agent.AgentID, req.ModelID, inputRef)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	overrides := copyStringMap(runtimeOverrides)
	if len(reportSlots) > 0 {
		credential, err := client.CreateCredential(r.Context(), service.CreateManagedCredentialRequest{
			Name:  "Aida Report MCP Auth " + runID,
			Kind:  "secret",
			Value: token,
			Metadata: map[string]string{
				"aida_user_id": u.ID,
				"ai_run_id":    runID,
				"purpose":      "generic_agent_report_mcp_auth",
			},
		})
		if err != nil {
			_ = h.markAIRunSubmitFailed(r, runID, u.ID, err.Error())
			writeManagedAgentError(w, err)
			return
		}
		for slot := range reportSlots {
			overrides[slot] = credential.CredentialID
		}
	}

	sessionReq := service.CreateManagedSessionRequest{
		AgentID:           agent.AgentID,
		ModelID:           req.ModelID,
		StartPromptValues: params,
	}
	if len(overrides) > 0 {
		sessionReq.CredentialOverrides = overrides
	}
	sessionResp, err := client.CreateSession(r.Context(), sessionReq)
	if err != nil {
		_ = h.markAIRunSubmitFailed(r, runID, u.ID, err.Error())
		writeManagedAgentError(w, err)
		return
	}
	modelID := req.ModelID
	if modelID == "" && sessionResp.ModelID != "" {
		modelID = sessionResp.ModelID
	}
	inputRef["external_session_id"] = sessionResp.SessionID
	inputRef["external_status"] = sessionResp.Status
	if err := h.attachSessionAIRun(runID, u.ID, sessionResp, modelID, inputRef); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	run, err := h.loadAIRun(runID, u.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func isReportMCPCredentialConfigError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "MCP_CONFIG_INVALID") && strings.Contains(msg, "requires a credential")
}

func (h *ManagedAgentHandler) repairedReportMCPOnlyCredentialRequest(agent model.ManagedAgent) (model.UpsertManagedAgentRequest, bool) {
	req := model.UpsertManagedAgentRequest{
		AgentID:             agent.AgentID,
		Name:                agent.Name,
		Description:         agent.Description,
		Engine:              agent.Engine,
		Instructions:        agent.Instructions,
		DefaultModelID:      agent.DefaultModelID,
		StartPromptTemplate: agent.StartPromptTemplate,
		CredentialSlots:     agent.CredentialSlots,
		DefaultBindings:     agent.DefaultBindings,
		Skills:              agent.Skills,
		MCPServers:          agent.MCPServers,
		MCPBindings:         agent.MCPBindings,
	}
	if !h.hasReportMCPBinding(req.MCPBindings) {
		return req, false
	}
	changed := false
	if !hasCredentialSlot(req.CredentialSlots, h.defaults.ReportMCPCredentialSlot) {
		req.CredentialSlots = append(req.CredentialSlots, model.ManagedCredentialSlot{
			Name:     h.defaults.ReportMCPCredentialSlot,
			Required: true,
		})
		changed = true
	}
	if ensureReportMCPBindingCredentialSlot(req.MCPBindings, h.defaults.ReportMCPSlug, h.defaults.ReportMCPVersion, h.defaults.ReportMCPCredentialSlot) {
		changed = true
	}
	return req, changed
}

func (h *ManagedAgentHandler) retryCredentialedReportMCPAgentRun(w http.ResponseWriter, r *http.Request, u *model.User, client *service.ManagedAgentClient, agentID string, req model.ManagedAgentManualRunRequest, params map[string]string, submitErr error) bool {
	if !isReportMCPCredentialConfigError(submitErr) {
		return false
	}
	agent, err := findMyManagedAgent(r, client, agentID)
	if err != nil || agent == nil || !h.hasReportMCPBinding(agent.MCPBindings) {
		return false
	}
	token := bearerTokenFromRequest(r)
	if token == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "current user token is required"})
		return true
	}
	patch, needsRepair := h.repairedReportMCPOnlyCredentialRequest(*agent)
	if needsRepair {
		if _, err := client.UpdateMyAgent(r.Context(), agentID, platformManagedAgentRequest(patch)); err != nil {
			writeManagedAgentError(w, err)
			return true
		}
	}
	inputRef := map[string]any{
		"message":             req.Message,
		"params":              req.Params,
		"trigger_source":      "manual",
		"credential_slot":     h.defaults.ReportMCPCredentialSlot,
		"credential_override": "redacted",
	}
	runID, err := h.insertPendingManagedSessionAIRun(u.ID, "manual_agent_run", agentID, req.ModelID, inputRef)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return true
	}
	credential, err := client.CreateCredential(r.Context(), service.CreateManagedCredentialRequest{
		Name:  "Aida Report MCP Auth " + runID,
		Kind:  "secret",
		Value: token,
		Metadata: map[string]string{
			"aida_user_id": u.ID,
			"ai_run_id":    runID,
			"purpose":      "generic_agent_report_mcp_auth",
		},
	})
	if err != nil {
		_ = h.markAIRunSubmitFailed(r, runID, u.ID, err.Error())
		writeManagedAgentError(w, err)
		return true
	}
	sessionResp, err := client.CreateSession(r.Context(), service.CreateManagedSessionRequest{
		AgentID:           agentID,
		ModelID:           req.ModelID,
		StartPromptValues: params,
		CredentialOverrides: map[string]string{
			h.defaults.ReportMCPCredentialSlot: credential.CredentialID,
		},
	})
	if err != nil {
		_ = h.markAIRunSubmitFailed(r, runID, u.ID, err.Error())
		writeManagedAgentError(w, err)
		return true
	}
	modelID := req.ModelID
	if modelID == "" && sessionResp.ModelID != "" {
		modelID = sessionResp.ModelID
	}
	inputRef["external_session_id"] = sessionResp.SessionID
	inputRef["external_status"] = sessionResp.Status
	if err := h.attachSessionAIRun(runID, u.ID, sessionResp, modelID, inputRef); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return true
	}
	run, err := h.loadAIRun(runID, u.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return true
	}
	writeJSON(w, http.StatusOK, run)
	return true
}

func (h *ManagedAgentHandler) StartReportAgentRun(w http.ResponseWriter, r *http.Request) {
	if !h.ensureConfigured(w) {
		return
	}
	if h.defaults.AIDAPublicBaseURL == "" {
		writeManagedAgentConfigError(w, "AIDA_PUBLIC_BASE_URL is required for Report Agent")
		return
	}
	u := getUser(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	token := bearerTokenFromRequest(r)
	if token == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "current user token is required"})
		return
	}

	agentID := strings.TrimSpace(chi.URLParam(r, "agentId"))
	var req model.ManagedReportAgentRunRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	req.ReportType = strings.TrimSpace(req.ReportType)
	if err := validateReportType(req.ReportType); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "REPORT_TYPE_NOT_SUPPORTED", "error": "unsupported report_type"})
		return
	}
	for key := range req.StartPromptValues {
		if isReportSystemPromptKey(key, h.defaults.ReportMCPCredentialSlot) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": reservedPromptValueCode, "error": strings.TrimSpace(key) + " is managed by Aida"})
			return
		}
	}

	client := h.clientForRequest(r)
	if agentID == "default" {
		agentsResp, err := client.ListMyAgents(r.Context())
		if err != nil {
			writeManagedAgentError(w, err)
			return
		}
		selected, found, err := h.selectReportAgentForUser(r.Context(), u.ID, agentsResp.Agents)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if !found {
			writeJSON(w, http.StatusOK, map[string]any{
				"available": false,
				"code":      "DEFAULT_REPORT_AGENT_NOT_CONFIGURED",
				"message":   "未配置默认报告 Agent",
			})
			return
		}
		agentID = selected.AgentID
	}
	agent, err := findMyManagedAgent(r, client, agentID)
	if err != nil {
		writeManagedAgentError(w, err)
		return
	}
	if agent == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent not found"})
		return
	}
	profile, err := h.loadManagedAgentProfile(r.Context(), u.ID, agentID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	var supported []string
	if profile != nil {
		if profile.BusinessType != managedAgentBusinessReport {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "NOT_REPORT_AGENT", "error": "agent is not a Report Agent"})
			return
		}
		supported = profile.ReportTypes
	} else {
		supported = reportTypesForAgent(*agent)
		if len(supported) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "NOT_REPORT_AGENT", "error": "agent is not a Report Agent"})
			return
		}
	}
	if len(supported) == 0 || !containsString(supported, req.ReportType) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "REPORT_TYPE_NOT_SUPPORTED", "error": "unsupported report_type"})
		return
	}
	repairDefault := profile != nil && profile.IsDefaultReport || profile == nil && isMarkedDefaultReportAgent(*agent)
	if err := h.resolveAndRepairReportAgent(r.Context(), client, agent, repairDefault); err != nil {
		writeManagedAgentError(w, err)
		return
	}
	if !h.hasRunnableReportMCP(*agent) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "REPORT_MCP_REQUIRED", "error": "Report Agent must bind Aida Report MCP"})
		return
	}
	reportSlots := map[string]struct{}{}
	for _, slot := range h.reportMCPCredentialSlots(*agent) {
		reportSlots[slot] = struct{}{}
	}
	if len(reportSlots) == 0 {
		reportSlots[h.defaults.ReportMCPCredentialSlot] = struct{}{}
	}
	runtimeOverrides := cleanCredentialOverrides(req.CredentialOverrides)
	for slot := range reportSlots {
		delete(runtimeOverrides, slot)
	}
	missing := h.missingRequiredCredentialSlots(*agent, reportSlots, runtimeOverrides)
	if len(missing) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"code":  "CREDENTIAL_REQUIRED",
			"error": "required credential slots are not bound to usable default credentials",
			"slots": missing,
		})
		return
	}

	period := periodArgs{Date: req.Period.Date, WeekStart: req.Period.WeekStart, WeekEnd: req.Period.WeekEnd}
	date, weekStart, weekEnd, err := resolveReportPeriod(req.ReportType, period)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid period"})
		return
	}
	targetIn := reportTarget{
		Type:         req.Target.Type,
		UserID:       req.Target.UserID,
		TeamID:       req.Target.TeamID,
		DepartmentID: req.Target.DepartmentID,
	}
	target, err := resolveTarget(u, targetIn, req.ReportType, true)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden target"})
		return
	}
	if err := validateResolvedReportTarget(req.ReportType, target); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	selectedSessionSliceKeys, err := normalizeReportSourceSliceKeys(req.SelectedSessionSliceKeys)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	reportSourceSelectionID := strings.TrimSpace(req.ReportSourceSelectionID)
	isPersonalReport := req.ReportType == reportTypePersonalDaily || req.ReportType == reportTypePersonalWeekly
	if sourceErr := validateReportSourceRunInput(isPersonalReport, reportSourceSelectionID, selectedSessionSliceKeys); sourceErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"code": sourceErr.Code, "error": sourceErr.Message,
		})
		return
	}
	reportSourceAvailable := isPersonalReport && h.reportSource != nil

	modelID := strings.TrimSpace(req.ModelID)
	inputRef := map[string]any{
		"trigger_source":  "manual",
		"report_type":     req.ReportType,
		"period":          reportPeriodInputRef(req.ReportType, date, weekStart, weekEnd),
		"target":          target,
		"model_id":        modelID,
		"mcp_server":      h.defaults.ReportMCPSlug,
		"credential_slot": h.defaults.ReportMCPCredentialSlot,
	}
	if len(selectedSessionSliceKeys) > 0 {
		inputRef["selected_session_slice_keys"] = selectedSessionSliceKeys
	}
	if len(runtimeOverrides) > 0 {
		inputRef["credential_override_slots"] = sortedStringMapKeys(runtimeOverrides)
		inputRef["credential_override"] = "redacted"
	}
	var runID string
	if reportSourceAvailable {
		selectionPeriod, periodErr := reportsource.ReportPeriod(req.ReportType, date, weekStart, weekEnd)
		if periodErr != nil {
			writeReportSourceError(w, periodErr)
			return
		}
		if len(selectedSessionSliceKeys) > 0 {
			inputs := make([]reportsource.SourceInput, 0, len(selectedSessionSliceKeys))
			for _, sliceKey := range selectedSessionSliceKeys {
				inputs = append(inputs, reportsource.SourceInput{SliceKey: sliceKey})
			}
			prepared, prepareErr := h.reportSource.CreateExplicit(
				r.Context(), u.ID, req.ReportType, selectionPeriod, inputs,
			)
			if prepareErr != nil {
				writeReportSourceError(w, prepareErr)
				return
			}
			reportSourceSelectionID = prepared.ID
		}
		var attached reportsource.Selection
		runID, attached, err = h.reportSource.CreateAttachedRun(
			r.Context(), u.ID, req.ReportType, selectionPeriod, reportSourceSelectionID,
			reportAgentRunBusinessType, agentID, modelID, req.LargeContextConfirmed, inputRef,
		)
		if err != nil {
			writeReportSourceError(w, err)
			return
		}
		reportSourceSelectionID = attached.ID
	} else {
		runID, err = h.insertPendingManagedSessionAIRun(u.ID, reportAgentRunBusinessType, agentID, modelID, inputRef)
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	credential, err := client.CreateCredential(r.Context(), service.CreateManagedCredentialRequest{
		Name:  "Aida Report MCP Auth " + runID,
		Kind:  "secret",
		Value: token,
		Metadata: map[string]string{
			"aida_user_id": u.ID,
			"ai_run_id":    runID,
			"purpose":      "report_mcp_auth",
		},
	})
	if err != nil {
		_ = h.markAIRunSubmitFailed(r, runID, u.ID, err.Error())
		writeManagedAgentError(w, err)
		return
	}

	systemPromptValues := reportAgentStartPromptValues(runID, req.ReportType, date, weekStart, weekEnd, target, nil, reportSourceSelectionID, h.reportMCPURL())
	userMessage := strings.TrimSpace(req.Message)
	if userMessage == "" {
		userMessage = fallbackReportRunMessage(req.ReportType, date, weekStart, weekEnd, target)
	}
	startPromptValues, reservedKey, ok := mergeReportStartPromptValues(systemPromptValues, req.StartPromptValues, userMessage, h.defaults.ReportMCPCredentialSlot)
	if !ok {
		_ = h.markAIRunSubmitFailed(r, runID, u.ID, reservedKey+" is managed by Aida")
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": reservedPromptValueCode, "error": reservedKey + " is managed by Aida"})
		return
	}
	sessionMessage := buildReportRunMessage(startPromptValues, userMessage, h.defaults.ReportMCPCredentialSlot)
	overrides := copyStringMap(runtimeOverrides)
	for slot := range reportSlots {
		overrides[slot] = credential.CredentialID
	}
	sessionResp, err := client.CreateSession(r.Context(), service.CreateManagedSessionRequest{
		AgentID:             agentID,
		ModelID:             modelID,
		StartPromptValues:   startPromptValues,
		Message:             sessionMessage,
		CredentialOverrides: overrides,
	})
	if err != nil {
		_ = h.markAIRunSubmitFailed(r, runID, u.ID, err.Error())
		writeManagedAgentError(w, err)
		return
	}
	if modelID == "" && sessionResp.ModelID != "" {
		modelID = sessionResp.ModelID
	}
	inputRef["start_prompt_values"] = copyStringMap(startPromptValues)
	if sessionMessage != "" {
		inputRef["message"] = sessionMessage
	}
	inputRef["credential_override"] = "redacted"
	if err := h.attachSessionAIRun(runID, u.ID, sessionResp, modelID, inputRef); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	run, err := h.loadAIRun(runID, u.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, run)
}

type reportSourceRunInputError struct {
	Code    string
	Message string
}

func normalizeReportSourceSliceKeys(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		key := strings.TrimSpace(value)
		if key == "" {
			continue
		}
		if !isValidUUID(key) {
			return nil, fmt.Errorf("selected_session_slice_keys must contain slice UUIDs")
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	if len(result) > 200 {
		return nil, fmt.Errorf("selected_session_slice_keys supports at most 200 values")
	}
	return result, nil
}

func validateReportSourceRunInput(isPersonalReport bool, selectionID string, sliceKeys []string) *reportSourceRunInputError {
	if strings.TrimSpace(selectionID) != "" && len(sliceKeys) > 0 {
		return &reportSourceRunInputError{
			Code:    "AMBIGUOUS_REPORT_SOURCE",
			Message: "report_source_selection_id and selected_session_slice_keys are mutually exclusive",
		}
	}
	if !isPersonalReport && (strings.TrimSpace(selectionID) != "" || len(sliceKeys) > 0) {
		return &reportSourceRunInputError{
			Code:    "INVALID_REPORT_SOURCE",
			Message: "report source selection is only valid for personal reports",
		}
	}
	return nil
}

func (h *ManagedAgentHandler) GetAgentRun(w http.ResponseWriter, r *http.Request) {
	if !h.ensureConfigured(w) {
		return
	}
	u := getUser(r)
	runID := chi.URLParam(r, "runId")
	run, err := h.loadAIRun(runID, u.ID)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"code":    "AI_RUN_NOT_FOUND",
			"message": "运行记录不存在或已失效",
		})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if run.ExternalTaskID != nil && (!isTerminalManagedStatus(run.Status) || (run.Status == "succeeded" && run.Result == "")) {
		refreshed, err := h.refreshAIRun(r, run)
		if err != nil {
			msg := err.Error()
			run.ErrorMessage = &msg
			writeJSON(w, http.StatusOK, run)
			return
		}
		run = refreshed
	}
	writeJSON(w, http.StatusOK, run)
}

func (h *ManagedAgentHandler) ListAgentRuns(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	agentID := strings.TrimSpace(r.URL.Query().Get("agent_id"))
	businessType := strings.TrimSpace(r.URL.Query().Get("business_type"))
	_, limit := parsePagination(r, 50, 100)

	query := aiRunSelectColumns + " WHERE user_id = $1"
	args := []any{u.ID}
	argIdx := 2
	if agentID != "" {
		query += fmt.Sprintf(" AND agent_id = $%d", argIdx)
		args = append(args, agentID)
		argIdx++
	}
	if businessType != "" {
		query += fmt.Sprintf(" AND business_type = $%d", argIdx)
		args = append(args, businessType)
		argIdx++
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := h.db.Query(query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	runs := []model.AIRun{}
	for rows.Next() {
		run, err := scanAIRun(rows)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		runs = append(runs, *run)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

func (h *ManagedAgentHandler) DailyReportIntegration(w http.ResponseWriter, r *http.Request) {
	mcpURL := h.reportMCPURL()
	if strings.TrimSpace(mcpURL) == "/api/v1/mcp/reports" {
		mcpURL = absoluteRequestURL(r, "/api/v1/mcp/reports")
	}
	skillMarkdown := h.reportSkillMarkdown()
	if h.defaults.ReportSkillMarkdown == "" && mcpURL != h.reportMCPURL() {
		skillMarkdown = service.ReportSkillMarkdownWithConfig(service.ReportSkillTemplateData{
			MCPURL:               mcpURL,
			MCPSlug:              h.defaults.ReportMCPSlug,
			MCPVersion:           h.defaults.ReportMCPVersion,
			CredentialSlot:       h.defaults.ReportMCPCredentialSlot,
			SupportedReportTypes: supportedReportTypes,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"mcp": map[string]any{
			"name":        h.defaults.ReportMCPName,
			"slug":        h.defaults.ReportMCPSlug,
			"version":     h.defaults.ReportMCPVersion,
			"url":         mcpURL,
			"transport":   "http",
			"status":      "active",
			"managed":     true,
			"description": h.defaults.ReportMCPDescription,
			"tools": []string{
				"get_sessions",
				"get_daily_reports",
				"get_weekly_reports",
				"get_tasks",
				"get_requirements",
				"get_existing_report",
				"get_report_inventory",
				"write_report_result",
				"write_report_failure",
			},
		},
		"skill": map[string]any{
			"slug":     h.defaults.ReportSkillSlug,
			"version":  h.defaults.ReportSkillVersion,
			"name":     h.defaults.ReportSkillName,
			"status":   "active",
			"managed":  true,
			"skill_md": skillMarkdown,
		},
	})
}

func (h *ManagedAgentHandler) ListAgentSchedules(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	rows, err := h.db.Query(managedAgentScheduleSelectColumns+" WHERE s.user_id = $1 ORDER BY s.created_at DESC", u.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	items := []model.ManagedAgentSchedule{}
	for rows.Next() {
		item, err := scanManagedAgentSchedule(rows)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"schedules": items})
}

func (h *ManagedAgentHandler) CreateAgentSchedule(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	var req model.UpsertManagedAgentScheduleRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	normalized, err := normalizeManagedAgentScheduleRequest(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := h.validateManagedAgentScheduleConfig(r.Context(), u, normalized, h.clientForRequest(r)); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	paramsJSON, _ := json.Marshal(normalized.StartPromptValues)
	weekdaysJSON, _ := json.Marshal(normalized.Weekdays)
	reportConfigJSON, _ := json.Marshal(normalized.ReportConfig)
	nextRunAt, err := computeManagedAgentNextRunAt(normalized.ScheduleType, normalized.Weekdays, normalized.TimeOfDay, normalized.Timezone, time.Now())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	var nextRunValue any
	if normalized.Enabled {
		nextRunValue = nextRunAt
	}

	var id string
	err = h.db.QueryRow(`
		INSERT INTO managed_agent_schedules (
			user_id, name, agent_id, run_kind, model_id, message, params_json,
			start_prompt_values_json, report_config_json, schedule_type, weekdays_json,
			time_of_day, timezone, enabled, next_run_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING id::text`,
		u.ID, normalized.Name, normalized.AgentID, normalized.RunKind, nullString(&normalized.ModelID),
		normalized.InitialMessage, paramsJSON, paramsJSON, reportConfigJSON, normalized.ScheduleType,
		weekdaysJSON, normalized.TimeOfDay, normalized.Timezone, normalized.Enabled, nextRunValue,
	).Scan(&id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	schedule, err := h.loadManagedAgentSchedule(id, u.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, schedule)
}

func (h *ManagedAgentHandler) UpdateAgentSchedule(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	scheduleID := chi.URLParam(r, "scheduleId")
	var req model.UpsertManagedAgentScheduleRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	normalized, err := normalizeManagedAgentScheduleRequest(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := h.validateManagedAgentScheduleConfig(r.Context(), u, normalized, h.clientForRequest(r)); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	paramsJSON, _ := json.Marshal(normalized.StartPromptValues)
	weekdaysJSON, _ := json.Marshal(normalized.Weekdays)
	reportConfigJSON, _ := json.Marshal(normalized.ReportConfig)
	nextRunAt, err := computeManagedAgentNextRunAt(normalized.ScheduleType, normalized.Weekdays, normalized.TimeOfDay, normalized.Timezone, time.Now())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	var nextRunValue any
	if normalized.Enabled {
		nextRunValue = nextRunAt
	}

	res, err := h.db.Exec(`
		UPDATE managed_agent_schedules
		SET name = $1, agent_id = $2, run_kind = $3, model_id = $4, message = $5,
			params_json = $6, start_prompt_values_json = $7, report_config_json = $8,
			schedule_type = $9, weekdays_json = $10, time_of_day = $11, timezone = $12,
			enabled = $13, next_run_at = $14, updated_at = now()
		WHERE id = $15 AND user_id = $16`,
		normalized.Name, normalized.AgentID, normalized.RunKind, nullString(&normalized.ModelID),
		normalized.InitialMessage, paramsJSON, paramsJSON, reportConfigJSON, normalized.ScheduleType,
		weekdaysJSON, normalized.TimeOfDay, normalized.Timezone, normalized.Enabled, nextRunValue, scheduleID, u.ID,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	schedule, err := h.loadManagedAgentSchedule(scheduleID, u.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, schedule)
}

func (h *ManagedAgentHandler) DeleteAgentSchedule(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	scheduleID := chi.URLParam(r, "scheduleId")
	res, err := h.db.Exec(`DELETE FROM managed_agent_schedules WHERE id = $1 AND user_id = $2`, scheduleID, u.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *ManagedAgentHandler) RunAgentScheduleNow(w http.ResponseWriter, r *http.Request) {
	if !h.ensureConfigured(w) {
		return
	}
	u := getUser(r)
	token := bearerTokenFromRequest(r)
	scheduleID := chi.URLParam(r, "scheduleId")
	schedule, err := h.loadManagedAgentSchedule(scheduleID, u.ID)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	triggerSource := "manual"
	var runReq struct {
		TriggerSource string `json:"trigger_source"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&runReq)
		if strings.TrimSpace(runReq.TriggerSource) == "save_and_run" {
			triggerSource = "save_and_run"
		}
	}
	run, err := h.executeManagedAgentScheduleRun(r.Context(), schedule, u, token, triggerSource, time.Now(), false)
	if err != nil {
		writeManagedAgentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (h *ManagedAgentHandler) PreviewAgentSchedule(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	var req model.PreviewManagedAgentScheduleRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	scheduleReq := model.UpsertManagedAgentScheduleRequest{
		Name:          "preview",
		AgentID:       req.AgentID,
		RunKind:       req.RunKind,
		ScheduleType:  req.ScheduleType,
		Weekdays:      req.Weekdays,
		TimeOfDay:     req.TimeOfDay,
		TriggerConfig: req.TriggerConfig,
		RunConfig:     req.RunConfig,
	}
	reportType := strings.TrimSpace(req.ReportType)
	if reportType == "" && req.ReportConfig != nil {
		reportType = strings.TrimSpace(req.ReportConfig.ReportType)
	}
	if reportType == "" && req.RunConfig != nil && req.RunConfig.ReportConfig != nil {
		reportType = strings.TrimSpace(req.RunConfig.ReportConfig.ReportType)
	}
	if reportType != "" {
		scheduleReq.ReportConfig = &model.ManagedAgentScheduleReportConfig{ReportType: reportType}
	}
	normalized, err := normalizeManagedAgentScheduleRequest(scheduleReq)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	nextRunAt, err := computeManagedAgentNextRunAt(normalized.ScheduleType, normalized.Weekdays, normalized.TimeOfDay, normalized.Timezone, time.Now())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	resp := model.PreviewManagedAgentScheduleResponse{
		NextRunAt:                    nextRunAt,
		ScheduledTriggerAtForPreview: nextRunAt,
		AgentType:                    managedAgentBusinessGeneric,
	}
	if normalized.RunKind == scheduleRunKindReport {
		reportType := normalized.ReportConfig["report_type"]
		target, err := resolveTarget(u, reportTarget{}, reportType, true)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "无法推导报告对象"})
			return
		}
		if err := validateResolvedReportTarget(reportType, target); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		period := reportPeriodForScheduledAt(reportType, nextRunAt, normalized.Timezone)
		resp.AgentType = managedAgentBusinessReport
		resp.ReportType = reportType
		resp.ReportTargetDisplay = reportTargetDisplay(reportType)
		resp.PeriodStart = period.Start
		resp.PeriodEnd = period.End
		resp.PeriodDisplay = period.Display
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *ManagedAgentHandler) StartReportRun(w http.ResponseWriter, r *http.Request) {
	if !h.ensureConfigured(w) {
		return
	}
	u := getUser(r)
	var req model.ManagedReportRunRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	req.SessionIDs = uniqueStringsPreserveOrder(req.SessionIDs)
	if strings.TrimSpace(req.AgentID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "agent_id is required"})
		return
	}
	if len(req.SessionIDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session_ids is required"})
		return
	}
	reportType := strings.TrimSpace(req.ReportType)
	if reportType == "" {
		reportType = "personal_daily"
	}
	if reportType != "personal_daily" && reportType != "personal_weekly" &&
		reportType != "team_daily" && reportType != "team_weekly" &&
		reportType != "department_daily" && reportType != "department_weekly" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported report_type"})
		return
	}
	reportDate := req.ReportDate
	if reportDate == "" {
		reportDate = service.TodayInLocalDate()
	}

	sessions, err := loadDraftSessions(h.db, u.ID, req.SessionIDs)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if len(sessions) != len(req.SessionIDs) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "one or more sessions are not accessible"})
		return
	}
	sessionURLs := dailyReportSessionLogURLs(r, orderDraftSessions(sessions, req.SessionIDs))
	urlsJSON, _ := json.Marshal(sessionURLs)
	req.ModelID = strings.TrimSpace(req.ModelID)
	if req.ModelID == "" {
		req.ModelID = h.defaults.ModelID
	}

	client := h.clientForRequest(r)
	submitResp, err := client.SubmitTask(r.Context(), service.SubmitManagedTaskRequest{
		AgentID: req.AgentID,
		ModelID: req.ModelID,
		Params: map[string]string{
			"urls":            string(urlsJSON),
			"output_contract": service.DailyReportOutputContract(),
			"report_date":     reportDate,
			"report_type":     reportType,
		},
	})
	if err != nil {
		writeManagedAgentError(w, err)
		return
	}

	period := map[string]any{"date": reportDate}
	if reportType == "personal_weekly" || reportType == "team_weekly" || reportType == "department_weekly" {
		period = map[string]any{"week_start": req.WeekStart, "week_end": req.WeekEnd}
	}
	inputRef := map[string]any{
		"report_type": reportType,
		"period":      period,
		"report_date": reportDate,
		"session_ids": req.SessionIDs,
		"urls":        sessionURLs,
	}
	runID, err := h.insertAIRun(u.ID, reportType, req.AgentID, submitResp, req.ModelID, inputRef)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	run, err := h.loadAIRun(runID, u.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (h *ManagedAgentHandler) GetDailyReportRun(w http.ResponseWriter, r *http.Request) {
	if !h.ensureConfigured(w) {
		return
	}
	u := getUser(r)
	runID := chi.URLParam(r, "runId")
	run, err := h.loadAIRun(runID, u.ID)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if run.ExternalTaskID != nil && !isTerminalManagedStatus(run.Status) {
		refreshed, err := h.refreshAIRun(r, run)
		if err != nil {
			writeJSON(w, http.StatusOK, run)
			return
		}
		run = refreshed
	}
	writeJSON(w, http.StatusOK, run)
}

func (h *ManagedAgentHandler) refreshAIRun(r *http.Request, run *model.AIRun) (*model.AIRun, error) {
	client := h.clientForRequest(r)
	task, err := client.GetTaskResult(r.Context(), *run.ExternalTaskID)
	if err != nil {
		return nil, err
	}
	status := normalizeManagedRunStatus(task.Status)
	output := map[string]any{
		"task_id":  task.TaskID,
		"status":   task.Status,
		"progress": task.Progress,
		"result":   task.Result,
	}
	if task.Error != "" {
		output["error"] = task.Error
	}
	if len(task.Raw) > 0 {
		var raw any
		if json.Unmarshal(task.Raw, &raw) == nil {
			output["raw"] = raw
		}
	}

	var errMsg *string
	if status == "failed" && task.Error != "" {
		errMsg = &task.Error
	}

	outputJSON, _ := json.Marshal(output)
	sets := []string{"status = $1", "output_ref_json = $2", "agent_version_id = $3"}
	args := []any{status, outputJSON, nullableInt(task.AgentVersionID)}
	argIdx := 4
	if task.ModelID != "" {
		sets = append(sets, fmt.Sprintf("model_id = $%d", argIdx))
		args = append(args, task.ModelID)
		argIdx++
	}
	if errMsg != nil {
		sets = append(sets, fmt.Sprintf("error_message = $%d", argIdx))
		args = append(args, *errMsg)
		argIdx++
	}
	if isTerminalManagedStatus(status) {
		sets = append(sets, "finished_at = now()")
	}
	args = append(args, run.ID)
	if _, err := h.db.Exec(fmt.Sprintf("UPDATE ai_runs SET %s WHERE id = $%d", joinWithCommas(sets), argIdx), args...); err != nil {
		return nil, err
	}

	refreshed, err := h.loadAIRun(run.ID, run.UserID)
	if err != nil {
		return nil, err
	}
	return refreshed, nil
}

// insertAIRun persists a freshly submitted managed-agent task as an ai_runs row
// and returns the new run id. Shared by the manual-agent and daily-report runs.
func (h *ManagedAgentHandler) insertAIRun(userID, businessType, agentID string, submit *service.SubmitManagedTaskResponse, modelID string, inputRef map[string]any) (string, error) {
	inputJSON, _ := json.Marshal(inputRef)
	var runID string
	err := h.db.QueryRow(`
		INSERT INTO ai_runs (
			user_id, business_type, runtime_type, agent_id, external_task_id,
			model_id, status, input_ref_json, started_at
		)
		VALUES ($1, $2, 'managed_task', $3, $4, $5, $6, $7, now())
		RETURNING id::text`,
		userID, businessType, agentID, submit.TaskID, nullableString(modelID), normalizeManagedRunStatus(submit.Status), inputJSON,
	).Scan(&runID)
	return runID, err
}

func (h *ManagedAgentHandler) insertPendingAIRun(userID, businessType, agentID, modelID string, inputRef map[string]any) (string, error) {
	inputJSON, _ := json.Marshal(inputRef)
	var runID string
	err := h.db.QueryRow(`
		INSERT INTO ai_runs (
			user_id, business_type, runtime_type, agent_id,
			model_id, status, input_ref_json, started_at
		)
		VALUES ($1, $2, 'managed_task', $3, $4, 'pending', $5, now())
		RETURNING id::text`,
		userID, businessType, agentID, nullableString(modelID), inputJSON,
	).Scan(&runID)
	return runID, err
}

func (h *ManagedAgentHandler) insertPendingManagedSessionAIRun(userID, businessType, agentID, modelID string, inputRef map[string]any) (string, error) {
	inputJSON, _ := json.Marshal(inputRef)
	var runID string
	err := h.db.QueryRow(`
		INSERT INTO ai_runs (
			user_id, business_type, runtime_type, agent_id,
			model_id, status, input_ref_json, started_at
		)
		VALUES ($1, $2, 'managed_session', $3, $4, 'pending', $5, now())
		RETURNING id::text`,
		userID, businessType, agentID, nullableString(modelID), inputJSON,
	).Scan(&runID)
	return runID, err
}

func (h *ManagedAgentHandler) attachSubmittedAIRun(runID, userID string, submit *service.SubmitManagedTaskResponse, modelID string, inputRef map[string]any) error {
	inputJSON, _ := json.Marshal(inputRef)
	_, err := h.db.Exec(`
		UPDATE ai_runs
		SET external_task_id = $1, model_id = $2, status = $3, input_ref_json = $4
		WHERE id = $5 AND user_id = $6`,
		submit.TaskID, nullableString(modelID), normalizeManagedRunStatus(submit.Status), inputJSON, runID, userID,
	)
	return err
}

func (h *ManagedAgentHandler) attachSessionAIRun(runID, userID string, session *service.CreateManagedSessionResponse, modelID string, inputRef map[string]any) error {
	inputJSON, _ := json.Marshal(inputRef)
	_, err := h.db.Exec(`
		UPDATE ai_runs
		SET external_session_id = $1, model_id = $2, status = $3, input_ref_json = $4
		WHERE id = $5 AND user_id = $6`,
		session.SessionID, nullableString(modelID), normalizeManagedRunStatus(session.Status), inputJSON, runID, userID,
	)
	return err
}

func (h *ManagedAgentHandler) markAIRunSubmitFailed(r *http.Request, runID, userID, message string) error {
	_, err := h.db.ExecContext(r.Context(), `
		UPDATE ai_runs
		SET status = 'failed', error_message = $1, finished_at = now()
		WHERE id = $2 AND user_id = $3`,
		message, runID, userID,
	)
	return err
}

func copyStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

const aiRunSelectColumns = `SELECT id::text, user_id::text, business_type, business_id::text, runtime_type,
			agent_id, agent_version_id, external_task_id, external_session_id, model_id,
			status, input_ref_json, output_ref_json, error_message, started_at, finished_at, created_at
		FROM ai_runs`

// scanAIRun scans one ai_runs row (from *sql.Row or *sql.Rows) into a model.AIRun.
// Shared by loadAIRun (point lookup) and ListAgentRuns (batch) so the column
// list and the scan cannot drift apart.
func scanAIRun(row rowScanner) (*model.AIRun, error) {
	var run model.AIRun
	var businessID, externalTaskID, externalSessionID, modelID, errMsg sql.NullString
	var agentVersionID sql.NullInt64
	var startedAt, finishedAt sql.NullTime
	var inputRaw, outputRaw []byte
	if err := row.Scan(
		&run.ID, &run.UserID, &run.BusinessType, &businessID, &run.RuntimeType,
		&run.AgentID, &agentVersionID, &externalTaskID, &externalSessionID, &modelID,
		&run.Status, &inputRaw, &outputRaw, &errMsg, &startedAt, &finishedAt, &run.CreatedAt,
	); err != nil {
		return nil, err
	}
	run.BusinessID = nullStringPtr(businessID)
	run.ExternalTaskID = nullStringPtr(externalTaskID)
	run.ExternalSessionID = nullStringPtr(externalSessionID)
	run.ModelID = nullStringPtr(modelID)
	run.ErrorMessage = nullStringPtr(errMsg)
	if agentVersionID.Valid {
		v := int(agentVersionID.Int64)
		run.AgentVersionID = &v
	}
	if startedAt.Valid {
		run.StartedAt = &startedAt.Time
	}
	if finishedAt.Valid {
		run.FinishedAt = &finishedAt.Time
	}
	_ = json.Unmarshal(inputRaw, &run.InputRef)
	_ = json.Unmarshal(outputRaw, &run.OutputRef)
	if result, ok := run.OutputRef["result"].(string); ok {
		run.Result = result
	}
	if draftRaw, ok := run.OutputRef["draft"]; ok {
		if b, err := json.Marshal(draftRaw); err == nil {
			var draft model.GenerateReportDraftResponse
			if json.Unmarshal(b, &draft) == nil && draft.ReportMarkdown != "" {
				run.Draft = &draft
			}
		}
	}
	return &run, nil
}

func (h *ManagedAgentHandler) loadAIRun(runID, userID string) (*model.AIRun, error) {
	run, err := scanAIRun(h.db.QueryRow(aiRunSelectColumns+" WHERE id = $1 AND user_id = $2", runID, userID))
	if err != nil {
		return nil, err
	}
	h.hydrateReportAIRunResult(run)
	return run, nil
}

func (h *ManagedAgentHandler) hydrateReportAIRunResult(run *model.AIRun) {
	if h == nil || h.db == nil || run == nil || run.BusinessType != reportAgentRunBusinessType || run.Result != "" || run.BusinessID == nil || strings.TrimSpace(*run.BusinessID) == "" {
		return
	}
	reportType, _ := run.OutputRef["report_type"].(string)
	query := ""
	switch reportType {
	case reportTypePersonalDaily:
		query = "SELECT content FROM daily_reports WHERE id::text = $1"
	case reportTypePersonalWeekly:
		query = "SELECT content FROM personal_weekly_reports WHERE id::text = $1"
	case reportTypeTeamDaily:
		query = "SELECT content FROM team_reports WHERE id::text = $1"
	case reportTypeTeamWeekly:
		query = "SELECT content FROM team_weekly_reports WHERE id::text = $1"
	case reportTypeDepartmentDaily:
		query = "SELECT content FROM department_reports WHERE id::text = $1"
	case reportTypeDepartmentWeekly:
		query = "SELECT content FROM department_weekly_reports WHERE id::text = $1"
	default:
		return
	}
	var content string
	if err := h.db.QueryRow(query, *run.BusinessID).Scan(&content); err != nil {
		return
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}
	run.Result = content
	if run.OutputRef == nil {
		run.OutputRef = map[string]any{}
	}
	run.OutputRef["result"] = content
}

const managedAgentScheduleSelectColumns = `SELECT s.id::text, s.user_id::text, s.name, s.agent_id,
			COALESCE(s.run_kind, 'generic_agent'), s.model_id, s.message,
			COALESCE(s.start_prompt_values_json, s.params_json, '{}'::jsonb), s.params_json,
			COALESCE(s.report_config_json, '{}'::jsonb), s.schedule_type, s.weekdays_json,
			s.time_of_day, s.timezone, s.enabled, s.next_run_at, s.last_run_at,
			s.last_ai_run_id::text, ar.status, s.last_error, s.last_skip_reason,
			s.last_skip_at, s.last_skipped_trigger_at, s.created_at, s.updated_at
		FROM managed_agent_schedules s
		LEFT JOIN ai_runs ar ON ar.id = s.last_ai_run_id`

type normalizedManagedAgentScheduleRequest struct {
	Name              string
	AgentID           string
	RunKind           string
	ModelID           string
	InitialMessage    string
	StartPromptValues map[string]string
	Params            map[string]string
	ReportConfig      map[string]string
	ScheduleType      string
	Weekdays          []int
	TimeOfDay         string
	Timezone          string
	Enabled           bool
}

func normalizeManagedAgentScheduleRequest(req model.UpsertManagedAgentScheduleRequest) (normalizedManagedAgentScheduleRequest, error) {
	if req.TriggerConfig != nil {
		req.ScheduleType = req.TriggerConfig.ScheduleType
		req.Weekdays = req.TriggerConfig.Weekdays
		req.TimeOfDay = req.TriggerConfig.TimeOfDay
	}
	if req.RunConfig != nil {
		req.ModelID = req.RunConfig.ModelID
		req.InitialMessage = req.RunConfig.InitialMessage
		req.StartPromptValues = req.RunConfig.StartPromptValues
		req.ReportConfig = req.RunConfig.ReportConfig
	}
	if strings.TrimSpace(req.InitialMessage) == "" {
		req.InitialMessage = req.Message
	}
	if req.StartPromptValues == nil {
		req.StartPromptValues = req.Params
	}
	normalized := normalizedManagedAgentScheduleRequest{
		Name:              strings.TrimSpace(req.Name),
		AgentID:           strings.TrimSpace(req.AgentID),
		RunKind:           strings.TrimSpace(req.RunKind),
		ModelID:           strings.TrimSpace(req.ModelID),
		InitialMessage:    strings.TrimSpace(req.InitialMessage),
		ScheduleType:      strings.TrimSpace(req.ScheduleType),
		TimeOfDay:         strings.TrimSpace(req.TimeOfDay),
		Timezone:          strings.TrimSpace(req.Timezone),
		Enabled:           true,
		StartPromptValues: map[string]string{},
		Params:            map[string]string{},
		ReportConfig:      map[string]string{},
	}
	if req.Enabled != nil {
		normalized.Enabled = *req.Enabled
	}
	if normalized.RunKind == "" {
		normalized.RunKind = scheduleRunKindGeneric
	}
	if normalized.ScheduleType == "" {
		normalized.ScheduleType = "daily"
	}
	if normalized.Timezone == "" {
		normalized.Timezone = defaultScheduleTimezone
	}
	for key, value := range req.StartPromptValues {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		normalized.StartPromptValues[key] = strings.TrimSpace(value)
		normalized.Params[key] = strings.TrimSpace(value)
	}
	if req.ReportConfig != nil {
		normalized.ReportConfig["report_type"] = strings.TrimSpace(req.ReportConfig.ReportType)
	}

	if normalized.Name == "" {
		return normalized, fmt.Errorf("name is required")
	}
	if normalized.AgentID == "" {
		return normalized, fmt.Errorf("agent_id is required")
	}
	switch normalized.RunKind {
	case scheduleRunKindGeneric:
	case scheduleRunKindReport:
		reportType := normalized.ReportConfig["report_type"]
		if err := validateReportType(reportType); err != nil {
			return normalized, fmt.Errorf("report_type is required")
		}
		for key := range normalized.StartPromptValues {
			if isReportSystemPromptKey(key, reportMCPCredentialSlot) {
				return normalized, fmt.Errorf("%s is managed by Aida", key)
			}
		}
	default:
		return normalized, fmt.Errorf("run_kind must be generic_agent or report_agent")
	}
	if _, _, ok := parseManagedScheduleTimeOfDay(normalized.TimeOfDay); !ok {
		return normalized, fmt.Errorf("time_of_day must use HH:mm")
	}
	if _, err := time.LoadLocation(normalized.Timezone); err != nil {
		return normalized, fmt.Errorf("timezone is invalid")
	}

	switch normalized.ScheduleType {
	case "daily":
		normalized.Weekdays = []int{}
	case "weekly":
		seen := map[int]bool{}
		for _, weekday := range req.Weekdays {
			if weekday < 1 || weekday > 7 {
				return normalized, fmt.Errorf("weekdays must be between 1 and 7")
			}
			if !seen[weekday] {
				normalized.Weekdays = append(normalized.Weekdays, weekday)
				seen[weekday] = true
			}
		}
		if len(normalized.Weekdays) == 0 {
			return normalized, fmt.Errorf("weekdays is required for weekly schedules")
		}
	default:
		return normalized, fmt.Errorf("schedule_type must be daily or weekly")
	}
	return normalized, nil
}

func scanManagedAgentSchedule(row rowScanner) (model.ManagedAgentSchedule, error) {
	var schedule model.ManagedAgentSchedule
	var modelID, lastRunID, lastRunStatus, lastError, lastSkipReason sql.NullString
	var startPromptRaw, paramsRaw, reportConfigRaw, weekdaysRaw []byte
	var nextRunAt, lastRunAt, lastSkipAt, lastSkippedAt sql.NullTime
	if err := row.Scan(
		&schedule.ID, &schedule.UserID, &schedule.Name, &schedule.AgentID,
		&schedule.RunKind, &modelID, &schedule.InitialMessage, &startPromptRaw, &paramsRaw,
		&reportConfigRaw, &schedule.ScheduleType, &weekdaysRaw, &schedule.TimeOfDay,
		&schedule.Timezone, &schedule.Enabled, &nextRunAt, &lastRunAt, &lastRunID,
		&lastRunStatus, &lastError, &lastSkipReason, &lastSkipAt, &lastSkippedAt,
		&schedule.CreatedAt, &schedule.UpdatedAt,
	); err != nil {
		return schedule, err
	}
	schedule.ModelID = nullStringPtr(modelID)
	schedule.LastAIRunID = nullStringPtr(lastRunID)
	schedule.LastRunStatus = nullStringPtr(lastRunStatus)
	schedule.LastError = nullStringPtr(lastError)
	schedule.LastSkipReason = nullStringPtr(lastSkipReason)
	schedule.Message = schedule.InitialMessage
	if nextRunAt.Valid {
		schedule.NextRunAt = &nextRunAt.Time
	}
	if lastRunAt.Valid {
		schedule.LastRunAt = &lastRunAt.Time
	}
	if lastSkipAt.Valid {
		schedule.LastSkipAt = &lastSkipAt.Time
	}
	if lastSkippedAt.Valid {
		schedule.LastSkippedAt = &lastSkippedAt.Time
	}
	_ = json.Unmarshal(startPromptRaw, &schedule.StartPromptValues)
	_ = json.Unmarshal(paramsRaw, &schedule.Params)
	_ = json.Unmarshal(reportConfigRaw, &schedule.ReportConfig)
	_ = json.Unmarshal(weekdaysRaw, &schedule.Weekdays)
	if schedule.StartPromptValues == nil {
		schedule.StartPromptValues = map[string]string{}
	}
	if schedule.Params == nil {
		schedule.Params = copyStringMap(schedule.StartPromptValues)
	}
	if schedule.ReportConfig == nil {
		schedule.ReportConfig = map[string]string{}
	}
	return schedule, nil
}

func (h *ManagedAgentHandler) loadManagedAgentSchedule(scheduleID, userID string) (model.ManagedAgentSchedule, error) {
	return scanManagedAgentSchedule(h.db.QueryRow(managedAgentScheduleSelectColumns+" WHERE s.id = $1 AND s.user_id = $2", scheduleID, userID))
}

func (h *ManagedAgentHandler) validateManagedAgentScheduleConfig(ctx context.Context, u *model.User, normalized normalizedManagedAgentScheduleRequest, client *service.ManagedAgentClient) error {
	if u == nil {
		return fmt.Errorf("unauthorized")
	}
	profile, err := h.loadManagedAgentProfile(ctx, u.ID, normalized.AgentID)
	if err != nil {
		return err
	}
	profileRunKind := ""
	if profile != nil {
		profileRunKind = scheduleRunKindGeneric
		if profile.BusinessType == managedAgentBusinessReport {
			profileRunKind = scheduleRunKindReport
		}
	}
	if client != nil && client.Configured() {
		agentsResp, err := client.ListMyAgents(ctx)
		if err != nil {
			return err
		}
		var matched *model.ManagedAgent
		for idx := range agentsResp.Agents {
			if agentsResp.Agents[idx].AgentID == normalized.AgentID {
				matched = &agentsResp.Agents[idx]
				break
			}
		}
		if matched == nil {
			return fmt.Errorf("agent not found")
		}
		if matched.Archived {
			return fmt.Errorf("agent is archived")
		}
		agentRunKind := scheduleRunKindGeneric
		if profileRunKind != "" {
			agentRunKind = profileRunKind
		} else if matched.BusinessType == managedAgentBusinessReport || (matched.BusinessType == "" && len(reportTypesForAgent(*matched)) > 0) {
			agentRunKind = scheduleRunKindReport
		}
		if normalized.RunKind != agentRunKind {
			return fmt.Errorf("run_kind does not match agent type")
		}
	}
	if profileRunKind != "" && normalized.RunKind != profileRunKind {
		return fmt.Errorf("run_kind does not match agent profile")
	}
	if normalized.RunKind != scheduleRunKindReport {
		return nil
	}
	for key := range normalized.StartPromptValues {
		if isReportSystemPromptKey(key, h.defaults.ReportMCPCredentialSlot) {
			return fmt.Errorf("%s is managed by Aida", key)
		}
	}
	reportType := normalized.ReportConfig["report_type"]
	if profile != nil && profile.BusinessType == managedAgentBusinessReport && len(profile.ReportTypes) > 0 && !containsString(profile.ReportTypes, reportType) {
		return fmt.Errorf("unsupported report_type")
	}
	target, err := resolveTarget(u, reportTarget{}, reportType, true)
	if err != nil {
		return fmt.Errorf("无法推导报告对象")
	}
	if err := validateResolvedReportTarget(reportType, target); err != nil {
		return err
	}
	return nil
}

type scheduledReportPeriod struct {
	Date      string
	WeekStart string
	WeekEnd   string
	Start     string
	End       string
	Display   string
}

func reportPeriodForScheduledAt(reportType string, scheduledAt time.Time, timezone string) scheduledReportPeriod {
	loc := scheduleLocation(timezone)
	local := scheduledAt.In(loc)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	format := "2006-01-02"
	switch reportType {
	case reportTypePersonalWeekly, reportTypeTeamWeekly, reportTypeDepartmentWeekly:
		weekday := int(day.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		weekStart := day.AddDate(0, 0, -(weekday - 1))
		weekEndExclusive := weekStart.AddDate(0, 0, 7)
		weekEndDisplay := weekEndExclusive.AddDate(0, 0, -1)
		return scheduledReportPeriod{
			WeekStart: weekStart.Format(format),
			WeekEnd:   weekEndDisplay.Format(format),
			Start:     weekStart.Format(format),
			End:       weekEndExclusive.Format(format),
			Display:   weekStart.Format(format) + " ~ " + weekEndDisplay.Format(format),
		}
	default:
		nextDay := day.AddDate(0, 0, 1)
		date := day.Format(format)
		return scheduledReportPeriod{
			Date:    date,
			Start:   date,
			End:     nextDay.Format(format),
			Display: date + " 全天",
		}
	}
}

func reportTargetDisplay(reportType string) string {
	switch reportType {
	case reportTypeTeamDaily, reportTypeTeamWeekly:
		return "我所在小组"
	case reportTypeDepartmentDaily, reportTypeDepartmentWeekly:
		return "我所在部门"
	default:
		return "我自己"
	}
}

func scheduleLocation(timezone string) *time.Location {
	timezone = strings.TrimSpace(timezone)
	if timezone == "" {
		timezone = defaultScheduleTimezone
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Local
	}
	return loc
}

func computeManagedAgentNextRunAt(scheduleType string, weekdays []int, timeOfDay, timezone string, now time.Time) (time.Time, error) {
	hour, minute, ok := parseManagedScheduleTimeOfDay(timeOfDay)
	if !ok {
		return time.Time{}, fmt.Errorf("time_of_day must use HH:mm")
	}
	loc := scheduleLocation(timezone)
	localNow := now.In(loc)
	baseDay := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), hour, minute, 0, 0, loc)
	switch scheduleType {
	case "daily":
		if !localNow.After(baseDay) {
			return baseDay, nil
		}
		return baseDay.AddDate(0, 0, 1), nil
	case "weekly":
		if len(weekdays) == 0 {
			return time.Time{}, fmt.Errorf("weekdays is required for weekly schedules")
		}
		seen := map[int]bool{}
		var next time.Time
		for _, weekday := range weekdays {
			if weekday < 1 || weekday > 7 || seen[weekday] {
				continue
			}
			seen[weekday] = true
			today := int(localNow.Weekday())
			if today == 0 {
				today = 7
			}
			days := (weekday - today + 7) % 7
			candidate := baseDay.AddDate(0, 0, days)
			if localNow.After(candidate) {
				candidate = candidate.AddDate(0, 0, 7)
			}
			if next.IsZero() || candidate.Before(next) {
				next = candidate
			}
		}
		if next.IsZero() {
			return time.Time{}, fmt.Errorf("weekdays is required for weekly schedules")
		}
		return next, nil
	default:
		return time.Time{}, fmt.Errorf("schedule_type must be daily or weekly")
	}
}

func computeManagedAgentNextRunAfter(schedule model.ManagedAgentSchedule, scheduledAt time.Time) (time.Time, error) {
	return computeManagedAgentNextRunAt(schedule.ScheduleType, schedule.Weekdays, schedule.TimeOfDay, schedule.Timezone, scheduledAt.Add(time.Minute))
}

func parseManagedScheduleTimeOfDay(value string) (hour int, minute int, ok bool) {
	value = strings.TrimSpace(value)
	if len(value) != 5 || value[2] != ':' || !isManagedScheduleDigit(value[0]) || !isManagedScheduleDigit(value[1]) || !isManagedScheduleDigit(value[3]) || !isManagedScheduleDigit(value[4]) {
		return 0, 0, false
	}
	parsed, err := time.Parse("15:04", value)
	if err != nil {
		return 0, 0, false
	}
	return parsed.Hour(), parsed.Minute(), true
}

func isManagedScheduleDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}

func (h *ManagedAgentHandler) executeManagedAgentScheduleRun(ctx context.Context, schedule model.ManagedAgentSchedule, u *model.User, userToken, triggerSource string, scheduledAt time.Time, advanceNext bool) (*model.AIRun, error) {
	if u == nil {
		return nil, fmt.Errorf("unauthorized")
	}
	client, resolvedToken, err := h.clientForUser(u, userToken)
	if err != nil {
		return nil, err
	}
	if err := h.ensureScheduleAgentRunnable(ctx, client, u.ID, schedule.AgentID, schedule.RunKind); err != nil {
		inputRef := map[string]any{
			"schedule_id":          schedule.ID,
			"schedule_name":        schedule.Name,
			"trigger_source":       triggerSource,
			"scheduled_trigger_at": scheduledAt.UTC().Format(time.RFC3339),
			"model_id":             nullableScheduleModelID(schedule),
			"start_prompt_values":  copyStringMap(schedule.StartPromptValues),
			"initial_message":      schedule.InitialMessage,
		}
		businessType := scheduledAgentRunBusinessType
		insertRun := h.insertPendingAIRun
		if schedule.RunKind == scheduleRunKindReport {
			businessType = reportAgentRunBusinessType
			insertRun = h.insertPendingManagedSessionAIRun
		}
		runID, insertErr := insertRun(u.ID, businessType, schedule.AgentID, nullableScheduleModelID(schedule), inputRef)
		if insertErr != nil {
			return nil, insertErr
		}
		_ = h.markAIRunSubmitFailedContext(ctx, runID, u.ID, err.Error())
		_ = h.updateManagedScheduleAfterRun(ctx, schedule.ID, u.ID, runID, scheduledAt, err.Error(), advanceNext, schedule)
		return h.loadAIRun(runID, u.ID)
	}
	if triggerSource == "" {
		triggerSource = "manual"
	}
	modelID := ""
	if schedule.ModelID != nil {
		modelID = *schedule.ModelID
	}
	actualStartedAt := time.Now()
	inputRef := map[string]any{
		"schedule_id":          schedule.ID,
		"schedule_name":        schedule.Name,
		"trigger_source":       triggerSource,
		"scheduled_trigger_at": scheduledAt.UTC().Format(time.RFC3339),
		"actual_started_at":    actualStartedAt.UTC().Format(time.RFC3339),
		"model_id":             modelID,
		"start_prompt_values":  copyStringMap(schedule.StartPromptValues),
		"initial_message":      schedule.InitialMessage,
	}
	if schedule.RunKind == scheduleRunKindReport {
		return h.executeReportAgentScheduleRun(ctx, client, schedule, u, resolvedToken, modelID, inputRef, scheduledAt, advanceNext)
	}
	params := copyStringMap(schedule.StartPromptValues)
	if strings.TrimSpace(schedule.InitialMessage) != "" {
		params["message"] = schedule.InitialMessage
	}
	params["trigger_source"] = triggerSource
	params["schedule_id"] = schedule.ID
	agent, err := findManagedAgent(ctx, client, schedule.AgentID)
	insertRun := h.insertPendingAIRun
	if agent != nil && h.agentRequiresCredentialedSession(*agent) {
		insertRun = h.insertPendingManagedSessionAIRun
	}
	runID, insertErr := insertRun(u.ID, scheduledAgentRunBusinessType, schedule.AgentID, modelID, inputRef)
	if insertErr != nil {
		return nil, insertErr
	}
	if err != nil {
		_ = h.markAIRunSubmitFailedContext(ctx, runID, u.ID, err.Error())
		_ = h.updateManagedScheduleAfterRun(ctx, schedule.ID, u.ID, runID, scheduledAt, err.Error(), advanceNext, schedule)
		return h.loadAIRun(runID, u.ID)
	}
	if agent != nil && h.agentRequiresCredentialedSession(*agent) {
		reportSlots := map[string]struct{}{}
		for _, slot := range h.reportMCPCredentialSlots(*agent) {
			reportSlots[slot] = struct{}{}
		}
		missing := h.missingRequiredCredentialSlots(*agent, reportSlots, nil)
		if len(missing) > 0 {
			errMsg := "required credential slots are not bound to usable default credentials: " + strings.Join(missing, ",")
			_ = h.markAIRunSubmitFailedContext(ctx, runID, u.ID, errMsg)
			_ = h.updateManagedScheduleAfterRun(ctx, schedule.ID, u.ID, runID, scheduledAt, errMsg, advanceNext, schedule)
			return h.loadAIRun(runID, u.ID)
		}
		overrides := map[string]string{}
		if len(reportSlots) > 0 {
			if strings.TrimSpace(resolvedToken) == "" {
				errMsg := "current user token is required"
				_ = h.markAIRunSubmitFailedContext(ctx, runID, u.ID, errMsg)
				_ = h.updateManagedScheduleAfterRun(ctx, schedule.ID, u.ID, runID, scheduledAt, errMsg, advanceNext, schedule)
				return h.loadAIRun(runID, u.ID)
			}
			inputRef["credential_slots"] = sortedStringKeys(reportSlots)
			inputRef["credential_override"] = "redacted"
			credential, err := client.CreateCredential(ctx, service.CreateManagedCredentialRequest{
				Name:  "Aida Report MCP Auth " + runID,
				Kind:  "secret",
				Value: resolvedToken,
				Metadata: map[string]string{
					"aida_user_id": u.ID,
					"ai_run_id":    runID,
					"purpose":      "scheduled_generic_agent_report_mcp_auth",
				},
			})
			if err != nil {
				_ = h.markAIRunSubmitFailedContext(ctx, runID, u.ID, err.Error())
				_ = h.updateManagedScheduleAfterRun(ctx, schedule.ID, u.ID, runID, scheduledAt, err.Error(), advanceNext, schedule)
				return h.loadAIRun(runID, u.ID)
			}
			for slot := range reportSlots {
				overrides[slot] = credential.CredentialID
			}
		}
		sessionReq := service.CreateManagedSessionRequest{
			AgentID:           schedule.AgentID,
			ModelID:           modelID,
			StartPromptValues: params,
		}
		if len(overrides) > 0 {
			sessionReq.CredentialOverrides = overrides
		}
		sessionResp, sessionErr := client.CreateSession(ctx, sessionReq)
		if sessionErr != nil {
			_ = h.markAIRunSubmitFailedContext(ctx, runID, u.ID, sessionErr.Error())
			_ = h.updateManagedScheduleAfterRun(ctx, schedule.ID, u.ID, runID, scheduledAt, sessionErr.Error(), advanceNext, schedule)
			return h.loadAIRun(runID, u.ID)
		}
		inputRef["external_session_id"] = sessionResp.SessionID
		inputRef["external_status"] = sessionResp.Status
		if sessionResp.ModelID != "" {
			modelID = sessionResp.ModelID
			inputRef["model_id"] = modelID
		}
		if err := h.attachSessionAIRun(runID, u.ID, sessionResp, modelID, inputRef); err != nil {
			return nil, err
		}
		_ = h.updateManagedScheduleAfterRun(ctx, schedule.ID, u.ID, runID, scheduledAt, "", advanceNext, schedule)
		return h.loadAIRun(runID, u.ID)
	}
	submitResp, submitErr := client.SubmitTask(ctx, service.SubmitManagedTaskRequest{
		AgentID: schedule.AgentID,
		ModelID: modelID,
		Params:  params,
	})
	if submitErr != nil {
		_ = h.markAIRunSubmitFailedContext(ctx, runID, u.ID, submitErr.Error())
		_ = h.updateManagedScheduleAfterRun(ctx, schedule.ID, u.ID, runID, scheduledAt, submitErr.Error(), advanceNext, schedule)
		return h.loadAIRun(runID, u.ID)
	}
	inputRef["external_task_id"] = submitResp.TaskID
	inputRef["external_status"] = submitResp.Status
	if submitResp.ModelID != "" {
		modelID = submitResp.ModelID
		inputRef["model_id"] = modelID
	}
	if err := h.attachSubmittedAIRun(runID, u.ID, submitResp, modelID, inputRef); err != nil {
		return nil, err
	}
	_ = h.updateManagedScheduleAfterRun(ctx, schedule.ID, u.ID, runID, scheduledAt, "", advanceNext, schedule)
	return h.loadAIRun(runID, u.ID)
}

func nullableScheduleModelID(schedule model.ManagedAgentSchedule) string {
	if schedule.ModelID == nil {
		return ""
	}
	return *schedule.ModelID
}

func (h *ManagedAgentHandler) ensureScheduleAgentRunnable(ctx context.Context, client *service.ManagedAgentClient, userID, agentID, runKind string) error {
	profileRunKind := ""
	profile, err := h.loadManagedAgentProfile(ctx, userID, agentID)
	if err != nil {
		return err
	}
	if profile != nil {
		profileRunKind = scheduleRunKindGeneric
		if profile.BusinessType == managedAgentBusinessReport {
			profileRunKind = scheduleRunKindReport
		}
	}
	if client == nil || !client.Configured() {
		if profileRunKind != "" && runKind != "" && runKind != profileRunKind {
			return fmt.Errorf("run_kind does not match agent profile")
		}
		return nil
	}
	resp, err := client.ListMyAgents(ctx)
	if err != nil {
		return err
	}
	for _, agent := range resp.Agents {
		if agent.AgentID != agentID {
			continue
		}
		if agent.Archived {
			return fmt.Errorf("agent is archived")
		}
		agentRunKind := scheduleRunKindGeneric
		if profileRunKind != "" {
			agentRunKind = profileRunKind
		} else if agent.BusinessType == managedAgentBusinessReport || (agent.BusinessType == "" && len(reportTypesForAgent(agent)) > 0) {
			agentRunKind = scheduleRunKindReport
		}
		if runKind != "" && runKind != agentRunKind {
			return fmt.Errorf("run_kind does not match agent type")
		}
		if agentRunKind == scheduleRunKindReport {
			repairDefault := profile != nil && profile.IsDefaultReport || profile == nil && isMarkedDefaultReportAgent(agent)
			if err := h.resolveAndRepairReportAgent(ctx, client, &agent, repairDefault); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("agent not found")
}

func (h *ManagedAgentHandler) executeReportAgentScheduleRun(ctx context.Context, client *service.ManagedAgentClient, schedule model.ManagedAgentSchedule, u *model.User, userToken, modelID string, inputRef map[string]any, scheduledAt time.Time, advanceNext bool) (*model.AIRun, error) {
	if h.defaults.AIDAPublicBaseURL == "" {
		return nil, fmt.Errorf("AIDA_PUBLIC_BASE_URL is required for Report Agent")
	}
	reportType := strings.TrimSpace(schedule.ReportConfig["report_type"])
	if err := validateReportType(reportType); err != nil {
		return nil, err
	}
	target, err := resolveTarget(u, reportTarget{}, reportType, true)
	if err != nil {
		return nil, err
	}
	if err := validateResolvedReportTarget(reportType, target); err != nil {
		return nil, err
	}
	period := reportPeriodForScheduledAt(reportType, scheduledAt, schedule.Timezone)
	periodRef := reportPeriodInputRef(reportType, period.Date, period.WeekStart, period.WeekEnd)
	inputRef["report_type"] = reportType
	inputRef["target"] = target
	inputRef["period"] = periodRef
	inputRef["period_start"] = period.Start
	inputRef["period_end"] = period.End
	inputRef["period_display"] = period.Display
	inputRef["mcp_server"] = h.defaults.ReportMCPSlug
	inputRef["credential_slot"] = h.defaults.ReportMCPCredentialSlot
	reportSourceSelectionID := ""
	var runID string
	isPersonalReport := reportType == reportTypePersonalDaily || reportType == reportTypePersonalWeekly
	if isPersonalReport && h.reportSource != nil {
		selectionPeriod, periodErr := reportsource.ReportPeriod(reportType, period.Date, period.WeekStart, period.WeekEnd)
		if periodErr != nil {
			return nil, periodErr
		}
		var attached reportsource.Selection
		runID, attached, err = h.reportSource.CreateAttachedRun(
			ctx, u.ID, reportType, selectionPeriod, "", reportAgentRunBusinessType,
			schedule.AgentID, modelID, true, inputRef,
		)
		if err != nil {
			return nil, err
		}
		reportSourceSelectionID = attached.ID
	} else {
		runID, err = h.insertPendingManagedSessionAIRun(u.ID, reportAgentRunBusinessType, schedule.AgentID, modelID, inputRef)
	}
	if err != nil {
		return nil, err
	}
	credential, err := client.CreateCredential(ctx, service.CreateManagedCredentialRequest{
		Name:  "Aida Report MCP Auth " + runID,
		Kind:  "secret",
		Value: userToken,
		Metadata: map[string]string{
			"aida_user_id": u.ID,
			"ai_run_id":    runID,
			"purpose":      "report_mcp_auth",
		},
	})
	if err != nil {
		_ = h.markAIRunSubmitFailedContext(ctx, runID, u.ID, err.Error())
		_ = h.updateManagedScheduleAfterRun(ctx, schedule.ID, u.ID, runID, scheduledAt, err.Error(), advanceNext, schedule)
		return h.loadAIRun(runID, u.ID)
	}
	systemPromptValues := reportAgentStartPromptValues(runID, reportType, period.Date, period.WeekStart, period.WeekEnd, target, nil, reportSourceSelectionID, h.reportMCPURL())
	userMessage := strings.TrimSpace(schedule.InitialMessage)
	if userMessage == "" {
		userMessage = fallbackReportRunMessage(reportType, period.Date, period.WeekStart, period.WeekEnd, target)
	}
	startPromptValues, reservedKey, ok := mergeReportStartPromptValues(systemPromptValues, schedule.StartPromptValues, userMessage, h.defaults.ReportMCPCredentialSlot)
	if !ok {
		err := fmt.Errorf("%s is managed by Aida", reservedKey)
		_ = h.markAIRunSubmitFailedContext(ctx, runID, u.ID, err.Error())
		_ = h.updateManagedScheduleAfterRun(ctx, schedule.ID, u.ID, runID, scheduledAt, err.Error(), advanceNext, schedule)
		return h.loadAIRun(runID, u.ID)
	}
	sessionMessage := buildReportRunMessage(startPromptValues, userMessage, h.defaults.ReportMCPCredentialSlot)
	sessionResp, err := client.CreateSession(ctx, service.CreateManagedSessionRequest{
		AgentID:           schedule.AgentID,
		ModelID:           modelID,
		StartPromptValues: startPromptValues,
		Message:           sessionMessage,
		CredentialOverrides: map[string]string{
			h.defaults.ReportMCPCredentialSlot: credential.CredentialID,
		},
	})
	if err != nil {
		_ = h.markAIRunSubmitFailedContext(ctx, runID, u.ID, err.Error())
		_ = h.updateManagedScheduleAfterRun(ctx, schedule.ID, u.ID, runID, scheduledAt, err.Error(), advanceNext, schedule)
		return h.loadAIRun(runID, u.ID)
	}
	if modelID == "" && sessionResp.ModelID != "" {
		modelID = sessionResp.ModelID
	}
	inputRef["start_prompt_values"] = copyStringMap(startPromptValues)
	inputRef["message"] = sessionMessage
	inputRef["credential_override"] = "redacted"
	inputRef["external_session_id"] = sessionResp.SessionID
	inputRef["external_status"] = sessionResp.Status
	if err := h.attachSessionAIRun(runID, u.ID, sessionResp, modelID, inputRef); err != nil {
		return nil, err
	}
	_ = h.updateManagedScheduleAfterRun(ctx, schedule.ID, u.ID, runID, scheduledAt, "", advanceNext, schedule)
	return h.loadAIRun(runID, u.ID)
}

func (h *ManagedAgentHandler) markAIRunSubmitFailedContext(ctx context.Context, runID, userID, message string) error {
	_, err := h.db.ExecContext(ctx, `
		UPDATE ai_runs
		SET status = 'failed', error_message = $1, finished_at = now()
		WHERE id = $2 AND user_id = $3`,
		message, runID, userID,
	)
	return err
}

func (h *ManagedAgentHandler) updateManagedScheduleAfterRun(ctx context.Context, scheduleID, userID, runID string, scheduledAt time.Time, errorMessage string, advanceNext bool, schedule model.ManagedAgentSchedule) error {
	var nextRun any
	if advanceNext {
		next, err := computeManagedAgentNextRunAfter(schedule, scheduledAt)
		if err != nil {
			nextRun = nil
		} else {
			nextRun = next
		}
		_, err = h.db.ExecContext(ctx, `
			UPDATE managed_agent_schedules
			SET last_run_at = $1, last_ai_run_id = $2, last_error = NULLIF($3, ''),
			    next_run_at = $4, updated_at = now()
			WHERE id = $5 AND user_id = $6`,
			scheduledAt, runID, errorMessage, nextRun, scheduleID, userID,
		)
		return err
	}
	_, err := h.db.ExecContext(ctx, `
		UPDATE managed_agent_schedules
		SET last_run_at = $1, last_ai_run_id = $2, last_error = NULLIF($3, ''), updated_at = now()
		WHERE id = $4 AND user_id = $5`,
		scheduledAt, runID, errorMessage, scheduleID, userID,
	)
	return err
}

func (h *ManagedAgentHandler) updateManagedScheduleLastError(ctx context.Context, scheduleID, userID, runID string, scheduledAt time.Time, errorMessage string) error {
	_, err := h.db.ExecContext(ctx, `
		UPDATE managed_agent_schedules
		SET last_run_at = $1, last_ai_run_id = $2, last_error = $3, updated_at = now()
		WHERE id = $4 AND user_id = $5`,
		scheduledAt, runID, errorMessage, scheduleID, userID,
	)
	return err
}

type ManagedAgentScheduleRunner struct {
	handler  *ManagedAgentHandler
	interval time.Duration
}

func NewManagedAgentScheduleRunner(h *ManagedAgentHandler) *ManagedAgentScheduleRunner {
	return &ManagedAgentScheduleRunner{handler: h, interval: time.Minute}
}

func (r *ManagedAgentScheduleRunner) Start(ctx context.Context) {
	if r == nil || r.handler == nil || r.handler.db == nil || r.handler.client == nil || !r.handler.client.Configured() {
		return
	}
	go func() {
		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				if err := r.RunDue(ctx, now); err != nil {
					fmt.Printf("managed agent schedule runner failed: %v\n", err)
				}
			}
		}
	}()
}

func (r *ManagedAgentScheduleRunner) RunDue(ctx context.Context, now time.Time) error {
	rows, err := r.handler.db.QueryContext(ctx, managedAgentScheduleSelectColumns+`
		WHERE s.enabled = true
			AND s.next_run_at IS NOT NULL
			AND s.next_run_at <= $1
		ORDER BY s.next_run_at ASC
		LIMIT 50`, now)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		schedule, err := scanManagedAgentSchedule(rows)
		if err != nil {
			return err
		}
		if schedule.NextRunAt == nil {
			continue
		}
		if blocked, err := r.handler.skipOrTimeoutActiveScheduleRun(ctx, schedule, *schedule.NextRunAt, now); err != nil {
			return err
		} else if blocked {
			continue
		}
		scheduledAt := *schedule.NextRunAt
		nextRunAt, err := computeManagedAgentNextRunAfter(schedule, scheduledAt)
		if err != nil {
			_, _ = r.handler.db.ExecContext(ctx, `
				UPDATE managed_agent_schedules
				SET last_error = $1, updated_at = now()
				WHERE id = $2 AND user_id = $3`, err.Error(), schedule.ID, schedule.UserID)
			continue
		}
		if _, err := r.handler.db.ExecContext(ctx, `
			UPDATE managed_agent_schedules
			SET next_run_at = $1, updated_at = now()
			WHERE id = $2 AND user_id = $3 AND enabled = true`, nextRunAt, schedule.ID, schedule.UserID); err != nil {
			return err
		}
		user, err := loadAidaUserByID(r.handler.db, schedule.UserID)
		if err != nil {
			_, _ = r.handler.db.ExecContext(ctx, `
				UPDATE managed_agent_schedules
				SET last_error = $1, updated_at = now()
				WHERE id = $2 AND user_id = $3`, err.Error(), schedule.ID, schedule.UserID)
			continue
		}
		if _, err := r.handler.executeManagedAgentScheduleRun(ctx, schedule, user, "", "scheduled", scheduledAt, false); err != nil {
			_, _ = r.handler.db.ExecContext(ctx, `
				UPDATE managed_agent_schedules
				SET last_error = $1, updated_at = now()
				WHERE id = $2 AND user_id = $3`, err.Error(), schedule.ID, schedule.UserID)
		}
	}
	return rows.Err()
}

func (h *ManagedAgentHandler) skipOrTimeoutActiveScheduleRun(ctx context.Context, schedule model.ManagedAgentSchedule, scheduledAt, now time.Time) (bool, error) {
	rows, err := h.db.QueryContext(ctx, `
		SELECT id::text, status, COALESCE(started_at, created_at), external_task_id, external_session_id
		FROM ai_runs
		WHERE input_ref_json->>'schedule_id' = $1
			AND status IN ('pending', 'running')
		ORDER BY created_at ASC`, schedule.ID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	blocked := false
	for rows.Next() {
		var runID, status string
		var startedAt time.Time
		var externalTaskID, externalSessionID sql.NullString
		if err := rows.Scan(&runID, &status, &startedAt, &externalTaskID, &externalSessionID); err != nil {
			return false, err
		}
		timedOut := false
		if status == "pending" && !externalTaskID.Valid && !externalSessionID.Valid && !now.Before(startedAt.Add(10*time.Minute)) {
			timedOut = true
		}
		runTimeout := service.ManagedAgentRunTimeout
		if externalSessionID.Valid && strings.TrimSpace(externalSessionID.String) != "" {
			runTimeout = service.ManagedAgentSessionTimeout
		}
		if status == "running" && !now.Before(startedAt.Add(runTimeout)) {
			timedOut = true
		}
		if timedOut {
			_, _ = h.db.ExecContext(ctx, `
				UPDATE ai_runs
				SET status = 'timeout', error_message = COALESCE(error_message, 'schedule active run timed out'), finished_at = now()
				WHERE id = $1`, runID)
			continue
		}
		blocked = true
	}
	if blocked {
		_, err := h.db.ExecContext(ctx, `
			UPDATE managed_agent_schedules
			SET last_skip_reason = $1, last_skip_at = $2, last_skipped_trigger_at = $3, updated_at = now()
			WHERE id = $4 AND user_id = $5`,
			"上一轮运行尚未结束", now, scheduledAt, schedule.ID, schedule.UserID,
		)
		return true, err
	}
	return false, rows.Err()
}

func normalizeManagedRunStatus(status string) string {
	switch strings.ToLower(status) {
	case "completed", "complete", "done", "success", "succeeded":
		return "succeeded"
	case "failed", "error", "cancelled", "canceled":
		return "failed"
	case "timeout", "timed_out":
		return "timeout"
	case "running", "in_progress", "processing", "queued", "submitted", "pending", "created", "active":
		return "running"
	default:
		return "pending"
	}
}

func isTerminalManagedStatus(status string) bool {
	return status == "succeeded" || status == "failed" || status == "timeout"
}

func nullableString(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func nullableInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

func absoluteRequestURL(r *http.Request, path string) string {
	proto := r.Header.Get("X-Forwarded-Proto")
	if proto == "" {
		proto = "http"
		if r.TLS != nil {
			proto = "https"
		}
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	return proto + "://" + host + path
}

func dailyReportSessionLogURLs(r *http.Request, sessions []model.ReportDraftSession) []string {
	urls := make([]string, 0, len(sessions))
	for _, session := range sessions {
		urls = append(urls, absoluteRequestURL(r, "/api/v1/sessions/"+url.PathEscape(session.ID)+"/log"))
	}
	return urls
}
