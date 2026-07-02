package main

import (
	"context"
	"log"
	"net/http"
	"strings"

	"github.com/aidashboard/api/config"
	"github.com/aidashboard/api/db"
	"github.com/aidashboard/api/handler"
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

	aihubClient := service.NewAIHubClient(cfg.AIHubHost, cfg.AIHubToken)
	authH := handler.NewAuthHandler(database, aihubClient, cfg.BootstrapAdminUIDs)
	aiClient := service.NewAIClient()
	managedAgentClient := service.NewManagedAgentClient(cfg.ManagedAgentURL, cfg.ManagedAgentToken)
	reqH := handler.NewRequirementHandler(database, aiClient)
	taskH := handler.NewTaskHandler(database)
	sessionH := handler.NewSessionHandler(database, minioStore, aiClient)
	reportH := handler.NewReportHandler(database, cfg.ReportGeneratorURL)
	managedAgentH := handler.NewManagedAgentHandlerWithDefaults(database, managedAgentClient, handler.ManagedAgentDefaults{
		Engine:                         cfg.ManagedAgentDefaultEngine,
		ModelID:                        cfg.ManagedAgentDefaultModelID,
		ReportSkillSlug:                cfg.ManagedAgentReportSkillSlug,
		ReportSkillVersion:             cfg.ManagedAgentReportSkillVersion,
		ReportSkillName:                cfg.ManagedAgentReportSkillName,
		ReportSkillDescription:         cfg.ManagedAgentReportSkillDescription,
		ReportSkillMarkdown:            cfg.ManagedAgentReportSkillMarkdown,
		ReportMCPSlug:                  cfg.ManagedAgentReportMCPSlug,
		ReportMCPVersion:               cfg.ManagedAgentReportMCPVersion,
		ReportMCPName:                  cfg.ManagedAgentReportMCPName,
		ReportMCPDescription:           cfg.ManagedAgentReportMCPDescription,
		ReportMCPURL:                   cfg.ManagedAgentReportMCPURL,
		ReportMCPCredentialSlot:        cfg.ManagedAgentReportCredentialSlot,
		ReportAgentName:                cfg.ManagedAgentReportAgentName,
		ReportAgentDescription:         cfg.ManagedAgentReportAgentDescription,
		ReportAgentInstructions:        cfg.ManagedAgentReportAgentInstructions,
		ReportAgentStartPromptTemplate: cfg.ManagedAgentReportAgentStartPrompt,
		ReportAssetRepair:              cfg.ManagedAgentReportAssetRepair,
		ReportAssetRepairConfigured:    true,
		AIDAPublicBaseURL:              cfg.AIDAPublicBaseURL,
		AIHubSecret:                    cfg.AIHubSecret,
	})
	dailyReportMCPH := handler.NewReportMCPHandler(database)
	schedulerCtx, stopScheduler := context.WithCancel(context.Background())
	defer stopScheduler()
	handler.NewManagedAgentScheduleRunner(managedAgentH).Start(schedulerCtx)
	service.NewManagedAgentRunStatusSyncer(database, managedAgentClient).Start(schedulerCtx)
	docH := handler.NewDocumentHandler(database)
	tokenH := handler.NewTokenHandler(database)
	teamH := handler.NewTeamHandler(database)
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

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(handler.AuthMiddleware(database, cfg.AIHubSecret, aihubClient))

		r.Get("/auth/me", authH.Me)
		r.Get("/users", authH.ListUsers)
		r.With(handler.AdminOnly).Get("/aihub/users/search", authH.SearchAIHubUsers)
		r.Get("/task-assignees", authH.ListTaskAssignees)
		r.Get("/teams", authH.ListTeams)

		r.Route("/admin", func(r chi.Router) {
			r.Use(handler.AdminOnly)
			r.Put("/users/{id}", authH.AdminUpdateUser)
			r.Put("/users/{id}/profile", authH.AdminUpdateUser)
			r.Post("/users/batch", authH.AdminBatchAddUsers)
			r.Post("/teams", authH.AdminCreateTeam)
			r.Put("/teams/{id}", authH.AdminUpdateTeam)
			r.Delete("/teams/{id}", authH.AdminDeleteTeam)
		})

		r.Get("/requirements", reqH.List)
		r.Post("/requirements", reqH.Create)
		r.Get("/requirements/{id}", reqH.Get)
		r.Put("/requirements/{id}", reqH.Update)
		r.Delete("/requirements/{id}", reqH.Delete)
		r.Put("/requirements/{id}/restore", reqH.Restore)
		r.Get("/requirements/{id}/ac", reqH.GetAC)
		r.Post("/requirements/{id}/regenerate-ac", reqH.RegenerateAC)

		r.Get("/tasks", taskH.List)
		r.Post("/tasks", taskH.Create)
		r.Get("/tasks/{id}", taskH.Get)
		r.Put("/tasks/{id}", taskH.Update)
		r.Delete("/tasks/{id}", taskH.Delete)
		r.Put("/tasks/{id}/status", taskH.UpdateStatus)
		r.Put("/tasks/{id}/progress", taskH.UpdateProgress)
		r.Post("/tasks/{id}/dependencies", taskH.AddDependency)
		r.Delete("/tasks/{id}/dependencies/{dep_id}", taskH.RemoveDependency)

		r.Get("/follows", followH.List)
		r.Get("/follows/followers", followH.Followers)
		r.Post("/follows", followH.Follow)
		r.Delete("/follows/{target_type}/{target_id}", followH.Unfollow)
		r.Get("/dashboard/follows", dashboardH.Follows)
		r.Get("/dashboard/risks", dashboardH.Risks)

		r.Post("/sessions/batch", sessionH.BatchUpload)
		r.Get("/sessions", sessionH.List)
		r.Get("/sessions/{id}", sessionH.Get)
		r.Get("/sessions/{id}/log", sessionH.DownloadLog)
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
		r.Post("/reports/today/draft", reportH.GenerateTodayDraft)
		r.Post("/reports/today/managed-agent-runs", managedAgentH.StartReportRun)
		r.Get("/reports/managed-agent-runs/{runId}", managedAgentH.GetDailyReportRun)
		r.Post("/reports/today/generate", reportH.GenerateToday)
		r.Get("/reports/weekly/mine", reportH.ListPersonalWeeklyReports)
		r.Get("/reports/weekly/mine/current", reportH.GetPersonalWeeklyReportCurrent)
		r.Get("/reports/weekly/mine/sources", reportH.GetPersonalWeeklyReportSources)
		r.Post("/reports/weekly/mine/current/generate", reportH.GeneratePersonalWeeklyReportPreview)
		r.Put("/reports/weekly/mine/current", reportH.SavePersonalWeeklyReportCurrent)
		r.Post("/reports/weekly/mine/current/submit", reportH.SubmitPersonalWeeklyReportCurrent)

		r.Get("/reports/team/members", reportH.ListTeamMemberReports)
		r.Get("/reports/team/sources", reportH.GetTeamReportSources)
		r.Get("/reports/team/today", reportH.GetTeamReportToday)
		r.Post("/reports/team/today/generate", reportH.GenerateTeamReport)
		r.Put("/reports/team/today", reportH.SaveTeamReportToday)
		r.Get("/reports/team/weekly/sources", reportH.GetTeamWeeklyReportSources)
		r.Get("/reports/team/weekly/current", reportH.GetTeamWeeklyReportCurrent)
		r.Post("/reports/team/weekly/current/generate", reportH.GenerateTeamWeeklyReport)
		r.Put("/reports/team/weekly/current", reportH.SaveTeamWeeklyReportCurrent)
		r.Post("/reports/team/weekly/current/submit", reportH.SubmitTeamWeeklyReportCurrent)
		r.Get("/reports/team/weekly", reportH.ListTeamWeeklyReports)
		r.Put("/reports/team/weekly/{id}", reportH.UpdateTeamWeeklyReport)
		r.Post("/reports/team/weekly/{id}/submit", reportH.SubmitTeamWeeklyReport)
		r.Get("/reports/team", reportH.ListTeamReports)
		r.Get("/reports/team/{id}", reportH.GetTeamReport)
		r.Put("/reports/team/{id}", reportH.UpdateTeamReport)
		r.Post("/reports/team/{id}/submit", reportH.SubmitTeamReport)
		r.Get("/reports/department/sources", reportH.GetDepartmentReportSources)
		r.Get("/reports/department/today", reportH.GetDepartmentReportToday)
		r.Post("/reports/department/today/generate", reportH.GenerateDepartmentReport)
		r.Put("/reports/department/today", reportH.SaveDepartmentReportToday)
		r.Get("/reports/department/weekly/sources", reportH.GetDepartmentWeeklyReportSources)
		r.Get("/reports/department/weekly/current", reportH.GetDepartmentWeeklyReportCurrent)
		r.Post("/reports/department/weekly/current/generate", reportH.GenerateDepartmentWeeklyReport)
		r.Put("/reports/department/weekly/current", reportH.SaveDepartmentWeeklyReportCurrent)
		r.Get("/reports/department/weekly", reportH.ListDepartmentWeeklyReports)
		r.Put("/reports/department/weekly/{id}", reportH.UpdateDepartmentWeeklyReport)
		r.Get("/reports/department", reportH.ListDepartmentReports)
		r.Get("/reports/department/{id}", reportH.GetDepartmentReport)
		r.Put("/reports/department/{id}", reportH.UpdateDepartmentReport)
		r.Get("/reports/{id}", reportH.Get)
		r.Put("/reports/{id}", reportH.Update)
		r.Post("/reports/{id}/submit", reportH.SubmitReport)

		r.Get("/tokens", tokenH.Aggregate)
		r.Get("/tokens/sessions", tokenH.ListSessionTokens)
		r.Get("/teams/activity", teamH.Activity)

		r.Post("/mcp/reports", dailyReportMCPH.Serve)

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
