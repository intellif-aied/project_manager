package sessiondigestv2

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/aidashboard/api/internal/observability"
	"github.com/aidashboard/api/internal/sessionsync"
)

type JobQueue interface {
	ClaimDigest(context.Context, string, string, time.Time, time.Duration, int, string) ([]sessionsync.ProcessingJob, error)
	Heartbeat(context.Context, string, string, time.Time, time.Duration) (bool, error)
	Complete(context.Context, string, string, time.Time) (bool, error)
	Fail(context.Context, string, string, time.Time, time.Duration, bool, string) (bool, error)
}

type JobProcessor interface {
	Process(context.Context, sessionsync.ProcessingJob) error
}

type Worker struct {
	queue      JobQueue
	processor  JobProcessor
	owner      string
	urgency    string
	interval   time.Duration
	leaseTTL   time.Duration
	batchLimit int
}

func NewWorker(queue JobQueue, processor JobProcessor, owner, urgency string, config Config) (*Worker, error) {
	if queue == nil || processor == nil || owner == "" ||
		(urgency != "background" && urgency != "interactive") {
		return nil, errors.New("digest v2 job queue, processor, owner, and urgency are required")
	}
	normalized, err := config.Normalized()
	if err != nil {
		return nil, err
	}
	return &Worker{
		queue: queue, processor: processor, owner: owner, urgency: urgency,
		interval: 2 * time.Second, leaseTTL: 5 * time.Minute,
		batchLimit: normalized.WorkerBatch,
	}, nil
}

func (w *Worker) Start(ctx context.Context) {
	go func() {
		if err := w.RunOnce(ctx, time.Now().UTC()); err != nil {
			log.Printf("session digest v2 worker failed: %v", err)
		}
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				if err := w.RunOnce(ctx, now.UTC()); err != nil {
					log.Printf("session digest v2 worker failed: %v", err)
				}
			}
		}
	}()
}

func (w *Worker) RunOnce(ctx context.Context, now time.Time) error {
	jobs, err := w.queue.ClaimDigest(
		ctx, w.owner, w.urgency, now, w.leaseTTL, w.batchLimit, JobType,
	)
	if err != nil {
		return err
	}
	var firstError error
	for _, job := range jobs {
		observability.ObserveDigestClaim(w.urgency, job.EligibleAt, now)
		startedAt := time.Now()
		processErr := w.processWithHeartbeat(ctx, job)
		finishedAt := time.Now().UTC()
		if processErr == nil || errors.Is(processErr, ErrStaleDigestSource) {
			observability.ObserveDigestBuild(w.urgency, "completed", "none", time.Since(startedAt))
			ok, completeErr := w.queue.Complete(ctx, job.ID, w.owner, finishedAt)
			if completeErr != nil && firstError == nil {
				firstError = completeErr
			} else if !ok && firstError == nil {
				firstError = fmt.Errorf("digest v2 job %s lost its lease before completion", job.ID)
			}
			continue
		}
		preserveAttempt := errors.Is(processErr, ErrDigestStatePersistence)
		result := "retry_wait"
		if !preserveAttempt && job.Attempts >= job.MaxAttempts {
			result = "dead"
		}
		observability.ObserveDigestBuild(
			w.urgency, result, FailureClass(processErr), time.Since(startedAt),
		)
		ok, failErr := w.queue.Fail(
			ctx, job.ID, w.owner, finishedAt, digestRetryDelay(job.Attempts),
			preserveAttempt, FailureCode(processErr),
		)
		if failErr != nil && firstError == nil {
			firstError = failErr
		} else if !ok && firstError == nil {
			firstError = fmt.Errorf("digest v2 job %s lost its lease before failure update", job.ID)
		}
	}
	return firstError
}

func (w *Worker) processWithHeartbeat(ctx context.Context, job sessionsync.ProcessingJob) error {
	processCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- w.processor.Process(processCtx, job) }()
	ticker := time.NewTicker(w.leaseTTL / 3)
	defer ticker.Stop()
	for {
		select {
		case err := <-result:
			return err
		case now := <-ticker.C:
			ok, err := w.queue.Heartbeat(ctx, job.ID, w.owner, now.UTC(), w.leaseTTL)
			if err != nil || !ok {
				cancel()
				<-result
				if err != nil {
					return err
				}
				return fmt.Errorf("digest v2 job %s lost its lease during processing", job.ID)
			}
		case <-ctx.Done():
			cancel()
			<-result
			return ctx.Err()
		}
	}
}

func digestRetryDelay(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	return time.Duration(1<<min(attempts-1, 6)) * time.Second
}
