package reportrun

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/aidashboard/api/internal/biztime"
	"github.com/aidashboard/api/internal/reportcontext"
	"github.com/aidashboard/api/internal/reportsource"
	"github.com/aidashboard/api/internal/sessiondigestv2"
)

const (
	reportRunLeaseTTL       = 5 * time.Minute
	reportRunHeartbeat      = time.Minute
	reportRunPollInterval   = 2 * time.Second
	contextExecutionTimeout = 10 * time.Minute
)

type DigestCoordinator interface {
	EnsureRunDigests(context.Context, string, string) (sessiondigestv2.EnsureResult, error)
}

type DigestFreezer interface {
	FreezeRunDigests(context.Context, string) (string, error)
}

type ContextBuilder interface {
	Build(context.Context, reportcontext.BuildRequest) (reportcontext.StoredContext, error)
}

type SubmissionResult struct {
	SessionID string
	ModelID   string
}

type SubmissionError struct {
	Code      string
	Message   string
	Retryable bool
}

func (e *SubmissionError) Error() string { return e.Message }

type AgentSubmitter interface {
	Submit(context.Context, Run) (SubmissionResult, error)
}

type Processor struct {
	repository  *Repository
	coordinator DigestCoordinator
	freezer     DigestFreezer
	context     ContextBuilder
	submitter   AgentSubmitter
	owner       string
	interval    time.Duration
}

func NewProcessor(
	repository *Repository,
	coordinator DigestCoordinator,
	freezer DigestFreezer,
	contextBuilder ContextBuilder,
	submitter AgentSubmitter,
	owner string,
) (*Processor, error) {
	if repository == nil || coordinator == nil || freezer == nil || contextBuilder == nil ||
		submitter == nil || owner == "" {
		return nil, errors.New("complete report run processor dependencies are required")
	}
	return &Processor{
		repository: repository, coordinator: coordinator, freezer: freezer,
		context: contextBuilder, submitter: submitter, owner: owner,
		interval: reportRunPollInterval,
	}, nil
}

func (p *Processor) Start(ctx context.Context) {
	go func() {
		if err := p.RunOnce(ctx, time.Now().UTC()); err != nil {
			log.Printf("report run processor %s failed: %v", p.owner, err)
		}
		ticker := time.NewTicker(p.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				if err := p.RunOnce(ctx, now.UTC()); err != nil {
					log.Printf("report run processor %s failed: %v", p.owner, err)
				}
			}
		}
	}()
}

func (p *Processor) RunOnce(ctx context.Context, now time.Time) error {
	run, found, err := p.repository.Claim(ctx, p.owner, now, reportRunLeaseTTL)
	if err != nil || !found {
		return err
	}
	switch run.Stage {
	case StageWaitingDigest:
		return p.processWaitingDigest(ctx, run)
	case StageBuildingContext:
		return p.withHeartbeat(ctx, run, contextExecutionTimeout, func(stageCtx context.Context) error {
			return p.processBuildingContext(stageCtx, run)
		})
	case StageSubmittingAgent:
		return p.withHeartbeat(ctx, run, reportRunLeaseTTL, func(stageCtx context.Context) error {
			return p.processSubmittingAgent(stageCtx, run)
		})
	default:
		return p.repository.Fail(ctx, run, "failed", "REPORT_RUN_STAGE_INVALID", "invalid report run stage")
	}
}

func (p *Processor) processWaitingDigest(ctx context.Context, run Run) error {
	result, err := p.coordinator.EnsureRunDigests(ctx, run.ID, sessiondigestv2.UrgencyInteractive)
	if err != nil {
		return p.repository.RetryStage(ctx, run, "DIGEST_COORDINATOR_FAILED", err.Error())
	}
	switch result.State {
	case sessiondigestv2.EnsureReady:
		return p.repository.Transition(ctx, run, StageBuildingContext, "pending", false)
	case sessiondigestv2.EnsureWaiting:
		return p.repository.WaitForDigest(ctx, run)
	case sessiondigestv2.EnsureFailed:
		code := result.ErrorCode
		if code == "" {
			code = "REPORT_SOURCE_DIGEST_FAILED"
		}
		return p.repository.Fail(ctx, run, "failed", code, "report source preparation failed")
	default:
		return p.repository.RetryStage(ctx, run, "DIGEST_COORDINATOR_FAILED", "invalid coordinator result")
	}
}

func (p *Processor) processBuildingContext(ctx context.Context, run Run) error {
	selectionID := stringValue(run.InputRef, "report_source_selection_id")
	if selectionID != "" {
		frozenSelectionID, err := p.freezer.FreezeRunDigests(ctx, run.ID)
		if err != nil {
			if errors.Is(err, reportsource.ErrDigestNotReady) {
				return p.repository.Fail(ctx, run, "failed", "REPORT_SOURCE_DIGEST_FAILED", err.Error())
			}
			return p.handleContextError(ctx, run, err)
		}
		if frozenSelectionID != selectionID {
			return p.repository.Fail(ctx, run, "failed", "REPORT_SOURCE_DIGEST_FAILED", "selection identity changed")
		}
	}
	request, err := contextBuildRequest(run, selectionID)
	if err != nil {
		return p.repository.Fail(ctx, run, "failed", "REPORT_CONTEXT_BUILD_FAILED", err.Error())
	}
	stored, err := p.context.Build(ctx, request)
	if err != nil {
		return p.handleContextError(ctx, run, err)
	}
	return p.repository.StoreContextAndTransition(ctx, run, stored.Hash, stored.Bytes)
}

func (p *Processor) handleContextError(ctx context.Context, run Run, err error) error {
	if errors.Is(err, reportsource.ErrDigestCorrupt) ||
		errors.Is(err, reportsource.ErrDigestVersionMismatch) ||
		errors.Is(err, reportsource.ErrSourceUnavailable) ||
		errors.Is(err, reportcontext.ErrIncomplete) {
		return p.repository.Fail(ctx, run, "failed", "REPORT_SOURCE_DIGEST_FAILED", err.Error())
	}
	if isRetryableDatabaseError(err) {
		return p.repository.RetryStage(ctx, run, "REPORT_CONTEXT_BUILD_FAILED", err.Error())
	}
	return p.repository.Fail(ctx, run, "failed", "REPORT_CONTEXT_BUILD_FAILED", err.Error())
}

func (p *Processor) processSubmittingAgent(ctx context.Context, run Run) error {
	if run.ExternalSessionID != "" {
		return p.repository.StoreExternalSessionAndTransition(
			ctx, run, run.ExternalSessionID, run.ModelID,
		)
	}
	result, err := p.submitter.Submit(ctx, run)
	if err != nil {
		var submissionErr *SubmissionError
		if errors.As(err, &submissionErr) {
			if submissionErr.Retryable {
				return p.repository.RetryStage(ctx, run, submissionErr.Code, submissionErr.Message)
			}
			return p.repository.Fail(ctx, run, "failed", submissionErr.Code, submissionErr.Message)
		}
		return p.repository.Fail(
			ctx, run, "failed", "EXTERNAL_SUBMISSION_STATE_UNKNOWN", err.Error(),
		)
	}
	if result.SessionID == "" {
		return p.repository.Fail(
			ctx, run, "failed", "EXTERNAL_SUBMISSION_REJECTED", "AIHub returned an empty session ID",
		)
	}
	return p.repository.StoreExternalSessionAndTransition(ctx, run, result.SessionID, result.ModelID)
}

func (p *Processor) withHeartbeat(
	ctx context.Context,
	run Run,
	timeout time.Duration,
	operation func(context.Context) error,
) error {
	stageCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- operation(stageCtx) }()
	ticker := time.NewTicker(reportRunHeartbeat)
	defer ticker.Stop()
	for {
		select {
		case err := <-result:
			return err
		case now := <-ticker.C:
			ok, err := p.repository.Heartbeat(
				ctx, run.ID, run.LeaseOwner, run.Stage, now.UTC(), reportRunLeaseTTL,
			)
			if err != nil || !ok {
				cancel()
				<-result
				if err != nil {
					return err
				}
				return ErrLeaseLost
			}
		case <-stageCtx.Done():
			cancel()
			<-result
			if errors.Is(stageCtx.Err(), context.DeadlineExceeded) {
				if run.Stage == StageSubmittingAgent {
					return p.repository.Fail(
						ctx, run, "failed", "EXTERNAL_SUBMISSION_STATE_UNKNOWN",
						"AIHub submission timed out with an unknown external state",
					)
				}
				return p.repository.RetryStage(ctx, run, stageTimeoutCode(run.Stage), stageCtx.Err().Error())
			}
			return stageCtx.Err()
		}
	}
}

func contextBuildRequest(run Run, selectionID string) (reportcontext.BuildRequest, error) {
	reportType := stringValue(run.InputRef, "report_type")
	periodMap, _ := run.InputRef["period"].(map[string]any)
	period := reportsource.Period{
		Start: firstString(periodMap, "date", "week_start", "start"),
		End:   firstString(periodMap, "date", "week_end", "end"),
	}
	targetMap, _ := run.InputRef["target"].(map[string]any)
	target := reportcontext.Target{
		Type: stringValue(targetMap, "type"), UserID: stringValue(targetMap, "user_id"),
		TeamID: stringValue(targetMap, "team_id"), DepartmentID: stringValue(targetMap, "department_id"),
	}
	timezone := stringValue(run.ExecutionInput, "timezone")
	if timezone == "" {
		timezone = biztime.Zone
	}
	request := reportcontext.BuildRequest{
		UserID: run.UserID, RunID: run.ID, ReportType: reportType,
		Period: period, Timezone: timezone,
		TriggerSource: stringValue(run.InputRef, "trigger_source"), ModelID: run.ModelID,
		Target: target, SourceSelectionID: selectionID,
		Representation: stringValue(run.ExecutionInput, "report_context_representation"),
	}
	if reportType == "" || period.Start == "" || period.End == "" {
		return reportcontext.BuildRequest{}, errors.New("frozen report run input is incomplete")
	}
	return request, nil
}

func stringValue(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringValue(values, key); value != "" {
			return value
		}
	}
	return ""
}

func isRetryableDatabaseError(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "serialization") || strings.Contains(message, "deadlock") ||
		strings.Contains(message, "connection") || strings.Contains(message, "database is closed")
}

func stageTimeoutCode(stage string) string {
	if stage == StageBuildingContext {
		return "REPORT_CONTEXT_BUILD_FAILED"
	}
	return "EXTERNAL_SUBMISSION_PREPARE_FAILED"
}
