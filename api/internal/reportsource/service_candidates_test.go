package reportsource

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestListCandidatesDoesNotQueryUsageMetrics(t *testing.T) {
	queryMatcher := sqlmock.QueryMatcherFunc(func(_ string, actualSQL string) error {
		for _, forbidden := range []string{
			"session_usage_components",
			"normalized_total_tokens",
			"GROUP BY sl.id, s.id, rev.id",
		} {
			if strings.Contains(actualSQL, forbidden) {
				return fmt.Errorf("candidate query still contains %s", forbidden)
			}
		}
		for _, required := range []string{
			"ORDER BY e.occurred_at ASC",
			"ORDER BY e.occurred_at DESC",
			"LEFT JOIN LATERAL",
		} {
			if !strings.Contains(actualSQL, required) {
				return fmt.Errorf("candidate query is missing %s", required)
			}
		}
		if !strings.Contains(actualSQL, "FROM paged p CROSS JOIN totals t") {
			return fmt.Errorf("unexpected candidate query")
		}
		return nil
	})
	database, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(queryMatcher))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	now := time.Date(2026, 7, 17, 8, 30, 0, 0, time.UTC)
	mock.ExpectQuery("candidate list without usage metrics").
		WithArgs("307", "%%", nil, nil, 10, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"slice_key", "session_ref", "agent_type", "summary", "last_activity_at",
			"activity_start_at", "activity_end_at", "cwd", "models", "content_status",
			"available_through_at", "total_count",
		}).AddRow(
			"slice-1", "session-1", "codex", "完成接口优化", now, now.Add(-time.Hour), now,
			"/workspace/project", "{gpt-test}", "available", now, 1,
		))

	service, err := NewService(database)
	if err != nil {
		t.Fatal(err)
	}
	page, err := service.ListCandidates(context.Background(), "307", CandidateQuery{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].SliceKey != "slice-1" {
		t.Fatalf("unexpected candidate page: %+v", page)
	}
	if page.Items[0].ContentIndexStatus != "ready" {
		t.Fatalf("content index status = %q", page.Items[0].ContentIndexStatus)
	}
	payload, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "total_tokens") {
		t.Fatalf("candidate response still contains token totals: %s", payload)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
