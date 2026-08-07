package config

import (
	"os"
	"testing"
)

func TestGetWorkerCount(t *testing.T) {
	tests := []struct {
		name string
		set  bool
		raw  string
		want int
		ok   bool
	}{
		{name: "absent uses default", want: DefaultWorkerCount, ok: true},
		{name: "explicit twenty", set: true, raw: "20", want: 20, ok: true},
		{name: "empty is invalid", set: true, raw: "", ok: false},
		{name: "zero is invalid", set: true, raw: "0", ok: false},
		{name: "too large is invalid", set: true, raw: "257", ok: false},
		{name: "text is invalid", set: true, raw: "many", ok: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			const key = "TEST_WORKER_COUNT"
			if test.set {
				t.Setenv(key, test.raw)
			} else {
				t.Setenv(key, "temporary")
				t.Setenv(key, "")
				// Setenv cannot represent absence.
				if err := os.Unsetenv(key); err != nil {
					t.Fatal(err)
				}
			}
			got, err := getWorkerCount(key)
			if (err == nil) != test.ok {
				t.Fatalf("error = %v, want success %v", err, test.ok)
			}
			if test.ok && got != test.want {
				t.Fatalf("count = %d, want %d", got, test.want)
			}
		})
	}
}

func TestLoadWorkerCountsUsesIndependentSettings(t *testing.T) {
	t.Setenv("REPORT_RUN_PROCESSOR_COUNT", "21")
	t.Setenv("DIGEST_BACKGROUND_WORKER_COUNT", "22")
	t.Setenv("DIGEST_INTERACTIVE_WORKER_COUNT", "23")
	counts, err := LoadWorkerCounts()
	if err != nil {
		t.Fatal(err)
	}
	if counts.ReportRun != 21 || counts.DigestBackground != 22 || counts.DigestInteractive != 23 {
		t.Fatalf("counts = %#v", counts)
	}
}

func TestLoadDoesNotFallbackReportSkillVersion(t *testing.T) {
	if err := os.Unsetenv("MANAGED_AGENT_REPORT_SKILL_VERSION"); err != nil {
		t.Fatal(err)
	}
	if got := Load().ManagedAgentReportSkillVersion; got != "" {
		t.Fatalf("version = %q, want empty so startup validation can fail closed", got)
	}
}

func TestValidateManagedReportResourcesRequiresSkillVersion(t *testing.T) {
	for _, test := range []struct {
		name    string
		version string
		wantErr bool
	}{
		{name: "missing", wantErr: true},
		{name: "whitespace", version: "  ", wantErr: true},
		{name: "configured", version: "1.0.45"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := &Config{ManagedAgentReportSkillVersion: test.version}
			err := cfg.ValidateManagedReportResources()
			if (err != nil) != test.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestLoadProjectMemoryConfiguration(t *testing.T) {
	t.Setenv("PROJECT_MEMORY_NIGHTLY_ENABLED", "true")
	t.Setenv("PROJECT_MEMORY_AGENT_ID", "memory-resolver-test")
	t.Setenv("PROJECT_MEMORY_MODEL_ID", "deepseek-v4-flash")
	t.Setenv("PROJECT_MEMORY_SKILL_OWNER", "100866")
	t.Setenv("PROJECT_MEMORY_SKILL_VERSION", "project-memory-v1")
	t.Setenv("PROJECT_MEMORY_MCP_URL", "https://test.example.com/api/v1/mcp/project-memory")
	t.Setenv("PROJECT_MEMORY_START_HOUR", "0")
	t.Setenv("PROJECT_MEMORY_END_HOUR", "24")
	config := Load()
	if !config.ProjectMemoryNightlyEnabled || config.ProjectMemoryAgentID != "memory-resolver-test" ||
		config.ProjectMemoryModelID != "deepseek-v4-flash" || config.ProjectMemorySkillOwner != "100866" ||
		config.ProjectMemoryStartHour != 0 || config.ProjectMemoryEndHour != 24 {
		t.Fatalf("project memory config = %#v", config)
	}
	if err := config.ValidateProjectMemoryResources(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadReportReviewConfiguration(t *testing.T) {
	t.Setenv("REPORT_REVIEW_ENABLED", "true")
	t.Setenv("REPORT_REVIEW_AGENT_ID", "report-review-test")
	t.Setenv("REPORT_REVIEW_MODEL_ID", "MiniMax-M2.5")
	t.Setenv("REPORT_REVIEW_SKILL_OWNER", "100866")
	t.Setenv("REPORT_REVIEW_SKILL_VERSION", "report-review-v8")
	t.Setenv("REPORT_REVIEW_MCP_URL", "http://api:8080/api/v1/mcp/report-review")

	config := Load()
	if !config.ReportReviewEnabled || config.ReportReviewAgentID != "report-review-test" ||
		config.ReportReviewModelID != "MiniMax-M2.5" || config.ReportReviewSkillOwner != "100866" {
		t.Fatalf("report review config = %#v", config)
	}
	if err := config.ValidateReportReviewResources(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateReportEmail(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{name: "disabled accepts empty config", config: Config{}},
		{name: "enabled requires address and password", config: Config{ReportEmailEnabled: true, ReportEmailAddress: "sender@example.com", ReportEmailTimeOfDay: "08:00"}, wantErr: true},
		{name: "invalid time", config: Config{ReportEmailEnabled: true, ReportEmailAddress: "sender@example.com", ReportEmailPassword: "secret", ReportEmailTimeOfDay: "8am"}, wantErr: true},
		{name: "invalid address", config: Config{ReportEmailEnabled: true, ReportEmailAddress: "not-an-email", ReportEmailPassword: "secret", ReportEmailTimeOfDay: "08:00"}, wantErr: true},
		{name: "valid Tencent enterprise email", config: Config{ReportEmailEnabled: true, ReportEmailAddress: "sender@example.com", ReportEmailPassword: "secret", ReportEmailTimeOfDay: "08:00"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.config.ValidateReportEmail(); (err != nil) != test.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestLoadReportEmailUsesThreeSettings(t *testing.T) {
	t.Setenv("REPORT_EMAIL_ADDRESS", "sender@example.com")
	t.Setenv("REPORT_EMAIL_PASSWORD", "secret")
	t.Setenv("REPORT_EMAIL_TIME_OF_DAY", "09:30")
	config := Load()
	if !config.ReportEmailEnabled || config.ReportEmailAddress != "sender@example.com" ||
		config.ReportEmailPassword != "secret" || config.ReportEmailTimeOfDay != "09:30" {
		t.Fatalf("report email config = %#v", config)
	}
}

func TestLoadReportEmailCanBePausedWithoutClearingCredentials(t *testing.T) {
	t.Setenv("REPORT_EMAIL_ENABLED", "false")
	t.Setenv("REPORT_EMAIL_ADDRESS", "sender@example.com")
	t.Setenv("REPORT_EMAIL_PASSWORD", "secret")
	config := Load()
	if config.ReportEmailEnabled {
		t.Fatal("report email must remain disabled while REPORT_EMAIL_ENABLED=false")
	}
	if config.ReportEmailAddress != "sender@example.com" || config.ReportEmailPassword != "secret" {
		t.Fatal("pausing report email must preserve credentials")
	}
}
