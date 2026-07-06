package model

import "time"

type Team struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	DirectorUserID *string   `json:"director_user_id,omitempty"`
	DirectorName   *string   `json:"director_name,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type User struct {
	ID            string     `json:"id"`
	Username      string     `json:"username"`
	Nickname      string     `json:"nickname"`
	Email         string     `json:"email"`
	Name          string     `json:"name"`
	EmployeeID    string     `json:"employee_id"`
	AppRole       string     `json:"app_role"`
	Role          string     `json:"role"`
	TeamID        *string    `json:"team_id,omitempty"`
	TeamName      *string    `json:"team_name,omitempty"`
	LocalEnabled  bool       `json:"local_enabled"`
	Status        string     `json:"status"`
	LastSyncedAt  *time.Time `json:"last_synced_at,omitempty"`
	DeactivatedAt *time.Time `json:"deactivated_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type Requirement struct {
	ID                 string                   `json:"id"`
	Title              string                   `json:"title"`
	Description        string                   `json:"description"`
	FeishuDocURL       *string                  `json:"feishu_doc_url,omitempty"`
	AcceptanceCriteria []string                 `json:"acceptance_criteria"`
	CreatorID          string                   `json:"creator_id"`
	CreatorName        string                   `json:"creator_name"`
	CreatorRole        string                   `json:"creator_role"`
	ResponsibleUserIDs []string                 `json:"responsible_user_ids"`
	ResponsibleUsers   []ResponsibleUser        `json:"responsible_users"`
	Status             string                   `json:"status"`
	Priority           string                   `json:"priority"`
	Progress           int                      `json:"progress"`
	Deadline           *string                  `json:"deadline,omitempty"`
	TeamIDs            []string                 `json:"team_ids"`
	TeamNames          []string                 `json:"team_names"`
	TokenSourceIDs     []string                 `json:"token_source_ids"`
	Dependencies       []TaskDep                `json:"dependencies,omitempty"`
	Blocking           []TaskDep                `json:"blocking,omitempty"`
	TaskSummary        RequirementTaskSummary   `json:"task_summary"`
	RiskSummary        RequirementRiskSummary   `json:"risk_summary"`
	FollowSummary      RequirementFollowSummary `json:"follow_summary"`
	IsFollowed         bool                     `json:"is_followed"`
	CanUpdate          bool                     `json:"can_update"`
	CanChangeStatus    bool                     `json:"can_change_status"`
	CanCancel          bool                     `json:"can_cancel"`
	CanRestore         bool                     `json:"can_restore"`
	CanDelete          bool                     `json:"can_delete"`
	CanManageAC        bool                     `json:"can_manage_ac"`
	CanCreateTask      bool                     `json:"can_create_task"`
	CompletedAt        *time.Time               `json:"completed_at,omitempty"`
	CreatedAt          time.Time                `json:"created_at"`
	UpdatedAt          time.Time                `json:"updated_at"`
	Version            int64                    `json:"version"`
}

type Task struct {
	ID                    string                   `json:"id"`
	RequirementID         string                   `json:"requirement_id"`
	RequirementTitle      string                   `json:"requirement_title,omitempty"`
	Title                 string                   `json:"title"`
	AcceptanceCriteria    []string                 `json:"acceptance_criteria"`
	CreatorID             string                   `json:"creator_id"`
	CreatorName           string                   `json:"creator_name"`
	ResponsibleUserIDs    []string                 `json:"responsible_user_ids"`
	ResponsibleUsers      []ResponsibleUser        `json:"responsible_users"`
	Status                string                   `json:"status"`
	DisplayStatus         string                   `json:"display_status"`
	Priority              string                   `json:"priority"`
	Progress              int                      `json:"progress"`
	DueDate               *string                  `json:"due_date,omitempty"`
	Dependencies          []TaskDep                `json:"dependencies,omitempty"`
	Blocking              []TaskDep                `json:"blocking,omitempty"`
	RiskTypes             []string                 `json:"risk_types"`
	TokenSourceIDs        []string                 `json:"token_source_ids"`
	FollowSummary         RequirementFollowSummary `json:"follow_summary"`
	IsFollowed            bool                     `json:"is_followed"`
	CanUpdateMeta         bool                     `json:"can_update_meta"`
	CanReassign           bool                     `json:"can_reassign"`
	CanUpdateStatus       bool                     `json:"can_update_status"`
	CanUpdateProgress     bool                     `json:"can_update_progress"`
	CanManageDependencies bool                     `json:"can_manage_dependencies"`
	CanDelete             bool                     `json:"can_delete"`
	CompletedAt           *time.Time               `json:"completed_at,omitempty"`
	CreatedAt             time.Time                `json:"created_at"`
	UpdatedAt             time.Time                `json:"updated_at"`
	Version               int64                    `json:"version"`
}

type TaskDep struct {
	ItemType         string   `json:"item_type"`
	ItemID           string   `json:"item_id"`
	Title            string   `json:"title"`
	TaskID           string   `json:"task_id,omitempty"`
	TaskTitle        string   `json:"task_title,omitempty"`
	RequirementID    string   `json:"requirement_id,omitempty"`
	RequirementTitle string   `json:"requirement_title,omitempty"`
	Status           string   `json:"status"`
	ResponsibleIDs   []string `json:"responsible_user_ids,omitempty"`
	ResponsibleNames []string `json:"responsible_names,omitempty"`
	DueDate          *string  `json:"due_date,omitempty"`
}

type RequirementTaskSummary struct {
	Total   int `json:"total"`
	Done    int `json:"done"`
	Blocked int `json:"blocked"`
}

type RequirementRiskSummary struct {
	Blocked            int `json:"blocked"`
	Overdue            int `json:"overdue"`
	RequirementOverdue int `json:"requirement_overdue"`
	DependencyConflict int `json:"dependency_conflict"`
}

type ResponsibleUser struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Role     string  `json:"role"`
	TeamID   *string `json:"team_id,omitempty"`
	TeamName *string `json:"team_name,omitempty"`
}

type RequirementFollowSummary struct {
	Count int    `json:"count"`
	Score int    `json:"score"`
	Level string `json:"level"`
}

type RequirementFollowState struct {
	Requirement bool `json:"requirement"`
	TaskCount   int  `json:"task_count"`
}

type WorkItemEvent struct {
	ID            string         `json:"id"`
	TargetType    string         `json:"target_type"`
	TargetID      string         `json:"target_id"`
	RequirementID *string        `json:"requirement_id,omitempty"`
	TaskID        *string        `json:"task_id,omitempty"`
	ActorID       *string        `json:"actor_id,omitempty"`
	ActorName     string         `json:"actor_name"`
	ActorRole     string         `json:"actor_role"`
	EventType     string         `json:"event_type"`
	EventTitle    string         `json:"event_title"`
	BeforeData    map[string]any `json:"before_data"`
	AfterData     map[string]any `json:"after_data"`
	Metadata      map[string]any `json:"metadata"`
	CreatedAt     time.Time      `json:"created_at"`
}

type PaginatedWorkItemEvents struct {
	Items    []WorkItemEvent `json:"items"`
	Total    int             `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
}

// P0 API DTO aliases keep the contract names explicit while reusing the
// existing transport structs used by legacy handlers.
type RequirementListItemDTO = Requirement
type RequirementDetailDTO = Requirement
type RequirementTaskDTO = Task
type TaskDependencyDTO = TaskDep
type RequirementRiskSummaryDTO = RequirementRiskSummary
type RequirementFollowStateDTO = RequirementFollowState

type PaginatedRequirements struct {
	Items    []Requirement `json:"items"`
	Total    int           `json:"total"`
	Page     int           `json:"page"`
	PageSize int           `json:"page_size"`
}

type PaginatedTasks struct {
	Items    []Task `json:"items"`
	Total    int    `json:"total"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}

type RequirementBoardColumn struct {
	Status   string        `json:"status"`
	Items    []Requirement `json:"items"`
	Total    int           `json:"total"`
	Page     int           `json:"page"`
	PageSize int           `json:"page_size"`
	HasMore  bool          `json:"has_more"`
}

type RequirementBoardResponse struct {
	Columns        []RequirementBoardColumn `json:"columns"`
	Total          int                      `json:"total"`
	ColumnPageSize int                      `json:"column_page_size"`
}

type UserFollow struct {
	UserID     string    `json:"user_id"`
	TargetType string    `json:"target_type"`
	TargetID   string    `json:"target_id"`
	CreatedAt  time.Time `json:"created_at"`
}

type FollowFollower struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Role       string    `json:"role"`
	TeamID     *string   `json:"teamId,omitempty"`
	TeamName   *string   `json:"teamName,omitempty"`
	FollowedAt time.Time `json:"followedAt"`
}

type PaginatedFollowFollowers struct {
	Items    []FollowFollower `json:"items"`
	Total    int              `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
}

type FollowRequest struct {
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
}

type DashboardNavigationTarget struct {
	RequirementID string  `json:"requirementId"`
	TaskID        *string `json:"taskId,omitempty"`
	URL           string  `json:"url"`
}

type DashboardFollowItem struct {
	Key                         string                    `json:"key"`
	Type                        string                    `json:"type"`
	Title                       string                    `json:"title"`
	Requirement                 string                    `json:"requirement,omitempty"`
	RequirementID               string                    `json:"requirementId"`
	TaskID                      *string                   `json:"taskId,omitempty"`
	CreatorID                   string                    `json:"creatorId,omitempty"`
	CreatorName                 string                    `json:"creatorName,omitempty"`
	RequirementResponsibleIDs   []string                  `json:"requirementResponsibleIds,omitempty"`
	RequirementResponsibleNames []string                  `json:"requirementResponsibleNames,omitempty"`
	TaskResponsibleIDs          []string                  `json:"taskResponsibleIds,omitempty"`
	TaskResponsibleNames        []string                  `json:"taskResponsibleNames,omitempty"`
	ResponsibleNames            []string                  `json:"responsibleNames,omitempty"`
	Status                      string                    `json:"status"`
	Deadline                    string                    `json:"deadline"`
	Risk                        string                    `json:"risk"`
	Dependency                  string                    `json:"dependency,omitempty"`
	BlockingTasks               []TaskDep                 `json:"blockingTasks,omitempty"`
	Activity                    string                    `json:"activity,omitempty"`
	FollowedByMe                bool                      `json:"followedByMe"`
	CreatedByMe                 bool                      `json:"createdByMe"`
	AssignedToMe                bool                      `json:"assignedToMe"`
	AttentionScore              int                       `json:"attentionScore"`
	AttentionLevel              string                    `json:"attentionLevel"`
	FollowerCount               int                       `json:"followerCount"`
	RiskPriority                int                       `json:"riskPriority"`
	SortDueDate                 *string                   `json:"-"`
	SortUpdatedAt               time.Time                 `json:"-"`
	Navigation                  DashboardNavigationTarget `json:"navigation"`
}

type PaginatedDashboardFollowItems struct {
	Items    []DashboardFollowItem `json:"items"`
	Total    int                   `json:"total"`
	Page     int                   `json:"page"`
	PageSize int                   `json:"page_size"`
}

type DashboardRiskTaskSummary struct {
	TaskID                    string    `json:"taskId"`
	Title                     string    `json:"title"`
	Deadline                  string    `json:"deadline,omitempty"`
	RiskTypes                 []string  `json:"riskTypes"`
	BlockingDependencies      []TaskDep `json:"blockingDependencies,omitempty"`
	UnfinishedDependencyCount int       `json:"unfinishedDependencyCount,omitempty"`
	SortDueDate               *string   `json:"-"`
	SortUpdatedAt             time.Time `json:"-"`
}

type DashboardRiskGroup struct {
	Key                     string                    `json:"key"`
	DisplayType             string                    `json:"displayType"`
	RequirementID           string                    `json:"requirementId"`
	RequirementTitle        string                    `json:"requirementTitle"`
	RiskTypes               []string                  `json:"riskTypes"`
	RequirementOverdue      bool                      `json:"requirementOverdue"`
	DeadlineTaskCount       int                       `json:"deadlineTaskCount"`
	DependencyBlockerCount  int                       `json:"dependencyBlockerCount"`
	DependencyConflictCount int                       `json:"dependencyConflictCount"`
	RepresentativeTask      *DashboardRiskTaskSummary `json:"representativeTask,omitempty"`
	Summary                 string                    `json:"summary"`
	Deadline                string                    `json:"deadline"`
	Level                   string                    `json:"level"`
	Tone                    string                    `json:"tone"`
	AttentionScore          int                       `json:"attentionScore"`
	AttentionLevel          string                    `json:"attentionLevel"`
	ActionText              string                    `json:"actionText"`
	TargetURL               string                    `json:"targetUrl"`
	Navigation              DashboardNavigationTarget `json:"navigation"`
	SortHasOverdue          bool                      `json:"-"`
	SortEarliestOverdueDate *string                   `json:"-"`
	SortUpdatedAt           time.Time                 `json:"-"`
}

type PaginatedDashboardRiskGroups struct {
	Items    []DashboardRiskGroup `json:"items"`
	Total    int                  `json:"total"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"page_size"`
}

type Session struct {
	ID              string     `json:"id"`
	SessionRef      string     `json:"session_ref"`
	UserID          string     `json:"user_id"`
	UserName        string     `json:"user_name"`
	AgentType       string     `json:"agent_type"`
	StartedAt       time.Time  `json:"started_at"`
	EndedAt         *time.Time `json:"ended_at,omitempty"`
	DurationSecs    *int       `json:"duration_secs,omitempty"`
	Model           string     `json:"model"`
	Summary         *string    `json:"summary,omitempty"`
	ToolCallsJSON   any        `json:"tool_calls_json,omitempty"`
	GitCommits      []string   `json:"git_commits,omitempty"`
	TaskID          *string    `json:"task_id,omitempty"`
	TaskTitle       *string    `json:"task_title,omitempty"`
	RequirementID   *string    `json:"requirement_id,omitempty"`
	MatchConfidence *float64   `json:"match_confidence,omitempty"`
	RawLogURL       *string    `json:"raw_log_url,omitempty"`
	UploadedAt      time.Time  `json:"uploaded_at"`
}

type PaginatedSessions struct {
	Items    []Session `json:"items"`
	Total    int       `json:"total"`
	Page     int       `json:"page"`
	PageSize int       `json:"page_size"`
}

type TokenUsage struct {
	ID            string    `json:"id"`
	SessionID     string    `json:"session_id"`
	UserID        string    `json:"user_id"`
	TaskID        *string   `json:"task_id,omitempty"`
	RequirementID *string   `json:"requirement_id,omitempty"`
	AgentType     string    `json:"agent_type"`
	Model         string    `json:"model"`
	InputTokens   int64     `json:"input_tokens"`
	OutputTokens  int64     `json:"output_tokens"`
	TotalTokens   int64     `json:"total_tokens"`
	RecordedAt    time.Time `json:"recorded_at"`
}

type DailyReport struct {
	ID                string     `json:"id"`
	UserID            string     `json:"user_id"`
	UserName          string     `json:"user_name"`
	ReportDate        string     `json:"report_date"`
	Content           string     `json:"content"`
	SubmittedContent  *string    `json:"submitted_content,omitempty"`
	Status            *string    `json:"status,omitempty"`
	SubmittedTo       *string    `json:"submitted_to,omitempty"`
	Edited            bool       `json:"edited"`
	FeishuDocURL      *string    `json:"feishu_doc_url,omitempty"`
	SessionIDs        []string   `json:"session_ids"`
	GenerationMode    string     `json:"generation_mode,omitempty"`
	ManagedAgentRunID *string    `json:"managed_agent_run_id"`
	AgentRunID        *string    `json:"agent_run_id,omitempty"`
	AgentID           *string    `json:"agent_id"`
	AgentVersionID    *int       `json:"agent_version_id"`
	ModelID           *string    `json:"model_id"`
	GeneratedAt       *time.Time `json:"generated_at,omitempty"`
	ProductStatus     string     `json:"product_status,omitempty"`
	Origin            string     `json:"origin,omitempty"`
	UpdatedByUser     bool       `json:"updated_by_user"`
	SavedAt           *time.Time `json:"saved_at,omitempty"`
	SubmittedAt       *time.Time `json:"submitted_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type DailyReportListItem struct {
	ID                 string     `json:"id"`
	UserID             string     `json:"user_id"`
	UserName           string     `json:"user_name"`
	ReportDate         string     `json:"report_date"`
	Status             *string    `json:"status,omitempty"`
	SubmittedTo        *string    `json:"submitted_to,omitempty"`
	Edited             bool       `json:"edited"`
	SourceSessionCount int        `json:"source_session_count"`
	SessionIDs         []string   `json:"session_ids"`
	SavedAt            *time.Time `json:"saved_at,omitempty"`
	SubmittedAt        *time.Time `json:"submitted_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type PaginatedDailyReports struct {
	Items    []DailyReportListItem `json:"items"`
	Total    int                   `json:"total"`
	Page     int                   `json:"page"`
	PageSize int                   `json:"page_size"`
}

// Request/Response types

type LoginRequest struct {
	Username   string `json:"username"`
	EmployeeID string `json:"employee_id"`
	Password   string `json:"password"`
}

type LoginResponse struct {
	Token string `json:"token"`
	User  User   `json:"user"`
}

type AdminCreateTeamRequest struct {
	Name           string  `json:"name"`
	DirectorUserID *string `json:"director_user_id,omitempty"`
}

type AdminUpdateUserRequest struct {
	AppRole      *string `json:"app_role,omitempty"`
	Role         *string `json:"role,omitempty"`
	TeamID       *string `json:"team_id,omitempty"`
	ClearTeam    bool    `json:"clear_team,omitempty"`
	LocalEnabled *bool   `json:"local_enabled,omitempty"`
}

type AIHubUserSearchItem struct {
	ID              int64   `json:"id"`
	Username        string  `json:"username"`
	Nickname        string  `json:"nickname"`
	Email           string  `json:"email"`
	Status          int     `json:"status,omitempty"`
	AidaStatus      string  `json:"aida_status"`
	AidaStatusLabel string  `json:"aida_status_label"`
	CurrentAppRole  *string `json:"current_app_role,omitempty"`
	CurrentTeamID   *string `json:"current_team_id,omitempty"`
	CurrentTeamName *string `json:"current_team_name,omitempty"`
}

type AdminBatchAddUsersRequest struct {
	UserIDs      []int64 `json:"user_ids"`
	AppRole      string  `json:"app_role"`
	TeamID       *string `json:"team_id,omitempty"`
	LocalEnabled *bool   `json:"local_enabled,omitempty"`
}

type AdminBatchAddUserResult struct {
	ID       string `json:"id"`
	Username string `json:"username,omitempty"`
	Nickname string `json:"nickname,omitempty"`
	Email    string `json:"email,omitempty"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
}

type AdminBatchAddUsersResponse struct {
	Created         int                       `json:"created"`
	Skipped         int                       `json:"skipped"`
	SkippedExisting int                       `json:"skipped_existing"`
	Failed          int                       `json:"failed"`
	Results         []AdminBatchAddUserResult `json:"results"`
}

type CreateRequirementRequest struct {
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	FeishuDocURL       *string  `json:"feishu_doc_url,omitempty"`
	Priority           string   `json:"priority"`
	Deadline           *string  `json:"deadline,omitempty"`
	ResponsibleUserIDs []string `json:"responsible_user_ids,omitempty"`
	TeamIDs            []string `json:"team_ids"`
	AcceptanceCriteria []string `json:"acceptance_criteria,omitempty"`
}

type UpdateRequirementRequest struct {
	Title              *string   `json:"title,omitempty"`
	Description        *string   `json:"description,omitempty"`
	FeishuDocURL       *string   `json:"feishu_doc_url,omitempty"`
	Priority           *string   `json:"priority,omitempty"`
	Status             *string   `json:"status,omitempty"`
	Deadline           *string   `json:"deadline,omitempty"`
	ResponsibleUserIDs *[]string `json:"responsible_user_ids,omitempty"`
	TeamIDs            *[]string `json:"team_ids,omitempty"`
	AcceptanceCriteria *[]string `json:"acceptance_criteria,omitempty"`
	BaseVersion        int64     `json:"base_version"`
}

type RequirementVersionRequest struct {
	BaseVersion int64 `json:"base_version"`
}

type RegenerateACRequest struct {
	BaseVersion int64 `json:"base_version"`
}

type CreateTaskRequest struct {
	RequirementID      string   `json:"requirement_id"`
	Title              string   `json:"title"`
	AcceptanceCriteria []string `json:"acceptance_criteria,omitempty"`
	ResponsibleUserIDs []string `json:"responsible_user_ids,omitempty"`
	Priority           string   `json:"priority"`
	DueDate            *string  `json:"due_date,omitempty"`
	DependsOnIDs       []string `json:"depends_on_ids,omitempty"`
}

type UpdateTaskRequest struct {
	Title              *string   `json:"title,omitempty"`
	AcceptanceCriteria *[]string `json:"acceptance_criteria,omitempty"`
	ResponsibleUserIDs *[]string `json:"responsible_user_ids,omitempty"`
	Status             *string   `json:"status,omitempty"`
	Priority           *string   `json:"priority,omitempty"`
	DueDate            *string   `json:"due_date,omitempty"`
	Progress           *int      `json:"progress,omitempty"`
	BaseVersion        int64     `json:"base_version"`
}

type UpdateTaskStatusRequest struct {
	Status      string `json:"status"`
	BaseVersion int64  `json:"base_version"`
}

type UpdateTaskProgressRequest struct {
	Progress    int   `json:"progress"`
	BaseVersion int64 `json:"base_version"`
}

type UpdateSessionRequirementRequest struct {
	RequirementID *string `json:"requirement_id"`
	ActivityDate  *string `json:"activity_date,omitempty"`
}

type AddDependencyRequest struct {
	DependsOnType string `json:"depends_on_type,omitempty"`
	DependsOnID   string `json:"depends_on_id"`
	BaseVersion   int64  `json:"base_version"`
}

type BatchSessionUpload struct {
	Sessions []SessionUpload `json:"sessions"`
}

type SessionUpload struct {
	SessionRef     string                       `json:"session_ref"`
	AgentType      string                       `json:"agent_type,omitempty"`
	StartedAt      time.Time                    `json:"started_at"`
	EndedAt        *time.Time                   `json:"ended_at,omitempty"`
	DurationSecs   *int                         `json:"duration_secs,omitempty"`
	Model          string                       `json:"model"`
	Summary        *string                      `json:"summary,omitempty"`
	ToolCalls      map[string]int               `json:"tool_calls,omitempty"`
	GitCommits     []string                     `json:"git_commits,omitempty"`
	TokenUsage     *TokenUpload                 `json:"token_usage,omitempty"`
	ActivitySlices []SessionActivitySliceUpload `json:"activity_slices,omitempty"`
}

type TokenUpload struct {
	InputTokens         int64    `json:"input_tokens"`
	OutputTokens        int64    `json:"output_tokens"`
	CacheCreationTokens int64    `json:"cache_creation_tokens"`
	CacheReadTokens     int64    `json:"cache_read_tokens"`
	TotalTokens         int64    `json:"total_tokens"`
	Models              []string `json:"models,omitempty"`
}

type SessionActivitySliceUpload struct {
	ActivityDate        string         `json:"activity_date"`
	ActivityStartAt     time.Time      `json:"activity_start_at"`
	ActivityEndAt       time.Time      `json:"activity_end_at"`
	Timezone            string         `json:"timezone,omitempty"`
	AgentType           string         `json:"agent_type,omitempty"`
	Model               string         `json:"model,omitempty"`
	Models              []string       `json:"models,omitempty"`
	Summary             string         `json:"summary,omitempty"`
	Excerpt             string         `json:"excerpt,omitempty"`
	MessageCount        int            `json:"message_count,omitempty"`
	SourceEventCount    int            `json:"source_event_count,omitempty"`
	ToolCalls           map[string]int `json:"tool_calls,omitempty"`
	GitCommits          []string       `json:"git_commits,omitempty"`
	InputTokens         int64          `json:"input_tokens,omitempty"`
	OutputTokens        int64          `json:"output_tokens,omitempty"`
	CacheCreationTokens int64          `json:"cache_creation_tokens,omitempty"`
	CacheReadTokens     int64          `json:"cache_read_tokens,omitempty"`
	TotalTokens         int64          `json:"total_tokens,omitempty"`
	SourceHasRawLog     bool           `json:"source_has_raw_log"`
	TokenSliceStrategy  string         `json:"token_slice_strategy,omitempty"`
	SummaryStrategy     string         `json:"summary_strategy,omitempty"`
	ParserVersion       string         `json:"parser_version,omitempty"`
	SliceVersion        int            `json:"slice_version,omitempty"`
	IsEstimated         bool           `json:"is_estimated"`
}

type UpdateSessionTaskRequest struct {
	TaskID       *string `json:"task_id"`
	ActivityDate *string `json:"activity_date,omitempty"`
}

type UpdateReportRequest struct {
	Content      *string   `json:"content,omitempty"`
	FeishuDocURL *string   `json:"feishu_doc_url,omitempty"`
	SessionIDs   *[]string `json:"session_ids,omitempty"`
}

type SubmitReportRequest struct {
	Content    *string   `json:"content,omitempty"`
	SessionIDs *[]string `json:"session_ids,omitempty"`
}

type WeeklySessionSource struct {
	SessionID        string     `json:"session_id"`
	SessionRef       string     `json:"session_ref"`
	AgentType        string     `json:"agent_type"`
	StartedAt        time.Time  `json:"started_at"`
	EndedAt          *time.Time `json:"ended_at,omitempty"`
	Summary          string     `json:"summary"`
	TaskID           *string    `json:"task_id,omitempty"`
	TaskTitle        string     `json:"task_title,omitempty"`
	RequirementID    *string    `json:"requirement_id,omitempty"`
	RequirementTitle string     `json:"requirement_title,omitempty"`
	TotalTokens      int64      `json:"total_tokens"`
}

type PersonalWeeklyReportSources struct {
	UserID       string                    `json:"user_id"`
	UserName     string                    `json:"user_name"`
	WeekStart    string                    `json:"week_start"`
	WeekEnd      string                    `json:"week_end"`
	DailyReports []WeeklyDailyReportSource `json:"daily_reports"`
	DailyCount   int                       `json:"daily_count"`
}

type PersonalWeeklyReport struct {
	ID                   string     `json:"id"`
	UserID               string     `json:"user_id"`
	UserName             string     `json:"user_name"`
	WeekStart            string     `json:"week_start"`
	WeekEnd              string     `json:"week_end"`
	Content              string     `json:"content"`
	SubmittedContent     *string    `json:"submitted_content,omitempty"`
	Status               string     `json:"status"`
	SavedAt              *time.Time `json:"saved_at,omitempty"`
	SubmittedAt          *time.Time `json:"submitted_at,omitempty"`
	SubmittedTo          *string    `json:"submitted_to,omitempty"`
	SourceDailyReportIDs []string   `json:"source_daily_report_ids"`
	SourceSessionIDs     []string   `json:"source_session_ids"`
	SourceTaskIDs        []string   `json:"source_task_ids"`
	GenerationMode       string     `json:"generation_mode,omitempty"`
	ManagedAgentRunID    *string    `json:"managed_agent_run_id"`
	AgentRunID           *string    `json:"agent_run_id,omitempty"`
	AgentID              *string    `json:"agent_id"`
	AgentVersionID       *int       `json:"agent_version_id"`
	ModelID              *string    `json:"model_id"`
	Edited               bool       `json:"edited"`
	GeneratedAt          *time.Time `json:"generated_at,omitempty"`
	ProductStatus        string     `json:"product_status,omitempty"`
	Origin               string     `json:"origin,omitempty"`
	UpdatedByUser        bool       `json:"updated_by_user"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

type PersonalWeeklyReportListItem struct {
	ID                 string     `json:"id"`
	UserID             string     `json:"user_id"`
	UserName           string     `json:"user_name"`
	WeekStart          string     `json:"week_start"`
	WeekEnd            string     `json:"week_end"`
	Status             string     `json:"status"`
	SavedAt            *time.Time `json:"saved_at,omitempty"`
	SubmittedAt        *time.Time `json:"submitted_at,omitempty"`
	SubmittedTo        *string    `json:"submitted_to,omitempty"`
	SourceDailyCount   int        `json:"source_daily_count"`
	SourceSessionCount int        `json:"source_session_count"`
	SourceTaskCount    int        `json:"source_task_count"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type PaginatedPersonalWeeklyReports struct {
	Items    []PersonalWeeklyReportListItem `json:"items"`
	Total    int                            `json:"total"`
	Page     int                            `json:"page"`
	PageSize int                            `json:"page_size"`
}

type GeneratePersonalWeeklyReportRequest struct {
	WeekStart            string   `json:"week_start"`
	SourceDailyReportIDs []string `json:"source_daily_report_ids"`
}

type PersonalWeeklyReportPreview struct {
	ReportMarkdown       string   `json:"report_markdown"`
	WeekStart            string   `json:"week_start"`
	WeekEnd              string   `json:"week_end"`
	SourceDailyReportIDs []string `json:"source_daily_report_ids"`
}

type SavePersonalWeeklyReportRequest struct {
	WeekStart            string   `json:"week_start"`
	Content              string   `json:"content"`
	SourceDailyReportIDs []string `json:"source_daily_report_ids"`
}

type GenerateReportDraftRequest struct {
	ReportDate          string   `json:"report_date"`
	SessionIDs          []string `json:"session_ids"`
	SkillID             string   `json:"skill_id"`
	SkillContent        string   `json:"skill_content,omitempty"`
	IncludeTaskProgress bool     `json:"include_task_progress"`
}

type ReportDraftSession struct {
	ID               string         `json:"id"`
	SessionRef       string         `json:"session_ref"`
	AgentType        string         `json:"agent_type"`
	StartedAt        time.Time      `json:"started_at"`
	EndedAt          *time.Time     `json:"ended_at,omitempty"`
	DurationSecs     *int           `json:"duration_secs,omitempty"`
	Model            string         `json:"model"`
	Summary          string         `json:"summary,omitempty"`
	ToolCallsJSON    map[string]int `json:"tool_calls_json,omitempty"`
	TaskID           *string        `json:"task_id,omitempty"`
	TaskTitle        string         `json:"task_title,omitempty"`
	RequirementID    *string        `json:"requirement_id,omitempty"`
	RequirementTitle string         `json:"requirement_title,omitempty"`
	InputTokens      int64          `json:"input_tokens"`
	OutputTokens     int64          `json:"output_tokens"`
	TotalTokens      int64          `json:"total_tokens"`
}

type ReportDraftTaskCandidate struct {
	TaskID           string `json:"task_id"`
	TaskTitle        string `json:"task_title"`
	RequirementID    string `json:"requirement_id"`
	RequirementTitle string `json:"requirement_title"`
	CurrentStatus    string `json:"current_status"`
	CurrentProgress  int    `json:"current_progress"`
	Owner            string `json:"owner"`
}

type ReportDraftGeneratorRequest struct {
	UserID              string                     `json:"user_id"`
	UserName            string                     `json:"user_name"`
	ReportDate          string                     `json:"report_date"`
	Sessions            []ReportDraftSession       `json:"sessions"`
	TaskCandidates      []ReportDraftTaskCandidate `json:"task_candidates"`
	SkillID             string                     `json:"skill_id"`
	SkillContent        string                     `json:"skill_content,omitempty"`
	IncludeTaskProgress bool                       `json:"include_task_progress"`
}

type TaskProgressSuggestion struct {
	TaskID                string   `json:"task_id"`
	TaskTitle             string   `json:"task_title"`
	RequirementID         string   `json:"requirement_id,omitempty"`
	RequirementTitle      string   `json:"requirement_title,omitempty"`
	SuggestedStatus       string   `json:"suggested_status"`
	SuggestedProgress     int      `json:"suggested_progress"`
	EvidenceSessionIDs    []string `json:"evidence_session_ids"`
	EvidenceSessionTitles []string `json:"evidence_session_titles"`
	Reason                string   `json:"reason"`
}

type GenerateReportDraftResponse struct {
	ReportMarkdown          string                   `json:"report_markdown"`
	SelectedSessionIDs      []string                 `json:"selected_session_ids"`
	SkillName               string                   `json:"skill_name"`
	TaskProgressSuggestions []TaskProgressSuggestion `json:"task_progress_suggestions"`
	ManagedAgentRunID       string                   `json:"managed_agent_run_id"`
	AgentID                 string                   `json:"agent_id"`
	AgentVersionID          *int                     `json:"agent_version_id"`
	ModelID                 string                   `json:"model_id"`
	Status                  string                   `json:"status,omitempty"`
}

type ManagedScope string

const (
	ManagedScopeMine   ManagedScope = "mine"
	ManagedScopePublic ManagedScope = "public"
	ManagedScopeAll    ManagedScope = "all"
)

type ManagedSkill struct {
	SkillID     string `json:"skill_id"`
	Owner       string `json:"owner,omitempty"`
	Slug        string `json:"slug"`
	Version     string `json:"version"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	SizeBytes   int64  `json:"size_bytes,omitempty"`
	Archived    bool   `json:"archived"`
	CreatedAt   int64  `json:"created_at,omitempty"`
}

type ManagedMCPEntry struct {
	EntryID            string            `json:"entry_id,omitempty"`
	Owner              string            `json:"owner,omitempty"`
	Slug               string            `json:"slug"`
	Version            string            `json:"version"`
	Name               string            `json:"name"`
	Description        string            `json:"description,omitempty"`
	Transport          string            `json:"transport"`
	Command            string            `json:"command,omitempty"`
	Args               []string          `json:"args,omitempty"`
	URL                string            `json:"url,omitempty"`
	Headers            map[string]string `json:"headers,omitempty"`
	Env                map[string]string `json:"env,omitempty"`
	RequiresCredential bool              `json:"requires_credential"`
	CredentialEnv      string            `json:"credential_env,omitempty"`
	AuthScheme         string            `json:"auth_scheme,omitempty"`
	AuthHeader         string            `json:"auth_header,omitempty"`
	Archived           bool              `json:"archived"`
	CreatedAt          int64             `json:"created_at,omitempty"`
}

type ManagedSkillRef struct {
	Owner   string `json:"owner,omitempty"`
	Slug    string `json:"slug"`
	Version string `json:"version"`
}

type ManagedMCPBinding struct {
	Owner          string `json:"owner,omitempty"`
	Slug           string `json:"slug"`
	Version        string `json:"version"`
	CredentialSlot string `json:"credential_slot,omitempty"`
}

type ManagedMCPServer struct {
	Name           string            `json:"name"`
	URL            string            `json:"url"`
	CredentialSlot string            `json:"credential_slot,omitempty"`
	AuthHeader     string            `json:"auth_header,omitempty"`
	AuthScheme     string            `json:"auth_scheme,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
}

type ManagedCredentialSlot struct {
	Name     string `json:"name"`
	Required bool   `json:"required,omitempty"`
}

type ManagedCredential struct {
	CredentialID string            `json:"credential_id"`
	Name         string            `json:"name"`
	Kind         string            `json:"kind,omitempty"`
	Description  string            `json:"description,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	Archived     bool              `json:"archived"`
	CreatedAt    int64             `json:"created_at,omitempty"`
	UpdatedAt    int64             `json:"updated_at,omitempty"`
}

type ManagedAgent struct {
	AgentID             string                  `json:"agent_id"`
	Name                string                  `json:"name"`
	Description         string                  `json:"description,omitempty"`
	Engine              string                  `json:"engine"`
	Instructions        string                  `json:"instructions,omitempty"`
	DefaultModelID      string                  `json:"default_model_id,omitempty"`
	StartPromptTemplate string                  `json:"start_prompt_template,omitempty"`
	CredentialSlots     []ManagedCredentialSlot `json:"credential_slots,omitempty"`
	DefaultBindings     map[string]string       `json:"default_bindings,omitempty"`
	CurrentVersionID    int                     `json:"current_version_id,omitempty"`
	ManagedVersion      int                     `json:"managed_version,omitempty"`
	Archived            bool                    `json:"archived"`
	IsPublic            bool                    `json:"is_public"`
	Skills              []ManagedSkillRef       `json:"skills,omitempty"`
	MCPServers          []ManagedMCPServer      `json:"mcp_servers,omitempty"`
	MCPBindings         []ManagedMCPBinding     `json:"mcp_bindings,omitempty"`
	CreatedAt           int64                   `json:"created_at,omitempty"`
	BusinessType        string                  `json:"business_type,omitempty"`
	ReportTypes         []string                `json:"report_types,omitempty"`
	IsDefaultReport     bool                    `json:"is_default_report,omitempty"`
}

type ListManagedSkillsResponse struct {
	Skills []ManagedSkill `json:"skills"`
}

type ListManagedMCPEntriesResponse struct {
	Entries []ManagedMCPEntry `json:"entries"`
}

type ListManagedAgentsResponse struct {
	Agents []ManagedAgent `json:"agents"`
}

type ListManagedCredentialsResponse struct {
	Credentials []ManagedCredential `json:"credentials"`
}

type UpsertManagedAgentRequest struct {
	AgentID             string                  `json:"agent_id"`
	Name                string                  `json:"name"`
	Description         string                  `json:"description,omitempty"`
	Engine              string                  `json:"engine"`
	Instructions        string                  `json:"instructions,omitempty"`
	DefaultModelID      string                  `json:"default_model_id,omitempty"`
	StartPromptTemplate string                  `json:"start_prompt_template,omitempty"`
	CredentialSlots     []ManagedCredentialSlot `json:"credential_slots"`
	DefaultBindings     map[string]string       `json:"default_bindings"`
	Skills              []ManagedSkillRef       `json:"skills"`
	MCPServers          []ManagedMCPServer      `json:"mcp_servers"`
	MCPBindings         []ManagedMCPBinding     `json:"mcp_bindings"`
	BusinessType        string                  `json:"business_type,omitempty"`
	ReportTypes         []string                `json:"report_types,omitempty"`
}

type UpsertManagedAgentResponse struct {
	AgentID        string `json:"agent_id"`
	ManagedVersion int    `json:"managed_version,omitempty"`
}

type CreateManagedMCPEntryRequest = ManagedMCPEntry

type ManagedReportRunRequest struct {
	ReportType string   `json:"report_type,omitempty"`
	ReportDate string   `json:"report_date"`
	WeekStart  string   `json:"week_start,omitempty"`
	WeekEnd    string   `json:"week_end,omitempty"`
	SessionIDs []string `json:"session_ids"`
	AgentID    string   `json:"agent_id"`
	ModelID    string   `json:"model_id"`
}

type ManagedAgentManualRunRequest struct {
	Message             string            `json:"message"`
	ModelID             string            `json:"model_id"`
	Params              map[string]string `json:"params,omitempty"`
	CredentialOverrides map[string]string `json:"credential_overrides,omitempty"`
}

type ManagedReportRunPeriod struct {
	Date      string `json:"date,omitempty"`
	WeekStart string `json:"week_start,omitempty"`
	WeekEnd   string `json:"week_end,omitempty"`
}

type ManagedReportRunTarget struct {
	Type         string `json:"type,omitempty"`
	UserID       string `json:"user_id,omitempty"`
	TeamID       string `json:"team_id,omitempty"`
	DepartmentID string `json:"department_id,omitempty"`
}

type ManagedReportAgentRunRequest struct {
	ReportType               string                 `json:"report_type"`
	Period                   ManagedReportRunPeriod `json:"period"`
	Target                   ManagedReportRunTarget `json:"target,omitempty"`
	ModelID                  string                 `json:"model_id,omitempty"`
	SelectedSessionSliceKeys []string               `json:"selected_session_slice_keys,omitempty"`
	StartPromptValues        map[string]string      `json:"start_prompt_values,omitempty"`
	Message                  string                 `json:"message,omitempty"`
	CredentialOverrides      map[string]string      `json:"credential_overrides,omitempty"`
}

type ManagedAgentSchedule struct {
	ID                string            `json:"id"`
	UserID            string            `json:"user_id"`
	Name              string            `json:"name"`
	AgentID           string            `json:"agent_id"`
	RunKind           string            `json:"run_kind"`
	ModelID           *string           `json:"model_id"`
	InitialMessage    string            `json:"initial_message"`
	Message           string            `json:"message"`
	StartPromptValues map[string]string `json:"start_prompt_values,omitempty"`
	Params            map[string]string `json:"params,omitempty"`
	ReportConfig      map[string]string `json:"report_config,omitempty"`
	ScheduleType      string            `json:"schedule_type"`
	Weekdays          []int             `json:"weekdays"`
	TimeOfDay         string            `json:"time_of_day"`
	Timezone          string            `json:"timezone"`
	Enabled           bool              `json:"enabled"`
	NextRunAt         *time.Time        `json:"next_run_at,omitempty"`
	LastRunAt         *time.Time        `json:"last_run_at,omitempty"`
	LastAIRunID       *string           `json:"last_ai_run_id,omitempty"`
	LastRunStatus     *string           `json:"last_run_status,omitempty"`
	LastError         *string           `json:"last_error,omitempty"`
	LastSkipReason    *string           `json:"last_skip_reason,omitempty"`
	LastSkipAt        *time.Time        `json:"last_skip_at,omitempty"`
	LastSkippedAt     *time.Time        `json:"last_skipped_trigger_at,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

type ManagedAgentScheduleTriggerConfig struct {
	ScheduleType string `json:"schedule_type"`
	Weekdays     []int  `json:"weekdays,omitempty"`
	TimeOfDay    string `json:"time_of_day"`
}

type ManagedAgentScheduleReportConfig struct {
	ReportType string `json:"report_type"`
}

type ManagedAgentScheduleRunConfig struct {
	ModelID           string                            `json:"model_id"`
	InitialMessage    string                            `json:"initial_message"`
	StartPromptValues map[string]string                 `json:"start_prompt_values,omitempty"`
	ReportConfig      *ManagedAgentScheduleReportConfig `json:"report_config,omitempty"`
}

type UpsertManagedAgentScheduleRequest struct {
	Name              string                             `json:"name"`
	AgentID           string                             `json:"agent_id"`
	RunKind           string                             `json:"run_kind"`
	ModelID           string                             `json:"model_id"`
	InitialMessage    string                             `json:"initial_message"`
	Message           string                             `json:"message"`
	StartPromptValues map[string]string                  `json:"start_prompt_values,omitempty"`
	Params            map[string]string                  `json:"params,omitempty"`
	ReportConfig      *ManagedAgentScheduleReportConfig  `json:"report_config,omitempty"`
	TriggerConfig     *ManagedAgentScheduleTriggerConfig `json:"trigger_config,omitempty"`
	RunConfig         *ManagedAgentScheduleRunConfig     `json:"run_config,omitempty"`
	ScheduleType      string                             `json:"schedule_type"`
	Weekdays          []int                              `json:"weekdays,omitempty"`
	TimeOfDay         string                             `json:"time_of_day"`
	Timezone          string                             `json:"timezone,omitempty"`
	Enabled           *bool                              `json:"enabled,omitempty"`
}

type PreviewManagedAgentScheduleRequest struct {
	AgentID       string                             `json:"agent_id"`
	RunKind       string                             `json:"run_kind"`
	ScheduleType  string                             `json:"schedule_type"`
	Weekdays      []int                              `json:"weekdays,omitempty"`
	TimeOfDay     string                             `json:"time_of_day"`
	ReportType    string                             `json:"report_type,omitempty"`
	ReportConfig  *ManagedAgentScheduleReportConfig  `json:"report_config,omitempty"`
	TriggerConfig *ManagedAgentScheduleTriggerConfig `json:"trigger_config,omitempty"`
	RunConfig     *ManagedAgentScheduleRunConfig     `json:"run_config,omitempty"`
}

type PreviewManagedAgentScheduleResponse struct {
	NextRunAt                    time.Time `json:"next_run_at"`
	ScheduledTriggerAtForPreview time.Time `json:"scheduled_trigger_at_for_preview"`
	AgentType                    string    `json:"agent_type"`
	ReportType                   string    `json:"report_type,omitempty"`
	ReportTargetDisplay          string    `json:"report_target_display,omitempty"`
	PeriodStart                  string    `json:"period_start,omitempty"`
	PeriodEnd                    string    `json:"period_end,omitempty"`
	PeriodDisplay                string    `json:"period_display,omitempty"`
}

type AIRun struct {
	ID                string                       `json:"id"`
	UserID            string                       `json:"user_id"`
	BusinessType      string                       `json:"business_type"`
	BusinessID        *string                      `json:"business_id,omitempty"`
	RuntimeType       string                       `json:"runtime_type"`
	AgentID           string                       `json:"agent_id"`
	AgentVersionID    *int                         `json:"agent_version_id"`
	ExternalTaskID    *string                      `json:"external_task_id,omitempty"`
	ExternalSessionID *string                      `json:"external_session_id,omitempty"`
	ModelID           *string                      `json:"model_id"`
	Status            string                       `json:"status"`
	InputRef          map[string]any               `json:"input_ref_json,omitempty"`
	OutputRef         map[string]any               `json:"output_ref_json,omitempty"`
	Result            string                       `json:"result,omitempty"`
	ErrorMessage      *string                      `json:"error_message,omitempty"`
	Draft             *GenerateReportDraftResponse `json:"draft,omitempty"`
	StartedAt         *time.Time                   `json:"started_at,omitempty"`
	FinishedAt        *time.Time                   `json:"finished_at,omitempty"`
	CreatedAt         time.Time                    `json:"created_at"`
}

type DailyReportAgentIntegration struct {
	MCP struct {
		Name        string   `json:"name"`
		Slug        string   `json:"slug"`
		Version     string   `json:"version"`
		URL         string   `json:"url"`
		Transport   string   `json:"transport"`
		Status      string   `json:"status,omitempty"`
		Managed     bool     `json:"managed"`
		Description string   `json:"description"`
		Tools       []string `json:"tools"`
	} `json:"mcp"`
	Skill struct {
		Slug    string `json:"slug"`
		Version string `json:"version"`
		Name    string `json:"name"`
		Status  string `json:"status,omitempty"`
		Managed bool   `json:"managed"`
		SkillMD string `json:"skill_md"`
	} `json:"skill"`
}

type TeamReport struct {
	ID                   string     `json:"id"`
	TeamID               string     `json:"team_id"`
	TeamName             string     `json:"team_name"`
	LeaderID             string     `json:"leader_id"`
	LeaderName           string     `json:"leader_name"`
	ReportDate           string     `json:"report_date"`
	Content              string     `json:"content"`
	SubmittedContent     *string    `json:"submitted_content,omitempty"`
	Status               *string    `json:"status,omitempty"`
	FeishuDocURL         *string    `json:"feishu_doc_url,omitempty"`
	MemberReportIDs      []string   `json:"member_report_ids"`
	SourceDailyReportIDs []string   `json:"source_daily_report_ids"`
	SessionIDs           []string   `json:"session_ids"`
	GenerationMode       string     `json:"generation_mode,omitempty"`
	ManagedAgentRunID    *string    `json:"managed_agent_run_id"`
	AgentRunID           *string    `json:"agent_run_id,omitempty"`
	AgentID              *string    `json:"agent_id"`
	AgentVersionID       *int       `json:"agent_version_id"`
	ModelID              *string    `json:"model_id"`
	Edited               bool       `json:"edited"`
	GeneratedAt          *time.Time `json:"generated_at,omitempty"`
	ProductStatus        string     `json:"product_status,omitempty"`
	Origin               string     `json:"origin,omitempty"`
	UpdatedByUser        bool       `json:"updated_by_user"`
	SavedAt              *time.Time `json:"saved_at,omitempty"`
	SubmittedAt          *time.Time `json:"submitted_at,omitempty"`
	SubmittedTo          *string    `json:"submitted_to,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

type TeamReportListItem struct {
	ID             string     `json:"id"`
	TeamID         string     `json:"team_id"`
	TeamName       string     `json:"team_name"`
	LeaderID       string     `json:"leader_id"`
	LeaderName     string     `json:"leader_name"`
	ReportDate     string     `json:"report_date"`
	MemberCount    int        `json:"member_count"`
	SubmittedCount int        `json:"submitted_count"`
	MissingCount   int        `json:"missing_count"`
	Status         *string    `json:"status,omitempty"`
	SavedAt        *time.Time `json:"saved_at,omitempty"`
	SubmittedAt    *time.Time `json:"submitted_at,omitempty"`
	SubmittedTo    *string    `json:"submitted_to,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type PaginatedTeamReports struct {
	Items    []TeamReportListItem `json:"items"`
	Total    int                  `json:"total"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"page_size"`
}

type TeamMemberReport struct {
	UserID      string     `json:"user_id"`
	UserName    string     `json:"user_name"`
	ReportID    *string    `json:"report_id,omitempty"`
	Content     string     `json:"content"`
	SubmittedAt *time.Time `json:"submitted_at,omitempty"`
	HasReport   bool       `json:"has_report"`
}

type UpdateTeamReportRequest struct {
	Content      *string `json:"content,omitempty"`
	FeishuDocURL *string `json:"feishu_doc_url,omitempty"`
}

type SubmitTeamReportRequest struct {
	Content *string `json:"content,omitempty"`
}

type TeamReportSources struct {
	TeamID           string             `json:"team_id"`
	TeamName         string             `json:"team_name"`
	ReportDate       string             `json:"report_date"`
	Members          []TeamMemberReport `json:"members"`
	SubmittedReports []TeamMemberReport `json:"submitted_reports"`
	MissingMembers   []TeamMemberReport `json:"missing_members"`
	TotalMemberCount int                `json:"total_member_count"`
	Submitted        int                `json:"submitted"`
	SubmittedCount   int                `json:"submitted_count"`
	Missing          int                `json:"missing"`
	MissingCount     int                `json:"missing_count"`
}

type DepartmentTeamReportSource struct {
	TeamID         string     `json:"team_id"`
	TeamName       string     `json:"team_name"`
	LeaderID       *string    `json:"leader_id,omitempty"`
	LeaderName     string     `json:"leader_name"`
	TeamLeaderName string     `json:"team_leader_name"`
	ReportID       *string    `json:"report_id,omitempty"`
	TeamReportID   *string    `json:"team_report_id,omitempty"`
	Content        string     `json:"content"`
	SubmittedAt    *time.Time `json:"submitted_at,omitempty"`
	HasReport      bool       `json:"has_report"`
}

type DepartmentMissingTeam struct {
	TeamID   string `json:"team_id"`
	TeamName string `json:"team_name"`
}

type DepartmentReportSources struct {
	ReportDate           string                       `json:"report_date"`
	SubmittedTeamCount   int                          `json:"submitted_team_count"`
	TotalTeamCount       int                          `json:"total_team_count"`
	MissingTeamCount     int                          `json:"missing_team_count"`
	SubmittedTeamReports []DepartmentTeamReportSource `json:"submitted_team_reports"`
	MissingTeams         []DepartmentMissingTeam      `json:"missing_teams"`
}

type DepartmentReport struct {
	ID                  string     `json:"id"`
	ReportDate          string     `json:"report_date"`
	Content             string     `json:"content"`
	Status              *string    `json:"status,omitempty"`
	SourceTeamReportIDs []string   `json:"source_team_report_ids"`
	GenerationMode      string     `json:"generation_mode,omitempty"`
	ManagedAgentRunID   *string    `json:"managed_agent_run_id"`
	AgentRunID          *string    `json:"agent_run_id,omitempty"`
	AgentID             *string    `json:"agent_id"`
	AgentVersionID      *int       `json:"agent_version_id"`
	ModelID             *string    `json:"model_id"`
	Edited              bool       `json:"edited"`
	GeneratedAt         *time.Time `json:"generated_at,omitempty"`
	ProductStatus       string     `json:"product_status,omitempty"`
	Origin              string     `json:"origin,omitempty"`
	UpdatedByUser       bool       `json:"updated_by_user"`
	SavedAt             *time.Time `json:"saved_at,omitempty"`
	ArchivedAt          *time.Time `json:"archived_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type DepartmentReportListItem struct {
	ID                 string     `json:"id"`
	ReportDate         string     `json:"report_date"`
	TeamCount          int        `json:"team_count"`
	SubmittedTeamCount int        `json:"submitted_team_count"`
	MissingTeamCount   int        `json:"missing_team_count"`
	Status             *string    `json:"status,omitempty"`
	SavedAt            *time.Time `json:"saved_at,omitempty"`
	ArchivedAt         *time.Time `json:"archived_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type PaginatedDepartmentReports struct {
	Items    []DepartmentReportListItem `json:"items"`
	Total    int                        `json:"total"`
	Page     int                        `json:"page"`
	PageSize int                        `json:"page_size"`
}

type UpdateDepartmentReportRequest struct {
	Content *string `json:"content,omitempty"`
	Archive bool    `json:"archive,omitempty"`
}

type WeeklyDailyReportSource struct {
	ReportID   string `json:"report_id"`
	UserID     string `json:"user_id"`
	UserName   string `json:"user_name"`
	ReportDate string `json:"report_date"`
	Content    string `json:"content"`
}

type WeeklyTeamDailyReportSource struct {
	ReportID    string     `json:"report_id"`
	TeamID      string     `json:"team_id"`
	TeamName    string     `json:"team_name"`
	LeaderID    string     `json:"leader_id"`
	LeaderName  string     `json:"leader_name"`
	ReportDate  string     `json:"report_date"`
	Content     string     `json:"content"`
	SubmittedAt *time.Time `json:"submitted_at,omitempty"`
}

type WeeklyTaskSource struct {
	TaskID           string  `json:"task_id"`
	TaskTitle        string  `json:"task_title"`
	RequirementID    string  `json:"requirement_id"`
	RequirementTitle string  `json:"requirement_title"`
	Status           string  `json:"status"`
	Priority         string  `json:"priority"`
	DueDate          *string `json:"due_date,omitempty"`
}

type TeamWeeklyReportSources struct {
	TeamID                         string                        `json:"team_id"`
	TeamName                       string                        `json:"team_name"`
	WeekStart                      string                        `json:"week_start"`
	WeekEnd                        string                        `json:"week_end"`
	SubmittedPersonalWeeklyReports []TeamPersonalWeeklySource    `json:"submitted_personal_weekly_reports"`
	MissingPeople                  []TeamWeeklyMissingPerson     `json:"missing_people"`
	SubmittedPersonalWeeklyCount   int                           `json:"submitted_personal_weekly_count"`
	MissingPeopleCount             int                           `json:"missing_people_count"`
	DailyReports                   []WeeklyDailyReportSource     `json:"daily_reports,omitempty"`
	TeamReports                    []WeeklyTeamDailyReportSource `json:"team_reports,omitempty"`
	Tasks                          []WeeklyTaskSource            `json:"tasks,omitempty"`
	SubmittedDailyCount            int                           `json:"submitted_daily_count,omitempty"`
	TeamReportCount                int                           `json:"team_report_count,omitempty"`
	TaskCount                      int                           `json:"task_count,omitempty"`
}

type TeamPersonalWeeklySource struct {
	ReportID         string     `json:"report_id"`
	UserID           string     `json:"user_id"`
	UserName         string     `json:"user_name"`
	SourceRole       string     `json:"source_role"`
	WeekStart        string     `json:"week_start"`
	WeekEnd          string     `json:"week_end"`
	SubmittedAt      *time.Time `json:"submitted_at,omitempty"`
	SubmittedContent string     `json:"submitted_content"`
}

type TeamWeeklyMissingPerson struct {
	UserID     string `json:"user_id"`
	UserName   string `json:"user_name"`
	SourceRole string `json:"source_role"`
}

type TeamWeeklyReport struct {
	ID                            string     `json:"id"`
	TeamID                        string     `json:"team_id"`
	TeamName                      string     `json:"team_name"`
	LeaderID                      string     `json:"leader_id"`
	LeaderName                    string     `json:"leader_name"`
	WeekStart                     string     `json:"week_start"`
	Content                       string     `json:"content"`
	SourceDailyReportIDs          []string   `json:"source_daily_report_ids"`
	SourceTeamReportIDs           []string   `json:"source_team_report_ids"`
	SourceTaskIDs                 []string   `json:"source_task_ids"`
	SourcePersonalWeeklyReportIDs []string   `json:"source_personal_weekly_report_ids"`
	GenerationMode                string     `json:"generation_mode,omitempty"`
	ManagedAgentRunID             *string    `json:"managed_agent_run_id"`
	AgentRunID                    *string    `json:"agent_run_id,omitempty"`
	AgentID                       *string    `json:"agent_id"`
	AgentVersionID                *int       `json:"agent_version_id"`
	ModelID                       *string    `json:"model_id"`
	Edited                        bool       `json:"edited"`
	GeneratedAt                   *time.Time `json:"generated_at,omitempty"`
	ProductStatus                 string     `json:"product_status,omitempty"`
	Origin                        string     `json:"origin,omitempty"`
	UpdatedByUser                 bool       `json:"updated_by_user"`
	SubmittedAt                   *time.Time `json:"submitted_at,omitempty"`
	CreatedAt                     time.Time  `json:"created_at"`
	UpdatedAt                     time.Time  `json:"updated_at"`
}

type GenerateTeamWeeklyReportRequest struct {
	WeekStart                     string   `json:"week_start"`
	SourcePersonalWeeklyReportIDs []string `json:"source_personal_weekly_report_ids"`
}

type TeamWeeklyReportPreview struct {
	ReportMarkdown                string   `json:"report_markdown"`
	WeekStart                     string   `json:"week_start"`
	WeekEnd                       string   `json:"week_end"`
	SourcePersonalWeeklyReportIDs []string `json:"source_personal_weekly_report_ids"`
}

type UpdateTeamWeeklyReportRequest struct {
	WeekStart                     string   `json:"week_start,omitempty"`
	Content                       *string  `json:"content,omitempty"`
	SourcePersonalWeeklyReportIDs []string `json:"source_personal_weekly_report_ids,omitempty"`
}

type DepartmentTeamWeeklyReportSource struct {
	TeamID      string     `json:"team_id"`
	TeamName    string     `json:"team_name"`
	LeaderID    *string    `json:"leader_id,omitempty"`
	LeaderName  string     `json:"leader_name"`
	ReportID    *string    `json:"report_id,omitempty"`
	Content     string     `json:"content"`
	SubmittedAt *time.Time `json:"submitted_at,omitempty"`
	HasReport   bool       `json:"has_report"`
}

type DepartmentWeeklyReportSources struct {
	WeekStart            string                             `json:"week_start"`
	WeekEnd              string                             `json:"week_end"`
	SubmittedTeamCount   int                                `json:"submitted_team_count"`
	TotalTeamCount       int                                `json:"total_team_count"`
	SubmittedTeamReports []DepartmentTeamWeeklyReportSource `json:"submitted_team_reports"`
	MissingTeams         []DepartmentMissingTeam            `json:"missing_teams"`
}

type DepartmentWeeklyReport struct {
	ID                        string     `json:"id"`
	WeekStart                 string     `json:"week_start"`
	Content                   string     `json:"content"`
	SourceTeamWeeklyReportIDs []string   `json:"source_team_weekly_report_ids"`
	GenerationMode            string     `json:"generation_mode,omitempty"`
	ManagedAgentRunID         *string    `json:"managed_agent_run_id"`
	AgentRunID                *string    `json:"agent_run_id,omitempty"`
	AgentID                   *string    `json:"agent_id"`
	AgentVersionID            *int       `json:"agent_version_id"`
	ModelID                   *string    `json:"model_id"`
	Edited                    bool       `json:"edited"`
	GeneratedAt               *time.Time `json:"generated_at,omitempty"`
	ProductStatus             string     `json:"product_status,omitempty"`
	Origin                    string     `json:"origin,omitempty"`
	UpdatedByUser             bool       `json:"updated_by_user"`
	ArchivedAt                *time.Time `json:"archived_at,omitempty"`
	CreatedAt                 time.Time  `json:"created_at"`
	UpdatedAt                 time.Time  `json:"updated_at"`
}

type UpdateDepartmentWeeklyReportRequest struct {
	Content *string `json:"content,omitempty"`
	Archive bool    `json:"archive,omitempty"`
}

type ACStatus struct {
	Index       int      `json:"index"`
	Text        string   `json:"text"`
	Completed   bool     `json:"completed"`
	LinkedTasks []string `json:"linked_tasks"`
}

type Document struct {
	ID            string    `json:"id"`
	UserID        string    `json:"user_id"`
	UserName      string    `json:"user_name"`
	Title         string    `json:"title"`
	URL           string    `json:"url"`
	Description   *string   `json:"description,omitempty"`
	TaskID        *string   `json:"task_id,omitempty"`
	TaskTitle     *string   `json:"task_title,omitempty"`
	RequirementID *string   `json:"requirement_id,omitempty"`
	UploadedAt    time.Time `json:"uploaded_at"`
}

type CreateDocumentRequest struct {
	Title       string  `json:"title"`
	URL         string  `json:"url"`
	Description *string `json:"description,omitempty"`
	TaskID      *string `json:"task_id,omitempty"`
}

type UpdateDocumentRequest struct {
	Title       *string `json:"title,omitempty"`
	URL         *string `json:"url,omitempty"`
	Description *string `json:"description,omitempty"`
	TaskID      *string `json:"task_id,omitempty"`
}

type UpdateACRequest struct {
	AcceptanceCriteria []string `json:"acceptance_criteria"`
}

type TokenAggregation struct {
	Total            int64        `json:"total"`
	InputSum         int64        `json:"input_sum"`
	OutputSum        int64        `json:"output_sum"`
	CacheCreationSum int64        `json:"cache_creation_sum"`
	CacheReadSum     int64        `json:"cache_read_sum"`
	Groups           []TokenGroup `json:"groups"`
	Series           []TokenPoint `json:"series"`
	Period           string       `json:"period"`
	GroupBy          string       `json:"group_by"`
}

type SessionTokens struct {
	SessionID           string     `json:"session_id"`
	SliceKey            string     `json:"slice_key,omitempty"`
	LocalSessionID      string     `json:"local_session_id,omitempty"`
	SessionRef          string     `json:"session_ref"`
	UserID              string     `json:"user_id"`
	UserName            string     `json:"user_name"`
	AgentType           string     `json:"agent_type"`
	Models              []string   `json:"models"`
	Summary             *string    `json:"summary,omitempty"`
	StartedAt           time.Time  `json:"started_at"`
	ActivityDate        string     `json:"activity_date,omitempty"`
	ActivityStartAt     *time.Time `json:"activity_start_at,omitempty"`
	ActivityEndAt       *time.Time `json:"activity_end_at,omitempty"`
	ActivityDates       []string   `json:"activity_dates,omitempty"`
	SliceCount          int        `json:"slice_count"`
	SourceHasRawLog     bool       `json:"source_has_raw_log"`
	IsEstimated         bool       `json:"is_estimated"`
	TokenSliceStrategy  string     `json:"token_slice_strategy,omitempty"`
	InputTokens         int64      `json:"input_tokens"`
	OutputTokens        int64      `json:"output_tokens"`
	CacheCreationTokens int64      `json:"cache_creation_tokens"`
	CacheReadTokens     int64      `json:"cache_read_tokens"`
	TotalTokens         int64      `json:"total_tokens"`
}

type PaginatedSessionTokens struct {
	Items    []SessionTokens `json:"items"`
	Total    int             `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
}

type TokenGroup struct {
	Key     string  `json:"key"`
	Label   string  `json:"label"`
	Value   int64   `json:"value"`
	Percent float64 `json:"percent"`
}

type TokenPoint struct {
	Date  string `json:"date"`
	Value int64  `json:"value"`
}

type TeamActivity struct {
	Teams        []TeamStat    `json:"teams"`
	IdleWarnings []IdleWarning `json:"idle_warnings"`
}

type TeamStat struct {
	TeamID   string       `json:"team_id"`
	TeamName string       `json:"team_name"`
	Active   int          `json:"active"`
	Total    int          `json:"total"`
	Members  []MemberStat `json:"members"`
}

type MemberStat struct {
	UserID     string  `json:"user_id"`
	UserName   string  `json:"user_name"`
	Active     bool    `json:"active"`
	LastActive *string `json:"last_active,omitempty"`
	IdleDays   int     `json:"idle_days"`
}

type IdleWarning struct {
	UserID   string `json:"user_id"`
	UserName string `json:"user_name"`
	TeamName string `json:"team_name"`
	IdleDays int    `json:"idle_days"`
}
