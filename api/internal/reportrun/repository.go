package reportrun

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

const (
	StageWaitingDigest   = "waiting_digest"
	StageBuildingContext = "building_context"
	StageSubmittingAgent = "submitting_agent"
	StageAgentRunning    = "agent_running"
	StageWritingResult   = "writing_result"
	StageCompleted       = "completed"
)

var ErrLeaseLost = errors.New("report run execution lease was lost")

type Run struct {
	ID                string
	UserID            string
	BusinessType      string
	AgentID           string
	ModelID           string
	Status            string
	Stage             string
	StageAttempts     int
	LeaseOwner        string
	LeaseUntil        time.Time
	ExternalSessionID string
	InputRef          map[string]any
	ExecutionInput    map[string]any
}

type Repository struct {
	db *sql.DB
}

func NewRepository(database *sql.DB) (*Repository, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	return &Repository{db: database}, nil
}

func (r *Repository) Claim(
	ctx context.Context,
	owner string,
	now time.Time,
	leaseTTL time.Duration,
) (Run, bool, error) {
	if owner == "" || leaseTTL <= 0 {
		return Run{}, false, errors.New("owner and positive lease TTL are required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Run{}, false, err
	}
	defer tx.Rollback()
	var run Run
	var modelID, externalSessionID sql.NullString
	var inputJSON, executionJSON []byte
	err = tx.QueryRowContext(ctx, `
		SELECT id::text, user_id::text, business_type, agent_id, model_id,
			status, execution_stage, stage_attempts, external_session_id,
			input_ref_json, execution_input_json
		FROM ai_runs
		WHERE business_type = 'report_agent_run' AND status = 'pending'
			AND execution_stage IN ('waiting_digest', 'building_context', 'submitting_agent')
			AND next_attempt_at IS NOT NULL AND next_attempt_at <= $1
			AND (execution_lease_until IS NULL OR execution_lease_until <= $1)
		ORDER BY next_attempt_at, created_at, id
		FOR UPDATE SKIP LOCKED
		LIMIT 1`, now,
	).Scan(
		&run.ID, &run.UserID, &run.BusinessType, &run.AgentID, &modelID,
		&run.Status, &run.Stage, &run.StageAttempts, &externalSessionID,
		&inputJSON, &executionJSON,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, false, nil
	}
	if err != nil {
		return Run{}, false, err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE ai_runs
		SET execution_lease_owner = $2, execution_lease_until = $3
		WHERE id = $1 AND status = 'pending' AND execution_stage = $4
			AND (execution_lease_until IS NULL OR execution_lease_until <= $5)`,
		run.ID, owner, now.Add(leaseTTL), run.Stage, now,
	)
	if err != nil {
		return Run{}, false, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return Run{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Run{}, false, err
	}
	run.LeaseOwner = owner
	run.LeaseUntil = now.Add(leaseTTL)
	if modelID.Valid {
		run.ModelID = modelID.String
	}
	if externalSessionID.Valid {
		run.ExternalSessionID = externalSessionID.String
	}
	_ = json.Unmarshal(inputJSON, &run.InputRef)
	_ = json.Unmarshal(executionJSON, &run.ExecutionInput)
	return run, true, nil
}

func (r *Repository) Heartbeat(
	ctx context.Context,
	runID, owner, stage string,
	now time.Time,
	leaseTTL time.Duration,
) (bool, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE ai_runs
		SET execution_lease_until = $1
		WHERE id = $2 AND execution_lease_owner = $3 AND execution_stage = $4
			AND status = 'pending' AND execution_lease_until > $5`,
		now.Add(leaseTTL), runID, owner, stage, now,
	)
	return oneChanged(result, err)
}

func (r *Repository) Transition(
	ctx context.Context,
	run Run,
	nextStage string,
	status string,
	startedAt bool,
) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE ai_runs
		SET execution_stage = $4, status = $5, stage_updated_at = now(),
			stage_attempts = 0, next_attempt_at = CASE WHEN $4 IN ('agent_running', 'completed') THEN NULL ELSE now() END,
			execution_lease_owner = NULL, execution_lease_until = NULL,
			started_at = CASE WHEN $6 THEN COALESCE(started_at, now()) ELSE started_at END
		WHERE id = $1 AND execution_lease_owner = $2 AND execution_stage = $3
			AND status = 'pending'`,
		run.ID, run.LeaseOwner, run.Stage, nextStage, status, startedAt,
	)
	ok, err := oneChanged(result, err)
	if err != nil {
		return err
	}
	if !ok {
		return ErrLeaseLost
	}
	return nil
}

func (r *Repository) WaitForDigest(ctx context.Context, run Run) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE ai_runs
		SET next_attempt_at = NULL, execution_lease_owner = NULL, execution_lease_until = NULL
		WHERE id = $1 AND execution_lease_owner = $2 AND execution_stage = $3
			AND status = 'pending'`, run.ID, run.LeaseOwner, run.Stage)
	ok, err := oneChanged(result, err)
	if err != nil {
		return err
	}
	if !ok {
		return ErrLeaseLost
	}
	return nil
}

func (r *Repository) StoreContextAndTransition(
	ctx context.Context,
	run Run,
	hash string,
	bytes int,
) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE ai_runs
		SET input_ref_json = input_ref_json || jsonb_build_object(
				'report_context_schema_version', 'report-context/v1',
				'report_context_hash', $4,
				'report_context_bytes', $5::integer
			) || CASE WHEN $5::integer > 1048576
				THEN jsonb_build_object('large_context_warning', true)
				ELSE '{}'::jsonb END,
			execution_stage = 'submitting_agent', stage_updated_at = now(),
			stage_attempts = 0, next_attempt_at = now(),
			execution_lease_owner = NULL, execution_lease_until = NULL
		WHERE id = $1 AND execution_lease_owner = $2 AND execution_stage = $3
			AND status = 'pending'`, run.ID, run.LeaseOwner, run.Stage, hash, bytes)
	ok, err := oneChanged(result, err)
	if err != nil {
		return err
	}
	if !ok {
		return ErrLeaseLost
	}
	return nil
}

func (r *Repository) StoreExternalSessionAndTransition(
	ctx context.Context,
	run Run,
	sessionID, modelID string,
) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE ai_runs
		SET external_session_id = COALESCE(external_session_id, $4),
			model_id = COALESCE(NULLIF($5, ''), model_id), status = 'running',
			execution_stage = 'agent_running', stage_updated_at = now(),
			stage_attempts = 0, next_attempt_at = NULL,
			execution_lease_owner = NULL, execution_lease_until = NULL,
			started_at = COALESCE(started_at, now())
		WHERE id = $1 AND execution_lease_owner = $2 AND execution_stage = $3
			AND status = 'pending' AND (external_session_id IS NULL OR external_session_id = $4)`,
		run.ID, run.LeaseOwner, run.Stage, sessionID, modelID)
	ok, err := oneChanged(result, err)
	if err != nil {
		return err
	}
	if !ok {
		return ErrLeaseLost
	}
	return nil
}

func (r *Repository) RetryStage(
	ctx context.Context,
	run Run,
	errorCode, message string,
) error {
	nextAttempt := run.StageAttempts + 1
	if nextAttempt >= 5 {
		return r.Fail(ctx, run, "failed", errorCode, message)
	}
	delays := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second}
	result, err := r.db.ExecContext(ctx, `
		UPDATE ai_runs
		SET stage_attempts = stage_attempts + 1, next_attempt_at = $4,
			error_code = $5, error_message = $6,
			execution_lease_owner = NULL, execution_lease_until = NULL
		WHERE id = $1 AND execution_lease_owner = $2 AND execution_stage = $3
			AND status = 'pending'`, run.ID, run.LeaseOwner, run.Stage,
		time.Now().UTC().Add(delays[run.StageAttempts]), errorCode, message)
	ok, err := oneChanged(result, err)
	if err != nil {
		return err
	}
	if !ok {
		return ErrLeaseLost
	}
	return nil
}

func (r *Repository) Fail(
	ctx context.Context,
	run Run,
	status, errorCode, message string,
) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE ai_runs
		SET status = $4, execution_stage = 'completed', stage_updated_at = now(),
			failure_stage = $3, error_code = $5, error_message = $6,
			finished_at = now(), next_attempt_at = NULL,
			execution_lease_owner = NULL, execution_lease_until = NULL
		WHERE id = $1 AND execution_lease_owner = $2 AND execution_stage = $3
			AND status = 'pending'`,
		run.ID, run.LeaseOwner, run.Stage, status, errorCode, message)
	ok, err := oneChanged(result, err)
	if err != nil {
		return err
	}
	if !ok {
		return ErrLeaseLost
	}
	return nil
}

func (r *Repository) DB() *sql.DB { return r.db }

func oneChanged(result sql.Result, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}
