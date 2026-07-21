package sessionsync

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lib/pq"
)

func TestRetryPrepareAfterDeadlockSucceeds(t *testing.T) {
	attempts := 0
	result, err := retryPrepareAfterDeadlockWithDelays(context.Background(), func() ([]PrepareSourceResponse, error) {
		attempts++
		if attempts < 3 {
			return nil, &pq.Error{Code: "40P01"}
		}
		return []PrepareSourceResponse{{SessionRef: "session-1"}}, nil
	}, []time.Duration{0, 0})
	if err != nil || attempts != 3 || len(result) != 1 || result[0].SessionRef != "session-1" {
		t.Fatalf("attempts=%d result=%+v err=%v", attempts, result, err)
	}
}

func TestRetryPrepareAfterDeadlockDoesNotRetryOtherErrors(t *testing.T) {
	attempts := 0
	wantErr := errors.New("ordinary failure")
	_, err := retryPrepareAfterDeadlockWithDelays(context.Background(), func() ([]PrepareSourceResponse, error) {
		attempts++
		return nil, wantErr
	}, []time.Duration{0, 0})
	if !errors.Is(err, wantErr) || attempts != 1 {
		t.Fatalf("attempts=%d err=%v", attempts, err)
	}
}

func TestRetryPrepareAfterDeadlockStopsWhenContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	_, err := retryPrepareAfterDeadlockWithDelays(ctx, func() ([]PrepareSourceResponse, error) {
		attempts++
		cancel()
		return nil, &pq.Error{Code: "40P01"}
	}, []time.Duration{time.Hour})
	if !errors.Is(err, context.Canceled) || attempts != 1 {
		t.Fatalf("attempts=%d err=%v", attempts, err)
	}
}
