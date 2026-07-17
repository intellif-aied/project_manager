package config

import (
	"os"
	"strings"
)

type Config struct {
	DatabaseURL                    string
	JWTSecret                      string
	AIHubHost                      string
	AIHubSecret                    string
	AIHubToken                     string
	AIGatewayModelsURL             string
	BootstrapAdminUIDs             string
	AIAPIURL                       string
	AIAPIKey                       string
	AIModel                        string
	CORSOrigin                     string
	Port                           string
	ManagedAgentURL                string
	ManagedAgentToken              string
	ManagedAgentDefaultEngine      string
	ManagedAgentDefaultModelID     string
	ManagedAgentReportSkillOwner   string
	ManagedAgentReportSkillVersion string
	ManagedAgentReportMCPURL       string
	AIDAPublicBaseURL              string
	EnablePublicRegister           bool
	ClaudeCacheWriteVariant        string

	MinioEndpoint         string
	MinioAccessKey        string
	MinioSecretKey        string
	MinioBucket           string
	MinioUseSSL           bool
	MinioExternalEndpoint string
}

func Load() *Config {
	return &Config{
		DatabaseURL:                    getEnv("DATABASE_URL", "postgres://aidashboard:devpassword@localhost:5432/aidashboard?sslmode=disable"),
		JWTSecret:                      getEnv("JWT_SECRET", "dev-jwt-secret"),
		AIHubHost:                      getEnv("AIHUB_HOST", ""),
		AIHubSecret:                    getEnv("AIHUB_SECRET", getEnv("JWT_SECRET", "dev-jwt-secret")),
		AIHubToken:                     getEnv("AIHUB_SERVICE_TOKEN", getEnv("AIHUB_TOKEN", "")),
		AIGatewayModelsURL:             getEnv("AI_GATEWAY_MODELS_URL", "http://192.168.11.18:30054/api/v2/models"),
		BootstrapAdminUIDs:             getEnv("AIDA_BOOTSTRAP_ADMIN_UIDS", ""),
		AIAPIURL:                       getEnv("AI_API_URL", ""),
		AIAPIKey:                       getEnv("AI_API_KEY", ""),
		AIModel:                        getEnv("AI_MODEL", ""),
		CORSOrigin:                     getEnv("CORS_ORIGIN", "http://localhost:3000"),
		Port:                           getEnv("PORT", "8080"),
		ManagedAgentURL:                getEnv("MANAGED_AGENT_URL", ""),
		ManagedAgentToken:              getEnv("MANAGED_AGENT_TOKEN", ""),
		ManagedAgentDefaultEngine:      getEnv("MANAGED_AGENT_DEFAULT_ENGINE", "claude-code"),
		ManagedAgentDefaultModelID:     getEnv("MANAGED_AGENT_DEFAULT_MODEL_ID", "MiniMax-M2.5"),
		ManagedAgentReportSkillOwner:   getEnv("MANAGED_AGENT_REPORT_SKILL_OWNER", ""),
		ManagedAgentReportSkillVersion: getEnv("MANAGED_AGENT_REPORT_SKILL_VERSION", "1.0.0"),
		ManagedAgentReportMCPURL:       getEnv("MANAGED_AGENT_REPORT_MCP_URL", ""),
		AIDAPublicBaseURL:              getEnv("AIDA_PUBLIC_BASE_URL", ""),
		EnablePublicRegister:           getEnv("ENABLE_PUBLIC_REGISTER", "false") == "true",
		ClaudeCacheWriteVariant:        strings.TrimSpace(strings.ToLower(getEnv("AIDA_CLAUDE_CACHE_WRITE_VARIANT", ""))),

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
