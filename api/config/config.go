package config

import (
	"os"
	"strings"
)

type Config struct {
	DatabaseURL                         string
	JWTSecret                           string
	AIHubHost                           string
	AIHubSecret                         string
	AIHubToken                          string
	BootstrapAdminUIDs                  string
	AIAPIURL                            string
	AIAPIKey                            string
	AIModel                             string
	CORSOrigin                          string
	Port                                string
	ReportGeneratorURL                  string
	ManagedAgentURL                     string
	ManagedAgentToken                   string
	ManagedAgentDefaultEngine           string
	ManagedAgentDefaultModelID          string
	ManagedAgentReportSkillSlug         string
	ManagedAgentReportSkillVersion      string
	ManagedAgentReportSkillName         string
	ManagedAgentReportSkillDescription  string
	ManagedAgentReportSkillMarkdown     string
	ManagedAgentReportMCPSlug           string
	ManagedAgentReportMCPVersion        string
	ManagedAgentReportMCPName           string
	ManagedAgentReportMCPDescription    string
	ManagedAgentReportMCPURL            string
	ManagedAgentReportCredentialSlot    string
	ManagedAgentReportAgentName         string
	ManagedAgentReportAgentDescription  string
	ManagedAgentReportAgentInstructions string
	ManagedAgentReportAgentStartPrompt  string
	ManagedAgentReportAssetRepair       bool
	AIDAPublicBaseURL                   string
	EnablePublicRegister                bool

	MinioEndpoint         string
	MinioAccessKey        string
	MinioSecretKey        string
	MinioBucket           string
	MinioUseSSL           bool
	MinioExternalEndpoint string
}

func Load() *Config {
	return &Config{
		DatabaseURL:                         getEnv("DATABASE_URL", "postgres://aidashboard:devpassword@localhost:5432/aidashboard?sslmode=disable"),
		JWTSecret:                           getEnv("JWT_SECRET", "dev-jwt-secret"),
		AIHubHost:                           getEnv("AIHUB_HOST", ""),
		AIHubSecret:                         getEnv("AIHUB_SECRET", getEnv("JWT_SECRET", "dev-jwt-secret")),
		AIHubToken:                          getEnv("AIHUB_SERVICE_TOKEN", getEnv("AIHUB_TOKEN", "")),
		BootstrapAdminUIDs:                  getEnv("AIDA_BOOTSTRAP_ADMIN_UIDS", ""),
		AIAPIURL:                            getEnv("AI_API_URL", ""),
		AIAPIKey:                            getEnv("AI_API_KEY", ""),
		AIModel:                             getEnv("AI_MODEL", ""),
		CORSOrigin:                          getEnv("CORS_ORIGIN", "http://localhost:3000"),
		Port:                                getEnv("PORT", "8080"),
		ReportGeneratorURL:                  getEnv("REPORT_GENERATOR_URL", ""),
		ManagedAgentURL:                     getEnv("MANAGED_AGENT_URL", ""),
		ManagedAgentToken:                   getEnv("MANAGED_AGENT_TOKEN", ""),
		ManagedAgentDefaultEngine:           getEnv("MANAGED_AGENT_DEFAULT_ENGINE", "claude-code"),
		ManagedAgentDefaultModelID:          getEnv("MANAGED_AGENT_DEFAULT_MODEL_ID", "MiniMax-M2.5"),
		ManagedAgentReportSkillSlug:         getEnv("MANAGED_AGENT_REPORT_SKILL_SLUG", "aida-report"),
		ManagedAgentReportSkillVersion:      getEnv("MANAGED_AGENT_REPORT_SKILL_VERSION", "1.0.2"),
		ManagedAgentReportSkillName:         getEnv("MANAGED_AGENT_REPORT_SKILL_NAME", "Aida Report Skill"),
		ManagedAgentReportSkillDescription:  getEnv("MANAGED_AGENT_REPORT_SKILL_DESCRIPTION", ""),
		ManagedAgentReportSkillMarkdown:     readOptionalFile(getEnv("MANAGED_AGENT_REPORT_SKILL_MD_FILE", "")),
		ManagedAgentReportMCPSlug:           getEnv("MANAGED_AGENT_REPORT_MCP_SLUG", "aida-report-mcp"),
		ManagedAgentReportMCPVersion:        getEnv("MANAGED_AGENT_REPORT_MCP_VERSION", "report-v1"),
		ManagedAgentReportMCPName:           getEnv("MANAGED_AGENT_REPORT_MCP_NAME", "Aida Report MCP"),
		ManagedAgentReportMCPDescription:    getEnv("MANAGED_AGENT_REPORT_MCP_DESCRIPTION", ""),
		ManagedAgentReportMCPURL:            getEnv("MANAGED_AGENT_REPORT_MCP_URL", ""),
		ManagedAgentReportCredentialSlot:    getEnv("MANAGED_AGENT_REPORT_CREDENTIAL_SLOT", "AIDA_REPORT_MCP_AUTH"),
		ManagedAgentReportAgentName:         getEnv("MANAGED_AGENT_REPORT_AGENT_NAME", "报告生成 Agent"),
		ManagedAgentReportAgentDescription:  getEnv("MANAGED_AGENT_REPORT_AGENT_DESCRIPTION", "默认报告生成 Agent。"),
		ManagedAgentReportAgentInstructions: readOptionalFile(getEnv("MANAGED_AGENT_REPORT_AGENT_INSTRUCTIONS_FILE", "")),
		ManagedAgentReportAgentStartPrompt:  readOptionalFile(getEnv("MANAGED_AGENT_REPORT_AGENT_START_PROMPT_FILE", "")),
		ManagedAgentReportAssetRepair:       getEnvBool("MANAGED_AGENT_REPORT_ASSET_REPAIR", true),
		AIDAPublicBaseURL:                   getEnv("AIDA_PUBLIC_BASE_URL", ""),
		EnablePublicRegister:                getEnv("ENABLE_PUBLIC_REGISTER", "false") == "true",

		MinioEndpoint:         getEnv("MINIO_ENDPOINT", ""),
		MinioAccessKey:        getEnv("MINIO_ACCESS_KEY", ""),
		MinioSecretKey:        getEnv("MINIO_SECRET_KEY", ""),
		MinioBucket:           getEnv("MINIO_BUCKET", "aidashboard"),
		MinioUseSSL:           getEnv("MINIO_USE_SSL", "false") == "true",
		MinioExternalEndpoint: getEnv("MINIO_EXTERNAL_ENDPOINT", ""),
	}
}

func (c *Config) MinioConfigured() bool {
	return c.MinioEndpoint != "" && c.MinioAccessKey != "" && c.MinioSecretKey != ""
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if value == "" {
		return fallback
	}
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func readOptionalFile(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(content)
}
