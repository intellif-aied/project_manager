package reportcontext

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/aidashboard/api/internal/biztime"
	"github.com/aidashboard/api/internal/reportsource"
)

func TestExtractContinuityThemesPreservesTwoLevelHierarchy(t *testing.T) {
	themes := extractContinuityThemes(`
- KV Cache 压缩算法研发
  - GLM-5.2 模型在苹果 800 的 OSCAR 算法适配任务
    - 完成精度测试并形成结论
  - 1-bit KV Cache 压缩算法研发
    - 阅读 RocketKV 论文
- 新员工 AI coding 培训
  - 完成培训材料准备
`)
	if len(themes) != 2 {
		t.Fatalf("themes = %#v, want two root themes", themes)
	}
	if themes[0].Title != "KV Cache 压缩算法研发" || len(themes[0].Children) != 2 ||
		themes[0].Children[0] != "GLM-5.2 模型在苹果 800 的 OSCAR 算法适配任务" ||
		themes[0].Children[1] != "1-bit KV Cache 压缩算法研发" {
		t.Fatalf("hierarchy was not preserved: %#v", themes[0])
	}
	if themes[1].Title != "新员工 AI coding 培训" || len(themes[1].Children) != 1 {
		t.Fatalf("second theme was not preserved: %#v", themes[1])
	}
}

func TestExtractContinuityThemesPrefersGeneratedOverview(t *testing.T) {
	themes := extractContinuityThemes(`
## 工作概览

1. GLM-5.2 DSpark 长生成评测与训练推进
2. Qwen3-4B 复现训练准备

## 工作详情

### GLM-5.2 DSpark 长生成评测与训练推进

1. 此处是当天成果，不应进入连续主题。
`)
	if len(themes) != 2 || themes[0].Title != "GLM-5.2 DSpark 长生成评测与训练推进" ||
		themes[1].Title != "Qwen3-4B 复现训练准备" {
		t.Fatalf("unexpected overview themes: %#v", themes)
	}
}

func TestExtractContinuityThemesKeepsPlainManualReport(t *testing.T) {
	themes := extractContinuityThemes("baigong demo 协议设计")
	if len(themes) != 1 || themes[0].Title != "baigong demo 协议设计" {
		t.Fatalf("plain manual report was lost: %#v", themes)
	}
}

func TestExtractContinuityThemesReadsSingleLayerDailyReport(t *testing.T) {
	themes := extractContinuityThemes(`1. 芯片验证平台：完成测试执行模块改造方案设计
2. Knowledge Map：完成产品判断并落地 knowledge-map-search Skill`)
	if len(themes) != 2 || themes[0].Title != "芯片验证平台：完成测试执行模块改造方案设计" ||
		themes[1].Title != "Knowledge Map：完成产品判断并落地 knowledge-map-search Skill" {
		t.Fatalf("single-layer daily report was not extracted: %#v", themes)
	}
}

func TestLoadContinuityContextUsesLatestThreeSavedReportsIncludingWeekend(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery(`(?s)FROM daily_reports r.*r\.report_date <.*r\.edited = true.*report_user_outcome_events.*ORDER BY r\.report_date DESC.*LIMIT`).
		WithArgs("62", "2026-08-03", continuityLookbackDays, continuityReportLimit).
		WillReturnRows(sqlmock.NewRows([]string{"report_date", "content"}).
			AddRow("2026-08-02", "- 周日加班主题").
			AddRow("2026-08-01", "- 周六加班主题").
			AddRow("2026-07-31", "- 周五主题"))

	continuity, err := loadContinuityContext(context.Background(), tx, BuildRequest{
		UserID: "62", ReportType: ReportTypePersonalDaily,
		Period:   reportsource.Period{Start: "2026-08-03", End: "2026-08-03"},
		Timezone: biztime.Zone, Target: Target{Type: "self", UserID: "62"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if continuity == nil || len(continuity.RecentReports) != 3 {
		t.Fatalf("continuity = %#v, want three reports", continuity)
	}
	if continuity.GroupingRule == "" || continuity.EvidenceRule == "" {
		t.Fatalf("continuity rules are incomplete: %#v", continuity)
	}
	if continuity.RecentReports[0].Date != "2026-08-02" || continuity.RecentReports[1].Date != "2026-08-01" {
		t.Fatalf("weekend reports were skipped: %#v", continuity.RecentReports)
	}
	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadContinuityContextSkipsNonDailyReports(t *testing.T) {
	continuity, err := loadContinuityContext(context.Background(), nil, BuildRequest{ReportType: ReportTypePersonalWeekly})
	if err != nil || continuity != nil {
		t.Fatalf("weekly continuity = %#v, err = %v", continuity, err)
	}
}
