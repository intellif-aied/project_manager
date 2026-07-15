import {
  BarChartOutlined,
  ClockCircleOutlined,
  EditOutlined,
  FileDoneOutlined,
  FileTextOutlined,
  LinkOutlined,
  RightOutlined,
  UnorderedListOutlined,
  WarningOutlined,
  UploadOutlined
} from "@ant-design/icons";
import {
  Alert,
  App,
  Button,
  Checkbox,
  Col,
  Empty,
  Input,
  Modal,
  Popconfirm,
  Popover,
  Row,
  Segmented,
  Select,
  Space,
  Steps,
  Tag,
  Upload
} from "antd";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import * as echarts from "echarts";
import type { ECharts, EChartsOption } from "echarts";
import type { ReactNode, UIEvent } from "react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import dayjs from "dayjs";

import { PagePanel } from "@/shared/components/PagePanel/PagePanel";
import { useAuth } from "@/shared/auth/authContext";
import type { UserRole } from "@/shared/auth/types";
import { isEditConflict } from "@/shared/request/apiError";
import { formatDateTime } from "@/shared/utils/dateTime";

import {
  fetchAllSessionTokens,
  fetchDepartmentReportSources,
  fetchDepartmentReportTodayOrNull,
  fetchDashboardMyItems,
  fetchPersonalWeeklyReportCurrentOrNull,
  fetchFollowFollowers,
  fetchDashboardRisks,
  fetchTask,
  fetchReports,
  fetchSessions,
  fetchTeamReportSources,
  fetchTeamReportTodayOrNull,
  fetchTodayReport,
  fetchTokens,
  updateDepartmentReport,
  updateReport as updateDailyReport,
  submitTeamReport,
  updateTeamReport,
  updateTask
} from "../api/client";
import type {
  DailyReport,
  DepartmentReport,
  DepartmentReportSources,
  AttentionLevel,
  DashboardFollowFollowerDTO,
  PersonalWeeklyReport,
  Session,
  TaskDependencyDTO,
  TaskProgressSuggestion as DraftTaskProgressSuggestion,
  TeamReport,
  TeamReportSources
} from "../api/types";
import {
  aggregateDashboardTokenReport,
  getDashboardTokenDateRange,
  type DashboardTokenRange,
  type DashboardTokenReport
} from "./dashboardTokenStats";
import { invalidateRequirementTaskWorkspace } from "../requirements/queryInvalidation";
import { requirementsBoardApi } from "../requirements/api/requirementsBoardApi";
import { RequirementDrawer, TaskDetailModal } from "../requirements/pages/RequirementsListPage";
import type {
  FavoriteTargetType,
  MockRequirement,
  MockTask,
  MockTokenSource,
  RequirementStage
} from "../requirements/types";
import {
  DailyReportGenerateModal,
  type DailyGenerateScope
} from "../reports/components/DailyReportGenerateModal";
import {
  DepartmentWeeklyReportModal,
  PersonalWeeklyReportModal,
  TeamWeeklyReportModal
} from "../reports/pages/WeeklyReportsPage";

import "./console-dashboard.css";

type DashboardRole = "employee" | "team_leader" | "director" | "pm";
type ReportStatus =
  | "待生成"
  | "生成中"
  | "草稿待确认"
  | "已保存"
  | "已发送"
  | "已保存，未发送最新修改"
  | "已归档"
  | "生成失败";
type ReportModalStep = "sessions" | "source" | "editor";
type ReportKind =
  | "personal_daily"
  | "personal_weekly"
  | "team_daily"
  | "team_weekly"
  | "department_daily"
  | "department_weekly";
type ReportScope = "personal" | "team" | "department";
type RiskTone = "red" | "orange" | "gold" | "blue";
type FollowType = "需求" | "任务";
type RiskType = "requirement_overdue" | "deadline" | "dependency_blocker" | "dependency_conflict";
type RiskRelatedObjectType = "requirement" | "task";
type TokenRange = DashboardTokenRange;
type ReportSkillOption = {
  label: string;
  value: string;
  source?: "system" | "upload";
  content?: string;
};
type DraftTaskStatus = "todo" | "in_progress" | "done";

interface SessionOption {
  tool: string;
  timeRange: string;
  summary: string;
  value: string;
  recommended: boolean;
}

interface FollowItem {
  key: string;
  type: FollowType;
  title: string;
  requirement?: string;
  requirementId: string;
  taskId?: string;
  creatorName?: string;
  requirementResponsibleNames?: string[];
  taskResponsibleNames?: string[];
  responsibleNames?: string[];
  owner?: string;
  status: string;
  deadline: string;
  risk: string;
  riskEvidence?: {
    primaryRisk?: RiskType;
    affectedTaskCount?: number;
    totalRiskCount?: number;
    samples?: Array<{
      taskId: string;
      taskTitle: string;
      riskTypes: RiskType[];
      blockingSources?: TaskDependencyDTO[];
      deadline?: string;
    }>;
  };
  dependency?: string;
  blockingTasks?: TaskDependencyDTO[];
  activity?: string;
  followedByMe?: boolean;
  createdByMe?: boolean;
  assignedToMe?: boolean;
  attentionScore?: number;
  attentionLevel?: AttentionLevel;
  followerCount?: number;
  riskPriority?: number;
}

interface RiskItem {
  key: string;
  displayType?: "requirement_group" | "single_task";
  riskType?: RiskType;
  riskTypes?: RiskType[];
  title?: string;
  source?: string;
  target?: string;
  relatedObjectType?: RiskRelatedObjectType;
  requirementId?: string;
  requirementTitle?: string;
  taskId?: string;
  requirementOverdue?: boolean;
  deadlineTaskCount?: number;
  dependencyBlockerCount?: number;
  dependencyConflictCount?: number;
  representativeTask?: {
    taskId: string;
    title: string;
    deadline?: string;
    riskTypes: RiskType[];
    blockingDependencies?: TaskDependencyDTO[];
    unfinishedDependencyCount?: number;
  };
  summary?: string;
  owner?: string;
  requirementResponsibleNames?: string[];
  deadline?: string;
  reason?: string;
  level?: "高" | "中" | "低";
  tone?: RiskTone;
  actionText?: string;
  targetUrl?: string;
  attentionScore?: number;
  attentionLevel?: AttentionLevel;
}

interface SummaryChipItem {
  label: string;
  value: number;
  tone?: "default" | RiskTone | "muted";
}

interface ReportCoverage {
  expected: number;
  submitted: number;
  missing: number;
  failed: number;
}

interface ReportItem {
  id: string;
  kind: ReportKind;
  scope: ReportScope;
  name: string;
  status: ReportStatus;
  description: string;
  sourceSummary: string;
  sessionCount: number;
  skill: string;
  updatedAt: string;
  nextAt?: string;
}

interface ConsoleRoleData {
  label: string;
  userLine: string;
  workCue: string;
  personalReports: ReportItem[];
  summaryReports?: ReportItem[];
  coverage: ReportCoverage;
  metrics: {
    focusCount: string;
    focusNote: string;
    riskCount: string;
    riskNote: string;
    dueCount: string;
    dueNote: string;
  };
  follows: FollowItem[];
  risks: RiskItem[];
}

type TokenReport = DashboardTokenReport;

const DASHBOARD_PREVIEW_LIMIT = 5;
const DASHBOARD_PAGE_SIZE = 20;

interface TaskProgressSuggestion {
  key: string;
  taskId: string;
  taskName: string;
  progress: number;
  status: DraftTaskStatus;
  sessionIds: string[];
  evidenceSessionTitles: string[];
  note: string;
  syncState?: "已修改" | "待同步";
}

const REPORT_SKILL_OPTIONS: ReportSkillOption[] = [
  { label: "默认日报 Skill", value: "default_daily", source: "system" }
];

const TOKEN_RANGE_OPTIONS: { label: string; value: TokenRange }[] = [
  { label: "昨天", value: "yesterday" },
  { label: "近 3 天", value: "last3days" },
  { label: "近 7 天", value: "last7days" }
];

const DEFAULT_MARKDOWN = `# 6 月 22 日日报

## 今日完成
* 收敛控制台首页信息架构，移除大盘式概览。
* 将日报生成入口调整为个人 session 生成个人日报。
* 梳理风险项、我关注的、日报 / 周报入口的页面层级。

## 风险与阻塞
* 飞书发送目标仍需确认，P0 先保留站内保存兜底。
* 需求看板尚未进入原型设计，风险定位入口暂为占位。

## 明日计划
* 继续完善控制台日报生成弹窗和 Markdown 编辑流程。
* 对齐需求看板定位规则。`;

function createReport(
  overrides: Omit<ReportItem, "sessionCount" | "skill" | "updatedAt"> & Partial<ReportItem>
): ReportItem {
  return {
    sessionCount: 0,
    skill: "默认日报 Skill",
    updatedAt: "-",
    ...overrides
  };
}

function applyTodayDailyReportState(
  report: ReportItem,
  dailyReport: DailyReport | undefined,
  loaded: boolean
): ReportItem {
  if (!loaded) return report;
  if (!dailyReport) {
    return {
      ...report,
      status: "待生成",
      sessionCount: 0,
      updatedAt: "-"
    };
  }

  let status: ReportStatus = "待生成";
  if (dailyReport.status === "submitted") {
    status = "已发送";
  } else if (dailyReport.status === "saved" && dailyReport.submitted_at) {
    status = "已保存，未发送最新修改";
  } else if (dailyReport.status === "saved") {
    status = "已保存";
  }

  return {
    ...report,
    status,
    sessionCount: dailyReport.session_ids.length,
    updatedAt: formatDateTime(dailyReport.updated_at, "HH:mm")
  };
}

function applyPersonalWeeklyReportState(
  report: ReportItem,
  weeklyReport: PersonalWeeklyReport | null | undefined,
  loaded: boolean
): ReportItem {
  if (!loaded) return report;
  if (!weeklyReport) {
    return {
      ...report,
      status: "待生成",
      sessionCount: 0,
      updatedAt: "-"
    };
  }
  return {
    ...report,
    status: weeklyReport.status === "submitted" ? "已发送" : "已保存",
    sessionCount: weeklyReport.source_session_ids.length,
    updatedAt: formatDateTime(weeklyReport.updated_at, "HH:mm")
  };
}

function applyTeamDailyReportState(
  report: ReportItem,
  teamReport: TeamReport | null | undefined,
  loaded: boolean
): ReportItem {
  if (!loaded) return report;
  if (!teamReport) {
    return {
      ...report,
      status: "待生成",
      sessionCount: 0,
      updatedAt: "-"
    };
  }

  return {
    ...report,
    status:
      teamReport.status === "submitted"
        ? "已发送"
        : teamReport.status === "saved" && teamReport.submitted_at
          ? "已保存，未发送最新修改"
          : "已保存",
    sessionCount: teamReport.source_daily_report_ids.length || teamReport.member_report_ids.length,
    skill: "小组日报 Agent",
    updatedAt: formatDateTime(teamReport.updated_at, "HH:mm")
  };
}

function applyDepartmentDailyReportState(
  report: ReportItem,
  departmentReport: DepartmentReport | null | undefined,
  loaded: boolean
): ReportItem {
  if (!loaded) return report;
  if (!departmentReport || !departmentReport.id) {
    return {
      ...report,
      status: "待生成",
      sessionCount: 0,
      updatedAt: "-"
    };
  }

  return {
    ...report,
    status:
      departmentReport.status === "saved" ||
      departmentReport.status === "archived" ||
      departmentReport.archived_at
        ? "已保存"
        : "待生成",
    sessionCount: departmentReport.source_team_report_ids.length,
    skill: "部门日报 Agent",
    updatedAt: formatDateTime(departmentReport.updated_at, "HH:mm")
  };
}

const ROLE_DATA: Record<DashboardRole, ConsoleRoleData> = {
  employee: {
    label: "个人",
    userLine: "陈一 · 前端工程师",
    workCue: "今天有 1 个阻塞任务，日报还没有发送。",
    personalReports: [
      createReport({
        id: "employee-personal-daily",
        kind: "personal_daily",
        scope: "personal",
        name: "今日日报",
        status: "草稿待确认",
        description: "今日日报已有内容，可继续编辑保存。",
        sourceSummary: "个人当日 session + 用户当天相关任务/需求状态",
        sessionCount: 2,
        updatedAt: "18:42",
        nextAt: "19:00"
      }),
      createReport({
        id: "employee-personal-weekly",
        kind: "personal_weekly",
        scope: "personal",
        name: "本周周报",
        status: "待生成",
        description: "暂无本周周报，可直接填写。",
        sourceSummary: "本周个人日报、个人工作记录、风险与阻塞",
        updatedAt: "-"
      })
    ],
    coverage: { expected: 1, submitted: 0, missing: 1, failed: 0 },
    metrics: {
      focusCount: "4",
      focusNote: "我负责或主动关注的任务",
      riskCount: "3",
      riskNote: "1 个阻塞，2 个超期",
      dueCount: "2",
      dueNote: "已超过截止日期"
    },
    follows: [
      {
        key: "employee-task-1",
        type: "任务",
        title: "补充日报生成验收标准",
        requirement: "AI 日报生成",
        requirementId: "req-ai-daily",
        taskId: "task-daily-ac",
        owner: "我",
        status: "进行中",
        deadline: "2026-06-23",
        dependency: "无阻塞依赖",
        risk: "已超期",
        activity: "验收口径刚更新"
      },
      {
        key: "employee-task-2",
        type: "任务",
        title: "飞书发送联调",
        requirement: "日报发送",
        requirementId: "req-daily-send",
        taskId: "task-feishu-integration",
        owner: "我",
        status: "阻塞",
        deadline: "2026-06-25",
        dependency: "依赖：发送目标确认",
        risk: "依赖阻塞",
        activity: "上游任务已超期"
      },
      {
        key: "employee-task-3",
        type: "任务",
        title: "整理工作记录解析异常样例",
        requirement: "Session 导入",
        requirementId: "req-session-import",
        taskId: "task-session-samples",
        owner: "我",
        status: "未开始",
        deadline: "2026-06-28",
        dependency: "依赖：解析规则确认",
        risk: "依赖未完成",
        activity: "等待样例补充"
      }
    ],
    risks: [
      {
        key: "employee-risk-1",
        riskType: "dependency_blocker",
        title: "飞书发送联调等待上游任务完成",
        source: "依赖阻塞",
        target: "日报发送 / 飞书发送联调",
        relatedObjectType: "task",
        requirementId: "req-daily-send",
        taskId: "task-feishu-integration",
        owner: "我",
        deadline: "2026-06-25",
        reason: "上游任务「发送目标确认」已超期",
        level: "高",
        tone: "red",
        actionText: "查看依赖",
        targetUrl:
          "/requirements?requirementId=req-daily-send&taskId=task-feishu-integration&focus=dependency"
      },
      {
        key: "employee-risk-2",
        riskType: "deadline",
        title: "日报生成验收标准已超过截止日期",
        source: "已超期",
        target: "AI 日报生成 / 补充验收标准",
        relatedObjectType: "task",
        requirementId: "req-ai-daily",
        taskId: "task-daily-ac",
        owner: "我",
        deadline: "2026-06-23",
        reason: "任务尚未完成，需要更新计划或推进状态",
        level: "中",
        tone: "orange",
        actionText: "查看任务",
        targetUrl: "/requirements?requirementId=req-ai-daily&taskId=task-daily-ac&focus=deadline"
      }
    ]
  },
  team_leader: {
    label: "TL",
    userLine: "李雷 · Aida 前端组 TL",
    workCue: "组内 2 人日报未发送，1 个阻塞任务影响下游。",
    personalReports: [
      createReport({
        id: "tl-personal-daily",
        kind: "personal_daily",
        scope: "personal",
        name: "今日日报",
        status: "草稿待确认",
        description: "今日日报已有内容，可继续编辑保存。",
        sourceSummary: "个人当日 session + 用户当天相关任务/需求状态",
        sessionCount: 2,
        updatedAt: "18:30",
        nextAt: "19:00"
      }),
      createReport({
        id: "tl-personal-weekly",
        kind: "personal_weekly",
        scope: "personal",
        name: "本周周报",
        status: "草稿待确认",
        description: "本周周报已有内容，可继续编辑保存。",
        sourceSummary: "本周个人日报、个人工作记录、风险与阻塞",
        updatedAt: "17:40"
      })
    ],
    summaryReports: [
      createReport({
        id: "tl-team-daily",
        kind: "team_daily",
        scope: "team",
        name: "今日组日报",
        status: "待生成",
        description: "暂无小组日报，可直接填写。",
        sourceSummary: "成员当天原始日报",
        updatedAt: "-"
      }),
      createReport({
        id: "tl-team-weekly",
        kind: "team_weekly",
        scope: "team",
        name: "本周组周报",
        status: "待生成",
        description: "暂无小组周报，可直接填写。",
        sourceSummary:
          "组内成员本周个人日报、个人周报、需求看板数据、本周风险与阻塞、完成/延期/下周计划",
        updatedAt: "-"
      })
    ],
    coverage: { expected: 8, submitted: 6, missing: 2, failed: 0 },
    metrics: {
      focusCount: "11",
      focusNote: "5 个需求，6 个任务",
      riskCount: "7",
      riskNote: "3 个阻塞，2 个超期",
      dueCount: "5",
      dueNote: "本周到期任务"
    },
    follows: [
      {
        key: "tl-req-1",
        type: "需求",
        title: "AI 日报生成",
        requirementId: "req-ai-daily",
        owner: "李雷",
        status: "进行中",
        deadline: "2026-06-30",
        dependency: "2 个强依赖",
        risk: "依赖阻塞",
        activity: "任务进度 62%"
      },
      {
        key: "tl-task-1",
        type: "任务",
        title: "解析 Claude Code session",
        requirement: "Session 导入",
        requirementId: "req-session-import",
        taskId: "task-session-parser",
        owner: "韩梅梅",
        status: "阻塞",
        deadline: "2026-06-24",
        dependency: "依赖：字段冻结",
        risk: "已超期",
        activity: "影响导入联调"
      },
      {
        key: "tl-task-2",
        type: "任务",
        title: "日报编辑态",
        requirement: "AI 日报生成",
        requirementId: "req-ai-daily",
        taskId: "task-daily-editor",
        owner: "王强",
        status: "进行中",
        deadline: "2026-06-27",
        dependency: "依赖：默认 Skill 输出",
        risk: "已超期",
        activity: "日报内容待确认"
      }
    ],
    risks: [
      {
        key: "tl-risk-1",
        riskType: "deadline",
        title: "本组有 2 个任务已超过 deadline",
        source: "已超期",
        target: "Session 导入 / 解析 Claude Code session",
        relatedObjectType: "task",
        requirementId: "req-session-import",
        taskId: "task-session-parser",
        owner: "韩梅梅",
        deadline: "2026-06-24",
        reason: "影响需求：Session 导入、需求看板原型",
        level: "高",
        tone: "red",
        actionText: "查看任务",
        targetUrl:
          "/requirements?requirementId=req-session-import&taskId=task-session-parser&focus=deadline"
      },
      {
        key: "tl-risk-2",
        riskType: "dependency_blocker",
        title: "日报发送联调等待接口任务完成",
        source: "依赖阻塞",
        target: "日报发送 / 飞书发送联调",
        relatedObjectType: "task",
        requirementId: "req-daily-send",
        taskId: "task-feishu-integration",
        owner: "李雷",
        deadline: "2026-06-25",
        reason: "上游发送接口任务已超期，影响当前联调任务",
        level: "高",
        tone: "red",
        actionText: "查看依赖",
        targetUrl:
          "/requirements?requirementId=req-daily-send&taskId=task-feishu-integration&focus=dependency"
      }
    ]
  },
  director: {
    label: "总监",
    userLine: "赵敏 · 研发总监",
    workCue: "部门日报提交率 78%，4 个高优先级风险需要下钻。",
    personalReports: [
      createReport({
        id: "director-personal-daily",
        kind: "personal_daily",
        scope: "personal",
        name: "今日日报",
        status: "已保存",
        description: "今日日报已保存，可随时打开查看。",
        sourceSummary: "个人当日 session + 用户当天相关任务/需求状态",
        sessionCount: 1,
        updatedAt: "17:55"
      }),
      createReport({
        id: "director-personal-weekly",
        kind: "personal_weekly",
        scope: "personal",
        name: "本周个人周报",
        status: "草稿待确认",
        description: "本周个人周报已有内容，可继续编辑保存。",
        sourceSummary: "本周个人日报、个人工作记录、风险与阻塞",
        updatedAt: "17:20"
      })
    ],
    summaryReports: [
      createReport({
        id: "director-department-daily",
        kind: "department_daily",
        scope: "department",
        name: "今日部门日报",
        status: "待生成",
        description: "暂无部门日报，可直接填写。",
        sourceSummary: "系统汇总的报告上下文、部门重点需求、高优先级风险、跨组依赖和阻塞",
        updatedAt: "18:05"
      }),
      createReport({
        id: "director-department-weekly",
        kind: "department_weekly",
        scope: "department",
        name: "本周部门周报",
        status: "待生成",
        description: "暂无部门周报，可直接填写。",
        sourceSummary: "各组周报、各组日报摘要、部门重点需求状态、高风险事项、资源/依赖/交付风险",
        updatedAt: "-"
      })
    ],
    coverage: { expected: 32, submitted: 25, missing: 6, failed: 1 },
    metrics: {
      focusCount: "16",
      focusNote: "重点需求",
      riskCount: "12",
      riskNote: "4 个高优先级",
      dueCount: "6",
      dueNote: "本周关键交付"
    },
    follows: [
      {
        key: "director-req-1",
        type: "需求",
        title: "AI 日报生成",
        requirementId: "req-ai-daily",
        owner: "李雷",
        status: "进行中",
        deadline: "2026-06-30",
        dependency: "2 个强依赖",
        risk: "依赖阻塞",
        activity: "关注需求，进度 62%"
      },
      {
        key: "director-req-2",
        type: "需求",
        title: "需求任务树重构",
        requirementId: "req-task-tree",
        owner: "周芷若",
        status: "进行中",
        deadline: "2026-07-05",
        dependency: "依赖：详情抽屉保存",
        risk: "已超期",
        activity: "关注需求，2 个任务延期"
      },
      {
        key: "director-req-3",
        type: "需求",
        title: "日报发送兜底方案",
        requirementId: "req-daily-fallback",
        owner: "王强",
        status: "未开始",
        deadline: "2026-07-10",
        dependency: "依赖：飞书目标确认",
        risk: "目标待定",
        activity: "关注需求，等待范围确认"
      }
    ],
    risks: [
      {
        key: "director-risk-1",
        riskType: "dependency_blocker",
        title: "关注需求「AI 日报生成」存在依赖阻塞",
        source: "依赖阻塞",
        target: "AI 日报生成",
        relatedObjectType: "requirement",
        requirementId: "req-ai-daily",
        owner: "李雷",
        deadline: "2026-06-30",
        reason: "影响任务：日报发送联调",
        level: "高",
        tone: "red",
        actionText: "查看依赖",
        targetUrl:
          "/requirements?requirementId=req-ai-daily&taskId=task-daily-send&focus=dependency"
      },
      {
        key: "director-risk-2",
        riskType: "deadline",
        title: "关注需求「需求任务树重构」存在延期风险",
        source: "已超期",
        target: "需求任务树重构",
        relatedObjectType: "requirement",
        requirementId: "req-task-tree",
        owner: "周芷若",
        deadline: "昨天",
        reason: "影响：2 个任务已超期",
        level: "高",
        tone: "red",
        actionText: "查看需求",
        targetUrl: "/requirements?requirementId=req-task-tree&focus=deadline"
      }
    ]
  },
  pm: {
    label: "PM",
    userLine: "周芷若 · 平台 PM",
    workCue: "2 个需求缺少 AC，日报发送目标仍待确认。",
    personalReports: [
      createReport({
        id: "pm-personal-daily",
        kind: "personal_daily",
        scope: "personal",
        name: "今日日报",
        status: "待生成",
        description: "暂无日报，可直接填写。",
        sourceSummary: "个人当日 session + 用户当天相关任务/需求状态",
        updatedAt: "-"
      }),
      createReport({
        id: "pm-personal-weekly",
        kind: "personal_weekly",
        scope: "personal",
        name: "本周周报",
        status: "待生成",
        description: "暂无本周周报，可直接填写。",
        sourceSummary: "本周个人日报、个人工作记录、风险与阻塞",
        updatedAt: "-"
      })
    ],
    coverage: { expected: 1, submitted: 0, missing: 1, failed: 0 },
    metrics: {
      focusCount: "7",
      focusNote: "我关注的需求",
      riskCount: "5",
      riskNote: "2 个超期，1 个依赖",
      dueCount: "3",
      dueNote: "本周需求节点"
    },
    follows: [
      {
        key: "pm-req-1",
        type: "需求",
        title: "AI 日报生成",
        requirementId: "req-ai-daily",
        owner: "李雷",
        status: "进行中",
        deadline: "2026-06-30",
        dependency: "2 个强依赖",
        risk: "关键任务已超期",
        activity: "需求进度 62%"
      },
      {
        key: "pm-req-2",
        type: "需求",
        title: "日报发送",
        requirementId: "req-daily-send",
        owner: "王强",
        status: "未开始",
        deadline: "2026-07-10",
        dependency: "依赖：发送目标确认",
        risk: "依赖待确认",
        activity: "需求尚未拆完"
      },
      {
        key: "pm-task-1",
        type: "任务",
        title: "补齐验收标准模板",
        requirement: "AI 日报生成",
        requirementId: "req-ai-daily",
        taskId: "task-ac-template",
        owner: "韩梅梅",
        status: "进行中",
        deadline: "2026-06-26",
        dependency: "无阻塞依赖",
        risk: "已超期",
        activity: "模板评审待完成"
      }
    ],
    risks: [
      {
        key: "pm-risk-1",
        riskType: "dependency_blocker",
        title: "AI 日报生成存在依赖阻塞任务",
        source: "依赖阻塞",
        target: "AI 日报生成 / 飞书发送联调",
        relatedObjectType: "requirement",
        requirementId: "req-ai-daily",
        taskId: "task-feishu-integration",
        owner: "李雷",
        deadline: "2026-06-30",
        reason: "影响：日报发送联调",
        level: "高",
        tone: "red",
        actionText: "查看依赖",
        targetUrl:
          "/requirements?requirementId=req-ai-daily&taskId=task-feishu-integration&focus=dependency"
      },
      {
        key: "pm-risk-2",
        riskType: "deadline",
        title: "AI 日报生成存在关键任务超期",
        source: "已超期",
        target: "AI 日报生成 / 补齐验收标准模板",
        relatedObjectType: "requirement",
        requirementId: "req-ai-daily",
        taskId: "task-ac-template",
        owner: "韩梅梅",
        deadline: "2026-06-26",
        reason: "关键任务「补齐验收标准模板」已超过截止日期",
        level: "中",
        tone: "orange",
        actionText: "查看需求",
        targetUrl: "/requirements?requirementId=req-ai-daily&view=risks"
      }
    ]
  }
};

export function DashboardPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { user } = useAuth();
  const { message } = App.useApp();
  const today = dayjs();
  const reportDate = today.format("YYYY-MM-DD");
  const weekStart = today.subtract((today.day() + 6) % 7, "day").format("YYYY-MM-DD");
  const weekEnd = dayjs(weekStart).add(6, "day").format("YYYY-MM-DD");
  const currentUserId = user?.id ?? "";
  const followsQuery = useQuery({
    queryKey: ["dashboard", currentUserId, "my-items"],
    queryFn: () => fetchDashboardMyItems({ page: 1, page_size: DASHBOARD_PAGE_SIZE }),
    staleTime: 30_000
  });
  const risksQuery = useQuery({
    queryKey: ["dashboard", currentUserId, "risks"],
    queryFn: () => fetchDashboardRisks({ page: 1, page_size: DASHBOARD_PAGE_SIZE }),
    staleTime: 30_000
  });
  const boardRequirementsQuery = useQuery({
    queryKey: ["requirements-board", "requirements", "dashboard-cache", currentUserId],
    queryFn: () =>
      requirementsBoardApi.listRequirementsPage({
        scope: "mine",
        page: "1",
        page_size: "100"
      }),
    staleTime: 30_000
  });
  const boardTasksQuery = useQuery({
    queryKey: ["requirements-board", "tasks"],
    queryFn: () => requirementsBoardApi.listTasks(),
    staleTime: 30_000
  });
  const boardFavoritesQuery = useQuery({
    queryKey: ["requirements-board", "favorites"],
    queryFn: () => requirementsBoardApi.listFavorites(),
    staleTime: 60_000
  });
  const boardTokenSourcesQuery = useQuery({
    queryKey: ["requirements-board", "token-sources"],
    queryFn: () => requirementsBoardApi.listTokenSources(),
    staleTime: 60_000
  });
  const todayReportsQuery = useQuery({
    queryKey: ["reports", "dashboard-today", currentUserId, reportDate],
    queryFn: () =>
      fetchReports({ scope: "mine", from: reportDate, to: reportDate, page: "1", page_size: "1" }),
    staleTime: 30_000
  });
  const [isReportModalOpen, setIsReportModalOpen] = useState(false);
  const [reportModalStep, setReportModalStep] = useState<ReportModalStep>("sessions");
  const [activeReportId, setActiveReportId] = useState<string>("employee-personal-daily");
  const [selectedSessionIds, setSelectedSessionIds] = useState<string[]>([]);
  const [validatedDraftSessionIds, setValidatedDraftSessionIds] = useState<string[]>([]);
  const [sessionSelectionTouched, setSessionSelectionTouched] = useState(false);
  const [reportSkillDraft, setReportSkillDraft] = useState<string>(REPORT_SKILL_OPTIONS[0].value);
  const [uploadedReportSkills, setUploadedReportSkills] = useState<ReportSkillOption[]>([]);
  const [draftMarkdown, setDraftMarkdown] = useState(DEFAULT_MARKDOWN);
  const [draftMarkdownTouched, setDraftMarkdownTouched] = useState(false);
  const [teamDraft, setTeamDraft] = useState<TeamReport | null>(null);
  const [departmentDraft, setDepartmentDraft] = useState<DepartmentReport | null>(null);
  const [taskSuggestions, setTaskSuggestions] = useState<TaskProgressSuggestion[]>([]);
  const [draftError, setDraftError] = useState<string | null>(null);
  const [editingTaskKey, setEditingTaskKey] = useState<string | null>(null);
  const [editingTaskDraft, setEditingTaskDraft] = useState<TaskProgressSuggestion | null>(null);
  const [tokenRange, setTokenRange] = useState<TokenRange>("last3days");
  const [dailyGenerateTarget, setDailyGenerateTarget] = useState<{
    scope: DailyGenerateScope;
    reportDate?: string;
    allowDateSwitch?: boolean;
  } | null>(null);
  const [weeklyMineOpen, setWeeklyMineOpen] = useState(false);
  const [teamWeeklyOpen, setTeamWeeklyOpen] = useState(false);
  const [departmentWeeklyOpen, setDepartmentWeeklyOpen] = useState(false);
  const [followExpanded, setFollowExpanded] = useState(false);
  const [riskExpanded, setRiskExpanded] = useState(false);
  const [loadedFollowItems, setLoadedFollowItems] = useState<FollowItem[]>([]);
  const [loadedRiskItems, setLoadedRiskItems] = useState<RiskItem[]>([]);
  const [followPage, setFollowPage] = useState(1);
  const [riskPage, setRiskPage] = useState(1);
  const [followLoadingMore, setFollowLoadingMore] = useState(false);
  const [riskLoadingMore, setRiskLoadingMore] = useState(false);
  const [dashboardRequirement, setDashboardRequirement] = useState<MockRequirement>();
  const [dashboardTask, setDashboardTask] = useState<MockTask>();
  const [dashboardTaskHistory, setDashboardTaskHistory] = useState<MockTask[]>([]);
  const [dashboardCreatorOpen, setDashboardCreatorOpen] = useState(false);
  const dashboardRole = getDashboardRole(user?.role);
  const data = useMemo(() => ROLE_DATA[dashboardRole], [dashboardRole]);
  const personalWeeklyQuery = useQuery({
    queryKey: ["reports", "weekly", "mine", "dashboard-current", currentUserId, weekStart],
    queryFn: () => fetchPersonalWeeklyReportCurrentOrNull(weekStart),
    staleTime: 30_000
  });
  const departmentSourcesQuery = useQuery({
    queryKey: ["department-report-sources", currentUserId, reportDate],
    queryFn: () => fetchDepartmentReportSources(reportDate),
    enabled: dashboardRole === "director",
    staleTime: 30_000
  });
  const teamSourcesQuery = useQuery({
    queryKey: ["team-report-sources", currentUserId, reportDate],
    queryFn: () => fetchTeamReportSources(reportDate),
    enabled: dashboardRole === "team_leader",
    staleTime: 30_000
  });
  const teamReportQuery = useQuery({
    queryKey: ["team-report-today", currentUserId, reportDate],
    queryFn: () => fetchTeamReportTodayOrNull(reportDate),
    enabled: dashboardRole === "team_leader",
    staleTime: 30_000
  });
  const departmentReportQuery = useQuery({
    queryKey: ["department-report-today", currentUserId, reportDate],
    queryFn: () => fetchDepartmentReportTodayOrNull(reportDate),
    enabled: dashboardRole === "director",
    staleTime: 30_000
  });
  const todayDailyReport = todayReportsQuery.data?.[0];
  const personalReports = data.personalReports.map((reportItem) => {
    if (reportItem.kind === "personal_weekly") {
      return applyPersonalWeeklyReportState(
        reportItem,
        personalWeeklyQuery.data,
        personalWeeklyQuery.isSuccess
      );
    }
    if (reportItem.kind !== "personal_daily") return reportItem;
    return applyTodayDailyReportState(reportItem, todayDailyReport, todayReportsQuery.isSuccess);
  });
  const summaryReports = (data.summaryReports ?? [])
    .filter(
      (reportItem) =>
        reportItem.kind === "team_daily" ||
        reportItem.kind === "department_daily" ||
        reportItem.kind === "team_weekly" ||
        reportItem.kind === "department_weekly"
    )
    .map((reportItem) => {
      if (reportItem.kind === "team_daily") {
        return applyTeamDailyReportState(
          reportItem,
          teamReportQuery.data,
          teamReportQuery.isSuccess
        );
      }
      if (reportItem.kind === "department_daily") {
        return applyDepartmentDailyReportState(
          reportItem,
          departmentReportQuery.data,
          departmentReportQuery.isSuccess
        );
      }
      return reportItem;
    });
  const effectiveCoverage =
    dashboardRole === "director" && departmentSourcesQuery.data
      ? {
          expected: departmentSourcesQuery.data.total_team_count,
          submitted: departmentSourcesQuery.data.submitted_team_count,
          missing: departmentSourcesQuery.data.missing_teams.length,
          failed: 0
        }
      : dashboardRole === "team_leader" && teamSourcesQuery.data
        ? {
            expected: teamSourcesQuery.data.members.length,
            submitted: teamSourcesQuery.data.submitted,
            missing: teamSourcesQuery.data.missing,
            failed: 0
          }
        : data.coverage;
  const dailyReport =
    personalReports.find((reportItem) => reportItem.kind === "personal_daily") ??
    personalReports[0];
  const availableReportIds = new Set(
    [...personalReports, ...summaryReports].map((reportItem) => reportItem.id)
  );
  const allVisibleReports = [...personalReports, ...summaryReports];
  const activeReport = availableReportIds.has(activeReportId)
    ? (allVisibleReports.find((reportItem) => reportItem.id === activeReportId) ?? dailyReport)
    : dailyReport;
  const tokenDateRange = useMemo(() => getDashboardTokenDateRange(tokenRange), [tokenRange]);
  const tokenScope =
    dashboardRole === "team_leader" || dashboardRole === "director" ? "team" : "mine";
  const shouldLoadMineTokens = dashboardRole === "team_leader" || dashboardRole === "director";
  const shouldLoadTeamTokenGroups = dashboardRole === "director";
  const tokenSessionsQuery = useQuery({
    queryKey: [
      "dashboard",
      currentUserId,
      "token-sessions",
      tokenDateRange.from,
      tokenDateRange.to,
      tokenScope
    ],
    queryFn: () =>
      fetchAllSessionTokens({
        from: tokenDateRange.from,
        to: tokenDateRange.to,
        scope: tokenScope
      }),
    staleTime: 60_000
  });
  const mineTokenSessionsQuery = useQuery({
    queryKey: [
      "dashboard",
      currentUserId,
      "token-sessions",
      tokenDateRange.from,
      tokenDateRange.to,
      "mine"
    ],
    queryFn: () =>
      fetchAllSessionTokens({ from: tokenDateRange.from, to: tokenDateRange.to, scope: "mine" }),
    enabled: shouldLoadMineTokens,
    staleTime: 60_000
  });
  const teamTokenGroupsQuery = useQuery({
    queryKey: [
      "dashboard",
      currentUserId,
      "token-groups",
      tokenDateRange.from,
      tokenDateRange.to,
      "team"
    ],
    queryFn: () =>
      fetchTokens({
        period: "range",
        from: tokenDateRange.from,
        to: tokenDateRange.to,
        group_by: "team"
      }),
    enabled: shouldLoadTeamTokenGroups,
    staleTime: 60_000
  });
  const reportSkillOptions = useMemo(
    () => [...REPORT_SKILL_OPTIONS, ...uploadedReportSkills],
    [uploadedReportSkills]
  );
  const tokenReport = useMemo(
    () =>
      aggregateDashboardTokenReport(tokenSessionsQuery.data ?? [], tokenDateRange, {
        mineSessions: shouldLoadMineTokens ? (mineTokenSessionsQuery.data ?? []) : undefined,
        teamAggregation: shouldLoadTeamTokenGroups ? (teamTokenGroupsQuery.data ?? null) : null,
        showUploaders: dashboardRole === "team_leader" || dashboardRole === "director"
      }),
    [
      dashboardRole,
      mineTokenSessionsQuery.data,
      shouldLoadMineTokens,
      shouldLoadTeamTokenGroups,
      teamTokenGroupsQuery.data,
      tokenDateRange,
      tokenSessionsQuery.data
    ]
  );
  const isTokenLoading =
    (tokenSessionsQuery.isLoading && !tokenSessionsQuery.data) ||
    (shouldLoadMineTokens && mineTokenSessionsQuery.isLoading && !mineTokenSessionsQuery.data) ||
    (shouldLoadTeamTokenGroups && teamTokenGroupsQuery.isLoading && !teamTokenGroupsQuery.data);
  const isTokenError =
    tokenSessionsQuery.isError ||
    (shouldLoadMineTokens && mineTokenSessionsQuery.isError) ||
    (shouldLoadTeamTokenGroups && teamTokenGroupsQuery.isError);
  const followItems = useMemo<FollowItem[]>(
    () => [...(followsQuery.data?.items ?? []), ...loadedFollowItems],
    [followsQuery.data, loadedFollowItems]
  );
  const riskItems = useMemo<RiskItem[]>(
    () => [...(risksQuery.data?.items ?? []), ...loadedRiskItems],
    [loadedRiskItems, risksQuery.data]
  );
  const followTotal = followsQuery.data?.total ?? followItems.length;
  const riskTotal = risksQuery.data?.total ?? riskItems.length;
  const followHasMore = followItems.length < followTotal;
  const riskHasMore = riskItems.length < riskTotal;
  const boardRequirements = useMemo(
    () => boardRequirementsQuery.data?.items ?? [],
    [boardRequirementsQuery.data]
  );
  const boardTasks = useMemo(() => boardTasksQuery.data ?? [], [boardTasksQuery.data]);
  const boardTokenSources = useMemo(
    () => boardTokenSourcesQuery.data ?? [],
    [boardTokenSourcesQuery.data]
  );
  const boardTokenSourceMap = useMemo(
    () => new Map<string, MockTokenSource>(boardTokenSources.map((source) => [source.id, source])),
    [boardTokenSources]
  );
  const requirementById = useMemo(
    () =>
      new Map<string, MockRequirement>(
        boardRequirements.map((requirement) => [requirement.id, requirement])
      ),
    [boardRequirements]
  );
  const taskById = useMemo(
    () => new Map<string, MockTask>(boardTasks.map((task) => [task.id, task])),
    [boardTasks]
  );
  const boardFavorites = useMemo(() => boardFavoritesQuery.data ?? [], [boardFavoritesQuery.data]);
  const favoriteRequirementIds = useMemo(
    () =>
      new Set(
        boardFavorites
          .filter((item) => item.target_type === "requirement")
          .map((item) => item.target_id)
      ),
    [boardFavorites]
  );
  const favoriteTaskIds = useMemo(
    () =>
      new Set(
        boardFavorites.filter((item) => item.target_type === "task").map((item) => item.target_id)
      ),
    [boardFavorites]
  );
  const tasksByRequirement = useMemo(() => {
    const result = new Map<string, MockTask[]>();
    boardTasks.forEach((task) => {
      const list = result.get(task.requirement_id) ?? [];
      list.push(task);
      result.set(task.requirement_id, list);
    });
    return result;
  }, [boardTasks]);
  const activeDashboardRequirement = dashboardRequirement
    ? (boardRequirements.find((item) => item.id === dashboardRequirement.id) ??
      dashboardRequirement)
    : undefined;
  const activeDashboardTask = dashboardTask
    ? (boardTasks.find((item) => item.id === dashboardTask.id) ?? dashboardTask)
    : undefined;
  const prioritizedFollowItems = useMemo(
    () =>
      followItems
        .map((item, index) => ({ item, index }))
        .sort(
          (left, right) =>
            compareDashboardMyItems(left.item, right.item) || left.index - right.index
        )
        .map(({ item }) => item),
    [followItems]
  );
  const prioritizedRiskItems = useMemo(
    () =>
      riskItems
        .map((item, index) => ({ item, index }))
        .sort(
          (left, right) => compareDashboardRisks(left.item, right.item) || left.index - right.index
        )
        .map(({ item }) => item),
    [riskItems]
  );
  const previewFollowItems = prioritizedFollowItems.slice(0, DASHBOARD_PREVIEW_LIMIT);
  const previewRiskItems = prioritizedRiskItems.slice(0, DASHBOARD_PREVIEW_LIMIT);
  const visibleFollowItems = followExpanded ? prioritizedFollowItems : previewFollowItems;
  const visibleRiskItems = riskExpanded ? prioritizedRiskItems : previewRiskItems;
  const followSummaryChips = useMemo(() => getFollowSummaryChips(followItems), [followItems]);
  const riskSummaryChips = useMemo(() => getRiskSummaryChips(riskItems), [riskItems]);
  const loadMoreFollowItems = async () => {
    if (!followExpanded) {
      setFollowExpanded(true);
      return;
    }
    if (!followHasMore || followLoadingMore) return;
    const nextPage = followPage + 1;
    setFollowLoadingMore(true);
    try {
      const payload = await fetchDashboardMyItems({
        page: nextPage,
        page_size: DASHBOARD_PAGE_SIZE
      });
      setLoadedFollowItems((current) => [...current, ...payload.items]);
      setFollowPage(nextPage);
    } catch (error) {
      message.error(error instanceof Error ? error.message : "我的事项加载失败");
    } finally {
      setFollowLoadingMore(false);
    }
  };
  const handleFollowMoreToggle = () => {
    if (followExpanded) {
      setFollowExpanded(false);
      return;
    }
    void loadMoreFollowItems();
  };
  const loadMoreRiskItems = async () => {
    if (!riskExpanded) {
      setRiskExpanded(true);
      return;
    }
    if (!riskHasMore || riskLoadingMore) return;
    const nextPage = riskPage + 1;
    setRiskLoadingMore(true);
    try {
      const payload = await fetchDashboardRisks({
        page: nextPage,
        page_size: DASHBOARD_PAGE_SIZE
      });
      setLoadedRiskItems((current) => [...current, ...payload.items]);
      setRiskPage(nextPage);
    } catch (error) {
      message.error(error instanceof Error ? error.message : "风险提示加载失败");
    } finally {
      setRiskLoadingMore(false);
    }
  };
  const handleRiskMoreToggle = () => {
    if (riskExpanded) {
      setRiskExpanded(false);
      return;
    }
    void loadMoreRiskItems();
  };
  const handleFollowListScroll = (event: UIEvent<HTMLDivElement>) => {
    if (!followExpanded || !followHasMore || followLoadingMore) return;
    const target = event.currentTarget;
    if (target.scrollTop + target.clientHeight >= target.scrollHeight - 24) {
      void loadMoreFollowItems();
    }
  };
  const handleRiskListScroll = (event: UIEvent<HTMLDivElement>) => {
    if (!riskExpanded || !riskHasMore || riskLoadingMore) return;
    const target = event.currentTarget;
    if (target.scrollTop + target.clientHeight >= target.scrollHeight - 24) {
      void loadMoreRiskItems();
    }
  };
  const modifiedTaskCount = taskSuggestions.filter((task) => task.syncState === "待同步").length;
  const reportSessionsQuery = useQuery({
    queryKey: ["dashboard", currentUserId, "daily-report-sessions", reportDate],
    queryFn: () =>
      fetchSessions({
        started_from: today.startOf("day").toISOString(),
        started_to: today.endOf("day").toISOString(),
        page: "1",
        page_size: "100"
      }),
    enabled: isReportModalOpen && activeReport.kind === "personal_daily",
    staleTime: 30_000
  });
  const reportSessionItems = reportSessionsQuery.data?.items;
  const reportSessions = useMemo(() => {
    const items = reportSessionItems ?? [];
    if (!currentUserId) return items;
    return items.filter((session) => session.user_id === currentUserId);
  }, [currentUserId, reportSessionItems]);
  const sessionOptions = useMemo(
    () => reportSessions.map((session) => toSessionOption(session)),
    [reportSessions]
  );
  const selectedSkill = reportSkillOptions.find((skill) => skill.value === reportSkillDraft);
  const effectiveSelectedSessionIds = sessionSelectionTouched
    ? selectedSessionIds
    : sessionOptions.map((session) => session.value);
  const handleSelectedSessionIdsChange = (value: string[]) => {
    setSessionSelectionTouched(true);
    setSelectedSessionIds(value);
  };
  const effectiveDraftMarkdown =
    activeReport.kind === "team_daily" &&
    reportModalStep === "editor" &&
    !draftMarkdownTouched &&
    draftMarkdown === "" &&
    teamReportQuery.data?.content
      ? teamReportQuery.data.content
      : draftMarkdown;
  const handleDraftMarkdownChange = (value: string) => {
    setDraftMarkdownTouched(true);
    setDraftMarkdown(value);
  };

  const updateReport = (...args: unknown[]) => {
    void args;
    // Dashboard 状态只来自接口；旧弹窗分支保留时不再写本地假状态。
  };

  const saveTeamMutation = useMutation({
    mutationFn: async ({ submit }: { submit: boolean }) => {
      const current = teamDraft ?? teamReportQuery.data;
      if (!current) {
        throw new Error("请先生成小组日报");
      }
      const saved = await updateTeamReport(current.id, { content: effectiveDraftMarkdown });
      return submit ? submitTeamReport(saved.id, { content: effectiveDraftMarkdown }) : saved;
    },
    onSuccess: (report, variables) => {
      setTeamDraft(report);
      setDraftMarkdown(report.content);
      updateReport(activeReport.id, {
        status:
          report.status === "submitted"
            ? "已发送"
            : report.status === "saved" && report.submitted_at
              ? "已保存，未发送最新修改"
              : "已保存",
        sessionCount: report.source_daily_report_ids.length || report.member_report_ids.length,
        skill: "小组日报 Agent",
        updatedAt: "刚刚"
      });
      queryClient.setQueryData(["team-report-today", reportDate], report);
      queryClient.setQueryData(["team-report-today"], report);
      void queryClient.invalidateQueries({ queryKey: ["team-report-today"] });
      void queryClient.invalidateQueries({ queryKey: ["team-report-sources"] });
      void queryClient.invalidateQueries({ queryKey: ["department-report-sources"] });
      message.success(variables.submit ? "已发送给总监" : "小组日报已保存");
      if (variables.submit) {
        setIsReportModalOpen(false);
      }
    },
    onError: (error: unknown) => {
      message.error(error instanceof Error ? error.message : "小组日报保存失败");
    }
  });

  const saveDepartmentMutation = useMutation({
    mutationFn: async ({ archive }: { archive: boolean }) => {
      const current = departmentDraft ?? departmentReportQuery.data;
      if (!current) {
        throw new Error("请先生成部门日报");
      }
      return {
        report: await updateDepartmentReport(current.id, { content: draftMarkdown }),
        archive
      };
    },
    onSuccess: ({ report, archive }) => {
      setDepartmentDraft(report);
      setDraftMarkdown(report.content);
      setDraftMarkdownTouched(false);
      updateReport(activeReport.id, {
        status: "已保存",
        sessionCount: report.source_team_report_ids.length,
        skill: "部门日报 Agent",
        updatedAt: "刚刚"
      });
      void queryClient.invalidateQueries({ queryKey: ["department-report-today"] });
      message.success("部门日报已保存");
      if (archive) {
        setIsReportModalOpen(false);
      }
    },
    onError: (error: unknown) => {
      message.error(error instanceof Error ? error.message : "部门日报保存失败");
    }
  });

  const saveReportMutation = useMutation({
    mutationFn: async ({ closeAfterSave }: { closeAfterSave: boolean }) => {
      const report = await fetchTodayReport();
      const sessionIDs =
        validatedDraftSessionIds.length > 0
          ? validatedDraftSessionIds
          : effectiveSelectedSessionIds;
      const saved = await updateDailyReport(report.id, {
        content: draftMarkdown,
        session_ids: sessionIDs
      });
      return { saved, closeAfterSave };
    },
    onSuccess: ({ saved, closeAfterSave }) => {
      message.success(closeAfterSave ? "日报已保存" : "日报修改已保存");
      updateReport(activeReport.id, {
        status: "草稿待确认",
        sessionCount: saved.session_ids.length,
        skill: saved.edited ? "默认日报 Skill" : activeReport.skill,
        updatedAt: "刚刚"
      });
      void queryClient.invalidateQueries({ queryKey: ["reports"] });
      if (closeAfterSave) {
        setIsReportModalOpen(false);
      }
    },
    onError: (error: unknown) => {
      message.error(error instanceof Error ? error.message : "日报保存失败");
    }
  });

  const applyTaskSuggestionMutation = useMutation({
    mutationFn: async (task: TaskProgressSuggestion) => {
      const current = await fetchTask(task.taskId);
      return updateTask(task.taskId, {
        status: task.status,
        progress: task.progress,
        base_version: current.version
      });
    },
    onSuccess: (_, task) => {
      message.success("任务进展已更新");
      setTaskSuggestions((current) =>
        current.map((item) =>
          item.key === task.key
            ? {
                ...task,
                syncState: "已修改"
              }
            : item
        )
      );
      setEditingTaskKey(null);
      setEditingTaskDraft(null);
      void invalidateRequirementTaskWorkspace(queryClient, { taskId: task.taskId });
    },
    onError: (error: unknown, task) => {
      if (isEditConflict(error)) {
        message.warning("内容已被其他人更新，请刷新后再操作");
        void invalidateRequirementTaskWorkspace(queryClient, { taskId: task.taskId });
        return;
      }
      message.error(error instanceof Error ? error.message : "任务更新失败");
    }
  });

  const openReportModal = (reportItem: ReportItem, step?: ReportModalStep) => {
    if (reportItem.kind === "personal_daily") {
      setDailyGenerateTarget({ scope: "personal", reportDate, allowDateSwitch: true });
      return;
    }
    if (reportItem.kind === "team_daily") {
      setDailyGenerateTarget({ scope: "team", reportDate, allowDateSwitch: true });
      return;
    }
    if (reportItem.kind === "department_daily") {
      setDailyGenerateTarget({ scope: "department", reportDate, allowDateSwitch: true });
      return;
    }
    if (reportItem.kind === "personal_weekly") {
      setWeeklyMineOpen(true);
      return;
    }
    if (reportItem.kind === "team_weekly") {
      setTeamWeeklyOpen(true);
      return;
    }
    if (reportItem.kind === "department_weekly") {
      setDepartmentWeeklyOpen(true);
      return;
    }
    void step;
  };

  const uploadReportSkill = (file: File) => {
    if (!file.name.toLowerCase().endsWith(".md")) {
      message.error("旧报告配置流程已停用");
      return false;
    }

    void file
      .text()
      .then((content) => {
        const uploadedSkillName = getUploadedSkillName(file.name, content);
        const uploadedSkillValue = `upload:${uploadedSkillName}`;
        setUploadedReportSkills((current) => {
          const next = current.filter((item) => item.value !== uploadedSkillValue);
          return [
            ...next,
            { label: uploadedSkillName, value: uploadedSkillValue, source: "upload", content }
          ];
        });
        setReportSkillDraft(uploadedSkillValue);
        message.success("Skill 已载入，本次生成将作为补充约束");
      })
      .catch(() => {
        message.error("Skill 文件读取失败");
      });

    return false;
  };

  const saveDraft = () => {
    if (!activeReport) return;
    if (activeReport.kind === "personal_daily") {
      saveReportMutation.mutate({ closeAfterSave: false });
      return;
    }
    if (activeReport.kind === "department_daily") {
      saveDepartmentMutation.mutate({ archive: false });
      return;
    }
    if (activeReport.kind === "team_daily") {
      saveTeamMutation.mutate({ submit: false });
      return;
    }
    updateReport(activeReport.id, { status: "草稿待确认", updatedAt: "刚刚" });
  };

  const goBackReportModalStep = () => {
    if (!activeReport) return;
    setReportModalStep("editor");
  };

  const sendReport = () => {
    if (!activeReport) return;
    if (activeReport.kind === "personal_daily") {
      saveReportMutation.mutate({ closeAfterSave: true });
      return;
    }
    if (activeReport.kind === "department_daily") {
      saveDepartmentMutation.mutate({ archive: true });
      return;
    }
    if (activeReport.kind === "team_daily") {
      saveTeamMutation.mutate({ submit: true });
      return;
    }
    updateReport(activeReport.id, { status: "已保存", updatedAt: "刚刚" });
    setIsReportModalOpen(false);
  };

  const openTaskEditModal = (task: TaskProgressSuggestion) => {
    setEditingTaskKey(task.key);
    setEditingTaskDraft({ ...task });
  };

  // Legacy report workflow is disabled for this round; Dashboard entries route to the report management modals above.
  void setActiveReportId;
  void draftError;
  void modifiedTaskCount;
  void handleSelectedSessionIdsChange;
  void handleDraftMarkdownChange;
  void uploadReportSkill;
  void saveDraft;
  void goBackReportModalStep;
  void sendReport;
  void openTaskEditModal;
  void getReportModalTitle;
  void getReportModalWidth;
  void getDefaultDraftMarkdown;
  void renderReportModalFooter;
  void ReportModalContent;

  const saveTaskEdit = () => {
    if (!editingTaskKey || !editingTaskDraft) return;
    applyTaskSuggestionMutation.mutate(editingTaskDraft);
  };

  const dashboardFavoriteMutation = useMutation({
    mutationFn: ({ targetType, targetId }: { targetType: FavoriteTargetType; targetId: string }) =>
      requirementsBoardApi.toggleFavorite(targetType, targetId),
    onSuccess: (result) => {
      message.success(result.favorited ? "已加入关注" : "已取消关注");
      void invalidateRequirementTaskWorkspace(queryClient);
    },
    onError: (error) => message.error(error instanceof Error ? error.message : "关注操作失败")
  });
  const dashboardRequirementStageMutation = useMutation({
    mutationFn: ({
      requirement,
      nextStatus
    }: {
      requirement: MockRequirement;
      nextStatus: RequirementStage;
    }) =>
      requirementsBoardApi.updateRequirementStage(requirement.id, nextStatus, requirement.version),
    onSuccess: (updated) => {
      setDashboardRequirement(updated);
      message.success("需求阶段已更新");
      void invalidateRequirementTaskWorkspace(queryClient, { requirementId: updated.id });
    },
    onError: (error) => message.error(error instanceof Error ? error.message : "需求阶段更新失败")
  });

  const openDashboardRequirementDetail = async (requirementId?: string) => {
    if (!requirementId) {
      message.warning("未找到关联需求");
      return false;
    }
    try {
      const requirement =
        boardRequirements.find((item) => item.id === requirementId) ??
        (await requirementsBoardApi.getRequirement(requirementId));
      setDashboardRequirement(requirement);
      setDashboardCreatorOpen(false);
      return true;
    } catch (error) {
      message.error(error instanceof Error ? error.message : "需求详情加载失败");
      return false;
    }
  };
  const openDashboardTaskDetail = async (
    taskId?: string,
    requirementId?: string,
    keepHistory = false
  ) => {
    if (!taskId) {
      return openDashboardRequirementDetail(requirementId);
    }
    try {
      const task =
        boardTasks.find((item) => item.id === taskId) ??
        (await requirementsBoardApi.getTask(taskId));
      if (keepHistory && activeDashboardTask && activeDashboardTask.id !== task.id) {
        setDashboardTaskHistory((current) => [...current, activeDashboardTask].slice(-12));
      } else if (!keepHistory) {
        setDashboardTaskHistory([]);
      }
      setDashboardTask(task);
      return true;
    } catch (error) {
      message.error(error instanceof Error ? error.message : "任务详情加载失败");
      return false;
    }
  };
  const returnPreviousDashboardTask = () => {
    const previousTask = dashboardTaskHistory[dashboardTaskHistory.length - 1];
    if (!previousTask) return;
    const latestTask = boardTasks.find((item) => item.id === previousTask.id) ?? previousTask;
    setDashboardTaskHistory((current) => current.slice(0, -1));
    setDashboardTask(latestTask);
  };
  const handleRiskAction = (risk: RiskItem) => {
    void (async () => {
      if (risk.displayType === "requirement_group") {
        if (risk.requirementId && (await openDashboardRequirementDetail(risk.requirementId)))
          return;
        const fallbackTaskId = risk.taskId ?? risk.representativeTask?.taskId;
        if (fallbackTaskId && (await openDashboardTaskDetail(fallbackTaskId, risk.requirementId)))
          return;
      } else {
        const taskId = risk.taskId ?? risk.representativeTask?.taskId;
        if (taskId && (await openDashboardTaskDetail(taskId, risk.requirementId))) return;
        if (risk.requirementId && (await openDashboardRequirementDetail(risk.requirementId)))
          return;
      }
      if (risk.targetUrl) {
        navigate(risk.targetUrl);
      }
    })();
  };

  const handleFollowAction = (item: FollowItem) => {
    void (async () => {
      if (item.type === "任务" && item.taskId) {
        await openDashboardTaskDetail(item.taskId, item.requirementId);
        return;
      }
      await openDashboardRequirementDetail(item.requirementId);
    })();
  };

  return (
    <PagePanel
      className="console-dashboard-page"
      bodyClassName="console-dashboard-page__body"
      title="控制台"
      description="查看报告状态、我的事项和需要处理的风险。"
      showNav={false}
    >
      <section className="console-dashboard">
        <div className="console-panel console-panel--follow">
          <PanelHeader
            icon={<UnorderedListOutlined />}
            title="我的事项"
            extra={<SummaryChips items={followSummaryChips} />}
          />
          {followItems.length > 0 ? (
            <>
              <div
                className={`console-follow-list console-dashboard-inline-list${followExpanded ? " is-expanded" : ""}`}
                onScroll={handleFollowListScroll}
              >
                {visibleFollowItems.map((item) => (
                  <FollowCard
                    key={item.key}
                    item={item}
                    requirement={requirementById.get(item.requirementId)}
                    task={item.taskId ? taskById.get(item.taskId) : undefined}
                    showAttention={dashboardRole !== "director"}
                    onView={handleFollowAction}
                  />
                ))}
              </div>
              {followTotal > DASHBOARD_PREVIEW_LIMIT ? (
                <InlineListMoreAction
                  expanded={followExpanded}
                  previewCount={
                    followExpanded ? visibleFollowItems.length : previewFollowItems.length
                  }
                  totalCount={followTotal}
                  loading={followLoadingMore}
                  onClick={handleFollowMoreToggle}
                />
              ) : null}
            </>
          ) : (
            <div className="console-report-status-card">
              <p>{followsQuery.isError ? "我的事项加载失败" : "暂无我的事项"}</p>
              <Button type="link" onClick={() => navigate("/requirements")}>
                前往需求看板查看
              </Button>
            </div>
          )}
        </div>

        <div className="console-panel console-panel--risk">
          <PanelHeader
            icon={<WarningOutlined />}
            title="我的风险提示"
            extra={<SummaryChips items={riskSummaryChips} />}
          />
          {riskItems.length > 0 ? (
            <>
              <div
                className={`console-risk-list console-dashboard-inline-list${riskExpanded ? " is-expanded" : ""}`}
                onScroll={handleRiskListScroll}
              >
                {visibleRiskItems.map((item) => (
                  <RiskCard
                    key={item.key}
                    item={item}
                    requirement={requirementById.get(item.requirementId ?? "")}
                    onAction={handleRiskAction}
                  />
                ))}
              </div>
              {riskTotal > DASHBOARD_PREVIEW_LIMIT ? (
                <InlineListMoreAction
                  expanded={riskExpanded}
                  previewCount={riskExpanded ? visibleRiskItems.length : previewRiskItems.length}
                  totalCount={riskTotal}
                  loading={riskLoadingMore}
                  onClick={handleRiskMoreToggle}
                />
              ) : null}
            </>
          ) : (
            <div className="console-report-status-card">
              <p>{risksQuery.isError ? "风险数据加载失败" : "暂无需要处理的风险"}</p>
              <Button type="link" onClick={() => navigate("/requirements")}>
                查看需求看板
              </Button>
            </div>
          )}
        </div>

        <Row className="console-dashboard-hero-row" gutter={[14, 14]} align="stretch">
          <Col className="console-dashboard-hero-row__report" xs={24} xl={12}>
            <ReportSection
              title="报告处理"
              icon={<FileDoneOutlined />}
              reports={personalReports}
              summaryReports={summaryReports}
              coverage={effectiveCoverage}
              variant="personal"
              onOpen={openReportModal}
              onViewReports={(scope) => navigate(`/reports/daily?tab=${scope}`)}
              onViewWeeklyReports={() => navigate("/reports/weekly")}
            />
          </Col>
          <Col className="console-dashboard-hero-row__token" xs={24} xl={12}>
            <SessionUploadCard
              role={dashboardRole}
              range={tokenRange}
              report={tokenReport}
              loading={isTokenLoading}
              error={isTokenError}
              onRangeChange={setTokenRange}
              onViewDetail={() => navigate("/tokens")}
            />
          </Col>
        </Row>
      </section>

      {dailyGenerateTarget ? (
        <DailyReportGenerateModal
          open
          scope={dailyGenerateTarget.scope}
          reportDate={dailyGenerateTarget.reportDate}
          allowDateSwitch={dailyGenerateTarget.allowDateSwitch}
          onClose={() => setDailyGenerateTarget(null)}
          onDone={() => {
            void queryClient.invalidateQueries({ queryKey: ["reports"] });
            void queryClient.invalidateQueries({
              queryKey: ["reports", "dashboard-today", reportDate]
            });
            void queryClient.invalidateQueries({ queryKey: ["team-report-today"] });
            void queryClient.invalidateQueries({ queryKey: ["department-report-today"] });
            void queryClient.invalidateQueries({ queryKey: ["team-report-sources"] });
            void queryClient.invalidateQueries({ queryKey: ["department-report-sources"] });
          }}
        />
      ) : null}

      <PersonalWeeklyReportModal
        open={weeklyMineOpen}
        weekStart={weekStart}
        weekEnd={weekEnd}
        allowWeekSwitch
        onClose={() => setWeeklyMineOpen(false)}
        onDone={() => {
          void queryClient.invalidateQueries({ queryKey: ["reports", "weekly"] });
          void queryClient.invalidateQueries({
            queryKey: ["reports", "weekly", "mine", "dashboard-current", currentUserId, weekStart]
          });
        }}
      />

      {teamWeeklyOpen ? (
        <TeamWeeklyReportModal
          open
          weekStart={weekStart}
          weekEnd={weekEnd}
          allowWeekSwitch
          onClose={() => setTeamWeeklyOpen(false)}
          onDone={() => {
            void queryClient.invalidateQueries({ queryKey: ["reports", "weekly"] });
          }}
        />
      ) : null}

      {departmentWeeklyOpen ? (
        <DepartmentWeeklyReportModal
          open
          weekStart={weekStart}
          weekEnd={weekEnd}
          allowWeekSwitch
          onClose={() => setDepartmentWeeklyOpen(false)}
          onDone={() => {
            void queryClient.invalidateQueries({ queryKey: ["reports", "weekly"] });
          }}
        />
      ) : null}

      <RequirementDrawer
        requirement={activeDashboardRequirement}
        tasks={
          activeDashboardRequirement
            ? (tasksByRequirement.get(activeDashboardRequirement.id) ?? [])
            : []
        }
        dependencyTasks={boardTasks}
        tokenSourceMap={boardTokenSourceMap}
        creatorOpen={dashboardCreatorOpen}
        isFavorite={
          activeDashboardRequirement
            ? favoriteRequirementIds.has(activeDashboardRequirement.id)
            : false
        }
        canManage={Boolean(
          activeDashboardRequirement?.can_update ||
            activeDashboardRequirement?.can_cancel ||
            activeDashboardRequirement?.can_restore ||
            activeDashboardRequirement?.can_delete
        )}
        canUpdateStatus={Boolean(activeDashboardRequirement?.can_change_status)}
        onUpdateStatus={(requirement, nextStatus) =>
          dashboardRequirementStageMutation.mutate({ requirement, nextStatus })
        }
        onToggleFavorite={
          activeDashboardRequirement
            ? () =>
                dashboardFavoriteMutation.mutate({
                  targetType: "requirement",
                  targetId: activeDashboardRequirement.id
                })
            : undefined
        }
        onCreatorOpenChange={setDashboardCreatorOpen}
        onClose={() => {
          setDashboardRequirement(undefined);
          setDashboardCreatorOpen(false);
        }}
        onSaved={(updated) => setDashboardRequirement(updated)}
        onOpenTask={(task) => {
          void openDashboardTaskDetail(task.id);
        }}
      />
      <TaskDetailModal
        task={activeDashboardTask}
        dependencyTasks={boardTasks}
        tokenSourceMap={boardTokenSourceMap}
        isFavorite={activeDashboardTask ? favoriteTaskIds.has(activeDashboardTask.id) : false}
        canManage={Boolean(
          activeDashboardTask?.can_update_meta ||
            activeDashboardTask?.can_update_status ||
            activeDashboardTask?.can_update_progress ||
            activeDashboardTask?.can_manage_dependencies ||
            activeDashboardTask?.can_delete
        )}
        onToggleFavorite={
          activeDashboardTask
            ? () =>
                dashboardFavoriteMutation.mutate({
                  targetType: "task",
                  targetId: activeDashboardTask.id
                })
            : undefined
        }
        canGoBack={dashboardTaskHistory.length > 0}
        onBackTask={returnPreviousDashboardTask}
        onOpenTask={(task) => {
          void openDashboardTaskDetail(task.id, task.requirement_id, true);
        }}
        onClose={() => {
          setDashboardTask(undefined);
          setDashboardTaskHistory([]);
        }}
        onSaved={(updated) => setDashboardTask(updated)}
        onDeleted={() => {
          setDashboardTask(undefined);
          setDashboardTaskHistory([]);
          void invalidateRequirementTaskWorkspace(queryClient);
        }}
      />

      <TaskProgressEditModal
        task={editingTaskDraft}
        open={Boolean(editingTaskDraft)}
        sessionOptions={sessionOptions}
        confirmLoading={applyTaskSuggestionMutation.isPending}
        onCancel={() => {
          setEditingTaskKey(null);
          setEditingTaskDraft(null);
        }}
        onChange={setEditingTaskDraft}
        onSave={saveTaskEdit}
      />
    </PagePanel>
  );
}

function PanelHeader({
  icon,
  title,
  extra
}: {
  icon: ReactNode;
  title: string;
  extra?: ReactNode;
}) {
  return (
    <div className="console-panel__header">
      <div>
        <span className="console-panel__icon">{icon}</span>
        <strong>{title}</strong>
      </div>
      {extra}
    </div>
  );
}

function SummaryChips({ items }: { items: SummaryChipItem[] }) {
  return (
    <div className="console-summary-chips" aria-label="数量摘要">
      {items.map((item) => (
        <span
          key={`${item.label}-${item.value}`}
          className={`console-summary-chip console-summary-chip--${item.tone ?? "default"}`}
        >
          {item.label === "全部" ? (
            <>
              <span>共</span>
              <strong>{item.value}</strong>
              <span>项</span>
            </>
          ) : (
            <>
              <span>{item.label}</span>
              <strong>{item.value}</strong>
            </>
          )}
        </span>
      ))}
    </div>
  );
}

function InlineListMoreAction({
  expanded,
  previewCount,
  totalCount,
  loading = false,
  onClick
}: {
  expanded: boolean;
  previewCount: number;
  totalCount: number;
  loading?: boolean;
  onClick: () => void;
}) {
  const hiddenCount = Math.max(0, totalCount - previewCount);
  const summary = expanded
    ? `已展开全部 ${totalCount} 项`
    : `已显示 ${previewCount} / ${totalCount} 项，另有 ${hiddenCount} 项未展示`;
  return (
    <div className="console-inline-list-more">
      <span className="console-inline-list-more__summary">{summary}</span>
      <Button
        type="link"
        className="console-inline-list-more__action"
        loading={loading}
        onClick={onClick}
      >
        {expanded ? "收起" : "展开全部"}
      </Button>
    </div>
  );
}

function ReportSection({
  title,
  icon,
  reports,
  summaryReports = [],
  coverage,
  variant,
  onOpen,
  onViewReports,
  onViewWeeklyReports
}: {
  title: string;
  icon: ReactNode;
  reports: ReportItem[];
  summaryReports?: ReportItem[];
  coverage?: ReportCoverage;
  variant: "personal" | "summary";
  onOpen: (report: ReportItem, step?: ReportModalStep) => void;
  onViewReports: (scope: ReportScope) => void;
  onViewWeeklyReports?: () => void;
}) {
  if (variant === "personal") {
    const dailyReport = reports.find((report) => report.kind === "personal_daily") ?? reports[0];
    const weeklyReport = reports.find((report) => report.kind === "personal_weekly");
    const summaryDailyReport = summaryReports.find(
      (report) => report.kind === "team_daily" || report.kind === "department_daily"
    );
    const summaryWeeklyReport = summaryReports.find(
      (report) => report.kind === "team_weekly" || report.kind === "department_weekly"
    );
    return (
      <div className="console-panel console-panel--daily">
        <PanelHeader icon={icon} title={title} />
        <div className="console-report-status-card console-report-status-card--personal">
          <div className="console-report-workbench">
            <section className="console-report-workbench-group" aria-label="今日处理">
              <div className="console-report-workbench-group__title">今日处理</div>
              <ReportTaskRow
                label="我的日报"
                report={dailyReport}
                description={getDailyReportCopy(dailyReport)}
                onOpen={onOpen}
              />
              {summaryDailyReport ? (
                <ReportSummaryInlineRow
                  report={summaryDailyReport}
                  meta={coverage ? getCoverageSummary(summaryDailyReport) : undefined}
                  onOpen={onOpen}
                />
              ) : null}
            </section>

            <section className="console-report-workbench-group" aria-label="本周处理">
              <div className="console-report-workbench-group__title">本周处理</div>
              {weeklyReport ? (
                <ReportWeeklyInlineRow report={weeklyReport} onOpen={onOpen} />
              ) : null}
              {summaryWeeklyReport ? (
                <ReportManagementWeeklyInlineRow report={summaryWeeklyReport} onOpen={onOpen} />
              ) : null}
            </section>
          </div>
          <div
            className="console-report-shortcuts console-report-shortcuts--list"
            aria-label="报告入口"
          >
            <button
              type="button"
              className="console-report-shortcut"
              onClick={() => onViewReports("personal")}
            >
              <span>
                <FileTextOutlined />
                <strong>日报记录</strong>
              </span>
              <RightOutlined />
            </button>
            <button
              type="button"
              className="console-report-shortcut"
              onClick={() =>
                onViewWeeklyReports
                  ? onViewWeeklyReports()
                  : onViewReports(summaryDailyReport?.scope ?? "personal")
              }
            >
              <span>
                <FileDoneOutlined />
                <strong>周报记录</strong>
              </span>
              <RightOutlined />
            </button>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="console-panel console-panel--daily">
      <PanelHeader icon={icon} title={title} />
      <div className="console-report-status-card">
        {reports.map((report) => (
          <div key={report.id} className="console-report-status-card">
            <Space size={8} wrap>
              <strong>{report.name}</strong>
              <ReportStatusTag status={report.status} />
            </Space>
            <p>{report.description}</p>
            <div className="console-report-actions">{renderReportActions(report, onOpen)}</div>
          </div>
        ))}
      </div>
    </div>
  );
}

function ReportSummaryInlineRow({
  report,
  meta,
  onOpen
}: {
  report: ReportItem;
  meta?: string;
  onOpen: (report: ReportItem, step?: ReportModalStep) => void;
}) {
  return (
    <section className="console-report-summary-inline">
      <div className="console-report-summary-inline__main">
        <span className="console-report-inline-title">
          <strong>{getSummaryReportLabel(report)}</strong>
          <ReportStatusTag status={report.status} />
        </span>
        {meta ? <span className="console-report-inline-meta">{meta}</span> : null}
      </div>
      <Button className="console-report-inline-action" onClick={() => onOpen(report, "editor")}>
        {getCompactReportButtonText(report)}
      </Button>
    </section>
  );
}

function ReportWeeklyInlineRow({
  report,
  onOpen
}: {
  report: ReportItem;
  onOpen: (report: ReportItem, step?: ReportModalStep) => void;
}) {
  return (
    <section className="console-report-weekly-inline">
      <div className="console-report-weekly-inline__main">
        <span className="console-report-inline-title">
          <strong>我的周报</strong>
          <ReportStatusTag status={report.status} />
        </span>
        <span className="console-report-inline-meta">{getPersonalWeeklyInlineCopy(report)}</span>
      </div>
      <Button className="console-report-inline-action" onClick={() => onOpen(report, "editor")}>
        {getCompactReportButtonText(report)}
      </Button>
    </section>
  );
}

function ReportManagementWeeklyInlineRow({
  report,
  onOpen
}: {
  report: ReportItem;
  onOpen: (report: ReportItem, step?: ReportModalStep) => void;
}) {
  return (
    <section className="console-report-weekly-inline">
      <div className="console-report-weekly-inline__main">
        <span className="console-report-inline-title">
          <strong>{getManagementWeeklyLabel(report)}</strong>
          <ReportStatusTag status={report.status} />
        </span>
        <span className="console-report-inline-meta">{getManagementWeeklyInlineCopy(report)}</span>
      </div>
      <Button className="console-report-inline-action" onClick={() => onOpen(report, "editor")}>
        {getCompactReportButtonText(report)}
      </Button>
    </section>
  );
}

function ReportTaskRow({
  label,
  report,
  description,
  meta,
  emphasized,
  onOpen
}: {
  label: string;
  report: ReportItem;
  description: string;
  meta?: string;
  emphasized?: boolean;
  onOpen: (report: ReportItem, step?: ReportModalStep) => void;
}) {
  return (
    <section className={`console-report-task${emphasized ? " console-report-task--summary" : ""}`}>
      <div className="console-report-card-head">
        <Space size={8} wrap>
          <strong>{label}</strong>
          <ReportStatusTag status={report.status} />
        </Space>
        <div className="console-report-actions console-report-actions--head">
          {renderPrimaryReportAction(report, onOpen)}
        </div>
      </div>
      <p>{description}</p>
      {meta ? <span className="console-report-task__meta">{meta}</span> : null}
    </section>
  );
}

function renderReportActions(
  report: ReportItem,
  onOpen: (report: ReportItem, step?: ReportModalStep) => void
) {
  return (
    <Button icon={<EditOutlined />} onClick={() => onOpen(report, "editor")}>
      {getReportButtonText(report)}
    </Button>
  );
}

function renderPrimaryReportAction(
  report: ReportItem,
  onOpen: (report: ReportItem, step?: ReportModalStep) => void
) {
  return (
    <Button
      className={`console-report-primary-action ${
        report.status === "待生成"
          ? "console-report-primary-action--generate"
          : report.status === "已保存，未发送最新修改"
            ? "console-report-primary-action--edited"
            : report.status === "已保存"
              ? "console-report-primary-action--saved"
              : "console-report-primary-action--quiet"
      }`}
      type={report.status === "待生成" ? "primary" : "default"}
      icon={<EditOutlined />}
      onMouseDown={(event) => {
        event.preventDefault();
        event.stopPropagation();
      }}
      onClick={(event) => {
        event.preventDefault();
        event.stopPropagation();
        onOpen(report, "editor");
      }}
    >
      {getReportButtonText(report)}
    </Button>
  );
}

function getReportButtonText(report: ReportItem) {
  const noun = getReportActionNoun(report);

  if (report.status === "待生成") return `填写${noun}`;
  if (report.status === "生成失败") return `打开${noun}`;
  if (report.status === "草稿待确认") return `编辑${noun}`;
  if (report.status === "已保存，未发送最新修改") return "继续编辑";
  if (report.status === "已保存") return `编辑${noun}`;
  if (report.status === "已发送") return `打开${noun}`;
  if (report.status === "已归档") return `打开${noun}`;
  if (report.status === "生成中") return `打开${noun}`;

  return `打开${noun}`;
}

function getCompactReportButtonText(report: ReportItem) {
  if (report.status === "待生成") return "填写报告";
  if (report.status === "生成失败") return "打开内容";
  if (report.status === "草稿待确认") return "编辑内容";
  if (report.status === "已保存，未发送最新修改") return "继续编辑";
  if (report.status === "已保存") return "继续编辑";
  if (report.status === "已发送" || report.status === "已归档") return "打开内容";
  if (report.status === "生成中") return "打开内容";
  return "打开内容";
}

function getReportActionNoun(report: ReportItem) {
  if (report.kind === "personal_weekly") return "个人周报";
  if (report.kind === "team_weekly") return "组周报";
  if (report.kind === "department_weekly") return "部门周报";
  if (report.scope === "team") return "组日报";
  if (report.scope === "department") return "部门日报";
  return "日报";
}

function getDailyReportCopy(report: ReportItem) {
  if (report.status === "已发送") {
    return "今日日报已保存，可继续打开查看或编辑。";
  }

  if (report.status === "已保存，未发送最新修改") {
    return "今日日报已保存，可继续编辑。";
  }

  if (report.status === "已保存") {
    return "今日日报已保存，可继续编辑。";
  }

  if (report.status === "草稿待确认") {
    return "日报内容可继续编辑保存。";
  }

  if (report.status === "已归档") {
    return "今日日报已保存，可回看内容。";
  }

  if (report.status === "生成中") {
    return "日报内容可打开查看或编辑。";
  }

  return "暂无日报，可直接填写。";
}

function getSummaryReportLabel(report: ReportItem) {
  return report.scope === "department" ? "部门日报" : "小组日报";
}

function getPersonalWeeklyInlineCopy(report: ReportItem) {
  if (report.status === "已发送") return "已保存";
  if (report.status === "已保存") return "可继续编辑";
  if (report.status === "生成中") return "可打开编辑";
  if (report.status === "生成失败") return "可打开编辑";
  return `已收集 ${report.sessionCount} 篇日报`;
}

function getManagementWeeklyLabel(report: ReportItem) {
  return report.kind === "department_weekly" ? "部门周报" : "小组周报";
}

function getManagementWeeklyInlineCopy(report: ReportItem) {
  if (report.status === "已归档") return "已保存";
  if (report.status === "已发送") return "已保存";
  if (report.status === "已保存") return "可继续编辑";
  if (report.status === "生成中") return "可打开编辑";
  if (report.status === "生成失败") return "可打开编辑";
  return "暂无周报，可通过系统生成";
}

function getCoverageSummary(report: ReportItem) {
  if (report.scope === "team" || report.kind === "team_weekly") {
    return "已汇总成员日报来源";
  }
  if (report.scope === "department" || report.kind === "department_weekly") {
    return "已汇总小组日报来源";
  }
  return "已基于当前可用数据生成报告";
}

function getReportModalTitle(report: ReportItem, step: ReportModalStep) {
  if (report.kind === "department_daily") {
    return "编辑部门日报";
  }
  if (step === "editor") {
    return `编辑${report.name}`;
  }

  return `编辑${report.name}`;
}

function getReportModalWidth(report: ReportItem, step: ReportModalStep) {
  if (step === "editor") return 860;
  if (report.kind === "team_daily") return 840;
  return 720;
}

function SessionUploadCard({
  role,
  range,
  report,
  loading,
  error,
  onRangeChange,
  onViewDetail
}: {
  role: DashboardRole;
  range: TokenRange;
  report: TokenReport;
  loading: boolean;
  error: boolean;
  onRangeChange: (range: TokenRange) => void;
  onViewDetail: () => void;
}) {
  return (
    <div className="console-panel console-panel--token">
      <PanelHeader
        icon={<BarChartOutlined />}
        title="Token 用量"
        extra={
          <Segmented
            className="console-token-range"
            size="small"
            options={TOKEN_RANGE_OPTIONS}
            value={range}
            onChange={(value) => onRangeChange(value as TokenRange)}
          />
        }
      />
      <div className="console-report-status-card">
        {loading ? (
          <div className="console-token-state">Token 数据加载中...</div>
        ) : error ? (
          <div className="console-token-state is-error">Token 数据加载失败</div>
        ) : report.sessions === 0 ? (
          <div className="console-token-state">当前范围暂无 Token 数据</div>
        ) : (
          renderSessionUploadSummary(role, range, report)
        )}
        <div className="console-token-footer">
          <span>基于上传的工作记录统计</span>
          <Button type="link" icon={<LinkOutlined />} onClick={onViewDetail}>
            查看 Token 明细
          </Button>
        </div>
      </div>
    </div>
  );
}

function getTokenRangeLabel(range: TokenRange) {
  const option = TOKEN_RANGE_OPTIONS.find((item) => item.value === range);
  return option?.label ?? "近 7 天";
}

function renderSessionUploadSummary(role: DashboardRole, range: TokenRange, report: TokenReport) {
  if (role === "director" && report.groups && report.groups.length > 0) {
    return (
      <div className="console-token-scope">
        <TokenSummaryRow
          range={range}
          report={report}
          scopeLabel="各组 Token"
          scopeMeta={`${report.sessions} 个 session · ${report.groups.length} 个组已上报`}
        />
        <TokenVerticalBars ariaLabel="各组 Token 分布" caption="各组 Token" items={report.groups} />
      </div>
    );
  }

  if (role === "team_leader" && typeof report.uploaders === "number") {
    return (
      <div className="console-token-scope">
        <TokenSummaryRow
          range={range}
          report={report}
          scopeLabel="本组 Token"
          scopeMeta={`${report.sessions} 个 session · ${report.uploaders} 人已上报`}
        />
        <TokenVerticalBars
          ariaLabel="组内成员 Token 分布"
          caption="组内成员 Token"
          items={report.memberGroups ?? []}
        />
      </div>
    );
  }

  if (typeof report.uploaders === "number") {
    return (
      <div className="console-token-scope">
        <TokenSummaryRow
          range={range}
          report={report}
          scopeLabel="本组 Token"
          scopeMeta={`${report.sessions} 个 session · ${report.uploaders} 人已上报`}
        />
        <TokenMetricBars bars={report.bars} />
      </div>
    );
  }

  return (
    <div className={`console-token-overview${report.bars.length >= 7 ? " is-compact" : ""}`}>
      <div className="console-token-total">
        <span>{getTokenRangeLabel(range)}</span>
        <strong>{report.total}</strong>
        <em>解析 Token</em>
      </div>
      <TokenMiniBars bars={report.bars} />
    </div>
  );
}

function TokenSummaryRow({
  range,
  report,
  scopeLabel,
  scopeMeta
}: {
  range: TokenRange;
  report: TokenReport;
  scopeLabel: string;
  scopeMeta: string;
}) {
  const sessions = report.mine?.sessions ?? report.sessions;
  const total = report.mine?.total ?? report.total;

  return (
    <div className="console-token-summary-row">
      <TokenSummaryMetric
        label="我的 Token"
        value={total}
        meta={`${getTokenRangeLabel(range)} · ${sessions} 个 session`}
      />
      <TokenSummaryMetric label={scopeLabel} value={report.total} meta={scopeMeta} primary />
    </div>
  );
}

function TokenSummaryMetric({
  label,
  value,
  meta,
  primary = false
}: {
  label: string;
  value: string;
  meta: string;
  primary?: boolean;
}) {
  return (
    <div
      className={`console-token-summary-metric${
        primary ? " console-token-summary-metric--primary" : ""
      }`}
    >
      <span>{label}</span>
      <strong>{value}</strong>
      <em>{meta}</em>
    </div>
  );
}

function TokenMiniBars({ bars }: { bars: TokenReport["bars"] }) {
  const activeBars = bars.filter((bar) => bar.value > 0);
  const maxValue = Math.max(...activeBars.map((bar) => bar.value), 1);
  const hiddenEmptyDays = Math.max(0, bars.length - activeBars.length);

  const compact = bars.length >= 7;

  return (
    <div
      className={`console-token-chart${compact ? " is-compact" : ""}`}
      aria-label="每日解析 Token 趋势"
    >
      <span className="console-token-chart__caption">每日解析 Token</span>
      {activeBars.length ? (
        <div className={`console-token-day-bars${compact ? " is-compact" : ""}`}>
          {activeBars.slice(-4).map((bar) => (
            <div key={bar.label} className="console-token-day-bars__item">
              <span>{bar.label}</span>
              <i>
                <b
                  style={{ width: `${Math.max(12, Math.round((bar.value / maxValue) * 100))}%` }}
                />
              </i>
              <em>{bar.text}</em>
            </div>
          ))}
          {hiddenEmptyDays ? <small>其余 {hiddenEmptyDays} 天暂无上传</small> : null}
        </div>
      ) : (
        <div className="console-token-empty-chart">
          <strong>当前范围暂无 Token 数据</strong>
          <span>上传 session 后将展示每日解析趋势</span>
        </div>
      )}
    </div>
  );
}

function TokenMetricBars({ bars }: { bars: TokenReport["bars"] }) {
  const maxValue = Math.max(...bars.map((bar) => bar.value), 1);

  return (
    <div className="console-token-metric-bars" aria-label="Token 上报摘要">
      {bars.map((bar) => (
        <div key={bar.label} className="console-token-metric-bars__item">
          <span>{bar.label}</span>
          <i>
            <b style={{ width: `${Math.max(8, Math.round((bar.value / maxValue) * 100))}%` }} />
          </i>
          <em>{bar.text}</em>
        </div>
      ))}
    </div>
  );
}

function TokenVerticalBars({
  ariaLabel,
  caption,
  items
}: {
  ariaLabel: string;
  caption: string;
  items: NonNullable<TokenReport["groups"]>;
}) {
  const chartRef = useRef<HTMLDivElement | null>(null);
  const chartInstanceRef = useRef<ECharts | null>(null);
  const option = useMemo<EChartsOption>(() => {
    const visibleCount = 6;
    const hasOverflow = items.length > visibleCount;
    const end = hasOverflow ? Math.max(20, Math.round((visibleCount / items.length) * 100)) : 100;

    return {
      animation: false,
      grid: {
        top: 24,
        right: 12,
        bottom: hasOverflow ? 42 : 34,
        left: 42,
        containLabel: false
      },
      tooltip: {
        trigger: "axis",
        axisPointer: {
          type: "shadow"
        },
        borderWidth: 0,
        padding: [6, 8],
        textStyle: {
          color: "#172033",
          fontSize: 12
        },
        formatter: (params: unknown) => {
          const item = Array.isArray(params) ? params[0] : params;
          const index =
            item && typeof item === "object" && "dataIndex" in item
              ? Number((item as { dataIndex: number }).dataIndex)
              : 0;
          const datum = items[index] ?? items[0];
          return datum
            ? `${datum.name}<br />${datum.total}${datum.note ? `<br />${datum.note}` : ""}`
            : "";
        }
      },
      dataZoom: hasOverflow
        ? [
            {
              type: "inside",
              xAxisIndex: 0,
              start: 0,
              end,
              zoomLock: true
            },
            {
              type: "slider",
              xAxisIndex: 0,
              height: 14,
              bottom: 4,
              start: 0,
              end,
              borderColor: "transparent",
              fillerColor: "rgba(22, 119, 255, 0.16)",
              handleSize: 0,
              showDetail: false,
              brushSelect: false
            }
          ]
        : undefined,
      xAxis: {
        type: "category",
        data: items.map((item) => item.name),
        axisTick: { show: false },
        axisLine: { lineStyle: { color: "#dbe6f2" } },
        axisLabel: {
          color: "#64748b",
          fontSize: 11,
          interval: 0,
          overflow: "truncate",
          width: 56
        }
      },
      yAxis: {
        type: "value",
        min: 0,
        axisLabel: {
          color: "#8a97a8",
          fontSize: 10,
          formatter: (value: number) => formatTokenAxisValue(value)
        },
        splitLine: { lineStyle: { color: "#eef3f8" } }
      },
      series: [
        {
          type: "bar",
          data: items.map((item) => item.value),
          barMaxWidth: 28,
          itemStyle: {
            borderRadius: [5, 5, 0, 0],
            color: new echarts.graphic.LinearGradient(0, 0, 0, 1, [
              { offset: 0, color: "#1677ff" },
              { offset: 1, color: "#69a6ff" }
            ])
          },
          label: {
            show: true,
            position: "top",
            color: "#526173",
            fontSize: 10,
            formatter: (params: unknown) => {
              const index =
                params && typeof params === "object" && "dataIndex" in params
                  ? Number((params as { dataIndex: number }).dataIndex)
                  : 0;
              return items[index]?.total ?? "";
            }
          },
          emphasis: {
            itemStyle: {
              color: "#0958d9"
            }
          }
        }
      ]
    };
  }, [items]);

  useEffect(() => {
    if (!chartRef.current) return undefined;

    const chart = echarts.init(chartRef.current, undefined, { renderer: "svg" });
    chartInstanceRef.current = chart;

    const resize = () => chart.resize();
    window.addEventListener("resize", resize);

    return () => {
      window.removeEventListener("resize", resize);
      chart.dispose();
      chartInstanceRef.current = null;
    };
  }, []);

  useEffect(() => {
    if (!chartInstanceRef.current) return;
    chartInstanceRef.current.setOption(option, true);
    chartInstanceRef.current.resize();
  }, [option]);

  return (
    <div className="console-token-vertical-chart" aria-label={ariaLabel}>
      <span className="console-token-chart__caption">{caption}</span>
      <div ref={chartRef} className="console-token-vertical-echart" />
    </div>
  );
}

function formatTokenAxisValue(value: number) {
  if (value >= 1_000_000) return `${Math.round(value / 1_000_000)}M`;
  if (value >= 1_000) return `${Math.round(value / 1_000)}K`;
  return String(value);
}

function getDefaultDraftMarkdown(report: ReportItem) {
  if (report.kind === "personal_weekly") {
    return `# 本周周报

## 本周完成
* 完成控制台报告入口梳理。
* 跟进我负责和关注任务的状态变化。

## 风险与阻塞
* 飞书发送目标仍需确认。

## 下周计划
* 继续完善需求看板定位和报告生成流程。`;
  }

  if (report.kind === "team_weekly") {
    return `# 组周报

## 组整体进展
* 控制台信息架构完成收敛。

## 重点需求进展
* AI 日报生成：进入原型调整阶段。

## 组员进展摘要
* 陈一：完成个人报告入口收敛。

## 风险与阻塞
* session 导入接口字段仍需冻结。

## 下周计划
* 补齐需求看板风险定位闭环。`;
  }

  if (report.kind === "department_weekly") {
    return `# 部门周报

## 部门整体进展
* 报告能力从日报扩展为个人与汇总报告。

## 各组进展
* 平台组：控制台原型继续收敛。

## 重点需求进展
* AIcoding 管理平台完成报告口径统一。

## 风险与阻塞
* 跨组依赖和发送目标仍需对齐。

## 下周重点
* 推进需求看板和报告编辑闭环。`;
  }

  if (report.kind === "team_daily") {
    return `# 组日报

## 组整体进展
* 组内成员日报和风险项已汇总。

## 重点风险
* 工作记录解析任务仍处于阻塞状态。

## 明日计划
* 继续推进日报发送和需求看板联动。`;
  }

  if (report.kind === "department_daily") {
    return `# 部门日报

## 部门整体进展
* 各组日报已完成汇总。

## 重点风险
* 部门日报可基于现有小组日报内容继续编辑保存。

## 明日重点
* 跟进高优先级风险和跨组依赖。`;
  }

  return DEFAULT_MARKDOWN;
}

function getReportSourceSteps(report: ReportItem) {
  if (report.kind === "personal_daily") {
    return [{ title: "报告内容" }, { title: "编辑内容" }];
  }

  return [{ title: "报告内容" }, { title: "编辑内容" }];
}

function getReportSourceTitle(report: ReportItem) {
  if (report.scope === "team") return "查看成员日报收集情况";
  if (report.scope === "department") return "查看小组日报收集情况";
  return "管理个人周报";
}

function getReportSourceMeta(report: ReportItem, coverage?: ReportCoverage) {
  if (report.scope === "team" && coverage) {
    return "已汇总成员日报来源";
  }

  if (report.scope === "department" && coverage) {
    return "已汇总小组日报来源";
  }

  return "该报告已切换至新版编辑流程。";
}

function getEditorMeta(report: ReportItem) {
  if (report.kind === "personal_daily") {
    return [`已关联 ${report.sessionCount} 条记录`, report.skill];
  }

  if (report.kind === "department_daily") {
    return ["系统汇总报告上下文", "来源：当前可用业务数据"];
  }

  if (report.kind === "team_daily") {
    return ["系统汇总报告上下文", "来源：当前可用业务数据"];
  }

  return [report.sourceSummary, report.skill];
}

function getSendButtonText(report: ReportItem) {
  if (report.scope === "team") return report.kind.includes("weekly") ? "保存周报" : "保存小组日报";
  if (report.scope === "department")
    return report.kind.includes("weekly") ? "保存周报" : "保存部门日报";
  return report.kind.includes("weekly") ? "保存周报" : "保存日报";
}

function renderReportModalFooter({
  step,
  report,
  selectedCount,
  teamSubmittedCount,
  departmentSubmittedCount,
  modifiedTaskCount,
  isSessionLoading,
  isGenerating,
  isSaving,
  onCancel,
  onNext,
  onGenerate,
  onBack,
  onSave,
  onSend
}: {
  step: ReportModalStep;
  report: ReportItem;
  selectedCount: number;
  teamSubmittedCount: number;
  departmentSubmittedCount: number;
  modifiedTaskCount: number;
  isSessionLoading: boolean;
  isGenerating: boolean;
  isSaving: boolean;
  onCancel: () => void;
  onNext: () => void;
  onGenerate: () => void;
  onBack: () => void;
  onSave: () => void;
  onSend: () => void;
}) {
  if (step === "sessions") {
    return (
      <Space>
        <Button onClick={onCancel} disabled={isGenerating}>
          稍后处理
        </Button>
        <Button
          type="primary"
          disabled={selectedCount === 0 || isSessionLoading}
          loading={isGenerating}
          onClick={onNext}
        >
          下一步
        </Button>
      </Space>
    );
  }

  if (step === "source") {
    return (
      <Space>
        <Button onClick={onCancel}>稍后处理</Button>
        <Button
          type="primary"
          loading={isGenerating}
          disabled={
            (report.kind === "department_daily" && departmentSubmittedCount === 0) ||
            (report.kind === "team_daily" && teamSubmittedCount === 0)
          }
          onClick={onGenerate}
        >
          {report.kind === "department_daily"
            ? "继续编辑部门日报"
            : report.kind === "team_daily"
              ? "继续编辑小组日报"
              : "继续编辑日报"}
        </Button>
      </Space>
    );
  }

  return (
    <Space>
      {modifiedTaskCount > 0 ? (
        <span className="console-report-footer-note">
          已修改 {modifiedTaskCount} 个任务，保存日报后同步任务进展。
        </span>
      ) : null}
      <Button onClick={onBack} disabled={isSaving}>
        上一步
      </Button>
      {report.scope === "department" ? null : (
        <Button onClick={onSave} loading={isSaving}>
          {report.scope === "team" ? "保存小组日报" : "保存日报"}
        </Button>
      )}
      <Button type="primary" icon={<FileDoneOutlined />} loading={isSaving} onClick={onSend}>
        {getSendButtonText(report)}
      </Button>
    </Space>
  );
}

function TaskProgressSuggestionList({
  tasks,
  sessionOptions,
  onEditTask
}: {
  tasks: TaskProgressSuggestion[];
  sessionOptions: SessionOption[];
  onEditTask: (task: TaskProgressSuggestion) => void;
}) {
  const sessionTitleById = new Map(
    sessionOptions.map((session) => [session.value, `${session.tool} ${session.timeRange}`])
  );
  return (
    <aside className="console-task-suggestion-list">
      <div className="console-session-modal__section">
        <strong>任务进展建议</strong>
        <span>LLM 根据已选 session 生成 {tasks.length} 条建议，可按需修改。</span>
      </div>
      <div className="console-task-suggestion-scroll">
        {tasks.length === 0 ? (
          <div className="console-task-suggestion-empty">
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无任务进展建议" />
          </div>
        ) : (
          tasks.map((task) => (
            <article key={task.key} className="console-task-suggestion-card">
              <div className="console-task-suggestion-card__top">
                <strong>{task.taskName}</strong>
                <Button size="small" onClick={() => onEditTask(task)}>
                  编辑任务
                </Button>
              </div>
              <div className="console-task-suggestion-card__meta">
                <Tag color="blue">{getTaskStatusLabel(task.status)}</Tag>
                <span>建议进度 {task.progress}%</span>
                <span>{task.sessionIds.length} 个 session</span>
              </div>
              <ul>
                {task.sessionIds.map((sessionId, index) => (
                  <li key={sessionId}>
                    {task.evidenceSessionTitles[index] ??
                      sessionTitleById.get(sessionId) ??
                      sessionId}
                  </li>
                ))}
              </ul>
              {task.note ? <p>{task.note}</p> : null}
              {task.syncState ? (
                <Tag className="console-task-suggestion-card__sync" color="blue">
                  {task.syncState}
                </Tag>
              ) : null}
            </article>
          ))
        )}
      </div>
    </aside>
  );
}

function getUploadedSkillName(fileName: string, content: string) {
  const frontmatterName = content.match(/^\s*name:\s*["']?(.+?)["']?\s*$/m)?.[1]?.trim();
  const baseName = fileName
    .replace(/\.[^.]+$/, "")
    .replace(/[-_]+/g, " ")
    .trim();
  const rawName = frontmatterName || baseName || "旧配置";

  return /skill/i.test(rawName) || rawName.includes("Skill") ? rawName : `${rawName} Skill`;
}

function getDashboardRole(role?: UserRole | null): DashboardRole {
  if (role === "admin") return "director";
  if (role === "director" || role === "pm" || role === "team_leader" || role === "employee")
    return role;
  return "employee";
}

function toSessionOption(session: Session): SessionOption {
  const started = formatDateTime(session.started_at, "HH:mm");
  const ended = session.ended_at ? formatDateTime(session.ended_at, "HH:mm") : "";
  const timeRange = ended && ended !== "-" ? `${started} - ${ended}` : started;
  return {
    tool: getAgentLabel(session.agent_type),
    timeRange: timeRange === "-" ? "时间未知" : timeRange,
    summary: session.summary || session.task_title || session.session_ref,
    value: session.id,
    recommended: true
  };
}

function getAgentLabel(agentType: string) {
  if (agentType === "codex") return "Codex session";
  if (agentType === "claude_code") return "Claude Code session";
  return `${agentType || "AI"} session`;
}

function mapDraftTaskSuggestion(item: DraftTaskProgressSuggestion): TaskProgressSuggestion {
  return {
    key: item.task_id,
    taskId: item.task_id,
    taskName: item.task_title,
    progress: clampTaskProgress(item.suggested_progress),
    status: item.suggested_status,
    sessionIds: item.evidence_session_ids,
    evidenceSessionTitles: item.evidence_session_titles,
    note: item.reason
  };
}

function clampTaskProgress(progress: number) {
  if (!Number.isFinite(progress)) return 0;
  return Math.max(0, Math.min(100, Math.round(progress)));
}

function getTaskStatusLabel(status: DraftTaskStatus) {
  if (status === "done") return "已完成";
  if (status === "in_progress") return "进行中";
  return "未开始";
}

function TaskProgressEditModal({
  task,
  open,
  sessionOptions,
  confirmLoading,
  onCancel,
  onChange,
  onSave
}: {
  task: TaskProgressSuggestion | null;
  open: boolean;
  sessionOptions: SessionOption[];
  confirmLoading: boolean;
  onCancel: () => void;
  onChange: (task: TaskProgressSuggestion | null) => void;
  onSave: () => void;
}) {
  if (!task) return null;

  return (
    <Modal
      className="console-task-progress-modal"
      title="修改任务进展"
      open={open}
      width={560}
      onCancel={onCancel}
      footer={
        <Space>
          <Button onClick={onCancel} disabled={confirmLoading}>
            取消
          </Button>
          <Popconfirm
            title="确认更新任务进展？"
            description="确认后会调用任务接口更新状态或进度。"
            okText="确认更新"
            cancelText="取消"
            onConfirm={onSave}
          >
            <Button type="primary" loading={confirmLoading}>
              确认更新任务
            </Button>
          </Popconfirm>
        </Space>
      }
    >
      <div className="console-task-edit-form">
        <div className="console-session-modal__section">
          <strong>任务：{task.taskName}</strong>
        </div>
        <label>
          <span>进度：</span>
          <Select
            value={task.progress}
            options={[0, 25, 50, 75, 100].map((value) => ({ label: `${value}%`, value }))}
            onChange={(progress) => onChange({ ...task, progress })}
          />
        </label>
        <label>
          <span>状态：</span>
          <Select
            value={task.status}
            options={[
              { label: "未开始", value: "todo" },
              { label: "进行中", value: "in_progress" },
              { label: "已完成", value: "done" }
            ]}
            onChange={(status) => onChange({ ...task, status: status as DraftTaskStatus })}
          />
        </label>
        <div className="console-session-modal__section">
          <strong>关联 session：</strong>
          <Checkbox.Group
            value={task.sessionIds}
            onChange={(value) => {
              const sessionIds = value as string[];
              onChange({
                ...task,
                sessionIds,
                evidenceSessionTitles: sessionIds.map((sessionId) => {
                  const session = sessionOptions.find((item) => item.value === sessionId);
                  return session ? `${session.tool} ${session.timeRange}` : sessionId;
                })
              });
            }}
          >
            <div className="console-task-edit-sessions">
              {sessionOptions.map((session) => (
                <Checkbox key={session.value} value={session.value}>
                  {session.tool} {session.timeRange}
                </Checkbox>
              ))}
            </div>
          </Checkbox.Group>
        </div>
        <label>
          <span>备注：</span>
          <Input.TextArea
            value={task.note}
            rows={3}
            placeholder="可选填写"
            onChange={(event) => onChange({ ...task, note: event.target.value })}
          />
        </label>
      </div>
    </Modal>
  );
}

function ReportModalContent({
  step,
  report,
  coverage,
  teamSources,
  teamSourcesLoading,
  teamSourcesError,
  departmentSources,
  departmentSourcesLoading,
  departmentSourcesError,
  selectedSessionIds,
  selectedSkill,
  skillOptions,
  uploadedSkills,
  sessionOptions,
  isSessionLoading,
  sessionError,
  draftError,
  taskSuggestions,
  draftMarkdown,
  onSelectedSessionIdsChange,
  onSelectedSkillChange,
  onSkillUpload,
  onEditTask,
  onDraftMarkdownChange
}: {
  step: ReportModalStep;
  report: ReportItem;
  coverage: ReportCoverage;
  teamSources: TeamReportSources | null;
  teamSourcesLoading: boolean;
  teamSourcesError: string | null;
  departmentSources: DepartmentReportSources | null;
  departmentSourcesLoading: boolean;
  departmentSourcesError: string | null;
  selectedSessionIds: string[];
  selectedSkill: string;
  skillOptions: ReportSkillOption[];
  uploadedSkills: ReportSkillOption[];
  sessionOptions: SessionOption[];
  isSessionLoading: boolean;
  sessionError: string | null;
  draftError: string | null;
  taskSuggestions: TaskProgressSuggestion[];
  draftMarkdown: string;
  onSelectedSessionIdsChange: (value: string[]) => void;
  onSelectedSkillChange: (value: string) => void;
  onSkillUpload: (file: File) => boolean;
  onEditTask: (task: TaskProgressSuggestion) => void;
  onDraftMarkdownChange: (value: string) => void;
}) {
  const [expandedTeamReportUserId, setExpandedTeamReportUserId] = useState<string | null>(null);
  const [expandedDepartmentReportId, setExpandedDepartmentReportId] = useState<string | null>(null);

  if (step === "sessions") {
    return (
      <div className="console-report-modal">
        <Steps size="small" current={0} items={getReportSourceSteps(report)} />
        <div className="console-session-modal__section">
          <strong>报告内容</strong>
          <span>
            {isSessionLoading
              ? "正在加载今日工作记录。"
              : `已找到 ${sessionOptions.length} 条工作记录，默认勾选今日全部记录。`}
          </span>
        </div>
        {sessionError ? <Alert type="error" showIcon message={sessionError} /> : null}
        {draftError ? (
          <Alert type="error" showIcon message="日报生成失败" description={draftError} />
        ) : null}
        <Checkbox.Group
          value={selectedSessionIds}
          onChange={(value) => onSelectedSessionIdsChange(value as string[])}
        >
          <div className="console-session-list">
            {isSessionLoading ? (
              <div className="console-session-empty">正在加载工作记录...</div>
            ) : sessionOptions.length === 0 ? (
              <div className="console-session-empty">
                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="今日暂无可用工作记录" />
              </div>
            ) : (
              sessionOptions.map((session) => (
                <label key={session.value} className="console-session-item">
                  <Checkbox value={session.value} />
                  <span>
                    <strong>{session.tool}</strong>
                    <em>
                      {session.timeRange} · {session.summary}
                    </em>
                  </span>
                  {session.recommended ? <Tag color="blue">默认勾选</Tag> : null}
                </label>
              ))
            )}
          </div>
        </Checkbox.Group>
        <GenerationSettingsPanel
          selectedSkill={selectedSkill}
          skillOptions={skillOptions}
          uploadedSkills={uploadedSkills}
          onSelectedSkillChange={onSelectedSkillChange}
          onSkillUpload={onSkillUpload}
        />
      </div>
    );
  }

  if (step === "source") {
    if (report.kind === "department_daily") {
      return (
        <div className="console-report-modal">
          <Steps size="small" current={0} items={getReportSourceSteps(report)} />
          <DepartmentSourceReview
            sources={departmentSources}
            loading={departmentSourcesLoading}
            error={departmentSourcesError}
            expandedReportId={expandedDepartmentReportId}
            onExpandedReportIdChange={setExpandedDepartmentReportId}
          />
          {draftError ? (
            <Alert type="error" showIcon message="部门日报生成失败" description={draftError} />
          ) : null}
        </div>
      );
    }

    if (report.kind === "team_daily") {
      return (
        <div className="console-report-modal">
          <Steps size="small" current={0} items={getReportSourceSteps(report)} />
          <TeamSourceReview
            sources={teamSources}
            loading={teamSourcesLoading}
            error={teamSourcesError}
            expandedUserId={expandedTeamReportUserId}
            onExpandedUserIdChange={setExpandedTeamReportUserId}
          />
          <details className="console-generation-settings-disclosure">
            <summary>高级配置</summary>
            <GenerationSettingsPanel
              selectedSkill={selectedSkill}
              skillOptions={skillOptions}
              uploadedSkills={uploadedSkills}
              onSelectedSkillChange={onSelectedSkillChange}
              onSkillUpload={onSkillUpload}
              compact
            />
          </details>
          {draftError ? (
            <Alert type="error" showIcon message="小组日报生成失败" description={draftError} />
          ) : null}
        </div>
      );
    }

    return (
      <div className="console-report-modal">
        <Steps size="small" current={0} items={getReportSourceSteps(report)} />
        <div className="console-session-modal__section">
          <strong>{getReportSourceTitle(report)}</strong>
          <span>{report.sourceSummary}</span>
        </div>
        <div className="console-editor-shell__meta">
          <span>{getReportSourceMeta(report, coverage)}</span>
        </div>
        <GenerationSettingsPanel
          selectedSkill={selectedSkill}
          skillOptions={skillOptions}
          uploadedSkills={uploadedSkills}
          onSelectedSkillChange={onSelectedSkillChange}
          onSkillUpload={onSkillUpload}
          compact
        />
      </div>
    );
  }

  return (
    <div className="console-report-modal">
      <Steps size="small" current={1} items={getReportSourceSteps(report)} />
      <div className="console-editor-shell__meta">
        {getEditorMeta(report).map((meta) => (
          <span key={meta}>{meta}</span>
        ))}
      </div>
      <div className={report.kind === "personal_daily" ? "console-daily-editor-layout" : undefined}>
        <Input.TextArea
          className="console-markdown-textarea"
          value={draftMarkdown}
          rows={18}
          onChange={(event) => onDraftMarkdownChange(event.target.value)}
        />
        {report.kind === "personal_daily" ? (
          <TaskProgressSuggestionList
            tasks={taskSuggestions}
            sessionOptions={sessionOptions}
            onEditTask={onEditTask}
          />
        ) : null}
      </div>
    </div>
  );
}

function TeamSourceReview({
  sources,
  loading,
  error,
  expandedUserId,
  onExpandedUserIdChange
}: {
  sources: TeamReportSources | null;
  loading: boolean;
  error: string | null;
  expandedUserId: string | null;
  onExpandedUserIdChange: (id: string | null) => void;
}) {
  if (loading) {
    return <div className="console-session-empty">正在加载成员原始日报收集情况...</div>;
  }

  if (error) {
    return <Alert type="error" showIcon message={error} />;
  }

  if (!sources) {
    return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无成员日报收集数据" />;
  }

  const members = sources.members;
  const total = members.length;
  const edited = members.filter(
    (member) => member.has_report && member.content.trim().length > 0
  ).length;

  return (
    <div className="console-department-source">
      <div className="console-session-modal__section">
        <strong>确认成员日报来源</strong>
        <span>
          {sources.team_name} · {sources.report_date} · 已汇总成员日报来源。
        </span>
      </div>

      <div className="console-team-source__stats" aria-label="成员日报发送统计">
        <span>
          <strong>{total}</strong>
          <em>成员总数</em>
        </span>
        <span>
          <strong>{edited}</strong>
          <em>可用来源</em>
        </span>
      </div>

      <section className="console-department-source__block console-team-source__block">
        <div className="console-department-source__head">
          <strong>成员日报来源</strong>
          <Tag color="blue">系统来源</Tag>
        </div>
        {members.length === 0 ? (
          <div className="console-session-empty">暂无团队成员</div>
        ) : (
          <div className="console-team-source__list">
            {members.map((member) => {
              const expanded = expandedUserId === member.user_id;

              return (
                <article
                  key={member.user_id}
                  className={`console-team-source__item ${member.has_report ? "" : "is-missing"}`}
                >
                  <div className="console-team-source__row">
                    <div className="console-team-source__member">
                      <strong title={member.user_name}>{member.user_name}</strong>
                      <Tag color={member.has_report ? "blue" : "gold"} variant="filled">
                        {member.has_report ? "有来源" : "无来源"}
                      </Tag>
                    </div>
                    <div className="console-team-source__actions">
                      <time>
                        {member.submitted_at
                          ? formatDateTime(member.submitted_at, "HH:mm")
                          : "无来源"}
                      </time>
                      {member.has_report ? (
                        <Button
                          size="small"
                          onClick={() => onExpandedUserIdChange(expanded ? null : member.user_id)}
                        >
                          {expanded ? "收起内容" : "查看内容"}
                        </Button>
                      ) : null}
                    </div>
                  </div>
                  {expanded ? (
                    <pre className="console-department-source__content console-team-source__content">
                      {member.content || "暂无内容"}
                    </pre>
                  ) : !member.has_report ? (
                    <p className="console-team-source__missing-note">
                      当前没有可用的成员日报来源。
                    </p>
                  ) : null}
                </article>
              );
            })}
          </div>
        )}
      </section>
    </div>
  );
}

function DepartmentSourceReview({
  sources,
  loading,
  error,
  expandedReportId,
  onExpandedReportIdChange
}: {
  sources: DepartmentReportSources | null;
  loading: boolean;
  error: string | null;
  expandedReportId: string | null;
  onExpandedReportIdChange: (id: string | null) => void;
}) {
  if (loading) {
    return <div className="console-session-empty">正在加载小组日报收集情况...</div>;
  }

  if (error) {
    return <Alert type="error" showIcon message={error} />;
  }

  if (!sources) {
    return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无小组日报收集数据" />;
  }

  const submitted = sources.submitted_team_reports;
  const missing = sources.missing_teams;

  return (
    <div className="console-department-source">
      <div className="console-session-modal__section">
        <strong>确认小组日报来源</strong>
        <span>已汇总小组日报来源。</span>
      </div>

      <section className="console-department-source__block">
        <div className="console-department-source__head">
          <strong>小组日报来源</strong>
          <Tag color="blue">系统来源</Tag>
        </div>
        {submitted.length === 0 ? (
          <div className="console-session-empty">暂无可用小组日报来源</div>
        ) : (
          <div className="console-department-source__list">
            {submitted.map((item) => {
              const reportId = item.team_report_id ?? item.report_id ?? item.team_id;
              const expanded = expandedReportId === reportId;
              return (
                <article key={reportId} className="console-department-source__item">
                  <div className="console-department-source__row">
                    <span>
                      <strong>{item.team_name}</strong>
                      <em>{item.team_leader_name || item.leader_name || "未记录 TL"}</em>
                    </span>
                    <span>
                      <time>
                        {item.submitted_at ? formatDateTime(item.submitted_at, "HH:mm") : "-"}
                      </time>
                      <Button
                        size="small"
                        onClick={() => onExpandedReportIdChange(expanded ? null : reportId)}
                      >
                        {expanded ? "收起原文" : "查看原文"}
                      </Button>
                    </span>
                  </div>
                  {expanded ? (
                    <pre className="console-department-source__content">
                      {item.content || "暂无内容"}
                    </pre>
                  ) : null}
                </article>
              );
            })}
          </div>
        )}
      </section>

      <section className="console-department-source__block">
        <div className="console-department-source__head">
          <strong>暂无来源小组</strong>
          <Tag color={missing.length > 0 ? "gold" : "green"}>{missing.length} 组</Tag>
        </div>
        {missing.length === 0 ? (
          <div className="console-session-empty">所有小组都有可用来源</div>
        ) : (
          <div className="console-department-source__missing">
            {missing.map((team) => (
              <Tag key={team.team_id} color="gold">
                {team.team_name}
              </Tag>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}

function GenerationSettingsPanel({
  selectedSkill,
  skillOptions,
  uploadedSkills,
  onSelectedSkillChange,
  onSkillUpload,
  compact
}: {
  selectedSkill: string;
  skillOptions: ReportSkillOption[];
  uploadedSkills: ReportSkillOption[];
  onSelectedSkillChange: (value: string) => void;
  onSkillUpload: (file: File) => boolean;
  compact?: boolean;
}) {
  const selectedSkillLabel =
    skillOptions.find((option) => option.value === selectedSkill)?.label ?? selectedSkill;
  return (
    <section
      className={`console-generation-settings${compact ? " console-generation-settings--compact" : ""}`}
    >
      <div className="console-generation-settings__head">
        <span>
          <strong>旧配置已停用</strong>
          <em>本轮报告弹窗不再提供旧配置入口。</em>
        </span>
        <Tag color="blue">{selectedSkillLabel}</Tag>
      </div>
      <div className="console-generation-settings__body">
        <label>
          <span>当前预设</span>
          <Select
            value={selectedSkill}
            options={skillOptions.map((option) => ({
              label: option.label,
              value: option.value
            }))}
            popupMatchSelectWidth={false}
            onChange={onSelectedSkillChange}
          />
        </label>
        <div className="console-generation-settings__upload">
          <span>旧配置</span>
          <Upload
            accept=".md,text/markdown"
            beforeUpload={(file) => onSkillUpload(file)}
            maxCount={1}
            showUploadList={false}
          >
            <Button icon={<UploadOutlined />}>旧配置已停用</Button>
          </Upload>
        </div>
      </div>
      {uploadedSkills.length > 0 ? (
        <div className="console-generation-settings__presets" aria-label="已停用旧配置">
          {uploadedSkills.map((skill) => (
            <Tag key={skill.value} color={skill.value === selectedSkill ? "blue" : "default"}>
              {skill.label}
            </Tag>
          ))}
        </div>
      ) : null}
    </section>
  );
}

function ReportStatusTag({ status }: { status: ReportStatus }) {
  const color =
    status === "已归档" || status === "已发送"
      ? "green"
      : status === "生成失败"
        ? "red"
        : status === "草稿待确认" || status === "生成中" || status === "已保存"
          ? "blue"
          : "gold";
  const label =
    status === "草稿待确认"
      ? "待编辑"
      : status === "已归档"
        ? "已保存"
        : status === "待生成"
          ? "暂无报告"
          : status === "生成中"
            ? "可编辑"
            : status === "生成失败"
              ? "可编辑"
              : status;
  return <Tag color={color}>{label}</Tag>;
}

function formatCompactNames(names: string[], fallback = "") {
  const validNames = names.filter(Boolean);
  if (!validNames.length) return fallback;
  const visible = validNames.slice(0, 2);
  const restCount = validNames.length - visible.length;
  return restCount > 0 ? `${visible.join("、")} +${restCount}` : visible.join("、");
}

function getRequirementResponsibleText(
  requirement?: Pick<MockRequirement, "responsible_users" | "responsible_user_ids">,
  fallbackNames?: string[]
) {
  const names =
    requirement?.responsible_users.map((responsible) => responsible.name || responsible.id) ?? [];
  return formatCompactNames(names.length ? names : (fallbackNames ?? []));
}

function getTaskResponsibleText(
  task?: Pick<MockTask, "responsible_users" | "responsible_user_ids">,
  fallbackNames?: string[]
) {
  const names =
    task?.responsible_users.map((responsible) => responsible.name || responsible.id) ?? [];
  return formatCompactNames(names.length ? names : (fallbackNames ?? []));
}

function getMyItemPeopleLine(item: FollowItem, requirement?: MockRequirement, task?: MockTask) {
  if (item.type === "任务") {
    const taskResponsible = getTaskResponsibleText(task, item.taskResponsibleNames);
    if (taskResponsible) return taskResponsible;
    if (item.owner) return item.owner;
    if (item.creatorName) return `创建人 ${item.creatorName}`;
    return "未设置";
  }

  const requirementResponsible = getRequirementResponsibleText(
    requirement,
    item.requirementResponsibleNames
  );
  if (requirementResponsible) return requirementResponsible;
  if (item.creatorName) return `创建人 ${item.creatorName}`;
  if (item.owner) return `创建人 ${item.owner}`;
  return "";
}

function getMyItemSubline(item: FollowItem, requirement?: MockRequirement, task?: MockTask) {
  const parts: string[] = [];
  const blockerLine = getMyItemBlockerLine(item);
  if (blockerLine) parts.push(blockerLine);
  if (item.type === "任务" && item.requirement) {
    parts.push(`所属需求：${item.requirement}`);
  }
  return parts.filter(Boolean);
}

function getMyItemBlockerLine(item: FollowItem) {
  const blockers = item.blockingTasks ?? [];
  if (blockers.length) {
    const visible = blockers.slice(0, 2).map(formatBlockingTaskSource);
    const suffix = blockers.length > visible.length ? ` 等 ${blockers.length} 个` : "";
    return `阻塞来源：${visible.join("、")}${suffix}`;
  }
  if (item.dependency) return `阻塞来源：${item.dependency}`;
  if (item.type === "需求") return getRequirementRiskEvidenceLine(item);
  return "";
}

function getRequirementRiskEvidenceLine(item: FollowItem) {
  const evidence = item.riskEvidence;
  if (!evidence) return "";
  const samples = evidence.samples ?? [];
  const blockerSample = samples.find((sample) => sample.blockingSources?.length);
  if (blockerSample) {
    const blockers = blockerSample.blockingSources ?? [];
    const visible = blockers.slice(0, 2).map(formatBlockingTaskSource);
    const blockerSuffix = blockers.length > visible.length ? ` 等 ${blockers.length} 个` : "";
    const affectedSuffix =
      (evidence.affectedTaskCount ?? 0) > 1
        ? ` · ${evidence.affectedTaskCount} 个任务受影响`
        : "";
    return `阻塞来源：${visible.join("、")}${blockerSuffix} · 影响任务：${compactWorkTitle(
      blockerSample.taskTitle
    )}${affectedSuffix}`;
  }

  const sample = samples[0];
  if (sample) {
    const labels = Array.from(new Set(sample.riskTypes.map(riskTypeLabel))).filter(Boolean);
    const extraCount = Math.max((evidence.totalRiskCount ?? labels.length) - labels.length, 0);
    const extraText = extraCount > 0 ? ` · 另有 ${extraCount} 项风险` : "";
    return `风险任务：${compactWorkTitle(sample.taskTitle)}${
      labels.length ? ` · ${labels.join(" / ")}` : ""
    }${extraText}`;
  }

  if (evidence.primaryRisk) {
    const countText =
      (evidence.affectedTaskCount ?? 0) > 0 ? ` · ${evidence.affectedTaskCount} 个任务受影响` : "";
    return `${riskTypeLabel(evidence.primaryRisk)}${countText}`;
  }
  return "";
}

function formatBlockingTaskSource(dependency: TaskDependencyDTO) {
  const title = dependency.task_title || dependency.title || dependency.item_id || "未命名任务";
  const displayTitle = compactWorkTitle(title);
  const ownerText = formatCompactNames(dependency.responsible_names ?? []);
  return ownerText ? `${displayTitle}（${ownerText}）` : displayTitle;
}

function compactWorkTitle(title: string) {
  const trimmed = title.trim();
  const code = trimmed.match(/^[A-Za-z]+(?:-[A-Za-z0-9]+)+/u)?.[0];
  return code || trimmed;
}

function normalizeFollowRiskLabel(item: FollowItem) {
  const value = item.risk?.trim();
  if (!value || value === "正常" || value === "正常推进" || value === "无风险") return "";
  if (value.includes("冲突")) return "依赖冲突";
  if (value.includes("阻塞")) return value.includes("依赖") ? "依赖阻塞" : "阻塞";
  if (value.includes("需求") && (value.includes("逾期") || value.includes("超期")))
    return "需求逾期";
  if (value.includes("任务") && (value.includes("逾期") || value.includes("超期")))
    return "任务逾期";
  if (value.includes("逾期") || value.includes("超期"))
    return item.type === "需求" ? "需求逾期" : "任务逾期";
  if (value.includes("依赖")) return "依赖风险";
  return value;
}

function getFollowAttentionConfig(item: FollowItem) {
  const count = item.followerCount ?? 0;
  const level = item.attentionLevel ?? "normal";
  if (level !== "high" && count <= 0) return null;
  return {
    level,
    count,
    label: attentionLabel(level, count)
  };
}

function FollowCard({
  item,
  requirement,
  task,
  showAttention,
  onView
}: {
  item: FollowItem;
  requirement?: MockRequirement;
  task?: MockTask;
  showAttention: boolean;
  onView: (item: FollowItem) => void;
}) {
  const isTask = item.type === "任务";
  const riskHint = getFollowRiskHint(item);
  const attention = showAttention ? getFollowAttentionConfig(item) : null;
  const peopleLine = getMyItemPeopleLine(item, requirement, task);
  const subline = getMyItemSubline(item, requirement, task);
  const tone = riskHint?.tone ?? "muted";
  const className = [
    "console-follow-card",
    `console-follow-card--${tone}`,
    !attention ? "console-follow-card--no-attention" : ""
  ]
    .filter(Boolean)
    .join(" ");

  return (
    <article className={className}>
      <span className="console-follow-card__rail" aria-hidden="true" />
      <div className="console-follow-card__main">
        <Tag color={isTask ? "geekblue" : "green"}>{item.type}</Tag>
        <span className="console-follow-card__status" title={item.status}>
          {item.status}
        </span>
      </div>
      <div className="console-follow-card__content">
        <strong className="console-follow-card__title" title={item.title}>
          {item.title}
        </strong>
        {subline.length ? (
          <OverflowPopoverText className="console-follow-card__subline" text={subline.join(" · ")} />
        ) : null}
      </div>
      <div className="console-follow-card__signals">
        {riskHint ? (
          <Tag color={riskHint.color} title={riskHint.label}>
            {riskHint.label}
          </Tag>
        ) : null}
        {attention ? (
          attention.count > 0 ? (
            <FollowFollowersPopover item={item} />
          ) : (
            <Tag color={followCountTagColor(attention.level)}>{attention.label}</Tag>
          )
        ) : null}
      </div>
      <OverflowPopoverText className="console-follow-card__people" text={peopleLine} />
      <div className="console-follow-card__deadline" title={item.deadline || "未设置"}>
        <ClockCircleOutlined /> 截止 {item.deadline || "未设置"}
      </div>
      <Button
        type="link"
        icon={<RightOutlined />}
        aria-label={`查看${item.title}详情`}
        onClick={() => onView(item)}
      >
        详情
      </Button>
    </article>
  );
}

function OverflowPopoverText({ className, text }: { className: string; text: string }) {
  const textRef = useRef<HTMLSpanElement>(null);
  const [overflowing, setOverflowing] = useState(false);
  const [open, setOpen] = useState(false);

  const measureOverflow = () => {
    const element = textRef.current;
    if (!element) return false;
    return (
      element.scrollWidth > element.clientWidth + 1 ||
      element.scrollHeight > element.clientHeight + 1
    );
  };

  useEffect(() => {
    const element = textRef.current;
    if (!element) return;

    const checkOverflow = () => {
      const nextOverflowing = measureOverflow();
      setOverflowing(nextOverflowing);
      if (!nextOverflowing) setOpen(false);
    };

    checkOverflow();
    const frame = window.requestAnimationFrame(checkOverflow);
    const observer = new ResizeObserver(checkOverflow);
    observer.observe(element);
    window.addEventListener("resize", checkOverflow);

    return () => {
      window.cancelAnimationFrame(frame);
      observer.disconnect();
      window.removeEventListener("resize", checkOverflow);
    };
  }, [text]);

  const handleOpenChange = (nextOpen: boolean) => {
    if (!nextOpen) {
      setOpen(false);
      return;
    }
    const nextOverflowing = measureOverflow();
    setOverflowing(nextOverflowing);
    setOpen(nextOverflowing);
  };

  const node = (
    <span ref={textRef} className={className}>
      {text}
    </span>
  );

  return (
    <Popover
      trigger="hover"
      placement="bottomLeft"
      open={open}
      onOpenChange={handleOpenChange}
      mouseEnterDelay={0.25}
      content={overflowing ? <div className="console-overflow-popover-content">{text}</div> : null}
    >
      {node}
    </Popover>
  );
}

function getFollowSummaryChips(items: FollowItem[]): SummaryChipItem[] {
  const followed = items.filter((item) => item.followedByMe).length;
  const assigned = items.filter((item) => item.assignedToMe).length;
  const created = items.filter((item) => item.createdByMe).length;
  const blocked = items.filter(isFollowBlocked).length;
  const overdue = items.filter(isFollowOverdue).length;
  const risky = items.filter(isFollowRisky).length;
  const chips: SummaryChipItem[] = [
    { label: "全部", value: items.length, tone: "default" },
    { label: "关注", value: followed, tone: "orange" },
    { label: "负责", value: assigned, tone: "blue" },
    { label: "创建", value: created, tone: "muted" },
    { label: "阻塞", value: blocked, tone: "red" },
    { label: "超期", value: overdue, tone: "red" },
    { label: "有风险", value: risky, tone: "orange" }
  ];
  return chips.filter((item) => item.label === "全部" || item.value > 0);
}

function getRiskSummaryChips(items: RiskItem[]): SummaryChipItem[] {
  const blockers = items.filter(isRiskBlocker).length;
  const conflicts = items.filter(isRiskConflict).length;
  const overdue = items.filter(
    (item) => isRiskDeadline(item) || isRiskRequirementOverdue(item)
  ).length;
  const highAttention = items.filter((item) => item.attentionLevel === "high").length;
  const chips: SummaryChipItem[] = [
    { label: "全部", value: items.length, tone: "default" },
    { label: "阻塞", value: blockers, tone: "red" },
    { label: "逾期", value: overdue, tone: "red" },
    { label: "冲突", value: conflicts, tone: "orange" },
    { label: "高关注", value: highAttention, tone: "orange" }
  ];
  return chips.filter((item) => item.label === "全部" || item.value > 0);
}

function compareDashboardMyItems(a: FollowItem, b: FollowItem) {
  const scoreDelta = getFollowPriorityScore(b) - getFollowPriorityScore(a);
  if (scoreDelta !== 0) return scoreDelta;
  return getDeadlineRank(a.deadline) - getDeadlineRank(b.deadline);
}

function compareDashboardRisks(a: RiskItem, b: RiskItem) {
  const scoreDelta = getRiskPriorityScore(b) - getRiskPriorityScore(a);
  if (scoreDelta !== 0) return scoreDelta;
  return getDeadlineRank(getRiskDeadline(a)) - getDeadlineRank(getRiskDeadline(b));
}

function getFollowPriorityScore(item: FollowItem) {
  let score = 0;
  if (isFollowBlocked(item)) score += 1200;
  if (isFollowOverdue(item)) score += 1000;
  if (item.risk.includes("依赖")) score += 720;
  if (item.assignedToMe) score += 520;
  const deadlineRank = getDeadlineRank(item.deadline);
  if (deadlineRank < 0) score += 430;
  else if (deadlineRank <= 2) score += 380 - deadlineRank * 35;
  else if (deadlineRank <= 7) score += 180 - deadlineRank * 10;
  if (item.attentionLevel === "high") score += 260;
  score += Math.min(item.attentionScore ?? 0, 160);
  score += getActivityWeight(item.activity);
  if (item.followedByMe) score += 18;
  if (item.createdByMe) score += 10;
  return score;
}

function getRiskPriorityScore(item: RiskItem) {
  let score = 0;
  if (isRiskBlocker(item)) score += 1200;
  if (isRiskConflict(item)) score += 980;
  score += Math.min(item.deadlineTaskCount ?? 0, 20) * 36;
  if (isRiskRequirementOverdue(item)) score += 520;
  score += { 高: 260, 中: 150, 低: 70 }[item.level ?? "高"];
  if (item.attentionLevel === "high") score += 130;
  const deadlineRank = getDeadlineRank(getRiskDeadline(item));
  if (deadlineRank < 0) score += 180;
  else if (deadlineRank <= 2) score += 130 - deadlineRank * 25;
  score += Math.min(item.attentionScore ?? 0, 120);
  return score;
}

function getActivityWeight(activity?: string) {
  if (!activity) return 0;
  if (activity.includes("今天")) return 80;
  if (activity.includes("昨天")) return 55;
  const match = activity.match(/(\d+)\s*天前/);
  if (match) return Math.max(0, 45 - Number(match[1]) * 6);
  return 0;
}

function isFollowBlocked(item: FollowItem) {
  return item.status === "阻塞" || item.risk.includes("阻塞");
}

function isFollowOverdue(item: FollowItem) {
  return item.risk.includes("超期") || item.risk.includes("逾期");
}

function isFollowRisky(item: FollowItem) {
  return isFollowBlocked(item) || isFollowOverdue(item) || item.risk.includes("依赖");
}

function isRiskBlocker(item: RiskItem) {
  return (
    (item.dependencyBlockerCount ?? 0) > 0 ||
    hasRiskType(item, "dependency_blocker") ||
    getRiskTagLabels(item).some((label) => label.includes("阻塞"))
  );
}

function isRiskDeadline(item: RiskItem) {
  return (
    (item.deadlineTaskCount ?? 0) > 0 ||
    hasRiskType(item, "deadline") ||
    getRiskTagLabels(item).some((label) => label.includes("任务逾期") || label.includes("任务超期"))
  );
}

function isRiskConflict(item: RiskItem) {
  return (
    (item.dependencyConflictCount ?? 0) > 0 ||
    hasRiskType(item, "dependency_conflict") ||
    getRiskTagLabels(item).some((label) => label.includes("冲突"))
  );
}

function isRiskRequirementOverdue(item: RiskItem) {
  return item.requirementOverdue === true || hasRiskType(item, "requirement_overdue");
}

function hasRiskType(item: RiskItem, riskType: RiskType) {
  if (item.riskType === riskType) return true;
  if (item.riskTypes?.includes(riskType)) return true;
  return item.representativeTask?.riskTypes.includes(riskType) ?? false;
}

function getDeadlineRank(value?: string) {
  if (!value || value === "未设置") return 9999;
  if (value.includes("已超") || value.includes("超期") || value.includes("逾期")) return -10;
  if (value.includes("今天")) return 0;
  if (value.includes("明天")) return 1;
  if (value.includes("后天")) return 2;
  const parsed = dayjs(value);
  if (!parsed.isValid()) return 999;
  return parsed.startOf("day").diff(dayjs().startOf("day"), "day");
}

type FollowRiskHint = { label: string; color: string; tone: "red" | "orange" | "blue" };

function getFollowRiskHint(item: FollowItem): FollowRiskHint | null {
  const label = normalizeFollowRiskLabel(item);
  if (!label) return null;
  if (label.includes("阻塞") || label.includes("逾期") || label.includes("超期")) {
    return { label, color: "red", tone: "red" };
  }
  if (label.includes("冲突") || label.includes("依赖")) {
    return { label, color: "orange", tone: "orange" };
  }
  return { label, color: "blue", tone: "blue" };
}

function FollowFollowersPopover({ item }: { item: FollowItem }) {
  const [open, setOpen] = useState(false);
  if ((item.followerCount ?? 0) <= 0) return null;

  return (
    <Popover
      trigger={["hover", "click"]}
      placement="leftTop"
      open={open}
      onOpenChange={setOpen}
      content={<FollowFollowersContent item={item} enabled={open} />}
    >
      <span>
        <FollowCountTag count={item.followerCount ?? 0} level={item.attentionLevel ?? "normal"} />
      </span>
    </Popover>
  );
}

function FollowCountTag({ count, level }: { count: number; level: AttentionLevel }) {
  return (
    <Tag color={followCountTagColor(level)} title="悬停查看关注人">
      {attentionLabel(level, count)}
    </Tag>
  );
}

function followCountTagColor(level: AttentionLevel) {
  if (level === "high") return "purple";
  if (level === "important") return "orange";
  if (level === "notable") return "blue";
  return "default";
}

function FollowFollowersContent({ item, enabled }: { item: FollowItem; enabled: boolean }) {
  const targetType = item.type === "任务" ? "task" : "requirement";
  const targetId = item.type === "任务" ? item.taskId : item.requirementId;
  const followersQuery = useQuery({
    queryKey: ["follows", "followers", targetType, targetId],
    queryFn: () => fetchFollowFollowers(targetType, targetId ?? ""),
    enabled: enabled && Boolean(targetId),
    staleTime: 30_000
  });

  if (!enabled) return <span className="console-follow-followers__state">悬停查看关注人</span>;
  if (followersQuery.isLoading)
    return <span className="console-follow-followers__state">加载中...</span>;
  if (followersQuery.isError)
    return <span className="console-follow-followers__state">关注人加载失败</span>;

  const followerItems = followersQuery.data?.items ?? [];
  const groups = groupFollowersByRole(followerItems);
  const totalCount = followersQuery.data?.total ?? item.followerCount ?? 0;
  const followers = groups.flatMap((group) =>
    group.followers.map((follower) => ({
      ...follower,
      roleLabel: group.shortLabel
    }))
  );
  return (
    <div className="console-follow-followers">
      <div className="console-follow-followers__header">
        <div>
          <strong>关注人员</strong>
          <span>
            {attentionLabel(item.attentionLevel ?? "normal", totalCount)} · {totalCount} 人
          </span>
        </div>
      </div>
      <div className="console-follow-followers__users">
        {followers.map((follower) => (
          <span key={follower.id}>
            <strong>{follower.name}</strong>
            {follower.roleLabel ? <em>{follower.roleLabel}</em> : null}
          </span>
        ))}
      </div>
      {totalCount > followers.length ? (
        <div className="console-follow-followers__more">已显示前 {followers.length} 个</div>
      ) : null}
    </div>
  );
}

function groupFollowersByRole(followers: DashboardFollowFollowerDTO[]) {
  const roleOrder = ["director", "team_leader", "pm", "employee", "admin"];
  const roleLabels: Record<string, string> = {
    director: "总监",
    team_leader: "TL",
    pm: "PM",
    employee: "员工",
    admin: "管理员"
  };
  const roleShortLabels: Record<string, string> = {
    director: "总监",
    team_leader: "TL",
    pm: "PM",
    employee: "",
    admin: "管理员"
  };
  return roleOrder
    .map((role) => ({
      role,
      label: roleLabels[role] ?? role,
      shortLabel: roleShortLabels[role] ?? role,
      followers: followers.filter((follower) => follower.role === role)
    }))
    .filter((group) => group.followers.length > 0);
}

function RiskAttentionPill({ level }: { level: AttentionLevel }) {
  const config = {
    normal: null,
    notable: { label: "一般关注", tone: "blue" },
    important: { label: "重点关注", tone: "orange" },
    high: { label: "高关注", tone: "red" }
  }[level];
  return config ? (
    <span className={`console-risk-attention console-risk-attention--${config.tone}`}>
      {config.label}
    </span>
  ) : null;
}

function attentionLabel(level: AttentionLevel, count: number) {
  if (count <= 0) return "暂无关注";
  if (level === "high") return "高关注";
  if (level === "important") return "重点关注";
  if (level === "notable") return "一般关注";
  return "普通关注";
}

function RiskCard({
  item,
  requirement,
  onAction
}: {
  item: RiskItem;
  requirement?: MockRequirement;
  onAction: (item: RiskItem) => void;
}) {
  const tone = item.tone ?? "red";
  const level = item.level ?? "高";
  const title = getRiskTitle(item);
  const primaryRisk = getPrimaryRiskType(item);
  const summary = getRiskSummaryText(item);
  const riskTaskLine = getRepresentativeRiskTaskLine(item);
  const blockerLine = getRiskBlockingSourceLine(item);
  const ownerText = getRequirementResponsibleText(requirement, item.requirementResponsibleNames);
  const deadline = getRiskDeadline(item);
  return (
    <article className={`console-risk-card console-risk-card--${tone}`}>
      <span className="console-risk-card__rail" aria-hidden="true" />
      <div className="console-risk-card__main">
        <div className="console-risk-card__primary">
          <span className={`console-risk-tag console-risk-tag--${tone}`}>{level}</span>
          <span className={`console-risk-tag console-risk-tag--${tone}`}>{primaryRisk.label}</span>
          <RiskAttentionPill level={item.attentionLevel ?? "normal"} />
          <strong title={title}>{title}</strong>
        </div>
        <span className="console-risk-card__summary" title={summary}>
          <em>风险摘要：</em>
          {summary}
        </span>
        {riskTaskLine ? (
          <span className="console-risk-card__task" title={riskTaskLine}>
            <em>风险任务：</em>
            {riskTaskLine}
          </span>
        ) : null}
        {blockerLine ? (
          <OverflowPopoverText
            className="console-risk-card__blocker"
            text={`阻塞来源：${blockerLine}`}
          />
        ) : null}
        {ownerText ? (
          <span className="console-risk-card__owner" title={ownerText}>
            <em>需求负责人：</em>
            {ownerText}
          </span>
        ) : null}
      </div>
      <div className="console-risk-card__meta">
        <span title={deadline}>
          <ClockCircleOutlined /> 截止 {deadline}
        </span>
      </div>
      <Button type="link" icon={<LinkOutlined />} onClick={() => onAction(item)}>
        {getRiskActionLabel(item)}
      </Button>
    </article>
  );
}

function getPrimaryRiskType(item: RiskItem) {
  if (isRiskBlocker(item)) return { label: "依赖阻塞", tone: "red" as const };
  if (isRiskConflict(item)) return { label: "依赖冲突", tone: "orange" as const };
  if (isRiskDeadline(item)) return { label: "任务逾期", tone: "red" as const };
  if (isRiskRequirementOverdue(item)) return { label: "需求逾期", tone: "red" as const };
  return { label: item.source ?? "风险", tone: "orange" as const };
}

function getRiskSummaryParts(item: RiskItem) {
  const parts: string[] = [];
  if (item.requirementOverdue) parts.push("需求逾期");
  if ((item.deadlineTaskCount ?? 0) > 0) parts.push(`${item.deadlineTaskCount} 个任务逾期`);
  if ((item.dependencyBlockerCount ?? 0) > 0)
    parts.push(`${item.dependencyBlockerCount} 个依赖阻塞`);
  if ((item.dependencyConflictCount ?? 0) > 0)
    parts.push(`${item.dependencyConflictCount} 个依赖冲突`);
  return parts;
}

function sanitizeRiskSummary(value?: string) {
  const legacyRiskTaskLabel = "重点" + "任务";
  return value?.replaceAll(legacyRiskTaskLabel, "风险任务").trim() ?? "需要查看并处理";
}

function getRiskSummaryText(item: RiskItem) {
  const parts = getRiskSummaryParts(item);
  return parts.length ? parts.join(" · ") : sanitizeRiskSummary(item.summary ?? item.reason);
}

function getRepresentativeRiskTaskLine(item: RiskItem) {
  const task = item.representativeTask;
  if (!task) return "";
  const parts = [task.title];
  if (task.deadline) parts.push(`截止 ${task.deadline}`);
  const riskLabels = Array.from(
    new Set(
      task.riskTypes.filter((riskType) => riskType !== "requirement_overdue").map(riskTypeLabel)
    )
  );
  if (riskLabels.length) parts.push(riskLabels.join(" / "));
  return parts.join(" · ");
}

function getRiskBlockingSourceLine(item: RiskItem) {
  const task = item.representativeTask;
  if (!task) return "";
  const blockers = task.blockingDependencies ?? [];
  if (blockers.length) {
    const visible = blockers.slice(0, 2).map(formatRiskBlockingDependency);
    const suffix = blockers.length > visible.length ? ` +${blockers.length - visible.length}` : "";
    return `${visible.join("、")}${suffix}`;
  }
  if ((task.unfinishedDependencyCount ?? 0) > 0) {
    return `${task.unfinishedDependencyCount} 个上游任务未完成`;
  }
  return "";
}

function formatRiskBlockingDependency(dependency: TaskDependencyDTO) {
  const title = compactWorkTitle(
    dependency.task_title || dependency.title || dependency.item_id || "未命名任务"
  );
  const ownerText = formatCompactNames(dependency.responsible_names ?? []);
  const parts = [ownerText ? `${title}（${ownerText}）` : title];
  const status = riskDependencyStatusLabel(dependency.status);
  if (status) parts.push(status);
  if (dependency.due_date) parts.push(`截止 ${dependency.due_date}`);
  return parts.join(" · ");
}

function riskDependencyStatusLabel(status?: string) {
  if (!status) return "";
  if (status === "todo") return "未开始";
  if (status === "in_progress" || status === "active") return "进行中";
  if (status === "review") return "评审";
  if (status === "done" || status === "completed") return "已完成";
  if (status === "cancelled") return "已取消";
  return status;
}

function getRiskTagLabels(item: RiskItem) {
  const parts = getRiskSummaryParts(item);
  return parts.length ? parts : [getPrimaryRiskType(item).label];
}

function getRiskTitle(item: RiskItem) {
  if (item.displayType === "single_task" && item.representativeTask) {
    return item.representativeTask.title;
  }
  return item.requirementTitle ?? item.title ?? item.target ?? "风险提示";
}

function getRiskDeadline(item: RiskItem) {
  if (item.deadline) return item.deadline;
  if (item.representativeTask?.deadline) return item.representativeTask.deadline;
  return "未设置";
}

function riskTypeLabel(riskType: RiskType) {
  if (riskType === "requirement_overdue") return "需求逾期";
  if (riskType === "dependency_blocker") return "依赖阻塞";
  if (riskType === "dependency_conflict") return "依赖冲突";
  return "任务逾期";
}

function getRiskActionLabel(item: RiskItem) {
  if (item.displayType === "requirement_group") return item.actionText ?? "查看需求";
  const riskTypes = item.representativeTask?.riskTypes ?? (item.riskType ? [item.riskType] : []);
  if (riskTypes.includes("dependency_blocker") && !riskTypes.includes("deadline")) {
    return item.actionText ?? "处理依赖";
  }
  if (riskTypes.includes("dependency_conflict") && !riskTypes.includes("deadline")) {
    return item.actionText ?? "检查排期";
  }
  if (item.displayType === "single_task" || riskTypes.includes("deadline")) {
    return item.actionText ?? "查看任务";
  }
  return item.actionText ?? "查看风险";
}
