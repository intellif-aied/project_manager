package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	projectdb "github.com/aidashboard/api/db"
)

type planItem struct {
	SessionID    string `json:"session_id"`
	SessionRef   string `json:"session_ref"`
	AgentType    string `json:"agent_type"`
	CWD          string `json:"cwd"`
	SourceUserID string `json:"source_user_id"`
	TargetUserID string `json:"target_user_id,omitempty"`
	MatchedPath  string `json:"matched_path,omitempty"`
	Status       string `json:"status"`
}

type planOutput struct {
	TeamID       string         `json:"team_id"`
	SourceUserID string         `json:"source_user_id"`
	Apply        bool           `json:"apply"`
	Counts       map[string]int `json:"counts"`
	Items        []planItem     `json:"items"`
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func main() {
	var databaseURL, teamID, sourceUserID string
	var apply bool
	flag.StringVar(&databaseURL, "database-url", os.Getenv("DATABASE_URL"), "PostgreSQL connection URL")
	flag.StringVar(&teamID, "team-id", "", "team UUID")
	flag.StringVar(&sourceUserID, "source-user-id", "", "personal account that previously uploaded team sessions")
	flag.BoolVar(&apply, "apply", false, "apply the checked reassignment in one transaction")
	flag.Parse()
	if strings.TrimSpace(databaseURL) == "" || strings.TrimSpace(teamID) == "" || strings.TrimSpace(sourceUserID) == "" {
		fmt.Fprintln(os.Stderr, "database-url, team-id, and source-user-id are required")
		os.Exit(2)
	}
	database, err := projectdb.Connect(databaseURL)
	if err != nil {
		fatal(err)
	}
	defer database.Close()
	ctx := context.Background()
	items, err := buildPlan(ctx, database, teamID, sourceUserID)
	if err != nil {
		fatal(err)
	}
	if apply {
		items, err = applyPlan(ctx, database, teamID, sourceUserID)
		if err != nil {
			fatal(err)
		}
	}
	counts := map[string]int{}
	for _, item := range items {
		counts[item.Status]++
	}
	if err := json.NewEncoder(os.Stdout).Encode(planOutput{
		TeamID: teamID, SourceUserID: sourceUserID, Apply: apply, Counts: counts, Items: items,
	}); err != nil {
		fatal(err)
	}
}

func buildPlan(ctx context.Context, database queryer, teamID, sourceUserID string) ([]planItem, error) {
	rows, err := database.QueryContext(ctx, `
		SELECT s.id, s.session_ref, s.agent_type, COALESCE(s.cwd, ''), s.user_id,
			COALESCE(match.user_id::text, ''), COALESCE(match.normalized_path, ''),
			CASE
				WHEN match.user_id IS NULL THEN 'unmapped'
				WHEN match.user_id = s.user_id THEN 'unchanged'
				WHEN EXISTS (
					SELECT 1 FROM sessions target
					WHERE target.user_id = match.user_id
						AND target.agent_type = s.agent_type AND target.session_ref = s.session_ref
				) THEN 'identity_conflict'
				ELSE 'ready'
			END
		FROM sessions s
		LEFT JOIN LATERAL (
			SELECT p.user_id, p.normalized_path
			FROM team_sync_paths p
			JOIN users owner ON owner.id = p.user_id AND owner.team_id = p.team_id
			WHERE p.team_id = $1 AND s.cwd IS NOT NULL
				AND (s.cwd = p.normalized_path OR s.cwd LIKE p.normalized_path || '/%')
			ORDER BY length(p.normalized_path) DESC
			LIMIT 1
		) match ON true
		WHERE s.user_id = $2
		ORDER BY s.started_at, s.id`, teamID, sourceUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []planItem{}
	for rows.Next() {
		var item planItem
		if err := rows.Scan(
			&item.SessionID, &item.SessionRef, &item.AgentType, &item.CWD, &item.SourceUserID,
			&item.TargetUserID, &item.MatchedPath, &item.Status,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	bySession := make(map[string]*planItem, len(items))
	for index := range items {
		bySession[items[index].SessionID] = &items[index]
	}
	familyRows, err := database.QueryContext(ctx, `
		SELECT membership.root_session_id, membership.member_session_id, member.user_id
		FROM session_family_memberships membership
		JOIN sessions root ON root.id = membership.root_session_id
		JOIN sessions member ON member.id = membership.member_session_id
		WHERE membership.valid_to IS NULL
			AND (root.user_id = $1 OR member.user_id = $1)
		ORDER BY membership.root_session_id, membership.depth`, sourceUserID)
	if err != nil {
		return nil, err
	}
	defer familyRows.Close()
	type familyState struct {
		targets map[string]bool
		invalid bool
		members []string
	}
	families := map[string]*familyState{}
	for familyRows.Next() {
		var rootID, memberID, ownerID string
		if err := familyRows.Scan(&rootID, &memberID, &ownerID); err != nil {
			return nil, err
		}
		family := families[rootID]
		if family == nil {
			family = &familyState{targets: map[string]bool{}}
			families[rootID] = family
		}
		family.members = append(family.members, memberID)
		if item := bySession[memberID]; item != nil {
			switch item.Status {
			case "ready":
				family.targets[item.TargetUserID] = true
			case "unchanged":
				family.targets[item.SourceUserID] = true
			default:
				family.invalid = true
			}
		} else {
			family.targets[ownerID] = true
		}
	}
	if err := familyRows.Err(); err != nil {
		return nil, err
	}
	for _, family := range families {
		if !family.invalid && len(family.targets) == 1 {
			continue
		}
		for _, sessionID := range family.members {
			if item := bySession[sessionID]; item != nil && item.Status == "ready" {
				item.Status = "family_conflict"
			}
		}
	}
	return items, nil
}

func applyPlan(ctx context.Context, database *sql.DB, teamID, sourceUserID string) ([]planItem, error) {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "team-session-reassign:"+teamID); err != nil {
		return nil, err
	}
	items, err := buildPlan(ctx, tx, teamID, sourceUserID)
	if err != nil {
		return nil, err
	}
	ready := make([]planItem, 0)
	for _, item := range items {
		if item.Status == "ready" {
			ready = append(ready, item)
		}
	}
	sort.Slice(ready, func(i, j int) bool { return ready[i].SessionID < ready[j].SessionID })
	for _, item := range ready {
		var lockedOwner string
		if err := tx.QueryRowContext(ctx, `
			SELECT user_id FROM sessions WHERE id = $1 AND user_id = $2 FOR UPDATE`,
			item.SessionID, sourceUserID).Scan(&lockedOwner); err != nil {
			return nil, err
		}
		var stagingCount int
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM session_sources
			WHERE session_id = $1 AND staging_generation_id IS NOT NULL`, item.SessionID).Scan(&stagingCount); err != nil {
			return nil, err
		}
		if stagingCount > 0 {
			return nil, fmt.Errorf("session %s has a staging generation", item.SessionID)
		}
	}
	for _, item := range ready {
		if err := reassignSession(ctx, tx, item); err != nil {
			return nil, err
		}
	}
	if len(ready) > 0 {
		userIDs := []any{sourceUserID}
		seen := map[string]bool{sourceUserID: true}
		for _, item := range ready {
			if !seen[item.TargetUserID] {
				seen[item.TargetUserID] = true
				userIDs = append(userIDs, item.TargetUserID)
			}
		}
		for _, userID := range userIDs {
			if _, err := tx.ExecContext(ctx, `DELETE FROM token_query_snapshots WHERE user_id = $1`, userID); err != nil {
				return nil, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	for index := range items {
		if items[index].Status == "ready" {
			items[index].Status = "migrated"
		}
	}
	return items, nil
}

func reassignSession(ctx context.Context, tx *sql.Tx, item planItem) error {
	updates := []struct {
		query string
		args  []any
	}{
		{`UPDATE token_usage SET user_id = $2 WHERE session_id = $1`, []any{item.SessionID, item.TargetUserID}},
		{`UPDATE session_activity_slices SET user_id = $2 WHERE session_id = $1`, []any{item.SessionID, item.TargetUserID}},
		{`UPDATE session_usage_components SET user_id = $2 WHERE session_id = $1`, []any{item.SessionID, item.TargetUserID}},
		{`UPDATE session_daily_usage SET user_id = $2 WHERE session_id = $1`, []any{item.SessionID, item.TargetUserID}},
		{`UPDATE report_source_slice_catalog SET user_id = $2 WHERE session_id = $1`, []any{item.SessionID, item.TargetUserID}},
		{`UPDATE session_usage_contributions SET user_id = $2 WHERE member_session_id = $1`, []any{item.SessionID, item.TargetUserID}},
		{`UPDATE session_usage_event_claims claims SET user_id = $2 FROM session_sources src WHERE claims.active_source_id = src.id AND src.session_id = $1`, []any{item.SessionID, item.TargetUserID}},
		{`UPDATE session_family_versions SET user_id = $2 WHERE root_session_id = $1`, []any{item.SessionID, item.TargetUserID}},
		{`UPDATE session_family_token_totals SET user_id = $2 WHERE root_session_id = $1`, []any{item.SessionID, item.TargetUserID}},
		{`UPDATE session_family_daily_usage SET user_id = $2 WHERE root_session_id = $1`, []any{item.SessionID, item.TargetUserID}},
		{`UPDATE session_chunk_usage SET user_id = $2 WHERE root_session_id = $1`, []any{item.SessionID, item.TargetUserID}},
		{`UPDATE sessions SET user_id = $2, updated_at = now() WHERE id = $1 AND user_id = $3`, []any{item.SessionID, item.TargetUserID, item.SourceUserID}},
	}
	for _, update := range updates {
		if _, err := tx.ExecContext(ctx, update.query, update.args...); err != nil {
			return fmt.Errorf("reassign session %s: %w", item.SessionID, err)
		}
	}
	return nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
