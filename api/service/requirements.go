package service

import (
	"time"

	"github.com/aidashboard/api/model"
)

const (
	TaskRiskBlocked            = "blocked"
	TaskRiskOverdue            = "overdue"
	TaskRiskDependencyConflict = "dependency_conflict"
)

// DeriveTaskRisks is the single P0 source of truth for task risks.
// blocked is never persisted as task.status.
func DeriveTaskRisks(task model.Task, now time.Time) []string {
	if task.Status == "done" {
		return []string{}
	}

	risks := make([]string, 0, 3)
	today := startOfDay(now)
	hasDependencyConflict := false
	for _, dependency := range task.Dependencies {
		if dependencyDone(dependency) {
			continue
		}
		if dependencyBlocksTask(task, dependency, today) {
			risks = append(risks, TaskRiskBlocked)
			break
		}
		if dependencyScheduleConflicts(task, dependency) {
			hasDependencyConflict = true
		}
	}

	if due, ok := parseOptionalDate(task.DueDate); ok && due.Before(today) {
		return append(risks, TaskRiskOverdue)
	}
	if hasDependencyConflict {
		risks = append(risks, TaskRiskDependencyConflict)
	}
	return risks
}

func dependencyDone(dependency model.TaskDep) bool {
	if dependency.ItemType == "requirement" {
		return dependency.Status == "completed"
	}
	return dependency.Status == "done"
}

func dependencyBlocksTask(task model.Task, dependency model.TaskDep, today time.Time) bool {
	if task.Status == "in_progress" {
		return true
	}
	if due, ok := parseOptionalDate(task.DueDate); ok && !due.After(today) {
		return true
	}
	if dependencyDue, ok := parseOptionalDate(dependency.DueDate); ok && dependencyDue.Before(today) {
		return true
	}
	return false
}

func dependencyScheduleConflicts(task model.Task, dependency model.TaskDep) bool {
	taskDue, taskDueOK := parseOptionalDate(task.DueDate)
	dependencyDue, dependencyDueOK := parseOptionalDate(dependency.DueDate)
	if !taskDueOK || !dependencyDueOK {
		return false
	}
	return !dependencyDue.Before(taskDue)
}

func parseOptionalDate(value *string) (time.Time, bool) {
	if value == nil || *value == "" {
		return time.Time{}, false
	}
	return parseTaskDate(*value)
}

func startOfDay(value time.Time) time.Time {
	utc := value.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}

func parseTaskDate(value string) (time.Time, bool) {
	if parsed, err := time.Parse("2006-01-02", value); err == nil {
		return parsed.UTC(), true
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return time.Date(parsed.UTC().Year(), parsed.UTC().Month(), parsed.UTC().Day(), 0, 0, 0, 0, time.UTC), true
	}
	return time.Time{}, false
}

func DisplayTaskStatus(task model.Task) string {
	if task.Status == "done" {
		return "done"
	}
	for _, risk := range task.RiskTypes {
		if risk == TaskRiskBlocked {
			return "blocked"
		}
	}
	return task.Status
}

func AggregateRequirementProgress(tasks []model.Task) int {
	if len(tasks) == 0 {
		return 0
	}
	total := 0
	for _, task := range tasks {
		total += task.Progress
	}
	return total / len(tasks)
}

func SummarizeRequirementTasks(tasks []model.Task) (model.RequirementTaskSummary, model.RequirementRiskSummary) {
	taskSummary := model.RequirementTaskSummary{Total: len(tasks)}
	riskSummary := model.RequirementRiskSummary{}
	for _, task := range tasks {
		if task.Status == "done" {
			taskSummary.Done++
		}
		for _, risk := range task.RiskTypes {
			switch risk {
			case TaskRiskBlocked:
				taskSummary.Blocked++
				riskSummary.Blocked++
			case TaskRiskOverdue:
				riskSummary.Overdue++
			case TaskRiskDependencyConflict:
				riskSummary.DependencyConflict++
			}
		}
	}
	return taskSummary, riskSummary
}

func SummarizeRequirement(requirement model.Requirement, tasks []model.Task, now time.Time) (model.RequirementTaskSummary, model.RequirementRiskSummary) {
	taskSummary, riskSummary := SummarizeRequirementTasks(tasks)
	if IsRequirementOverdue(requirement, now) {
		riskSummary.RequirementOverdue = 1
	}
	for _, dependency := range requirement.Dependencies {
		if !dependencyDone(dependency) {
			riskSummary.DependencyConflict++
		}
	}
	return taskSummary, riskSummary
}

func IsRequirementOverdue(requirement model.Requirement, now time.Time) bool {
	if requirement.Status == "completed" || requirement.Status == "cancelled" {
		return false
	}
	deadline, ok := parseOptionalDate(requirement.Deadline)
	return ok && deadline.Before(startOfDay(now))
}
