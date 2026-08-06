package reportmemory

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"path"
	"strconv"
	"strings"
	"time"
)

const workspaceIdentityVersion = "report-workspace-identity/v1"

type WorkspaceEvidenceStats struct {
	IdentitiesObserved int
	EvidenceCreated    int
}

type workspaceSourceObservation struct {
	SelectionID   string
	SessionID     string
	SliceID       sql.NullString
	RevisionID    string
	StartCursor   int64
	EndCursor     int64
	StartedAt     time.Time
	EndedAt       time.Time
	CWD           string
	RepositoryKey string
}

// materializeWorkspaceEvidenceForReport is a shadow writer. It records the
// deterministic workspace identity behind a report's frozen source slices,
// but does not change Project Memory candidates or Report Context.
func materializeWorkspaceEvidenceForReport(ctx context.Context, database *sql.DB, userID, reportID string) (WorkspaceEvidenceStats, error) {
	var stats WorkspaceEvidenceStats
	if database == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(reportID) == "" {
		return stats, nil
	}
	var selectionID string
	if err := database.QueryRowContext(ctx, `
		SELECT selection.id::text
		FROM daily_reports report
		JOIN report_source_selections selection ON selection.attached_run_id = report.managed_agent_run_id
		WHERE report.id = $1 AND report.user_id = $2`, reportID, userID).Scan(&selectionID); err != nil {
		if err == sql.ErrNoRows {
			return stats, nil
		}
		return stats, err
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return stats, err
	}
	defer tx.Rollback()
	stats, err = ObserveSelectionWorkspaces(ctx, tx, userID, selectionID)
	if err != nil {
		return stats, err
	}
	return stats, tx.Commit()
}

// ObserveSelectionWorkspaces records deterministic workspace identity for a
// frozen source selection inside the caller's transaction. It is deliberately
// independent from Project Memory decisions.
func ObserveSelectionWorkspaces(ctx context.Context, tx *sql.Tx, userID, selectionID string) (WorkspaceEvidenceStats, error) {
	var stats WorkspaceEvidenceStats
	if tx == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(selectionID) == "" {
		return stats, nil
	}
	// Serialize identity resolution per user. This keeps concurrent report runs
	// from creating competing workspaces for the same CWD/repository keys.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "report-workspace:"+userID); err != nil {
		return stats, err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT selection.id::text, item.session_id::text,
		       item.session_content_slice_id::text,
		       item.content_projection_revision_id::text,
		       item.start_cursor, item.end_cursor,
		       item.activity_start_at, item.activity_end_at,
		       COALESCE(NULLIF(catalog.cwd, ''), NULLIF(session.cwd, ''), ''),
		       COALESCE(session.repository_key, '')
		FROM report_source_selections selection
		JOIN report_source_selection_items item ON item.selection_id = selection.id
		JOIN sessions session ON session.id = item.session_id
		LEFT JOIN report_source_slice_catalog catalog
		  ON catalog.slice_id = item.session_content_slice_id
		 AND catalog.content_projection_revision_id = item.content_projection_revision_id
		WHERE selection.id = $1 AND selection.user_id = $2
		ORDER BY item.activity_start_at, item.session_ref_snapshot, item.start_cursor`, selectionID, userID)
	if err != nil {
		return stats, err
	}
	defer rows.Close()
	observations := make([]workspaceSourceObservation, 0)
	for rows.Next() {
		var item workspaceSourceObservation
		if err := rows.Scan(
			&item.SelectionID, &item.SessionID, &item.SliceID, &item.RevisionID,
			&item.StartCursor, &item.EndCursor, &item.StartedAt, &item.EndedAt, &item.CWD, &item.RepositoryKey,
		); err != nil {
			return stats, err
		}
		item.RepositoryKey = strings.TrimSpace(item.RepositoryKey)
		if item.CWD = normalizeWorkspacePath(item.CWD); item.CWD != "" || validWorkspaceHash(item.RepositoryKey) {
			observations = append(observations, item)
		}
	}
	if err := rows.Err(); err != nil {
		return stats, err
	}
	if len(observations) == 0 {
		return stats, nil
	}

	seenIdentities := make(map[string]struct{})
	for _, item := range observations {
		keys := make([]workspaceIdentityKey, 0, 2)
		if validWorkspaceHash(item.RepositoryKey) {
			keys = append(keys, workspaceIdentityKey{Kind: "git_repository", Hash: workspaceHash("identity", userID, "git_repository", item.RepositoryKey)})
		}
		if item.CWD != "" {
			keys = append(keys, workspaceIdentityKey{Kind: "cwd", Hash: workspaceHash("identity", userID, "cwd", item.CWD)})
		}
		workspaceID, err := resolveOrCreateWorkspace(ctx, tx, userID, keys, item.StartedAt, item.EndedAt)
		if err != nil {
			return stats, err
		}
		seenIdentities[workspaceID] = struct{}{}
		for _, key := range keys {
			evidenceType := "cwd"
			if key.Kind == "git_repository" {
				evidenceType = "git_remote"
			}
			evidenceHash := workspaceHash(
				"evidence", userID, workspaceID, item.SessionID, item.RevisionID,
				strconv.FormatInt(item.StartCursor, 10), strconv.FormatInt(item.EndCursor, 10), key.Hash,
			)
			result, err := tx.ExecContext(ctx, `
			INSERT INTO report_workspace_evidence (
				user_id, workspace_id, evidence_hash, evidence_type,
				source_selection_id, source_session_id, source_slice_id,
				content_projection_revision_id, start_cursor, end_cursor,
				observed_from, observed_to
			) VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, '')::uuid, $8, $9, $10, $11, $12)
			ON CONFLICT (user_id, evidence_hash) DO NOTHING`,
				userID, workspaceID, evidenceHash, evidenceType, item.SelectionID, item.SessionID, item.SliceID.String,
				item.RevisionID, item.StartCursor, item.EndCursor, item.StartedAt, item.EndedAt,
			)
			if err != nil {
				return stats, err
			}
			if affected, err := result.RowsAffected(); err == nil {
				stats.EvidenceCreated += int(affected)
			}
		}
	}
	stats.IdentitiesObserved = len(seenIdentities)
	return stats, nil
}

type workspaceIdentityKey struct {
	Kind string
	Hash string
}

func resolveOrCreateWorkspace(ctx context.Context, tx *sql.Tx, userID string, keys []workspaceIdentityKey, startedAt, endedAt time.Time) (string, error) {
	var workspaceID string
	for _, key := range keys {
		err := tx.QueryRowContext(ctx, `
			SELECT workspace_id::text
			FROM report_workspace_keys
			WHERE user_id = $1 AND key_kind = $2 AND key_hash = $3`, userID, key.Kind, key.Hash).Scan(&workspaceID)
		if err == nil {
			break
		}
		if err != sql.ErrNoRows {
			return "", err
		}
	}
	if workspaceID == "" {
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO report_workspaces (user_id, first_seen_at, last_seen_at, resolver_version)
			VALUES ($1, $2, $3, $4)
			RETURNING id::text`, userID, startedAt, endedAt, workspaceIdentityVersion).Scan(&workspaceID); err != nil {
			return "", err
		}
	} else if _, err := tx.ExecContext(ctx, `
		UPDATE report_workspaces SET
			first_seen_at = LEAST(first_seen_at, $2),
			last_seen_at = GREATEST(last_seen_at, $3),
			updated_at = now()
		WHERE id = $1`, workspaceID, startedAt, endedAt); err != nil {
		return "", err
	}
	for _, key := range keys {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO report_workspace_keys (
				user_id, workspace_id, key_kind, key_hash, first_seen_at, last_seen_at
			) VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (user_id, key_kind, key_hash) DO UPDATE SET
				first_seen_at = LEAST(report_workspace_keys.first_seen_at, EXCLUDED.first_seen_at),
				last_seen_at = GREATEST(report_workspace_keys.last_seen_at, EXCLUDED.last_seen_at),
				updated_at = now()`, userID, workspaceID, key.Kind, key.Hash, startedAt, endedAt); err != nil {
			return "", err
		}
	}
	return workspaceID, nil
}

func normalizeWorkspacePath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, `\`, "/"))
	if value == "" || !strings.Contains(value, "/") {
		return ""
	}
	value = path.Clean(value)
	if len(value) >= 2 && value[1] == ':' {
		value = strings.ToLower(value[:1]) + value[1:]
	}
	if value == "." || value == "/" {
		return ""
	}
	return value
}

func workspaceHash(parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}

func validWorkspaceHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, current := range value {
		if (current < '0' || current > '9') && (current < 'a' || current > 'f') {
			return false
		}
	}
	return true
}
