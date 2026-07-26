package sessionsync

import (
	"context"
	"errors"
	"time"

	"github.com/lib/pq"
)

var postgresConflictRetryDelays = []time.Duration{50 * time.Millisecond, 100 * time.Millisecond}

func retryPostgresConflict[T any](ctx context.Context, operation func() (T, error)) (T, error) {
	return retryPostgresConflictWithDelays(ctx, operation, postgresConflictRetryDelays)
}

func retryPostgresConflictWithDelays[T any](
	ctx context.Context,
	operation func() (T, error),
	delays []time.Duration,
) (T, error) {
	for attempt := 0; ; attempt++ {
		result, err := operation()
		if err == nil || !isPostgresConflict(err) || attempt >= len(delays) {
			return result, err
		}

		timer := time.NewTimer(delays[attempt])
		select {
		case <-ctx.Done():
			timer.Stop()
			var zero T
			return zero, ctx.Err()
		case <-timer.C:
		}
	}
}

func isPostgresConflict(err error) bool {
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) {
		return false
	}
	code := string(pqErr.Code)
	return code == "40P01" || code == "40001"
}
