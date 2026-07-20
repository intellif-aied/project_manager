package reportcontext

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/aidashboard/api/internal/biztime"
)

func verifyRun(ctx context.Context, tx *sql.Tx, userID, runID string) error {
	var exists bool
	err := tx.QueryRowContext(ctx, `
		SELECT true
		FROM ai_runs
		WHERE id = $1 AND user_id = $2 AND business_type = 'report_agent_run'`, runID, userID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func assemble(ctx context.Context, tx *sql.Tx, request BuildRequest) (Payload, error) {
	startUTC, endExclusiveUTC, err := biztime.DateBounds(request.Period.Start, request.Period.End)
	if err != nil {
		return Payload{}, fmt.Errorf("%w: period", ErrInvalidRequest)
	}
	scope, err := loadScope(ctx, tx, request)
	if err != nil {
		return Payload{}, err
	}
	reports, coverage, issues, err := loadReportsAndCoverage(ctx, tx, request, scope)
	if err != nil {
		return Payload{}, err
	}
	requirements, err := loadRequirements(ctx, tx, request, scope, startUTC, endExclusiveUTC)
	if err != nil {
		return Payload{}, err
	}
	tasks, err := loadTasks(ctx, tx, request, scope, startUTC, endExclusiveUTC)
	if err != nil {
		return Payload{}, err
	}

	return Payload{
		SchemaVersion: SchemaVersion,
		Run: Run{
			ID:            request.RunID,
			ReportType:    request.ReportType,
			Period:        request.Period,
			Timezone:      request.Timezone,
			TriggerSource: request.TriggerSource,
			ModelID:       request.ModelID,
			Target:        request.Target,
		},
		Scope:         scope,
		Coverage:      nonNilCoverage(coverage),
		SourceReports: nonNilReports(reports),
		Requirements:  nonNilRequirements(requirements),
		Tasks:         nonNilTasks(tasks),
		Sessions:      []SessionSource{},
		SourceIssues:  nonNilIssues(issues),
	}, nil
}

func sourceStateFor(payload Payload, mode string) SourceState {
	missingNames := make([]string, 0)
	for _, issue := range payload.SourceIssues {
		if issue.Type != "missing" && issue.Type != "invalid" {
			continue
		}
		name := strings.TrimSpace(issue.SourceName)
		if name == "" {
			name = issue.SourceID
		}
		if name != "" {
			missingNames = append(missingNames, name)
		}
	}
	sort.Strings(missingNames)
	missingNames = uniqueStrings(missingNames)
	return SourceState{
		Mode:             mode,
		SourceMode:       mode,
		CoverageComplete: true,
		DependencyReady:  len(missingNames) == 0,
		MissingNames:     missingNames,
	}
}

func nonNilCoverage(v []CoverageItem) []CoverageItem {
	if v == nil {
		return []CoverageItem{}
	}
	return v
}

func nonNilReports(v []SourceReport) []SourceReport {
	if v == nil {
		return []SourceReport{}
	}
	return v
}

func nonNilRequirements(v []Requirement) []Requirement {
	if v == nil {
		return []Requirement{}
	}
	return v
}

func nonNilTasks(v []Task) []Task {
	if v == nil {
		return []Task{}
	}
	return v
}

func nonNilIssues(v []SourceIssue) []SourceIssue {
	if v == nil {
		return []SourceIssue{}
	}
	return v
}

func uniqueStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	out := values[:1]
	for _, value := range values[1:] {
		if value != out[len(out)-1] {
			out = append(out, value)
		}
	}
	return out
}
