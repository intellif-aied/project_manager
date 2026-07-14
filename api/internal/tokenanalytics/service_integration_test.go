package tokenanalytics

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	projectdb "github.com/aidashboard/api/db"
	"github.com/aidashboard/api/internal/pricing"
)

const (
	analyticsAdminID    int64 = 991100
	analyticsDirectorID int64 = 991101
	analyticsLeaderID   int64 = 991102
	analyticsEmployeeID int64 = 991103
	analyticsPMID       int64 = 991104
)

type analyticsFixture struct {
	db           *sql.DB
	departmentID string
	teamOneID    string
	teamTwoID    string
	sessionID    string
	sourceID     string
	generationID string
	revisionID   string
	chunkID      string
	priceBookID  string
	priceID      string
	rateID       string
	activityDate string
}

func TestTokenAnalyticsOrganizationPricingAndSnapshotIntegration(t *testing.T) {
	database := openAnalyticsIntegrationDatabase(t)
	fixture := newAnalyticsFixture(t, database)
	defer fixture.cleanup(t)

	firstComponentID := fixture.insertUsageComponent(t, "first", analyticsEmployeeID, 1_000_000)
	assertComponentOrganization(t, database, firstComponentID, fixture.teamOneID, fixture.departmentID, "via_team")

	pricingService := pricing.NewService(database)
	priced, err := pricingService.Recalculate(context.Background(), pricing.RecalculateFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if priced.Priced != 1 || priced.Changed != 1 {
		t.Fatalf("initial pricing = %+v", priced)
	}
	assertActiveCost(t, database, firstComponentID, "3.000000000000", "21.000000000000")
	fixture.insertPendingSource(t)

	analyticsService := NewService(database)
	filters := Filters{Scope: "management", From: fixture.activityDate, To: fixture.activityDate}
	director := Actor{ID: analyticsDirectorID, Role: "director"}
	firstSummary, err := analyticsService.CreateSummary(context.Background(), director, filters)
	if err != nil {
		t.Fatal(err)
	}
	if firstSummary.TotalTokens != "1000000" || firstSummary.PricingStatus != "priced" ||
		firstSummary.EstimatedCostCNY == nil || *firstSummary.EstimatedCostCNY != "21.000000000000" ||
		firstSummary.PendingSourceCount != "1" || firstSummary.DataFreshness != "pending" {
		t.Fatalf("first summary = %+v", firstSummary)
	}

	trends, err := analyticsService.Trends(context.Background(), director, filters, firstSummary.QuerySnapshotToken)
	if err != nil || len(trends.Items) != 1 || trends.Items[0].TotalTokens != "1000000" {
		t.Fatalf("trends=%+v err=%v", trends, err)
	}
	rankings, err := analyticsService.Rankings(context.Background(), director, filters, firstSummary.QuerySnapshotToken, "user")
	if err != nil || len(rankings.Items) != 4 || rankings.Items[0].Key != "991103" || rankings.Items[0].TotalTokens != "1000000" {
		t.Fatalf("rankings=%+v err=%v", rankings, err)
	}
	sessions, err := analyticsService.Sessions(context.Background(), director, filters, firstSummary.QuerySnapshotToken, 1, 20)
	if err != nil || sessions.Total != 1 || len(sessions.Items) != 1 || sessions.Items[0].SessionRef != "stage3-token-session" {
		t.Fatalf("sessions=%+v err=%v", sessions, err)
	}

	if _, err := analyticsService.Trends(context.Background(), director,
		Filters{Scope: "management", From: fixture.activityDate, To: fixture.activityDate, Model: "other"},
		firstSummary.QuerySnapshotToken); !errors.Is(err, ErrSnapshotMismatch) {
		t.Fatalf("snapshot mismatch error = %v", err)
	}
	if _, err := analyticsService.CreateSummary(context.Background(), Actor{ID: analyticsPMID, Role: "pm"}, filters); !errors.Is(err, ErrForbidden) {
		t.Fatalf("PM management error = %v", err)
	}
	if _, err := analyticsService.CreateSummary(context.Background(), director,
		Filters{Scope: "management", From: fixture.activityDate, To: fixture.activityDate,
			DepartmentID: "00000000-0000-4000-8000-000000000099"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("foreign department error = %v", err)
	}

	if _, err := database.Exec(`UPDATE users SET team_id=$1 WHERE id=$2`, fixture.teamTwoID, analyticsEmployeeID); err != nil {
		t.Fatal(err)
	}
	secondComponentID := fixture.insertUsageComponent(t, "second", analyticsEmployeeID, 1_000_000)
	assertComponentOrganization(t, database, secondComponentID, fixture.teamTwoID, fixture.departmentID, "via_team")
	assertComponentOrganization(t, database, firstComponentID, fixture.teamOneID, fixture.departmentID, "via_team")

	fixture.publishPriceCorrection(t, "4")
	repriced, err := pricingService.RecalculateWithAudit(
		context.Background(), pricing.RecalculateFilter{}, analyticsAdminID, "stage3 price correction",
	)
	if err != nil {
		t.Fatal(err)
	}
	if repriced.Priced != 2 || repriced.Changed != 2 {
		t.Fatalf("repricing = %+v", repriced)
	}
	var auditReason string
	if err := database.QueryRow(`
		SELECT reason FROM pricing_recalculation_runs
		WHERE requested_by=$1 ORDER BY created_at DESC LIMIT 1`, analyticsAdminID).Scan(&auditReason); err != nil {
		t.Fatal(err)
	}
	if auditReason != "stage3 price correction" {
		t.Fatalf("recalculation audit reason = %q", auditReason)
	}
	assertActiveCost(t, database, firstComponentID, "4.000000000000", "28.000000000000")
	assertActiveCost(t, database, secondComponentID, "4.000000000000", "28.000000000000")

	stableTrends, err := analyticsService.Trends(context.Background(), director, filters, firstSummary.QuerySnapshotToken)
	if err != nil || len(stableTrends.Items) != 1 || stableTrends.Items[0].TotalTokens != "1000000" ||
		stableTrends.Items[0].EstimatedCostCNY == nil || *stableTrends.Items[0].EstimatedCostCNY != "21.000000000000" {
		t.Fatalf("stable snapshot trends=%+v err=%v", stableTrends, err)
	}
	newSummary, err := analyticsService.CreateSummary(context.Background(), director, filters)
	if err != nil {
		t.Fatal(err)
	}
	if newSummary.TotalTokens != "2000000" || newSummary.EstimatedCostCNY == nil || *newSummary.EstimatedCostCNY != "56.000000000000" {
		t.Fatalf("new summary = %+v", newSummary)
	}

	teamOneFilters := Filters{Scope: "management", From: fixture.activityDate, To: fixture.activityDate}
	teamOne := Actor{ID: analyticsLeaderID, Role: "team_leader", TeamID: &fixture.teamOneID}
	teamSummary, err := analyticsService.CreateSummary(context.Background(), teamOne, teamOneFilters)
	if err != nil || teamSummary.TotalTokens != "1000000" {
		t.Fatalf("historical team summary=%+v err=%v", teamSummary, err)
	}

	if _, err := database.Exec(`
		INSERT INTO user_team_memberships(user_id, team_id, effective_from, source)
		VALUES($1, $2, now() - interval '2 hours', 'invalid_overlap')`, analyticsEmployeeID, fixture.teamOneID); err == nil {
		t.Fatal("expected overlapping membership to be rejected")
	}
	if _, err := database.Exec(`UPDATE model_price_versions SET input_per_million=99 WHERE id=$1`, fixture.priceID); err == nil {
		t.Fatal("expected superseded published price to remain immutable")
	}
}

func newAnalyticsFixture(t *testing.T, database *sql.DB) *analyticsFixture {
	t.Helper()
	fixture := &analyticsFixture{db: database, activityDate: time.Now().Format("2006-01-02")}
	fixture.cleanup(t)
	for _, user := range []struct {
		id   int64
		role string
		name string
	}{
		{analyticsAdminID, "admin", "stage3-admin"},
		{analyticsDirectorID, "director", "stage3-director"},
		{analyticsPMID, "pm", "stage3-pm"},
	} {
		if _, err := database.Exec(`
			INSERT INTO users(id, employee_id, username, nickname, name, app_role, role, local_enabled, aida_enabled)
			VALUES($1::bigint, $1::text, $2, $2, $2, $3, $3, true, true)`, user.id, user.name, user.role); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.QueryRow(`
		INSERT INTO departments(name, director_user_id) VALUES('stage3-department', $1)
		RETURNING id::text`, analyticsDirectorID).Scan(&fixture.departmentID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE users SET department_id=$1 WHERE id IN ($2, $3)`, fixture.departmentID, analyticsDirectorID, analyticsPMID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`INSERT INTO teams(name, department_id) VALUES('stage3-team-one', $1) RETURNING id::text`, fixture.departmentID).Scan(&fixture.teamOneID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`INSERT INTO teams(name, department_id) VALUES('stage3-team-two', $1) RETURNING id::text`, fixture.departmentID).Scan(&fixture.teamTwoID); err != nil {
		t.Fatal(err)
	}
	for _, user := range []struct {
		id   int64
		role string
		name string
	}{
		{analyticsLeaderID, "team_leader", "stage3-leader"},
		{analyticsEmployeeID, "employee", "stage3-employee"},
	} {
		if _, err := database.Exec(`
			INSERT INTO users(id, employee_id, username, nickname, name, app_role, role, team_id, local_enabled, aida_enabled)
			VALUES($1::bigint, $1::text, $2, $2, $2, $3, $3, $4, true, true)`, user.id, user.name, user.role, fixture.teamOneID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.Exec(`UPDATE user_team_memberships SET effective_from=now()-interval '1 day' WHERE user_id IN ($1,$2)`, analyticsLeaderID, analyticsEmployeeID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE team_department_memberships SET effective_from=now()-interval '1 day' WHERE team_id IN ($1,$2)`, fixture.teamOneID, fixture.teamTwoID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE user_department_memberships SET effective_from=now()-interval '1 day' WHERE user_id IN ($1,$2)`, analyticsDirectorID, analyticsPMID); err != nil {
		t.Fatal(err)
	}

	if err := database.QueryRow(`
		INSERT INTO sessions(session_ref, user_id, agent_type, started_at, last_activity_at, summary)
		VALUES('stage3-token-session', $1, 'codex', now(), now(), 'stage3 searchable summary')
		RETURNING id::text`, analyticsEmployeeID).Scan(&fixture.sessionID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO session_sources(session_id, source_role, source_key)
		VALUES($1, 'primary', 'stage3-source') RETURNING id::text`, fixture.sessionID).Scan(&fixture.sourceID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO session_source_generations(source_id, status)
		VALUES($1, 'active') RETURNING id::text`, fixture.sourceID).Scan(&fixture.generationID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE session_sources SET active_generation_id=$1 WHERE id=$2`, fixture.generationID, fixture.sourceID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO session_upload_chunks(
			generation_id, start_cursor, end_cursor, start_line, end_line,
			content_sha256, content_epoch, raw_object_key, object_status,
			content_index_status, usage_parse_status
		) VALUES($1, 0, 10, 1, 1, repeat('a',64), 0, 'stage3-object', 'available', 'indexed', 'parsed')
		RETURNING id::text`, fixture.generationID).Scan(&fixture.chunkID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO session_metrics_revisions(
			source_id, generation_id, parser_version, normalizer_version,
			status, quality_status, validated_through_cursor, source_high_water_cursor,
			validated_at, activated_at
		) VALUES($1, $2, 'stage3-parser', 'stage3-normalizer', 'active', 'exact', 10, 10, now(), now())
		RETURNING id::text`, fixture.sourceID, fixture.generationID).Scan(&fixture.revisionID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO session_source_metrics_states(
			source_id, active_revision_id, target_generation_id, status,
			active_usage_parsed_cursor, source_high_water_cursor
		) VALUES($1, $2, $3, 'ready', 10, 10)`, fixture.sourceID, fixture.revisionID, fixture.generationID); err != nil {
		t.Fatal(err)
	}

	if err := database.QueryRow(`
		INSERT INTO price_books(name, status, created_by)
		VALUES('stage3-official-prices', 'active', $1) RETURNING id::text`, analyticsAdminID).Scan(&fixture.priceBookID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO model_aliases(provider, raw_model_pattern, canonical_model, status, reviewed_by, reviewed_at, created_by)
		VALUES('codex', 'gpt-stage3', 'gpt-stage3-canonical', 'reviewed', $1, now(), $1)`, analyticsAdminID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO model_price_versions(
			price_book_id, canonical_model, billing_variant,
			input_per_million, cache_read_per_million, cache_write_5m_per_million,
			cache_write_1h_per_million, output_per_million,
			effective_from, status, published_by, published_at
		) VALUES($1, 'gpt-stage3-canonical', 'any', 3, 0, 0, 0, 0,
			current_date-1, 'published', $2, now()) RETURNING id::text`, fixture.priceBookID, analyticsAdminID).Scan(&fixture.priceID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO usd_cny_rate_versions(rate, effective_from, status, published_by, published_at)
		VALUES(7, current_date-1, 'published', $1, now()) RETURNING id::text`, analyticsAdminID).Scan(&fixture.rateID); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (fixture *analyticsFixture) insertUsageComponent(t *testing.T, key string, userID, inputTokens int64) string {
	t.Helper()
	var observationID, eventID, componentID string
	hashCharacter := "b"
	if key == "second" {
		hashCharacter = "c"
	}
	if err := fixture.db.QueryRow(`
		INSERT INTO session_usage_observations(
			revision_id, generation_id, chunk_id, provider, source_start_cursor, source_end_cursor,
			occurred_at, raw_model, raw_usage_json, parsed_counters_json, raw_usage_hash,
			parser_version, quality_status
		) VALUES($1, $2, $3, 'codex', $4::bigint, $4::bigint+1, now(), 'gpt-stage3', '{}', '{}', repeat($5,64), 'stage3-parser', 'exact')
		RETURNING id::text`, fixture.revisionID, fixture.generationID, fixture.chunkID,
		map[string]int64{"first": 0, "second": 1}[key], hashCharacter).Scan(&observationID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.QueryRow(`
		INSERT INTO session_logical_usage_events(
			revision_id, generation_id, provider, usage_event_key, identity_strategy,
			current_observation_id, logical_occurred_at, logical_raw_model, provider_event_fingerprint
		) VALUES($1, $2, 'codex', $3, 'message.id', $4, now(), 'gpt-stage3', $3)
		RETURNING id::text`, fixture.revisionID, fixture.generationID, key, observationID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.QueryRow(`
		INSERT INTO session_usage_components(
			revision_id, logical_usage_event_id, observation_id, chunk_id, session_id, user_id,
			activity_date, occurred_at, provider, raw_model, canonical_model, billing_variant,
			uncached_input_tokens, normalized_total_tokens, normalization_strategy,
			quality_status, is_estimated
		) VALUES($1, $2, $3, $4, $5, $6, current_date, now(), 'codex', 'gpt-stage3',
			'gpt-stage3', 'unknown', $7, $7, 'stage3-exact', 'exact', false)
		RETURNING id::text`, fixture.revisionID, eventID, observationID, fixture.chunkID,
		fixture.sessionID, userID, inputTokens).Scan(&componentID); err != nil {
		t.Fatal(err)
	}
	return componentID
}

func (fixture *analyticsFixture) insertPendingSource(t *testing.T) {
	t.Helper()
	var sessionID, sourceID, generationID string
	if err := fixture.db.QueryRow(`
		INSERT INTO sessions(session_ref, user_id, agent_type, started_at, last_activity_at, summary)
		VALUES('stage3-pending-session', $1, 'codex', now(), now(), 'pending source without components')
		RETURNING id::text`, analyticsEmployeeID).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.QueryRow(`
		INSERT INTO session_sources(session_id, source_role, source_key)
		VALUES($1, 'primary', 'stage3-pending-source') RETURNING id::text`, sessionID).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.QueryRow(`
		INSERT INTO session_source_generations(source_id, status)
		VALUES($1, 'active') RETURNING id::text`, sourceID).Scan(&generationID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(`UPDATE session_sources SET active_generation_id=$1 WHERE id=$2`, generationID, sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(`
		INSERT INTO session_upload_chunks(
			generation_id, start_cursor, end_cursor, start_line, end_line,
			content_sha256, content_epoch, raw_object_key, object_status,
			content_index_status, usage_parse_status, event_start_at, event_end_at
		) VALUES($1, 0, 10, 1, 1, repeat('d',64), 0, 'stage3-pending-object',
			'available', 'indexed', 'pending', now(), now())`, generationID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(`
		INSERT INTO session_source_metrics_states(
			source_id, target_generation_id, status,
			active_usage_parsed_cursor, source_high_water_cursor
		) VALUES($1, $2, 'pending', 0, 10)`, sourceID, generationID); err != nil {
		t.Fatal(err)
	}
}

func (fixture *analyticsFixture) publishPriceCorrection(t *testing.T, inputPrice string) {
	t.Helper()
	tx, err := fixture.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE model_price_versions SET superseded_at=now() WHERE id=$1`, fixture.priceID); err != nil {
		t.Fatal(err)
	}
	var newPriceID string
	if err := tx.QueryRow(`
		INSERT INTO model_price_versions(
			price_book_id, canonical_model, billing_variant,
			input_per_million, cache_read_per_million, cache_write_5m_per_million,
			cache_write_1h_per_million, output_per_million,
			effective_from, status, published_by, published_at, supersedes_id
		) VALUES($1, 'gpt-stage3-canonical', 'any', $2::numeric, 0, 0, 0, 0,
			current_date-1, 'published', $3, now(), $4) RETURNING id::text`,
		fixture.priceBookID, inputPrice, analyticsAdminID, fixture.priceID).Scan(&newPriceID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	fixture.priceID = newPriceID
}

func (fixture *analyticsFixture) cleanup(t *testing.T) {
	t.Helper()
	_, _ = fixture.db.Exec(`DELETE FROM token_query_snapshots WHERE user_id BETWEEN $1 AND $2`, analyticsAdminID, analyticsPMID)
	_, _ = fixture.db.Exec(`DELETE FROM pricing_recalculation_runs WHERE requested_by BETWEEN $1 AND $2`, analyticsAdminID, analyticsPMID)
	_, _ = fixture.db.Exec(`DELETE FROM sessions WHERE user_id BETWEEN $1 AND $2`, analyticsAdminID, analyticsPMID)
	_, _ = fixture.db.Exec(`DELETE FROM model_aliases WHERE provider='codex' AND raw_model_pattern='gpt-stage3'`)
	_, _ = fixture.db.Exec(`DELETE FROM model_price_versions WHERE price_book_id IN (SELECT id FROM price_books WHERE name='stage3-official-prices')`)
	_, _ = fixture.db.Exec(`DELETE FROM usd_cny_rate_versions WHERE published_by BETWEEN $1 AND $2`, analyticsAdminID, analyticsPMID)
	_, _ = fixture.db.Exec(`DELETE FROM price_books WHERE name='stage3-official-prices'`)
	_, _ = fixture.db.Exec(`UPDATE departments SET director_user_id=NULL WHERE director_user_id BETWEEN $1 AND $2`, analyticsAdminID, analyticsPMID)
	_, _ = fixture.db.Exec(`DELETE FROM users WHERE id BETWEEN $1 AND $2`, analyticsAdminID, analyticsPMID)
	_, _ = fixture.db.Exec(`DELETE FROM teams WHERE name LIKE 'stage3-team-%'`)
	_, _ = fixture.db.Exec(`DELETE FROM departments WHERE name='stage3-department'`)
}

func assertComponentOrganization(t *testing.T, database *sql.DB, componentID, teamID, departmentID, source string) {
	t.Helper()
	var actualTeam, actualDepartment, actualSource string
	if err := database.QueryRow(`
		SELECT team_id_snapshot::text, department_id_snapshot::text, department_attribution_source
		FROM session_usage_components WHERE id=$1`, componentID).Scan(&actualTeam, &actualDepartment, &actualSource); err != nil {
		t.Fatal(err)
	}
	if actualTeam != teamID || actualDepartment != departmentID || actualSource != source {
		t.Fatalf("component organization team=%s department=%s source=%s", actualTeam, actualDepartment, actualSource)
	}
}

func assertActiveCost(t *testing.T, database *sql.DB, componentID, expectedUSD, expectedCNY string) {
	t.Helper()
	var status, usd, cny string
	if err := database.QueryRow(`
		SELECT pricing_status, estimated_cost_usd::text, estimated_cost_cny::text
		FROM session_activity_costs
		WHERE usage_component_id=$1 AND superseded_at IS NULL`, componentID).Scan(&status, &usd, &cny); err != nil {
		t.Fatal(err)
	}
	if status != "priced" || usd != expectedUSD || cny != expectedCNY {
		t.Fatalf("cost status=%s usd=%s cny=%s", status, usd, cny)
	}
}

func openAnalyticsIntegrationDatabase(t *testing.T) *sql.DB {
	t.Helper()
	databaseURL := os.Getenv("AIDA_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("AIDA_TEST_DATABASE_URL is not set")
	}
	database, err := projectdb.Connect(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := projectdb.RunMigrations(database); err != nil {
		t.Fatal(err)
	}
	return database
}
