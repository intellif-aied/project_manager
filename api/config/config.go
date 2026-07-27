package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	DefaultWorkerCount = 20
	MaxWorkerCount     = 256
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
	ManagedAgentReportModelID      string
	ManagedAgentReportSkillOwner   string
	ManagedAgentReportSkillVersion string
	ManagedAgentReportMCPURL       string
	AIDAPublicBaseURL              string
	AIDAInternalMetricsAddr        string
	EnablePublicRegister           bool
	ClaudeCacheWriteVariant        string
	ReportRunProcessorCount        int
	DigestBackgroundWorkerCount    int
	DigestInteractiveWorkerCount   int

	MinioEndpoint         string
	MinioAccessKey        string
	MinioSecretKey        string
	MinioBucket           string
	MinioUseSSL           bool
	MinioExternalEndpoint string
}

type WorkerCounts struct {
	ReportRun         int
	DigestBackground  int
	DigestInteractive int
}

func Load() *Config {
	cfg := &Config{
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
		ManagedAgentReportModelID:      getEnv("MANAGED_AGENT_REPORT_MODEL_ID", "deepseek-v4-flash"),
		ManagedAgentReportSkillOwner:   getEnv("MANAGED_AGENT_REPORT_SKILL_OWNER", ""),
		ManagedAgentReportSkillVersion: getEnv("MANAGED_AGENT_REPORT_SKILL_VERSION", ""),
		ManagedAgentReportMCPURL:       getEnv("MANAGED_AGENT_REPORT_MCP_URL", ""),
		AIDAPublicBaseURL:              getEnv("AIDA_PUBLIC_BASE_URL", ""),
		AIDAInternalMetricsAddr:        getEnv("AIDA_INTERNAL_METRICS_ADDR", ":9091"),
		EnablePublicRegister:           getEnv("ENABLE_PUBLIC_REGISTER", "false") == "true",
		ClaudeCacheWriteVariant:        strings.TrimSpace(strings.ToLower(getEnv("AIDA_CLAUDE_CACHE_WRITE_VARIANT", ""))),

		MinioEndpoint:         getEnv("MINIO_ENDPOINT", ""),
		MinioAccessKey:        getEnv("MINIO_ACCESS_KEY", ""),
		MinioSecretKey:        getEnv("MINIO_SECRET_KEY", ""),
		MinioBucket:           getEnv("MINIO_BUCKET", "aidashboard"),
		MinioUseSSL:           getEnv("MINIO_USE_SSL", "false") == "true",
		MinioExternalEndpoint: getEnv("MINIO_EXTERNAL_ENDPOINT", ""),
	}
	return cfg
}

func LoadWorkerCounts() (WorkerCounts, error) {
	var counts WorkerCounts
	var err error
	if counts.ReportRun, err = getWorkerCount("REPORT_RUN_PROCESSOR_COUNT"); err != nil {
		return WorkerCounts{}, err
	}
	if counts.DigestBackground, err = getWorkerCount("DIGEST_BACKGROUND_WORKER_COUNT"); err != nil {
		return WorkerCounts{}, err
	}
	if counts.DigestInteractive, err = getWorkerCount("DIGEST_INTERACTIVE_WORKER_COUNT"); err != nil {
		return WorkerCounts{}, err
	}
	return counts, nil
}

func (c *Config) ValidateManagedReportResources() error {
	if c == nil {
		return fmt.Errorf("managed report resource configuration is required")
	}
	c.ManagedAgentReportSkillVersion = strings.TrimSpace(c.ManagedAgentReportSkillVersion)
	if c.ManagedAgentReportSkillVersion == "" {
		return fmt.Errorf("MANAGED_AGENT_REPORT_SKILL_VERSION is required; publish and configure an immutable environment Skill version")
	}
	return nil
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

func getWorkerCount(key string) (int, error) {
	raw, present := os.LookupEnv(key)
	if !present {
		return DefaultWorkerCount, nil
	}
	raw = strings.TrimSpace(raw)
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > MaxWorkerCount {
		return 0, fmt.Errorf("%s must be an integer between 1 and %d", key, MaxWorkerCount)
	}
	return value, nil
}
