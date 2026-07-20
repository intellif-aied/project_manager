package reportcontext

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/aidashboard/api/internal/biztime"
	"github.com/aidashboard/api/internal/reportsource"
)

func TestBuildAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("REPORT_CONTEXT_INTEGRATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("REPORT_CONTEXT_INTEGRATION_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	var userID, teamID, departmentID string
	err = db.QueryRowContext(ctx, `
		SELECT u.id::text, u.team_id::text, COALESCE(u.department_id::text, t.department_id::text)
		FROM users u JOIN teams t ON t.id = u.team_id
		WHERE u.status = 'active' AND t.department_id IS NOT NULL
		ORDER BY u.id LIMIT 1`).Scan(&userID, &teamID, &departmentID)
	if err != nil {
		t.Fatal(err)
	}

	tests := []BuildRequest{
		{ReportType: ReportTypePersonalWeekly, Period: reportsource.Period{Start: "2026-07-13", End: "2026-07-19"}, Target: Target{Type: "self", UserID: userID}},
		{ReportType: ReportTypeTeamDaily, Period: reportsource.Period{Start: "2026-07-16", End: "2026-07-16"}, Target: Target{Type: "team", TeamID: teamID}},
		{ReportType: ReportTypeTeamWeekly, Period: reportsource.Period{Start: "2026-07-13", End: "2026-07-19"}, Target: Target{Type: "team", TeamID: teamID}},
		{ReportType: ReportTypeDepartmentDaily, Period: reportsource.Period{Start: "2026-07-16", End: "2026-07-16"}, Target: Target{Type: "department", DepartmentID: departmentID}},
		{ReportType: ReportTypeDepartmentWeekly, Period: reportsource.Period{Start: "2026-07-13", End: "2026-07-19"}, Target: Target{Type: "department", DepartmentID: departmentID}},
	}
	svc := &Service{db: db}
	for _, test := range tests {
		t.Run(test.ReportType, func(t *testing.T) {
			var runID string
			err := db.QueryRowContext(ctx, `
				INSERT INTO ai_runs (user_id, business_type, runtime_type, agent_id, status, input_ref_json, started_at)
				VALUES ($1, 'report_agent_run', 'managed_session', 'report-context-integration', 'pending', '{}'::jsonb, now())
				RETURNING id::text`, userID).Scan(&runID)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM ai_runs WHERE id = $1`, runID) })

			test.UserID = userID
			test.RunID = runID
			test.Timezone = biztime.Zone
			test.TriggerSource = "integration_test"
			stored, err := svc.Build(ctx, test)
			if err != nil {
				t.Fatal(err)
			}
			var payload Payload
			if err := json.Unmarshal(stored.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Run.ReportType != test.ReportType || payload.Scope.EffectiveUserID != userID || stored.Hash == "" {
				t.Fatalf("unexpected context: run=%+v scope=%+v hash=%q", payload.Run, payload.Scope, stored.Hash)
			}
			again, err := svc.Build(ctx, test)
			if err != nil {
				t.Fatal(err)
			}
			if again.Hash != stored.Hash || string(again.Payload) != string(stored.Payload) {
				t.Fatalf("same run did not reuse frozen context: first=%s second=%s", stored.Hash, again.Hash)
			}
			sum := sha256.Sum256(again.Payload)
			if hex.EncodeToString(sum[:]) != again.Hash {
				t.Fatalf("stored hash does not match canonical payload")
			}
			t.Logf("bytes=%d reports=%d requirements=%d tasks=%d coverage=%d issues=%d", stored.Bytes, len(payload.SourceReports), len(payload.Requirements), len(payload.Tasks), len(payload.Coverage), len(payload.SourceIssues))
		})
	}
}
