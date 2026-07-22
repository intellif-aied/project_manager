package observability

import (
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMetricsExposeConfiguredWorkersAndBoundedLabels(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	metrics, err := New(database, WorkerCounts{ReportRun: 20, DigestBackground: 21, DigestInteractive: 22})
	if err != nil {
		t.Fatal(err)
	}
	SetDefault(metrics)
	t.Cleanup(func() { SetDefault(nil) })
	ObserveDigestClaim("interactive", time.Unix(10, 0), time.Unix(12, 0))
	ObserveDigestBuild("interactive", "completed", "none", 3*time.Second)
	ObserveDigestRebuild("rejected", "exhausted")
	ObserveReportLeaseExpired("building_context", 1)
	ObserveReportReconcile("lease_expired", "success", 1)
	ObserveDigestWakeup("noop")
	ObservePayload("item", payloadWarningBytes+1)

	mock.ExpectQuery("(?s)SELECT urgency, status, COUNT.*session_processing_jobs").
		WillReturnRows(sqlmock.NewRows([]string{"urgency", "status", "count"}).
			AddRow("interactive", "pending", 2))
	mock.ExpectQuery("(?s)SELECT urgency, COALESCE.*session_processing_jobs").
		WillReturnRows(sqlmock.NewRows([]string{"urgency", "seconds"}).
			AddRow("interactive", 4.0))
	mock.ExpectQuery("(?s)SELECT status, execution_stage, COUNT.*FROM ai_runs").
		WillReturnRows(sqlmock.NewRows([]string{"status", "stage", "count", "seconds"}).
			AddRow("pending", "waiting_digest", 3, 8.0))

	workerText := testutil.ToFloat64(metrics.workerConfigured.WithLabelValues("report_run"))
	if workerText != 20 {
		t.Fatalf("report worker gauge = %v", workerText)
	}
	families, err := metrics.registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, family := range families {
		found[family.GetName()] = true
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				name := label.GetName()
				if strings.Contains(name, "run_id") || strings.Contains(name, "user_id") || strings.Contains(name, "session_id") {
					t.Fatalf("high-cardinality label emitted: %s", name)
				}
			}
		}
	}
	for _, name := range []string{
		"aida_worker_configured", "aida_digest_jobs", "aida_report_runs",
		"aida_digest_build_total", "aida_digest_payload_over_warning_total",
	} {
		if !found[name] {
			t.Fatalf("metric family %s is missing", name)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
