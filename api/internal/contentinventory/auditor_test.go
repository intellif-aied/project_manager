package contentinventory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aidashboard/api/internal/contentreader"
)

type fakeRepository struct {
	through  string
	eligible int64
	items    []candidate
	hasMore  bool
	found    candidate
}

func (r *fakeRepository) plan(context.Context) (string, int64, error) {
	return r.through, r.eligible, nil
}

func (r *fakeRepository) list(context.Context, string, string, int) ([]candidate, bool, error) {
	return r.items, r.hasMore, nil
}

func (r *fakeRepository) find(context.Context, string) (candidate, error) {
	return r.found, nil
}

type fakeReader struct {
	results map[string]contentreader.Result
	errors  map[string]error
	calls   []string
}

func (r *fakeReader) Stream(
	_ context.Context,
	request contentreader.Request,
	_ func(contentreader.Event) error,
) (contentreader.Result, error) {
	r.calls = append(r.calls, request.RevisionID)
	return r.results[request.RevisionID], r.errors[request.RevisionID]
}

func TestAuditorPlanAndBoundedScan(t *testing.T) {
	repository := &fakeRepository{
		through:  "00000000-0000-0000-0000-000000000099",
		eligible: 4,
		items: []candidate{
			{RevisionID: "r1", StartCursor: 0, EndCursor: 20},
			{RevisionID: "r2", StartCursor: 10, EndCursor: 10},
			{RevisionID: "r3", StartCursor: 0, EndCursor: 30},
		},
		hasMore: true,
	}
	reader := &fakeReader{
		results: map[string]contentreader.Result{
			"r1": {ObjectCount: 2, EventCount: 3},
		},
		errors: map[string]error{"r3": errors.New("object hash mismatch")},
	}
	auditor, err := newAuditor(repository, reader)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := auditor.Run(context.Background(), Options{Action: ActionPlan})
	if err != nil {
		t.Fatal(err)
	}
	if plan.SnapshotThroughID != repository.through || plan.EligibleRevisions != 4 || plan.Complete {
		t.Fatalf("plan=%+v", plan)
	}
	report, err := auditor.Run(context.Background(), Options{
		Action: ActionScan, SnapshotThroughID: repository.through,
		Limit: 3, PerRevisionTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Complete || report.SelectedRevisions != 3 || report.SucceededRevisions != 2 ||
		report.EmptyRevisions != 1 || report.FailedRevisions != 1 ||
		report.ValidatedObjects != 2 || report.ValidatedEvents != 3 ||
		report.ValidatedBytes != 20 || report.NextAfterRevisionID != "r3" {
		t.Fatalf("scan=%+v", report)
	}
	if len(reader.calls) != 2 || reader.calls[0] != "r1" || reader.calls[1] != "r3" {
		t.Fatalf("reader calls=%v", reader.calls)
	}
}

func TestInventoryOptionsStayBounded(t *testing.T) {
	tests := []struct {
		name    string
		options Options
		wantErr bool
	}{
		{name: "plan", options: Options{Action: ActionPlan}},
		{name: "plan scan options", options: Options{Action: ActionPlan, Limit: 1}, wantErr: true},
		{name: "scan no snapshot", options: Options{Action: ActionScan, Limit: 1, PerRevisionTimeout: time.Second}, wantErr: true},
		{name: "scan unbounded", options: Options{Action: ActionScan, SnapshotThroughID: "id", Limit: MaximumRevisionsPerRun + 1, PerRevisionTimeout: time.Second}, wantErr: true},
		{name: "scan bounded", options: Options{Action: ActionScan, SnapshotThroughID: "id", Limit: 1, PerRevisionTimeout: time.Second}},
		{name: "single retry", options: Options{Action: ActionScan, OnlyRevisionID: "id", PerRevisionTimeout: time.Second}},
		{name: "single retry mixed", options: Options{Action: ActionScan, OnlyRevisionID: "id", Limit: 1, PerRevisionTimeout: time.Second}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.options.validate()
			if (err != nil) != test.wantErr {
				t.Fatalf("validate error=%v wantErr=%v", err, test.wantErr)
			}
		})
	}
}
