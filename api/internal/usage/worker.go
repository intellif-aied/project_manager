package usage

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/aidashboard/api/internal/sessionsync"
)

type JobProcessor interface {
	Process(context.Context, sessionsync.ProcessingJob) error
}

type Worker struct {
	queue      sessionsync.ContentJobQueue
	processor  JobProcessor
	owner      string
	interval   time.Duration
	leaseTTL   time.Duration
	batchLimit int
	jobTypes   []string
	logLabel   string
}

func NewWorker(queue sessionsync.ContentJobQueue, processor JobProcessor, owner string) (*Worker, error) {
	return newWorker(queue, processor, owner, "usage projection", []string{
		sessionsync.JobParseUsageChunk,
		sessionsync.JobRebuildMetricsRevision,
	})
}

func NewMeteringWorker(queue sessionsync.ContentJobQueue, processor JobProcessor, owner string) (*Worker, error) {
	return newWorker(queue, processor, owner, "metering lifecycle", []string{
		sessionsync.JobBuildMeteringEnvelope,
		sessionsync.JobDeleteObject,
	})
}

func newWorker(
	queue sessionsync.ContentJobQueue,
	processor JobProcessor,
	owner, logLabel string,
	jobTypes []string,
) (*Worker, error) {
	if queue == nil || processor == nil || owner == "" {
		return nil, errors.New("usage job queue, processor, and owner are required")
	}
	if len(jobTypes) == 0 {
		return nil, errors.New("usage worker job types are required")
	}
	return &Worker{
		queue: queue, processor: processor, owner: owner,
		interval: 2 * time.Second, leaseTTL: 5 * time.Minute, batchLimit: 10,
		jobTypes: append([]string(nil), jobTypes...), logLabel: logLabel,
	}, nil
}

func (w *Worker) Start(ctx context.Context) {
	go func() {
		if err := w.RunOnce(ctx, time.Now().UTC()); err != nil {
			log.Printf("%s worker failed: %v", w.logLabel, err)
		}
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				if err := w.RunOnce(ctx, now.UTC()); err != nil {
					log.Printf("%s worker failed: %v", w.logLabel, err)
				}
			}
		}
	}()
}

func (w *Worker) RunOnce(ctx context.Context, now time.Time) error {
	jobs, err := w.queue.ClaimTypes(ctx, w.owner, now, w.leaseTTL, w.batchLimit, w.jobTypes)
	if err != nil {
		return err
	}
	var firstError error
	for _, job := range jobs {
		processErr := w.processWithHeartbeat(ctx, job)
		if errors.Is(processErr, ErrStaleMeteringEpoch) {
			processErr = nil
		}
		finishedAt := time.Now().UTC()
		if processErr == nil {
			ok, completeErr := w.queue.Complete(ctx, job.ID, w.owner, finishedAt)
			if completeErr != nil && firstError == nil {
				firstError = completeErr
			} else if !ok && firstError == nil {
				firstError = fmt.Errorf("usage job %s lost its lease before completion", job.ID)
			}
			continue
		}
		retryAfter := usageJobRetryDelay(job.Attempts, processErr)
		ok, failErr := w.queue.Fail(ctx, job.ID, w.owner, finishedAt, retryAfter, processErr.Error())
		if failErr != nil && firstError == nil {
			firstError = failErr
		} else if !ok && firstError == nil {
			firstError = fmt.Errorf("usage job %s lost its lease before failure update", job.ID)
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
				return fmt.Errorf("usage job %s lost its lease during processing", job.ID)
			}
		case <-ctx.Done():
			cancel()
			<-result
			return ctx.Err()
		}
	}
}

func usageJobRetryDelay(attempts int, failure error) time.Duration {
	if errors.Is(failure, ErrUsageOutOfOrder) {
		return 2 * time.Second
	}
	if attempts < 1 {
		attempts = 1
	}
	return time.Duration(1<<min(attempts-1, 6)) * time.Second
}
