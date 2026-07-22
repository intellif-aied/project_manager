package observability

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const payloadWarningBytes = 1 << 20

type WorkerCounts struct {
	ReportRun         int
	DigestBackground  int
	DigestInteractive int
}

type Metrics struct {
	registry *prometheus.Registry
	database *sql.DB

	digestClaimLatency  *prometheus.HistogramVec
	digestBuildDuration *prometheus.HistogramVec
	digestBuildTotal    *prometheus.CounterVec
	digestRebuildTotal  *prometheus.CounterVec
	reportLeaseExpired  *prometheus.CounterVec
	reportReconcile     *prometheus.CounterVec
	reportDigestWakeup  *prometheus.CounterVec
	workerConfigured    *prometheus.GaugeVec
	payloadBytes        *prometheus.HistogramVec
	payloadWarning      *prometheus.CounterVec
}

var defaultMetrics atomic.Pointer[Metrics]

func New(database *sql.DB, workers WorkerCounts) (*Metrics, error) {
	if database == nil {
		return nil, sql.ErrConnDone
	}
	registry := prometheus.NewRegistry()
	m := &Metrics{
		registry: registry,
		database: database,
		digestClaimLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "aida_digest_claim_latency_seconds", Help: "Eligible-to-lease latency for Digest jobs.",
			Buckets: prometheus.DefBuckets,
		}, []string{"urgency"}),
		digestBuildDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "aida_digest_build_duration_seconds", Help: "Digest build execution duration.",
			Buckets: prometheus.DefBuckets,
		}, []string{"urgency", "result"}),
		digestBuildTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "aida_digest_build_total", Help: "Digest build outcomes.",
		}, []string{"urgency", "result", "failure_class"}),
		digestRebuildTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "aida_digest_rebuild_total", Help: "Controlled Digest rebuild decisions.",
		}, []string{"result", "reason"}),
		reportLeaseExpired: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "aida_report_run_lease_expired_total", Help: "Expired Report Run leases reclaimed.",
		}, []string{"stage"}),
		reportReconcile: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "aida_report_run_reconcile_total", Help: "Report Run reconciler outcomes.",
		}, []string{"reason", "result"}),
		reportDigestWakeup: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "aida_report_run_digest_wakeup_total", Help: "Waiting Report Runs woken by Digest terminal events.",
		}, []string{"result"}),
		workerConfigured: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "aida_worker_configured", Help: "Workers configured in this API process.",
		}, []string{"role"}),
		payloadBytes: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "aida_digest_payload_bytes", Help: "Complete Digest and Report Context payload sizes.",
			Buckets: []float64{64 << 10, 256 << 10, 1 << 20, 4 << 20, 16 << 20, 64 << 20, 256 << 20},
		}, []string{"scope"}),
		payloadWarning: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "aida_digest_payload_over_warning_total", Help: "Payloads above the advisory one MiB threshold.",
		}, []string{"scope"}),
	}
	registry.MustRegister(
		&databaseCollector{database: database},
		m.digestClaimLatency, m.digestBuildDuration, m.digestBuildTotal,
		m.digestRebuildTotal, m.reportLeaseExpired, m.reportReconcile,
		m.reportDigestWakeup, m.workerConfigured, m.payloadBytes, m.payloadWarning,
	)
	m.workerConfigured.WithLabelValues("report_run").Set(float64(workers.ReportRun))
	m.workerConfigured.WithLabelValues("digest_background").Set(float64(workers.DigestBackground))
	m.workerConfigured.WithLabelValues("digest_interactive").Set(float64(workers.DigestInteractive))
	return m, nil
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

func SetDefault(metrics *Metrics) { defaultMetrics.Store(metrics) }

func ObserveDigestClaim(urgency string, eligibleAt, claimedAt time.Time) {
	if m := defaultMetrics.Load(); m != nil && !eligibleAt.IsZero() {
		m.digestClaimLatency.WithLabelValues(urgency).Observe(maxDuration(claimedAt.Sub(eligibleAt)).Seconds())
	}
}

func ObserveDigestBuild(urgency, result, failureClass string, duration time.Duration) {
	if m := defaultMetrics.Load(); m != nil {
		m.digestBuildDuration.WithLabelValues(urgency, result).Observe(maxDuration(duration).Seconds())
		m.digestBuildTotal.WithLabelValues(urgency, result, failureClass).Inc()
	}
}

func ObserveDigestRebuild(result, reason string) {
	if m := defaultMetrics.Load(); m != nil {
		m.digestRebuildTotal.WithLabelValues(result, reason).Inc()
	}
}

func ObserveReportLeaseExpired(stage string, count int64) {
	if m := defaultMetrics.Load(); m != nil && count > 0 {
		m.reportLeaseExpired.WithLabelValues(stage).Add(float64(count))
	}
}

func ObserveReportReconcile(reason, result string, count int64) {
	if m := defaultMetrics.Load(); m != nil && count > 0 {
		m.reportReconcile.WithLabelValues(reason, result).Add(float64(count))
	}
}

func ObserveDigestWakeup(result string) {
	if m := defaultMetrics.Load(); m != nil {
		m.reportDigestWakeup.WithLabelValues(result).Inc()
	}
}

func ObservePayload(scope string, bytes int) {
	if m := defaultMetrics.Load(); m != nil && bytes >= 0 {
		m.payloadBytes.WithLabelValues(scope).Observe(float64(bytes))
		if bytes > payloadWarningBytes {
			m.payloadWarning.WithLabelValues(scope).Inc()
		}
	}
}

func maxDuration(value time.Duration) time.Duration {
	if value < 0 {
		return 0
	}
	return value
}

type databaseCollector struct {
	database *sql.DB
}

var (
	digestJobsDesc   = prometheus.NewDesc("aida_digest_jobs", "Active Digest jobs.", []string{"urgency", "status"}, nil)
	digestOldestDesc = prometheus.NewDesc("aida_digest_oldest_eligible_wait_seconds", "Oldest eligible Digest job wait.", []string{"urgency"}, nil)
	reportRunsDesc   = prometheus.NewDesc("aida_report_runs", "Active Report Runs.", []string{"status", "stage"}, nil)
	reportOldestDesc = prometheus.NewDesc("aida_report_run_oldest_stage_seconds", "Oldest active Report Run stage age.", []string{"stage"}, nil)
)

func (c *databaseCollector) Describe(target chan<- *prometheus.Desc) {
	target <- digestJobsDesc
	target <- digestOldestDesc
	target <- reportRunsDesc
	target <- reportOldestDesc
}

func (c *databaseCollector) Collect(target chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.collectDigestJobs(ctx, target); err != nil {
		log.Printf("collect Digest job metrics failed: %v", err)
	}
	if err := c.collectReportRuns(ctx, target); err != nil {
		log.Printf("collect Report Run metrics failed: %v", err)
	}
}

func (c *databaseCollector) collectDigestJobs(ctx context.Context, target chan<- prometheus.Metric) error {
	counts := map[string]float64{}
	rows, err := c.database.QueryContext(ctx, `
		SELECT urgency, status, COUNT(*)
		FROM session_processing_jobs
		WHERE job_type = 'build_content_slice_digest_v2'
		  AND status IN ('pending', 'retry_wait', 'leased')
		GROUP BY urgency, status`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var urgency, status string
		var count float64
		if err := rows.Scan(&urgency, &status, &count); err != nil {
			rows.Close()
			return err
		}
		counts[urgency+"\x00"+status] = count
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, urgency := range []string{"interactive", "background"} {
		for _, status := range []string{"pending", "retry_wait", "leased"} {
			target <- prometheus.MustNewConstMetric(digestJobsDesc, prometheus.GaugeValue, counts[urgency+"\x00"+status], urgency, status)
		}
	}
	oldest := map[string]float64{}
	rows, err = c.database.QueryContext(ctx, `
		SELECT urgency, COALESCE(EXTRACT(EPOCH FROM now() - MIN(GREATEST(
			created_at,
			COALESCE(urgency_raised_at, created_at),
			CASE WHEN status = 'retry_wait' THEN COALESCE(next_retry_at, created_at)
			     WHEN status = 'leased' THEN COALESCE(lease_until, created_at)
			     ELSE created_at END
		))), 0)
		FROM session_processing_jobs
		WHERE job_type = 'build_content_slice_digest_v2' AND attempts < max_attempts
		  AND (status = 'pending'
		    OR (status = 'retry_wait' AND COALESCE(next_retry_at, created_at) <= now())
		    OR (status = 'leased' AND lease_until <= now()))
		GROUP BY urgency`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var urgency string
		var seconds float64
		if err := rows.Scan(&urgency, &seconds); err != nil {
			rows.Close()
			return err
		}
		oldest[urgency] = seconds
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, urgency := range []string{"interactive", "background"} {
		target <- prometheus.MustNewConstMetric(digestOldestDesc, prometheus.GaugeValue, oldest[urgency], urgency)
	}
	return nil
}

func (c *databaseCollector) collectReportRuns(ctx context.Context, target chan<- prometheus.Metric) error {
	counts := map[string]float64{}
	oldest := map[string]float64{}
	rows, err := c.database.QueryContext(ctx, `
		SELECT status, execution_stage, COUNT(*),
			COALESCE(EXTRACT(EPOCH FROM now() - MIN(stage_updated_at)), 0)
		FROM ai_runs
		WHERE business_type = 'report_agent_run' AND status IN ('pending', 'running')
		  AND execution_stage IS NOT NULL AND execution_stage <> 'completed'
		GROUP BY status, execution_stage`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var status, stage string
		var count, seconds float64
		if err := rows.Scan(&status, &stage, &count, &seconds); err != nil {
			return err
		}
		counts[status+"\x00"+stage] = count
		if seconds > oldest[stage] {
			oldest[stage] = seconds
		}
	}
	stages := []string{"waiting_digest", "building_context", "submitting_agent", "agent_running", "writing_result"}
	for _, status := range []string{"pending", "running"} {
		for _, stage := range stages {
			target <- prometheus.MustNewConstMetric(reportRunsDesc, prometheus.GaugeValue, counts[status+"\x00"+stage], status, stage)
		}
	}
	for _, stage := range stages {
		target <- prometheus.MustNewConstMetric(reportOldestDesc, prometheus.GaugeValue, oldest[stage], stage)
	}
	return rows.Err()
}
