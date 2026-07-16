package config

import "testing"

func TestCacheVariantDefaultsToUnspecified(t *testing.T) {
	t.Setenv("AIDA_CLAUDE_CACHE_WRITE_VARIANT", "")
	loaded := Load()
	if loaded.ClaudeCacheWriteVariant != "" {
		t.Fatalf("cache variant=%q", loaded.ClaudeCacheWriteVariant)
	}
}

func TestReportDigestConfigurationDefaults(t *testing.T) {
	for _, key := range []string{
		"MANAGED_AGENT_REPORT_SESSION_READ_MODE",
		"MANAGED_AGENT_REPORT_DIGEST_VERSION",
		"MANAGED_AGENT_REPORT_REDACTION_VERSION",
		"MANAGED_AGENT_REPORT_DIGEST_TARGET_BYTES",
		"MANAGED_AGENT_REPORT_DIGEST_HARD_LIMIT_BYTES",
		"MANAGED_AGENT_REPORT_DIGEST_ROLLOUT_PERCENT",
		"MANAGED_AGENT_REPORT_DIGEST_CANARY_USER_IDS",
	} {
		t.Setenv(key, "")
	}
	loaded := Load()
	if loaded.ManagedAgentReportSessionReadMode != "full" ||
		loaded.ManagedAgentReportDigestVersion != "session-digest/v1" ||
		loaded.ManagedAgentReportRedactionVersion != "report-redaction/v1" ||
		loaded.ManagedAgentReportDigestTargetBytes != 65536 ||
		loaded.ManagedAgentReportDigestHardLimit != 131072 ||
		loaded.ManagedAgentReportDigestRolloutPct != 100 ||
		len(loaded.ManagedAgentReportDigestCanaryUsers) != 0 {
		t.Fatalf("unexpected report digest defaults: %+v", loaded)
	}
}

func TestInvalidReportDigestIntegerIsNotSilentlyDefaulted(t *testing.T) {
	t.Setenv("MANAGED_AGENT_REPORT_DIGEST_TARGET_BYTES", "not-an-int")
	if got := Load().ManagedAgentReportDigestTargetBytes; got != -1 {
		t.Fatalf("invalid integer must reach startup validation, got %d", got)
	}
}
