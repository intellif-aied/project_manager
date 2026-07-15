package handler

import (
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/aidashboard/api/model"
)

func TestApplyRequirementPermissionsSetsCanDeleteForCreator(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery(`SELECT EXISTS\(`).
		WithArgs(testRequirementID, "303").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	h := NewRequirementHandler(db, nil)
	req := &model.Requirement{ID: testRequirementID, CreatorID: "303", Status: "todo"}
	h.applyRequirementPermissions(req, &model.User{ID: "303", Role: "employee"})

	if !req.CanDelete {
		t.Fatalf("CanDelete = false, want true for manageable creator")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

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

func TestNormalizeRequirementResponsibleUserIDsRequiresAtLeastOneOwner(t *testing.T) {
	h := &RequirementHandler{}
	for _, input := range [][]string{nil, {}, {"", " "}} {
		if _, err := h.normalizeRequirementResponsibleUserIDs(input); err == nil {
			t.Fatalf("normalizeRequirementResponsibleUserIDs(%#v) error = nil, want required error", input)
		}
	}
}

func TestNormalizeRequirementResponsibleUserIDsDeduplicatesOwners(t *testing.T) {
	h := &RequirementHandler{}
	got, err := h.normalizeRequirementResponsibleUserIDs([]string{"12", " 12 ", "34"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "12" || got[1] != "34" {
		t.Fatalf("normalized owners = %#v, want [12 34]", got)
	}
}
