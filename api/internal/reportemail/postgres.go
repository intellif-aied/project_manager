package reportemail

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(database *sql.DB) (*PostgresStore, error) {
	if database == nil {
		return nil, errors.New("report email database is required")
	}
	return &PostgresStore{db: database}, nil
}

func (store *PostgresStore) ListPersonalCandidates(ctx context.Context, date time.Time) ([]PersonalCandidate, error) {
	rows, err := store.db.QueryContext(ctx, `
		SELECT u.id::text,
		       COALESCE(NULLIF(u.nickname, ''), NULLIF(u.name, ''), NULLIF(u.username, ''), u.id::text),
		       COALESCE(recipient.email, ''),
		       COALESCE(NULLIF(report.content, ''), NULLIF(report.submitted_content, ''), '')
		FROM users u
		LEFT JOIN report_email_recipients recipient
		  ON recipient.user_id = u.id AND recipient.enabled = true
		LEFT JOIN daily_reports report
		  ON report.user_id = u.id AND report.report_date = $1::date
		WHERE u.status = 'active'
		ORDER BY u.id`, date.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var candidates []PersonalCandidate
	for rows.Next() {
		var candidate PersonalCandidate
		if err := rows.Scan(&candidate.UserID, &candidate.DisplayName, &candidate.Email, &candidate.Content); err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	return candidates, rows.Err()
}

func (store *PostgresStore) ListTeamCandidates(ctx context.Context, date time.Time) ([]TeamCandidate, error) {
	rows, err := store.db.QueryContext(ctx, `
		SELECT leader.team_id::text, team.name, leader.id::text,
		       COALESCE(NULLIF(leader.nickname, ''), NULLIF(leader.name, ''), NULLIF(leader.username, ''), leader.id::text),
		       COALESCE(recipient.email, ''),
		       COALESCE(NULLIF(member.nickname, ''), NULLIF(member.name, ''), NULLIF(member.username, ''), member.id::text),
		       COALESCE(NULLIF(report.content, ''), NULLIF(report.submitted_content, ''), '')
		FROM users leader
		JOIN teams team ON team.id = leader.team_id
		LEFT JOIN report_email_recipients recipient
		  ON recipient.user_id = leader.id AND recipient.enabled = true
		JOIN users member ON member.team_id = leader.team_id AND member.status = 'active'
		LEFT JOIN daily_reports report
		  ON report.user_id = member.id AND report.report_date = $1::date
		WHERE leader.status = 'active' AND leader.app_role = 'team_leader'
		ORDER BY leader.id, member.id`, date.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var candidates []TeamCandidate
	var current *TeamCandidate
	for rows.Next() {
		var teamID, teamName, leaderID, leaderName, leaderEmail, memberName, content string
		if err := rows.Scan(&teamID, &teamName, &leaderID, &leaderName, &leaderEmail, &memberName, &content); err != nil {
			return nil, err
		}
		if current == nil || current.LeaderUserID != leaderID {
			candidates = append(candidates, TeamCandidate{TeamID: teamID, TeamName: teamName, LeaderUserID: leaderID, LeaderName: leaderName, LeaderEmail: leaderEmail})
			current = &candidates[len(candidates)-1]
		}
		current.Members = append(current.Members, TeamMember{DisplayName: memberName, Content: content})
	}
	return candidates, rows.Err()
}

func (store *PostgresStore) CreateDelivery(ctx context.Context, delivery Delivery) error {
	_, err := store.db.ExecContext(ctx, `
		INSERT INTO report_email_deliveries (
			report_date, delivery_type, recipient_user_id, team_id, recipient_email,
			subject, text_body, html_body, status, next_attempt_at, last_error
		) VALUES ($1::date, $2, $3::bigint, NULLIF($4, '')::uuid, lower(btrim($5)),
		          $6, $7, $8, $9, CASE WHEN $9 = 'pending' THEN now() ELSE NULL END, NULLIF($10, ''))
		ON CONFLICT (report_date, delivery_type, recipient_user_id) DO UPDATE SET
			recipient_email = EXCLUDED.recipient_email,
			subject = EXCLUDED.subject,
			text_body = EXCLUDED.text_body,
			html_body = EXCLUDED.html_body,
			status = EXCLUDED.status,
			next_attempt_at = EXCLUDED.next_attempt_at,
			last_error = EXCLUDED.last_error,
			updated_at = now()
		WHERE report_email_deliveries.status = 'skipped'`,
		delivery.ReportDate.Format("2006-01-02"), delivery.Type, delivery.RecipientUserID,
		delivery.TeamID, delivery.RecipientEmail, delivery.Subject, delivery.TextBody,
		delivery.HTMLBody, delivery.Status, delivery.SkipReason)
	return err
}

func (store *PostgresStore) ClaimDelivery(ctx context.Context, now time.Time, workerID string, leaseTTL time.Duration) (Delivery, bool, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return Delivery{}, false, err
	}
	defer tx.Rollback()
	var delivery Delivery
	err = tx.QueryRowContext(ctx, `
		SELECT id::text, report_date, delivery_type, recipient_user_id::text,
		       COALESCE(team_id::text, ''), recipient_email, subject, text_body, html_body, attempts
		FROM report_email_deliveries
		WHERE attempts < 3 AND (
			(status IN ('pending', 'failed') AND (next_attempt_at IS NULL OR next_attempt_at <= $1))
			OR (status = 'sending' AND (lease_until IS NULL OR lease_until <= $1))
		)
		ORDER BY report_date, delivery_type, recipient_user_id
		FOR UPDATE SKIP LOCKED LIMIT 1`, now).Scan(
		&delivery.ID, &delivery.ReportDate, &delivery.Type, &delivery.RecipientUserID,
		&delivery.TeamID, &delivery.RecipientEmail, &delivery.Subject, &delivery.TextBody,
		&delivery.HTMLBody, &delivery.Attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return Delivery{}, false, nil
	}
	if err != nil {
		return Delivery{}, false, err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE report_email_deliveries
		SET status = 'sending', attempts = attempts + 1, lease_owner = $2,
		    lease_until = $3, next_attempt_at = NULL, last_error = NULL, updated_at = now()
		WHERE id = $1::uuid`, delivery.ID, workerID, now.Add(leaseTTL))
	if err != nil {
		return Delivery{}, false, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return Delivery{}, false, fmt.Errorf("report email delivery lease was lost")
	}
	delivery.Attempts++
	return delivery, true, tx.Commit()
}

func (store *PostgresStore) MarkSent(ctx context.Context, deliveryID, workerID string, now time.Time) error {
	result, err := store.db.ExecContext(ctx, `
		UPDATE report_email_deliveries
		SET status = 'sent', sent_at = $3, lease_owner = NULL, lease_until = NULL,
		    next_attempt_at = NULL, last_error = NULL, updated_at = now()
		WHERE id = $1::uuid AND status = 'sending' AND lease_owner = $2`, deliveryID, workerID, now)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errors.New("report email delivery lease was lost before success")
	}
	return nil
}

func (store *PostgresStore) MarkFailed(ctx context.Context, deliveryID, workerID string, now time.Time, retryDelay time.Duration, sendErr error) error {
	result, err := store.db.ExecContext(ctx, `
		UPDATE report_email_deliveries
		SET status = 'failed', lease_owner = NULL, lease_until = NULL,
		    next_attempt_at = CASE WHEN attempts < 3 THEN $3 ELSE NULL END,
		    last_error = left($4, 2000), updated_at = now()
		WHERE id = $1::uuid AND status = 'sending' AND lease_owner = $2`,
		deliveryID, workerID, now.Add(retryDelay), sendErr.Error())
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errors.New("report email delivery lease was lost before failure")
	}
	return nil
}
