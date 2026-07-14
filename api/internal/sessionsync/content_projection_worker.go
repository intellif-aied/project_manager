package sessionsync

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"
)

type ContentJobQueue interface {
	ClaimTypes(context.Context, string, time.Time, time.Duration, int, []string) ([]ProcessingJob, error)
	Heartbeat(context.Context, string, string, time.Time, time.Duration) (bool, error)
	Complete(context.Context, string, string, time.Time) (bool, error)
	Fail(context.Context, string, string, time.Time, time.Duration, string) (bool, error)
}

type ContentJobProcessor interface {
	Process(context.Context, ProcessingJob) error
}

type ContentProjectionWorker struct {
	queue      ContentJobQueue
	processor  ContentJobProcessor
	owner      string
	interval   time.Duration
	leaseTTL   time.Duration
	batchLimit int
}

func NewContentProjectionWorker(queue ContentJobQueue, processor ContentJobProcessor, owner string) (*ContentProjectionWorker, error) {
	if queue == nil || processor == nil || owner == "" {
		return nil, errors.New("content job queue, processor, and owner are required")
	}
	return &ContentProjectionWorker{
		queue: queue, processor: processor, owner: owner,
		interval: 2 * time.Second, leaseTTL: 5 * time.Minute, batchLimit: 10,
	}, nil
}

func (w *ContentProjectionWorker) Start(ctx context.Context) {
	go func() {
		if err := w.RunOnce(ctx, time.Now().UTC()); err != nil {
			log.Printf("content projection worker failed: %v", err)
		}
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				if err := w.RunOnce(ctx, now.UTC()); err != nil {
					log.Printf("content projection worker failed: %v", err)
				}
			}
		}
	}()
}

func (w *ContentProjectionWorker) RunOnce(ctx context.Context, now time.Time) error {
	jobs, err := w.queue.ClaimTypes(ctx, w.owner, now, w.leaseTTL, w.batchLimit, []string{
		JobIndexContentChunk,
		JobRebuildContentRevision,
	})
	if err != nil {
		return err
	}
	var firstError error
	for _, job := range jobs {
		processErr := w.processWithHeartbeat(ctx, job)
		finishedAt := time.Now().UTC()
		if processErr == nil || errors.Is(processErr, ErrStaleContentEpoch) {
			ok, completeErr := w.queue.Complete(ctx, job.ID, w.owner, finishedAt)
			if completeErr != nil && firstError == nil {
				firstError = completeErr
			} else if !ok && firstError == nil {
				firstError = fmt.Errorf("content job %s lost its lease before completion", job.ID)
			}
			continue
		}
		retryAfter := contentJobRetryDelay(job.Attempts, processErr)
		ok, failErr := w.queue.Fail(ctx, job.ID, w.owner, finishedAt, retryAfter, processErr.Error())
		if failErr != nil && firstError == nil {
			firstError = failErr
		} else if !ok && firstError == nil {
			firstError = fmt.Errorf("content job %s lost its lease before failure update", job.ID)
		}
	}
	return firstError
}

func (w *ContentProjectionWorker) processWithHeartbeat(ctx context.Context, job ProcessingJob) error {
	processCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- w.processor.Process(processCtx, job)
	}()
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
				return fmt.Errorf("content job %s lost its lease during processing", job.ID)
			}
		case <-ctx.Done():
			cancel()
			<-result
			return ctx.Err()
		}
	}
}

func contentJobRetryDelay(attempts int, failure error) time.Duration {
	if errors.Is(failure, ErrProjectionOutOfOrder) {
		return 2 * time.Second
	}
	if attempts < 1 {
		attempts = 1
	}
	delay := time.Duration(1<<min(attempts-1, 6)) * time.Second
	return delay
}
