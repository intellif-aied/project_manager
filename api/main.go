package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/aidashboard/api/config"
	"github.com/aidashboard/api/db"
	"github.com/aidashboard/api/handler"
	"github.com/aidashboard/api/internal/contentreader"
	"github.com/aidashboard/api/internal/pricing"
	"github.com/aidashboard/api/internal/reportcontext"
	"github.com/aidashboard/api/internal/reportsource"
	"github.com/aidashboard/api/internal/reportsourcecatalog"
	"github.com/aidashboard/api/internal/sessiondigest"
	"github.com/aidashboard/api/internal/sessiondigestv2"
	"github.com/aidashboard/api/internal/sessionsync"
	"github.com/aidashboard/api/internal/tokenanalytics"
	"github.com/aidashboard/api/internal/tokenrollup"
	"github.com/aidashboard/api/internal/usage"
	"github.com/aidashboard/api/service"
	"github.com/aidashboard/api/storage"
	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
)

func main() {
	cfg := config.Load()

	database, err := db.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer database.Close()

	log.Println("Running migrations...")
	if err := db.RunMigrations(database); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}
	log.Println("Migrations complete")

	var minioStore *storage.MinioStorage
	if cfg.MinioConfigured() {
		minioStore, err = storage.NewMinioStorage(cfg)
		if err != nil {
			log.Fatalf("Failed to init MinIO storage: %v", err)
		}
		log.Println("MinIO storage ready")
	} else {
		log.Println("MinIO not configured, raw log upload disabled")
	}
	sessionContentReader, err := contentreader.New(database, minioStore)
	if err != nil {
		log.Fatalf("Failed to init session content reader: %v", err)
	}

	aihubClient := service.NewAIHubClient(cfg.AIHubHost, cfg.AIHubToken)
	modelCatalogClient := service.NewModelCatalogClient(cfg.AIGatewayModelsURL)
	authH := handler.NewAuthHandler(database, aihubClient, cfg.BootstrapAdminUIDs)
	modelCatalogH := handler.NewModelCatalogHandler(modelCatalogClient)
	aiClient := service.NewAIClient()
	managedAgentClient := service.NewManagedAgentClient(cfg.ManagedAgentURL, cfg.ManagedAgentToken)
	workItemEventRecorder := service.NewWorkItemEventRecorder(database)
	reqH := handler.NewRequirementHandlerWithRecorder(database, aiClient, workItemEventRecorder)
	taskH := handler.NewTaskHandlerWithRecorder(database, workItemEventRecorder)
	sessionH := handler.NewSessionHandlerWithRecorder(database, minioStore, aiClient, workItemEventRecorder)
	sessionH.ConfigureContentReader(sessionContentReader)
	sessionSyncH, err := handler.NewSessionSyncHandler(database, minioStore)
	if err != nil {
		log.Fatalf("Failed to init session sync handler: %v", err)
	}
	reportH := handler.NewReportHandler(database)
	reportSourceConfig, err := reportsource.ProductConfig().Normalized()
	if err != nil {
		log.Fatalf("Invalid report source digest configuration: %v", err)
	}
	reportSourceService, err := reportsource.NewServiceWithConfigAndReader(
		database, sessionContentReader, reportSourceConfig,
	)
	if err != nil {
		log.Fatalf("Failed to init report source service: %v", err)
	}
	reportSourceH := handler.NewReportSourceHandler(reportSourceService)
	reportContextService := reportcontext.NewService(database, reportSourceService)
	managedAgentH := handler.NewManagedAgentHandlerWithDefaults(database, managedAgentClient, handler.ManagedAgentDefaults{
		Engine:             cfg.ManagedAgentDefaultEngine,
		ModelID:            cfg.ManagedAgentDefaultModelID,
		ReportSkillOwner:   cfg.ManagedAgentReportSkillOwner,
		ReportSkillVersion: cfg.ManagedAgentReportSkillVersion,
		ReportMCPURL:       cfg.ManagedAgentReportMCPURL,
		AIDAPublicBaseURL:  cfg.AIDAPublicBaseURL,
		AIHubSecret:        cfg.AIHubSecret,
	})
	managedAgentH.ConfigureReportSourceSelection(reportSourceService)
	managedAgentH.ConfigureReportContext(reportContextService)
	dailyReportMCPH := handler.NewReportMCPHandler(database)
	dailyReportMCPH.ConfigureReportSourceSelection(reportSourceService)
	dailyReportMCPH.ConfigureReportContext(reportContextService)
	schedulerCtx, stopScheduler := context.WithCancel(context.Background())
	defer stopScheduler()
	handler.NewManagedAgentScheduleRunner(managedAgentH).Start(schedulerCtx)
	service.NewManagedAgentRunStatusSyncer(database, managedAgentClient).Start(schedulerCtx)
	reportSourceCatalogReconciler, err := reportsourcecatalog.NewReconciler(database)
	if err != nil {
		log.Fatalf("Failed to init report source catalog reconciler: %v", err)
	}
	reportSourceCatalogReconciler.Start(schedulerCtx)
	log.Println("Report source catalog reconciler started")
	tokenRollupReconciler, err := tokenrollup.NewReconciler(database)
	if err != nil {
		log.Fatalf("Failed to init token rollup reconciler: %v", err)
	}
	tokenRollupReconciler.Start(schedulerCtx)
	log.Println("Token rollup reconciler started")
	// Session content and usage processing are core services. Leaving either worker
	// stopped accepts uploads that can never appear in reports or Token analytics.
	{
		if minioStore == nil {
			log.Fatal("Session content projection worker requires MinIO")
		}
		jobRepository, err := sessionsync.NewPostgresJobRepository(database)
		if err != nil {
			log.Fatalf("Failed to init session content job repository: %v", err)
		}
		contentProcessor, err := sessionsync.NewContentProjectionProcessor(database, minioStore)
		if err != nil {
			log.Fatalf("Failed to init session content processor: %v", err)
		}
		hostname, _ := os.Hostname()
		contentWorker, err := sessionsync.NewContentProjectionWorker(jobRepository, contentProcessor, "api:"+hostname)
		if err != nil {
			log.Fatalf("Failed to init session content worker: %v", err)
		}
		contentWorker.Start(schedulerCtx)
		log.Println("Session content projection worker started")
	}
	{
		if minioStore == nil {
			log.Fatal("Session usage projection worker requires MinIO")
		}
		jobRepository, err := sessionsync.NewPostgresJobRepository(database)
		if err != nil {
			log.Fatalf("Failed to init session usage job repository: %v", err)
		}
		usageProcessor, err := usage.NewProcessor(database, minioStore, cfg.ClaudeCacheWriteVariant)
		if err != nil {
			log.Fatalf("Failed to init session usage processor: %v", err)
		}
		hostname, _ := os.Hostname()
		usageWorker, err := usage.NewWorker(jobRepository, usageProcessor, "api:"+hostname+":usage")
		if err != nil {
			log.Fatalf("Failed to init session usage worker: %v", err)
		}
		usageWorker.Start(schedulerCtx)
		log.Println("Session usage projection worker started")
		meteringProcessor, err := usage.NewMeteringProcessor(database, minioStore)
		if err != nil {
			log.Fatalf("Failed to init session metering processor: %v", err)
		}
		meteringWorker, err := usage.NewMeteringWorker(jobRepository, meteringProcessor, "api:"+hostname+":metering")
		if err != nil {
			log.Fatalf("Failed to init session metering worker: %v", err)
		}
		meteringWorker.Start(schedulerCtx)
		log.Println("Session metering lifecycle worker started")
	}
	if reportSourceConfig.SessionReadMode == reportsource.ReadModeDigestV1 ||
		reportSourceConfig.SessionReadMode == reportsource.ReadModeDigestV2 ||
		reportSourceConfig.SessionReadMode == reportsource.ReadModeShadow {
		digestConfig := sessiondigest.DefaultConfig()
		reconciler, err := sessiondigest.NewReconciler(database, digestConfig)
		if err != nil {
			log.Fatalf("Failed to init session digest reconciler: %v", err)
		}
		jobRepository, err := sessionsync.NewPostgresJobRepository(database)
		if err != nil {
			log.Fatalf("Failed to init session digest job repository: %v", err)
		}
		digestProcessor, err := sessiondigest.NewProcessor(database, sessionContentReader, digestConfig)
		if err != nil {
			log.Fatalf("Failed to init session digest processor: %v", err)
		}
		hostname, _ := os.Hostname()
		digestWorker, err := sessiondigest.NewWorker(jobRepository, digestProcessor, "api:"+hostname+":digest")
		if err != nil {
			log.Fatalf("Failed to init session digest worker: %v", err)
		}
		reconciler.Start(schedulerCtx)
		digestWorker.Start(schedulerCtx)
		log.Printf("Session digest services started in %s mode", reportSourceConfig.SessionReadMode)
	}
	digestV2Config := sessiondigestv2.DefaultConfig()
	reconciler, err := sessiondigestv2.NewReconciler(database, digestV2Config)
	if err != nil {
		log.Fatalf("Failed to init session digest v2 reconciler: %v", err)
	}
	jobRepository, err := sessionsync.NewPostgresJobRepository(database)
	if err != nil {
		log.Fatalf("Failed to init session digest v2 job repository: %v", err)
	}
	digestProcessor, err := sessiondigestv2.NewProcessor(database, sessionContentReader, digestV2Config)
	if err != nil {
		log.Fatalf("Failed to init session digest v2 processor: %v", err)
	}
	hostname, _ := os.Hostname()
	digestWorker, err := sessiondigestv2.NewWorker(
		jobRepository, digestProcessor, "api:"+hostname+":digest-v2", digestV2Config,
	)
	if err != nil {
		log.Fatalf("Failed to init session digest v2 worker: %v", err)
	}
	reconciler.Start(schedulerCtx)
	digestWorker.Start(schedulerCtx)
	log.Printf(
		"Session digest v2 services started (reconcile_batch=%d worker_batch=%d read_mode=%s)",
		digestV2Config.ReconcileBatch, digestV2Config.WorkerBatch,
		reportSourceConfig.SessionReadMode,
	)
	docH := handler.NewDocumentHandler(database)
	tokenH := handler.NewTokenHandler(database)
	pricingService := pricing.NewService(database)
	tokenAnalyticsH := handler.NewTokenAnalyticsHandler(tokenanalytics.NewService(database))
	pricingAdminH := handler.NewPricingAdminHandler(database, pricingService)
	teamH := handler.NewTeamHandler(database)
	departmentH := handler.NewDepartmentHandler(database)
	followH := handler.NewFollowHandler(database)
	dashboardH := handler.NewDashboardHandler(database)

	r := chi.NewRouter()
	r.Use(chiMiddleware.Logger)
	r.Use(chiMiddleware.Recoverer)
	r.Use(corsMiddleware(cfg.CORSOrigin))

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	r.Post("/api/v1/auth/login", authH.Login)
	r.Post("/api/v1/auth/register", authH.Register)
	r.With(handler.CLIAuthMiddleware(database, cfg.AIHubSecret, aihubClient)).Post("/api/v1/session-syncs/prepare", sessionSyncH.Prepare)
	r.With(handler.CLIAuthMiddleware(database, cfg.AIHubSecret, aihubClient)).Post("/api/v1/session-chunks/batch", sessionSyncH.UploadChunks)
	r.With(handler.CLIAuthMiddleware(database, cfg.AIHubSecret, aihubClient)).Get("/api/v1/session-syncs/{generationId}/status", sessionSyncH.Status)
	r.With(handler.CLIAuthMiddleware(database, cfg.AIHubSecret, aihubClient)).Post("/api/v1/session-syncs/{generationId}/finalize", sessionSyncH.Finalize)
	r.With(handler.CLIAuthMiddleware(database, cfg.AIHubSecret, aihubClient)).Post("/api/v1/session-syncs/{generationId}/abort", sessionSyncH.Abort)
	r.With(handler.CLIAuthMiddleware(database, cfg.AIHubSecret, aihubClient)).Post("/api/v1/sessions/batch", handler.LegacySessionBatchUploadDisabled)

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(handler.AuthMiddleware(database, cfg.AIHubSecret, aihubClient))

		r.Get("/auth/me", authH.Me)
		r.Get("/users", authH.ListUsers)
		r.With(handler.AdminOnly).Get("/aihub/users/search", authH.SearchAIHubUsers)
		r.Get("/task-assignees", authH.ListTaskAssignees)
		r.Get("/teams", authH.ListTeams)
		r.Get("/departments", departmentH.List)
		r.Get("/ai-assets/models", modelCatalogH.List)

		r.Route("/admin", func(r chi.Router) {
			r.Use(handler.AdminOnly)
			r.Put("/users/{id}", authH.AdminUpdateUser)
			r.Put("/users/{id}/profile", authH.AdminUpdateUser)
			r.Post("/users/batch", authH.AdminBatchAddUsers)
			r.Post("/teams", authH.AdminCreateTeam)
			r.Put("/teams/{id}", authH.AdminUpdateTeam)
			r.Delete("/teams/{id}", authH.AdminDeleteTeam)
			r.Post("/departments", departmentH.Create)
			r.Put("/departments/{id}", departmentH.Update)
			r.Get("/price-books", pricingAdminH.ListPriceBooks)
			r.Post("/price-books", pricingAdminH.SavePriceBook)
			r.Get("/model-aliases", pricingAdminH.ListModelAliases)
			r.Post("/model-aliases", pricingAdminH.SaveModelAlias)
			r.Get("/model-price-versions", pricingAdminH.ListModelPrices)
			r.Post("/model-price-versions", pricingAdminH.SaveModelPrice)
			r.Get("/exchange-rate-versions", pricingAdminH.ListExchangeRates)
			r.Post("/exchange-rate-versions", pricingAdminH.SaveExchangeRate)
			r.Post("/pricing/import-suggestions", pricingAdminH.ImportSuggestions)
			r.Get("/pricing/unpriced-models", pricingAdminH.ListUnpricedModels)
			r.Get("/pricing/recalculation-runs", pricingAdminH.ListRecalculationRuns)
			r.Post("/pricing/recalculate/preview", pricingAdminH.RecalculatePreview)
			r.Post("/pricing/recalculate/apply", pricingAdminH.RecalculateApply)
		})

		r.Get("/requirements", reqH.List)
		r.Post("/requirements", reqH.Create)
		r.Get("/requirements/{id}", reqH.Get)
		r.Put("/requirements/{id}", reqH.Update)
		r.Delete("/requirements/{id}", reqH.Delete)
		r.Put("/requirements/{id}/restore", reqH.Restore)
		r.Post("/requirements/{id}/dependencies", reqH.AddDependency)
		r.Delete("/requirements/{id}/dependencies/{target_type}/{dep_id}", reqH.RemoveDependency)
		r.Get("/requirements/{id}/ac", reqH.GetAC)
		r.Post("/requirements/{id}/regenerate-ac", reqH.RegenerateAC)
		r.Get("/requirements/{id}/events", reqH.ListEvents)

		r.Get("/tasks", taskH.List)
		r.Post("/tasks", taskH.Create)
		r.Get("/tasks/{id}", taskH.Get)
		r.Put("/tasks/{id}", taskH.Update)
		r.Delete("/tasks/{id}", taskH.Delete)
		r.Put("/tasks/{id}/status", taskH.UpdateStatus)
		r.Put("/tasks/{id}/progress", taskH.UpdateProgress)
		r.Post("/tasks/{id}/dependencies", taskH.AddDependency)
		r.Delete("/tasks/{id}/dependencies/{dep_id}", taskH.RemoveDependency)
		r.Get("/tasks/{id}/events", taskH.ListEvents)

		r.Get("/follows", followH.List)
		r.Get("/follows/followers", followH.Followers)
		r.Post("/follows", followH.Follow)
		r.Delete("/follows/{target_type}/{target_id}", followH.Unfollow)
		r.Get("/dashboard/follows", dashboardH.Follows)
		r.Get("/dashboard/my-items", dashboardH.MyItems)
		r.Get("/dashboard/risks", dashboardH.Risks)

		r.Get("/sessions", sessionH.List)
		r.Get("/sessions/{id}", sessionH.Get)
		r.Get("/sessions/{id}/log", sessionH.DownloadLog)
		r.Post("/sessions/{id}/clear-content", sessionSyncH.ClearContent)
		r.Post("/sessions/{id}/restore-content", sessionSyncH.RestoreContent)
		r.Put("/sessions/{id}/task", sessionH.UpdateTask)
		r.Put("/sessions/{id}/requirement", sessionH.UpdateRequirement)
		r.Delete("/sessions/{id}", sessionH.Withdraw)

		r.Get("/documents", docH.List)
		r.Post("/documents", docH.Create)
		r.Put("/documents/{id}", docH.Update)
		r.Delete("/documents/{id}", docH.Delete)

		r.Get("/reports", reportH.List)
		r.Get("/reports/mine", reportH.ListMine)
		r.Get("/reports/today", reportH.GetOrCreateToday)
		r.Post("/reports/today/managed-agent-runs", managedAgentH.StartReportRun)
		r.Get("/reports/managed-agent-runs/{runId}", managedAgentH.GetDailyReportRun)
		r.Get("/reports/weekly/mine", reportH.ListPersonalWeeklyReports)
		r.Get("/reports/weekly/mine/current", reportH.GetPersonalWeeklyReportCurrent)
		r.Get("/reports/weekly/mine/sources", reportH.GetPersonalWeeklyReportSources)
		r.Put("/reports/weekly/mine/current", reportH.SavePersonalWeeklyReportCurrent)
		r.Post("/reports/weekly/mine/current/submit", reportH.SubmitPersonalWeeklyReportCurrent)
		r.Delete("/reports/weekly/mine/{id}", reportH.DeletePersonalWeeklyReport)
		r.Get("/reports/members/daily", reportH.ListMemberDailyReports)
		r.Get("/reports/members/daily/{id}", reportH.GetMemberDailyReport)
		r.Get("/reports/members/weekly", reportH.ListMemberWeeklyReports)
		r.Get("/reports/members/weekly/{id}", reportH.GetMemberWeeklyReport)

		r.Get("/reports/team/members", reportH.ListTeamMemberReports)
		r.Get("/reports/team/sources", reportH.GetTeamReportSources)
		r.Get("/reports/team/today", reportH.GetTeamReportToday)
		r.Put("/reports/team/today", reportH.SaveTeamReportToday)
		r.Get("/reports/team/weekly/sources", reportH.GetTeamWeeklyReportSources)
		r.Get("/reports/team/weekly/current", reportH.GetTeamWeeklyReportCurrent)
		r.Put("/reports/team/weekly/current", reportH.SaveTeamWeeklyReportCurrent)
		r.Post("/reports/team/weekly/current/submit", reportH.SubmitTeamWeeklyReportCurrent)
		r.Get("/reports/team/weekly", reportH.ListTeamWeeklyReports)
		r.Put("/reports/team/weekly/{id}", reportH.UpdateTeamWeeklyReport)
		r.Post("/reports/team/weekly/{id}/submit", reportH.SubmitTeamWeeklyReport)
		r.Delete("/reports/team/weekly/{id}", reportH.DeleteTeamWeeklyReport)
		r.Get("/reports/team", reportH.ListTeamReports)
		r.Get("/reports/team/{id}", reportH.GetTeamReport)
		r.Put("/reports/team/{id}", reportH.UpdateTeamReport)
		r.Post("/reports/team/{id}/submit", reportH.SubmitTeamReport)
		r.Delete("/reports/team/{id}", reportH.DeleteTeamDailyReport)
		r.Get("/reports/department/sources", reportH.GetDepartmentReportSources)
		r.Get("/reports/department/today", reportH.GetDepartmentReportToday)
		r.Put("/reports/department/today", reportH.SaveDepartmentReportToday)
		r.Get("/reports/department/weekly/sources", reportH.GetDepartmentWeeklyReportSources)
		r.Get("/reports/department/weekly/current", reportH.GetDepartmentWeeklyReportCurrent)
		r.Put("/reports/department/weekly/current", reportH.SaveDepartmentWeeklyReportCurrent)
		r.Get("/reports/department/weekly", reportH.ListDepartmentWeeklyReports)
		r.Put("/reports/department/weekly/{id}", reportH.UpdateDepartmentWeeklyReport)
		r.Delete("/reports/department/weekly/{id}", reportH.DeleteDepartmentWeeklyReport)
		r.Get("/reports/department", reportH.ListDepartmentReports)
		r.Get("/reports/department/{id}", reportH.GetDepartmentReport)
		r.Put("/reports/department/{id}", reportH.UpdateDepartmentReport)
		r.Delete("/reports/department/{id}", reportH.DeleteDepartmentDailyReport)
		r.Get("/reports/{id}", reportH.Get)
		r.Put("/reports/{id}", reportH.Update)
		r.Post("/reports/{id}/submit", reportH.SubmitReport)
		r.Delete("/reports/{id}", reportH.DeletePersonalDailyReport)

		r.Get("/tokens", tokenH.Aggregate)
		r.Get("/tokens/sessions", tokenH.ListSessionTokens)
		r.Get("/token-analytics/capability", tokenAnalyticsH.Capability)
		r.Get("/token-analytics/summary", tokenAnalyticsH.Summary)
		r.Get("/token-analytics/trends", tokenAnalyticsH.Trends)
		r.Get("/token-analytics/rankings", tokenAnalyticsH.Rankings)
		r.Get("/token-analytics/sessions", tokenAnalyticsH.Sessions)
		r.Get("/teams/activity", teamH.Activity)

		r.Post("/mcp/reports", dailyReportMCPH.Serve)
		r.Get("/report-source-capability", reportSourceH.Capability)
		r.Get("/report-source-sessions", reportSourceH.ListCandidates)
		r.Post("/report-source-selections", reportSourceH.CreateSelection)

		r.Get("/ai-assets/skills", managedAgentH.ListSkills)
		r.Post("/ai-assets/skills", managedAgentH.CreateSkill)
		r.Get("/ai-assets/skills/{owner}/{slug}/{version}/skill-md", managedAgentH.GetSkillMarkdown)
		r.Post("/ai-assets/skills/{slug}/{version}/archive", managedAgentH.ArchiveSkill)
		r.Delete("/ai-assets/skills/{slug}/{version}", managedAgentH.DeleteSkill)
		r.Get("/ai-assets/mcp", managedAgentH.ListMCPEntries)
		r.Post("/ai-assets/mcp", managedAgentH.CreateMCPEntry)
		r.Post("/ai-assets/mcp/{slug}/{version}/archive", managedAgentH.ArchiveMCPEntry)
		r.Delete("/ai-assets/mcp/{slug}/{version}", managedAgentH.DeleteMCPEntry)
		r.Get("/ai-assets/credentials", managedAgentH.ListCredentials)
		r.Post("/ai-assets/credentials", managedAgentH.CreateCredential)
		r.Delete("/ai-assets/credentials/{credentialId}", managedAgentH.DeleteCredential)
		r.Get("/ai-assets/daily-report-integration", managedAgentH.DailyReportIntegration)
		r.Get("/ai-assets/agents", managedAgentH.ListMyAgents)
		r.Post("/ai-assets/agents", managedAgentH.CreateMyAgent)
		r.Post("/ai-assets/report-agents/default", managedAgentH.CreateDefaultReportAgent)
		r.Post("/ai-assets/report-agents/{agentId}/default", managedAgentH.SetDefaultReportAgent)
		r.Put("/ai-assets/agents/{agentId}", managedAgentH.UpdateMyAgent)
		r.Post("/ai-assets/agents/{agentId}/archive", managedAgentH.ArchiveMyAgent)
		r.Post("/ai-assets/agents/{agentId}/runs", managedAgentH.StartAgentRun)
		r.Post("/ai-assets/report-agents/{agentId}/runs", managedAgentH.StartReportAgentRun)
		r.Get("/ai-assets/agent-runs", managedAgentH.ListAgentRuns)
		r.Get("/ai-assets/agent-runs/{runId}", managedAgentH.GetAgentRun)
		r.Get("/ai-assets/agent-schedules", managedAgentH.ListAgentSchedules)
		r.Post("/ai-assets/agent-schedules/preview", managedAgentH.PreviewAgentSchedule)
		r.Post("/ai-assets/agent-schedules", managedAgentH.CreateAgentSchedule)
		r.Put("/ai-assets/agent-schedules/{scheduleId}", managedAgentH.UpdateAgentSchedule)
		r.Delete("/ai-assets/agent-schedules/{scheduleId}", managedAgentH.DeleteAgentSchedule)
		r.Post("/ai-assets/agent-schedules/{scheduleId}/runs", managedAgentH.RunAgentScheduleNow)
	})

	log.Printf("Starting API server on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, r); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func corsMiddleware(origin string) func(http.Handler) http.Handler {
	allowedOrigins := map[string]bool{}
	defaultOrigin := ""
	for _, item := range strings.Split(origin, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			if defaultOrigin == "" {
				defaultOrigin = item
			}
			allowedOrigins[item] = true
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestOrigin := r.Header.Get("Origin")
			if allowedOrigins["*"] {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else if allowedOrigins[requestOrigin] {
				w.Header().Set("Access-Control-Allow-Origin", requestOrigin)
				w.Header().Set("Vary", "Origin")
			} else if defaultOrigin != "" && requestOrigin == "" {
				w.Header().Set("Access-Control-Allow-Origin", defaultOrigin)
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
