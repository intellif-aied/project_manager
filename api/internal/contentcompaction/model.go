package contentcompaction

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	SourceTable  = "session_content_events"
	ShadowTable  = "session_content_events_compact"
	ArchiveTable = "session_content_events_payload_archive"
	StateTable   = "session_content_events_compaction_state"
	BatchTable   = "session_content_events_compaction_batches"

	MirrorFunction = "mirror_session_content_events_compaction"
	MirrorTrigger  = "trg_mirror_session_content_events_compaction"
)

type Action string

const (
	ActionPlan      Action = "plan"
	ActionCopy      Action = "copy"
	ActionMirror    Action = "mirror"
	ActionReconcile Action = "reconcile"
	ActionVerify    Action = "verify"
	ActionCutover   Action = "cutover"
	ActionRollback  Action = "rollback"
	ActionFinalize  Action = "finalize"
)

type Options struct {
	Action             Action
	Apply              bool
	BatchSize          int
	MaxBatches         int
	ExpectedSourceRows int64
	ConfirmDrop        string
	LockTimeout        time.Duration
}

type Report struct {
	Action             Action `json:"action"`
	Applied            bool   `json:"applied"`
	Phase              string `json:"phase"`
	SourceTable        string `json:"source_table"`
	TargetTable        string `json:"target_table"`
	SourceRows         int64  `json:"source_rows"`
	TargetRows         int64  `json:"target_rows"`
	RowCountsExact     bool   `json:"row_counts_exact"`
	MissingRows        int64  `json:"missing_rows,omitempty"`
	ExtraRows          int64  `json:"extra_rows,omitempty"`
	MismatchedRows     int64  `json:"mismatched_rows,omitempty"`
	SourceBytes        int64  `json:"source_relation_bytes"`
	TargetBytes        int64  `json:"target_relation_bytes"`
	SourceHasPayload   bool   `json:"source_has_content_payload"`
	TargetHasPayload   bool   `json:"target_has_content_payload"`
	ProcessedRows      int64  `json:"processed_rows,omitempty"`
	ProcessedBatches   int    `json:"processed_batches,omitempty"`
	Complete           bool   `json:"complete"`
	CopyCursor         string `json:"copy_cursor,omitempty"`
	MissingCursor      string `json:"reconcile_missing_cursor,omitempty"`
	ExtraCursor        string `json:"reconcile_extra_cursor,omitempty"`
	ExpectedSourceRows int64  `json:"expected_source_rows,omitempty"`
}

type migrationState struct {
	Phase                 string
	CopyCursor            string
	MissingCursor         string
	ExtraCursor           string
	MissingComplete       bool
	ExtraComplete         bool
	SourceRowsAtStart     int64
	CopiedRows            int64
	ReconciledMissingRows int64
	ReconciledExtraRows   int64
	SourceTable           string
	ShadowTable           string
	ArchiveTable          string
}

func (options Options) validate() error {
	switch options.Action {
	case ActionPlan, ActionVerify:
		if options.Apply {
			return errors.New("plan and verify are read-only and do not accept --apply")
		}
	case ActionCopy, ActionReconcile:
		if !options.Apply {
			return fmt.Errorf("%s requires --apply", options.Action)
		}
		if options.BatchSize <= 0 || options.MaxBatches <= 0 {
			return fmt.Errorf("%s requires positive --batch-size and --max-batches", options.Action)
		}
	case ActionMirror:
		if !options.Apply {
			return errors.New("mirror requires --apply")
		}
	case ActionCutover, ActionRollback:
		if !options.Apply {
			return fmt.Errorf("%s requires --apply", options.Action)
		}
		if options.ExpectedSourceRows < 0 {
			return fmt.Errorf("%s requires non-negative --expected-source-rows", options.Action)
		}
	case ActionFinalize:
		if !options.Apply {
			return errors.New("finalize requires --apply")
		}
		if strings.TrimSpace(options.ConfirmDrop) != ArchiveTable {
			return fmt.Errorf("finalize requires --confirm-drop %s", ArchiveTable)
		}
	default:
		return fmt.Errorf("unsupported compaction action %q", options.Action)
	}
	if options.LockTimeout < 0 {
		return errors.New("lock timeout must not be negative")
	}
	return nil
}

func checkedTable(name string) (string, error) {
	switch name {
	case SourceTable, ShadowTable, ArchiveTable:
		return `"` + name + `"`, nil
	default:
		return "", fmt.Errorf("unexpected compaction table %q", name)
	}
}
