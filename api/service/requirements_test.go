package service

import (
	"reflect"
	"testing"
	"time"

	"github.com/aidashboard/api/model"
)

func TestDeriveTaskRisks(t *testing.T) {
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	overdue := "2026-06-23"
	overdueFromDatabase := "2026-06-23T00:00:00Z"
	today := "2026-06-24"
	notDueSoon := "2026-06-26"
	dependencyDueSameDay := "2026-06-26"
	dependencyDueAfterTask := "2026-06-28"

	tests := []struct {
		name string
		task model.Task
		want []string
	}{
		{
			name: "blocked and overdue are both derived",
			task: model.Task{
				Status:       "in_progress",
				DueDate:      &overdue,
				Dependencies: []model.TaskDep{{Status: "todo"}},
			},
			want: []string{TaskRiskBlocked, TaskRiskOverdue},
		},
		{
			name: "database date serialization is supported",
			task: model.Task{Status: "todo", DueDate: &overdueFromDatabase},
			want: []string{TaskRiskOverdue},
		},
		{
			name: "future deadline does not create risk",
			task: model.Task{Status: "todo", DueDate: &notDueSoon},
			want: []string{},
		},
		{
			name: "future task with same-day unfinished dependency creates schedule conflict",
			task: model.Task{
				Status:  "todo",
				DueDate: &notDueSoon,
				Dependencies: []model.TaskDep{{
					Status:  "todo",
					DueDate: &dependencyDueSameDay,
				}},
			},
			want: []string{TaskRiskDependencyConflict},
		},
		{
			name: "future task with dependency due after task creates schedule conflict",
			task: model.Task{
				Status:  "todo",
				DueDate: &notDueSoon,
				Dependencies: []model.TaskDep{{
					Status:  "todo",
					DueDate: &dependencyDueAfterTask,
				}},
			},
			want: []string{TaskRiskDependencyConflict},
		},
		{
			name: "deadline today does not create overdue risk",
			task: model.Task{Status: "todo", DueDate: &today},
			want: []string{},
		},
		{
			name: "empty deadline does not create overdue risk",
			task: model.Task{Status: "todo"},
			want: []string{},
		},
		{
			name: "done task has no risks",
			task: model.Task{
				Status:       "done",
				DueDate:      &overdue,
				Dependencies: []model.TaskDep{{Status: "todo"}},
			},
			want: []string{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := DeriveTaskRisks(test.task, now); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("DeriveTaskRisks() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestSummarizeRequirementIncludesOwnDeadline(t *testing.T) {
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	deadline := "2026-06-23"
	taskDue := "2026-06-23"
	req := model.Requirement{Status: "active", Deadline: &deadline}
	tasks := []model.Task{{Status: "todo", DueDate: &taskDue}}
	tasks[0].RiskTypes = DeriveTaskRisks(tasks[0], now)

	_, riskSummary := SummarizeRequirement(req, tasks, now)
	if riskSummary.RequirementOverdue != 1 {
		t.Fatalf("RequirementOverdue = %d, want 1", riskSummary.RequirementOverdue)
	}
	if riskSummary.Overdue != 1 {
		t.Fatalf("Overdue = %d, want 1", riskSummary.Overdue)
	}
}

func TestAggregateRequirementProgress(t *testing.T) {
	tasks := []model.Task{{Progress: 25}, {Progress: 75}, {Progress: 100}}
	if got, want := AggregateRequirementProgress(tasks), 66; got != want {
		t.Fatalf("AggregateRequirementProgress() = %d, want %d", got, want)
	}
	if got := AggregateRequirementProgress(nil); got != 0 {
		t.Fatalf("AggregateRequirementProgress(nil) = %d, want 0", got)
	}
}

func TestDeriveTaskRisksUsesBusinessDateBoundary(t *testing.T) {
	now := time.Date(2026, 7, 5, 16, 30, 0, 0, time.UTC) // 2026-07-06 00:30 Asia/Shanghai.
	yesterday := "2026-07-05"
	today := "2026-07-06"

	if got := DeriveTaskRisks(model.Task{Status: "todo", DueDate: &yesterday}, now); !reflect.DeepEqual(got, []string{TaskRiskOverdue}) {
		t.Fatalf("yesterday risks = %#v, want overdue", got)
	}
	if got := DeriveTaskRisks(model.Task{Status: "todo", DueDate: &today}, now); len(got) != 0 {
		t.Fatalf("today risks = %#v, want none", got)
	}
}
