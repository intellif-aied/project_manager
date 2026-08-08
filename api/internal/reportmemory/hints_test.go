package reportmemory

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestLoadHistoricalHintsDoesNotExposeUnanchoredRecentProject(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	mock.ExpectQuery("SELECT project_memory_json").
		WithArgs("5192", "2026-07-31", ResolverVersion).
		WillReturnRows(sqlmock.NewRows([]string{"project_memory_json"}).AddRow(
			`{"projects":[{"project_ref":"project-1","canonical_name":"芯片验证平台","last_seen_on":"2026-07-30"}]}`,
		))
	hints, err := LoadHistoricalHints(context.Background(), tx, HintRequest{
		UserID: "5192", ReportDate: "2026-07-31",
		Facts: []FactInput{{FactRef: "fact-001", Text: "完成测试执行模块改造方案设计"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hints) != 0 {
		t.Fatalf("hints = %#v, want no unanchored candidate", hints)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadHistoricalHintsUsesWorkspaceAsBoundedWeakReference(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	mock.ExpectQuery("SELECT project_memory_json").
		WithArgs("21", "2026-08-05", ResolverVersion).
		WillReturnRows(sqlmock.NewRows([]string{"project_memory_json"}).AddRow(`{"projects":[{"project_ref":"project-1","canonical_name":"芯片验证平台","last_seen_on":"2026-08-04","workspace_refs":["workspace-1"]}]}`))
	mock.ExpectQuery("FROM report_run_fact_sources source").
		WithArgs("run-1", "21").
		WillReturnRows(sqlmock.NewRows([]string{"workspace_id", "fact_ref"}).AddRow("workspace-1", "fact-001"))
	hints, err := LoadHistoricalHints(context.Background(), tx, HintRequest{
		UserID: "21", RunID: "run-1", ReportDate: "2026-08-05",
		Facts: []FactInput{{FactRef: "fact-001", Text: "完成测试执行模块改造"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hints) != 1 || !hints[0].CandidateOnly || hints[0].MatchBasis != "workspace" {
		t.Fatalf("workspace hints = %#v", hints)
	}
	if len(hints[0].MatchedFactRef) != 1 || hints[0].MatchedFactRef[0] != "fact-001" {
		t.Fatalf("matched facts = %#v", hints[0].MatchedFactRef)
	}
}

func TestLoadHistoricalHintsSeparatesSemanticAnchorsFromWorkspaceCandidates(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	mock.ExpectQuery("SELECT project_memory_json").
		WithArgs("21", "2026-08-05", ResolverVersion).
		WillReturnRows(sqlmock.NewRows([]string{"project_memory_json"}).AddRow(`{"projects":[{"project_ref":"project-1","canonical_name":"芯片验证平台","last_seen_on":"2026-08-04","workstream_cues":["版本流"],"workspace_refs":["workspace-1"]}]}`))
	mock.ExpectQuery("FROM report_run_fact_sources source").
		WithArgs("run-1", "21").
		WillReturnRows(sqlmock.NewRows([]string{"workspace_id", "fact_ref"}).
			AddRow("workspace-1", "fact-001").AddRow("workspace-1", "fact-002"))
	hints, err := LoadHistoricalHints(context.Background(), tx, HintRequest{
		UserID: "21", RunID: "run-1", ReportDate: "2026-08-05",
		Facts: []FactInput{
			{FactRef: "fact-001", Text: "完成芯片验证平台版本流改造"},
			{FactRef: "fact-002", Text: "完成用例筛选工作台交互优化"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hints) != 1 || hints[0].MatchBasis != "workspace_semantic" || hints[0].CandidateOnly {
		t.Fatalf("hints = %#v", hints)
	}
	if len(hints[0].SemanticFactRef) != 1 || hints[0].SemanticFactRef[0] != "fact-001" {
		t.Fatalf("semantic anchors = %#v", hints[0].SemanticFactRef)
	}
	if len(hints[0].WorkspaceFactRef) != 2 {
		t.Fatalf("workspace candidates = %#v", hints[0].WorkspaceFactRef)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadHistoricalHintsUsesWorkstreamCueAsSemanticAnchor(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	mock.ExpectQuery("SELECT project_memory_json").
		WithArgs("21", "2026-08-06", ResolverVersion).
		WillReturnRows(sqlmock.NewRows([]string{"project_memory_json"}).AddRow(`{"projects":[{"project_ref":"project-1","canonical_name":"芯片验证平台","last_seen_on":"2026-08-05","workstream_cues":["调用执行"]}]}`))
	hints, err := LoadHistoricalHints(context.Background(), tx, HintRequest{
		UserID: "21", ReportDate: "2026-08-06",
		Facts: []FactInput{{FactRef: "fact-001", Text: "完成调用执行链路调整"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hints) != 1 || hints[0].CanonicalName != "芯片验证平台" || hints[0].CandidateOnly {
		t.Fatalf("workstream cue hints = %#v", hints)
	}
	if len(hints[0].WorkstreamCues) != 1 || hints[0].WorkstreamCues[0] != "调用执行" {
		t.Fatalf("workstream cues = %#v", hints[0].WorkstreamCues)
	}
}

func TestInputProjectSimilarityPrefersSharedWorkspace(t *testing.T) {
	shared := inputProjectSimilarity(
		InputProject{CanonicalName: "芯片验证平台", WorkspaceRefs: []string{"workspace-1"}},
		[]InputTheme{{Title: "测试执行模块", WorkspaceRefs: []string{"workspace-1"}}},
	)
	unrelated := inputProjectSimilarity(
		InputProject{CanonicalName: "另一个项目", WorkspaceRefs: []string{"workspace-2"}},
		[]InputTheme{{Title: "测试执行模块", WorkspaceRefs: []string{"workspace-1"}}},
	)
	if shared <= unrelated {
		t.Fatalf("shared workspace score=%v, unrelated=%v", shared, unrelated)
	}
}
