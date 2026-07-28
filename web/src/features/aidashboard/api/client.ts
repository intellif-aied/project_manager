import { runtimeConfig } from "@/config/runtimeConfig";
import { api } from "@/shared/request/httpClient";
import { getAuthSession } from "@/shared/auth/session";
import { HttpError } from "@/shared/request/types";
import type { User } from "@/shared/auth/types";
import type {
  AIHubUser,
  AvailableModelsResponse,
  AdminBatchAddUsersResponse,
  ACStatus,
  AIRun,
  DailyReport,
  DailyReportAgentIntegration,
  Department,
  DepartmentReport,
  DepartmentReportSources,
  DepartmentWeeklyReport,
  DepartmentWeeklyReportSources,
  Document,
  CreateManagedSkillPayload,
  ManagedAgent,
  ManagedAgentManualRunPayload,
  ManagedReportAgentRunPayload,
  ManagedReportAgentRunResponse,
  ManagedAgentSchedule,
  ManagedCredential,
  ManagedMCPEntry,
  ManagedSkill,
  MemberPersonalReport,
  PaginatedDailyReports,
  PaginatedDepartmentReports,
  PaginatedDashboardFollowItems,
  PaginatedDashboardRiskGroups,
  PaginatedFollowFollowers,
  PaginatedWorkItemEvents,
  PaginatedPersonalWeeklyReports,
  PaginatedRequirements,
  PaginatedSessions,
  PaginatedSessionTokens,
  PaginatedTeamReports,
  PaginatedTasks,
  PersonalWeeklyReport,
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
  TeamWeeklyReportSources,
  TokenAggregation,
  TokenAnalyticsCapability,
  TokenAnalyticsFilters,
  TokenAnalyticsRankingItem,
  TokenAnalyticsSessionItem,
  TokenAnalyticsSummary,
  TokenAnalyticsTrendPoint,
  TokenGroupBy,
  TokenPeriod,
  ExchangeRateVersion,
  ModelAlias,
  ModelPriceVersion,
  PriceBook,
  PricingRecalculationRun,
  UnpricedModel,
  Team,
  PreviewManagedAgentSchedulePayload,
  PreviewManagedAgentScheduleResponse,
  RequirementBoardResponseDTO,
  ReportSourceCandidatePage,
  ReportSourceSelection,
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
  return Array.isArray(payload) ? payload : (payload.items ?? []);
};

export const fetchAIHubUsers = async (params?: {
  search_key?: string;
  page_size?: number;
  page_num?: number;
}) => {
  const payload = await unwrap<{
    items?: AIHubUser[];
    total?: number;
    page_size?: number;
    page_num?: number;
  }>(api.get("/aihub/users/search", params));
  return {
    items: payload.items ?? [],
    total: payload.total ?? 0,
    page_size: payload.page_size ?? params?.page_size ?? 20,
    page_num: payload.page_num ?? params?.page_num ?? 1
  };
};
export const fetchTaskAssignees = () => unwrap(api.get<User[]>("/task-assignees"));
export const fetchTeams = () => unwrap(api.get<Team[]>("/teams"));
export const fetchDepartments = () => unwrap(api.get<Department[]>("/departments"));
export const fetchTeamActivity = (date?: string) =>
  unwrap(api.get<TeamActivity>("/teams/activity", date ? { date } : undefined));

// ───────────────────────── Admin ─────────────────────────

export const adminCreateTeam = (data: { name: string; director_user_id?: string }) =>
  unwrap(api.post<Team>("/admin/teams", data));
export const adminUpdateTeam = (id: string, data: { name: string; director_user_id?: string }) =>
  unwrap(api.put<Team>(`/admin/teams/${id}`, data));
export const adminDeleteTeam = (id: string) =>
  unwrap(api.delete<{ status: string; id: string }>(`/admin/teams/${id}`));
export const adminCreateDepartment = (data: {
  name: string;
  director_user_id?: string;
  team_ids: string[];
  pm_user_ids: string[];
}) => unwrap(api.post<{ id: string }>("/admin/departments", data));
export const adminUpdateDepartment = (
  id: string,
  data: {
    name: string;
    director_user_id?: string;
    team_ids: string[];
    pm_user_ids: string[];
  }
) => unwrap(api.put<{ id: string }>(`/admin/departments/${id}`, data));

export const adminUpdateUser = (
  id: string,
  data: {
    app_role?: string;
    role?: string;
    team_id?: string;
    clear_team?: boolean;
    local_enabled?: boolean;
  }
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
export const fetchPaginatedRequirements = (params?: Record<string, string>) =>
  unwrap(api.get<PaginatedRequirements>("/requirements", { view: "list", ...(params ?? {}) }));
export const fetchRequirementBoard = (params?: Record<string, string>) =>
  unwrap(
    api.get<RequirementBoardResponseDTO>("/requirements", { view: "board", ...(params ?? {}) })
  );
export const fetchRequirement = (id: string) => unwrap(api.get<Requirement>(`/requirements/${id}`));
export const createRequirement = (data: {
  title: string;
  description: string;
  priority: string;
  deadline?: string;
  responsible_user_ids?: string[];
  team_ids?: string[];
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
  unwrap(
    api.put<Requirement>(`/requirements/${id}`, { status: "cancelled", base_version: baseVersion })
  );
export const restoreRequirement = (id: string, baseVersion: number) =>
  unwrap(api.put<Requirement>(`/requirements/${id}/restore`, { base_version: baseVersion }));
export const fetchACStatus = (id: string) => unwrap(api.get<ACStatus[]>(`/requirements/${id}/ac`));
export const regenerateAC = (id: string, baseVersion: number) =>
  unwrap(api.post<Requirement>(`/requirements/${id}/regenerate-ac`, { base_version: baseVersion }));
export const fetchRequirementEvents = (
  id: string,
  params?: { page?: number; page_size?: number }
) =>
  unwrap(
    api.get<PaginatedWorkItemEvents>(`/requirements/${id}/events`, {
      page: String(params?.page ?? 1),
      page_size: String(params?.page_size ?? 20)
    })
  );

// ───────────────────────── Tasks ─────────────────────────

export const fetchTasks = (params?: Record<string, string>) =>
  unwrap(api.get<Task[]>("/tasks", params));
export const fetchPaginatedTasks = (params?: Record<string, string>) =>
  unwrap(api.get<PaginatedTasks>("/tasks", params));
export const fetchTask = (id: string) => unwrap(api.get<Task>(`/tasks/${id}`));
export const fetchTaskEvents = async (
  id: string,
  params?: { page?: number; page_size?: number }
) => {
  const page = params?.page ?? 1;
  const pageSize = params?.page_size ?? 20;
  try {
    return await unwrap(
      api.get<PaginatedWorkItemEvents>(
        `/tasks/${id}/events`,
        {
          page: String(page),
          page_size: String(pageSize)
        },
        { skipErrorHandler: true }
      )
    );
  } catch (error) {
    if (error instanceof HttpError && error.status === 404) {
      return { items: [], total: 0, page, page_size: pageSize };
    }
    throw error;
  }
};
export const createTask = (data: {
  requirement_id: string;
  title: string;
  acceptance_criteria?: string[];
  responsible_user_ids?: string[];
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
  unwrap(
    api.put<Task>(
      `/tasks/${id}/status`,
      { status, base_version: baseVersion },
      { skipErrorHandler: true }
    )
  );
export const updateTaskProgress = (id: string, progress: number, baseVersion: number) =>
  unwrap(
    api.put<Task>(
      `/tasks/${id}/progress`,
      { progress, base_version: baseVersion },
      { skipErrorHandler: true }
    )
  );
export const addTaskDependency = (
  taskId: string,
  dependsOnId: string,
  baseVersion: number,
  dependsOnType: "requirement" | "task" = "task"
) =>
  unwrap(
    api.post<Task>(
      `/tasks/${taskId}/dependencies`,
      { depends_on_type: dependsOnType, depends_on_id: dependsOnId, base_version: baseVersion },
      { skipErrorHandler: true }
    )
  );
export const removeTaskDependency = (
  taskId: string,
  depId: string,
  baseVersion: number,
  dependsOnType: "requirement" | "task" = "task"
) =>
  unwrap(
    api.delete<Task>(
      `/tasks/${taskId}/dependencies/${depId}?base_version=${encodeURIComponent(
        baseVersion
      )}&depends_on_type=${encodeURIComponent(dependsOnType)}`,
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
    api.get<PaginatedFollowFollowers>("/follows/followers", {
      target_type: targetType,
      target_id: targetId,
      page: "1",
      page_size: "20"
    })
  );
export const fetchDashboardFollows = (params?: { page?: number; page_size?: number }) =>
  unwrap(
    api.get<PaginatedDashboardFollowItems>("/dashboard/follows", {
      page: String(params?.page ?? 1),
      page_size: String(params?.page_size ?? 20)
    })
  );
export const fetchDashboardMyItems = (params?: { page?: number; page_size?: number }) =>
  unwrap(
    api.get<PaginatedDashboardFollowItems>("/dashboard/my-items", {
      page: String(params?.page ?? 1),
      page_size: String(params?.page_size ?? 20)
    })
  );
export const fetchDashboardRisks = (params?: { page?: number; page_size?: number }) =>
  unwrap(
    api.get<PaginatedDashboardRiskGroups>("/dashboard/risks", {
      page: String(params?.page ?? 1),
      page_size: String(params?.page_size ?? 20)
    })
  );

// ───────────────────────── Sessions ─────────────────────────

export const fetchSessions = (params?: Record<string, string>) =>
  unwrap(api.get<PaginatedSessions>("/sessions", params));
export const updateSessionTask = (
  sessionId: string,
  taskId: string | null,
  activityDate?: string
) =>
  unwrap(
    api.put<Session>(`/sessions/${sessionId}/task`, {
      task_id: taskId,
      ...(activityDate ? { activity_date: activityDate } : {})
    })
  );
export const updateSessionRequirement = (
  sessionId: string,
  requirementId: string | null,
  activityDate?: string
) =>
  unwrap(
    api.put<Session>(`/sessions/${sessionId}/requirement`, {
      requirement_id: requirementId,
      ...(activityDate ? { activity_date: activityDate } : {})
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
    next_day_plan: item.next_day_plan,
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
export const fetchTodayReport = (reportDate?: string) =>
  unwrap(
    api.get<DailyReport>("/reports/today", reportDate ? { report_date: reportDate } : undefined)
  );
export const fetchReport = (id: string) => unwrap(api.get<DailyReport>(`/reports/${id}`));
export const deleteReport = (id: string) => unwrap(api.delete<void>(`/reports/${id}`));
export const updateReport = (
  id: string,
  data: {
    content?: string;
    next_day_plan?: string;
    feishu_doc_url?: string;
    session_ids?: string[];
  }
) => unwrap(api.put<DailyReport>(`/reports/${id}`, data));
export const saveReport = updateReport;
export const submitReport = (id: string, data: { content?: string; session_ids?: string[] }) =>
  unwrap(api.post<DailyReport>(`/reports/${id}/submit`, data));

export const fetchTeamMemberReports = (date: string, teamId?: string) =>
  unwrap(
    api.get<TeamMemberReport[]>(
      `/reports/team/members`,
      teamId ? { date, team_id: teamId } : { date }
    )
  );
export const fetchMemberDailyReports = (date: string, departmentId?: string) =>
  unwrap(
    api.get<MemberPersonalReport[]>("/reports/members/daily", {
      date,
      ...(departmentId ? { department_id: departmentId } : {})
    })
  );
export const fetchMemberDailyReport = (id: string) =>
  unwrap(api.get<DailyReport>(`/reports/members/daily/${id}`));
export const fetchMemberWeeklyReports = (weekStart: string, departmentId?: string) =>
  unwrap(
    api.get<MemberPersonalReport[]>("/reports/members/weekly", {
      week_start: weekStart,
      ...(departmentId ? { department_id: departmentId } : {})
    })
  );
export const fetchMemberWeeklyReport = (id: string) =>
  unwrap(api.get<PersonalWeeklyReport>(`/reports/members/weekly/${id}`));
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
  if (reportDate) {
    const reports = await fetchTeamReports({
      from: reportDate,
      to: reportDate,
      page: "1",
      page_size: "1"
    });
    const report = reports.items[0];
    return report ? fetchTeamReport(report.id) : null;
  }

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
  next_day_plan?: string;
}) => unwrap(api.put<TeamReport>("/reports/team/today", data));
export const fetchTeamReports = (params?: Record<string, string>) =>
  unwrap(api.get<PaginatedTeamReports>("/reports/team", params));
export const fetchTeamReport = (id: string) => unwrap(api.get<TeamReport>(`/reports/team/${id}`));
export const deleteTeamReport = (id: string) =>
  unwrap(api.delete<void>(`/reports/team/${id}`));
export const updateTeamReport = (
  id: string,
  data: { content?: string; next_day_plan?: string; feishu_doc_url?: string }
) => unwrap(api.put<TeamReport>(`/reports/team/${id}`, data));
export const submitTeamReport = (id: string, data?: { content?: string }) =>
  unwrap(api.post<TeamReport>(`/reports/team/${id}/submit`, data));
export const fetchDepartmentReportSources = (date: string, departmentId?: string) =>
  unwrap(
    api.get<DepartmentReportSources>("/reports/department/sources", {
      date,
      ...(departmentId ? { department_id: departmentId } : {})
    })
  );
export const fetchDepartmentReportToday = (reportDate?: string, departmentId?: string) =>
  unwrap(
    api.get<DepartmentReport>("/reports/department/today", {
      ...(reportDate ? { report_date: reportDate } : {}),
      ...(departmentId ? { department_id: departmentId } : {})
    })
  );
export async function fetchDepartmentReportTodayOrNull(reportDate?: string, departmentId?: string) {
  if (reportDate) {
    const reports = await fetchDepartmentReports({
      from: reportDate,
      to: reportDate,
      page: "1",
      page_size: "1",
      ...(departmentId ? { department_id: departmentId } : {})
    });
    const report = reports.items[0];
    return report ? fetchDepartmentReport(report.id, departmentId) : null;
  }

  try {
    return await unwrap(
      api.get<DepartmentReport>(
        "/reports/department/today",
        {
          ...(reportDate ? { report_date: reportDate } : {}),
          ...(departmentId ? { department_id: departmentId } : {})
        },
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
export const saveDepartmentReportCurrent = (data: {
  department_id?: string;
  report_date: string;
  content?: string;
  next_day_plan?: string;
  archive?: boolean;
}) => unwrap(api.put<DepartmentReport>("/reports/department/today", data));
export const fetchDepartmentReports = (params?: Record<string, string>) =>
  unwrap(api.get<PaginatedDepartmentReports>("/reports/department", params));
export const fetchDepartmentReport = (id: string, departmentId?: string) =>
  unwrap(
    api.get<DepartmentReport>(
      `/reports/department/${id}`,
      departmentId ? { department_id: departmentId } : undefined
    )
  );
export const updateDepartmentReport = (
  id: string,
  data: { content?: string; next_day_plan?: string; archive?: boolean },
  departmentId?: string
) =>
  unwrap(
    api.put<DepartmentReport>(
      departmentId
        ? `/reports/department/${id}?department_id=${encodeURIComponent(departmentId)}`
        : `/reports/department/${id}`,
      data
    )
  );
export const deleteDepartmentReport = (id: string, departmentId?: string) =>
  unwrap(
    api.delete<void>(
      departmentId
        ? `/reports/department/${id}?department_id=${encodeURIComponent(departmentId)}`
        : `/reports/department/${id}`
    )
  );

export const fetchPersonalWeeklyReports = (params?: Record<string, string>) =>
  unwrap(api.get<PaginatedPersonalWeeklyReports>("/reports/weekly/mine", params));
export const deletePersonalWeeklyReport = (id: string) =>
  unwrap(api.delete<void>(`/reports/weekly/mine/${id}`));
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
  const reports = await fetchTeamWeeklyReports({
    from_week: weekStart,
    to_week: weekStart,
    ...(teamId ? { team_id: teamId } : {})
  });
  return reports[0] ?? null;
}
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
export const deleteTeamWeeklyReport = (id: string) =>
  unwrap(api.delete<void>(`/reports/team/weekly/${id}`));

export const fetchDepartmentWeeklyReportSources = (weekStart: string, departmentId?: string) =>
  unwrap(
    api.get<DepartmentWeeklyReportSources>("/reports/department/weekly/sources", {
      week_start: weekStart,
      ...(departmentId ? { department_id: departmentId } : {})
    })
  );
export const fetchDepartmentWeeklyReportCurrent = (weekStart: string, departmentId?: string) =>
  unwrap(
    api.get<DepartmentWeeklyReport>("/reports/department/weekly/current", {
      week_start: weekStart,
      ...(departmentId ? { department_id: departmentId } : {})
    })
  );
export async function fetchDepartmentWeeklyReportCurrentOrNull(
  weekStart: string,
  departmentId?: string
) {
  const reports = await fetchDepartmentWeeklyReports({
    from_week: weekStart,
    to_week: weekStart,
    ...(departmentId ? { department_id: departmentId } : {})
  });
  return reports[0] ?? null;
}
export const saveDepartmentWeeklyReportCurrent = (data: {
  department_id?: string;
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
export const deleteDepartmentWeeklyReport = (id: string, departmentId?: string) =>
  unwrap(
    api.delete<void>(
      departmentId
        ? `/reports/department/weekly/${id}?department_id=${encodeURIComponent(departmentId)}`
        : `/reports/department/weekly/${id}`
    )
  );

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
export const fetchManagedSkillMarkdown = (
  owner: string | undefined,
  slug: string,
  version: string
) =>
  unwrap(
    api.get<{ content: string }>(
      `/ai-assets/skills/${encodeURIComponent(owner || "_mine")}/${encodeURIComponent(slug)}/${encodeURIComponent(version)}/skill-md`
    )
  );
export const archiveManagedSkill = (slug: string, version: string, archived: boolean) =>
  unwrap(
    api.post<Record<string, unknown>>(
      `/ai-assets/skills/${encodeURIComponent(slug)}/${encodeURIComponent(version)}/archive`,
      { archived }
    )
  );
export const deleteManagedSkill = (slug: string, version: string) =>
  unwrap(
    api.delete<Record<string, unknown>>(
      `/ai-assets/skills/${encodeURIComponent(slug)}/${encodeURIComponent(version)}`
    )
  );
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
  unwrap(
    api.post<Record<string, unknown>>(
      `/ai-assets/mcp/${encodeURIComponent(slug)}/${encodeURIComponent(version)}/archive`,
      { archived }
    )
  );
export const deleteManagedMCPEntry = (slug: string, version: string) =>
  unwrap(
    api.delete<Record<string, unknown>>(
      `/ai-assets/mcp/${encodeURIComponent(slug)}/${encodeURIComponent(version)}`
    )
  );
export const fetchManagedCredentials = () =>
  unwrap(
    api.get<{ credentials: ManagedCredential[] }>("/ai-assets/credentials", undefined, {
      skipErrorHandler: true
    })
  );
export const createManagedCredential = (payload: {
  name: string;
  value: string;
  kind?: string;
  description?: string;
}) => unwrap(api.post<{ credential_id: string }>("/ai-assets/credentials", payload));
export const deleteManagedCredential = (credentialId: string) =>
  unwrap(
    api.delete<Record<string, unknown>>(
      `/ai-assets/credentials/${encodeURIComponent(credentialId)}`
    )
  );
export const fetchDailyReportAgentIntegration = () =>
  unwrap(api.get<DailyReportAgentIntegration>("/ai-assets/daily-report-integration"));
export const fetchManagedAgents = () =>
  unwrap(
    api.get<{ agents: ManagedAgent[] }>("/ai-assets/agents", undefined, { skipErrorHandler: true })
  );
export const fetchAvailableModels = () =>
  unwrap(
    api.get<AvailableModelsResponse>("/ai-assets/models", undefined, {
      skipErrorHandler: true
    })
  );
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
  unwrap(
    api.post<Record<string, unknown>>(`/ai-assets/agents/${encodeURIComponent(agentId)}/archive`, {
      archived
    })
  );
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
export const fetchReportSourceCandidates = (params: {
  report_type: "personal_daily" | "personal_weekly";
  period_start: string;
  period_end: string;
  q?: string;
  activity_from?: string;
  activity_to?: string;
  page?: string;
  page_size?: string;
}) => unwrap(api.get<ReportSourceCandidatePage>("/report-source-sessions", params));
export const fetchReportSourceCapability = () =>
  unwrap(api.get<{ enabled: boolean }>("/report-source-capability"));
export const createReportSourceSelection = (payload: {
  report_type: "personal_daily" | "personal_weekly";
  period: { date?: string; week_start?: string; week_end?: string };
  selected_slice_keys: string[];
}) => unwrap(api.post<ReportSourceSelection>("/report-source-selections", payload));
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
  unwrap(
    api.post<PreviewManagedAgentScheduleResponse>("/ai-assets/agent-schedules/preview", payload)
  );
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
) =>
  unwrap(
    api.post<AIRun>(`/ai-assets/agent-schedules/${scheduleId}/runs`, {
      trigger_source: triggerSource
    })
  );

// ───────────────────────── Tokens ─────────────────────────

export interface TeamSyncPath {
  id: string;
  normalized_path: string;
  created_at: string;
  updated_at: string;
}

export const fetchTeamSyncPaths = () =>
  unwrap(api.get<{ items: TeamSyncPath[] }>("/me/team-sync-paths"));

export const createTeamSyncPath = (path: string) =>
  unwrap(api.post<TeamSyncPath>("/me/team-sync-paths", { path }));

export const updateTeamSyncPath = (id: string, path: string) =>
  unwrap(api.put<TeamSyncPath>(`/me/team-sync-paths/${id}`, { path }));

export const deleteTeamSyncPath = (id: string) =>
  unwrap(api.delete<void>(`/me/team-sync-paths/${id}`));

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
  query_snapshot_token?: string;
}) => unwrap(api.get<PaginatedSessionTokens>("/tokens/sessions", params));

export async function fetchAllSessionTokens(params: {
  from?: string;
  to?: string;
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
      page_size: String(pageSize),
      ...(firstPage.query_snapshot_token
        ? { query_snapshot_token: firstPage.query_snapshot_token }
        : {})
    });
    items.push(...nextPage.items);
  }

  return items;
}

export const fetchTokenAnalyticsCapability = () =>
  unwrap(api.get<TokenAnalyticsCapability>("/token-analytics/capability"));

export const fetchTokenAnalyticsSummary = (params: TokenAnalyticsFilters) =>
  unwrap(api.get<TokenAnalyticsSummary>("/token-analytics/summary", params));

const tokenAnalyticsSnapshotRequestOptions = {
  silentErrorCodes: ["QUERY_SNAPSHOT_EXPIRED"]
} as const;

export const fetchTokenAnalyticsTrends = (
  params: TokenAnalyticsFilters & { query_snapshot_token: string }
) =>
  unwrap(
    api.get<{ query_snapshot_token: string; items: TokenAnalyticsTrendPoint[] }>(
      "/token-analytics/trends",
      params,
      tokenAnalyticsSnapshotRequestOptions
    )
  );

export const fetchTokenAnalyticsRankings = (
  params: TokenAnalyticsFilters & {
    query_snapshot_token: string;
    group_by: "department" | "team" | "user" | "model";
  }
) =>
  unwrap(
    api.get<{
      query_snapshot_token: string;
      group_by: string;
      items: TokenAnalyticsRankingItem[];
    }>("/token-analytics/rankings", params, tokenAnalyticsSnapshotRequestOptions)
  );

export const fetchTokenAnalyticsSessions = (
  params: TokenAnalyticsFilters & {
    query_snapshot_token: string;
    page: string;
    page_size: string;
  }
) =>
  unwrap(
    api.get<{
      query_snapshot_token: string;
      search_mode: "filtered" | "exact_session_ref";
      items: TokenAnalyticsSessionItem[];
      total: number;
      page: number;
      page_size: number;
    }>("/token-analytics/sessions", params, tokenAnalyticsSnapshotRequestOptions)
  );

export const fetchPriceBooks = () => unwrap(api.get<PriceBook[]>("/admin/price-books"));
export const savePriceBook = (payload: {
  id?: string;
  name: string;
  status: PriceBook["status"];
}) => unwrap(api.post<{ id: string }>("/admin/price-books", payload));

export const fetchModelAliases = () => unwrap(api.get<ModelAlias[]>("/admin/model-aliases"));
export const saveModelAlias = (payload: {
  provider: string;
  raw_model_pattern: string;
  canonical_model: string;
  status: ModelAlias["status"];
}) => unwrap(api.post<{ id: string }>("/admin/model-aliases", payload));

export const fetchModelPriceVersions = () =>
  unwrap(api.get<ModelPriceVersion[]>("/admin/model-price-versions"));
export const saveModelPriceVersion = (payload: Record<string, string | undefined>) =>
  unwrap(api.post<{ id: string }>("/admin/model-price-versions", payload));

export const fetchExchangeRateVersions = () =>
  unwrap(api.get<ExchangeRateVersion[]>("/admin/exchange-rate-versions"));
export const saveExchangeRateVersion = (payload: Record<string, string | undefined>) =>
  unwrap(api.post<{ id: string }>("/admin/exchange-rate-versions", payload));

export const fetchUnpricedModels = () =>
  unwrap(api.get<UnpricedModel[]>("/admin/pricing/unpriced-models"));

export const fetchPricingRecalculationRuns = () =>
  unwrap(api.get<PricingRecalculationRun[]>("/admin/pricing/recalculation-runs"));

export const previewPricingRecalculation = (payload: {
  from?: string;
  to?: string;
  model?: string;
}) =>
  unwrap(
    api.post<{
      eligible_components: number;
      priced_components: number;
      unpriced_components: number;
      changed_components: number;
      unchanged_components: number;
    }>("/admin/pricing/recalculate/preview", payload)
  );

export const applyPricingRecalculation = (payload: {
  from?: string;
  to?: string;
  model?: string;
  reason: string;
}) =>
  unwrap(
    api.post<{
      eligible_components: number;
      priced_components: number;
      unpriced_components: number;
      changed_components: number;
      unchanged_components: number;
    }>("/admin/pricing/recalculate/apply", payload)
  );
