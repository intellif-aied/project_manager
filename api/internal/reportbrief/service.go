package reportbrief

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/aidashboard/api/internal/reportcontext"
)

const personalDaily = "personal_daily"

type contextReader interface {
	Get(context.Context, string, string) (reportcontext.StoredContext, error)
}

type Service struct {
	db      *sql.DB
	context contextReader
}

func NewService(db *sql.DB, reportContext *reportcontext.Service) *Service {
	return &Service{db: db, context: reportContext}
}

type runRecord struct {
	BusinessType   string
	Status         string
	Stage          string
	ModelID        string
	Representation string
}

type contextEnvelope struct {
	Run struct {
		ReportType string `json:"report_type"`
		Period     struct {
			Start string `json:"start"`
			End   string `json:"end"`
		} `json:"period"`
	} `json:"run"`
	WorkEvidence *struct {
		Facts []struct {
			FactRef string `json:"fact_ref"`
		} `json:"facts"`
	} `json:"work_evidence"`
}

func (s *Service) Accept(ctx context.Context, userID, runID string, draft Draft) (Stored, error) {
	if s == nil || s.db == nil || s.context == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(runID) == "" {
		return Stored{}, ErrInvalid
	}
	run, err := s.loadRun(ctx, userID, runID)
	if err != nil {
		return Stored{}, err
	}
	if run.BusinessType != "report_agent_run" || run.Status != "running" || run.Stage != "agent_running" ||
		run.Representation != reportcontext.RepresentationWorkEvidence {
		return Stored{}, ErrRunNotWritable
	}
	storedContext, err := s.context.Get(ctx, userID, runID)
	if errors.Is(err, reportcontext.ErrNotFound) {
		return Stored{}, ErrNotFound
	}
	if err != nil {
		return Stored{}, err
	}
	var envelope contextEnvelope
	if err := json.Unmarshal(storedContext.Payload, &envelope); err != nil || envelope.WorkEvidence == nil ||
		envelope.Run.ReportType != personalDaily || envelope.Run.Period.Start == "" || envelope.Run.Period.End == "" {
		return Stored{}, fmt.Errorf("%w: personal daily work evidence context is required", ErrInvalid)
	}
	factRefs := make(map[string]struct{}, len(envelope.WorkEvidence.Facts))
	for _, fact := range envelope.WorkEvidence.Facts {
		ref := strings.TrimSpace(fact.FactRef)
		if !factRefPattern.MatchString(ref) {
			return Stored{}, fmt.Errorf("%w: context contains an invalid fact_ref", ErrInvalid)
		}
		if _, exists := factRefs[ref]; exists {
			return Stored{}, fmt.Errorf("%w: context contains duplicate fact_ref %s", ErrInvalid, ref)
		}
		factRefs[ref] = struct{}{}
	}
	payload, err := normalizeDraft(draft, envelope.Run.ReportType, Period{
		Start: envelope.Run.Period.Start, End: envelope.Run.Period.End,
	}, factRefs)
	if err != nil {
		return s.rejectInvalidBrief(ctx, userID, runID, err)
	}
	encoded, err := json.Marshal(payload)
	if err != nil || len(encoded) > MaxPayloadBytes {
		return s.rejectInvalidBrief(ctx, userID, runID,
			fmt.Errorf("%w: normalized payload exceeds %d bytes", ErrInvalid, MaxPayloadBytes))
	}
	sum := sha256.Sum256(encoded)
	briefHash := hex.EncodeToString(sum[:])
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO report_run_briefs (
			run_id, schema_version, context_hash, brief_hash, brief_payload, model_id
		) VALUES ($1::uuid, $2, $3, $4, $5::jsonb, NULLIF($6, ''))
		ON CONFLICT (run_id) DO NOTHING`,
		runID, SchemaVersion, storedContext.Hash, briefHash, encoded, run.ModelID,
	)
	if err != nil {
		return Stored{}, err
	}
	written, err := result.RowsAffected()
	if err != nil {
		return Stored{}, err
	}
	if written == 0 {
		existing, err := s.loadStored(ctx, userID, runID)
		if err != nil {
			return Stored{}, err
		}
		if existing.BriefHash != briefHash || existing.ContextHash != storedContext.Hash {
			return Stored{}, ErrConflict
		}
		return existing, nil
	}
	return Stored{Payload: payload, BriefHash: briefHash, ContextHash: storedContext.Hash}, nil
}

func (s *Service) ValidateForWrite(ctx context.Context, userID, runID, briefHash, summary, content string) (Stored, error) {
	if s == nil || s.db == nil || strings.TrimSpace(briefHash) == "" {
		return Stored{}, ErrNotFound
	}
	stored, err := s.loadStored(ctx, userID, runID)
	if err != nil {
		return Stored{}, err
	}
	if stored.BriefHash != strings.TrimSpace(briefHash) {
		return Stored{}, ErrMismatch
	}
	var contextHash string
	err = s.db.QueryRowContext(ctx, `
		SELECT c.context_hash
		FROM report_run_contexts c
		JOIN ai_runs r ON r.id = c.run_id
		WHERE c.run_id = $1::uuid AND r.user_id = $2 AND r.business_type = 'report_agent_run'`,
		runID, userID,
	).Scan(&contextHash)
	if errors.Is(err, sql.ErrNoRows) {
		return Stored{}, ErrNotFound
	}
	if err != nil {
		return Stored{}, err
	}
	if contextHash != stored.ContextHash {
		return Stored{}, ErrMismatch
	}
	issues := make([]string, 0, 8)
	appendValidationIssues(&issues, validateTextIssues("summary", normalizeText(summary), 0, MaxPayloadBytes))
	appendValidationIssues(&issues, validateTextIssues("content", normalizeText(content), 0, MaxPayloadBytes))
	if len(issues) > 0 {
		validationErr := invalidError(ErrResultInvalid, issues)
		attempts, attemptErr := s.recordInvalidAttempt(ctx, userID, runID, "result")
		if attemptErr != nil {
			return Stored{}, attemptErr
		}
		if attempts > MaxResultInvalidAttempts {
			return Stored{}, fmt.Errorf("%w: %s", ErrResultRetryExhausted, errorDetails(validationErr, ErrResultInvalid))
		}
		return Stored{}, validationErr
	}
	return stored, nil
}

func (s *Service) rejectInvalidBrief(ctx context.Context, userID, runID string, validationErr error) (Stored, error) {
	if !errors.Is(validationErr, ErrInvalid) {
		return Stored{}, validationErr
	}
	attempts, err := s.recordInvalidAttempt(ctx, userID, runID, "brief")
	if err != nil {
		return Stored{}, err
	}
	if attempts > MaxBriefInvalidAttempts {
		return Stored{}, fmt.Errorf("%w: %s", ErrBriefRetryExhausted, errorDetails(validationErr, ErrInvalid))
	}
	return Stored{}, validationErr
}

func (s *Service) recordInvalidAttempt(ctx context.Context, userID, runID, stage string) (int, error) {
	column := "brief_invalid_attempts"
	otherColumn := "result_invalid_attempts"
	capValue := MaxBriefInvalidAttempts + 1
	if stage == "result" {
		column, otherColumn = otherColumn, column
		capValue = MaxResultInvalidAttempts + 1
	}
	query := fmt.Sprintf(`
		INSERT INTO report_run_generation_attempts (run_id, %s, %s)
		SELECT r.id, 1, 0
		FROM ai_runs r
		WHERE r.id = $1::uuid AND r.user_id = $2 AND r.business_type = 'report_agent_run'
		ON CONFLICT (run_id) DO UPDATE
		SET %s = LEAST(report_run_generation_attempts.%s + 1, $3), updated_at = now()
		RETURNING %s`, column, otherColumn, column, column, column)
	var attempts int
	err := s.db.QueryRowContext(ctx, query, runID, userID, capValue).Scan(&attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return attempts, err
}

func errorDetails(err, sentinel error) string {
	details := strings.TrimSpace(err.Error())
	return strings.TrimPrefix(details, sentinel.Error()+": ")
}

func (s *Service) loadRun(ctx context.Context, userID, runID string) (runRecord, error) {
	var run runRecord
	err := s.db.QueryRowContext(ctx, `
		SELECT business_type, status, COALESCE(execution_stage, ''), COALESCE(model_id, ''),
		       COALESCE(execution_input_json->>'report_context_representation', '')
		FROM ai_runs WHERE id = $1::uuid AND user_id = $2`, runID, userID,
	).Scan(&run.BusinessType, &run.Status, &run.Stage, &run.ModelID, &run.Representation)
	if errors.Is(err, sql.ErrNoRows) {
		return runRecord{}, ErrNotFound
	}
	return run, err
}

func (s *Service) loadStored(ctx context.Context, userID, runID string) (Stored, error) {
	var schemaVersion, contextHash, briefHash string
	var raw []byte
	err := s.db.QueryRowContext(ctx, `
		SELECT b.schema_version, b.context_hash, b.brief_hash, b.brief_payload
		FROM report_run_briefs b
		JOIN ai_runs r ON r.id = b.run_id
		WHERE b.run_id = $1::uuid AND r.user_id = $2 AND r.business_type = 'report_agent_run'`,
		runID, userID,
	).Scan(&schemaVersion, &contextHash, &briefHash, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return Stored{}, ErrNotFound
	}
	if err != nil {
		return Stored{}, err
	}
	if schemaVersion != SchemaVersion {
		return Stored{}, ErrMismatch
	}
	var payload Payload
	if err := json.Unmarshal(raw, &payload); err != nil || payload.SchemaVersion != SchemaVersion {
		return Stored{}, ErrMismatch
	}
	return Stored{Payload: payload, BriefHash: briefHash, ContextHash: contextHash}, nil
}

var (
	factRefPattern          = regexp.MustCompile(FactRefJSONPattern)
	uuidPattern             = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\b`)
	privateIPPattern        = regexp.MustCompile(`\b(?:10\.|192\.168\.|172\.(?:1[6-9]|2[0-9]|3[01])\.)[0-9]{1,3}\.[0-9]{1,3}\b`)
	longHexPattern          = regexp.MustCompile(`(?i)\b[0-9a-f]{7,64}\b`)
	rawCodePattern          = regexp.MustCompile(`\bREPORT_[A-Z0-9_]+\b`)
	pascalIdentifierPattern = regexp.MustCompile(`\b[A-Z][a-z0-9]+(?:[A-Z][A-Za-z0-9]*)+\b`)
	absUnixPathPattern      = regexp.MustCompile(`(?:^|\s)/(?:home|tmp|var|etc|opt|usr)/[^\s]+`)
	absWindowsPathPattern   = regexp.MustCompile(`(?i)\b[A-Z]:\\[^\s]+`)
	internalRoutePattern    = regexp.MustCompile(`(?:^|[\s(\x60])/[A-Za-z][A-Za-z0-9_-]*(?:/[A-Za-z0-9_.{}:-]+)*`)
	networkPortPattern      = regexp.MustCompile(`(?i)(?:udp|tcp|port|端口)\s*[:：]?\s*[0-9]{2,5}\b`)
	commandFlagPattern      = regexp.MustCompile(`(?:^|\s)--[A-Za-z][A-Za-z0-9-]*\b`)
)

var validStates = valueSet(ValidStates())
var validEnvironments = valueSet(ValidEnvironments())
var validExclusionReasons = valueSet(ValidExclusionReasons())

func valueSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func normalizeDraft(draft Draft, reportType string, period Period, available map[string]struct{}) (Payload, error) {
	issues := make([]string, 0, 16)
	if len(draft.Workstreams) > MaxWorkstreams || len(draft.ExcludedFacts) > len(available) {
		appendValidationIssue(&issues, "workstream or excluded fact count exceeds the limit")
	}
	if len(available) == 0 && !draft.NoReportableWork {
		appendValidationIssue(&issues, "no_reportable_work must be true when Context has no facts")
	}
	if draft.NoReportableWork && len(draft.Workstreams) != 0 {
		appendValidationIssue(&issues, "no_reportable_work requires empty workstreams")
	}
	if !draft.NoReportableWork && len(draft.Workstreams) == 0 && len(available) > 0 {
		appendValidationIssue(&issues, "workstreams are required when reportable work exists")
	}
	payload := Payload{
		SchemaVersion: SchemaVersion, ReportType: reportType, Period: period,
		Workstreams:      make([]Workstream, 0, len(draft.Workstreams)),
		ExcludedFacts:    make([]ExcludedFact, 0, len(draft.ExcludedFacts)),
		NoReportableWork: draft.NoReportableWork,
	}
	included := map[string]struct{}{}
	workstreamIndexes := map[string]int{}
	for workstreamIndex, rawWorkstream := range draft.Workstreams {
		workstream := Workstream{
			Title: normalizeText(rawWorkstream.Title), Objective: normalizeText(rawWorkstream.Objective),
			Deliverables: make([]Deliverable, 0, len(rawWorkstream.Deliverables)),
		}
		appendValidationIssues(&issues, validateTextIssues(fmt.Sprintf("workstreams[%d].title", workstreamIndex), workstream.Title, 1, 80))
		appendValidationIssues(&issues, validateTextIssues(fmt.Sprintf("workstreams[%d].objective", workstreamIndex), workstream.Objective, 1, 240))
		if len(rawWorkstream.Deliverables) == 0 || len(rawWorkstream.Deliverables) > MaxDeliverables {
			appendValidationIssue(&issues, fmt.Sprintf("workstreams[%d] requires 1 to 8 deliverables", workstreamIndex))
		}
		for deliverableIndex, rawDeliverable := range rawWorkstream.Deliverables {
			path := fmt.Sprintf("workstreams[%d].deliverables[%d]", workstreamIndex, deliverableIndex)
			deliverable := Deliverable{
				Result: normalizeText(rawDeliverable.Result), State: strings.TrimSpace(rawDeliverable.State),
				Environment: strings.TrimSpace(rawDeliverable.Environment),
				Validation:  normalizeText(rawDeliverable.Validation), NextAction: normalizeText(rawDeliverable.NextAction),
			}
			appendValidationIssues(&issues, validateTextIssues(path+".result", deliverable.Result, 1, 500))
			appendValidationIssues(&issues, validateTextIssues(path+".validation", deliverable.Validation, 1, 320))
			appendValidationIssues(&issues, validateTextIssues(path+".next_action", deliverable.NextAction, 1, 240))
			if _, ok := validStates[deliverable.State]; !ok {
				appendValidationIssue(&issues, path+".state is invalid")
			}
			if _, ok := validEnvironments[deliverable.Environment]; !ok {
				appendValidationIssue(&issues, path+".environment is invalid")
			}
			if deliverable.State == "released" && deliverable.Environment != "production" {
				appendValidationIssue(&issues, path+": released requires production environment")
			}
			if deliverable.State == "validated" && deliverable.Environment != "test" {
				appendValidationIssue(&issues, path+": validated requires test environment")
			}
			deliverable.FactRefs = normalizeFactRefs(rawDeliverable.FactRefs)
			if len(deliverable.FactRefs) == 0 {
				appendValidationIssue(&issues, path+".fact_refs is required")
			}
			for _, ref := range deliverable.FactRefs {
				if _, ok := available[ref]; !ok {
					appendValidationIssue(&issues, "unknown fact_ref "+ref)
					continue
				}
				included[ref] = struct{}{}
			}
			workstream.Deliverables = append(workstream.Deliverables, deliverable)
		}
		key := workstream.Title + "\x00" + workstream.Objective
		if existingIndex, exists := workstreamIndexes[key]; exists {
			combined := append(payload.Workstreams[existingIndex].Deliverables, workstream.Deliverables...)
			if len(combined) > MaxDeliverables {
				appendValidationIssue(&issues, "merged workstream exceeds 8 deliverables")
			}
			payload.Workstreams[existingIndex].Deliverables = combined
			continue
		}
		workstreamIndexes[key] = len(payload.Workstreams)
		payload.Workstreams = append(payload.Workstreams, workstream)
	}
	excluded := map[string]struct{}{}
	for _, rawExcluded := range draft.ExcludedFacts {
		item := ExcludedFact{FactRef: strings.TrimSpace(rawExcluded.FactRef), Reason: strings.TrimSpace(rawExcluded.Reason)}
		if _, ok := available[item.FactRef]; !ok {
			appendValidationIssue(&issues, "unknown excluded fact_ref "+item.FactRef)
			continue
		}
		if _, ok := validExclusionReasons[item.Reason]; !ok {
			appendValidationIssue(&issues, "excluded fact reason is invalid for "+item.FactRef)
		}
		if _, ok := included[item.FactRef]; ok {
			appendValidationIssue(&issues, "fact_ref "+item.FactRef+" cannot be included and excluded")
		}
		if _, exists := excluded[item.FactRef]; exists {
			continue
		}
		excluded[item.FactRef] = struct{}{}
		payload.ExcludedFacts = append(payload.ExcludedFacts, item)
	}
	for ref := range available {
		if _, ok := included[ref]; ok {
			continue
		}
		if _, ok := excluded[ref]; !ok {
			appendValidationIssue(&issues, "fact_ref "+ref+" is not accounted for")
		}
	}
	sort.SliceStable(payload.ExcludedFacts, func(i, j int) bool {
		return payload.ExcludedFacts[i].FactRef < payload.ExcludedFacts[j].FactRef
	})
	if len(issues) > 0 {
		return Payload{}, invalidError(ErrInvalid, issues)
	}
	return payload, nil
}

func normalizeFactRefs(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, raw := range values {
		ref := strings.TrimSpace(raw)
		if _, exists := seen[ref]; exists {
			continue
		}
		seen[ref] = struct{}{}
		result = append(result, ref)
	}
	sort.Strings(result)
	return result
}

func normalizeText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func validateTextIssues(path, value string, minRunes, maxRunes int) []string {
	issues := make([]string, 0, 4)
	length := len([]rune(value))
	if length < minRunes || length > maxRunes {
		issues = append(issues, fmt.Sprintf("%s length must be between %d and %d", path, minRunes, maxRunes))
	}
	if value == "" {
		return issues
	}
	for _, forbidden := range []string{"报表", "深链", "报告Agent", "报告MCP"} {
		if strings.Contains(value, forbidden) {
			issues = append(issues, fmt.Sprintf("%s contains forbidden term %q", path, forbidden))
		}
	}
	for _, rule := range []struct {
		pattern *regexp.Regexp
		label   string
	}{
		{uuidPattern, "UUID"}, {privateIPPattern, "private IP"}, {longHexPattern, "hash"},
		{rawCodePattern, "raw error code"}, {pascalIdentifierPattern, "internal identifier"},
		{absUnixPathPattern, "absolute path"}, {absWindowsPathPattern, "absolute path"},
		{internalRoutePattern, "internal route"}, {networkPortPattern, "network port"},
		{commandFlagPattern, "command-line flag"},
	} {
		if rule.pattern.MatchString(value) {
			issues = append(issues, fmt.Sprintf("%s contains %s", path, rule.label))
		}
	}
	if strings.Contains(strings.ToLower(value), "bearer ") || strings.Contains(value, "eyJ") ||
		strings.Contains(value, "http://") || strings.Contains(value, "https://") {
		issues = append(issues, path+" contains credentials or an internal location")
	}
	return issues
}

func appendValidationIssue(issues *[]string, issue string) {
	if len(*issues) >= 32 || strings.TrimSpace(issue) == "" {
		return
	}
	*issues = append(*issues, strings.TrimSpace(issue))
}

func appendValidationIssues(issues *[]string, additions []string) {
	for _, issue := range additions {
		appendValidationIssue(issues, issue)
	}
}

func invalidError(sentinel error, issues []string) error {
	return fmt.Errorf("%w: %s", sentinel, strings.Join(issues, "; "))
}
