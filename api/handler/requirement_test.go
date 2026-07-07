package handler

import (
	"strings"
	"testing"
)

func TestAppendRequirementRiskFilterDependencyConflictDoesNotAddUnusedDateArg(t *testing.T) {
	h := &RequirementHandler{}
	where := []string{}
	args := []any{}

	h.appendRequirementRiskFilter(&where, &args, "dependency_conflict")

	if len(args) != 0 {
		t.Fatalf("args len = %d, want 0; args=%#v", len(args), args)
	}
	if len(where) != 1 {
		t.Fatalf("where len = %d, want 1; where=%#v", len(where), where)
	}
	if !strings.Contains(where[0], "work_item_relations") {
		t.Fatalf("dependency conflict where = %q, want relation risk SQL", where[0])
	}
}

func TestAppendRequirementRiskFilterDateBasedRisksAddDateArg(t *testing.T) {
	tests := []string{"requirement_overdue", "task_overdue", "blocked"}

	for _, risk := range tests {
		t.Run(risk, func(t *testing.T) {
			h := &RequirementHandler{}
			where := []string{}
			args := []any{}

			h.appendRequirementRiskFilter(&where, &args, risk)

			if len(args) != 1 {
				t.Fatalf("args len = %d, want 1; args=%#v", len(args), args)
			}
			if len(where) != 1 {
				t.Fatalf("where len = %d, want 1; where=%#v", len(where), where)
			}
		})
	}
}
