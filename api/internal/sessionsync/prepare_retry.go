package sessionsync

import (
	"context"
	"errors"
	"time"

	"github.com/lib/pq"
)

var prepareDeadlockRetryDelays = []time.Duration{50 * time.Millisecond, 100 * time.Millisecond}

func retryPrepareAfterDeadlock(
	ctx context.Context,
	operation func() ([]PrepareSourceResponse, error),
) ([]PrepareSourceResponse, error) {
	return retryPrepareAfterDeadlockWithDelays(ctx, operation, prepareDeadlockRetryDelays)
}

func retryPrepareAfterDeadlockWithDelays(
	ctx context.Context,
	operation func() ([]PrepareSourceResponse, error),
	delays []time.Duration,
) ([]PrepareSourceResponse, error) {
	for attempt := 0; ; attempt++ {
		result, err := operation()
		if err == nil || !isPostgresDeadlock(err) || attempt >= len(delays) {
			return result, err
		}

		timer := time.NewTimer(delays[attempt])
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func isPostgresDeadlock(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && string(pqErr.Code) == "40P01"
}
