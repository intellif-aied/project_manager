package reportsource

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	projectdb "github.com/aidashboard/api/db"
)

func TestCreateReportRunIdempotencyAndActiveDedupeIntegration(t *testing.T) {
	databaseURL := os.Getenv("AIDA_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("AIDA_TEST_DATABASE_URL is not set")
	}
	database, err := projectdb.Connect(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := projectdb.RunMigrations(database); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	userID := time.Now().UnixNano()%100000000 + 780000000
	if _, err := database.ExecContext(ctx, `INSERT INTO users (id, username) VALUES ($1, $2)`, userID, "report-run-idempotency"); err != nil {
		t.Fatal(err)
	}
	defer database.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, userID)
	service, err := NewService(database)
	if err != nil {
		t.Fatal(err)
	}
	period := Period{Start: "2026-07-22", End: "2026-07-22"}
	scope := map[string]any{
		"report_type": "team_daily", "period": period, "team_id": "team-1",
		"agent_id": "agent-1", "model_id": "model-1",
	}
	request := RunSubmissionRequest{
		UserID:                  int64Text(userID),
		ReportType:              "team_daily",
		Period:                  period,
		BusinessType:            "report_agent_run",
		AgentID:                 "agent-1",
		ModelID:                 "model-1",
		IdempotencyKey:          "72ebd9f8-4cd6-4fd8-9977-a31c697f498a",
		RequestFingerprintInput: scope,
		ActiveDedupeInput:       scope,
		InputRef:                map[string]any{"report_type": "team_daily", "period": map[string]any{"date": "2026-07-22"}},
		ExecutionInput:          map[string]any{"timezone": "Asia/Shanghai"},
	}
	first, err := service.CreateReportRun(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if first.RunID == "" || first.SelectionID != "" || first.Replayed {
		t.Fatalf("first result = %#v", first)
	}
	replayed, err := service.CreateReportRun(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.RunID != first.RunID || !replayed.Replayed {
		t.Fatalf("idempotent result = %#v first = %#v", replayed, first)
	}
	request.IdempotencyKey = "15be42c3-6e9e-460a-a35f-cb1e4ca012f8"
	deduped, err := service.CreateReportRun(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if deduped.RunID != first.RunID || !deduped.Replayed {
		t.Fatalf("active dedupe result = %#v first = %#v", deduped, first)
	}
	var status, stage string
	var nextAttempt, deadline time.Time
	var startedAt sql.NullTime
	if err := database.QueryRowContext(ctx, `
		SELECT status, execution_stage, next_attempt_at, digest_wait_deadline_at, started_at
		FROM ai_runs WHERE id = $1`, first.RunID,
	).Scan(&status, &stage, &nextAttempt, &deadline, &startedAt); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || stage != "waiting_digest" || nextAttempt.IsZero() || deadline.IsZero() || startedAt.Valid {
		t.Fatalf("run state status=%s stage=%s next=%v deadline=%v started=%v", status, stage, nextAttempt, deadline, startedAt)
	}
}

func int64Text(value int64) string {
	return fmt.Sprintf("%d", value)
}
