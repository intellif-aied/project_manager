package reportreview

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/aidashboard/api/internal/reportbrief"
	"github.com/aidashboard/api/internal/reportcontext"
)

const (
	defaultPollInterval      = 10 * time.Second
	defaultLeaseTTL          = 3 * time.Minute
	defaultClaimBatch        = 5
	finalizingRetryDelay     = 30 * time.Second
	maxReviewAttempts        = 1
	maxReviewDuration        = 20 * time.Minute
	maxExpandedReviewPatches = 8
)

type contextReader interface {
	Get(context.Context, string, string) (reportcontext.StoredContext, error)
}

type Service struct {
	db        *sql.DB
	context   contextReader
	resolver  Resolver
	finalizer Finalizer
	config    Config
}

type queuedJob struct {
	RunID          string
	UserID         string
	BriefHash      string
	ContextHash    string
	Status         string
	ExternalTaskID string
	InputJSON      []byte
	DecisionJSON   []byte
	FinalBriefJSON []byte
	FinalMode      string
	Attempts       int
	StartedAt      time.Time
}

func NewService(database *sql.DB, contextReader contextReader, resolver Resolver, finalizer Finalizer, config Config) (*Service, error) {
	config = normalizeConfig(config)
	if !config.Enabled {
		return &Service{db: database, context: contextReader, resolver: resolver, finalizer: finalizer, config: config}, nil
	}
	if database == nil || contextReader == nil || resolver == nil || finalizer == nil ||
		strings.TrimSpace(config.AgentID) == "" || strings.TrimSpace(config.WorkerID) == "" {
		return nil, errors.New("enabled report review requires database, context, resolver, finalizer, agent ID, and worker ID")
	}
	return &Service{db: database, context: contextReader, resolver: resolver, finalizer: finalizer, config: config}, nil
}

func normalizeConfig(config Config) Config {
	config.AgentID = strings.TrimSpace(config.AgentID)
	config.ModelID = strings.TrimSpace(config.ModelID)
	config.WorkerID = strings.TrimSpace(config.WorkerID)
	if config.PollInterval <= 0 {
		config.PollInterval = defaultPollInterval
	}
	if config.LeaseTTL <= 0 {
		config.LeaseTTL = defaultLeaseTTL
	}
	if config.ClaimBatch <= 0 || config.ClaimBatch > 10 {
		config.ClaimBatch = defaultClaimBatch
	}
	return config
}

func (service *Service) Enabled() bool {
	return service != nil && service.config.Enabled
}

func JobRef(runID, briefHash string) string {
	return strings.TrimSpace(runID) + ":" + strings.TrimSpace(briefHash)
}

func parseJobRef(value string) (string, string, error) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || len(strings.TrimSpace(parts[1])) != 64 {
		return "", "", errors.New("invalid report review job ref")
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), nil
}

func (service *Service) Queue(ctx context.Context, userID, runID string, candidate reportbrief.Stored) (QueueResult, error) {
	if !service.Enabled() {
		return QueueResult{}, errors.New("report review is disabled")
	}
	storedContext, err := service.context.Get(ctx, userID, runID)
	if err != nil {
		return QueueResult{}, err
	}
	_, inputPayload, err := BuildInput(runID, storedContext, candidate)
	if err != nil {
		return QueueResult{}, err
	}
	tx, err := service.db.BeginTx(ctx, nil)
	if err != nil {
		return QueueResult{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		INSERT INTO report_review_jobs (
			run_id, user_id, brief_hash, context_hash, status, due_at, input_json
		) VALUES ($1::uuid, $2, $3, $4, 'pending', now(), $5::jsonb)
		ON CONFLICT (run_id) DO NOTHING`, runID, userID, candidate.BriefHash, candidate.ContextHash, inputPayload)
	if err != nil {
		return QueueResult{}, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return QueueResult{}, err
	}
	if inserted == 0 {
		var existingHash, existingContext, status string
		if err := tx.QueryRowContext(ctx, `
			SELECT brief_hash, context_hash, status FROM report_review_jobs
			WHERE run_id = $1::uuid AND user_id = $2`, runID, userID).
			Scan(&existingHash, &existingContext, &status); err != nil {
			return QueueResult{}, err
		}
		if existingHash != candidate.BriefHash || existingContext != candidate.ContextHash {
			return QueueResult{}, errors.New("report review job conflicts with candidate")
		}
		if err := tx.Commit(); err != nil {
			return QueueResult{}, err
		}
		return QueueResult{RunID: runID, JobRef: JobRef(runID, existingHash), BriefHash: existingHash, Status: status}, nil
	}
	stageResult, err := tx.ExecContext(ctx, `
		UPDATE ai_runs SET execution_stage = 'review_pending', stage_updated_at = now(),
			next_attempt_at = NULL, execution_lease_owner = NULL, execution_lease_until = NULL
		WHERE id = $1::uuid AND user_id = $2 AND status = 'running'
		  AND execution_stage = 'agent_running'`, runID, userID)
	if err != nil {
		return QueueResult{}, err
	}
	if changed, _ := stageResult.RowsAffected(); changed != 1 {
		return QueueResult{}, errors.New("report run is not ready for review")
	}
	if err := tx.Commit(); err != nil {
		return QueueResult{}, err
	}
	return QueueResult{RunID: runID, JobRef: JobRef(runID, candidate.BriefHash), BriefHash: candidate.BriefHash, Status: "pending"}, nil
}

func (service *Service) GetContext(ctx context.Context, userID, jobRef string) (Input, error) {
	runID, briefHash, err := parseJobRef(jobRef)
	if err != nil {
		return Input{}, err
	}
	var payload []byte
	err = service.db.QueryRowContext(ctx, `
		SELECT input_json FROM report_review_jobs
		WHERE run_id = $1::uuid AND user_id = $2 AND brief_hash = $3
		  AND status IN ('submitting', 'running', 'finalizing')`, runID, userID, briefHash).Scan(&payload)
	if err != nil {
		return Input{}, err
	}
	var input Input
	if err := json.Unmarshal(payload, &input); err != nil {
		return Input{}, err
	}
	return input, nil
}

func (service *Service) WriteDecision(ctx context.Context, userID, jobRef string, decision reportbrief.ReviewDecision) (reportbrief.ReviewFinalized, error) {
	runID, briefHash, err := parseJobRef(jobRef)
	if err != nil {
		return reportbrief.ReviewFinalized{}, err
	}
	job, input, err := service.loadJobInput(ctx, userID, runID, briefHash)
	if err != nil {
		return reportbrief.ReviewFinalized{}, err
	}
	allowed := make(map[string]struct{}, len(input.AllowedFactRefs))
	for _, ref := range input.AllowedFactRefs {
		allowed[ref] = struct{}{}
	}
	candidate := reportbrief.Stored{Payload: input.Candidate, BriefHash: input.BriefHash, ContextHash: input.ContextHash}
	decision, err = expandProjectAttachments(decision, input)
	if err != nil {
		return reportbrief.ReviewFinalized{}, err
	}
	finalized, err := reportbrief.FinalizeReview(candidate, decision, allowed)
	if err != nil {
		return reportbrief.ReviewFinalized{}, err
	}
	decisionPayload, err := json.Marshal(decision)
	if err != nil {
		return reportbrief.ReviewFinalized{}, err
	}
	finalPayload, err := json.Marshal(finalized.Stored.Payload)
	if err != nil {
		return reportbrief.ReviewFinalized{}, err
	}
	result, err := service.db.ExecContext(ctx, `
		UPDATE report_review_jobs SET status = 'finalizing', decision_json = $1::jsonb,
			final_brief_json = $2::jsonb, finalization_mode = $3,
			lease_owner = $4, lease_until = $5, updated_at = now()
		WHERE run_id = $6::uuid AND user_id = $7 AND brief_hash = $8
		  AND status IN ('running', 'submitting')`, decisionPayload, finalPayload, finalized.Mode,
		service.config.WorkerID, time.Now().Add(service.config.LeaseTTL), runID, userID, briefHash)
	if err != nil {
		return reportbrief.ReviewFinalized{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		if job.Status == "written" {
			return finalized, nil
		}
		return reportbrief.ReviewFinalized{}, errors.New("report review job is not writable")
	}
	if err := service.finalizer.FinalizeReviewedReport(ctx, userID, runID, finalized, decisionPayload); err != nil {
		_ = service.releaseFinalizingLease(ctx, runID, err.Error())
		return reportbrief.ReviewFinalized{}, err
	}
	_, err = service.db.ExecContext(ctx, `
		UPDATE report_review_jobs SET status = 'written', finished_at = now(),
			lease_owner = NULL, lease_until = NULL, last_error = NULL, updated_at = now()
		WHERE run_id = $1::uuid AND user_id = $2 AND status = 'finalizing'`, runID, userID)
	return finalized, err
}

func expandProjectAttachments(decision reportbrief.ReviewDecision, input Input) (reportbrief.ReviewDecision, error) {
	// Parent-project attachments are a bounded deterministic grouping pass.
	// They must also run when the Reviewer repairs another issue in the Brief;
	// otherwise a valid Project Memory grouping is lost just because the same
	// review contained an unrelated correction.
	if len(decision.ProjectAttachments) == 0 && decision.Decision != reportbrief.ReviewDecisionConservative {
		for _, project := range input.ProjectCandidates {
			if project.IdentityUsage != "parent_label_for_matching_cues" || len(project.ProposedTargets) < 2 {
				continue
			}
			decision.ProjectAttachments = append(decision.ProjectAttachments, reportbrief.ReviewProjectAttachment{
				CanonicalName: project.CanonicalName, Targets: append([]string(nil), project.ProposedTargets...),
				FactRefs: append([]string(nil), project.RelatedFactRefs...),
			})
		}
	}
	if len(decision.ProjectAttachments) == 0 {
		return decision, nil
	}
	if decision.Decision == reportbrief.ReviewDecisionConservative {
		return decision, errors.New("conservative review cannot attach projects")
	}
	projects := make(map[string]ProjectCandidate, len(input.ProjectCandidates))
	for _, candidate := range input.ProjectCandidates {
		if candidate.IdentityUsage == "parent_label_for_matching_cues" {
			projects[candidate.CanonicalName] = candidate
		}
	}
	seenTargets := map[string]struct{}{}
	for _, attachment := range decision.ProjectAttachments {
		project, ok := projects[strings.TrimSpace(attachment.CanonicalName)]
		if !ok || len(attachment.Targets) == 0 || len(attachment.FactRefs) == 0 {
			return decision, errors.New("invalid project attachment")
		}
		projectRefs := make(map[string]struct{}, len(project.RelatedFactRefs))
		for _, ref := range project.RelatedFactRefs {
			projectRefs[ref] = struct{}{}
		}
		for _, ref := range attachment.FactRefs {
			if _, ok := projectRefs[ref]; !ok {
				return decision, errors.New("project attachment references unrelated Fact")
			}
		}
		for _, target := range attachment.Targets {
			target = strings.TrimSpace(target)
			if len(target) < 2 || target[0] != 'w' || strings.Contains(target, ".") {
				return decision, errors.New("project attachment target must be a Workstream")
			}
			index, err := strconv.Atoi(target[1:])
			if err != nil || index < 1 || index > len(input.Candidate.Workstreams) {
				return decision, errors.New("project attachment target is out of range")
			}
			if _, exists := seenTargets[target]; exists {
				return decision, errors.New("project attachment target is duplicated")
			}
			seenTargets[target] = struct{}{}
			if input.Candidate.Workstreams[index-1].Subject == project.CanonicalName {
				continue
			}
			decision.Patches = append(decision.Patches, reportbrief.ReviewPatch{
				Op: "replace_subject", Target: target, Value: project.CanonicalName,
				SupportingFactRefs: append([]string(nil), attachment.FactRefs...),
			})
		}
	}
	if len(decision.Patches) > maxExpandedReviewPatches {
		return decision, errors.New("expanded review exceeds patch limit")
	}
	if len(decision.Patches) > 0 && decision.Decision == reportbrief.ReviewDecisionAccept {
		decision.Decision = reportbrief.ReviewDecisionRepair
	}
	return decision, nil
}

func (service *Service) loadJobInput(ctx context.Context, userID, runID, briefHash string) (queuedJob, Input, error) {
	var job queuedJob
	var inputPayload []byte
	err := service.db.QueryRowContext(ctx, `
		SELECT run_id::text, user_id::text, brief_hash, context_hash, status,
		       COALESCE(external_task_id, ''), input_json, COALESCE(decision_json, 'null'::jsonb),
		       attempts, COALESCE(started_at, created_at)
		FROM report_review_jobs WHERE run_id = $1::uuid AND user_id = $2 AND brief_hash = $3`,
		runID, userID, briefHash).Scan(
		&job.RunID, &job.UserID, &job.BriefHash, &job.ContextHash, &job.Status,
		&job.ExternalTaskID, &inputPayload, &job.DecisionJSON, &job.Attempts, &job.StartedAt,
	)
	if err != nil {
		return queuedJob{}, Input{}, err
	}
	var input Input
	if err := json.Unmarshal(inputPayload, &input); err != nil {
		return queuedJob{}, Input{}, err
	}
	job.InputJSON = inputPayload
	return job, input, nil
}

func (service *Service) Start(ctx context.Context) {
	if !service.Enabled() {
		return
	}
	go func() {
		run := func(now time.Time) {
			if err := service.RunOnce(ctx, now); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("report review worker failed: %v", err)
			}
		}
		run(time.Now())
		ticker := time.NewTicker(service.config.PollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				run(now)
			}
		}
	}()
}

func (service *Service) RunOnce(ctx context.Context, now time.Time) error {
	if !service.Enabled() {
		return nil
	}
	for i := 0; i < service.config.ClaimBatch; i++ {
		job, found, err := service.claimFinalizing(ctx, now)
		if err != nil {
			return err
		}
		if !found {
			break
		}
		if err := service.resumeFinalizing(ctx, job); err != nil {
			log.Printf("report review run %s finalization retry failed: %v", job.RunID, err)
		}
	}
	for i := 0; i < service.config.ClaimBatch; i++ {
		job, found, err := service.claimRunning(ctx, now)
		if err != nil {
			return err
		}
		if !found {
			break
		}
		if err := service.refreshRunning(ctx, job, now); err != nil {
			log.Printf("report review run %s refresh failed: %v", job.RunID, err)
		}
	}
	for i := 0; i < service.config.ClaimBatch; i++ {
		job, found, err := service.claimPending(ctx, now)
		if err != nil {
			return err
		}
		if !found {
			break
		}
		if err := service.submit(ctx, job, now); err != nil {
			log.Printf("report review run %s submit failed, using candidate: %v", job.RunID, err)
			if fallbackErr := service.finalizeCandidate(ctx, job, "review_submit_failed"); fallbackErr != nil {
				return fallbackErr
			}
		}
	}
	return nil
}

func (service *Service) claimPending(ctx context.Context, now time.Time) (queuedJob, bool, error) {
	tx, err := service.db.BeginTx(ctx, nil)
	if err != nil {
		return queuedJob{}, false, err
	}
	defer tx.Rollback()
	var job queuedJob
	var input []byte
	err = tx.QueryRowContext(ctx, `
		SELECT run_id::text, user_id::text, brief_hash, context_hash, status,
		       input_json, attempts, COALESCE(started_at, created_at)
		FROM report_review_jobs
		WHERE status IN ('pending', 'submitting') AND due_at <= $1
		  AND attempts < $2 AND (lease_until IS NULL OR lease_until <= $1)
		ORDER BY due_at, created_at FOR UPDATE SKIP LOCKED LIMIT 1`, now, maxReviewAttempts).Scan(
		&job.RunID, &job.UserID, &job.BriefHash, &job.ContextHash, &job.Status,
		&input, &job.Attempts, &job.StartedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return queuedJob{}, false, nil
	}
	if err != nil {
		return queuedJob{}, false, err
	}
	job.InputJSON = input
	job.Attempts++
	_, err = tx.ExecContext(ctx, `
		UPDATE report_review_jobs SET status = 'submitting', attempts = attempts + 1,
			lease_owner = $1, lease_until = $2, started_at = COALESCE(started_at, $3), updated_at = now()
		WHERE run_id = $4::uuid`, service.config.WorkerID, now.Add(service.config.LeaseTTL), now, job.RunID)
	if err != nil {
		return queuedJob{}, false, err
	}
	return job, true, tx.Commit()
}

func (service *Service) submit(ctx context.Context, job queuedJob, now time.Time) error {
	submission, err := service.resolver.Submit(ctx, ResolverRequest{
		AgentID: service.config.AgentID, ModelID: service.config.ModelID,
		UserID: job.UserID, JobRef: JobRef(job.RunID, job.BriefHash),
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(submission.TaskID) == "" {
		return errors.New("report review resolver returned an empty task ID")
	}
	tx, err := service.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE report_review_jobs SET status = 'running', external_task_id = $1,
			model_id = NULLIF($2, ''), lease_owner = NULL, lease_until = NULL, updated_at = now()
		WHERE run_id = $3::uuid AND status = 'submitting'`, submission.TaskID, service.config.ModelID, job.RunID)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		var status string
		if queryErr := tx.QueryRowContext(ctx, `
			SELECT status FROM report_review_jobs WHERE run_id = $1::uuid`, job.RunID).Scan(&status); queryErr != nil {
			return queryErr
		}
		if status == "finalizing" || status == "written" {
			// The Reviewer can write back before CreateSession returns. Its
			// finalization already owns the parent Run, so submission is done.
			return nil
		}
		return errors.New("report review job changed during submit")
	}
	result, err = tx.ExecContext(ctx, `
		UPDATE ai_runs SET execution_stage = 'review_running', stage_updated_at = now()
		WHERE id = $1::uuid AND user_id = $2 AND status = 'running'
		  AND execution_stage = 'review_pending'`, job.RunID, job.UserID)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errors.New("report run changed before review submit")
	}
	return tx.Commit()
}

func (service *Service) claimRunning(ctx context.Context, now time.Time) (queuedJob, bool, error) {
	tx, err := service.db.BeginTx(ctx, nil)
	if err != nil {
		return queuedJob{}, false, err
	}
	defer tx.Rollback()
	var job queuedJob
	var input []byte
	err = tx.QueryRowContext(ctx, `
		SELECT run_id::text, user_id::text, brief_hash, context_hash, status,
		       COALESCE(external_task_id, ''), input_json, attempts, COALESCE(started_at, created_at)
		FROM report_review_jobs
		WHERE status = 'running' AND (lease_until IS NULL OR lease_until <= $1)
		ORDER BY updated_at FOR UPDATE SKIP LOCKED LIMIT 1`, now).Scan(
		&job.RunID, &job.UserID, &job.BriefHash, &job.ContextHash, &job.Status,
		&job.ExternalTaskID, &input, &job.Attempts, &job.StartedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return queuedJob{}, false, nil
	}
	if err != nil {
		return queuedJob{}, false, err
	}
	job.InputJSON = input
	_, err = tx.ExecContext(ctx, `
		UPDATE report_review_jobs SET lease_owner = $1, lease_until = $2, updated_at = now()
		WHERE run_id = $3::uuid AND status = 'running'`, service.config.WorkerID, now.Add(service.config.LeaseTTL), job.RunID)
	if err != nil {
		return queuedJob{}, false, err
	}
	return job, true, tx.Commit()
}

func (service *Service) refreshRunning(ctx context.Context, job queuedJob, now time.Time) error {
	if !job.StartedAt.IsZero() && now.Sub(job.StartedAt) >= maxReviewDuration {
		return service.finalizeCandidate(ctx, job, "review_timeout")
	}
	task, err := service.resolver.Status(ctx, job.ExternalTaskID)
	if err != nil {
		return service.releaseLease(ctx, job, err.Error())
	}
	switch normalizeResolverStatus(task.Status) {
	case "running":
		return service.releaseLease(ctx, job, "")
	case "succeeded":
		var status string
		if err := service.db.QueryRowContext(ctx, `SELECT status FROM report_review_jobs WHERE run_id = $1::uuid`, job.RunID).Scan(&status); err != nil {
			return err
		}
		if status == "written" {
			return nil
		}
		return service.finalizeCandidate(ctx, job, "review_writeback_missing")
	default:
		return service.finalizeCandidate(ctx, job, "review_failed")
	}
}

func (service *Service) finalizeCandidate(ctx context.Context, job queuedJob, reason string) error {
	var input Input
	if err := json.Unmarshal(job.InputJSON, &input); err != nil {
		return err
	}
	candidate := reportbrief.Stored{Payload: input.Candidate, BriefHash: input.BriefHash, ContextHash: input.ContextHash}
	finalized := reportbrief.ReviewFinalized{
		Stored: candidate, Mode: reportbrief.ReviewModeAccepted, Warnings: []string{reason},
	}
	metadata, _ := json.Marshal(map[string]any{"decision": "unavailable", "reason": reason})
	result, err := service.db.ExecContext(ctx, `
		UPDATE report_review_jobs SET status = 'finalizing', final_brief_json = $1::jsonb,
			decision_json = $2::jsonb, finalization_mode = 'unavailable', last_error = $3,
			lease_owner = $4, lease_until = $5, updated_at = now()
		WHERE run_id = $6::uuid AND status IN ('pending', 'submitting', 'running')`,
		mustJSON(input.Candidate), metadata, reason, service.config.WorkerID,
		time.Now().Add(service.config.LeaseTTL), job.RunID)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return nil
	}
	if err := service.finalizer.FinalizeReviewedReport(ctx, job.UserID, job.RunID, finalized, metadata); err != nil {
		_ = service.releaseFinalizingLease(ctx, job.RunID, err.Error())
		return err
	}
	_, err = service.db.ExecContext(ctx, `
		UPDATE report_review_jobs SET status = 'written', finished_at = now(),
			lease_owner = NULL, lease_until = NULL, updated_at = now()
		WHERE run_id = $1::uuid AND status = 'finalizing'`, job.RunID)
	return err
}

func (service *Service) claimFinalizing(ctx context.Context, now time.Time) (queuedJob, bool, error) {
	tx, err := service.db.BeginTx(ctx, nil)
	if err != nil {
		return queuedJob{}, false, err
	}
	defer tx.Rollback()
	var job queuedJob
	err = tx.QueryRowContext(ctx, `
		SELECT run_id::text, user_id::text, brief_hash, context_hash, status,
		       input_json, COALESCE(decision_json, 'null'::jsonb), final_brief_json,
		       COALESCE(finalization_mode, ''), attempts, COALESCE(started_at, created_at)
		FROM report_review_jobs
		WHERE status = 'finalizing' AND (lease_until IS NULL OR lease_until <= $1)
		ORDER BY updated_at FOR UPDATE SKIP LOCKED LIMIT 1`, now).Scan(
		&job.RunID, &job.UserID, &job.BriefHash, &job.ContextHash, &job.Status,
		&job.InputJSON, &job.DecisionJSON, &job.FinalBriefJSON, &job.FinalMode,
		&job.Attempts, &job.StartedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return queuedJob{}, false, nil
	}
	if err != nil {
		return queuedJob{}, false, err
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE report_review_jobs SET lease_owner = $1, lease_until = $2, updated_at = now()
		WHERE run_id = $3::uuid AND status = 'finalizing'`,
		service.config.WorkerID, now.Add(service.config.LeaseTTL), job.RunID)
	if err != nil {
		return queuedJob{}, false, err
	}
	return job, true, tx.Commit()
}

func (service *Service) resumeFinalizing(ctx context.Context, job queuedJob) error {
	var payload reportbrief.Payload
	if err := json.Unmarshal(job.FinalBriefJSON, &payload); err != nil {
		return service.releaseFinalizingLease(ctx, job.RunID, err.Error())
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return service.releaseFinalizingLease(ctx, job.RunID, err.Error())
	}
	hash := sha256.Sum256(encoded)
	finalized := reportbrief.ReviewFinalized{
		Stored: reportbrief.Stored{
			Payload: payload, BriefHash: hex.EncodeToString(hash[:]), ContextHash: job.ContextHash,
		},
		Mode: job.FinalMode,
	}
	if err := service.finalizer.FinalizeReviewedReport(ctx, job.UserID, job.RunID, finalized, job.DecisionJSON); err != nil {
		_ = service.releaseFinalizingLease(ctx, job.RunID, err.Error())
		return err
	}
	_, err = service.db.ExecContext(ctx, `
		UPDATE report_review_jobs SET status = 'written', finished_at = now(),
			lease_owner = NULL, lease_until = NULL, last_error = NULL, updated_at = now()
		WHERE run_id = $1::uuid AND status = 'finalizing'`, job.RunID)
	return err
}

func (service *Service) releaseFinalizingLease(ctx context.Context, runID, message string) error {
	_, err := service.db.ExecContext(ctx, `
		UPDATE report_review_jobs SET lease_owner = NULL, lease_until = $1,
			last_error = NULLIF($2, ''), updated_at = now()
		WHERE run_id = $3::uuid AND status = 'finalizing'`, time.Now().Add(finalizingRetryDelay), limitRunes(message, 1000), runID)
	return err
}

func (service *Service) releaseLease(ctx context.Context, job queuedJob, message string) error {
	_, err := service.db.ExecContext(ctx, `
		UPDATE report_review_jobs SET lease_owner = NULL, lease_until = NULL,
			last_error = NULLIF($1, ''), updated_at = now()
		WHERE run_id = $2::uuid AND status = 'running'`, limitRunes(message, 1000), job.RunID)
	return err
}

func normalizeResolverStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "complete", "done", "success", "succeeded":
		return "succeeded"
	case "failed", "error", "cancelled", "canceled", "timeout", "timed_out":
		return "failed"
	default:
		return "running"
	}
}

func mustJSON(value any) []byte {
	payload, _ := json.Marshal(value)
	return payload
}

func limitRunes(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit])
}

func (service *Service) DebugJob(ctx context.Context, runID string) (map[string]any, error) {
	var status, externalTaskID, mode, lastError string
	err := service.db.QueryRowContext(ctx, `
		SELECT status, COALESCE(external_task_id, ''), COALESCE(finalization_mode, ''), COALESCE(last_error, '')
		FROM report_review_jobs WHERE run_id = $1::uuid`, runID).Scan(&status, &externalTaskID, &mode, &lastError)
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": status, "external_task_id": externalTaskID, "finalization_mode": mode, "last_error": lastError}, nil
}
