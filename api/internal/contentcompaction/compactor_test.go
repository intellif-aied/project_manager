package contentcompaction

import (
	"testing"
	"time"
)

func TestOptionsRequireBoundedAndExplicitMutations(t *testing.T) {
	tests := []struct {
		name    string
		options Options
		wantErr bool
	}{
		{name: "plan", options: Options{Action: ActionPlan}},
		{name: "plan apply rejected", options: Options{Action: ActionPlan, Apply: true}, wantErr: true},
		{name: "unbounded copy rejected", options: Options{Action: ActionCopy, Apply: true}, wantErr: true},
		{name: "bounded copy", options: Options{Action: ActionCopy, Apply: true, BatchSize: 100, MaxBatches: 1}},
		{name: "copy batch too large", options: Options{Action: ActionCopy, Apply: true, BatchSize: MaximumBatchSize + 1, MaxBatches: 1}, wantErr: true},
		{name: "copy run too large", options: Options{Action: ActionCopy, Apply: true, BatchSize: 1, MaxBatches: MaximumBatchesPerRun + 1}, wantErr: true},
		{name: "cutover expected required", options: Options{Action: ActionCutover, Apply: true, ExpectedSourceRows: -1}, wantErr: true},
		{name: "cutover zero allowed", options: Options{Action: ActionCutover, Apply: true, ExpectedSourceRows: 0}},
		{name: "finalize confirmation rejected", options: Options{Action: ActionFinalize, Apply: true, ConfirmDrop: "yes"}, wantErr: true},
		{name: "finalize exact confirmation", options: Options{Action: ActionFinalize, Apply: true, ConfirmDrop: ArchiveTable}},
		{name: "critical statement timeout too large", options: Options{Action: ActionPlan, StatementTimeout: MaximumCriticalStatementTimeout + time.Second}, wantErr: true},
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
