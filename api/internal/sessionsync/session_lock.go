package sessionsync

import (
	"context"
	"database/sql"
)

// LockSessionForUpdate establishes the common lock order for transactions
// that mutate a session and its source-derived data.
func LockSessionForUpdate(ctx context.Context, tx *sql.Tx, sessionID string) error {
	var lockedID string
	return tx.QueryRowContext(ctx, `
		SELECT id FROM sessions WHERE id = $1 FOR UPDATE`, sessionID).Scan(&lockedID)
}
