package handler

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aidashboard/api/internal/biztime"
	"github.com/aidashboard/api/internal/reportvalue"
	"github.com/go-chi/chi/v5"
)

type DailyReportValueHandler struct {
	db *sql.DB
}

type dailyReportValueQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type valueRatio struct {
	Numerator   int      `json:"numerator"`
	Denominator int      `json:"denominator"`
	Value       *float64 `json:"value"`
}

type valueRun struct {
	ID                 string              `json:"run_id"`
	Status             string              `json:"status"`
	FailureStage       string              `json:"failure_stage,omitempty"`
	AgentID            string              `json:"agent_id"`
	ModelID            string              `json:"model_id,omitempty"`
	CreatedAt          time.Time           `json:"created_at"`
	StartedAt          *time.Time          `json:"started_at,omitempty"`
	FinishedAt         *time.Time          `json:"finished_at,omitempty"`
	DurationMS         *int64              `json:"duration_ms,omitempty"`
	SourceSessionCount int                 `json:"source_session_count"`
	Snapshot           *valueSnapshot      `json:"snapshot,omitempty"`
	Outcome            *valueOutcome       `json:"current_outcome,omitempty"`
	Diff               *reportvalue.Result `json:"diff,omitempty"`
}

type valueSnapshot struct {
	ReportID      string          `json:"report_id"`
	Generated     string          `json:"generated_content,omitempty"`
	GeneratedHash string          `json:"generated_content_sha256"`
	Summary       string          `json:"summary_content,omitempty"`
	SummaryHash   string          `json:"summary_sha256"`
	Variant       json.RawMessage `json:"variant_manifest"`
	VariantHash   string          `json:"variant_hash"`
	CreatedAt     time.Time       `json:"created_at"`
}

type valueOutcome struct {
	ID       string    `json:"id"`
	RunID    string    `json:"run_id,omitempty"`
	Action   string    `json:"action"`
	Content  string    `json:"content,omitempty"`
	Hash     string    `json:"content_sha256,omitempty"`
	ActionAt time.Time `json:"action_at"`
}

type currentDailyReport struct {
	ID             string
	UserID         string
	ReportDate     string
	Content        string
	Status         string
	GenerationMode string
	RunID          string
	UpdatedAt      time.Time
	Downstream     bool
}

type valueUserDay struct {
	UserID           string              `json:"user_id"`
	UserName         string              `json:"user_name"`
	DepartmentID     string              `json:"department_id,omitempty"`
	DepartmentName   string              `json:"department_name,omitempty"`
	TeamID           string              `json:"team_id,omitempty"`
	TeamName         string              `json:"team_name,omitempty"`
	ReportDate       string              `json:"report_date"`
	ReportID         string              `json:"report_id,omitempty"`
	RunCount         int                 `json:"run_count"`
	SuccessfulRuns   int                 `json:"successful_run_count"`
	LastFailureStage string              `json:"last_failure_stage,omitempty"`
	VariantHash      string              `json:"variant_hash,omitempty"`
	OutcomeStatus    string              `json:"outcome_status"`
	Diff             *reportvalue.Result `json:"diff,omitempty"`
	ObservedDiff     *reportvalue.Result `json:"observed_diff,omitempty"`
	CurrentContent   string              `json:"current_content,omitempty"`
	Regenerated      bool                `json:"regenerated"`
	DownstreamReuse  bool                `json:"downstream_reuse"`
	MissingReason    string              `json:"missing_reason,omitempty"`
	CurrentRunID     string              `json:"current_run_id,omitempty"`
	CurrentOutcomeID string              `json:"current_outcome_id,omitempty"`
	Runs             []valueRun          `json:"runs,omitempty"`
	OutcomeEvents    []valueOutcome      `json:"outcome_events,omitempty"`
}

type valueMetrics struct {
	TotalReports            int        `json:"total_reports"`
	AIReports               int        `json:"ai_reports"`
	HandwrittenReports      int        `json:"handwritten_reports"`
	TotalRuns               int        `json:"total_runs"`
	SuccessfulRuns          int        `json:"successful_runs"`
	ComparableOutcomes      int        `json:"comparable_outcomes"`
	AIReportCoverage        valueRatio `json:"ai_report_coverage"`
	GenerationSuccess       valueRatio `json:"generation_success"`
	ConfirmedDirectUse      valueRatio `json:"confirmed_direct_use"`
	LightOrLess             valueRatio `json:"light_or_less"`
	SignificantModification valueRatio `json:"significant_modification"`
	SummaryRemoved          valueRatio `json:"summary_removed"`
	Regeneration            valueRatio `json:"regeneration"`
	ObservedUnchanged       valueRatio `json:"observed_unchanged"`
	DownstreamReuse         valueRatio `json:"downstream_reuse"`
	DraftRetentionP25       *float64   `json:"draft_retention_p25"`
	DraftRetentionP50       *float64   `json:"draft_retention_p50"`
	DraftRetentionP95       *float64   `json:"draft_retention_p95"`
	AverageDurationMS       *float64   `json:"average_duration_ms"`
	P95DurationMS           *float64   `json:"p95_duration_ms"`
	Deletion                valueRatio `json:"deletion"`
}

type valueTrendPoint struct {
	ReportDate string       `json:"report_date"`
	Metrics    valueMetrics `json:"metrics"`
}

type dailyReportValueResponse struct {
	ReportDate       string            `json:"report_date"`
	ObservedAt       time.Time         `json:"observed_at"`
	DataCompleteness string            `json:"data_completeness"`
	MissingCount     int               `json:"missing_count"`
	Metrics          valueMetrics      `json:"metrics"`
	ChangeBands      map[string]int    `json:"change_bands"`
	SummaryOutcomes  map[string]int    `json:"summary_outcomes"`
	FailureStages    map[string]int    `json:"failure_stages"`
	Items            []valueUserDay    `json:"items"`
	Total            int               `json:"total"`
	Page             int               `json:"page"`
	PageSize         int               `json:"page_size"`
	Trend            []valueTrendPoint `json:"trend"`
	Variants         []string          `json:"variants"`
}

type valueFacts struct {
	Runs     map[string][]valueRun
	Reports  map[string]currentDailyReport
	Outcomes map[string][]valueOutcome
	People   map[string]valueUserDay
}

func NewDailyReportValueHandler(db *sql.DB) *DailyReportValueHandler {
	return &DailyReportValueHandler{db: db}
}

func (h *DailyReportValueHandler) List(w http.ResponseWriter, r *http.Request) {
	reportDate, ok := valueReportDate(w, r)
	if !ok {
		return
	}
	trendDays := queryInt(r, "trend_days", 14, 0, 30)
	fromDate := reportDate
	if trendDays > 1 {
		parsed, _ := time.Parse("2006-01-02", reportDate)
		fromDate = parsed.AddDate(0, 0, -(trendDays - 1)).Format("2006-01-02")
	}
	facts, observedAt, err := h.loadFacts(r.Context(), fromDate, reportDate)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	allItems := aggregateValueDays(facts, reportDate, false)
	variants := valueVariantOptions(allItems)
	allItems = filterValueItems(allItems, r)
	page := queryInt(r, "page", 1, 1, 1000000)
	pageSize := queryInt(r, "page_size", 20, 1, 100)
	start := (page - 1) * pageSize
	end := start + pageSize
	if start > len(allItems) {
		start = len(allItems)
	}
	if end > len(allItems) {
		end = len(allItems)
	}
	metrics, bands, summaries, failures, missing := calculateValueMetrics(allItems, facts, reportDate)
	response := dailyReportValueResponse{
		ReportDate: reportDate, ObservedAt: observedAt,
		DataCompleteness: completeness(missing), MissingCount: missing,
		Metrics: metrics, ChangeBands: bands, SummaryOutcomes: summaries, FailureStages: failures,
		Items: allItems[start:end], Total: len(allItems), Page: page, PageSize: pageSize,
		Variants: variants,
	}
	if trendDays > 0 {
		response.Trend = buildValueTrend(facts, fromDate, reportDate, r)
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *DailyReportValueHandler) Detail(w http.ResponseWriter, r *http.Request) {
	reportDate, ok := valueReportDate(w, r)
	if !ok {
		return
	}
	userID := strings.TrimSpace(chi.URLParam(r, "user_id"))
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user_id is required"})
		return
	}
	facts, observedAt, err := h.loadFacts(r.Context(), reportDate, reportDate)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	for _, item := range aggregateValueDays(facts, reportDate, true) {
		if item.UserID == userID {
			writeJSON(w, http.StatusOK, map[string]any{"report_date": reportDate, "observed_at": observedAt, "item": item})
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
}

func (h *DailyReportValueHandler) Export(w http.ResponseWriter, r *http.Request) {
	reportDate, ok := valueReportDate(w, r)
	if !ok {
		return
	}
	facts, observedAt, err := h.loadFacts(r.Context(), reportDate, reportDate)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	items := filterValueItems(aggregateValueDays(facts, reportDate, true), r)
	metrics, bands, summaries, failures, missing := calculateValueMetrics(items, facts, reportDate)
	payload := map[string]any{
		"schema_version": "production-daily-report-value/v1", "report_date": reportDate,
		"observed_at": observedAt, "filters": r.URL.Query(), "data_completeness": completeness(missing),
		"missing_count": missing, "metrics": metrics, "change_bands": bands,
		"summary_outcomes": summaries, "failure_stages": failures, "items": items,
	}
	canonical, err := json.Marshal(payload)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	sum := sha256.Sum256(canonical)
	payload["snapshot_sha256"] = hex.EncodeToString(sum[:])
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="daily-report-value-%s.json"`, reportDate))
	writeJSON(w, http.StatusOK, payload)
}

func (h *DailyReportValueHandler) loadFacts(ctx context.Context, fromDate, toDate string) (valueFacts, time.Time, error) {
	facts := valueFacts{Runs: map[string][]valueRun{}, Reports: map[string]currentDailyReport{}, Outcomes: map[string][]valueOutcome{}, People: map[string]valueUserDay{}}
	tx, err := h.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return facts, time.Time{}, err
	}
	defer tx.Rollback()
	var observedAt time.Time
	if err := tx.QueryRowContext(ctx, `SELECT transaction_timestamp()`).Scan(&observedAt); err != nil {
		return facts, time.Time{}, err
	}
	if err := h.loadValueRuns(ctx, tx, &facts, fromDate, toDate, observedAt); err != nil {
		return facts, time.Time{}, err
	}
	if err := h.loadValueReports(ctx, tx, &facts, fromDate, toDate, observedAt); err != nil {
		return facts, time.Time{}, err
	}
	if err := h.loadValueOutcomes(ctx, tx, &facts, fromDate, toDate, observedAt); err != nil {
		return facts, time.Time{}, err
	}
	if err := tx.Commit(); err != nil {
		return facts, time.Time{}, err
	}
	return facts, observedAt.UTC(), nil
}

func (h *DailyReportValueHandler) loadValueRuns(ctx context.Context, queryer dailyReportValueQueryer, facts *valueFacts, fromDate, toDate string, observedAt time.Time) error {
	rows, err := queryer.QueryContext(ctx, `
		SELECT ar.id::text, ar.user_id::text,
			COALESCE(NULLIF(u.nickname, ''), u.username), COALESCE(t.id::text, ''), COALESCE(t.name, ''),
			COALESCE(d.id::text, ''), COALESCE(d.name, ''),
			COALESCE(ar.status, ''), COALESCE(ar.failure_stage, ''), COALESCE(ar.execution_stage, ''),
			ar.agent_id, COALESCE(ar.model_id, ''), ar.created_at, ar.started_at, ar.finished_at,
			COALESCE(s.report_id::text, ''), COALESCE(s.generated_content, ''), COALESCE(s.generated_content_sha256, ''),
			COALESCE(s.summary_content, ''), COALESCE(s.summary_sha256, ''), s.variant_manifest_json,
			COALESCE(s.variant_sha256, ''), s.created_at,
			(SELECT COUNT(DISTINCT rssi.session_id)
			 FROM report_source_selections rss
			 JOIN report_source_selection_items rssi ON rssi.selection_id = rss.id
			 WHERE rss.attached_run_id = ar.id),
			COALESCE(ar.input_ref_json #>> '{period,date}', ar.input_ref_json ->> 'report_date', '')
		FROM ai_runs ar
		JOIN users u ON u.id = ar.user_id
		LEFT JOIN teams t ON t.id = u.team_id
		LEFT JOIN departments d ON d.id = t.department_id
		LEFT JOIN report_generation_snapshots s ON s.run_id = ar.id AND s.created_at <= $3
		WHERE ar.business_type = 'report_agent_run'
		  AND ar.input_ref_json ->> 'report_type' = 'personal_daily'
		  AND COALESCE(ar.input_ref_json #>> '{period,date}', ar.input_ref_json ->> 'report_date', '') BETWEEN $1 AND $2
		  AND ar.created_at <= $3
		ORDER BY ar.created_at, ar.id`, fromDate, toDate, observedAt)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var run valueRun
		var userID, userName, teamID, teamName, departmentID, departmentName, executionStage, reportDate string
		var startedAt, finishedAt, snapshotAt sql.NullTime
		var reportID, generated, generatedHash, summary, summaryHash, variantHash string
		var variant []byte
		if err := rows.Scan(&run.ID, &userID, &userName, &teamID, &teamName, &departmentID, &departmentName,
			&run.Status, &run.FailureStage, &executionStage, &run.AgentID, &run.ModelID, &run.CreatedAt, &startedAt, &finishedAt,
			&reportID, &generated, &generatedHash, &summary, &summaryHash, &variant, &variantHash, &snapshotAt,
			&run.SourceSessionCount, &reportDate); err != nil {
			return err
		}
		if run.FailureStage == "" && run.Status != "succeeded" {
			run.FailureStage = executionStage
		}
		if startedAt.Valid {
			value := startedAt.Time
			run.StartedAt = &value
		}
		if finishedAt.Valid {
			value := finishedAt.Time
			run.FinishedAt = &value
			start := run.CreatedAt
			if run.StartedAt != nil {
				start = *run.StartedAt
			}
			duration := value.Sub(start).Milliseconds()
			run.DurationMS = &duration
		}
		if reportID != "" && snapshotAt.Valid {
			run.Snapshot = &valueSnapshot{ReportID: reportID, Generated: generated, GeneratedHash: generatedHash, Summary: summary, SummaryHash: summaryHash, Variant: variant, VariantHash: variantHash, CreatedAt: snapshotAt.Time}
		}
		key := valueDayKey(userID, reportDate)
		facts.Runs[key] = append(facts.Runs[key], run)
		facts.People[key] = valueUserDay{UserID: userID, UserName: userName, DepartmentID: departmentID, DepartmentName: departmentName, TeamID: teamID, TeamName: teamName, ReportDate: reportDate}
	}
	return rows.Err()
}

func (h *DailyReportValueHandler) loadValueReports(ctx context.Context, queryer dailyReportValueQueryer, facts *valueFacts, fromDate, toDate string, observedAt time.Time) error {
	rows, err := queryer.QueryContext(ctx, `
		SELECT dr.id::text, dr.user_id::text, dr.report_date::text, dr.content, COALESCE(dr.status, ''),
			COALESCE(dr.generation_mode, ''), COALESCE(dr.managed_agent_run_id::text, ''), dr.updated_at,
			COALESCE(NULLIF(u.nickname, ''), u.username), COALESCE(t.id::text, ''), COALESCE(t.name, ''),
			COALESCE(d.id::text, ''), COALESCE(d.name, ''),
			EXISTS (SELECT 1 FROM team_reports tr WHERE dr.id = ANY(COALESCE(tr.source_daily_report_ids, '{}')))
		FROM daily_reports dr
		JOIN users u ON u.id = dr.user_id
		LEFT JOIN teams t ON t.id = u.team_id
		LEFT JOIN departments d ON d.id = t.department_id
		WHERE dr.report_date BETWEEN $1 AND $2 AND dr.created_at <= $3`, fromDate, toDate, observedAt)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var report currentDailyReport
		var userName, teamID, teamName, departmentID, departmentName string
		if err := rows.Scan(&report.ID, &report.UserID, &report.ReportDate, &report.Content, &report.Status,
			&report.GenerationMode, &report.RunID, &report.UpdatedAt, &userName, &teamID, &teamName, &departmentID, &departmentName, &report.Downstream); err != nil {
			return err
		}
		key := valueDayKey(report.UserID, report.ReportDate)
		facts.Reports[key] = report
		facts.People[key] = valueUserDay{UserID: report.UserID, UserName: userName, DepartmentID: departmentID, DepartmentName: departmentName, TeamID: teamID, TeamName: teamName, ReportDate: report.ReportDate}
	}
	return rows.Err()
}

func (h *DailyReportValueHandler) loadValueOutcomes(ctx context.Context, queryer dailyReportValueQueryer, facts *valueFacts, fromDate, toDate string, observedAt time.Time) error {
	rows, err := queryer.QueryContext(ctx, `
		SELECT id::text, user_id::text, report_date::text, COALESCE(managed_agent_run_id::text, ''),
			action, COALESCE(content, ''), COALESCE(content_sha256, ''), action_at
		FROM report_user_outcome_events
		WHERE report_date BETWEEN $1 AND $2 AND action_at <= $3
		ORDER BY action_at, id`, fromDate, toDate, observedAt)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var outcome valueOutcome
		var userID, reportDate string
		if err := rows.Scan(&outcome.ID, &userID, &reportDate, &outcome.RunID, &outcome.Action, &outcome.Content, &outcome.Hash, &outcome.ActionAt); err != nil {
			return err
		}
		facts.Outcomes[valueDayKey(userID, reportDate)] = append(facts.Outcomes[valueDayKey(userID, reportDate)], outcome)
	}
	return rows.Err()
}

func aggregateValueDays(facts valueFacts, reportDate string, includeContent bool) []valueUserDay {
	items := make([]valueUserDay, 0)
	for key, person := range facts.People {
		if person.ReportDate != reportDate {
			continue
		}
		runs := append([]valueRun(nil), facts.Runs[key]...)
		outcomes := append([]valueOutcome(nil), facts.Outcomes[key]...)
		for index := range runs {
			if runs[index].Snapshot != nil {
				snapshot := *runs[index].Snapshot
				runs[index].Snapshot = &snapshot
				for outcomeIndex := range outcomes {
					outcome := outcomes[outcomeIndex]
					if outcome.RunID != runs[index].ID || outcome.ActionAt.Before(snapshot.CreatedAt) ||
						(outcome.Action != "saved" && outcome.Action != "submitted") {
						continue
					}
					latest := outcome
					runs[index].Outcome = &latest
				}
				if runs[index].Outcome != nil {
					diff := reportvalue.Compare(snapshot.Generated, runs[index].Outcome.Content)
					runs[index].Diff = &diff
				}
			}
		}
		item := person
		item.Runs = runs
		item.OutcomeEvents = outcomes
		item.RunCount = len(runs)
		item.Regenerated = len(runs) > 1
		for index := range runs {
			if runs[index].Status == "succeeded" {
				item.SuccessfulRuns++
			} else if runs[index].FailureStage != "" {
				item.LastFailureStage = runs[index].FailureStage
			}
		}
		report, hasReport := facts.Reports[key]
		if hasReport {
			item.ReportID = report.ID
			item.DownstreamReuse = report.Downstream
			if includeContent {
				item.CurrentContent = report.Content
			}
		}
		var currentRunIndex = -1
		for index := range runs {
			if runs[index].Status != "succeeded" {
				continue
			}
			if currentRunIndex < 0 || valueRunCompletedAt(runs[index]).After(valueRunCompletedAt(runs[currentRunIndex])) ||
				(valueRunCompletedAt(runs[index]).Equal(valueRunCompletedAt(runs[currentRunIndex])) && runs[index].ID > runs[currentRunIndex].ID) {
				currentRunIndex = index
			}
		}
		if currentRunIndex < 0 {
			if hasReport {
				item.OutcomeStatus = "handwritten"
			} else {
				item.OutcomeStatus = "no_result"
			}
			item = redactValueItem(item, includeContent)
			items = append(items, item)
			continue
		}
		currentRun := &item.Runs[currentRunIndex]
		item.CurrentRunID = currentRun.ID
		if currentRun.Snapshot == nil {
			item.OutcomeStatus = "not_comparable"
			item.MissingReason = "historical_draft_not_collected"
			item = redactValueItem(item, includeContent)
			items = append(items, item)
			continue
		}
		item.VariantHash = currentRun.Snapshot.VariantHash
		latestOutcome := currentRun.Outcome
		var latestDelete *valueOutcome
		for index := range facts.Outcomes[key] {
			outcome := facts.Outcomes[key][index]
			if outcome.ActionAt.Before(currentRun.Snapshot.CreatedAt) {
				continue
			}
			if outcome.Action == "deleted" {
				copy := outcome
				latestDelete = &copy
			}
		}
		if latestOutcome != nil && (latestDelete == nil || latestOutcome.ActionAt.After(latestDelete.ActionAt)) {
			item.CurrentOutcomeID = latestOutcome.ID
			item.Diff = currentRun.Diff
			diff := *currentRun.Diff
			if diff.Text.ChangeBand == reportvalue.ChangeUnchanged {
				item.OutcomeStatus = "confirmed_direct_use"
			} else {
				item.OutcomeStatus = "modified"
			}
		} else if latestDelete != nil {
			item.OutcomeStatus = "deleted"
		} else if hasReport && report.RunID == currentRun.ID {
			diff := reportvalue.Compare(currentRun.Snapshot.Generated, report.Content)
			item.ObservedDiff = &diff
			if diff.Text.ChangeBand == reportvalue.ChangeUnchanged {
				item.OutcomeStatus = "observed_unchanged"
			} else {
				item.OutcomeStatus = "no_explicit_outcome"
			}
		} else {
			item.OutcomeStatus = "no_explicit_outcome"
		}
		item = redactValueItem(item, includeContent)
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].DepartmentName != items[j].DepartmentName {
			return items[i].DepartmentName < items[j].DepartmentName
		}
		if items[i].TeamName != items[j].TeamName {
			return items[i].TeamName < items[j].TeamName
		}
		return items[i].UserName < items[j].UserName
	})
	return items
}

func valueRunCompletedAt(run valueRun) time.Time {
	if run.Snapshot != nil && !run.Snapshot.CreatedAt.IsZero() {
		return run.Snapshot.CreatedAt
	}
	if run.FinishedAt != nil {
		return *run.FinishedAt
	}
	return run.CreatedAt
}

func redactValueRuns(runs []valueRun, includeContent bool) []valueRun {
	if includeContent {
		return runs
	}
	for index := range runs {
		if runs[index].Snapshot != nil {
			runs[index].Snapshot.Generated = ""
			runs[index].Snapshot.Summary = ""
		}
		if runs[index].Outcome != nil {
			runs[index].Outcome.Content = ""
		}
	}
	return runs
}

func redactValueItem(item valueUserDay, includeContent bool) valueUserDay {
	item.Runs = redactValueRuns(item.Runs, includeContent)
	if includeContent {
		return item
	}
	item.CurrentContent = ""
	for index := range item.OutcomeEvents {
		item.OutcomeEvents[index].Content = ""
	}
	return item
}

func calculateValueMetrics(items []valueUserDay, facts valueFacts, reportDate string) (valueMetrics, map[string]int, map[string]int, map[string]int, int) {
	metrics := valueMetrics{}
	bands := map[string]int{"unchanged": 0, "light": 0, "medium": 0, "heavy": 0, "not_comparable": 0}
	summaries := map[string]int{"summary_unchanged": 0, "summary_modified": 0, "summary_removed": 0, "summary_reduced_30": 0, "not_applicable": 0}
	failures := map[string]int{}
	retentions := make([]float64, 0)
	aiUserDays, regenerated, observedWithoutOutcome, observedUnchanged, downstreamEligible, downstream := 0, 0, 0, 0, 0, 0
	direct, lightOrLess, significant, summaryRemoved, summaryComparable, deleted, missing := 0, 0, 0, 0, 0, 0, 0
	durations := make([]float64, 0)
	for _, item := range items {
		report, hasReport := facts.Reports[valueDayKey(item.UserID, reportDate)]
		if hasReport {
			metrics.TotalReports++
			if report.RunID != "" || report.GenerationMode == "managed_agent" {
				metrics.AIReports++
			} else {
				metrics.HandwrittenReports++
			}
		}
		metrics.TotalRuns += item.RunCount
		metrics.SuccessfulRuns += item.SuccessfulRuns
		for _, run := range item.Runs {
			if run.Status != "succeeded" && run.FailureStage != "" {
				failures[run.FailureStage]++
			}
			if run.DurationMS != nil {
				durations = append(durations, float64(*run.DurationMS))
			}
		}
		if item.SuccessfulRuns > 0 {
			aiUserDays++
			if item.Regenerated {
				regenerated++
			}
		}
		if item.MissingReason != "" {
			missing++
		}
		if item.Diff != nil {
			metrics.ComparableOutcomes++
			bands[item.Diff.Text.ChangeBand]++
			if item.Diff.Text.DraftRetention != nil {
				retentions = append(retentions, *item.Diff.Text.DraftRetention)
			}
			if item.OutcomeStatus == "confirmed_direct_use" {
				direct++
			}
			if item.Diff.Text.ChangeBand == "unchanged" || item.Diff.Text.ChangeBand == "light" {
				lightOrLess++
			}
			if item.Diff.Text.ChangeBand == "medium" || item.Diff.Text.ChangeBand == "heavy" {
				significant++
			}
			summaries[item.Diff.Summary.Outcome]++
			if item.Diff.Summary.Reduced30 {
				summaries["summary_reduced_30"]++
			}
			if item.Diff.Summary.GeneratedPresent {
				summaryComparable++
				if item.Diff.Summary.Outcome == "summary_removed" {
					summaryRemoved++
				}
			}
		} else {
			bands["not_comparable"]++
			summaries["not_applicable"]++
		}
		if item.OutcomeStatus == "observed_unchanged" {
			observedUnchanged++
		}
		if item.OutcomeStatus == "deleted" {
			deleted++
		}
		if item.OutcomeStatus == "observed_unchanged" || item.OutcomeStatus == "no_explicit_outcome" {
			observedWithoutOutcome++
		}
		if hasReport && (report.RunID != "" || report.GenerationMode == "managed_agent") {
			downstreamEligible++
			if item.DownstreamReuse {
				downstream++
			}
		}
	}
	metrics.AIReportCoverage = ratio(metrics.AIReports, metrics.TotalReports)
	metrics.GenerationSuccess = ratio(metrics.SuccessfulRuns, metrics.TotalRuns)
	metrics.ConfirmedDirectUse = ratio(direct, metrics.ComparableOutcomes)
	metrics.LightOrLess = ratio(lightOrLess, metrics.ComparableOutcomes)
	metrics.SignificantModification = ratio(significant, metrics.ComparableOutcomes)
	metrics.SummaryRemoved = ratio(summaryRemoved, summaryComparable)
	metrics.Regeneration = ratio(regenerated, aiUserDays)
	metrics.ObservedUnchanged = ratio(observedUnchanged, observedWithoutOutcome)
	metrics.DownstreamReuse = ratio(downstream, downstreamEligible)
	metrics.Deletion = ratio(deleted, aiUserDays)
	sort.Float64s(retentions)
	sort.Float64s(durations)
	metrics.DraftRetentionP25 = percentile(retentions, 0.25)
	metrics.DraftRetentionP50 = percentile(retentions, 0.50)
	metrics.DraftRetentionP95 = percentile(retentions, 0.95)
	if len(durations) > 0 {
		var total float64
		for _, duration := range durations {
			total += duration
		}
		average := total / float64(len(durations))
		metrics.AverageDurationMS = &average
	}
	metrics.P95DurationMS = percentile(durations, 0.95)
	return metrics, bands, summaries, failures, missing
}

func buildValueTrend(facts valueFacts, fromDate, toDate string, r *http.Request) []valueTrendPoint {
	from, _ := time.Parse("2006-01-02", fromDate)
	to, _ := time.Parse("2006-01-02", toDate)
	result := make([]valueTrendPoint, 0)
	for date := from; !date.After(to); date = date.AddDate(0, 0, 1) {
		value := date.Format("2006-01-02")
		items := filterValueItems(aggregateValueDays(facts, value, false), r)
		metrics, _, _, _, _ := calculateValueMetrics(items, facts, value)
		result = append(result, valueTrendPoint{ReportDate: value, Metrics: metrics})
	}
	return result
}

func filterValueItems(items []valueUserDay, r *http.Request) []valueUserDay {
	query := r.URL.Query()
	result := make([]valueUserDay, 0, len(items))
	for _, item := range items {
		if value := strings.TrimSpace(query.Get("department_id")); value != "" && item.DepartmentID != value {
			continue
		}
		if value := strings.TrimSpace(query.Get("team_id")); value != "" && item.TeamID != value {
			continue
		}
		if value := strings.TrimSpace(query.Get("variant_hash")); value != "" && item.VariantHash != value {
			continue
		}
		if value := strings.TrimSpace(query.Get("outcome_status")); value != "" && item.OutcomeStatus != value {
			continue
		}
		if value := strings.TrimSpace(query.Get("generation_status")); value != "" {
			matches := value == "succeeded" && item.SuccessfulRuns > 0 && item.SuccessfulRuns == item.RunCount ||
				value == "failed" && item.RunCount > 0 && item.SuccessfulRuns == 0 ||
				value == "partial" && item.SuccessfulRuns > 0 && item.SuccessfulRuns < item.RunCount
			if !matches {
				continue
			}
		}
		if value := strings.TrimSpace(query.Get("change_band")); value != "" && (item.Diff == nil || item.Diff.Text.ChangeBand != value) {
			continue
		}
		if value := strings.TrimSpace(query.Get("summary_outcome")); value != "" && (item.Diff == nil || item.Diff.Summary.Outcome != value) {
			continue
		}
		if value := strings.TrimSpace(query.Get("regenerated")); value != "" && strconv.FormatBool(item.Regenerated) != value {
			continue
		}
		if value := strings.TrimSpace(query.Get("missing")); value != "" && strconv.FormatBool(item.MissingReason != "") != value {
			continue
		}
		result = append(result, item)
	}
	return result
}

func valueVariantOptions(items []valueUserDay) []string {
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0)
	for _, item := range items {
		value := strings.TrimSpace(item.VariantHash)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func valueReportDate(w http.ResponseWriter, r *http.Request) (string, bool) {
	value := strings.TrimSpace(r.URL.Query().Get("report_date"))
	if value == "" {
		today, _ := biztime.ParseDate(biztime.Today())
		value = today.AddDate(0, 0, -1).Format("2006-01-02")
	}
	if _, err := time.Parse("2006-01-02", value); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "report_date must be YYYY-MM-DD"})
		return "", false
	}
	return value, true
}

func queryInt(r *http.Request, key string, defaultValue, minimum, maximum int) int {
	value, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil || value < minimum || value > maximum {
		return defaultValue
	}
	return value
}

func valueDayKey(userID, reportDate string) string { return userID + "\x00" + reportDate }

func ratio(numerator, denominator int) valueRatio {
	result := valueRatio{Numerator: numerator, Denominator: denominator}
	if denominator > 0 {
		value := float64(numerator) / float64(denominator)
		result.Value = &value
	}
	return result
}

func percentile(sortedValues []float64, percentile float64) *float64 {
	if len(sortedValues) == 0 {
		return nil
	}
	index := int(float64(len(sortedValues)-1)*percentile + 0.5)
	value := sortedValues[index]
	return &value
}

func completeness(missing int) string {
	if missing > 0 {
		return "partial"
	}
	return "complete"
}
