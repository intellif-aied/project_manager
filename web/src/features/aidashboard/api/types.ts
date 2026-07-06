// AIDashboard domain types — ported from web.legacy/src/lib/types.ts

export interface Team {
  id: string;
  name: string;
  director_user_id?: string | null;
  director_name?: string | null;
}

export type AIHubAidaStatus = "not_added" | "active" | "disabled";

export interface AIHubUser {
  id: number;
  username: string;
  nickname: string;
  email: string;
  status?: number;
  aida_status?: AIHubAidaStatus;
  aida_status_label?: string;
  current_app_role?: string | null;
  current_team_id?: string | null;
  current_team_name?: string | null;
}

export interface AdminBatchAddUsersResponse {
  created: number;
  skipped: number;
  skipped_existing?: number;
  failed: number;
  results: Array<{
    id: string;
    username?: string;
    nickname?: string;
    email?: string;
    status: "created" | "skipped" | "failed";
    error?: string;
  }>;
}

export type RequirementStatus = "todo" | "review" | "active" | "completed" | "cancelled";
export type RequirementPriority = "low" | "medium" | "high" | "urgent";
export type TaskStatus = "todo" | "in_progress" | "done";
export type TaskDisplayStatus = TaskStatus | "blocked";
export type StoredTaskStatus = TaskStatus;
export type TaskPriority = "low" | "medium" | "high";

export interface RequirementTaskSummaryDTO {
  total: number;
  done: number;
  blocked: number;
}

export interface RequirementRiskSummaryDTO {
  blocked: number;
  overdue: number;
  requirement_overdue?: number;
  dependency_conflict?: number;
}

export type AttentionLevel = "normal" | "notable" | "important" | "high";

export interface ResponsibleUserDTO {
  id: string;
  name: string;
  role: string;
  team_id?: string;
  team_name?: string;
}

export interface RequirementFollowSummaryDTO {
  count: number;
  score: number;
  level: AttentionLevel;
}

export interface Requirement {
  id: string;
  title: string;
  description: string;
  feishu_doc_url?: string;
  acceptance_criteria: string[];
  creator_id: string;
  creator_name: string;
  creator_role: string;
  responsible_user_ids?: string[];
  responsible_users?: ResponsibleUserDTO[];
  status: RequirementStatus;
  priority: RequirementPriority;
  progress: number;
  deadline?: string;
  team_ids: string[];
  team_names: string[];
  token_source_ids: string[];
  dependencies?: TaskDep[];
  blocking?: TaskDep[];
  task_summary: RequirementTaskSummaryDTO;
  risk_summary: RequirementRiskSummaryDTO;
  follow_summary?: RequirementFollowSummaryDTO;
  is_followed: boolean;
  can_update?: boolean;
  can_change_status?: boolean;
  can_cancel?: boolean;
  can_restore?: boolean;
  can_delete?: boolean;
  can_manage_ac?: boolean;
  can_create_task?: boolean;
  completed_at?: string;
  created_at: string;
  updated_at: string;
  version: number;
}

export interface PaginatedRequirements {
  items: Requirement[];
  total: number;
  page: number;
  page_size: number;
}

export interface RequirementBoardColumnDTO {
  status: RequirementStatus;
  items: Requirement[];
  total: number;
  page: number;
  page_size: number;
  has_more: boolean;
}

export interface RequirementBoardResponseDTO {
  columns: RequirementBoardColumnDTO[];
  total: number;
  column_page_size: number;
}

export interface ACStatus {
  index: number;
  text: string;
  completed: boolean;
  linked_tasks?: string[];
}

export interface TaskDep {
  item_type?: "requirement" | "task";
  item_id?: string;
  title?: string;
  task_id?: string;
  task_title?: string;
  requirement_id?: string;
  requirement_title?: string;
  status: RequirementStatus | TaskDisplayStatus | string;
  due_date?: string;
}

export interface Task {
  id: string;
  requirement_id: string;
  requirement_title?: string;
  title: string;
  acceptance_criteria: string[];
  creator_id: string;
  creator_name: string;
  responsible_user_ids?: string[];
  responsible_users?: ResponsibleUserDTO[];
  status: TaskStatus;
  display_status: TaskDisplayStatus;
  priority: TaskPriority;
  progress: number;
  due_date?: string;
  dependencies?: TaskDep[];
  blocking?: TaskDep[];
  risk_types: Array<"blocked" | "overdue" | "dependency_conflict">;
  token_source_ids: string[];
  follow_summary?: RequirementFollowSummaryDTO;
  is_followed: boolean;
  can_update_meta?: boolean;
  can_reassign?: boolean;
  can_update_status?: boolean;
  can_update_progress?: boolean;
  can_manage_dependencies?: boolean;
  can_delete?: boolean;
  completed_at?: string;
  created_at: string;
  updated_at: string;
  version: number;
}

export interface PaginatedTasks {
  items: Task[];
  total: number;
  page: number;
  page_size: number;
}

export type RequirementListItemDTO = Requirement;
export type RequirementDetailDTO = Requirement;
export type RequirementTaskDTO = Task;
export type TaskDependencyDTO = TaskDep;

export type FollowTargetType = "requirement" | "task";

export interface RequirementFollowStateDTO {
  user_id: string;
  target_type: FollowTargetType;
  target_id: string;
  created_at: string;
}

export interface WorkItemEventDTO {
  id: string;
  target_type: "requirement" | "task";
  target_id: string;
  requirement_id?: string;
  task_id?: string;
  actor_id?: string;
  actor_name: string;
  actor_role: string;
  event_type: string;
  event_title: string;
  before_data: Record<string, unknown>;
  after_data: Record<string, unknown>;
  metadata: Record<string, unknown>;
  created_at: string;
}

export interface PaginatedWorkItemEvents {
  items: WorkItemEventDTO[];
  total: number;
  page: number;
  page_size: number;
}

export interface DashboardNavigationTargetDTO {
  requirementId: string;
  taskId?: string;
  url: string;
}

export interface DashboardFollowFollowerDTO {
  id: string;
  name: string;
  role: string;
  teamId?: string;
  teamName?: string;
  followedAt: string;
}

export interface PaginatedFollowFollowers {
  items: DashboardFollowFollowerDTO[];
  total: number;
  page: number;
  page_size: number;
}

export interface DashboardFollowItemDTO {
  key: string;
  type: "需求" | "任务";
  title: string;
  requirement?: string;
  requirementId: string;
  taskId?: string;
  creatorId?: string;
  creatorName?: string;
  requirementResponsibleIds?: string[];
  requirementResponsibleNames?: string[];
  taskResponsibleIds?: string[];
  taskResponsibleNames?: string[];
  responsibleNames?: string[];
  status: string;
  deadline: string;
  risk: string;
  dependency?: string;
  activity?: string;
  followedByMe: boolean;
  createdByMe: boolean;
  assignedToMe: boolean;
  attentionScore: number;
  attentionLevel: AttentionLevel;
  followerCount: number;
  riskPriority: number;
  navigation: DashboardNavigationTargetDTO;
}

export interface PaginatedDashboardFollowItems {
  items: DashboardFollowItemDTO[];
  total: number;
  page: number;
  page_size: number;
}

export type DashboardRiskType =
  | "requirement_overdue"
  | "deadline"
  | "dependency_blocker"
  | "dependency_conflict";

export interface DashboardRiskTaskSummaryDTO {
  taskId: string;
  title: string;
  deadline?: string;
  riskTypes: DashboardRiskType[];
  unfinishedDependencyCount?: number;
}

export interface DashboardRiskGroupDTO {
  key: string;
  displayType: "requirement_group" | "single_task";
  requirementId: string;
  requirementTitle: string;
  riskTypes: DashboardRiskType[];
  requirementOverdue: boolean;
  deadlineTaskCount: number;
  dependencyBlockerCount: number;
  dependencyConflictCount: number;
  representativeTask?: DashboardRiskTaskSummaryDTO;
  summary: string;
  deadline: string;
  level: "高" | "中" | "低";
  tone: "red" | "orange" | "gold" | "blue";
  attentionScore: number;
  attentionLevel: AttentionLevel;
  actionText: string;
  targetUrl: string;
  navigation?: DashboardNavigationTargetDTO;
}

export interface PaginatedDashboardRiskGroups {
  items: DashboardRiskGroupDTO[];
  total: number;
  page: number;
  page_size: number;
}

export interface Session {
  id: string;
  session_ref: string;
  user_id: string;
  user_name: string;
  agent_type: string;
  started_at: string;
  ended_at?: string;
  duration_secs?: number;
  model: string;
  summary?: string;
  tool_calls_json?: Record<string, number>;
  git_commits?: string[];
  task_id?: string;
  task_title?: string;
  requirement_id?: string;
  match_confidence?: number;
  raw_log_url?: string;
  uploaded_at: string;
}

export interface PaginatedSessions {
  items: Session[];
  total: number;
  page: number;
  page_size: number;
}

export interface DailyReport {
  id: string;
  user_id: string;
  user_name: string;
  report_date: string;
  content: string;
  submitted_content?: string;
  status?: "saved" | "submitted" | null;
  submitted_to?: "team_leader" | "director" | null;
  edited: boolean;
  feishu_doc_url?: string;
  session_ids: string[];
  generation_mode?: "default" | "managed_agent";
  managed_agent_run_id?: string;
  agent_run_id?: string;
  agent_id?: string;
  agent_version_id?: number;
  model_id?: string;
  origin?: "ai" | "manual";
  updated_by_user?: boolean;
  generated_at?: string;
  product_status?: "missing" | "ai_generated" | "modified" | "manual" | "generation_failed";
  saved_at?: string;
  submitted_at?: string;
  created_at: string;
  updated_at: string;
}

export interface DailyReportListItem {
  id: string;
  user_id: string;
  user_name: string;
  report_date: string;
  status?: "saved" | "submitted" | null;
  submitted_to?: "team_leader" | "director" | null;
  edited: boolean;
  source_session_count: number;
  session_ids: string[];
  saved_at?: string;
  submitted_at?: string;
  created_at: string;
  updated_at: string;
}

export interface PaginatedDailyReports {
  items: DailyReportListItem[];
  total: number;
  page: number;
  page_size: number;
}

export interface GenerateReportDraftPayload {
  report_date: string;
  session_ids: string[];
  skill_id: "default_daily";
  skill_content?: string;
  include_task_progress: boolean;
}

export interface TaskProgressSuggestion {
  task_id: string;
  task_title: string;
  requirement_id?: string;
  requirement_title?: string;
  suggested_status: "todo" | "in_progress" | "done";
  suggested_progress: number;
  evidence_session_ids: string[];
  evidence_session_titles: string[];
  reason: string;
}

export interface GenerateReportDraftResponse {
  report_markdown: string;
  selected_session_ids: string[];
  skill_name: string;
  task_progress_suggestions: TaskProgressSuggestion[];
  managed_agent_run_id?: string;
  agent_id?: string;
  agent_version_id?: number;
  model_id?: string;
  status?: string;
}

export interface ManagedSkillRef {
  owner?: string;
  slug: string;
  version: string;
}

export interface ManagedMCPBinding {
  owner?: string;
  slug: string;
  version: string;
  credential_slot?: string;
}

export interface ManagedCredentialSlot {
  name: string;
  required?: boolean;
}

export interface ManagedCredential {
  credential_id: string;
  name: string;
  kind?: string;
  description?: string;
  metadata?: Record<string, string>;
  archived: boolean;
  created_at?: number;
  updated_at?: number;
}

export interface ManagedSkill {
  skill_id: string;
  owner?: string;
  slug: string;
  version: string;
  name: string;
  description?: string;
  sha256?: string;
  size_bytes?: number;
  archived: boolean;
  created_at?: number;
}

export interface CreateManagedSkillPayload {
  slug: string;
  version: string;
  name?: string;
  description?: string;
  skill_md: string;
}

export interface ManagedMCPEntry {
  entry_id?: string;
  owner?: string;
  slug: string;
  version: string;
  name: string;
  description?: string;
  transport: string;
  command?: string;
  args?: string[];
  url?: string;
  headers?: Record<string, string>;
  env?: Record<string, string>;
  requires_credential: boolean;
  credential_env?: string;
  auth_scheme?: string;
  auth_header?: string;
  archived: boolean;
  created_at?: number;
}

export interface ManagedAgent {
  agent_id: string;
  name: string;
  description?: string;
  engine: string;
  instructions?: string;
  default_model_id?: string;
  start_prompt_template?: string;
  current_version_id?: number;
  managed_version?: number;
  archived: boolean;
  is_public: boolean;
  credential_slots?: ManagedCredentialSlot[];
  default_bindings?: Record<string, string>;
  skills?: ManagedSkillRef[];
  mcp_servers?: Array<{
    name: string;
    url: string;
    credential_slot?: string;
    auth_header?: string;
    auth_scheme?: string;
  }>;
  mcp_bindings?: ManagedMCPBinding[];
  business_type?: "generic" | "report";
  report_types?: ReportType[];
  is_default_report?: boolean;
}

export interface UpsertManagedAgentPayload {
  agent_id?: string;
  name: string;
  description?: string;
  engine: string;
  instructions?: string;
  default_model_id?: string;
  start_prompt_template?: string;
  credential_slots?: ManagedCredentialSlot[];
  default_bindings?: Record<string, string>;
  skills?: ManagedSkillRef[];
  mcp_servers?: Array<{
    name: string;
    url: string;
    credential_slot?: string;
    auth_header?: string;
    auth_scheme?: string;
  }>;
  mcp_bindings?: ManagedMCPBinding[];
  business_type?: "generic" | "report";
  report_types?: ReportType[];
}

export interface ManagedAgentManualRunPayload {
  message: string;
  model_id?: string;
  params?: Record<string, string>;
  credential_overrides?: Record<string, string>;
}

export type ReportType =
  | "personal_daily"
  | "personal_weekly"
  | "team_daily"
  | "team_weekly"
  | "department_daily"
  | "department_weekly";

export interface ManagedReportAgentRunPayload {
  report_type: ReportType;
  period: {
    date?: string;
    week_start?: string;
    week_end?: string;
  };
  target?: {
    type?: "self" | "user" | "team" | "department";
    user_id?: string;
    team_id?: string;
    department_id?: string;
  };
  model_id?: string;
  selected_session_slice_keys?: string[];
  start_prompt_values?: Record<string, string>;
  message?: string;
  credential_overrides?: Record<string, string>;
}

export interface ManagedReportAgentUnavailable {
  available: false;
  code: "DEFAULT_REPORT_AGENT_NOT_CONFIGURED";
  message: string;
}

export type ManagedReportAgentRunResponse = AIRun | ManagedReportAgentUnavailable;

export interface ManagedAgentSchedule {
  id: string;
  user_id: string;
  name: string;
  agent_id: string;
  run_kind: "generic_agent" | "report_agent";
  model_id?: string;
  initial_message?: string;
  message: string;
  start_prompt_values?: Record<string, string>;
  params?: Record<string, string>;
  report_config?: {
    report_type?: ReportType;
  };
  schedule_type: "daily" | "weekly";
  weekdays: number[];
  time_of_day: string;
  timezone: string;
  enabled: boolean;
  next_run_at?: string;
  last_run_at?: string;
  last_ai_run_id?: string;
  last_run_status?: "pending" | "running" | "succeeded" | "failed" | "timeout";
  last_error?: string;
  last_skip_reason?: string;
  last_skip_at?: string;
  last_skipped_trigger_at?: string;
  created_at: string;
  updated_at: string;
}

export interface UpsertManagedAgentSchedulePayload {
  name: string;
  agent_id: string;
  run_kind: "generic_agent" | "report_agent";
  enabled?: boolean;
  trigger_config: {
    schedule_type: "daily" | "weekly";
    weekdays?: number[];
    time_of_day: string;
  };
  run_config: {
    model_id?: string;
    initial_message?: string;
    start_prompt_values?: Record<string, string>;
    report_config?: {
      report_type: ReportType;
    };
  };
}

export interface PreviewManagedAgentSchedulePayload {
  agent_id?: string;
  run_kind: "generic_agent" | "report_agent";
  schedule_type: "daily" | "weekly";
  weekdays?: number[];
  time_of_day: string;
  report_type?: ReportType;
}

export interface PreviewManagedAgentScheduleResponse {
  next_run_at: string;
  scheduled_trigger_at_for_preview: string;
  agent_type: "generic" | "report";
  report_type?: ReportType;
  report_target_display?: string;
  period_start?: string;
  period_end?: string;
  period_display?: string;
}

export interface DailyReportAgentIntegration {
  mcp: {
    name: string;
    slug: string;
    version: string;
    url: string;
    transport: "http";
    status?: string;
    managed: boolean;
    description: string;
    tools: string[];
  };
  skill: {
    slug: string;
    version: string;
    name: string;
    status?: string;
    managed: boolean;
    skill_md: string;
  };
}

export interface AIRun {
  id: string;
  user_id: string;
  business_type: string;
  business_id?: string;
  runtime_type: string;
  agent_id: string;
  agent_version_id?: number;
  external_task_id?: string;
  external_session_id?: string;
  model_id?: string;
  status: "pending" | "running" | "succeeded" | "failed" | "timeout";
  input_ref_json?: Record<string, unknown>;
  output_ref_json?: Record<string, unknown>;
  result?: string;
  error_message?: string;
  draft?: GenerateReportDraftResponse;
  started_at?: string;
  finished_at?: string;
  created_at: string;
}

export interface TeamReport {
  id: string;
  team_id: string;
  team_name: string;
  leader_id: string;
  leader_name: string;
  report_date: string;
  content: string;
  submitted_content?: string;
  status?: "saved" | "submitted" | null;
  feishu_doc_url?: string;
  member_report_ids: string[];
  source_daily_report_ids: string[];
  session_ids: string[];
  edited: boolean;
  generation_mode?: "default" | "managed_agent";
  managed_agent_run_id?: string;
  agent_run_id?: string;
  agent_id?: string;
  agent_version_id?: number;
  model_id?: string;
  origin?: "ai" | "manual";
  updated_by_user?: boolean;
  generated_at?: string;
  product_status?: "missing" | "ai_generated" | "modified" | "manual" | "generation_failed";
  saved_at?: string;
  submitted_at?: string;
  submitted_to?: "director" | null;
  created_at: string;
  updated_at: string;
}

export interface TeamReportListItem {
  id: string;
  team_id: string;
  team_name: string;
  leader_id: string;
  leader_name: string;
  report_date: string;
  member_count: number;
  submitted_count: number;
  missing_count: number;
  status?: "saved" | "submitted" | null;
  saved_at?: string;
  submitted_at?: string;
  submitted_to?: "director" | null;
  created_at: string;
  updated_at: string;
}

export interface PaginatedTeamReports {
  items: TeamReportListItem[];
  total: number;
  page: number;
  page_size: number;
}

export interface TeamMemberReport {
  user_id: string;
  user_name: string;
  report_id?: string;
  content: string;
  submitted_at?: string;
  has_report: boolean;
}

export interface TeamReportSources {
  team_id: string;
  team_name: string;
  report_date: string;
  members: TeamMemberReport[];
  submitted_reports?: TeamMemberReport[];
  missing_members?: TeamMemberReport[];
  total_member_count?: number;
  submitted: number;
  submitted_count?: number;
  missing: number;
  missing_count?: number;
}

export interface DepartmentTeamReportSource {
  team_id: string;
  team_name: string;
  leader_id?: string;
  leader_name: string;
  team_leader_name: string;
  report_id?: string;
  team_report_id?: string;
  content: string;
  submitted_at?: string;
  has_report: boolean;
}

export interface DepartmentMissingTeam {
  team_id: string;
  team_name: string;
}

export interface DepartmentReportSources {
  report_date: string;
  submitted_team_count: number;
  total_team_count: number;
  missing_team_count?: number;
  submitted_team_reports: DepartmentTeamReportSource[];
  missing_teams: DepartmentMissingTeam[];
}

export interface DepartmentReport {
  id: string;
  report_date: string;
  content: string;
  status?: "saved" | "archived" | null;
  source_team_report_ids: string[];
  edited: boolean;
  generation_mode?: "default" | "managed_agent";
  managed_agent_run_id?: string;
  agent_run_id?: string;
  agent_id?: string;
  agent_version_id?: number;
  model_id?: string;
  origin?: "ai" | "manual";
  updated_by_user?: boolean;
  generated_at?: string;
  product_status?: "missing" | "ai_generated" | "modified" | "manual" | "generation_failed";
  saved_at?: string;
  archived_at?: string;
  created_at: string;
  updated_at: string;
}

export interface DepartmentReportListItem {
  id: string;
  report_date: string;
  team_count: number;
  submitted_team_count: number;
  missing_team_count: number;
  status?: "saved" | "archived" | null;
  saved_at?: string;
  archived_at?: string;
  created_at: string;
  updated_at: string;
}

export interface PaginatedDepartmentReports {
  items: DepartmentReportListItem[];
  total: number;
  page: number;
  page_size: number;
}

export interface WeeklySessionSource {
  session_id: string;
  session_ref: string;
  agent_type: string;
  started_at: string;
  ended_at?: string;
  summary: string;
  task_id?: string;
  task_title?: string;
  requirement_id?: string;
  requirement_title?: string;
  total_tokens: number;
}

export interface WeeklyDailyReportSource {
  report_id: string;
  user_id: string;
  user_name: string;
  report_date: string;
  content: string;
}

export interface WeeklyTeamDailyReportSource {
  report_id: string;
  team_id: string;
  team_name: string;
  leader_id: string;
  leader_name: string;
  report_date: string;
  content: string;
  submitted_at?: string;
}

export interface WeeklyTaskSource {
  task_id: string;
  task_title: string;
  requirement_id: string;
  requirement_title: string;
  responsible_user_ids?: string[];
  responsible_names?: string[];
  status: string;
  priority: string;
  due_date?: string;
}

export interface PersonalWeeklyReportSources {
  user_id: string;
  user_name: string;
  week_start: string;
  week_end: string;
  daily_reports: WeeklyDailyReportSource[];
  daily_count: number;
}

export interface PersonalWeeklyReport {
  id: string;
  user_id: string;
  user_name: string;
  week_start: string;
  week_end: string;
  content: string;
  submitted_content?: string;
  status: "saved" | "submitted";
  saved_at?: string;
  submitted_at?: string;
  submitted_to?: "team_leader" | "director";
  source_daily_report_ids: string[];
  source_session_ids: string[];
  source_task_ids: string[];
  edited: boolean;
  generation_mode?: "default" | "managed_agent";
  managed_agent_run_id?: string;
  agent_run_id?: string;
  agent_id?: string;
  agent_version_id?: number;
  model_id?: string;
  origin?: "ai" | "manual";
  updated_by_user?: boolean;
  generated_at?: string;
  product_status?: "missing" | "ai_generated" | "modified" | "manual" | "generation_failed";
  created_at: string;
  updated_at: string;
}

export interface PersonalWeeklyReportListItem {
  id: string;
  user_id: string;
  user_name: string;
  week_start: string;
  week_end: string;
  status: "saved" | "submitted";
  saved_at?: string;
  submitted_at?: string;
  submitted_to?: "team_leader" | "director";
  source_daily_count: number;
  source_session_count: number;
  source_task_count: number;
  created_at: string;
  updated_at: string;
}

export interface PaginatedPersonalWeeklyReports {
  items: PersonalWeeklyReportListItem[];
  total: number;
  page: number;
  page_size: number;
}

export interface PersonalWeeklyReportPreview {
  report_markdown: string;
  week_start: string;
  week_end: string;
  source_daily_report_ids: string[];
}

export interface TeamWeeklyReportSources {
  team_id: string;
  team_name: string;
  week_start: string;
  week_end: string;
  submitted_personal_weekly_reports: TeamPersonalWeeklySource[];
  missing_people: TeamWeeklyMissingPerson[];
  submitted_personal_weekly_count: number;
  missing_people_count: number;
  daily_reports?: WeeklyDailyReportSource[];
  team_reports?: WeeklyTeamDailyReportSource[];
  tasks?: WeeklyTaskSource[];
  submitted_daily_count?: number;
  team_report_count?: number;
  task_count?: number;
}

export interface TeamPersonalWeeklySource {
  report_id: string;
  user_id: string;
  user_name: string;
  source_role: "leader" | "member";
  week_start: string;
  week_end: string;
  submitted_at?: string;
  submitted_content: string;
}

export interface TeamWeeklyMissingPerson {
  user_id: string;
  user_name: string;
  source_role: "leader" | "member";
}

export interface TeamWeeklyReport {
  id: string;
  team_id: string;
  team_name: string;
  leader_id: string;
  leader_name: string;
  week_start: string;
  content: string;
  source_daily_report_ids: string[];
  source_team_report_ids: string[];
  source_task_ids: string[];
  source_personal_weekly_report_ids: string[];
  edited: boolean;
  generation_mode?: "default" | "managed_agent";
  managed_agent_run_id?: string;
  agent_run_id?: string;
  agent_id?: string;
  agent_version_id?: number;
  model_id?: string;
  origin?: "ai" | "manual";
  updated_by_user?: boolean;
  generated_at?: string;
  product_status?: "missing" | "ai_generated" | "modified" | "manual" | "generation_failed";
  submitted_at?: string;
  created_at: string;
  updated_at: string;
}

export interface TeamWeeklyReportPreview {
  report_markdown: string;
  week_start: string;
  week_end: string;
  source_personal_weekly_report_ids: string[];
}

export interface DepartmentTeamWeeklyReportSource {
  team_id: string;
  team_name: string;
  leader_id?: string;
  leader_name: string;
  report_id?: string;
  content: string;
  submitted_at?: string;
  has_report: boolean;
}

export interface DepartmentWeeklyReportSources {
  week_start: string;
  week_end: string;
  submitted_team_count: number;
  total_team_count: number;
  submitted_team_reports: DepartmentTeamWeeklyReportSource[];
  missing_teams: DepartmentMissingTeam[];
}

export interface DepartmentWeeklyReport {
  id: string;
  week_start: string;
  content: string;
  source_team_weekly_report_ids: string[];
  edited: boolean;
  generation_mode?: "default" | "managed_agent";
  managed_agent_run_id?: string;
  agent_run_id?: string;
  agent_id?: string;
  agent_version_id?: number;
  model_id?: string;
  origin?: "ai" | "manual";
  updated_by_user?: boolean;
  generated_at?: string;
  product_status?: "missing" | "ai_generated" | "modified" | "manual" | "generation_failed";
  archived_at?: string;
  created_at: string;
  updated_at: string;
}

export interface Document {
  id: string;
  user_id: string;
  user_name: string;
  title: string;
  url: string;
  description?: string;
  task_id?: string;
  task_title?: string;
  requirement_id?: string;
  uploaded_at: string;
}

export interface TokenGroup {
  key: string;
  label: string;
  value: number;
  percent: number;
}

export interface TokenPoint {
  date: string;
  value: number;
}

export interface TokenAggregation {
  total: number;
  input_sum: number;
  output_sum: number;
  cache_creation_sum?: number;
  cache_read_sum?: number;
  groups: TokenGroup[];
  series: TokenPoint[];
  period: string;
  group_by: string;
}

export interface SessionTokens {
  session_id: string;
  slice_key?: string;
  local_session_id?: string;
  session_ref: string;
  user_id: string;
  user_name: string;
  agent_type: string;
  models: string[];
  summary?: string;
  started_at: string;
  activity_date?: string;
  activity_start_at?: string;
  activity_end_at?: string;
  activity_dates?: string[];
  slice_count?: number;
  source_has_raw_log?: boolean;
  is_estimated?: boolean;
  token_slice_strategy?: string;
  input_tokens: number;
  output_tokens: number;
  cache_creation_tokens: number;
  cache_read_tokens: number;
  total_tokens: number;
}

export interface PaginatedSessionTokens {
  items: SessionTokens[];
  total: number;
  page: number;
  page_size: number;
}

export interface MemberStat {
  user_id: string;
  user_name: string;
  active: boolean;
  last_active?: string;
  idle_days: number;
}

export interface TeamStat {
  team_id: string;
  team_name: string;
  active: number;
  total: number;
  members: MemberStat[];
}

export interface IdleWarning {
  user_id: string;
  user_name: string;
  team_name: string;
  idle_days: number;
}

export interface TeamActivity {
  teams: TeamStat[];
  idle_warnings: IdleWarning[];
}

export type TokenPeriod = "today" | "week" | "month" | "range";
export type TokenGroupBy = "team" | "user" | "requirement" | "task" | "model";
