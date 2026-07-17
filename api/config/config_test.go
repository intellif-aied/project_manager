package config

import "testing"

func TestCacheVariantDefaultsToUnspecified(t *testing.T) {
	t.Setenv("AIDA_CLAUDE_CACHE_WRITE_VARIANT", "")
	loaded := Load()
	if loaded.ClaudeCacheWriteVariant != "" {
		t.Fatalf("cache variant=%q", loaded.ClaudeCacheWriteVariant)
	}
}

func TestReportEnvironmentBindings(t *testing.T) {
	t.Setenv("MANAGED_AGENT_DEFAULT_ENGINE", "codex")
	t.Setenv("MANAGED_AGENT_DEFAULT_MODEL_ID", "model-1")
	t.Setenv("MANAGED_AGENT_REPORT_SKILL_OWNER", "10086")
	t.Setenv("MANAGED_AGENT_REPORT_SKILL_VERSION", "1.2.3")
	t.Setenv("MANAGED_AGENT_REPORT_MCP_URL", "https://aida.example.com/api/v1/mcp/reports")
	t.Setenv("AIDA_PUBLIC_BASE_URL", "https://aida.example.com")
	loaded := Load()
	if loaded.ManagedAgentDefaultEngine != "codex" ||
		loaded.ManagedAgentDefaultModelID != "model-1" ||
		loaded.ManagedAgentReportSkillOwner != "10086" ||
		loaded.ManagedAgentReportSkillVersion != "1.2.3" ||
		loaded.ManagedAgentReportMCPURL != "https://aida.example.com/api/v1/mcp/reports" ||
		loaded.AIDAPublicBaseURL != "https://aida.example.com" {
		t.Fatalf("unexpected report environment bindings: %+v", loaded)
	}
}
