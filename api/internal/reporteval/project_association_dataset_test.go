package reporteval

import "testing"

func TestProjectAssociationDatasetValidatesRequiredAndSeparateProjects(t *testing.T) {
	dataset := ProjectAssociationDataset{
		SchemaVersion:  ProjectAssociationDatasetSchemaVersion,
		DatasetVersion: "project-association-regression/v1", ControlledArchiveVersion: "archive/v1",
		Cases: []ProjectAssociationCase{{
			CaseID: "pa-203", ReportDate: "2026-08-05",
			SourceSetSHA256: "8c58ea39e2167716f0c3dbfb468d9c066dd6656e3aea12b4a6c8c5ab9bd7147a",
			SourceItemCount: 3, Tags: []string{"must-not-overmerge"},
			ExpectedWorkstreams: []ExpectedProjectWorkstream{
				{CanonicalName: "KV Cache 压缩算法研发", Requirement: "required", FactScope: "matching_facts"},
				{CanonicalName: "v5 精度评测套件", Requirement: "required", FactScope: "matching_facts"},
			},
			ForbiddenMerges: []ForbiddenProjectMerge{{Left: "KV Cache 压缩算法研发", Right: "v5 精度评测套件"}},
		}},
	}
	if err := dataset.Validate(); err != nil {
		t.Fatal(err)
	}
	dataset.Cases[0].ForbiddenMerges[0].Right = "未知项目"
	if err := dataset.Validate(); err == nil {
		t.Fatal("unknown project in forbidden merge must be rejected")
	}
}

func TestEvaluateProjectAssociationUsesDeterministicProjectGates(t *testing.T) {
	dataset := ProjectAssociationDataset{
		SchemaVersion: ProjectAssociationDatasetSchemaVersion, DatasetVersion: "project-association-regression/v1", ControlledArchiveVersion: "archive/v1",
		Cases: []ProjectAssociationCase{{
			CaseID: "pa-203", ReportDate: "2026-08-05", SourceSetSHA256: "8c58ea39e2167716f0c3dbfb468d9c066dd6656e3aea12b4a6c8c5ab9bd7147a",
			SourceItemCount: 3, Tags: []string{"must-not-overmerge"},
			ExpectedWorkstreams: []ExpectedProjectWorkstream{
				{CanonicalName: "KV Cache 压缩算法研发", Aliases: []string{"OSCAR"}, Requirement: "required", FactScope: "matching_facts"},
				{CanonicalName: "v5 精度评测套件", Aliases: []string{"精度评测套件"}, Requirement: "required", FactScope: "matching_facts"},
			}, ForbiddenMerges: []ForbiddenProjectMerge{{Left: "KV Cache 压缩算法研发", Right: "v5 精度评测套件"}},
		}},
	}
	candidates := ProjectAssociationCandidates{SchemaVersion: ProjectAssociationCandidatesSchemaVersion, DatasetVersion: dataset.DatasetVersion,
		Cases: []ProjectAssociationCandidate{{CaseID: "pa-203", RunID: "run-pass", WorkstreamSubjects: []string{"KV Cache 压缩算法研发", "精度评测套件"}}},
	}
	result, err := EvaluateProjectAssociation(dataset, candidates)
	if err != nil || !result.Passed {
		t.Fatalf("expected separated projects to pass: result=%+v err=%v", result, err)
	}
	candidates.Cases[0].WorkstreamSubjects = []string{"KV Cache 压缩算法研发与精度评测套件"}
	result, err = EvaluateProjectAssociation(dataset, candidates)
	if err != nil || result.Passed || len(result.Cases[0].Errors) == 0 {
		t.Fatalf("expected merged projects to fail: result=%+v err=%v", result, err)
	}
}
