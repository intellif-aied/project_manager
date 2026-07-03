import { runtimeConfig } from "@/config/runtimeConfig";
import { api } from "@/shared/request/httpClient";
import { getAuthSession } from "@/shared/auth/session";
import { HttpError } from "@/shared/request/types";
import type { User } from "@/shared/auth/types";
import type {
  AIHubUser,
  AdminBatchAddUsersResponse,
  ACStatus,
  AIRun,
  DashboardFollowFollowerDTO,
  DashboardFollowItemDTO,
  DashboardRiskGroupDTO,
  DailyReport,
  DailyReportAgentIntegration,
  DepartmentReport,
  DepartmentReportSources,
  DepartmentWeeklyReport,
  DepartmentWeeklyReportSources,
  Document,
  GenerateReportDraftPayload,
  GenerateReportDraftResponse,
  CreateManagedSkillPayload,
  ManagedAgent,
  ManagedAgentManualRunPayload,
  ManagedReportAgentRunPayload,
  ManagedReportAgentRunResponse,
  ManagedAgentSchedule,
  ManagedCredential,
  ManagedMCPEntry,
  ManagedSkill,
  PaginatedDailyReports,
  PaginatedDepartmentReports,
  PaginatedPersonalWeeklyReports,
  PaginatedSessions,
  PaginatedSessionTokens,
  PaginatedTeamReports,
  PersonalWeeklyReport,
  PersonalWeeklyReportPreview,
  PersonalWeeklyReportSources,
  Requirement,
  RequirementFollowStateDTO,
  Session,
  Task,
  TeamActivity,
  TeamMemberReport,
  TeamReport,
  TeamReportSources,
  TeamWeeklyReport,
  TeamWeeklyReportPreview,
  TeamWeeklyReportSources,
  TokenAggregation,
  TokenGroupBy,
  TokenPeriod,
  Team,
  PreviewManagedAgentSchedulePayload,
  PreviewManagedAgentScheduleResponse,
  UpsertManagedAgentPayload,
  UpsertManagedAgentSchedulePayload
} from "./types";

async function unwrap<T>(promise: Promise<{ data: T }>): Promise<T> {
  const res = await promise;
  return res.data;
}

function trimTrailingSlash(value: string) {
  return value.replace(/\/+$/, "");
}

// ───────────────────────── Users / Teams ─────────────────────────

export const fetchUsers = async () => {
  const payload = await unwrap<User[] | { items?: User[] }>(api.get("/users"));
  return Array.isArray(payload) ? payload : payload.items ?? [];
};

export const fetchAIHubUsers = async (params?: { search_key?: string; page_size?: number; page_num?: number }) => {
  const payload = await unwrap<{ items?: AIHubUser[]; total?: number; page_size?: number; page_num?: number }>(
    api.get("/aihub/users/search", params)
  );
  return {
    items: payload.items ?? [],
    total: payload.total ?? 0,
    page_size: payload.page_size ?? params?.page_size ?? 20,
    page_num: payload.page_num ?? params?.page_num ?? 1
  };
};
export const fetchTaskAssignees = () => unwrap(api.get<User[]>("/task-assignees"));
export const fetchTeams = () => unwrap(api.get<Team[]>("/teams"));
export const fetchTeamActivity = (date?: string) =>
  unwrap(api.get<TeamActivity>("/teams/activity", date ? { date } : undefined));

// ───────────────────────── Admin ─────────────────────────

export const adminCreateTeam = (data: { name: string; director_user_id?: string }) =>
  unwrap(api.post<Team>("/admin/teams", data));
export const adminUpdateTeam = (id: string, data: { name: string; director_user_id?: string }) =>
  unwrap(api.put<Team>(`/admin/teams/${id}`, data));
export const adminDeleteTeam = (id: string) =>
  unwrap(api.delete<{ status: string; id: string }>(`/admin/teams/${id}`));

export const adminUpdateUser = (
  id: string,
  data: { app_role?: string; role?: string; team_id?: string; clear_team?: boolean; local_enabled?: boolean }
) => unwrap(api.put<unknown>(`/admin/users/${id}/profile`, data));

export const adminBatchAddUsers = (data: {
  user_ids: number[];
  app_role: string;
  team_id?: string;
  local_enabled?: boolean;
}) => unwrap(api.post<AdminBatchAddUsersResponse>("/admin/users/batch", data));

// ───────────────────────── Requirements ─────────────────────────

export const fetchRequirements = (params?: Record<string, string>) =>
  unwrap(api.get<Requirement[]>("/requirements", params));
export const fetchRequirement = (id: string) => unwrap(api.get<Requirement>(`/requirements/${id}`));
export const createRequirement = (data: {
  title: string;
  description: string;
  priority: string;
  deadline?: string;
  team_ids: string[];
  feishu_doc_url?: string;
  acceptance_criteria?: string[];
}) => unwrap(api.post<Requirement>("/requirements", data));
export const updateRequirement = (id: string, data: Record<string, unknown>) =>
  unwrap(api.put<Requirement>(`/requirements/${id}`, data));
export const deleteRequirement = (id: string, baseVersion: number) =>
  unwrap(
    api.delete<{ status: string; id: string }>(
      `/requirements/${id}?base_version=${encodeURIComponent(baseVersion)}`
    )
  );
export const cancelRequirement = (id: string, baseVersion: number) =>
  unwrap(api.put<Requirement>(`/requirements/${id}`, { status: "cancelled", base_version: baseVersion }));
export const restoreRequirement = (id: string, baseVersion: number) =>
  unwrap(api.put<Requirement>(`/requirements/${id}/restore`, { base_version: baseVersion }));
export const fetchACStatus = (id: string) => unwrap(api.get<ACStatus[]>(`/requirements/${id}/ac`));
export const regenerateAC = (id: string, baseVersion: number) =>
  unwrap(api.post<Requirement>(`/requirements/${id}/regenerate-ac`, { base_version: baseVersion }));

// ───────────────────────── Tasks ─────────────────────────

export const fetchTasks = (params?: Record<string, string>) =>
  unwrap(api.get<Task[]>("/tasks", params));
export const fetchTask = (id: string) => unwrap(api.get<Task>(`/tasks/${id}`));
export const createTask = (data: {
  requirement_id: string;
  title: string;
  acceptance_criteria?: string[];
  assignee_id?: string;
  priority: string;
  due_date?: string;
  depends_on_ids?: string[];
}) => unwrap(api.post<{ id: string; status: string }>("/tasks", data));
export const updateTask = (id: string, data: Record<string, unknown>) =>
  unwrap(api.put<Task>(`/tasks/${id}`, data, { skipErrorHandler: true }));
export const deleteTask = (id: string, baseVersion: number) =>
  unwrap(
    api.delete<{ status: string; id: string }>(
      `/tasks/${id}?base_version=${encodeURIComponent(baseVersion)}`,
      undefined,
      { skipErrorHandler: true }
    )
  );
export const updateTaskStatus = (id: string, status: string, baseVersion: number) =>
  unwrap(api.put<Task>(`/tasks/${id}/status`, { status, base_version: baseVersion }, { skipErrorHandler: true }));
export const updateTaskProgress = (id: string, progress: number, baseVersion: number) =>
  unwrap(api.put<Task>(`/tasks/${id}/progress`, { progress, base_version: baseVersion }, { skipErrorHandler: true }));
export const addTaskDependency = (taskId: string, dependsOnId: string, baseVersion: number) =>
  unwrap(
    api.post<Task>(
      `/tasks/${taskId}/dependencies`,
      { depends_on_id: dependsOnId, base_version: baseVersion },
      { skipErrorHandler: true }
    )
  );
export const removeTaskDependency = (taskId: string, depId: string, baseVersion: number) =>
  unwrap(
    api.delete<Task>(
      `/tasks/${taskId}/dependencies/${depId}?base_version=${encodeURIComponent(baseVersion)}`,
      undefined,
      {
        skipErrorHandler: true
      }
    )
  );

// ───────────────────────── Follows / Dashboard projections ─────────────────────────

export const fetchFollows = () => unwrap(api.get<RequirementFollowStateDTO[]>("/follows"));
export const followTarget = (targetType: "requirement" | "task", targetId: string) =>
  unwrap(
    api.post<{ favorited: true; target_type: "requirement" | "task"; target_id: string }>(
      "/follows",
      { target_type: targetType, target_id: targetId }
    )
  );
export const unfollowTarget = (targetType: "requirement" | "task", targetId: string) =>
  unwrap(
    api.delete<{ favorited: false; target_type: "requirement" | "task"; target_id: string }>(
      `/follows/${targetType}/${targetId}`
    )
  );
export const fetchFollowFollowers = (targetType: "requirement" | "task", targetId: string) =>
  unwrap(
    api.get<DashboardFollowFollowerDTO[]>("/follows/followers", {
      target_type: targetType,
      target_id: targetId
    })
  );
export const fetchDashboardFollows = () =>
  unwrap(api.get<DashboardFollowItemDTO[]>("/dashboard/follows"));
export const fetchDashboardRisks = () =>
  unwrap(api.get<DashboardRiskGroupDTO[]>("/dashboard/risks"));

// ───────────────────────── Sessions ─────────────────────────

export const fetchSessions = (params?: Record<string, string>) =>
  unwrap(api.get<PaginatedSessions>("/sessions", params));
export const updateSessionTask = (sessionId: string, taskId: string | null) =>
  unwrap(api.put<Session>(`/sessions/${sessionId}/task`, { task_id: taskId }));
export const updateSessionRequirement = (sessionId: string, requirementId: string | null) =>
  unwrap(
    api.put<Session>(`/sessions/${sessionId}/requirement`, {
      requirement_id: requirementId
    })
  );
export const withdrawSession = (sessionId: string) =>
  unwrap(api.delete<{ status: string }>(`/sessions/${sessionId}`));

export function getSessionLogURL(sessionId: string): string {
  return `${trimTrailingSlash(runtimeConfig.apiBaseUrl)}/sessions/${sessionId}/log`;
}

export async function downloadSessionLog(sessionId: string): Promise<void> {
  const { token } = getAuthSession();
  const res = await fetch(getSessionLogURL(sessionId), {
    headers: token ? { Authorization: `Bearer ${token}` } : undefined
  });
  if (!res.ok) throw new Error("日志下载失败");
  const blob = await res.blob();
  const blobUrl = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = blobUrl;
  link.download = `${sessionId}.jsonl`;
  link.click();
  URL.revokeObjectURL(blobUrl);
}

// ───────────────────────── Documents ─────────────────────────

export const fetchDocuments = (params?: Record<string, string>) =>
  unwrap(api.get<Document[]>("/documents", params));
export const createDocument = (data: {
  title: string;
  url: string;
  description?: string;
  task_id?: string;
}) => unwrap(api.post<Document>("/documents", data));
export const updateDocument = (id: string, data: Record<string, unknown>) =>
  unwrap(api.put<Document>(`/documents/${id}`, data));
export const deleteDocument = (id: string) =>
  unwrap(api.delete<{ status: string }>(`/documents/${id}`));

// ───────────────────────── Reports ─────────────────────────

export const fetchPaginatedReports = (params?: Record<string, string>) =>
  unwrap(api.get<PaginatedDailyReports>("/reports", params));
export const fetchMyReports = (params?: Record<string, string>) =>
  unwrap(api.get<PaginatedDailyReports>("/reports/mine", params));
export const fetchReports = async (params?: Record<string, string>) => {
  const page =
    params?.scope === "mine" ? await fetchMyReports(params) : await fetchPaginatedReports(params);
  return page.items.map((item) => ({
    id: item.id,
    user_id: item.user_id,
    user_name: item.user_name,
    report_date: item.report_date,
    content: "",
    status: item.status,
    submitted_to: item.submitted_to,
    edited: item.edited,
    session_ids: item.session_ids,
    saved_at: item.saved_at,
    submitted_at: item.submitted_at,
    created_at: item.created_at,
    updated_at: item.updated_at
  }));
};
export const fetchTodayReport = () => unwrap(api.get<DailyReport>("/reports/today"));
export const generateTodayReportDraft = (payload: GenerateReportDraftPayload) =>
  unwrap(api.post<GenerateReportDraftResponse>("/reports/today/draft", payload));
export const generateTodayReport = (reportDate?: string) =>
  unwrap(
    api.post<DailyReport>(
      "/reports/today/generate",
      undefined,
      reportDate ? { params: { report_date: reportDate } } : undefined
    )
  );
export const fetchReport = (id: string) => unwrap(api.get<DailyReport>(`/reports/${id}`));
export const updateReport = (
  id: string,
  data: { content?: string; feishu_doc_url?: string; session_ids?: string[] }
) => unwrap(api.put<DailyReport>(`/reports/${id}`, data));
export const saveReport = updateReport;
export const submitReport = (id: string, data: { content?: string; session_ids?: string[] }) =>
  unwrap(api.post<DailyReport>(`/reports/${id}/submit`, data));

export const fetchTeamMemberReports = (date: string) =>
  unwrap(api.get<TeamMemberReport[]>(`/reports/team/members`, { date }));
export const fetchTeamReportSources = (date: string, teamId?: string) =>
  unwrap(
    api.get<TeamReportSources>(
      "/reports/team/sources",
      teamId ? { date, team_id: teamId } : { date }
    )
  );
export const fetchTeamReportToday = (reportDate?: string) =>
  unwrap(
    api.get<TeamReport>("/reports/team/today", reportDate ? { report_date: reportDate } : undefined)
  );
export async function fetchTeamReportTodayOrNull(reportDate?: string) {
  try {
    return await unwrap(
      api.get<TeamReport>(
        "/reports/team/today",
        reportDate ? { report_date: reportDate } : undefined,
        { skipErrorHandler: true }
      )
    );
  } catch (error) {
    if (error instanceof HttpError && error.status === 404) {
      return null;
    }
    throw error;
  }
}
export const saveTeamReportCurrent = (data: {
  report_date: string;
  content?: string;
}) => unwrap(api.put<TeamReport>("/reports/team/today", data));
export const generateTeamReport = (reportDate?: string) =>
  unwrap(
    api.post<TeamReport>(
      "/reports/team/today/generate",
      undefined,
      reportDate ? { params: { report_date: reportDate } } : undefined
    )
  );
export const fetchTeamReports = (params?: Record<string, string>) =>
  unwrap(api.get<PaginatedTeamReports>("/reports/team", params));
export const fetchTeamReport = (id: string) => unwrap(api.get<TeamReport>(`/reports/team/${id}`));
export const updateTeamReport = (id: string, data: { content?: string; feishu_doc_url?: string }) =>
  unwrap(api.put<TeamReport>(`/reports/team/${id}`, data));
export const submitTeamReport = (id: string, data?: { content?: string }) =>
  unwrap(api.post<TeamReport>(`/reports/team/${id}/submit`, data));
export const fetchDepartmentReportSources = (date: string) =>
  unwrap(api.get<DepartmentReportSources>("/reports/department/sources", { date }));
export const fetchDepartmentReportToday = (reportDate?: string) =>
  unwrap(
    api.get<DepartmentReport>(
      "/reports/department/today",
      reportDate ? { report_date: reportDate } : undefined
    )
  );
export async function fetchDepartmentReportTodayOrNull(reportDate?: string) {
  try {
    return await unwrap(
      api.get<DepartmentReport>(
        "/reports/department/today",
        reportDate ? { report_date: reportDate } : undefined,
        { skipErrorHandler: true }
      )
    );
  } catch (error) {
    if (error instanceof HttpError && error.status === 404) {
      return null;
    }
    throw error;
  }
}
export const generateDepartmentReport = (reportDate?: string) =>
  unwrap(
    api.post<DepartmentReport>(
      "/reports/department/today/generate",
      undefined,
      reportDate ? { params: { report_date: reportDate } } : undefined
    )
  );
export const saveDepartmentReportCurrent = (data: {
  report_date: string;
  content?: string;
  archive?: boolean;
}) => unwrap(api.put<DepartmentReport>("/reports/department/today", data));
export const fetchDepartmentReports = (params?: Record<string, string>) =>
  unwrap(api.get<PaginatedDepartmentReports>("/reports/department", params));
export const fetchDepartmentReport = (id: string) =>
  unwrap(api.get<DepartmentReport>(`/reports/department/${id}`));
export const updateDepartmentReport = (id: string, data: { content?: string; archive?: boolean }) =>
  unwrap(api.put<DepartmentReport>(`/reports/department/${id}`, data));

export const fetchPersonalWeeklyReports = (params?: Record<string, string>) =>
  unwrap(api.get<PaginatedPersonalWeeklyReports>("/reports/weekly/mine", params));
export const fetchPersonalWeeklyReportSources = (weekStart: string) =>
  unwrap(
    api.get<PersonalWeeklyReportSources>("/reports/weekly/mine/sources", { week_start: weekStart })
  );
export const fetchPersonalWeeklyReportCurrent = (weekStart: string) =>
  unwrap(
    api.get<PersonalWeeklyReport | null>("/reports/weekly/mine/current", { week_start: weekStart })
  );
export async function fetchPersonalWeeklyReportCurrentOrNull(weekStart: string) {
  try {
    return await unwrap(
      api.get<PersonalWeeklyReport | null>(
        "/reports/weekly/mine/current",
        { week_start: weekStart },
        { skipErrorHandler: true }
      )
    );
  } catch (error) {
    if (error instanceof HttpError && error.status === 404) {
      return null;
    }
    throw error;
  }
}
export const generatePersonalWeeklyReport = (data: {
  week_start: string;
  source_daily_report_ids?: string[];
}) => unwrap(api.post<PersonalWeeklyReportPreview>("/reports/weekly/mine/current/generate", data));
export const savePersonalWeeklyReport = (data: {
  week_start: string;
  content: string;
  source_daily_report_ids?: string[];
}) => unwrap(api.put<PersonalWeeklyReport>("/reports/weekly/mine/current", data));
export const submitPersonalWeeklyReport = (data: {
  week_start: string;
  content: string;
  source_daily_report_ids?: string[];
}) => unwrap(api.post<PersonalWeeklyReport>("/reports/weekly/mine/current/submit", data));

export const fetchTeamWeeklyReportSources = (weekStart: string, teamId?: string) =>
  unwrap(
    api.get<TeamWeeklyReportSources>(
      "/reports/team/weekly/sources",
      teamId ? { week_start: weekStart, team_id: teamId } : { week_start: weekStart }
    )
  );
export const fetchTeamWeeklyReportCurrent = (weekStart: string, teamId?: string) =>
  unwrap(
    api.get<TeamWeeklyReport>(
      "/reports/team/weekly/current",
      teamId ? { week_start: weekStart, team_id: teamId } : { week_start: weekStart }
    )
  );
export async function fetchTeamWeeklyReportCurrentOrNull(weekStart: string, teamId?: string) {
  try {
    return await unwrap(
      api.get<TeamWeeklyReport>(
        "/reports/team/weekly/current",
        teamId ? { week_start: weekStart, team_id: teamId } : { week_start: weekStart },
        { skipErrorHandler: true }
      )
    );
  } catch (error) {
    if (error instanceof HttpError && error.status === 404) {
      return null;
    }
    throw error;
  }
}
export const generateTeamWeeklyReport = (data: {
  week_start: string;
  source_personal_weekly_report_ids?: string[];
}) => unwrap(api.post<TeamWeeklyReportPreview>("/reports/team/weekly/current/generate", data));
export const saveTeamWeeklyReport = (data: {
  week_start: string;
  content: string;
  source_personal_weekly_report_ids?: string[];
}) => unwrap(api.put<TeamWeeklyReport>("/reports/team/weekly/current", data));
export const submitTeamWeeklyReportCurrent = (data: {
  week_start: string;
  content: string;
  source_personal_weekly_report_ids?: string[];
}) => unwrap(api.post<TeamWeeklyReport>("/reports/team/weekly/current/submit", data));
export const updateTeamWeeklyReport = (id: string, data: { content?: string }) =>
  unwrap(api.put<TeamWeeklyReport>(`/reports/team/weekly/${id}`, data));
export const submitTeamWeeklyReport = (id: string) =>
  unwrap(api.post<TeamWeeklyReport>(`/reports/team/weekly/${id}/submit`));
export const fetchTeamWeeklyReports = (params?: Record<string, string>) =>
  unwrap(api.get<TeamWeeklyReport[]>("/reports/team/weekly", params));

export const fetchDepartmentWeeklyReportSources = (weekStart: string) =>
  unwrap(
    api.get<DepartmentWeeklyReportSources>("/reports/department/weekly/sources", {
      week_start: weekStart
    })
  );
export const fetchDepartmentWeeklyReportCurrent = (weekStart: string) =>
  unwrap(
    api.get<DepartmentWeeklyReport>("/reports/department/weekly/current", { week_start: weekStart })
  );
export async function fetchDepartmentWeeklyReportCurrentOrNull(weekStart: string) {
  try {
    return await unwrap(
      api.get<DepartmentWeeklyReport>(
        "/reports/department/weekly/current",
        { week_start: weekStart },
        { skipErrorHandler: true }
      )
    );
  } catch (error) {
    if (error instanceof HttpError && error.status === 404) {
      return null;
    }
    throw error;
  }
}
export const generateDepartmentWeeklyReport = (weekStart: string) =>
  unwrap(
    api.post<DepartmentWeeklyReport>("/reports/department/weekly/current/generate", undefined, {
      params: { week_start: weekStart }
    })
  );
export const saveDepartmentWeeklyReportCurrent = (data: {
  week_start: string;
  content: string;
  archive?: boolean;
}) => unwrap(api.put<DepartmentWeeklyReport>("/reports/department/weekly/current", data));
export const updateDepartmentWeeklyReport = (
  id: string,
  data: { content?: string; archive?: boolean }
) => unwrap(api.put<DepartmentWeeklyReport>(`/reports/department/weekly/${id}`, data));
export const fetchDepartmentWeeklyReports = (params?: Record<string, string>) =>
  unwrap(api.get<DepartmentWeeklyReport[]>("/reports/department/weekly", params));

// ───────────────────────── Managed AI assets ─────────────────────────

export const fetchManagedSkills = (includeSystem = false) =>
  unwrap(
    api.get<{ skills: ManagedSkill[] }>(
      "/ai-assets/skills",
      includeSystem ? { scope: "mine", include_system: "true" } : { scope: "mine" },
      { skipErrorHandler: true }
    )
  );
export const createManagedSkill = (payload: CreateManagedSkillPayload) =>
  unwrap(api.post<ManagedSkill>("/ai-assets/skills", payload));
export const fetchManagedSkillMarkdown = (owner: string | undefined, slug: string, version: string) =>
  unwrap(
    api.get<{ content: string }>(
      `/ai-assets/skills/${encodeURIComponent(owner || "_mine")}/${encodeURIComponent(slug)}/${encodeURIComponent(version)}/skill-md`
    )
  );
export const archiveManagedSkill = (slug: string, version: string, archived: boolean) =>
  unwrap(api.post<Record<string, unknown>>(`/ai-assets/skills/${encodeURIComponent(slug)}/${encodeURIComponent(version)}/archive`, { archived }));
export const deleteManagedSkill = (slug: string, version: string) =>
  unwrap(api.delete<Record<string, unknown>>(`/ai-assets/skills/${encodeURIComponent(slug)}/${encodeURIComponent(version)}`));
export const fetchManagedMCPEntries = (includeSystem = false) =>
  unwrap(
    api.get<{ entries: ManagedMCPEntry[] }>(
      "/ai-assets/mcp",
      includeSystem ? { scope: "mine", include_system: "true" } : { scope: "mine" },
      { skipErrorHandler: true }
    )
  );
export const createManagedMCPEntry = (payload: ManagedMCPEntry) =>
  unwrap(api.post<ManagedMCPEntry>("/ai-assets/mcp", payload));
export const archiveManagedMCPEntry = (slug: string, version: string, archived: boolean) =>
  unwrap(api.post<Record<string, unknown>>(`/ai-assets/mcp/${encodeURIComponent(slug)}/${encodeURIComponent(version)}/archive`, { archived }));
export const deleteManagedMCPEntry = (slug: string, version: string) =>
  unwrap(api.delete<Record<string, unknown>>(`/ai-assets/mcp/${encodeURIComponent(slug)}/${encodeURIComponent(version)}`));
export const fetchManagedCredentials = () =>
  unwrap(api.get<{ credentials: ManagedCredential[] }>("/ai-assets/credentials", undefined, { skipErrorHandler: true }));
export const createManagedCredential = (payload: { name: string; value: string; kind?: string; description?: string }) =>
  unwrap(api.post<{ credential_id: string }>("/ai-assets/credentials", payload));
export const deleteManagedCredential = (credentialId: string) =>
  unwrap(api.delete<Record<string, unknown>>(`/ai-assets/credentials/${encodeURIComponent(credentialId)}`));
export const fetchDailyReportAgentIntegration = () =>
  unwrap(api.get<DailyReportAgentIntegration>("/ai-assets/daily-report-integration"));
export const fetchManagedAgents = () =>
  unwrap(api.get<{ agents: ManagedAgent[] }>("/ai-assets/agents", undefined, { skipErrorHandler: true }));
export const createManagedAgent = (payload: UpsertManagedAgentPayload) =>
  unwrap(api.post<{ agent_id: string; managed_version?: number }>("/ai-assets/agents", payload));
export const createDefaultReportAgent = () =>
  unwrap(api.post<ManagedAgent>("/ai-assets/report-agents/default"));
export const setDefaultReportAgent = (agentId: string) =>
  unwrap(api.post<ManagedAgent>(`/ai-assets/report-agents/${encodeURIComponent(agentId)}/default`));
export const updateManagedAgent = (agentId: string, payload: UpsertManagedAgentPayload) =>
  unwrap(
    api.put<{ agent_id: string; managed_version?: number }>(`/ai-assets/agents/${agentId}`, payload)
  );
export const archiveManagedAgent = (agentId: string, archived: boolean) =>
  unwrap(api.post<Record<string, unknown>>(`/ai-assets/agents/${encodeURIComponent(agentId)}/archive`, { archived }));
export const startManagedAgentRun = (agentId: string, payload: ManagedAgentManualRunPayload) =>
  unwrap(api.post<AIRun>(`/ai-assets/agents/${agentId}/runs`, payload));
export const startReportAgentRun = (
  agentId: string,
  payload: ManagedReportAgentRunPayload,
  options?: { skipErrorHandler?: boolean }
) =>
  unwrap(
    api.post<ManagedReportAgentRunResponse>(
      `/ai-assets/report-agents/${agentId}/runs`,
      payload,
      options
    )
  );
export const fetchManagedAgentRuns = (params?: {
  agent_id?: string;
  business_type?: string;
  page_size?: string;
}) => unwrap(api.get<{ runs: AIRun[] }>("/ai-assets/agent-runs", params));
export const fetchManagedAgentRun = (runId: string, options?: { skipErrorHandler?: boolean }) =>
  unwrap(api.get<AIRun>(`/ai-assets/agent-runs/${runId}`, undefined, options));
export const fetchManagedAgentSchedules = () =>
  unwrap(api.get<{ schedules: ManagedAgentSchedule[] }>("/ai-assets/agent-schedules"));
export const previewManagedAgentSchedule = (payload: PreviewManagedAgentSchedulePayload) =>
  unwrap(api.post<PreviewManagedAgentScheduleResponse>("/ai-assets/agent-schedules/preview", payload));
export const createManagedAgentSchedule = (payload: UpsertManagedAgentSchedulePayload) =>
  unwrap(api.post<ManagedAgentSchedule>("/ai-assets/agent-schedules", payload));
export const updateManagedAgentSchedule = (
  scheduleId: string,
  payload: UpsertManagedAgentSchedulePayload
) => unwrap(api.put<ManagedAgentSchedule>(`/ai-assets/agent-schedules/${scheduleId}`, payload));
export const deleteManagedAgentSchedule = (scheduleId: string) =>
  unwrap(api.delete<{ status: string }>(`/ai-assets/agent-schedules/${scheduleId}`));
export const runManagedAgentScheduleNow = (
  scheduleId: string,
  triggerSource: "manual" | "save_and_run" = "manual"
) => unwrap(api.post<AIRun>(`/ai-assets/agent-schedules/${scheduleId}/runs`, { trigger_source: triggerSource }));

// ───────────────────────── Tokens ─────────────────────────

export const fetchTokens = (params?: {
  period?: TokenPeriod;
  from?: string;
  to?: string;
  group_by?: TokenGroupBy;
  scope?: "mine" | "team";
}) => {
  const qs = params ? "?" + new URLSearchParams(params as Record<string, string>).toString() : "";
  return unwrap(api.get<TokenAggregation>(`/tokens${qs}`));
};

export const fetchSessionTokens = (params: {
  from?: string;
  to?: string;
  scope?: "mine" | "team";
  page?: string;
  page_size?: string;
}) => unwrap(api.get<PaginatedSessionTokens>("/tokens/sessions", params));

export async function fetchAllSessionTokens(params: {
  from: string;
  to: string;
  scope?: "mine" | "team";
}) {
  const pageSize = 100;
  const firstPage = await fetchSessionTokens({
    ...params,
    page: "1",
    page_size: String(pageSize)
  });
  const items = [...firstPage.items];
  const totalPages = Math.ceil(firstPage.total / firstPage.page_size);

  for (let page = 2; page <= totalPages; page += 1) {
    const nextPage = await fetchSessionTokens({
      ...params,
      page: String(page),
      page_size: String(pageSize)
    });
    items.push(...nextPage.items);
  }

  return items;
}
