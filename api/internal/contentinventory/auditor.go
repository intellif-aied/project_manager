package contentinventory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/aidashboard/api/internal/contentreader"
)

type revisionReader interface {
	Stream(
		context.Context,
		contentreader.Request,
		func(contentreader.Event) error,
	) (contentreader.Result, error)
}

type Auditor struct {
	repository repository
	reader     revisionReader
}

func New(database *sql.DB, reader *contentreader.Reader) (*Auditor, error) {
	if database == nil || reader == nil {
		return nil, errors.New("database and content reader are required")
	}
	return newAuditor(&postgresRepository{db: database}, reader)
}

func newAuditor(repository repository, reader revisionReader) (*Auditor, error) {
	if repository == nil || reader == nil {
		return nil, errors.New("repository and content reader are required")
	}
	return &Auditor{repository: repository, reader: reader}, nil
}

func (a *Auditor) Run(ctx context.Context, options Options) (Report, error) {
	options.SnapshotThroughID = strings.TrimSpace(options.SnapshotThroughID)
	options.AfterRevisionID = strings.TrimSpace(options.AfterRevisionID)
	options.OnlyRevisionID = strings.TrimSpace(options.OnlyRevisionID)
	if err := options.validate(); err != nil {
		return Report{}, err
	}
	if options.Action == ActionPlan {
		through, count, err := a.repository.plan(ctx)
		if err != nil {
			return Report{}, err
		}
		return Report{
			Action: ActionPlan, SnapshotThroughID: through,
			EligibleRevisions: count, Complete: count == 0,
		}, nil
	}

	var candidates []candidate
	hasMore := false
	if options.OnlyRevisionID != "" {
		item, err := a.repository.find(ctx, options.OnlyRevisionID)
		if err != nil {
			return Report{}, err
		}
		candidates = []candidate{item}
	} else {
		var err error
		candidates, hasMore, err = a.repository.list(
			ctx, options.AfterRevisionID, options.SnapshotThroughID, options.Limit,
		)
		if err != nil {
			return Report{}, err
		}
	}

	report := Report{
		Action: ActionScan, SnapshotThroughID: options.SnapshotThroughID,
		AfterRevisionID: options.AfterRevisionID, MaxBytes: options.MaxBytes,
		Complete: !hasMore,
	}
	for _, item := range candidates {
		if err := ctx.Err(); err != nil {
			return Report{}, err
		}
		if item.StartCursor < 0 || item.EndCursor < item.StartCursor {
			report.SelectedRevisions++
			report.NextAfterRevisionID = item.RevisionID
			report.addFailure(item.RevisionID, fmt.Errorf(
				"invalid indexed range [%d,%d)", item.StartCursor, item.EndCursor,
			))
			continue
		}
		if item.EndCursor == item.StartCursor {
			report.SelectedRevisions++
			report.NextAfterRevisionID = item.RevisionID
			report.EmptyRevisions++
			report.SucceededRevisions++
			continue
		}
		revisionBytes := item.EndCursor - item.StartCursor
		if revisionBytes > options.MaxBytes-report.AttemptedBytes {
			report.ByteBudgetExhausted = true
			report.BudgetBlockedRevisionID = item.RevisionID
			break
		}
		report.SelectedRevisions++
		report.NextAfterRevisionID = item.RevisionID
		report.AttemptedBytes += revisionBytes
		revisionCtx, cancel := context.WithTimeout(ctx, options.PerRevisionTimeout)
		result, err := a.reader.Stream(revisionCtx, contentreader.Request{
			RevisionID:  item.RevisionID,
			StartCursor: item.StartCursor,
			EndCursor:   item.EndCursor,
		}, func(contentreader.Event) error { return nil })
		cancel()
		if err != nil {
			report.addFailure(item.RevisionID, err)
			continue
		}
		report.SucceededRevisions++
		report.ValidatedObjects += int64(result.ObjectCount)
		report.ValidatedEvents += result.EventCount
		report.ValidatedBytes += revisionBytes
	}
	report.Complete = !hasMore && !report.ByteBudgetExhausted && report.FailedRevisions == 0
	return report, nil
}

func (report *Report) addFailure(revisionID string, err error) {
	report.FailedRevisions++
	report.Failures = append(report.Failures, Failure{
		RevisionID: revisionID,
		Error:      err.Error(),
	})
}
