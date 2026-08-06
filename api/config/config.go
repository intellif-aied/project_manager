package config

import (
	"fmt"
	"net/mail"
	"os"
	"strconv"
	"strings"
	"time"
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
	ProjectMemoryNightlyEnabled    bool
	ProjectMemoryAgentID           string
	ProjectMemoryModelID           string
	ProjectMemorySkillOwner        string
	ProjectMemorySkillVersion      string
	ProjectMemoryMCPURL            string
	ProjectMemoryStartHour         int
	ProjectMemoryEndHour           int
	ReportTwoPassEnabled           bool
	AIDAPublicBaseURL              string
	AIDAInternalMetricsAddr        string
	EnablePublicRegister           bool
	ClaudeCacheWriteVariant        string
	ReportRunProcessorCount        int
	DigestBackgroundWorkerCount    int
	DigestInteractiveWorkerCount   int
	Environment                    string
	EvaluationEnabled              bool
	EvaluationInstanceID           string
	ReportEmailEnabled             bool
	ReportEmailTimezone            string
	ReportEmailTimeOfDay           string
	ReportEmailSMTPHost            string
	ReportEmailSMTPPort            int
	ReportEmailSMTPUsername        string
	ReportEmailSMTPPassword        string
	ReportEmailSMTPFrom            string
	ReportEmailSMTPFromName        string
	ReportEmailSMTPTLSMode         string

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
		ProjectMemoryNightlyEnabled:    getEnv("PROJECT_MEMORY_NIGHTLY_ENABLED", "false") == "true",
		ProjectMemoryAgentID:           strings.TrimSpace(getEnv("PROJECT_MEMORY_AGENT_ID", "")),
		ProjectMemoryModelID:           strings.TrimSpace(getEnv("PROJECT_MEMORY_MODEL_ID", "deepseek-v4-flash")),
		ProjectMemorySkillOwner:        strings.TrimSpace(getEnv("PROJECT_MEMORY_SKILL_OWNER", "")),
		ProjectMemorySkillVersion:      strings.TrimSpace(getEnv("PROJECT_MEMORY_SKILL_VERSION", "")),
		ProjectMemoryMCPURL:            strings.TrimSpace(getEnv("PROJECT_MEMORY_MCP_URL", "")),
		ProjectMemoryStartHour:         getEnvInt("PROJECT_MEMORY_START_HOUR", 2),
		ProjectMemoryEndHour:           getEnvInt("PROJECT_MEMORY_END_HOUR", 6),
		ReportTwoPassEnabled:           getEnv("REPORT_TWO_PASS_ENABLED", "false") == "true",
		AIDAPublicBaseURL:              getEnv("AIDA_PUBLIC_BASE_URL", ""),
		AIDAInternalMetricsAddr:        getEnv("AIDA_INTERNAL_METRICS_ADDR", ":9091"),
		EnablePublicRegister:           getEnv("ENABLE_PUBLIC_REGISTER", "false") == "true",
		ClaudeCacheWriteVariant:        strings.TrimSpace(strings.ToLower(getEnv("AIDA_CLAUDE_CACHE_WRITE_VARIANT", ""))),
		Environment:                    strings.TrimSpace(strings.ToLower(getEnv("AIDA_ENVIRONMENT", "development"))),
		EvaluationEnabled:              getEnv("AIDA_EVALUATION_ENABLED", "false") == "true",
		EvaluationInstanceID:           strings.TrimSpace(getEnv("AIDA_EVALUATION_INSTANCE_ID", "")),
		ReportEmailEnabled:             getEnv("REPORT_EMAIL_ENABLED", "false") == "true",
		ReportEmailTimezone:            strings.TrimSpace(getEnv("REPORT_EMAIL_TIMEZONE", "Asia/Shanghai")),
		ReportEmailTimeOfDay:           strings.TrimSpace(getEnv("REPORT_EMAIL_TIME_OF_DAY", "08:00")),
		ReportEmailSMTPHost:            strings.TrimSpace(getEnv("REPORT_EMAIL_SMTP_HOST", "")),
		ReportEmailSMTPPort:            getEnvInt("REPORT_EMAIL_SMTP_PORT", 587),
		ReportEmailSMTPUsername:        strings.TrimSpace(getEnv("REPORT_EMAIL_SMTP_USERNAME", "")),
		ReportEmailSMTPPassword:        getEnv("REPORT_EMAIL_SMTP_PASSWORD", ""),
		ReportEmailSMTPFrom:            strings.TrimSpace(getEnv("REPORT_EMAIL_SMTP_FROM", "")),
		ReportEmailSMTPFromName:        strings.TrimSpace(getEnv("REPORT_EMAIL_SMTP_FROM_NAME", "Aida 日报")),
		ReportEmailSMTPTLSMode:         strings.TrimSpace(strings.ToLower(getEnv("REPORT_EMAIL_SMTP_TLS_MODE", "starttls"))),

		MinioEndpoint:         getEnv("MINIO_ENDPOINT", ""),
		MinioAccessKey:        getEnv("MINIO_ACCESS_KEY", ""),
		MinioSecretKey:        getEnv("MINIO_SECRET_KEY", ""),
		MinioBucket:           getEnv("MINIO_BUCKET", "aidashboard"),
		MinioUseSSL:           getEnv("MINIO_USE_SSL", "false") == "true",
		MinioExternalEndpoint: getEnv("MINIO_EXTERNAL_ENDPOINT", ""),
	}
	return cfg
}

func (c *Config) ValidateReportEmail() error {
	if c == nil || !c.ReportEmailEnabled {
		return nil
	}
	missing := make([]string, 0, 4)
	for key, value := range map[string]string{
		"REPORT_EMAIL_SMTP_HOST":     c.ReportEmailSMTPHost,
		"REPORT_EMAIL_SMTP_FROM":     c.ReportEmailSMTPFrom,
		"REPORT_EMAIL_SMTP_USERNAME": c.ReportEmailSMTPUsername,
		"REPORT_EMAIL_SMTP_PASSWORD": c.ReportEmailSMTPPassword,
	} {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("report email configuration is incomplete: %s", strings.Join(missing, ", "))
	}
	if c.ReportEmailSMTPPort < 1 || c.ReportEmailSMTPPort > 65535 {
		return fmt.Errorf("REPORT_EMAIL_SMTP_PORT must be between 1 and 65535")
	}
	if c.ReportEmailTimezone == "" || c.ReportEmailTimeOfDay == "" {
		return fmt.Errorf("report email timezone and time of day are required")
	}
	if _, err := time.LoadLocation(c.ReportEmailTimezone); err != nil {
		return fmt.Errorf("REPORT_EMAIL_TIMEZONE is invalid: %w", err)
	}
	if _, err := time.Parse("15:04", c.ReportEmailTimeOfDay); err != nil {
		return fmt.Errorf("REPORT_EMAIL_TIME_OF_DAY must use HH:MM: %w", err)
	}
	if parsed, err := mail.ParseAddress(c.ReportEmailSMTPFrom); err != nil || parsed.Address != c.ReportEmailSMTPFrom {
		return fmt.Errorf("REPORT_EMAIL_SMTP_FROM must be a plain email address")
	}
	switch c.ReportEmailSMTPTLSMode {
	case "starttls", "implicit":
	default:
		return fmt.Errorf("REPORT_EMAIL_SMTP_TLS_MODE must be starttls or implicit")
	}
	return nil
}

func (c *Config) ValidateProjectMemoryResources() error {
	if c == nil || !c.ProjectMemoryNightlyEnabled {
		return nil
	}
	missing := make([]string, 0, 4)
	for key, value := range map[string]string{
		"PROJECT_MEMORY_AGENT_ID":      c.ProjectMemoryAgentID,
		"PROJECT_MEMORY_SKILL_OWNER":   c.ProjectMemorySkillOwner,
		"PROJECT_MEMORY_SKILL_VERSION": c.ProjectMemorySkillVersion,
		"PROJECT_MEMORY_MCP_URL":       c.ProjectMemoryMCPURL,
	} {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("project memory system resources are incomplete: %s", strings.Join(missing, ", "))
	}
	return nil
}

func (c *Config) ValidateEvaluationRuntime() error {
	if c == nil {
		return fmt.Errorf("evaluation runtime configuration is required")
	}
	if !c.EvaluationEnabled {
		return nil
	}
	if c.Environment != "test" {
		return fmt.Errorf("AIDA_EVALUATION_ENABLED requires AIDA_ENVIRONMENT=test")
	}
	if c.EvaluationInstanceID == "" {
		return fmt.Errorf("AIDA_EVALUATION_INSTANCE_ID is required when evaluation is enabled")
	}
	return nil
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

func getEnvInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
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
