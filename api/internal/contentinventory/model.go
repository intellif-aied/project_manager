package contentinventory

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type Action string

const (
	ActionPlan Action = "plan"
	ActionScan Action = "scan"

	MaximumRevisionsPerRun = 50
	MaximumBytesPerRun     = int64(10 << 30)
)

type Options struct {
	Action             Action
	SnapshotThroughID  string
	AfterRevisionID    string
	OnlyRevisionID     string
	Limit              int
	MaxBytes           int64
	PerRevisionTimeout time.Duration
}

type Failure struct {
	RevisionID string `json:"revision_id"`
	Error      string `json:"error"`
}

type Report struct {
	Action                  Action    `json:"action"`
	SnapshotThroughID       string    `json:"snapshot_through_revision_id,omitempty"`
	AfterRevisionID         string    `json:"after_revision_id,omitempty"`
	NextAfterRevisionID     string    `json:"next_after_revision_id,omitempty"`
	EligibleRevisions       int64     `json:"eligible_revisions,omitempty"`
	SelectedRevisions       int       `json:"selected_revisions,omitempty"`
	SucceededRevisions      int       `json:"succeeded_revisions,omitempty"`
	EmptyRevisions          int       `json:"empty_revisions,omitempty"`
	FailedRevisions         int       `json:"failed_revisions,omitempty"`
	ValidatedObjects        int64     `json:"validated_objects,omitempty"`
	ValidatedEvents         int64     `json:"validated_events,omitempty"`
	ValidatedBytes          int64     `json:"validated_bytes,omitempty"`
	AttemptedBytes          int64     `json:"attempted_bytes,omitempty"`
	MaxBytes                int64     `json:"max_bytes,omitempty"`
	ByteBudgetExhausted     bool      `json:"byte_budget_exhausted,omitempty"`
	BudgetBlockedRevisionID string    `json:"budget_blocked_revision_id,omitempty"`
	Complete                bool      `json:"complete"`
	Failures                []Failure `json:"failures,omitempty"`
}

type candidate struct {
	RevisionID  string
	StartCursor int64
	EndCursor   int64
}

func (options Options) validate() error {
	switch options.Action {
	case ActionPlan:
		if options.SnapshotThroughID != "" || options.AfterRevisionID != "" ||
			options.OnlyRevisionID != "" || options.Limit != 0 || options.MaxBytes != 0 ||
			options.PerRevisionTimeout != 0 {
			return errors.New("plan does not accept scan options")
		}
	case ActionScan:
		options.SnapshotThroughID = strings.TrimSpace(options.SnapshotThroughID)
		options.AfterRevisionID = strings.TrimSpace(options.AfterRevisionID)
		options.OnlyRevisionID = strings.TrimSpace(options.OnlyRevisionID)
		if options.OnlyRevisionID != "" {
			if options.SnapshotThroughID != "" || options.AfterRevisionID != "" || options.Limit != 0 {
				return errors.New("single revision scan does not accept snapshot, after, or limit")
			}
		} else {
			if options.SnapshotThroughID == "" {
				return errors.New("scan requires --snapshot-through-revision-id from plan")
			}
			if options.Limit <= 0 || options.Limit > MaximumRevisionsPerRun {
				return fmt.Errorf("scan --limit must be between 1 and %d", MaximumRevisionsPerRun)
			}
		}
		if options.PerRevisionTimeout <= 0 {
			return errors.New("scan requires a positive per-revision timeout")
		}
		if options.MaxBytes <= 0 || options.MaxBytes > MaximumBytesPerRun {
			return fmt.Errorf("scan --max-bytes must be between 1 and %d", MaximumBytesPerRun)
		}
	default:
		return fmt.Errorf("unsupported content inventory action %q", options.Action)
	}
	return nil
}
