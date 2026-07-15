import {
  AppstoreOutlined,
  CloseOutlined,
  ClockCircleOutlined,
  DeleteOutlined,
  EditOutlined,
  FileTextOutlined,
  InfoCircleOutlined,
  LinkOutlined,
  MoreOutlined,
  PlusOutlined,
  ReloadOutlined,
  RightOutlined,
  RollbackOutlined,
  StarFilled,
  StarOutlined,
  StopOutlined,
  UnorderedListOutlined,
  UserOutlined,
  WarningOutlined
} from "@ant-design/icons";
import { DragDropContext, Draggable, Droppable } from "@hello-pangea/dnd";
import type { DropResult } from "@hello-pangea/dnd";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import {
  Alert,
  App,
  Badge,
  Button,
  DatePicker,
  Checkbox,
  Descriptions,
  Drawer,
  Dropdown,
  Empty,
  Form,
  Input,
  InputNumber,
  Modal,
  Popover,
  Progress,
  Select,
  Segmented,
  Skeleton,
  Slider,
  Tabs,
  Table,
  Tag,
  Timeline,
  Tooltip
} from "antd";
import type { TableProps } from "antd";
import dayjs from "dayjs";
import type { ReactNode } from "react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";

import { useAuth } from "@/shared/auth/authContext";
import { ROLE_LABELS, type User, type UserRole } from "@/shared/auth/types";
import { PagePanel } from "@/shared/components/PagePanel/PagePanel";
import { isEditConflict } from "@/shared/request/apiError";
import { appendSearch } from "@/shared/utils/urlQuery";

import { fetchAllSessionTokens, fetchFollowFollowers, fetchSessionTokens } from "../../api/client";
import type {
  AttentionLevel,
  DashboardFollowFollowerDTO,
  FollowTargetType,
  SessionTokens,
  WorkItemEventDTO
} from "../../api/types";
import { AcceptanceCriteriaEditor } from "../components/AcceptanceCriteriaEditor";
import {
  RequirementMetricCard,
  RequirementMetricGrid,
  type RequirementMetricTone
} from "../components/RequirementMetricCard";
import { requirementsBoardApi } from "../api/requirementsBoardApi";
import { invalidateRequirementTaskWorkspace } from "../queryInvalidation";
import {
  acceptanceCriteriaRules,
  dependencyArrayRules,
  descriptionRules,
  normalizeCriteria,
  normalizeOptionalText,
  normalizeRequiredText,
  optionalUrlRules,
  requiredSelectRules,
  titleRules
} from "../validation/requirementTaskValidation";
import type {
  FavoriteTargetType,
  MockAssignee,
  MockFavorite,
  MockRequirement,
  MockTask,
  MockTaskDependency,
  MockTaskPriority,
  MockTaskStatus,
  MockTeam,
  MockTokenSource,
  RequirementPriority,
  RequirementStage
} from "../types";
import "./RequirementsBoard.css";

type BoardView = "board" | "tree";
type WorkScope = "all" | "mine" | "followed" | "assigned" | "created";
type RiskFilter = "requirement_overdue" | "task_overdue" | "blocked" | "dependency_conflict";
type RequirementRiskTone = "danger" | "warning";
type RequirementRiskBadge = {
  value: RiskFilter;
  label: string;
  count: number;
  tone: RequirementRiskTone;
};
type TaskRiskBadge = {
  key: string;
  label: string;
  tone: RequirementRiskTone;
};
type RequirementQuickEditField = "deadline" | "owner" | "teams";

const EDIT_CONFLICT_MESSAGE = "内容已被其他人更新，请刷新后再操作";

type MessageApi = {
  warning: (content: string) => unknown;
};

function handleEditConflict(error: unknown, messageApi: MessageApi, queryClient: QueryClient) {
  if (!isEditConflict(error)) return false;
  messageApi.warning(EDIT_CONFLICT_MESSAGE);
  void invalidateRequirementTaskWorkspace(queryClient);
  return true;
}

const STATUS_COLUMNS: Array<{
  value: RequirementStage;
  label: string;
  description: string;
  emptyText: string;
  tone: string;
}> = [
  {
    value: "todo",
    label: "待开始",
    description: "等待拆解或排期",
    emptyText: "暂无待开始需求",
    tone: "gray"
  },
  {
    value: "review",
    label: "评审",
    description: "确认范围与验收标准",
    emptyText: "暂无评审中需求",
    tone: "purple"
  },
  {
    value: "active",
    label: "进行中",
    description: "任务正在推进",
    emptyText: "暂无进行中需求",
    tone: "blue"
  },
  {
    value: "completed",
    label: "完成",
    description: "已经完成的需求",
    emptyText: "暂无完成需求",
    tone: "green"
  }
];

const CANCELLED_COLUMN = {
  value: "cancelled" as const,
  label: "已取消",
  description: "已取消的需求",
  emptyText: "暂无已取消需求",
  tone: "gray"
};

const STATUS_OPTIONS = [
  ...STATUS_COLUMNS.map(({ value, label }) => ({ value, label })),
  { value: "cancelled", label: "已取消" }
];

const PRIORITY_OPTIONS: Array<{ value: RequirementPriority; label: string }> = [
  { value: "urgent", label: "紧急" },
  { value: "high", label: "高" },
  { value: "medium", label: "中" },
  { value: "low", label: "低" }
];

const RISK_OPTIONS: Array<{ value: RiskFilter; label: string }> = [
  { value: "requirement_overdue", label: "需求逾期" },
  { value: "task_overdue", label: "任务逾期" },
  { value: "blocked", label: "依赖阻塞" },
  { value: "dependency_conflict", label: "依赖冲突" }
];

const WORK_SCOPE_OPTIONS: Array<{ value: WorkScope; label: string }> = [
  { value: "mine", label: "我的事项" },
  { value: "followed", label: "关注" },
  { value: "assigned", label: "负责" },
  { value: "created", label: "创建" },
  { value: "all", label: "全部" }
];

const EMPTY_REQUIREMENTS: MockRequirement[] = [];
const EMPTY_TASKS: MockTask[] = [];
const EMPTY_DEPENDENCIES: MockTaskDependency[] = [];
const EMPTY_TOKEN_SOURCES: MockTokenSource[] = [];
const EMPTY_FAVORITES: MockFavorite[] = [];
const REQUIREMENT_LIST_PAGE_SIZE = 50;
const REQUIREMENT_BOARD_COLUMN_PAGE_SIZE = 20;
const DEPENDENCY_PICKER_PAGE_SIZE = 30;

function isRiskFilter(value: string | null): value is RiskFilter {
  return (
    value === "requirement_overdue" ||
    value === "task_overdue" ||
    value === "blocked" ||
    value === "dependency_conflict"
  );
}

function isWorkScope(value: string | null): value is WorkScope {
  return (
    value === "all" ||
    value === "mine" ||
    value === "followed" ||
    value === "assigned" ||
    value === "created"
  );
}

function defaultRequirementWorkScope(user: User | null): WorkScope {
  if (
    user?.role === "employee" ||
    user?.role === "pm" ||
    user?.role === "team_leader" ||
    user?.role === "director"
  ) {
    return "mine";
  }
  return "all";
}

function canManageTaskForUser(user: User | null, task?: MockTask) {
  if (!user || !task) return false;
  return Boolean(
    task.can_update_meta ||
      task.can_update_status ||
      task.can_update_progress ||
      task.can_manage_dependencies ||
      task.can_delete
  );
}

const STAGE_META: Record<RequirementStage, { label: string; color: string }> = {
  todo: { label: "待开始", color: "default" },
  review: { label: "评审", color: "purple" },
  active: { label: "进行中", color: "processing" },
  completed: { label: "完成", color: "success" },
  cancelled: { label: "已取消", color: "default" }
};

const PRIORITY_META: Record<RequirementPriority, { label: string; color: string }> = {
  low: { label: "低", color: "default" },
  medium: { label: "中", color: "gold" },
  high: { label: "高", color: "orange" },
  urgent: { label: "紧急", color: "red" }
};

const TASK_STATUS_META: Record<MockTaskStatus, { label: string; tone: string }> = {
  todo: { label: "未开始", tone: "neutral" },
  in_progress: { label: "进行中", tone: "info" },
  done: { label: "已完成", tone: "success" }
};

const REQUIREMENT_STAGE_TONE: Record<RequirementStage, string> = {
  todo: "neutral",
  review: "warning",
  active: "info",
  completed: "success",
  cancelled: "neutral"
};

const PRIORITY_TONE: Record<RequirementPriority | MockTaskPriority, string> = {
  low: "low",
  medium: "medium",
  high: "high",
  urgent: "urgent"
};

function RequirementStageTag({ stage }: { stage: RequirementStage }) {
  const meta = STAGE_META[stage];
  return (
    <span className={`requirements-status-pill is-${REQUIREMENT_STAGE_TONE[stage]}`}>
      {meta.label}
    </span>
  );
}

function TaskStatusPill({ status }: { status: MockTaskStatus }) {
  const meta = TASK_STATUS_META[status];
  return <span className={`requirements-status-pill is-${meta.tone}`}>{meta.label}</span>;
}

function PriorityPill({ priority }: { priority: RequirementPriority | MockTaskPriority }) {
  const meta = PRIORITY_META[priority];
  return (
    <span className={`requirements-priority-pill is-${PRIORITY_TONE[priority]}`}>{meta.label}</span>
  );
}

function RequirementPriorityTag({ priority }: { priority: RequirementPriority }) {
  const meta = PRIORITY_META[priority];
  return <Tag color={meta.color}>{meta.label}</Tag>;
}

function formatDateTime(value?: string) {
  return value ? dayjs(value).format("YYYY-MM-DD HH:mm") : "-";
}

function formatDate(value?: string) {
  return value ? dayjs(value).format("YYYY-MM-DD") : "未设置";
}

function getDependencyType(dependency: MockTaskDependency) {
  return dependency.item_type ?? "task";
}

function getDependencyId(dependency: MockTaskDependency) {
  return dependency.item_id || dependency.task_id || "";
}

function getDependencyTitle(dependency: MockTaskDependency) {
  return (
    dependency.title ||
    dependency.task_title ||
    dependency.item_id ||
    dependency.task_id ||
    "未命名工作项"
  );
}

function getDependencyLabel(dependency: MockTaskDependency) {
  const title = getDependencyTitle(dependency);
  const requirementTitle = dependency.requirement_title;
  if (getDependencyType(dependency) === "requirement") {
    return `需求 · ${title}`;
  }
  return requirementTitle ? `${title} · ${requirementTitle}` : title;
}

function isDependencyDone(dependency: MockTaskDependency) {
  return getDependencyType(dependency) === "requirement"
    ? dependency.status === "completed"
    : dependency.status === "done";
}

function getBlockingDependencies(task: MockTask) {
  return task.dependencies.filter((dependency) => !isDependencyDone(dependency));
}

function getLoadedDependencyTask(dependency: MockTaskDependency, tasks: MockTask[]) {
  if (getDependencyType(dependency) !== "task") return undefined;
  const dependencyId = getDependencyId(dependency);
  return tasks.find((item) => item.id === dependencyId);
}

function dependencyStatusText(dependency: MockTaskDependency, task?: MockTask) {
  if (task) return TASK_STATUS_META[task.status]?.label ?? task.status;
  if (getDependencyType(dependency) === "requirement") {
    return STAGE_META[dependency.status as RequirementStage]?.label ?? (dependency.status || "-");
  }
  return TASK_STATUS_META[dependency.status as MockTaskStatus]?.label ?? (dependency.status || "-");
}

function dependencyTone(dependency: MockTaskDependency, task?: MockTask) {
  const status = task?.status ?? dependency.status;
  if (status === "done" || status === "completed") return "success";
  if (status === "in_progress" || status === "active" || status === "review") return "processing";
  return "error";
}

function isBeforeToday(value?: string) {
  if (!value) return false;
  const parsed = dayjs(value);
  return parsed.isValid() && parsed.startOf("day").isBefore(dayjs().startOf("day"));
}

function getRequirementRiskBadges(
  requirement: MockRequirement,
  requirementTasks: MockTask[]
): RequirementRiskBadge[] {
  const summary = requirement.risk_summary;
  const requirementOverdue =
    (summary?.requirement_overdue ?? 0) > 0 ||
    (requirement.status !== "completed" &&
      requirement.status !== "cancelled" &&
      isBeforeToday(requirement.deadline));
  const taskOverdue =
    summary?.overdue ??
    requirementTasks.filter(
      (task) => task.risk_types?.includes("overdue") || isBeforeToday(task.due_date)
    ).length;
  const blocked = summary?.blocked ?? requirementTasks.filter(isTaskBlocked).length;
  const dependencyConflict =
    summary?.dependency_conflict ??
    requirementTasks.filter((task) => task.risk_types?.includes("dependency_conflict")).length;

  const badges: RequirementRiskBadge[] = [];
  if (requirementOverdue) {
    badges.push({ value: "requirement_overdue", label: "需求已逾期", count: 1, tone: "danger" });
  }
  if (taskOverdue > 0) {
    badges.push({
      value: "task_overdue",
      label: `${taskOverdue} 个任务逾期`,
      count: taskOverdue,
      tone: "danger"
    });
  }
  if (blocked > 0) {
    badges.push({
      value: "blocked",
      label: `${blocked} 个依赖阻塞`,
      count: blocked,
      tone: "danger"
    });
  }
  if (dependencyConflict > 0) {
    badges.push({
      value: "dependency_conflict",
      label: `${dependencyConflict} 个依赖冲突`,
      count: dependencyConflict,
      tone: "warning"
    });
  }
  return badges;
}

function formatRiskOverflow(count: number) {
  return `另有 ${count} 项风险`;
}

function isTaskBlocked(task: Pick<MockTask, "risk_types">) {
  return task.risk_types?.includes("blocked") ?? false;
}

function getTaskRiskBadges(task: MockTask): TaskRiskBadge[] {
  if (task.status === "done") return [];
  const riskTypes = task.risk_types ?? [];
  const badges: TaskRiskBadge[] = [];
  if (riskTypes.includes("overdue") || isBeforeToday(task.due_date)) {
    badges.push({ key: "overdue", label: "逾期", tone: "danger" });
  }
  if (riskTypes.includes("blocked")) {
    badges.push({ key: "blocked", label: "依赖阻塞", tone: "danger" });
  }
  if (riskTypes.includes("dependency_conflict")) {
    badges.push({ key: "dependency_conflict", label: "依赖冲突", tone: "warning" });
  }
  return badges;
}

function getRequirementOwnerLabel(requirement: MockRequirement) {
  const responsibleNames = requirement.responsible_users
    .map((responsible) => responsible.name || responsible.id)
    .filter(Boolean);
  if (responsibleNames.length === 0) {
    return "未指定负责人";
  }
  const visibleResponsibles = responsibleNames.slice(0, 2);
  const restCount = responsibleNames.length - visibleResponsibles.length;
  return restCount > 0
    ? `${visibleResponsibles.join("、")} +${restCount}`
    : visibleResponsibles.join("、");
}

function getRequirementOwnerTitle(requirement: MockRequirement) {
  const responsibleNames = requirement.responsible_users
    .map((responsible) => responsible.name || responsible.id)
    .filter(Boolean);
  return responsibleNames.length ? responsibleNames.join("、") : "未指定负责人";
}

function getTaskResponsibleNames(
  task: Pick<MockTask, "responsible_users" | "responsible_user_ids">
) {
  const names = task.responsible_users
    .map((responsible) => responsible.name || responsible.id)
    .filter(Boolean);
  if (names.length) return names;
  return task.responsible_user_ids.filter(Boolean);
}

function getTaskResponsibleLabel(
  task: Pick<MockTask, "responsible_users" | "responsible_user_ids">,
  limit = 2
) {
  const names = getTaskResponsibleNames(task);
  if (!names.length) return "未分配";
  const visible = names.slice(0, limit);
  const restCount = names.length - visible.length;
  return restCount > 0 ? `${visible.join("、")} +${restCount}` : visible.join("、");
}

function getTaskResponsibleTitle(
  task: Pick<MockTask, "responsible_users" | "responsible_user_ids">
) {
  const names = getTaskResponsibleNames(task);
  return names.length ? names.join("、") : "未分配";
}

function getRequirementTeamCompactLabel(requirement: MockRequirement, limit = 2) {
  const names = requirement.team_names.filter(Boolean);
  if (!names.length) return "未指定参与团队";
  const visible = names.slice(0, limit);
  const restCount = names.length - visible.length;
  return restCount > 0 ? `${visible.join("、")} +${restCount}` : visible.join("、");
}

function getRequirementTeamTitle(requirement: MockRequirement) {
  return requirement.team_names.length ? requirement.team_names.join("、") : "未指定参与团队";
}

function getTaskDependencySummary(task: MockTask) {
  if (!task.dependencies.length) return "无上游依赖";
  return `${task.dependencies.length} 个上游依赖`;
}

function getTaskDependencyTitle(task: MockTask) {
  if (!task.dependencies.length) return "无上游依赖";
  return task.dependencies.map(getDependencyLabel).join("、");
}

function getTaskRiskLabel(task: MockTask) {
  const riskBadges = getTaskRiskBadges(task);
  return riskBadges.length ? riskBadges.map((risk) => risk.label).join("、") : "正常";
}

function RequirementAttentionPill({ requirement }: { requirement: MockRequirement }) {
  return (
    <AttentionPill
      targetType="requirement"
      targetId={requirement.id}
      summary={requirement.follow_summary}
    />
  );
}

function TaskAttentionPill({ task }: { task: MockTask }) {
  return <AttentionPill targetType="task" targetId={task.id} summary={task.follow_summary} />;
}

function AttentionPill({
  targetType,
  targetId,
  summary
}: {
  targetType: FollowTargetType;
  targetId: string;
  summary: { count: number; score: number; level: AttentionLevel };
}) {
  const [open, setOpen] = useState(false);
  const label = attentionLabel(summary.level, summary.count);
  const pill = (
    <span
      className={`requirements-attention-pill is-${summary.level}`}
      title={`关注权重 ${summary.score}`}
    >
      {label}
    </span>
  );
  if (summary.count <= 0) {
    return pill;
  }
  return (
    <Popover
      trigger={["hover", "click"]}
      placement="leftTop"
      open={open}
      onOpenChange={setOpen}
      content={
        <AttentionFollowers
          targetType={targetType}
          targetId={targetId}
          enabled={open}
          fallbackCount={summary.count}
          level={summary.level}
        />
      }
    >
      <span onClick={(event) => event.stopPropagation()}>{pill}</span>
    </Popover>
  );
}

function attentionLabel(level: AttentionLevel, count: number) {
  if (count <= 0) return "暂无关注";
  if (level === "high") return "高关注";
  if (level === "important") return "重点关注";
  if (level === "notable") return "一般关注";
  return "普通关注";
}

function AttentionFollowers({
  targetType,
  targetId,
  enabled,
  fallbackCount,
  level
}: {
  targetType: FollowTargetType;
  targetId: string;
  enabled: boolean;
  fallbackCount: number;
  level: AttentionLevel;
}) {
  const followersQuery = useQuery({
    queryKey: ["follows", "followers", targetType, targetId],
    queryFn: () => fetchFollowFollowers(targetType, targetId),
    enabled,
    staleTime: 30_000
  });

  if (!enabled)
    return <span className="requirements-attention-followers__state">悬停查看关注人</span>;
  if (followersQuery.isLoading)
    return <span className="requirements-attention-followers__state">加载中...</span>;
  if (followersQuery.isError)
    return <span className="requirements-attention-followers__state">关注人加载失败</span>;

  const followerItems = followersQuery.data?.items ?? [];
  const groups = groupFollowersByRole(followerItems);
  const totalCount = followersQuery.data?.total ?? fallbackCount;
  const followers = groups.flatMap((group) =>
    group.followers.map((follower) => ({
      ...follower,
      roleLabel: group.shortLabel
    }))
  );
  if (!groups.length) {
    return <span className="requirements-attention-followers__state">暂无关注人</span>;
  }
  return (
    <div className="requirements-attention-followers">
      <div className="requirements-attention-followers__header">
        <div>
          <strong>关注人员</strong>
          <span>
            {attentionLabel(level, totalCount)} · {totalCount} 人
          </span>
        </div>
      </div>
      <div className="requirements-attention-followers__users">
        {followers.map((follower) => (
          <span key={follower.id}>
            <strong>{follower.name}</strong>
            {follower.roleLabel ? <em>{follower.roleLabel}</em> : null}
          </span>
        ))}
      </div>
      {totalCount > followers.length ? (
        <div className="requirements-attention-followers__more">已显示前 {followers.length} 个</div>
      ) : null}
    </div>
  );
}

function AttentionScoreHelp() {
  return (
    <div className="requirements-attention-help">
      <strong>关注度评分</strong>
      <p>按关注人的角色权重累加，用来判断需求是否需要优先跟进。</p>
      <div className="requirements-attention-help__grid">
        <span>总监</span>
        <strong>100</strong>
        <span>TL</span>
        <strong>50</strong>
        <span>PM</span>
        <strong>40</strong>
        <span>员工</span>
        <strong>10</strong>
      </div>
      <div className="requirements-attention-help__levels">
        <span>高关注：150+</span>
        <span>重点关注：80-149</span>
        <span>一般关注：40-79</span>
        <span>普通关注：1-39</span>
      </div>
    </div>
  );
}

function AttentionColumnTitle() {
  return (
    <span className="requirements-attention-title">
      关注度
      <Popover trigger={["hover", "click"]} placement="leftTop" content={<AttentionScoreHelp />}>
        <InfoCircleOutlined
          className="requirements-attention-title__icon"
          onClick={(event) => event.stopPropagation()}
        />
      </Popover>
    </span>
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

function formatTokens(value: number) {
  if (!value) return "0";
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`;
  if (value >= 1_000) return `${Math.round(value / 1_000)}K`;
  return String(value);
}

function sumTokensFromSources(ids: string[], sourceMap: Map<string, MockTokenSource>) {
  return ids.reduce((total, id) => total + (sourceMap.get(id)?.token ?? 0), 0);
}

function formatTokenSourceTime(value: string) {
  return dayjs(value).format("MM-DD HH:mm");
}

function realSessionId(session: SessionTokens) {
  return session.local_session_id || session.session_ref;
}

function sessionRowKey(session: SessionTokens) {
  return (
    session.slice_key ||
    `${session.session_id}:${session.activity_date || session.activity_start_at || session.started_at}`
  );
}

function formatSessionActivityRange(session: SessionTokens) {
  const start = session.activity_start_at || session.started_at;
  const end = session.activity_end_at;
  if (!end || end === start) return formatTokenSourceTime(start);
  return `${formatTokenSourceTime(start)} ~ ${formatTokenSourceTime(end)}`;
}

function getQuickEditPopupContainer(triggerNode: HTMLElement) {
  return triggerNode.parentElement ?? document.body;
}

function getTaskModalPopupContainer() {
  return document.body;
}

function RequirementProgress({ value }: { value: number }) {
  return (
    <div className="requirements-board__progress">
      <Progress percent={value} showInfo={false} strokeColor={{ from: "#2563eb", to: "#14b8a6" }} />
      <strong>{value}%</strong>
    </div>
  );
}

export function RequirementsListPage() {
  const { message, modal } = App.useApp();
  const { user } = useAuth();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [searchParams, setSearchParams] = useSearchParams();
  const [searchDraft, setSearchDraft] = useState(searchParams.get("keyword") ?? "");
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [selectedRequirement, setSelectedRequirement] = useState<MockRequirement>();
  const [statusEditRequirement, setStatusEditRequirement] = useState<MockRequirement>();
  const [ownerPromptRequirement, setOwnerPromptRequirement] = useState<MockRequirement>();
  const [selectedTask, setSelectedTask] = useState<MockTask>();
  const [taskHistory, setTaskHistory] = useState<MockTask[]>([]);
  const [creatorOpen, setCreatorOpen] = useState(false);

  const viewParam = searchParams.get("view");
  const defaultView: BoardView = "board";
  const view: BoardView =
    viewParam === "tree" || viewParam === "list"
      ? "tree"
      : viewParam === "board"
        ? "board"
        : defaultView;
  const favoriteParam = searchParams.get("favorite");
  const scopeParam = searchParams.get("scope");
  const keyword = searchParams.get("keyword") ?? "";
  const priority = (searchParams.get("priority") as RequirementPriority | null) ?? undefined;
  const status = (searchParams.get("status") as RequirementStage | null) ?? undefined;
  const riskParam = searchParams.get("risk");
  const risk: RiskFilter | undefined = isRiskFilter(riskParam) ? riskParam : undefined;
  const defaultWorkScope = defaultRequirementWorkScope(user);
  const workScope: WorkScope = isWorkScope(scopeParam)
    ? scopeParam
    : favoriteParam === "1"
      ? "followed"
      : defaultWorkScope;
  const requirementQueryParams = useMemo(() => {
    const params: Record<string, string> = {
      scope: workScope
    };
    if (keyword) params.keyword = keyword;
    if (priority) params.priority = priority;
    if (status) params.status = status;
    if (risk) params.risk = risk;
    return params;
  }, [keyword, priority, risk, status, workScope]);
  const [boardColumnExtras, setBoardColumnExtras] = useState<Record<string, MockRequirement[]>>({});
  const [boardColumnPages, setBoardColumnPages] = useState<Record<string, number>>({});
  const [loadingBoardColumns, setLoadingBoardColumns] = useState<Record<string, boolean>>({});
  const [listPage, setListPage] = useState(1);

  useEffect(() => {
    setBoardColumnExtras({});
    setBoardColumnPages({});
    setLoadingBoardColumns({});
    setListPage(1);
    setExpanded(new Set());
  }, [requirementQueryParams]);

  const updateParam = (key: string, value?: string) => {
    setSearchParams(
      (previous) => {
        const next = new URLSearchParams(previous);
        if (value) next.set(key, value);
        else next.delete(key);
        return next;
      },
      { replace: true }
    );
  };

  const updateWorkScope = (nextScope: WorkScope) => {
    setSearchParams(
      (previous) => {
        const next = new URLSearchParams(previous);
        next.delete("favorite");
        if (nextScope === defaultWorkScope) {
          next.delete("scope");
        } else {
          next.set("scope", nextScope);
        }
        return next;
      },
      { replace: true }
    );
  };

  useEffect(() => {
    if (defaultWorkScope === "all" || searchParams.has("scope") || searchParams.has("favorite"))
      return;
    setSearchParams(
      (previous) => {
        const next = new URLSearchParams(previous);
        next.set("scope", defaultWorkScope);
        return next;
      },
      { replace: true }
    );
  }, [defaultWorkScope, searchParams, setSearchParams]);

  const boardRequirementsQuery = useQuery({
    queryKey: ["requirements-board", "requirements", "board", requirementQueryParams],
    queryFn: () =>
      requirementsBoardApi.listRequirementBoard({
        ...requirementQueryParams,
        column_page_size: String(REQUIREMENT_BOARD_COLUMN_PAGE_SIZE)
      }),
    enabled: view === "board",
    staleTime: 60_000
  });
  const listRequirementsQuery = useQuery({
    queryKey: ["requirements-board", "requirements", "list", requirementQueryParams, listPage],
    queryFn: () =>
      requirementsBoardApi.listRequirementsPage({
        ...requirementQueryParams,
        page: String(listPage),
        page_size: String(REQUIREMENT_LIST_PAGE_SIZE)
      }),
    enabled: view === "tree",
    staleTime: 60_000
  });
  const tasksQuery = useQuery({
    queryKey: ["requirements-board", "tasks"],
    queryFn: () => requirementsBoardApi.listTasks(),
    staleTime: 30_000
  });
  useEffect(() => {
    setBoardColumnExtras({});
    setBoardColumnPages({});
    setLoadingBoardColumns({});
  }, [boardRequirementsQuery.dataUpdatedAt]);
  const tokenSourcesQuery = useQuery({
    queryKey: ["requirements-board", "token-sources"],
    queryFn: () => requirementsBoardApi.listTokenSources(),
    staleTime: 60_000
  });
  const favoritesQuery = useQuery({
    queryKey: ["requirements-board", "favorites"],
    queryFn: () => requirementsBoardApi.listFavorites(),
    staleTime: 60_000
  });
  const tokenSources = tokenSourcesQuery.data ?? EMPTY_TOKEN_SOURCES;
  const tokenSourceMap = useMemo(
    () => new Map(tokenSources.map((source) => [source.id, source])),
    [tokenSources]
  );

  const favorites = favoritesQuery.data ?? EMPTY_FAVORITES;
  const favoriteRequirementIds = useMemo(
    () =>
      new Set(
        favorites.filter((item) => item.target_type === "requirement").map((item) => item.target_id)
      ),
    [favorites]
  );
  const favoriteTaskIds = useMemo(
    () =>
      new Set(
        favorites.filter((item) => item.target_type === "task").map((item) => item.target_id)
      ),
    [favorites]
  );

  const favoriteMutation = useMutation({
    mutationFn: ({ targetType, targetId }: { targetType: FavoriteTargetType; targetId: string }) =>
      requirementsBoardApi.toggleFavorite(targetType, targetId),
    onSuccess: (result) => {
      message.success(result.favorited ? "已加入关注" : "已取消关注");
      void invalidateRequirementTaskWorkspace(queryClient);
    },
    onError: (error) => message.error(error instanceof Error ? error.message : "关注操作失败")
  });
  const toggleRequirementFavorite = (requirementId: string) =>
    favoriteMutation.mutate({ targetType: "requirement", targetId: requirementId });
  const toggleTaskFavorite = (taskId: string) =>
    favoriteMutation.mutate({ targetType: "task", targetId: taskId });

  const deleteTaskFromTreeMutation = useMutation({
    mutationFn: (task: MockTask) => requirementsBoardApi.deleteTask(task.id, task.version),
    onSuccess: (_result, task) => {
      message.success("任务已删除");
      void invalidateRequirementTaskWorkspace(queryClient, {
        requirementId: task.requirement_id,
        taskId: task.id
      });
      if (selectedTask?.id === task.id) {
        setSelectedTask(undefined);
        setTaskHistory([]);
      }
    },
    onError: (error) => {
      if (handleEditConflict(error, message, queryClient)) return;
      message.error(error instanceof Error ? error.message : "任务删除失败");
    }
  });
  const confirmDeleteTask = (task: MockTask) => {
    modal.confirm({
      title: "确认删除任务？",
      content: "删除后会解绑相关 Session/Token/文档，并重算需求进度，操作不可恢复。",
      okText: "删除任务",
      okButtonProps: { danger: true },
      cancelText: "取消",
      onOk: () => deleteTaskFromTreeMutation.mutateAsync(task)
    });
  };

  const canMoveRequirement = Boolean(
    user && ["admin", "director", "pm", "team_leader"].includes(user.role)
  );
  const canCreateRequirement = Boolean(
    user && ["admin", "director", "pm", "team_leader"].includes(user.role)
  );

  const statusMutation = useMutation({
    mutationFn: ({
      id,
      nextStatus,
      baseVersion
    }: {
      id: string;
      nextStatus: RequirementStage;
      baseVersion: number;
      promptOwnerAfterSave?: boolean;
    }) => requirementsBoardApi.updateRequirementStage(id, nextStatus, baseVersion),
    onError: (error) => {
      if (handleEditConflict(error, message, queryClient)) return;
      message.error(error instanceof Error ? error.message : "需求阶段更新失败");
    },
    onSuccess: (updated, variables) => {
      const shouldPromptOwner = Boolean(
        variables.promptOwnerAfterSave && updated.responsible_user_ids.length === 0
      );
      if (!shouldPromptOwner) {
        message.success("需求阶段已更新");
      }
      setBoardColumnExtras({});
      setBoardColumnPages({});
      setLoadingBoardColumns({});
      void queryClient.invalidateQueries({ queryKey: ["requirements-board", "requirements"] });
      void queryClient.invalidateQueries({
        queryKey: ["requirements-board", "requirement-events", updated.id]
      });
      if (selectedRequirement?.id === updated.id) {
        setSelectedRequirement(updated);
      }
      if (shouldPromptOwner) {
        setOwnerPromptRequirement(updated);
      }
    }
  });

  const boardColumnData = useMemo(
    () => boardRequirementsQuery.data?.columns ?? [],
    [boardRequirementsQuery.data?.columns]
  );
  const boardRequirements = useMemo(
    () =>
      boardColumnData.flatMap((column) => [
        ...column.items,
        ...(boardColumnExtras[column.status] ?? [])
      ]),
    [boardColumnData, boardColumnExtras]
  );
  const listRequirements = listRequirementsQuery.data?.items ?? EMPTY_REQUIREMENTS;
  const requirements = view === "board" ? boardRequirements : listRequirements;
  const requirementsTotal =
    view === "board"
      ? (boardRequirementsQuery.data?.total ?? requirements.length)
      : (listRequirementsQuery.data?.total ?? requirements.length);
  const requirementsQueryLoading =
    view === "board" ? boardRequirementsQuery.isLoading : listRequirementsQuery.isLoading;
  const requirementsQueryFetching =
    view === "board" ? boardRequirementsQuery.isFetching : listRequirementsQuery.isFetching;
  const requirementsQueryError =
    view === "board" ? boardRequirementsQuery.isError : listRequirementsQuery.isError;
  const tasks = tasksQuery.data ?? EMPTY_TASKS;
  const tasksByRequirement = useMemo(() => {
    const result = new Map<string, MockTask[]>();
    tasks.forEach((task) => {
      const list = result.get(task.requirement_id) ?? [];
      list.push(task);
      result.set(task.requirement_id, list);
    });
    return result;
  }, [tasks]);
  const boardColumnTotalMap = useMemo(
    () => new Map(boardColumnData.map((column) => [column.status, column.total])),
    [boardColumnData]
  );
  const getStatusMetricValue = useCallback((stage: RequirementStage) => {
    if (view === "board") {
      return (
        boardColumnTotalMap.get(stage) ??
        requirements.filter((item) => item.status === stage).length
      );
    }
    return requirements.filter((item) => item.status === stage).length;
  }, [boardColumnTotalMap, requirements, view]);

  const selectedRequirementFromLatest = selectedRequirement
    ? (requirements.find((item) => item.id === selectedRequirement.id) ?? selectedRequirement)
    : undefined;
  const selectedTaskFromLatest = selectedTask
    ? (tasks.find((item) => item.id === selectedTask.id) ?? selectedTask)
    : undefined;
  const activeRequirement = selectedRequirementFromLatest;
  const activeTask = selectedTaskFromLatest;

  useEffect(() => {
    if (!searchParams.has("requirementId") && !searchParams.has("taskId")) return;
    setSearchParams(
      (previous) => {
        const next = new URLSearchParams(previous);
        next.delete("requirementId");
        next.delete("taskId");
        return next;
      },
      { replace: true }
    );
  }, [searchParams, setSearchParams]);

  const openRequirementDetail = (requirement: MockRequirement) => {
    setSelectedRequirement(requirement);
    setCreatorOpen(false);
  };
  const openTaskDetail = (task: MockTask) => {
    setTaskHistory([]);
    setSelectedTask(task);
  };
  const openRelatedTaskDetail = (task: MockTask) => {
    if (activeTask?.id === task.id) return;
    if (activeTask) {
      setTaskHistory((current) => [...current, activeTask].slice(-12));
    }
    setSelectedTask(task);
  };
  const returnPreviousTask = () => {
    const previousTask = taskHistory[taskHistory.length - 1];
    if (!previousTask) return;
    const latestTask = tasks.find((item) => item.id === previousTask.id) ?? previousTask;
    setTaskHistory((current) => current.slice(0, -1));
    setSelectedTask(latestTask);
  };

  const filteredRequirements = useMemo(() => {
    const normalizedKeyword = keyword.trim().toLowerCase();
    const currentUserId = user?.id ?? "";
    return requirements.filter((requirement) => {
      const requirementTasks = tasksByRequirement.get(requirement.id) ?? [];
      const requirementFollowed = favoriteRequirementIds.has(requirement.id);
      const requirementAssigned =
        Boolean(currentUserId) &&
        (requirement.responsible_user_ids.includes(currentUserId) ||
          requirement.responsible_users.some((responsible) => responsible.id === currentUserId));
      const requirementCreated = Boolean(currentUserId) && requirement.creator_id === currentUserId;
      const taskFollowed = requirementTasks.some((task) => favoriteTaskIds.has(task.id));
      const taskAssigned =
        Boolean(currentUserId) &&
        requirementTasks.some(
          (task) =>
            task.responsible_user_ids.includes(currentUserId) ||
            task.responsible_users.some((responsible) => responsible.id === currentUserId)
        );
      const taskCreated =
        Boolean(currentUserId) &&
        requirementTasks.some((task) => task.creator_id === currentUserId);
      const followedMatched = requirementFollowed || taskFollowed;
      const assignedMatched = requirementAssigned || taskAssigned;
      const createdMatched = requirementCreated || taskCreated;
      const scopeMatched =
        workScope === "all" ||
        (workScope === "mine" && (followedMatched || assignedMatched || createdMatched)) ||
        (workScope === "followed" && followedMatched) ||
        (workScope === "assigned" && assignedMatched) ||
        (workScope === "created" && createdMatched);
      const searchContent = [
        requirement.title,
        requirement.description,
        requirement.creator_name,
        ...requirement.responsible_users.map((responsible) => responsible.name || responsible.id),
        ...requirement.team_names,
        ...requirement.acceptance_criteria,
        ...requirementTasks.flatMap((task) => [task.title, getTaskResponsibleTitle(task)])
      ]
        .join(" ")
        .toLowerCase();
      const riskMatched =
        !risk ||
        getRequirementRiskBadges(requirement, requirementTasks).some((item) => item.value === risk);
      const statusMatched = status
        ? requirement.status === status
        : requirement.status !== "cancelled";
      return (
        (!normalizedKeyword || searchContent.includes(normalizedKeyword)) &&
        (!priority || requirement.priority === priority) &&
        statusMatched &&
        riskMatched &&
        scopeMatched
      );
    });
  }, [
    favoriteRequirementIds,
    favoriteTaskIds,
    keyword,
    priority,
    requirements,
    risk,
    status,
    tasksByRequirement,
    user?.id,
    workScope
  ]);

  const metrics = useMemo<
    Array<{
      key: string;
      title: string;
      value: number;
      description: string;
      tone: RequirementMetricTone;
      icon: ReactNode;
    }>
  >(
    () => [
      {
        key: "total",
        title: "有效需求",
        value: requirementsTotal,
        description: "当前筛选范围",
        tone: "primary",
        icon: <UnorderedListOutlined />
      },
      {
        key: "review",
        title: "待确认",
        value: getStatusMetricValue("review"),
        description: view === "board" ? "当前筛选范围" : "当前页需求中",
        tone: "warning",
        icon: <FileTextOutlined />
      },
      {
        key: "active",
        title: "推进中",
        value: getStatusMetricValue("active"),
        description: view === "board" ? "当前筛选范围" : "当前页需求中",
        tone: "info",
        icon: <ClockCircleOutlined />
      },
      {
        key: "risk",
        title: "风险需求",
        value: requirements.filter(
          (item) => getRequirementRiskBadges(item, tasksByRequirement.get(item.id) ?? []).length > 0
        ).length,
        description: "已加载需求中",
        tone: "danger",
        icon: <WarningOutlined />
      }
    ],
    [requirements, requirementsTotal, tasksByRequirement, view, getStatusMetricValue]
  );

  const visibleColumns = useMemo(() => {
    if (!status) return STATUS_COLUMNS;
    if (status === "cancelled") return [CANCELLED_COLUMN];
    const matchedColumn = STATUS_COLUMNS.find((column) => column.value === status);
    return matchedColumn ? [matchedColumn] : STATUS_COLUMNS;
  }, [status]);
  const shouldPromptOwnerAfterStatus = (
    requirement: MockRequirement,
    nextStatus: RequirementStage
  ) =>
    requirement.responsible_user_ids.length === 0 &&
    ["review", "active", "completed"].includes(nextStatus);

  const mutateRequirementStatus = (requirement: MockRequirement, nextStatus: RequirementStage) => {
    statusMutation.mutate({
      id: requirement.id,
      nextStatus,
      baseVersion: requirement.version,
      promptOwnerAfterSave: shouldPromptOwnerAfterStatus(requirement, nextStatus)
    });
  };

  const handleDrop = (result: DropResult) => {
    if (!result.destination || !canMoveRequirement) return;
    const nextStatus = result.destination.droppableId as RequirementStage;
    const requirement = requirements.find((item) => item.id === result.draggableId);
    if (!requirement || requirement.status === nextStatus || nextStatus === "cancelled") return;
    updateRequirementStatus(requirement, nextStatus);
  };
  const updateRequirementStatus = (requirement: MockRequirement, nextStatus: RequirementStage) => {
    if (!canMoveRequirement || requirement.status === nextStatus || nextStatus === "cancelled")
      return;
    mutateRequirementStatus(requirement, nextStatus);
  };

  const toggleRequirement = (id: string) => {
    setExpanded((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const refreshAll = () => {
    setBoardColumnExtras({});
    setBoardColumnPages({});
    setLoadingBoardColumns({});
    void Promise.all([
      view === "board" ? boardRequirementsQuery.refetch() : listRequirementsQuery.refetch(),
      tasksQuery.refetch(),
      tokenSourcesQuery.refetch()
    ]);
  };
  const loadMoreBoardColumn = async (columnStatus: RequirementStage) => {
    const column = boardColumnData.find((item) => item.status === columnStatus);
    if (!column || loadingBoardColumns[columnStatus]) return;
    const currentPage = boardColumnPages[columnStatus] ?? column.page;
    const nextPage = currentPage + 1;
    setLoadingBoardColumns((current) => ({ ...current, [columnStatus]: true }));
    try {
      const payload = await requirementsBoardApi.listRequirementsPage({
        ...requirementQueryParams,
        status: columnStatus,
        page: String(nextPage),
        page_size: String(column.page_size || REQUIREMENT_BOARD_COLUMN_PAGE_SIZE)
      });
      setBoardColumnExtras((current) => ({
        ...current,
        [columnStatus]: [...(current[columnStatus] ?? []), ...payload.items]
      }));
      setBoardColumnPages((current) => ({ ...current, [columnStatus]: nextPage }));
    } catch (error) {
      message.error(error instanceof Error ? error.message : "加载更多需求失败");
    } finally {
      setLoadingBoardColumns((current) => ({ ...current, [columnStatus]: false }));
    }
  };
  const allExpanded =
    filteredRequirements.length > 0 && filteredRequirements.every((item) => expanded.has(item.id));
  const listPagination: TableProps<MockRequirement>["pagination"] =
    view === "tree"
      ? {
          current: listPage,
          pageSize: REQUIREMENT_LIST_PAGE_SIZE,
          total: requirementsTotal,
          showSizeChanger: false,
          showTotal: (total) => `共 ${total} 条`,
          onChange: (page) => {
            setListPage(page);
            setExpanded(new Set());
          }
        }
      : false;
  const hasActiveFilters = Boolean(
    keyword || searchDraft || priority || status || risk || workScope !== defaultWorkScope
  );
  const resetFilters = () => {
    setSearchDraft("");
    const next: Record<string, string> = {};
    if (view !== "board") next.view = view;
    if (defaultWorkScope !== "all") next.scope = defaultWorkScope;
    setSearchParams(next, { replace: true });
  };

  return (
    <PagePanel
      title="需求推进"
      className="requirements-board-page"
      description="跟踪需求阶段、任务拆解与风险推进"
      breadcrumbs={[{ title: "业务" }, { title: "需求推进" }]}
      showNav={false}
    >
      <RequirementMetricGrid>
        {metrics.map((metric) => (
          <RequirementMetricCard
            key={metric.key}
            metric={metric}
            icon={metric.icon}
            tone={metric.tone}
            loading={requirementsQueryLoading || tasksQuery.isLoading}
          />
        ))}
      </RequirementMetricGrid>

      <section className="requirements-board__workspace">
        <div className="requirements-board__workspace-head">
          <div className="requirements-board__toolbar">
            <Segmented
              className="requirements-board__scope-filter"
              value={workScope}
              onChange={(next) => updateWorkScope(next as WorkScope)}
              options={WORK_SCOPE_OPTIONS}
            />
            <div className="requirements-board__toolbar-actions">
              <Segmented
                className="requirements-board__view-switch"
                value={view}
                onChange={(next) => updateParam("view", String(next))}
                options={[
                  { value: "board", label: "阶段看板", icon: <AppstoreOutlined /> },
                  { value: "tree", label: "需求列表", icon: <UnorderedListOutlined /> }
                ]}
              />
              {canCreateRequirement ? (
                <Button
                  className="requirements-board__primary-action"
                  type="primary"
                  icon={<PlusOutlined />}
                  onClick={() => navigate(appendSearch("/requirements/create", searchParams))}
                >
                  新建需求
                </Button>
              ) : null}
            </div>
          </div>

          <div className="requirements-board__filter-row">
            <Input.Search
              className="requirements-board__search"
              allowClear
              value={searchDraft}
              placeholder="搜索需求、任务或负责人"
              onChange={(event) => setSearchDraft(event.target.value)}
              onSearch={(value) => updateParam("keyword", value.trim() || undefined)}
            />
            <div className="requirements-board__filter-controls">
              <Select
                allowClear
                placeholder="全部阶段"
                value={status}
                onChange={(next) => updateParam("status", next)}
                options={STATUS_OPTIONS}
              />
              <Select
                allowClear
                placeholder="全部优先级"
                value={priority}
                onChange={(next) => updateParam("priority", next)}
                options={PRIORITY_OPTIONS}
              />
              <Select
                allowClear
                placeholder="风险类型"
                value={risk}
                onChange={(next) => updateParam("risk", next)}
                options={RISK_OPTIONS}
              />
              <Button
                className="requirements-board__reset-action"
                disabled={!hasActiveFilters}
                onClick={resetFilters}
              >
                <span className="requirements-board__reset-label">
                  <span>重</span>
                  <span>置</span>
                </span>
              </Button>
              {view === "tree" ? (
                <Button
                  className="requirements-board__utility-action"
                  type="text"
                  onClick={() =>
                    setExpanded(
                      allExpanded ? new Set() : new Set(filteredRequirements.map((item) => item.id))
                    )
                  }
                >
                  {allExpanded ? "收起全部" : "展开全部"}
                </Button>
              ) : null}
              <span className="requirements-board__filter-count">共 {requirementsTotal} 条</span>
              <Button
                className="requirements-board__refresh-action"
                type="text"
                aria-label="刷新"
                icon={<ReloadOutlined />}
                loading={requirementsQueryFetching || tasksQuery.isFetching}
                onClick={refreshAll}
              />
            </div>
          </div>
        </div>

        {requirementsQueryError || tasksQuery.isError ? (
          <Alert
            className="requirements-board__alert"
            type="error"
            showIcon
            message="需求数据加载失败"
            description="需求或任务数据未能正确加载，请重试。"
            action={<Button onClick={refreshAll}>重试</Button>}
          />
        ) : null}

        <div className="requirements-board__content">
          {requirementsQueryLoading ? (
            <div className="requirements-board__loading">
              <Skeleton active paragraph={{ rows: 8 }} />
            </div>
          ) : filteredRequirements.length === 0 ? (
            <Empty description="暂无符合条件的需求" />
          ) : view === "board" ? (
            <DragDropContext onDragEnd={handleDrop}>
              <div
                className={`requirements-board__columns${
                  visibleColumns.length === 1 ? " is-filter-result" : ""
                }`}
              >
                {visibleColumns.map((column) => {
                  const allColumnRequirements = filteredRequirements.filter(
                    (item) => item.status === column.value
                  );
                  const columnState = boardColumnData.find((item) => item.status === column.value);
                  const columnRequirements = allColumnRequirements;
                  const headerCountBadge = columnState?.total ?? columnRequirements.length;
                  const hasMoreColumnItems = Boolean(
                    columnState && columnRequirements.length < columnState.total
                  );
                  return (
                    <Droppable droppableId={column.value} key={column.value}>
                      {(provided, snapshot) => (
                        <section
                          className={`requirements-board__column is-${column.tone}${
                            snapshot.isDraggingOver ? " is-dragging-over" : ""
                          }`}
                          ref={provided.innerRef}
                          {...provided.droppableProps}
                        >
                          <header title={column.description}>
                            <div className="requirements-board__column-head-left">
                              <span className="requirements-board__status-dot" />
                              <strong>{column.label}</strong>
                              <Tag variant="filled">{headerCountBadge}</Tag>
                            </div>
                            {column.value === "completed" ? (
                              <div className="requirements-board__column-head-actions">
                                <span className="requirements-board__column-head-meta">
                                  已显示 {columnRequirements.length} 个
                                </span>
                              </div>
                            ) : null}
                          </header>
                          <div className="requirements-board__card-list">
                            {columnRequirements.map((requirement, index) => (
                              <RequirementCard
                                key={requirement.id}
                                requirement={requirement}
                                tasks={tasksByRequirement.get(requirement.id) ?? []}
                                index={index}
                                draggable={canMoveRequirement && column.value !== "cancelled"}
                                isCompletedColumn={column.value === "completed"}
                                isFavorite={favoriteRequirementIds.has(requirement.id)}
                                onToggleFavorite={() => toggleRequirementFavorite(requirement.id)}
                                onOpen={() => openRequirementDetail(requirement)}
                              />
                            ))}
                            {provided.placeholder}
                            {!columnRequirements.length ? (
                              <div className="requirements-board__column-empty">
                                {column.emptyText}
                              </div>
                            ) : null}
                            {hasMoreColumnItems ? (
                              <Button
                                className="requirements-board__column-load-more"
                                type="text"
                                loading={Boolean(loadingBoardColumns[column.value])}
                                onClick={() => void loadMoreBoardColumn(column.value)}
                              >
                                已显示 {columnRequirements.length}/{headerCountBadge}，加载更多
                              </Button>
                            ) : null}
                          </div>
                        </section>
                      )}
                    </Droppable>
                  );
                })}
              </div>
            </DragDropContext>
          ) : (
            <RequirementTree
              requirements={filteredRequirements}
              pagination={listPagination}
              expanded={expanded}
              tasksByRequirement={tasksByRequirement}
              favoriteRequirementIds={favoriteRequirementIds}
              favoriteTaskIds={favoriteTaskIds}
              onToggle={toggleRequirement}
              onOpenRequirement={openRequirementDetail}
              onOpenTask={openTaskDetail}
              onDeleteTask={confirmDeleteTask}
              onToggleRequirementFavorite={toggleRequirementFavorite}
              onToggleTaskFavorite={toggleTaskFavorite}
            />
          )}
        </div>
      </section>

      <RequirementDrawer
        requirement={activeRequirement}
        tasks={activeRequirement ? (tasksByRequirement.get(activeRequirement.id) ?? []) : []}
        dependencyTasks={tasks}
        tokenSourceMap={tokenSourceMap}
        creatorOpen={creatorOpen}
        isFavorite={activeRequirement ? favoriteRequirementIds.has(activeRequirement.id) : false}
        canManage={Boolean(
          activeRequirement?.can_update ||
            activeRequirement?.can_cancel ||
            activeRequirement?.can_restore ||
            activeRequirement?.can_delete
        )}
        canUpdateStatus={canMoveRequirement}
        onUpdateStatus={updateRequirementStatus}
        onToggleFavorite={
          activeRequirement ? () => toggleRequirementFavorite(activeRequirement.id) : undefined
        }
        onCreatorOpenChange={setCreatorOpen}
        onClose={() => {
          setSelectedRequirement(undefined);
          setCreatorOpen(false);
        }}
        onSaved={(updated) => setSelectedRequirement(updated)}
        onOpenTask={openTaskDetail}
      />
      <TaskDetailModal
        task={activeTask}
        dependencyTasks={tasks}
        tokenSourceMap={tokenSourceMap}
        isFavorite={activeTask ? favoriteTaskIds.has(activeTask.id) : false}
        canManage={canManageTaskForUser(user, activeTask)}
        onToggleFavorite={activeTask ? () => toggleTaskFavorite(activeTask.id) : undefined}
        canGoBack={taskHistory.length > 0}
        onBackTask={returnPreviousTask}
        onOpenTask={openRelatedTaskDetail}
        onClose={() => {
          setSelectedTask(undefined);
          setTaskHistory([]);
        }}
        onSaved={(updated) => setSelectedTask(updated)}
        onDeleted={() => {
          setSelectedTask(undefined);
          setTaskHistory([]);
        }}
      />
      <Modal
        className="requirements-owner-prompt-modal"
        open={Boolean(ownerPromptRequirement)}
        title={null}
        closable={false}
        width={440}
        footer={null}
        onCancel={() => setOwnerPromptRequirement(undefined)}
        destroyOnHidden
      >
        <div className="requirements-owner-prompt-modal__panel">
          <div className="requirements-owner-prompt-modal__topline">
            <span>阶段已更新</span>
          </div>
          <div className="requirements-owner-prompt-modal__content">
            <div className="requirements-owner-prompt-modal__icon">
              <UserOutlined />
            </div>
            <div>
              <strong>建议补充负责人</strong>
              <p>指定负责人后，需求会进入对应成员的关注范围，后续拆分任务也更清晰。</p>
            </div>
          </div>
          <div className="requirements-owner-prompt-modal__footer">
            <Button onClick={() => setOwnerPromptRequirement(undefined)}>稍后处理</Button>
            <Button
              type="primary"
              onClick={() => {
                const target = ownerPromptRequirement;
                setOwnerPromptRequirement(undefined);
                if (target) {
                  window.setTimeout(() => setStatusEditRequirement(target), 120);
                }
              }}
            >
              指定负责人
            </Button>
          </div>
        </div>
      </Modal>
      {statusEditRequirement ? (
        <RequirementEditModal
          open={Boolean(statusEditRequirement)}
          requirement={statusEditRequirement}
          onCancel={() => setStatusEditRequirement(undefined)}
          onSaved={(updated) => {
            setStatusEditRequirement(undefined);
            if (selectedRequirement?.id === updated.id) {
              setSelectedRequirement(updated);
            }
          }}
        />
      ) : null}
    </PagePanel>
  );
}

function RequirementCard({
  requirement,
  tasks,
  index,
  draggable,
  isCompletedColumn,
  isFavorite,
  onToggleFavorite,
  onOpen
}: {
  requirement: MockRequirement;
  tasks: MockTask[];
  index: number;
  draggable: boolean;
  isCompletedColumn: boolean;
  isFavorite: boolean;
  onToggleFavorite: () => void;
  onOpen: () => void;
}) {
  const riskBadges = getRequirementRiskBadges(requirement, tasks);
  const primaryRisk = riskBadges[0];
  const blockedRisk = riskBadges.find((item) => item.value === "blocked");
  const taskTotal = requirement.task_summary.total;
  const completedTasks = requirement.task_summary.done;
  const ownerLine = getRequirementOwnerLabel(requirement);
  const ownerTitle = getRequirementOwnerTitle(requirement);
  const teamLine = getRequirementTeamCompactLabel(requirement);
  const teamTitle = getRequirementTeamTitle(requirement);
  const taskProgressLabel = taskTotal ? `${completedTasks}/${taskTotal} 完成` : "待拆解";
  const dateLabel = isCompletedColumn
    ? `完成 ${formatDate(requirement.updated_at)}`
    : `截止 ${formatDate(requirement.deadline)}`;
  const riskTitle = riskBadges.map((item) => item.label).join("；");
  const showRiskRow = !isCompletedColumn && Boolean(primaryRisk);

  return (
    <Draggable draggableId={requirement.id} index={index} isDragDisabled={!draggable}>
      {(provided, snapshot) => (
        <article
          className={`requirements-board__card is-status-${requirement.status}${snapshot.isDragging ? " is-dragging" : ""}${
            primaryRisk ? ` has-risk has-${primaryRisk.tone}` : ""
          }${blockedRisk ? " has-blocked" : ""}`}
          ref={provided.innerRef}
          {...provided.draggableProps}
          {...provided.dragHandleProps}
          onClick={onOpen}
        >
          <div className="requirements-board__card-top">
            <div className="requirements-board__card-title">
              <h3 title={requirement.title}>{requirement.title}</h3>
            </div>
            <div className="requirements-board__card-actions">
              <RequirementPriorityTag priority={requirement.priority} />
              <button
                type="button"
                className={`requirements-board__favorite${isFavorite ? " is-active" : ""}`}
                aria-label={isFavorite ? "取消关注" : "关注需求"}
                onClick={(event) => {
                  event.stopPropagation();
                  onToggleFavorite();
                }}
              >
                {isFavorite ? <StarFilled /> : <StarOutlined />}
              </button>
            </div>
          </div>

          {showRiskRow ? (
            <div
              className={`requirements-board__card-risk-line is-${primaryRisk?.tone ?? "warning"}`}
              title={riskTitle}
            >
              <span className="requirements-board__card-risk-chip" title={primaryRisk?.label}>
                {primaryRisk?.label}
              </span>
              {riskBadges.length > 1 ? (
                <span className="requirements-board__card-risk-more">
                  {formatRiskOverflow(riskBadges.length - 1)}
                </span>
              ) : null}
            </div>
          ) : null}

          <div className="requirements-board__card-status-row">
            <strong title={taskProgressLabel}>{taskProgressLabel}</strong>
            {blockedRisk ? <span title={blockedRisk.label}>阻塞 {blockedRisk.count}</span> : null}
            <em title={dateLabel}>{dateLabel}</em>
          </div>

          {tasks.length ? (
            <div className="requirements-board__card-progress-line" aria-hidden="true">
              <span style={{ width: `${requirement.progress}%` }} />
            </div>
          ) : null}

          <footer className="requirements-board__card-meta">
            <span title={`负责人：${ownerTitle}`}>
              <UserOutlined /> {ownerLine}
            </span>
            <span title={`参与团队：${teamTitle}`}>团队 {teamLine}</span>
          </footer>
        </article>
      )}
    </Draggable>
  );
}

function RequirementTree({
  requirements,
  pagination,
  expanded,
  tasksByRequirement,
  favoriteRequirementIds,
  favoriteTaskIds,
  onToggle,
  onOpenRequirement,
  onOpenTask,
  onDeleteTask,
  onToggleRequirementFavorite,
  onToggleTaskFavorite
}: {
  requirements: MockRequirement[];
  pagination: TableProps<MockRequirement>["pagination"];
  expanded: Set<string>;
  tasksByRequirement: Map<string, MockTask[]>;
  favoriteRequirementIds: Set<string>;
  favoriteTaskIds: Set<string>;
  onToggle: (id: string) => void;
  onOpenRequirement: (requirement: MockRequirement) => void;
  onOpenTask: (task: MockTask) => void;
  onDeleteTask: (task: MockTask) => void;
  onToggleRequirementFavorite: (requirementId: string) => void;
  onToggleTaskFavorite: (taskId: string) => void;
}) {
  const columns: TableProps<MockRequirement>["columns"] = [
    {
      title: "需求",
      key: "title",
      width: 320,
      ellipsis: true,
      render: (_, requirement) => (
        <div className="requirements-tree__title-cell">
          <button
            type="button"
            className={`requirements-tree__favorite${
              favoriteRequirementIds.has(requirement.id) ? " is-active" : ""
            }`}
            aria-label={favoriteRequirementIds.has(requirement.id) ? "取消关注" : "关注需求"}
            onClick={(event) => {
              event.stopPropagation();
              onToggleRequirementFavorite(requirement.id);
            }}
          >
            {favoriteRequirementIds.has(requirement.id) ? <StarFilled /> : <StarOutlined />}
          </button>
          <strong className="requirements-tree__title" title={requirement.title}>
            {requirement.title}
          </strong>
        </div>
      )
    },
    {
      title: "负责人",
      key: "owner",
      width: 120,
      ellipsis: true,
      render: (_, requirement) => (
        <span className="requirements-tree__text" title={getRequirementOwnerTitle(requirement)}>
          {getRequirementOwnerLabel(requirement)}
        </span>
      )
    },
    {
      title: "阶段",
      dataIndex: "status",
      key: "status",
      width: 92,
      render: (stage: RequirementStage) => <RequirementStageTag stage={stage} />
    },
    {
      title: "优先级",
      dataIndex: "priority",
      key: "priority",
      width: 80,
      render: (priority: RequirementPriority) => <PriorityPill priority={priority} />
    },
    {
      title: "任务进度",
      key: "progress",
      width: 112,
      render: (_, requirement) => {
        const requirementTasks = tasksByRequirement.get(requirement.id) ?? [];
        const doneTasks = requirementTasks.filter((task) => task.status === "done").length;
        return (
          <div className="requirements-tree__progress-summary">
            <strong>
              {requirementTasks.length
                ? `${doneTasks}/${requirementTasks.length} 已完成`
                : "待拆解"}
            </strong>
          </div>
        );
      }
    },
    {
      title: "风险",
      key: "risk",
      width: 176,
      render: (_, requirement) => {
        const requirementTasks = tasksByRequirement.get(requirement.id) ?? [];
        const riskBadges = getRequirementRiskBadges(requirement, requirementTasks);
        const primaryRisk = riskBadges[0];
        if (!primaryRisk) return <span className="requirements-tree__risk">正常</span>;
        return (
          <span className={`requirements-tree__risk is-${primaryRisk.tone}`}>
            {primaryRisk.label}
            {riskBadges.length > 1 ? <em>{formatRiskOverflow(riskBadges.length - 1)}</em> : null}
          </span>
        );
      }
    },
    {
      title: <AttentionColumnTitle />,
      key: "attention",
      width: 96,
      render: (_, requirement) => <RequirementAttentionPill requirement={requirement} />
    },
    {
      title: "截止",
      key: "deadline",
      width: 96,
      render: (_, requirement) => (
        <span className="requirements-tree__text">{formatDate(requirement.deadline)}</span>
      )
    }
  ];

  return (
    <Table<MockRequirement>
      className="requirements-tree"
      columns={columns}
      dataSource={requirements}
      pagination={pagination}
      rowKey="id"
      size="middle"
      scroll={{ x: 960 }}
      onRow={(requirement) => ({
        onClick: () => onOpenRequirement(requirement)
      })}
      rowClassName={(requirement) => {
        const hasRisk =
          getRequirementRiskBadges(requirement, tasksByRequirement.get(requirement.id) ?? [])
            .length > 0;
        return hasRisk ? "requirements-tree__table-row has-risk" : "requirements-tree__table-row";
      }}
      expandable={{
        columnWidth: 42,
        expandedRowKeys: Array.from(expanded),
        rowExpandable: (requirement) => Boolean(tasksByRequirement.get(requirement.id)?.length),
        onExpand: (_isExpanded, requirement) => onToggle(requirement.id),
        expandedRowRender: (requirement) => {
          const requirementTasks = tasksByRequirement.get(requirement.id) ?? [];
          return (
            <div className="requirements-tree__task-panel">
              <div className="requirements-tree__task-header" aria-hidden="true">
                <span>任务</span>
                <span>负责人</span>
                <span>状态</span>
                <span>进度</span>
                <span>截止</span>
                <span>关注度</span>
                <span>操作</span>
              </div>
              {[...requirementTasks]
                .sort((a, b) => Number(isTaskBlocked(b)) - Number(isTaskBlocked(a)))
                .map((task) => {
                  const taskRiskBadges = getTaskRiskBadges(task);
                  return (
                    <div
                      className={`requirements-tree__task-item${
                        taskRiskBadges.length ? " has-risk" : ""
                      }`}
                      key={task.id}
                      onClick={() => onOpenTask(task)}
                    >
                      <div className="requirements-tree__task-main">
                        <strong title={task.title}>{task.title}</strong>
                        {taskRiskBadges.length ? (
                          <div className="requirements-tree__task-risks">
                            {taskRiskBadges.map((risk) => (
                              <span
                                className={`requirements-tree__task-risk is-${risk.tone}`}
                                key={risk.key}
                              >
                                {risk.label}
                              </span>
                            ))}
                          </div>
                        ) : null}
                      </div>
                      <span
                        className="requirements-tree__task-owner"
                        title={getTaskResponsibleTitle(task)}
                      >
                        {getTaskResponsibleLabel(task)}
                      </span>
                      <div className="requirements-tree__task-status">
                        <TaskStatusPill status={task.status} />
                        <PriorityPill priority={task.priority} />
                      </div>
                      <div className="requirements-tree__task-progress">
                        <RequirementProgress value={task.progress} />
                      </div>
                      <span className="requirements-tree__task-date">
                        {formatDate(task.due_date)}
                      </span>
                      <div className="requirements-tree__task-attention">
                        <TaskAttentionPill task={task} />
                      </div>
                      <div className="requirements-tree__task-actions">
                        <button
                          type="button"
                          className={`requirements-tree__favorite requirements-tree__task-favorite${
                            favoriteTaskIds.has(task.id) ? " is-active" : ""
                          }`}
                          aria-label={favoriteTaskIds.has(task.id) ? "取消关注任务" : "关注任务"}
                          onClick={(event) => {
                            event.stopPropagation();
                            onToggleTaskFavorite(task.id);
                          }}
                        >
                          {favoriteTaskIds.has(task.id) ? <StarFilled /> : <StarOutlined />}
                        </button>
                        <Dropdown
                          trigger={["click"]}
                          menu={{
                            items: [
                              {
                                key: "open",
                                icon: <FileTextOutlined />,
                                label: "打开详情"
                              },
                              ...(task.can_delete
                                ? [
                                    {
                                      key: "delete",
                                      danger: true,
                                      icon: <DeleteOutlined />,
                                      label: "删除任务"
                                    }
                                  ]
                                : [])
                            ],
                            onClick: ({ key, domEvent }) => {
                              domEvent.stopPropagation();
                              if (key === "delete") {
                                onDeleteTask(task);
                                return;
                              }
                              onOpenTask(task);
                            }
                          }}
                        >
                          <Button
                            type="text"
                            size="small"
                            icon={<MoreOutlined />}
                            aria-label="任务操作"
                            onClick={(event) => event.stopPropagation()}
                          />
                        </Dropdown>
                      </div>
                    </div>
                  );
                })}
            </div>
          );
        }
      }}
    />
  );
}

interface TokenSourcePickerProps {
  open: boolean;
  selectedIds: string[];
  onCancel: () => void;
  onConfirm: (ids: string[]) => Promise<void> | void;
  confirmLoading?: boolean;
}

function TokenSourcePicker({
  open,
  selectedIds,
  onCancel,
  onConfirm,
  confirmLoading
}: TokenSourcePickerProps) {
  const [selected, setSelected] = useState<string[]>([]);
  const [selectedRows, setSelectedRows] = useState<Record<string, SessionTokens>>({});
  const [dateRange, setDateRange] = useState<[string, string]>();
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  const selectedIdsKey = useMemo(() => selectedIds.join("|"), [selectedIds]);
  const selectedIdSet = useMemo(() => new Set(selectedIds), [selectedIds]);
  const sessionsQuery = useQuery({
    queryKey: ["requirements-board", "linkable-session-tokens", page, pageSize, dateRange],
    queryFn: () =>
      fetchSessionTokens({
        scope: "mine",
        ...(dateRange ? { from: dateRange[0], to: dateRange[1] } : {}),
        page: String(page),
        page_size: String(pageSize)
      }),
    enabled: open,
    placeholderData: (previousData) => previousData,
    staleTime: 60_000
  });
  const selectAllMutation = useMutation({
    mutationFn: () =>
      fetchAllSessionTokens({
        scope: "mine",
        ...(dateRange ? { from: dateRange[0], to: dateRange[1] } : {})
      }),
    onSuccess: (items) => {
      const selectableItems = items.filter((item) => item.total_tokens > 0);
      setSelected((current) => [
        ...new Set([...current, ...selectableItems.map((item) => sessionRowKey(item))])
      ]);
      setSelectedRows((current) => {
        const next = { ...current };
        selectableItems.forEach((item) => {
          next[sessionRowKey(item)] = item;
        });
        return next;
      });
    }
  });
  const sessions = useMemo(() => sessionsQuery.data?.items ?? [], [sessionsQuery.data]);
  const rows = useMemo(() => sessions.filter((source) => source.total_tokens > 0), [sessions]);
  useEffect(() => {
    if (!open) return;
    const timer = window.setTimeout(() => {
      setSelected(selectedIds);
      setSelectedRows({});
      setPage(1);
    }, 0);
    return () => window.clearTimeout(timer);
  }, [open, selectedIds, selectedIdsKey]);
  useEffect(() => {
    if (!open || !rows.length) return;
    const timer = window.setTimeout(() => {
      setSelectedRows((prev) => {
        let changed = false;
        const next = { ...prev };
        rows.forEach((row) => {
          const key = sessionRowKey(row);
          if ((selected.includes(key) || selectedIdSet.has(key)) && next[key] !== row) {
            next[key] = row;
            changed = true;
          }
        });
        return changed ? next : prev;
      });
    }, 0);
    return () => window.clearTimeout(timer);
  }, [open, rows, selected, selectedIdSet]);
  const selectionChanged = useMemo(
    () => selected.length !== selectedIds.length || selected.some((id) => !selectedIdSet.has(id)),
    [selected, selectedIdSet, selectedIds.length]
  );
  const selectedTokenTotal = useMemo(
    () =>
      selected.reduce((total, id) => {
        const source = selectedRows[id];
        return total + (source?.total_tokens ?? 0);
      }, 0),
    [selected, selectedRows]
  );
  const selectedSliceKeys = useMemo(() => selected, [selected]);

  const columns: TableProps<SessionTokens>["columns"] = [
    {
      title: "Session",
      key: "record",
      width: 300,
      render: (_, source) => <span className="tokens-session-id">{realSessionId(source)}</span>
    },
    {
      title: "摘要",
      dataIndex: "summary",
      width: 390,
      render: (value: string | undefined) =>
        value ? (
          <span className="tokens-session-summary" title={value}>
            {value}
          </span>
        ) : (
          "-"
        )
    },
    {
      title: "Token",
      dataIndex: "total_tokens",
      width: 110,
      align: "right" as const,
      render: (value: number) => <span className="tokens-total-cell">{formatTokens(value)}</span>
    },
    {
      title: "日期范围",
      dataIndex: "activity_start_at",
      width: 230,
      render: (_, source) => (
        <span className="tokens-activity-range">{formatSessionActivityRange(source)}</span>
      )
    }
  ];

  return (
    <Modal
      className="requirements-session-modal"
      title={
        <div className="requirements-modal-title">
          <strong>关联工作记录</strong>
          <span>选择可作为该需求证据来源的 session</span>
        </div>
      }
      open={open}
      width={1120}
      onCancel={() => {
        setSelected([]);
        setSelectedRows({});
        setDateRange(undefined);
        setPage(1);
        setPageSize(10);
        onCancel();
      }}
      okText={selectedSliceKeys.length ? `保存 ${selectedSliceKeys.length} 条关联` : "清空关联"}
      okButtonProps={{ disabled: !selectionChanged, loading: confirmLoading }}
      cancelText="取消"
      onOk={async () => {
        await onConfirm(selectedSliceKeys);
        setSelected([]);
        setSelectedRows({});
      }}
      destroyOnHidden
    >
      <div className="requirements-session-modal__toolbar">
        <DatePicker.RangePicker
          allowClear
          value={dateRange ? [dayjs(dateRange[0]), dayjs(dateRange[1])] : null}
          format="YYYY-MM-DD"
          placeholder={["开始日期", "结束日期"]}
          onChange={(_, values) => {
            const nextRange =
              values[0] && values[1] ? ([values[0], values[1]] as [string, string]) : undefined;
            setDateRange(nextRange);
            setPage(1);
          }}
        />
        <Button
          loading={selectAllMutation.isPending}
          disabled={(sessionsQuery.data?.total ?? 0) === 0}
          onClick={() => selectAllMutation.mutate()}
        >
          全选查询结果
        </Button>
        <Button
          disabled={selected.length === 0}
          onClick={() => {
            setSelected([]);
            setSelectedRows({});
          }}
        >
          清空选择
        </Button>
      </div>
      <div className="requirements-session-modal__table tokens-table-card">
        <Table<SessionTokens>
          rowKey={sessionRowKey}
          size="small"
          tableLayout="fixed"
          columns={columns}
          dataSource={rows}
          loading={sessionsQuery.isLoading}
          pagination={{
            current: sessionsQuery.data?.page ?? page,
            pageSize: sessionsQuery.data?.page_size ?? pageSize,
            total: sessionsQuery.data?.total ?? 0,
            showSizeChanger: true,
            showTotal: (value) => `共 ${value} 条工作记录`,
            onChange: (nextPage, nextPageSize) => {
              setPage(nextPage);
              setPageSize(nextPageSize);
            }
          }}
          rowSelection={{
            preserveSelectedRowKeys: true,
            selectedRowKeys: selected,
            onChange: (keys, rows) => {
              const nextRows = { ...selectedRows };
              rows.forEach((row) => {
                nextRows[sessionRowKey(row)] = row;
              });
              setSelected(keys as string[]);
              setSelectedRows(nextRows);
            }
          }}
          locale={{ emptyText: "暂无可关联 session" }}
        />
      </div>
      <div className="requirements-session-modal__summary">
        <span>
          本页 {rows.length} 条{dateRange ? ` · ${dateRange[0]} 至 ${dateRange[1]}` : ""}
        </span>
        <strong>
          已选 {selectedSliceKeys.length} 条 · {formatTokens(selectedTokenTotal)} Token
        </strong>
      </div>
    </Modal>
  );
}

export function RequirementDrawer({
  requirement,
  tasks,
  dependencyTasks,
  tokenSourceMap,
  creatorOpen,
  isFavorite,
  canManage,
  canUpdateStatus,
  onToggleFavorite,
  onCreatorOpenChange,
  onClose,
  onSaved,
  onUpdateStatus,
  onOpenTask
}: {
  requirement?: MockRequirement;
  tasks: MockTask[];
  dependencyTasks: MockTask[];
  tokenSourceMap: Map<string, MockTokenSource>;
  creatorOpen: boolean;
  isFavorite: boolean;
  canManage: boolean;
  canUpdateStatus: boolean;
  onToggleFavorite?: () => void;
  onCreatorOpenChange: (open: boolean) => void;
  onClose: () => void;
  onSaved: (requirement: MockRequirement) => void;
  onUpdateStatus: (requirement: MockRequirement, nextStatus: RequirementStage) => void;
  onOpenTask: (task: MockTask) => void;
}) {
  const { message, modal } = App.useApp();
  const queryClient = useQueryClient();
  const [pickerOpen, setPickerOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [quickEditor, setQuickEditor] = useState<RequirementQuickEditField>();
  const [draftDeadline, setDraftDeadline] = useState<dayjs.Dayjs>();
  const [draftOwnerIds, setDraftOwnerIds] = useState<string[]>([]);
  const [draftTeamIds, setDraftTeamIds] = useState<string[]>([]);

  useEffect(() => {
    setEditOpen(false);
    setPickerOpen(false);
    setQuickEditor(undefined);
  }, [requirement?.id]);

  const invalidateBoard = (requirementId = requirement?.id) => {
    void invalidateRequirementTaskWorkspace(queryClient, { requirementId });
    if (requirementId) {
      void queryClient.invalidateQueries({
        queryKey: ["requirements-board", "requirement-events", requirementId]
      });
    }
  };

  const assigneesQuery = useQuery({
    queryKey: ["requirements-board", "assignees"],
    queryFn: () => requirementsBoardApi.listAssignees(),
    enabled: Boolean(requirement?.id),
    staleTime: 5 * 60_000
  });
  const teamsQuery = useQuery({
    queryKey: ["requirements-board", "teams"],
    queryFn: () => requirementsBoardApi.listTeams(),
    enabled: Boolean(requirement?.id),
    staleTime: 5 * 60_000
  });
  const requirementEventsQuery = useQuery({
    queryKey: ["requirements-board", "requirement-events", requirement?.id],
    queryFn: () => requirementsBoardApi.listRequirementEvents(requirement!.id),
    enabled: Boolean(requirement?.id),
    staleTime: 0,
    refetchOnMount: "always"
  });
  const eventUserNameMap = useMemo(
    () => buildWorkItemEventUserNameMap(assigneesQuery.data ?? [], requirement),
    [assigneesQuery.data, requirement]
  );
  const eventTeamNameMap = useMemo(
    () => buildWorkItemEventTeamNameMap(teamsQuery.data ?? [], requirement),
    [teamsQuery.data, requirement]
  );

  const quickAssigneeOptions = useMemo(() => {
    const options = (assigneesQuery.data ?? []).map((assignee) => ({
      value: assignee.id,
      label: assignee.name
    }));
    requirement?.responsible_users.forEach((responsible) => {
      if (!options.some((item) => item.value === responsible.id)) {
        options.push({
          value: responsible.id,
          label: responsible.name || responsible.id
        });
      }
    });
    return options;
  }, [assigneesQuery.data, requirement?.responsible_users]);
  const quickTeamOptions = useMemo(() => {
    const options = (teamsQuery.data ?? []).map((team) => ({
      value: team.id,
      label: team.name
    }));
    requirement?.team_ids.forEach((teamId, index) => {
      if (!options.some((item) => item.value === teamId)) {
        options.push({
          value: teamId,
          label: requirement.team_names[index] ?? teamId
        });
      }
    });
    return options;
  }, [requirement?.team_ids, requirement?.team_names, teamsQuery.data]);

  const resetQuickDraft = (target = requirement) => {
    if (!target) return;
    setDraftDeadline(target.deadline ? dayjs(target.deadline) : undefined);
    setDraftOwnerIds(target.responsible_user_ids);
    setDraftTeamIds(target.team_ids);
  };

  const openQuickEditor = (field: RequirementQuickEditField) => {
    resetQuickDraft();
    setQuickEditor(field);
  };

  const cancelMutation = useMutation({
    mutationFn: (target: MockRequirement) =>
      requirementsBoardApi.cancelRequirement(target.id, target.version),
    onSuccess: (updated) => {
      message.success("需求已取消");
      onSaved(updated);
      void invalidateBoard();
    },
    onError: (error) => {
      if (handleEditConflict(error, message, queryClient)) return;
      message.error(error instanceof Error ? error.message : "取消需求失败");
    }
  });

  const restoreMutation = useMutation({
    mutationFn: (target: MockRequirement) =>
      requirementsBoardApi.restoreRequirement(target.id, target.version),
    onSuccess: (updated) => {
      message.success("需求已恢复");
      onSaved(updated);
      void invalidateBoard();
    },
    onError: (error) => {
      if (handleEditConflict(error, message, queryClient)) return;
      message.error(error instanceof Error ? error.message : "恢复需求失败");
    }
  });

  const deleteMutation = useMutation({
    mutationFn: (target: MockRequirement) =>
      requirementsBoardApi.deleteRequirement(target.id, target.version),
    onSuccess: (_deleted, deletedRequirement) => {
      onClose();
      message.success("需求已删除");
      queryClient.removeQueries({
        queryKey: ["requirements-board", "requirement-events", deletedRequirement.id]
      });
      void invalidateRequirementTaskWorkspace(queryClient, {
        requirementId: deletedRequirement.id,
        skipRequirementEvents: true
      });
    },
    onError: (error) => {
      if (handleEditConflict(error, message, queryClient)) return;
      const text = error instanceof Error ? error.message : "删除需求失败";
      if (/409|has_associations|associated/i.test(text)) {
        message.warning("该需求已有历史数据，无法删除，可选择取消需求");
      } else {
        message.error(text);
      }
    }
  });

  const handleCancel = () => {
    if (!requirement) return;
    modal.confirm({
      title: "确认取消需求？",
      content:
        "取消后，该需求不会出现在主看板和 Dashboard 风险/关注中，但历史数据会保留，可后续恢复。",
      okText: "取消需求",
      okButtonProps: { danger: true },
      cancelText: "返回",
      onOk: () => cancelMutation.mutateAsync(requirement)
    });
  };

  const handleRestore = () => {
    if (!requirement) return;
    modal.confirm({
      title: "确认恢复需求？",
      content: "恢复后，该需求将回到待开始状态，并重新进入需求看板。",
      okText: "恢复需求",
      cancelText: "返回",
      onOk: () => restoreMutation.mutateAsync(requirement)
    });
  };

  const handleDelete = () => {
    if (!requirement) return;
    modal.confirm({
      title: "确认彻底删除？",
      content: "删除后不可恢复。仅适用于误创建且无历史数据的需求。",
      okText: "彻底删除",
      okButtonProps: { danger: true },
      cancelText: "取消",
      onOk: () => deleteMutation.mutateAsync(requirement)
    });
  };

  const requirementTokens = requirement
    ? sumTokensFromSources(requirement.token_source_ids, tokenSourceMap)
    : 0;
  const taskTokens = tasks.reduce(
    (total, task) => total + sumTokensFromSources(task.token_source_ids, tokenSourceMap),
    0
  );
  const totalTokens = requirementTokens + taskTokens;
  const completedCount = tasks.filter((task) => task.status === "done").length;
  const blockedCount = tasks.filter(isTaskBlocked).length;
  const overdueTaskCount = tasks.filter(
    (task) => task.status !== "done" && isBeforeToday(task.due_date)
  ).length;
  const requirementOverdue = Boolean(
    requirement &&
      requirement.status !== "completed" &&
      requirement.status !== "cancelled" &&
      isBeforeToday(requirement.deadline)
  );
  const canQuickEditRequirement = Boolean(requirement?.can_update);

  const quickUpdateMutation = useMutation({
    mutationFn: ({
      data
    }: {
      field: RequirementQuickEditField;
      data: {
        deadline?: string;
        responsible_user_ids?: string[];
        team_ids?: string[];
      };
    }) =>
      requirementsBoardApi.updateRequirement(requirement!.id, {
        ...data,
        base_version: requirement!.version
      }),
    onSuccess: (updated) => {
      message.success("已保存");
      onSaved(updated);
      resetQuickDraft(updated);
      setQuickEditor(undefined);
      void invalidateBoard(updated.id);
    },
    onError: (error) => {
      if (handleEditConflict(error, message, queryClient)) return;
      message.error(error instanceof Error ? error.message : "保存失败");
    }
  });

  const closeQuickEditor = () => {
    if (quickUpdateMutation.isPending) return;
    setQuickEditor(undefined);
  };

  const saveQuickOwner = () => {
    quickUpdateMutation.mutate({
      field: "owner",
      data: { responsible_user_ids: draftOwnerIds }
    });
  };

  const saveQuickTeams = () => {
    quickUpdateMutation.mutate({
      field: "teams",
      data: { team_ids: draftTeamIds }
    });
  };

  const linkMutation = useMutation({
    mutationFn: (sourceIds: string[]) =>
      requirementsBoardApi.setRequirementTokenSources(
        requirement!.id,
        sourceIds,
        requirement!.token_source_ids
      ),
    onSuccess: (updated) => {
      message.success("已关联 session");
      onSaved(updated);
      void invalidateBoard(updated.id);
      setPickerOpen(false);
    },
    onError: (error) => message.error(error instanceof Error ? error.message : "关联失败")
  });
  const unlinkMutation = useMutation({
    mutationFn: (sourceId: string) =>
      requirementsBoardApi.unlinkRequirementTokenSource(requirement!.id, sourceId),
    onSuccess: (updated) => {
      message.success("已移除 session");
      onSaved(updated);
      void invalidateBoard(updated.id);
    },
    onError: (error) => message.error(error instanceof Error ? error.message : "移除失败")
  });
  const moreActionItems = requirement
    ? requirement.status === "cancelled"
      ? requirement.can_delete
        ? [
            {
              key: "delete",
              danger: true,
              icon: <DeleteOutlined />,
              label: "删除需求",
              onClick: handleDelete
            }
          ]
        : []
      : [
          ...(requirement.can_cancel
            ? [
                {
                  key: "cancel",
                  icon: <StopOutlined />,
                  label: "取消需求",
                  onClick: handleCancel
                }
              ]
            : []),
          ...(requirement.can_delete
            ? [
                {
                  key: "delete",
                  danger: true,
                  icon: <DeleteOutlined />,
                  label: "删除需求",
                  onClick: handleDelete
                }
              ]
            : [])
        ]
    : [];
  const deadlineQuickEditor = (
    <div className="requirements-quick-edit">
      <DatePicker
        value={draftDeadline}
        format="YYYY-MM-DD"
        placement="topRight"
        getPopupContainer={getQuickEditPopupContainer}
        disabled={
          quickUpdateMutation.isPending && quickUpdateMutation.variables?.field === "deadline"
        }
        onChange={(value) => {
          setDraftDeadline(value ?? undefined);
          if (!value) return;
          quickUpdateMutation.mutate({
            field: "deadline",
            data: { deadline: value.format("YYYY-MM-DD") }
          });
        }}
        style={{ width: "100%" }}
      />
    </div>
  );
  const ownerQuickEditor = (
    <div className="requirements-quick-edit">
      <Select
        allowClear
        mode="multiple"
        showSearch
        value={draftOwnerIds}
        loading={assigneesQuery.isLoading}
        placeholder={assigneesQuery.isError ? "负责人加载失败" : "选择负责人"}
        optionFilterProp="label"
        maxTagCount="responsive"
        options={quickAssigneeOptions}
        placement="topLeft"
        getPopupContainer={getQuickEditPopupContainer}
        onChange={setDraftOwnerIds}
      />
      <div className="requirements-quick-edit__actions">
        <Button
          size="small"
          type="primary"
          loading={
            quickUpdateMutation.isPending && quickUpdateMutation.variables?.field === "owner"
          }
          onClick={saveQuickOwner}
        >
          保存
        </Button>
      </div>
    </div>
  );
  const teamsQuickEditor = (
    <div className="requirements-quick-edit requirements-quick-edit--wide">
      <Select
        mode="multiple"
        value={draftTeamIds}
        loading={teamsQuery.isLoading}
        disabled={teamsQuery.isLoading || teamsQuery.isError}
        placeholder={teamsQuery.isError ? "团队加载失败" : "选择参与团队"}
        options={quickTeamOptions}
        maxTagCount="responsive"
        placement="topLeft"
        getPopupContainer={getQuickEditPopupContainer}
        onChange={setDraftTeamIds}
      />
      <div className="requirements-quick-edit__actions">
        <Button
          size="small"
          type="primary"
          loading={
            quickUpdateMutation.isPending && quickUpdateMutation.variables?.field === "teams"
          }
          onClick={saveQuickTeams}
        >
          保存
        </Button>
      </div>
    </div>
  );

  return (
    <Drawer
      className="requirements-drawer requirements-drawer--requirement"
      width={980}
      open={Boolean(requirement)}
      onClose={onClose}
      closable={false}
      title={
        requirement ? (
          <div className="requirements-drawer__header">
            <div className="requirements-drawer__header-main">
              <button
                type="button"
                className="requirements-drawer__close"
                aria-label="关闭需求详情"
                onClick={onClose}
              >
                <CloseOutlined />
              </button>
              {onToggleFavorite ? (
                <button
                  type="button"
                  className={`requirements-drawer__favorite${isFavorite ? " is-active" : ""}`}
                  aria-label={isFavorite ? "取消关注" : "关注需求"}
                  onClick={onToggleFavorite}
                >
                  {isFavorite ? <StarFilled style={{ color: "#f59e0b" }} /> : <StarOutlined />}
                </button>
              ) : null}
              <div className="requirements-drawer__title">
                <Tooltip title={requirement.title}>
                  <strong>{requirement.title}</strong>
                </Tooltip>
                <div className="requirements-drawer__header-tags">
                  <RequirementStageTag stage={requirement.status} />
                  <RequirementPriorityTag priority={requirement.priority} />
                  {blockedCount ? <Tag color="error">阻塞 {blockedCount}</Tag> : null}
                  {requirementOverdue ? <Tag color="warning">需求逾期</Tag> : null}
                </div>
              </div>
            </div>
            <div className="requirements-drawer__header-actions">
              {canManage || canUpdateStatus ? (
                <>
                  {requirement.status === "cancelled" ? (
                    requirement.can_restore ? (
                      <Button
                        type="primary"
                        icon={<RollbackOutlined />}
                        loading={restoreMutation.isPending}
                        onClick={handleRestore}
                      >
                        恢复
                      </Button>
                    ) : null
                  ) : requirement.can_update ? (
                    <Button
                      type="primary"
                      icon={<EditOutlined />}
                      onClick={() => {
                        onCreatorOpenChange(false);
                        setPickerOpen(false);
                        setEditOpen(true);
                      }}
                    >
                      编辑
                    </Button>
                  ) : null}
                  {canUpdateStatus && requirement.status !== "cancelled" ? (
                    <Dropdown
                      menu={{
                        items: STATUS_COLUMNS.map((item) => ({
                          key: item.value,
                          label: item.label,
                          disabled: item.value === requirement.status,
                          onClick: () => onUpdateStatus(requirement, item.value)
                        }))
                      }}
                      trigger={["click"]}
                    >
                      <Button>修改进度</Button>
                    </Dropdown>
                  ) : null}
                  {moreActionItems.length ? (
                    <Dropdown menu={{ items: moreActionItems }} trigger={["click"]}>
                      <Button icon={<MoreOutlined />} aria-label="更多操作" />
                    </Dropdown>
                  ) : null}
                </>
              ) : null}
            </div>
          </div>
        ) : null
      }
    >
      {requirement ? (
        <div className="requirements-drawer__content">
          {editOpen ? (
            <RequirementEditModal
              embedded
              open={editOpen}
              requirement={requirement}
              onCancel={() => setEditOpen(false)}
              onSaved={(updated) => {
                onSaved(updated);
                setEditOpen(false);
              }}
            />
          ) : (
            <>
              {Boolean(requirement.description) && !requirement.description ? <section className="requirements-drawer__compact-summary" aria-label="需求关键信息">
                <div className="requirements-drawer__summary-item is-strong">
                  <span>任务</span>
                  <strong>
                    {tasks.length ? `${completedCount}/${tasks.length} 完成` : "0/0 完成"}
                  </strong>
                </div>
                <div
                  className={`requirements-drawer__summary-item${blockedCount ? " is-danger" : ""}`}
                >
                  <span>阻塞</span>
                  <strong>{blockedCount || 0}</strong>
                </div>
                <div
                  className={`requirements-drawer__summary-item${
                    requirementOverdue || overdueTaskCount ? " is-warning" : ""
                  }`}
                >
                  <span>逾期</span>
                  <strong>
                    {requirementOverdue
                      ? overdueTaskCount
                        ? `需求逾期 + ${overdueTaskCount} 任务`
                        : "需求逾期"
                      : overdueTaskCount
                        ? `${overdueTaskCount} 任务逾期`
                        : "无"}
                  </strong>
                </div>
                <div className="requirements-drawer__summary-item">
                  <span>截止</span>
                  <Tooltip title={formatDate(requirement.deadline)}>
                    <strong>{formatDate(requirement.deadline)}</strong>
                  </Tooltip>
                  {canQuickEditRequirement ? (
                    <Popover
                      open={quickEditor === "deadline"}
                      trigger="click"
                      placement="bottomRight"
                      destroyOnHidden
                      content={deadlineQuickEditor}
                      onOpenChange={(nextOpen) =>
                        nextOpen ? openQuickEditor("deadline") : closeQuickEditor()
                      }
                    >
                      <button
                        type="button"
                        className="requirements-drawer__summary-edit"
                        aria-label="设置截止日期"
                        onClick={(event) => event.stopPropagation()}
                      >
                        <EditOutlined />
                      </button>
                    </Popover>
                  ) : null}
                </div>
                <div className="requirements-drawer__summary-item is-wide">
                  <span>负责人</span>
                  <Tooltip title={getRequirementOwnerLabel(requirement)}>
                    <strong>{getRequirementOwnerLabel(requirement)}</strong>
                  </Tooltip>
                  {canQuickEditRequirement ? (
                    <Popover
                      open={quickEditor === "owner"}
                      trigger="click"
                      placement="bottomRight"
                      destroyOnHidden
                      content={ownerQuickEditor}
                      onOpenChange={(nextOpen) =>
                        nextOpen ? openQuickEditor("owner") : closeQuickEditor()
                      }
                    >
                      <button
                        type="button"
                        className="requirements-drawer__summary-edit"
                        aria-label="设置负责人"
                        onClick={(event) => event.stopPropagation()}
                      >
                        <EditOutlined />
                      </button>
                    </Popover>
                  ) : null}
                </div>
                <div className="requirements-drawer__summary-item is-wide">
                  <span>团队</span>
                  <Tooltip title={getRequirementTeamTitle(requirement)}>
                    <strong>{getRequirementTeamCompactLabel(requirement)}</strong>
                  </Tooltip>
                  {canQuickEditRequirement ? (
                    <Popover
                      open={quickEditor === "teams"}
                      trigger="click"
                      placement="bottomRight"
                      destroyOnHidden
                      content={teamsQuickEditor}
                      onOpenChange={(nextOpen) =>
                        nextOpen ? openQuickEditor("teams") : closeQuickEditor()
                      }
                    >
                      <button
                        type="button"
                        className="requirements-drawer__summary-edit"
                        aria-label="设置参与团队"
                        onClick={(event) => event.stopPropagation()}
                      >
                        <EditOutlined />
                      </button>
                    </Popover>
                  ) : null}
                </div>
              </section> : null}

              <section className="requirements-drawer__overview" aria-label="需求概览">
                <div className="requirements-drawer__overview-copy">
                  <p>{requirement.description || "暂无需求描述"}</p>
                </div>
                <dl className="requirements-drawer__overview-meta">
                  <div className="requirements-drawer__overview-meta-row is-actions">
                    <div><dt>截止时间</dt><dd>{formatDate(requirement.deadline)} {canQuickEditRequirement ? <Popover open={quickEditor === "deadline"} trigger="click" placement="bottomRight" destroyOnHidden content={deadlineQuickEditor} onOpenChange={(nextOpen) => nextOpen ? openQuickEditor("deadline") : closeQuickEditor()}><button type="button" className="requirements-drawer__summary-edit" aria-label="设置截止日期"><EditOutlined /></button></Popover> : null}</dd></div>
                    <div><dt>负责人</dt><dd>{getRequirementOwnerLabel(requirement)} {canQuickEditRequirement ? <Popover open={quickEditor === "owner"} trigger="click" placement="bottomRight" destroyOnHidden content={ownerQuickEditor} onOpenChange={(nextOpen) => nextOpen ? openQuickEditor("owner") : closeQuickEditor()}><button type="button" className="requirements-drawer__summary-edit" aria-label="设置负责人"><EditOutlined /></button></Popover> : null}</dd></div>
                    <div><dt>参与团队</dt><dd>{getRequirementTeamCompactLabel(requirement)} {canQuickEditRequirement ? <Popover open={quickEditor === "teams"} trigger="click" placement="bottomRight" destroyOnHidden content={teamsQuickEditor} onOpenChange={(nextOpen) => nextOpen ? openQuickEditor("teams") : closeQuickEditor()}><button type="button" className="requirements-drawer__summary-edit" aria-label="设置参与团队"><EditOutlined /></button></Popover> : null}</dd></div>
                  </div>
                  <div className="requirements-drawer__overview-meta-row is-details">
                    <div><dt>推进进度</dt><dd><RequirementProgress value={requirement.progress} /></dd></div>
                    <div><dt>创建者</dt><dd>{requirement.creator_name}</dd></div>
                    <div><dt>飞书文档</dt><dd>{requirement.feishu_doc_url ? <a href={requirement.feishu_doc_url} target="_blank" rel="noreferrer">打开文档</a> : "-"}</dd></div>
                  </div>
                </dl>
              </section>

              <Tabs
                key={requirement.id}
                className="requirements-drawer__tabs"
                defaultActiveKey="tasks"
                tabBarExtraContent={
                  <Button
                    className="requirements-drawer__tabs-session-action"
                    type="text"
                    size="small"
                    icon={<LinkOutlined />}
                    onClick={() => setPickerOpen(true)}
                  >
                    关联 session
                  </Button>
                }
                items={[
                  {
                    key: "tasks",
                    label: (
                      <span className="requirements-drawer__tab-label">
                        任务 <Badge size="small" count={tasks.length} />
                      </span>
                    ),
                    children: (
                      <section
                        className={`requirements-drawer__section requirements-drawer__task-section${
                          creatorOpen ? " is-creating" : ""
                        }`}
                      >
                        <div className="requirements-drawer__section-head">
                          <h3>任务拆解</h3>
                          <div className="requirements-drawer__section-actions">
                            <span>
                              {completedCount}/{tasks.length || 0} 完成
                              {blockedCount ? ` · ${blockedCount} 阻塞` : ""}
                            </span>
                            {requirement.can_create_task ? (
                              <Button
                                size="small"
                                icon={<PlusOutlined />}
                                disabled={creatorOpen}
                                onClick={() => {
                                  setQuickEditor(undefined);
                                  onCreatorOpenChange(true);
                                }}
                              >
                                添加任务
                              </Button>
                            ) : null}
                          </div>
                        </div>
                        {creatorOpen ? (
                          <TaskCreateModal
                            embedded
                            open={creatorOpen}
                            requirementId={requirement.id}
                            requirementTitle={requirement.title}
                            existingTasks={dependencyTasks}
                            onCancel={() => onCreatorOpenChange(false)}
                            onCreated={() => onCreatorOpenChange(false)}
                          />
                        ) : null}
                        {!tasks.length && !creatorOpen ? (
                          <div className="requirements-drawer__execution-empty">
                            <Empty
                              image={Empty.PRESENTED_IMAGE_SIMPLE}
                              description="尚未拆解任务"
                            />
                          </div>
                        ) : tasks.length ? (
                          <div className="requirements-drawer__task-list">
                            <div className="requirements-drawer__task-list-head" aria-hidden="true">
                              <span>任务</span>
                              <span>状态</span>
                              <span>负责人</span>
                              <span>截止</span>
                              <span>风险</span>
                              <span>操作</span>
                            </div>
                            {[...tasks]
                              .sort((a, b) => Number(isTaskBlocked(b)) - Number(isTaskBlocked(a)))
                              .map((task) => {
                                const riskBadges = getTaskRiskBadges(task);
                                const primaryRisk = riskBadges[0];
                                return (
                                  <button
                                    key={task.id}
                                    type="button"
                                    className={`requirements-drawer__task-row${
                                      isTaskBlocked(task) || primaryRisk ? " has-risk" : ""
                                    }`}
                                    onClick={() => onOpenTask(task)}
                                  >
                                    <div className="requirements-drawer__task-title">
                                      <strong title={task.title}>{task.title}</strong>
                                      <span title={getTaskDependencyTitle(task)}>
                                        {getTaskDependencySummary(task)}
                                      </span>
                                    </div>
                                    <div className="requirements-drawer__task-state">
                                      <TaskStatusPill status={task.status} />
                                    </div>
                                    <span
                                      className="requirements-drawer__task-owner"
                                      title={getTaskResponsibleTitle(task)}
                                    >
                                      {getTaskResponsibleLabel(task)}
                                    </span>
                                    <span
                                      className="requirements-drawer__task-date"
                                      title={formatDate(task.due_date)}
                                    >
                                      {formatDate(task.due_date)}
                                    </span>
                                    <span
                                      className={`requirements-drawer__task-risk${
                                        primaryRisk ? ` is-${primaryRisk.tone}` : ""
                                      }`}
                                      title={getTaskRiskLabel(task)}
                                    >
                                      {primaryRisk?.label ?? "正常"}
                                    </span>
                                    <span className="requirements-drawer__task-action">查看</span>
                                  </button>
                                );
                              })}
                          </div>
                        ) : null}
                      </section>
                    )
                  },
                  {
                    key: "acceptance",
                    label: (
                      <span className="requirements-drawer__tab-label">
                        验收 <Badge size="small" count={requirement.acceptance_criteria.length} />
                      </span>
                    ),
                    children: (
                      <section className="requirements-drawer__section">
                        <div className="requirements-drawer__section-head">
                          <h3>需求验收标准</h3>
                          <Tag>{requirement.acceptance_criteria.length} 项</Tag>
                        </div>
                        {requirement.acceptance_criteria.length ? (
                          <ol className="requirements-drawer__ac-list">
                            {requirement.acceptance_criteria.map((item, index) => (
                              <li key={`${index}-${item}`}>
                                <span>AC {index + 1}</span>
                                {item}
                              </li>
                            ))}
                          </ol>
                        ) : (
                          <Empty
                            image={Empty.PRESENTED_IMAGE_SIMPLE}
                            description="暂无需求验收标准"
                          />
                        )}
                      </section>
                    )
                  },
                  {
                    key: "records",
                    label: "关联session",
                    children: (
                      <section className="requirements-drawer__section">
                        <div className="requirements-drawer__section-head">
                          <h3>工作记录</h3>
                          <div className="requirements-drawer__section-actions">
                            {totalTokens > 0 ? (
                              <span>合计 {formatTokens(totalTokens)} Token</span>
                            ) : (
                              <span>暂无关联 session</span>
                            )}
                            <Button
                              size="small"
                              icon={<LinkOutlined />}
                              onClick={() => setPickerOpen(true)}
                            >
                              关联 session
                            </Button>
                          </div>
                        </div>
                        <TokenSourceList
                          requirementSources={requirement.token_source_ids
                            .map((id) => tokenSourceMap.get(id))
                            .filter((source): source is MockTokenSource => Boolean(source))}
                          taskSources={tasks.flatMap((task) =>
                            task.token_source_ids
                              .map((id) => tokenSourceMap.get(id))
                              .filter((source): source is MockTokenSource => Boolean(source))
                              .map((source) => ({ source, taskTitle: task.title }))
                          )}
                          onRemoveRequirementSource={(id) => unlinkMutation.mutate(id)}
                          removing={unlinkMutation.isPending ? unlinkMutation.variables : undefined}
                        />
                      </section>
                    )
                  },
                  {
                    key: "activity",
                    label: (
                      <span className="requirements-drawer__tab-label">
                        动态 <Badge size="small" count={requirementEventsQuery.data?.total ?? 0} />
                      </span>
                    ),
                    children: (
                      <section className="requirements-drawer__section requirements-drawer__events-panel">
                        <div className="requirements-drawer__section-head">
                          <h3>操作记录</h3>
                          <span>需求与任务关键变更</span>
                        </div>
                        <WorkItemEventTimeline
                          events={requirementEventsQuery.data?.items ?? []}
                          loading={requirementEventsQuery.isLoading}
                          emptyText="暂无操作记录"
                          userNameMap={eventUserNameMap}
                          teamNameMap={eventTeamNameMap}
                        />
                      </section>
                    )
                  },
                  {
                    key: "overview",
                    label: "信息",
                    children: (
                      <section className="requirements-drawer__section">
                        <h3>基础信息</h3>
                        <Descriptions column={1} size="small" colon={false}>
                          <Descriptions.Item label="需求描述">
                            <span className="requirements-drawer__info-text">
                              {requirement.description || "暂无需求描述"}
                            </span>
                          </Descriptions.Item>
                          <Descriptions.Item label="推进进度">
                            <span className="requirements-drawer__info-progress">
                              <RequirementProgress value={requirement.progress} />
                            </span>
                          </Descriptions.Item>
                          <Descriptions.Item label="负责人">
                            <span className="requirements-drawer__info-text">
                              {getRequirementOwnerLabel(requirement)}
                            </span>
                          </Descriptions.Item>
                          <Descriptions.Item label="创建者">
                            {requirement.creator_name}（
                            {ROLE_LABELS[requirement.creator_role as UserRole] ??
                              requirement.creator_role}
                            ）
                          </Descriptions.Item>
                          <Descriptions.Item label="参与团队">
                            <span className="requirements-drawer__info-text">
                              {requirement.team_names.join("、") || "-"}
                            </span>
                          </Descriptions.Item>
                          <Descriptions.Item label="更新时间">
                            {formatDateTime(requirement.updated_at)}
                          </Descriptions.Item>
                          <Descriptions.Item label="飞书文档">
                            {requirement.feishu_doc_url ? (
                              <a href={requirement.feishu_doc_url} target="_blank" rel="noreferrer">
                                <LinkOutlined /> 打开文档
                              </a>
                            ) : (
                              "-"
                            )}
                          </Descriptions.Item>
                        </Descriptions>
                      </section>
                    )
                  }
                ].filter((item) => item.key !== "records" && item.key !== "overview")}
              />

              <TokenSourcePicker
                open={pickerOpen}
                selectedIds={requirement.token_source_ids}
                confirmLoading={linkMutation.isPending}
                onCancel={() => setPickerOpen(false)}
                onConfirm={async (ids) => {
                  await linkMutation.mutateAsync(ids);
                }}
              />
            </>
          )}
        </div>
      ) : null}
    </Drawer>
  );
}

function RequirementEditModal({
  embedded = false,
  open,
  requirement,
  onCancel,
  onSaved
}: {
  embedded?: boolean;
  open: boolean;
  requirement: MockRequirement;
  onCancel: () => void;
  onSaved: (requirement: MockRequirement) => void;
}) {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [form] = Form.useForm<{
    title: string;
    description: string;
    priority: RequirementPriority;
    deadline?: dayjs.Dayjs;
    responsible_user_ids?: string[];
    team_ids?: string[];
    feishu_doc_url?: string;
    acceptance_criteria: string[];
  }>();
  const teamsQuery = useQuery({
    queryKey: ["requirements-board", "teams"],
    queryFn: () => requirementsBoardApi.listTeams(),
    staleTime: 5 * 60_000
  });
  const assigneesQuery = useQuery({
    queryKey: ["requirements-board", "assignees"],
    queryFn: () => requirementsBoardApi.listAssignees(),
    staleTime: 5 * 60_000
  });

  const initialValues = useMemo(
    () => ({
      title: requirement.title,
      description: requirement.description,
      priority: requirement.priority,
      deadline: requirement.deadline ? dayjs(requirement.deadline) : undefined,
      responsible_user_ids: requirement.responsible_user_ids,
      team_ids: requirement.team_ids,
      feishu_doc_url: requirement.feishu_doc_url ?? "",
      acceptance_criteria: requirement.acceptance_criteria.length
        ? requirement.acceptance_criteria
        : [""]
    }),
    [requirement]
  );

  useEffect(() => {
    if (open) {
      form.setFieldsValue(initialValues);
    } else {
      form.resetFields();
    }
  }, [form, initialValues, open]);

  const updateMutation = useMutation({
    mutationFn: (values: {
      title: string;
      description: string;
      priority: RequirementPriority;
      deadline?: dayjs.Dayjs;
      responsible_user_ids?: string[];
      team_ids?: string[];
      feishu_doc_url?: string;
      acceptance_criteria: string[];
    }) =>
      requirementsBoardApi.updateRequirement(requirement.id, {
        title: normalizeRequiredText(values.title),
        description: normalizeRequiredText(values.description),
        priority: values.priority,
        deadline: values.deadline ? values.deadline.format("YYYY-MM-DD") : undefined,
        responsible_user_ids: values.responsible_user_ids ?? [],
        team_ids: values.team_ids ?? [],
        feishu_doc_url: normalizeOptionalText(values.feishu_doc_url),
        acceptance_criteria: normalizeCriteria(values.acceptance_criteria),
        base_version: requirement.version
      }),
    onSuccess: (updated) => {
      message.success("需求已更新");
      void invalidateRequirementTaskWorkspace(queryClient, { requirementId: updated.id });
      onSaved(updated);
    },
    onError: (error) => {
      if (handleEditConflict(error, message, queryClient)) return;
      message.error(error instanceof Error ? error.message : "需求更新失败");
    }
  });
  const assigneeOptions = useMemo(() => {
    const options = (assigneesQuery.data ?? []).map((assignee) => ({
      value: assignee.id,
      label: assignee.name
    }));
    requirement.responsible_users.forEach((responsible) => {
      if (!options.some((item) => item.value === responsible.id)) {
        options.push({
          value: responsible.id,
          label: responsible.name || responsible.id
        });
      }
    });
    return options;
  }, [assigneesQuery.data, requirement.responsible_users]);

  const handleCancel = () => {
    if (updateMutation.isPending) return;
    onCancel();
  };

  const formContent = (
    <Form
      form={form}
      layout="vertical"
      className="requirements-edit-modal__form"
      initialValues={initialValues}
      onFinish={(values) => updateMutation.mutate(values)}
    >
      <section className="requirements-edit-modal__field-grid">
        <Form.Item
          className="requirements-edit-modal__full"
          label="需求标题"
          name="title"
          rules={titleRules("需求标题")}
        >
          <Input placeholder="需求标题" />
        </Form.Item>
        <Form.Item label="优先级" name="priority" rules={requiredSelectRules("优先级")}>
          <Select
            options={[
              { value: "low", label: "低" },
              { value: "medium", label: "中" },
              { value: "high", label: "高" },
              { value: "urgent", label: "紧急" }
            ]}
          />
        </Form.Item>
        <Form.Item label="截止日期" name="deadline">
          <DatePicker style={{ width: "100%" }} />
        </Form.Item>
        <Form.Item label="负责人" name="responsible_user_ids">
          <Select
            allowClear
            mode="multiple"
            showSearch
            loading={assigneesQuery.isLoading}
            placeholder={assigneesQuery.isError ? "负责人加载失败" : "可稍后指定"}
            optionFilterProp="label"
            maxTagCount="responsive"
            options={assigneeOptions}
          />
        </Form.Item>
        <Form.Item
          label="飞书文档链接"
          name="feishu_doc_url"
          rules={optionalUrlRules("飞书文档链接")}
        >
          <Input placeholder="https://..." />
        </Form.Item>
        <Form.Item className="requirements-edit-modal__full" label="参与团队" name="team_ids">
          <Select
            mode="multiple"
            loading={teamsQuery.isLoading}
            disabled={teamsQuery.isLoading || teamsQuery.isError}
            placeholder={teamsQuery.isError ? "团队加载失败" : "可稍后指定"}
            options={(teamsQuery.data ?? []).map((team) => ({
              value: team.id,
              label: team.name
            }))}
          />
        </Form.Item>
      </section>

      <section className="requirements-edit-modal__detail-grid">
        <Form.Item label="需求描述" name="description" rules={descriptionRules("需求描述")}>
          <Input.TextArea rows={4} placeholder="补充背景与目标" />
        </Form.Item>
        <Form.Item
          label="标准列表"
          name="acceptance_criteria"
          rules={acceptanceCriteriaRules()}
          extra="留空可清空需求验收标准"
        >
          <AcceptanceCriteriaEditor placeholder="输入一条可验证的需求验收标准" />
        </Form.Item>
      </section>
    </Form>
  );

  if (embedded) {
    return (
      <section className="requirements-edit-panel">
        <div className="requirements-edit-panel__head">
          <div className="requirements-edit-modal__title">
            <strong>编辑需求</strong>
            <span>保存后会同步刷新需求详情和看板状态</span>
          </div>
          <Button onClick={handleCancel}>返回详情</Button>
        </div>
        <div className="requirements-edit-panel__body">{formContent}</div>
        <div className="requirements-edit-panel__footer">
          <Button onClick={handleCancel}>取消</Button>
          <Button type="primary" loading={updateMutation.isPending} onClick={() => form.submit()}>
            保存
          </Button>
        </div>
      </section>
    );
  }

  return (
    <Modal
      className="requirements-edit-modal"
      title={
        <div className="requirements-edit-modal__title">
          <strong>编辑需求</strong>
        </div>
      }
      open={open}
      width={820}
      style={{ top: 36 }}
      styles={{
        body: {
          maxHeight: "calc(100vh - 190px)",
          overflowY: "auto",
          padding: "16px 24px 6px"
        }
      }}
      destroyOnHidden
      onCancel={handleCancel}
      onOk={() => form.submit()}
      okText="保存"
      cancelText="取消"
      confirmLoading={updateMutation.isPending}
    >
      {formContent}
    </Modal>
  );
}

function TaskCreateModal({
  embedded = false,
  open,
  requirementId,
  requirementTitle,
  existingTasks,
  onCancel,
  onCreated
}: {
  embedded?: boolean;
  open: boolean;
  requirementId: string;
  requirementTitle: string;
  existingTasks: MockTask[];
  onCancel: () => void;
  onCreated: (task: MockTask) => void;
}) {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [form] = Form.useForm<{
    title: string;
    responsible_user_ids: string[];
    priority: MockTaskPriority;
    due_date?: dayjs.Dayjs;
    dependency_task_ids?: string[];
    acceptance_criteria?: string[];
  }>();

  const assigneesQuery = useQuery({
    queryKey: ["requirements-board", "assignees"],
    queryFn: () => requirementsBoardApi.listAssignees(),
    staleTime: 5 * 60_000
  });

  const createMutation = useMutation({
    mutationFn: (values: {
      title: string;
      responsible_user_ids: string[];
      priority: MockTaskPriority;
      due_date?: dayjs.Dayjs;
      dependency_task_ids?: string[];
      acceptance_criteria?: string[];
    }) =>
      requirementsBoardApi.createTask({
        requirement_id: requirementId,
        title: normalizeRequiredText(values.title),
        acceptance_criteria: normalizeCriteria(values.acceptance_criteria),
        responsible_user_ids: values.responsible_user_ids ?? [],
        priority: values.priority,
        due_date: values.due_date?.format("YYYY-MM-DD"),
        dependency_task_ids: values.dependency_task_ids
      }),
    onSuccess: (task) => {
      message.success("任务已创建");
      form.resetFields();
      void invalidateRequirementTaskWorkspace(queryClient, {
        requirementId: task.requirement_id,
        taskId: task.id
      });
      onCreated(task);
    },
    onError: (error) => message.error(error instanceof Error ? error.message : "创建任务失败")
  });

  const handleCancel = () => {
    if (createMutation.isPending) return;
    form.resetFields();
    onCancel();
  };

  const formContent = (
    <Form
      className="requirements-task-modal__form"
      form={form}
      layout="vertical"
      initialValues={{ priority: "medium" }}
      onFinish={(values) => createMutation.mutate(values)}
    >
      <section className="requirements-task-modal__section">
        <h4>基本信息</h4>
        <Form.Item label="任务标题" name="title" rules={titleRules("任务标题")}>
          <Input placeholder="输入清晰、可交付的任务标题" />
        </Form.Item>
        <div className="requirements-task-modal__grid">
          <Form.Item
            label="负责人"
            name="responsible_user_ids"
            rules={requiredSelectRules("负责人")}
          >
            <Select
              mode="multiple"
              showSearch
              maxTagCount="responsive"
              optionFilterProp="label"
              placeholder="选择负责人"
              loading={assigneesQuery.isLoading}
              disabled={assigneesQuery.isLoading || assigneesQuery.isError}
              options={(assigneesQuery.data ?? []).map((item: MockAssignee) => ({
                value: item.id,
                label: item.name
              }))}
            />
          </Form.Item>
          <Form.Item label="优先级" name="priority" rules={requiredSelectRules("优先级")}>
            <Select
              options={[
                { value: "low", label: "低" },
                { value: "medium", label: "中" },
                { value: "high", label: "高" }
              ]}
            />
          </Form.Item>
          <Form.Item label="截止日期" name="due_date">
            <DatePicker style={{ width: "100%" }} />
          </Form.Item>
        </div>
      </section>
      <section className="requirements-task-modal__section">
        <h4>依赖关系</h4>
        <Form.Item label="上游依赖任务" name="dependency_task_ids" rules={dependencyArrayRules()}>
          <DependencyTaskPicker tasks={existingTasks} />
        </Form.Item>
      </section>
      <section className="requirements-task-modal__section">
        <h4>验收标准</h4>
        <Form.Item label="标准列表" name="acceptance_criteria" rules={acceptanceCriteriaRules()}>
          <AcceptanceCriteriaEditor placeholder="输入一条可验证的任务验收标准" />
        </Form.Item>
      </section>
    </Form>
  );

  if (embedded) {
    if (!open) return null;
    return (
      <section className="requirements-task-inline-form">
        <div className="requirements-task-inline-form__head">
          <div className="requirements-modal-title">
            <strong>添加任务</strong>
            <span>所属需求：{requirementTitle}</span>
          </div>
          <Button onClick={handleCancel}>收起</Button>
        </div>
        {formContent}
        <div className="requirements-task-inline-form__footer">
          <Button onClick={handleCancel}>取消</Button>
          <Button type="primary" loading={createMutation.isPending} onClick={() => form.submit()}>
            创建任务
          </Button>
        </div>
      </section>
    );
  }

  return (
    <Modal
      className="requirements-task-modal"
      title={
        <div className="requirements-modal-title">
          <strong>添加任务</strong>
          <span>所属需求：{requirementTitle}</span>
        </div>
      }
      open={open}
      width={640}
      destroyOnHidden
      onCancel={handleCancel}
      onOk={() => form.submit()}
      okText="创建任务"
      cancelText="取消"
      confirmLoading={createMutation.isPending}
    >
      {formContent}
    </Modal>
  );
}

function TokenSourceList({
  requirementSources,
  taskSources,
  onRemoveRequirementSource,
  removing
}: {
  requirementSources: MockTokenSource[];
  taskSources: Array<{ source: MockTokenSource; taskTitle: string }>;
  onRemoveRequirementSource?: (id: string) => void;
  removing?: string;
}) {
  const [visibleCount, setVisibleCount] = useState(8);
  const records = [
    ...requirementSources.map((source) => ({
      key: `requirement-${source.id}`,
      source,
      tag: <Tag color="geekblue">需求关联</Tag>,
      removable: Boolean(onRemoveRequirementSource)
    })),
    ...taskSources.map(({ source, taskTitle }) => ({
      key: `task-${taskTitle}-${source.id}`,
      source,
      tag: <Tag color="purple">来自任务：{taskTitle}</Tag>,
      removable: false
    }))
  ];
  const visibleRecords = records.slice(0, visibleCount);
  const hasMore = visibleCount < records.length;

  if (!records.length) {
    return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无关联 session" />;
  }
  return (
    <div className="requirements-drawer__record-list">
      <div className="requirements-drawer__record-scroll">
        {visibleRecords.map(({ key, source, tag, removable }) => (
          <div key={key} className="requirements-drawer__token-row">
            <div className="requirements-drawer__token-row-main">
              <strong title={source.summary || "（无摘要）"}>
                {source.summary || "（无摘要）"}
              </strong>
              <span>
                {formatTokenSourceTime(source.recorded_at)} · {source.tool} · {source.uploader}
              </span>
            </div>
            <div className="requirements-drawer__token-row-meta">
              {tag}
              <span>{formatTokens(source.token)} Token</span>
              {removable && onRemoveRequirementSource ? (
                <Button
                  size="small"
                  type="text"
                  icon={<CloseOutlined />}
                  loading={removing === source.id}
                  onClick={() => onRemoveRequirementSource(source.id)}
                  aria-label="移除"
                />
              ) : null}
            </div>
          </div>
        ))}
      </div>
      {hasMore ? (
        <div className="requirements-drawer__record-footer">
          <span>
            已显示 {visibleRecords.length}/{records.length} 条
          </span>
          <Button size="small" onClick={() => setVisibleCount((current) => current + 8)}>
            加载更多
          </Button>
        </div>
      ) : records.length > 8 ? (
        <div className="requirements-drawer__record-footer">
          <span>已显示全部 {records.length} 条</span>
        </div>
      ) : null}
    </div>
  );
}

function WorkItemEventTimeline({
  events,
  loading,
  emptyText,
  compact = false,
  userNameMap,
  teamNameMap
}: {
  events: WorkItemEventDTO[];
  loading: boolean;
  emptyText: string;
  compact?: boolean;
  userNameMap?: Map<string, string>;
  teamNameMap?: Map<string, string>;
}) {
  if (loading) {
    return (
      <div className="requirements-event-timeline">
        <Skeleton active paragraph={{ rows: compact ? 2 : 4 }} title={false} />
      </div>
    );
  }
  if (!events.length) {
    return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={emptyText} />;
  }
  return (
    <Timeline
      className={`requirements-event-timeline${compact ? " is-compact" : ""}`}
      items={events.map((event) => {
        const metaText = getWorkItemEventMetaText(event, userNameMap, teamNameMap);
        const eventTitle = getWorkItemEventTitle(event);
        return {
          key: event.id,
          dot: <ClockCircleOutlined />,
          children: (
            <div className="requirements-event-timeline__item">
              <div className="requirements-event-timeline__body">
                <div className="requirements-event-timeline__main">
                  <strong title={eventTitle}>{eventTitle}</strong>
                  <span>{formatDateTime(event.created_at)}</span>
                </div>
                <div className="requirements-event-timeline__meta">
                  <span title={getWorkItemEventActorTitle(event)}>
                    {getWorkItemEventActorLabel(event)}
                  </span>
                  {metaText ? <em title={metaText}>{metaText}</em> : null}
                </div>
              </div>
            </div>
          )
        };
      })}
    />
  );
}

function getWorkItemEventTitle(event: WorkItemEventDTO) {
  const baseTitle = event.event_title || "操作记录";
  if (event.event_type === "task_deleted") {
    const taskTitle = getWorkItemEventString(event.before_data?.title);
    if (taskTitle && !baseTitle.includes(taskTitle)) {
      return `${baseTitle}：${taskTitle}`;
    }
  }
  return baseTitle;
}

function getWorkItemEventString(value: unknown) {
  return typeof value === "string" ? value.trim() : "";
}

function getWorkItemEventActorLabel(event: WorkItemEventDTO) {
  const role = ROLE_LABELS[event.actor_role as UserRole] ?? event.actor_role;
  if (!event.actor_name) return "系统";
  return role ? `${event.actor_name}（${role}）` : event.actor_name;
}

function getWorkItemEventActorTitle(event: WorkItemEventDTO) {
  const actor = getWorkItemEventActorLabel(event);
  return event.actor_id ? `${actor} · ID ${event.actor_id}` : actor;
}

function getWorkItemEventMetaText(
  event: WorkItemEventDTO,
  userNameMap?: Map<string, string>,
  teamNameMap?: Map<string, string>
) {
  const metadata = event.metadata ?? {};
  if (typeof metadata.target_title === "string" && metadata.target_title) {
    return `关联对象：${metadata.target_title}`;
  }
  if (typeof metadata.session_id === "string" && metadata.session_id) {
    return metadata.activity_date
      ? `Session ${metadata.session_id} · ${metadata.activity_date}`
      : `Session ${metadata.session_id}`;
  }
  const changed = metadata.changed_fields;
  if (Array.isArray(changed) && changed.length) {
    const changeText = getWorkItemEventChangeText(
      event,
      changed.map((field) => String(field)),
      userNameMap,
      teamNameMap
    );
    if (changeText) return changeText;
    return `变更字段：${changed.map((field) => getWorkItemEventFieldLabel(String(field))).join("、")}`;
  }
  if (event.target_type === "task" && event.task_id) {
    return "任务动态";
  }
  return "";
}

function getWorkItemEventChangeText(
  event: WorkItemEventDTO,
  fields: string[],
  userNameMap?: Map<string, string>,
  teamNameMap?: Map<string, string>
) {
  const details = fields
    .map((field) => {
      const beforeValue = formatWorkItemEventValue(
        field,
        event.before_data?.[field],
        event,
        userNameMap,
        teamNameMap
      );
      const afterValue = formatWorkItemEventValue(
        field,
        event.after_data?.[field],
        event,
        userNameMap,
        teamNameMap
      );
      if (!beforeValue && !afterValue) return "";
      if (beforeValue === afterValue) return "";
      return `${getWorkItemEventFieldLabel(field)}：${beforeValue || "未设置"} → ${afterValue || "未设置"}`;
    })
    .filter(Boolean);
  return details.join("；");
}

function formatWorkItemEventValue(
  field: string,
  value: unknown,
  event: WorkItemEventDTO,
  userNameMap?: Map<string, string>,
  teamNameMap?: Map<string, string>
) {
  if (value === null || value === undefined || value === "") return "";
  if (field === "status" && typeof value === "string") {
    if (event.target_type === "task" || event.event_type.startsWith("task_")) {
      return TASK_STATUS_META[value as MockTaskStatus]?.label ?? value;
    }
    return STAGE_META[value as RequirementStage]?.label ?? value;
  }
  if (field === "priority" && typeof value === "string") {
    return PRIORITY_META[value as RequirementPriority]?.label ?? value;
  }
  if ((field === "deadline" || field === "due_date") && typeof value === "string") {
    return formatDate(value);
  }
  if (field === "progress" && (typeof value === "number" || typeof value === "string")) {
    return `${value}%`;
  }
  if (field === "acceptance_criteria" && Array.isArray(value)) {
    return `${value.length} 项`;
  }
  if (field === "responsible_user_ids" && Array.isArray(value)) {
    return formatMappedEventValues(value, userNameMap);
  }
  if (field === "team_ids" && Array.isArray(value)) {
    return formatMappedEventValues(value, teamNameMap);
  }
  if (Array.isArray(value)) {
    return value.length ? value.map((item) => String(item)).join("、") : "";
  }
  if (typeof value === "object") {
    return JSON.stringify(value);
  }
  return truncateEventValue(String(value));
}

function formatMappedEventValues(values: unknown[], nameMap?: Map<string, string>) {
  return values.length
    ? values
        .map((item) => {
          const id = String(item);
          return nameMap?.get(id) ?? id;
        })
        .join("、")
    : "";
}

function buildWorkItemEventUserNameMap(
  assignees: MockAssignee[],
  requirement?: MockRequirement,
  task?: MockTask
) {
  const result = new Map<string, string>();
  assignees.forEach((assignee) => {
    result.set(assignee.id, assignee.name || assignee.id);
  });
  requirement?.responsible_users.forEach((responsible) => {
    result.set(responsible.id, responsible.name || responsible.id);
  });
  if (requirement?.creator_id) {
    result.set(requirement.creator_id, requirement.creator_name || requirement.creator_id);
  }
  task?.responsible_users.forEach((responsible) => {
    result.set(responsible.id, responsible.name || responsible.id);
  });
  return result;
}

function buildWorkItemEventTeamNameMap(teams: MockTeam[], requirement?: MockRequirement) {
  const result = new Map<string, string>();
  teams.forEach((team) => {
    result.set(team.id, team.name || team.id);
  });
  requirement?.team_ids.forEach((teamId, index) => {
    const teamName = requirement.team_names[index];
    if (teamName) result.set(teamId, teamName);
  });
  return result;
}

function truncateEventValue(value: string) {
  return value.length > 36 ? `${value.slice(0, 36)}...` : value;
}

function getWorkItemEventFieldLabel(field: string) {
  const labels: Record<string, string> = {
    title: "标题",
    description: "描述",
    feishu_doc_url: "飞书文档",
    priority: "优先级",
    status: "状态",
    deadline: "截止日期",
    due_date: "截止日期",
    responsible_user_ids: "负责人",
    team_ids: "参与团队",
    progress: "进度",
    acceptance_criteria: "验收标准"
  };
  return labels[field] ?? field;
}

function BlockingDependencyTrace({
  task,
  dependencyTasks,
  onOpenTask,
  compact = false
}: {
  task: MockTask;
  dependencyTasks: MockTask[];
  onOpenTask?: (task: MockTask) => void;
  compact?: boolean;
}) {
  const blockingDependencies = getBlockingDependencies(task);
  if (!blockingDependencies.length) return null;

  const content = (
    <div className="requirements-blocker-popover">
      <div className="requirements-blocker-popover__head">
        <strong>阻塞来源</strong>
        <span>{blockingDependencies.length} 项未完成</span>
      </div>
      <div className="requirements-blocker-popover__list">
        {blockingDependencies.map((dependency) => {
          const dependencyId = getDependencyId(dependency);
          const dependencyType = getDependencyType(dependency);
          const targetTask = getLoadedDependencyTask(dependency, dependencyTasks);
          const canOpen = Boolean(targetTask && onOpenTask);
          return (
            <button
              type="button"
              className={`requirements-blocker-popover__item${canOpen ? "" : " is-disabled"}`}
              key={`${dependencyType}:${dependencyId}`}
              disabled={!canOpen}
              title={canOpen ? "打开依赖任务" : "依赖对象未加载，暂不能打开"}
              onClick={() => {
                if (targetTask && onOpenTask) onOpenTask(targetTask);
              }}
            >
              <div>
                <strong title={getDependencyTitle(dependency)}>
                  {getDependencyTitle(dependency)}
                </strong>
                <span title={dependency.requirement_title || "需求依赖"}>
                  {dependency.requirement_title ||
                    (dependencyType === "requirement" ? "需求依赖" : "所属需求未加载")}
                </span>
                <div className="requirements-blocker-popover__meta">
                  <Tag color={dependencyTone(dependency, targetTask)}>
                    {dependencyStatusText(dependency, targetTask)}
                  </Tag>
                  <em>
                    截止 {formatDate(targetTask?.due_date ?? dependency.due_date ?? undefined)}
                  </em>
                  {targetTask ? (
                    <em title={getTaskResponsibleTitle(targetTask)}>
                      {getTaskResponsibleLabel(targetTask)}
                    </em>
                  ) : null}
                </div>
              </div>
              {canOpen ? <RightOutlined className="requirements-clickable-task-cue" /> : null}
            </button>
          );
        })}
      </div>
    </div>
  );

  if (compact) {
    return (
      <Popover placement="bottomLeft" content={content} trigger={["hover", "click"]} zIndex={1500}>
        <button type="button" className="requirements-blocker-trigger">
          <WarningOutlined />
          上游阻塞 · {blockingDependencies.length}
        </button>
      </Popover>
    );
  }

  return (
    <div className="requirements-task-detail__blocker-card">
      <WarningOutlined />
      <div className="requirements-task-detail__blocker-card-body">
        <div className="requirements-task-detail__blocker-card-head">
          <strong>依赖阻塞</strong>
          <span>{blockingDependencies.length} 项未完成</span>
        </div>
        <div className="requirements-task-detail__blocker-list">
          {blockingDependencies.map((dependency) => {
            const dependencyId = getDependencyId(dependency);
            const dependencyType = getDependencyType(dependency);
            const targetTask = getLoadedDependencyTask(dependency, dependencyTasks);
            const canOpen = Boolean(targetTask && onOpenTask);
            return (
              <button
                type="button"
                className={`requirements-task-detail__blocker-item${canOpen ? "" : " is-disabled"}`}
                key={`${dependencyType}:${dependencyId}`}
                disabled={!canOpen}
                title={canOpen ? "打开依赖任务" : "依赖对象未加载，暂不能打开"}
                onClick={() => {
                  if (targetTask && onOpenTask) onOpenTask(targetTask);
                }}
              >
                <div>
                  <strong title={getDependencyTitle(dependency)}>
                    {getDependencyTitle(dependency)}
                  </strong>
                  <span title={dependency.requirement_title || "所属需求未加载"}>
                    {dependency.requirement_title || "所属需求未加载"}
                  </span>
                </div>
                <Tag color={dependencyTone(dependency, targetTask)}>
                  {dependencyStatusText(dependency, targetTask)}
                </Tag>
                {canOpen ? <RightOutlined className="requirements-clickable-task-cue" /> : null}
              </button>
            );
          })}
        </div>
        {blockingDependencies.length > 5 ? (
          <div
            className="requirements-task-detail__dependency-more requirements-task-detail__dependency-more--blocker"
            title={blockingDependencies
              .map((dependency) => getDependencyTitle(dependency))
              .join("、")}
          >
            共 {blockingDependencies.length} 个阻塞依赖
          </div>
        ) : null}
      </div>
    </div>
  );
}

function TaskDependencyList({
  task,
  dependencyTasks,
  onOpenTask
}: {
  task: MockTask;
  dependencyTasks: MockTask[];
  onOpenTask: (task: MockTask) => void;
}) {
  if (!task.dependencies.length) return <strong>无上游依赖</strong>;
  const dependencyCount = task.dependencies.length;

  return (
    <div
      className={`requirements-task-detail__dependency-stack${
        dependencyCount > 5 ? " is-scrollable" : ""
      }`}
    >
      <div className="requirements-task-detail__dependency-list">
        {task.dependencies.map((dependency) => {
          const dependencyId = getDependencyId(dependency);
          const dependencyType = getDependencyType(dependency);
          const targetTask = getLoadedDependencyTask(dependency, dependencyTasks);
          const canOpen = Boolean(targetTask);
          const done = isDependencyDone(dependency);
          return (
            <button
              type="button"
              key={`${dependencyType}:${dependencyId}`}
              className={`requirements-task-detail__dependency-item${done ? " is-done" : " is-blocked"}${
                canOpen ? "" : " is-disabled"
              }`}
              disabled={!canOpen}
              title={canOpen ? "打开依赖任务" : "依赖对象未加载，暂不能打开"}
              onClick={() => {
                if (targetTask) onOpenTask(targetTask);
              }}
            >
              <div>
                <strong title={getDependencyTitle(dependency)}>
                  {getDependencyTitle(dependency)}
                </strong>
                <span
                  title={
                    dependency.requirement_title ||
                    (dependencyType === "requirement" ? "需求依赖" : "所属需求未加载")
                  }
                >
                  {dependency.requirement_title ||
                    (dependencyType === "requirement" ? "需求依赖" : "所属需求未加载")}
                </span>
              </div>
              <Tag color={dependencyTone(dependency, targetTask)}>
                {dependencyStatusText(dependency, targetTask)}
              </Tag>
              {canOpen ? <RightOutlined className="requirements-clickable-task-cue" /> : null}
            </button>
          );
        })}
      </div>
      {dependencyCount > 1 ? (
        <div
          className="requirements-task-detail__dependency-more"
          title={task.dependencies.map((dependency) => getDependencyTitle(dependency)).join("、")}
        >
          共 {dependencyCount} 个上游依赖
        </div>
      ) : null}
    </div>
  );
}

type DependencyPickerTask = {
  id: string;
  title: string;
  requirementId: string;
  requirementTitle: string;
  status?: string;
  responsibleLabel?: string;
  dueDate?: string;
  priority?: MockTaskPriority;
};

function DependencyTaskPicker({
  value,
  onChange,
  tasks,
  currentTaskId,
  dependencyFallbacks = EMPTY_DEPENDENCIES
}: {
  value?: string[];
  onChange?: (next: string[]) => void;
  tasks: MockTask[];
  currentTaskId?: string;
  dependencyFallbacks?: MockTaskDependency[];
}) {
  const { message } = App.useApp();
  const [open, setOpen] = useState(false);
  const [keyword, setKeyword] = useState("");
  const [draftSelected, setDraftSelected] = useState<string[]>([]);
  const [activeRequirementId, setActiveRequirementId] = useState<string>();
  const [loadedDependencyTasks, setLoadedDependencyTasks] = useState<MockTask[]>([]);
  const [dependencyTaskPage, setDependencyTaskPage] = useState(1);
  const [loadingMoreDependencyTasks, setLoadingMoreDependencyTasks] = useState(false);
  const selectedValues = useMemo(() => value ?? [], [value]);
  const selectedValueKey = selectedValues.join("|");
  const requirementKeyword = keyword.trim();
  const dependencyTasksQuery = useQuery({
    queryKey: ["requirements-board", "dependency-task-picker", requirementKeyword],
    queryFn: () =>
      requirementsBoardApi.listTasksPage({
        page: "1",
        page_size: String(DEPENDENCY_PICKER_PAGE_SIZE),
        ...(requirementKeyword ? { requirement_keyword: requirementKeyword } : {})
      }),
    enabled: open,
    staleTime: 30_000
  });
  const queriedTasks = useMemo(
    () => [...(dependencyTasksQuery.data?.items ?? []), ...loadedDependencyTasks],
    [dependencyTasksQuery.data?.items, loadedDependencyTasks]
  );
  const dependencyTotal = dependencyTasksQuery.data?.total ?? queriedTasks.length;
  const dependencyHasMore = queriedTasks.length < dependencyTotal;
  const pickerTasks = useMemo(() => {
    const result = new Map<string, DependencyPickerTask>();
    const sourceTasks = open ? queriedTasks : tasks;
    sourceTasks
      .filter((task) => task.id !== currentTaskId)
      .forEach((task) => {
        result.set(task.id, {
          id: task.id,
          title: task.title,
          requirementId: task.requirement_id || "unknown",
          requirementTitle: task.requirement_title || "未命名需求",
          status: task.status,
          responsibleLabel: getTaskResponsibleLabel(task),
          dueDate: task.due_date,
          priority: task.priority
        });
      });

    dependencyFallbacks
      .filter((dependency) => getDependencyType(dependency) === "task")
      .forEach((dependency) => {
        const dependencyId = getDependencyId(dependency);
        if (!dependencyId || dependencyId === currentTaskId || result.has(dependencyId)) return;
        result.set(dependencyId, {
          id: dependencyId,
          title: getDependencyTitle(dependency),
          requirementId: dependency.requirement_id || "unknown",
          requirementTitle: dependency.requirement_title || "所属需求未加载",
          status: dependency.status,
          dueDate: dependency.due_date
        });
      });

    return Array.from(result.values()).sort((left, right) => {
      const requirementCompare = left.requirementTitle.localeCompare(
        right.requirementTitle,
        "zh-Hans-CN"
      );
      if (requirementCompare !== 0) return requirementCompare;
      return left.title.localeCompare(right.title, "zh-Hans-CN");
    });
  }, [currentTaskId, dependencyFallbacks, open, queriedTasks, tasks]);

  const taskMap = useMemo(() => new Map(pickerTasks.map((task) => [task.id, task])), [pickerTasks]);
  const groups = useMemo(() => {
    const result = new Map<string, { id: string; title: string; tasks: DependencyPickerTask[] }>();
    pickerTasks.forEach((task) => {
      const group = result.get(task.requirementId) ?? {
        id: task.requirementId,
        title: task.requirementTitle,
        tasks: []
      };
      group.tasks.push(task);
      result.set(task.requirementId, group);
    });
    return Array.from(result.values());
  }, [pickerTasks]);
  const selectedTasks = selectedValues
    .map((taskId) => taskMap.get(taskId))
    .filter((task): task is DependencyPickerTask => Boolean(task));
  const draftSelectedTasks = draftSelected
    .map((taskId) => taskMap.get(taskId))
    .filter((task): task is DependencyPickerTask => Boolean(task));
  const filteredGroups = groups;
  const activeGroup =
    filteredGroups.find((group) => group.id === activeRequirementId) ?? filteredGroups[0];

  useEffect(() => {
    if (!open) return;
    const timer = window.setTimeout(() => {
      setDraftSelected(selectedValues);
      setKeyword("");
      setLoadedDependencyTasks([]);
      setDependencyTaskPage(1);
      setActiveRequirementId(undefined);
    }, 0);
    return () => window.clearTimeout(timer);
  }, [open, selectedValueKey, selectedValues]);

  useEffect(() => {
    if (!open) return;
    const timer = window.setTimeout(() => {
      setLoadedDependencyTasks([]);
      setDependencyTaskPage(1);
      setActiveRequirementId(undefined);
    }, 0);
    return () => window.clearTimeout(timer);
  }, [open, requirementKeyword]);

  useEffect(() => {
    if (!open || !groups.length) return;
    const activeStillVisible = groups.some((group) => group.id === activeRequirementId);
    if (activeStillVisible) return;
    const firstSelected = selectedValues.map((taskId) => taskMap.get(taskId)).find(Boolean);
    const timer = window.setTimeout(
      () => setActiveRequirementId(firstSelected?.requirementId ?? groups[0]?.id),
      0
    );
    return () => window.clearTimeout(timer);
  }, [activeRequirementId, groups, open, selectedValues, taskMap]);

  const toggleTask = (taskId: string) => {
    setDraftSelected((current) =>
      current.includes(taskId) ? current.filter((item) => item !== taskId) : [...current, taskId]
    );
  };
  const removeDraftTask = (taskId: string) => {
    setDraftSelected((current) => current.filter((item) => item !== taskId));
  };
  const confirmSelection = () => {
    onChange?.(draftSelected);
    setOpen(false);
  };
  const loadMoreDependencyTasks = async () => {
    if (!dependencyHasMore || loadingMoreDependencyTasks) return;
    const nextPage = dependencyTaskPage + 1;
    setLoadingMoreDependencyTasks(true);
    try {
      const payload = await requirementsBoardApi.listTasksPage({
        page: String(nextPage),
        page_size: String(DEPENDENCY_PICKER_PAGE_SIZE),
        ...(requirementKeyword ? { requirement_keyword: requirementKeyword } : {})
      });
      setLoadedDependencyTasks((current) => [...current, ...payload.items]);
      setDependencyTaskPage(nextPage);
    } catch (error) {
      message.error(error instanceof Error ? error.message : "依赖任务加载失败");
    } finally {
      setLoadingMoreDependencyTasks(false);
    }
  };

  return (
    <>
      <button
        type="button"
        className="requirements-dependency-picker-trigger"
        onClick={() => {
          setOpen(true);
        }}
      >
        <div>
          <span>按需求分组</span>
          {selectedTasks.length ? (
            <strong>
              已选择 {selectedTasks.length} 个任务
              <em>
                {selectedTasks
                  .slice(0, 2)
                  .map((task) => task.title)
                  .join("，")}
                {selectedTasks.length > 2 ? ` 等 ${selectedTasks.length} 个任务` : ""}
              </em>
            </strong>
          ) : (
            <strong>选择需要先完成的任务</strong>
          )}
        </div>
        <span>选择</span>
      </button>

      <Modal
        className="requirements-dependency-picker-modal"
        title="选择上游依赖任务"
        open={open}
        width={900}
        zIndex={1700}
        destroyOnHidden
        onCancel={() => setOpen(false)}
        footer={[
          <Button key="cancel" onClick={() => setOpen(false)}>
            取消
          </Button>,
          <Button key="confirm" type="primary" onClick={confirmSelection}>
            确定
          </Button>
        ]}
      >
        <div className="requirements-dependency-picker">
          <Input.Search
            allowClear
            value={keyword}
            placeholder="搜索需求名称"
            onChange={(event) => setKeyword(event.target.value)}
            onSearch={setKeyword}
          />
          <div className="requirements-dependency-picker__pool-status">
            <span>
              已加载 {Math.min(queriedTasks.length, dependencyTotal)} / {dependencyTotal} 个候选任务
            </span>
            {dependencyHasMore ? (
              <Button
                size="small"
                type="text"
                loading={loadingMoreDependencyTasks}
                onClick={() => void loadMoreDependencyTasks()}
              >
                加载更多
              </Button>
            ) : queriedTasks.length ? (
              <em>已全部加载</em>
            ) : null}
          </div>
          <div className="requirements-dependency-picker__body">
            <aside className="requirements-dependency-picker__requirements">
              {dependencyTasksQuery.isLoading && !filteredGroups.length ? (
                <Skeleton active paragraph={{ rows: 5 }} title={false} />
              ) : filteredGroups.length ? (
                filteredGroups.map((group) => {
                  const selectedCount = group.tasks.filter((task) =>
                    draftSelected.includes(task.id)
                  ).length;
                  return (
                    <button
                      type="button"
                      key={group.id}
                      className={group.id === activeGroup?.id ? "is-active" : ""}
                      onClick={() => setActiveRequirementId(group.id)}
                    >
                      <strong title={group.title}>{group.title}</strong>
                      <span>
                        {group.tasks.length} 个任务{selectedCount ? ` · 已选 ${selectedCount}` : ""}
                      </span>
                    </button>
                  );
                })
              ) : (
                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="没有匹配的需求" />
              )}
            </aside>
            <section className="requirements-dependency-picker__tasks">
              {dependencyTasksQuery.isLoading && !activeGroup ? (
                <div className="requirements-dependency-picker__loading">
                  <Skeleton active paragraph={{ rows: 5 }} title={false} />
                </div>
              ) : activeGroup ? (
                <>
                  <div className="requirements-dependency-picker__tasks-head">
                    <div>
                      <strong title={activeGroup.title}>{activeGroup.title}</strong>
                      <span>{activeGroup.tasks.length} 个任务</span>
                    </div>
                    <Button
                      size="small"
                      disabled={!draftSelected.length}
                      onClick={() => setDraftSelected([])}
                    >
                      清空选择
                    </Button>
                  </div>
                  <div className="requirements-dependency-picker__task-list">
                    {activeGroup.tasks.map((task) => {
                      const checked = draftSelected.includes(task.id);
                      return (
                        <div
                          className={`requirements-dependency-picker__task${checked ? " is-selected" : ""}`}
                          key={task.id}
                          role="button"
                          tabIndex={0}
                          onClick={() => toggleTask(task.id)}
                          onKeyDown={(event) => {
                            if (event.key === "Enter" || event.key === " ") {
                              event.preventDefault();
                              toggleTask(task.id);
                            }
                          }}
                        >
                          <Checkbox
                            checked={checked}
                            onClick={(event) => event.stopPropagation()}
                            onChange={() => toggleTask(task.id)}
                          />
                          <div>
                            <strong title={task.title}>{task.title}</strong>
                            <span>
                              {task.responsibleLabel || "未分配"} · 截止 {formatDate(task.dueDate)}
                            </span>
                          </div>
                          <Tag
                            color={dependencyTone({
                              item_type: "task",
                              item_id: task.id,
                              title: task.title,
                              status: task.status ?? "todo"
                            })}
                          >
                            {TASK_STATUS_META[task.status as MockTaskStatus]?.label ??
                              task.status ??
                              "-"}
                          </Tag>
                        </div>
                      );
                    })}
                  </div>
                </>
              ) : (
                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="请选择左侧需求" />
              )}
            </section>
          </div>
          <div className="requirements-dependency-picker__selected">
            <span>已选择 {draftSelectedTasks.length} 个任务</span>
            <div>
              {draftSelectedTasks.slice(0, 6).map((task) => (
                <Tag
                  key={task.id}
                  closable
                  onClose={(event) => {
                    event.preventDefault();
                    removeDraftTask(task.id);
                  }}
                >
                  {task.title}
                </Tag>
              ))}
              {draftSelectedTasks.length > 6 ? <em>+{draftSelectedTasks.length - 6}</em> : null}
            </div>
          </div>
        </div>
      </Modal>
    </>
  );
}

export function TaskDetailModal({
  task,
  dependencyTasks,
  tokenSourceMap,
  isFavorite,
  canManage,
  onToggleFavorite,
  canGoBack,
  onBackTask,
  onClose,
  onSaved,
  onDeleted,
  onOpenTask
}: {
  task?: MockTask;
  dependencyTasks: MockTask[];
  tokenSourceMap: Map<string, MockTokenSource>;
  isFavorite: boolean;
  canManage: boolean;
  onToggleFavorite?: () => void;
  canGoBack: boolean;
  onBackTask: () => void;
  onClose: () => void;
  onSaved: (task: MockTask) => void;
  onDeleted: () => void;
  onOpenTask: (task: MockTask) => void;
}) {
  return (
    <Modal
      className="requirements-task-detail-modal"
      wrapClassName="requirements-task-detail-modal-wrap"
      width={1040}
      zIndex={1300}
      open={Boolean(task)}
      onCancel={onClose}
      footer={null}
      destroyOnHidden
      style={{ top: 36 }}
      styles={{
        body: {
          maxHeight: "calc(100vh - 132px)",
          overflowY: "auto",
          padding: "14px 18px 18px"
        }
      }}
      title={
        task ? (
          <div className="requirements-task-detail-modal__title">
            {canGoBack ? (
              <button
                type="button"
                className="requirements-task-detail-modal__back"
                aria-label="返回上一个任务"
                onClick={onBackTask}
              >
                <RollbackOutlined />
                <span>返回</span>
              </button>
            ) : null}
            {onToggleFavorite ? (
              <button
                type="button"
                className={`requirements-task-detail-modal__favorite${isFavorite ? " is-active" : ""}`}
                aria-label={isFavorite ? "取消关注" : "关注任务"}
                onClick={onToggleFavorite}
              >
                {isFavorite ? <StarFilled style={{ color: "#f59e0b" }} /> : <StarOutlined />}
              </button>
            ) : null}
            <strong title={task.title}>{task.title}</strong>
          </div>
        ) : (
          "任务详情"
        )
      }
    >
      {task ? (
        <TaskDrawerContent
          key={`${task.id}-${task.updated_at}`}
          task={task}
          dependencyTasks={dependencyTasks}
          tokenSourceMap={tokenSourceMap}
          canManage={canManage}
          onSaved={onSaved}
          onDeleted={onDeleted}
          onOpenTask={onOpenTask}
        />
      ) : null}
    </Modal>
  );
}

function TaskDrawerContent({
  task,
  dependencyTasks,
  tokenSourceMap,
  canManage,
  onSaved,
  onDeleted,
  onOpenTask
}: {
  task: MockTask;
  dependencyTasks: MockTask[];
  tokenSourceMap: Map<string, MockTokenSource>;
  canManage: boolean;
  onSaved: (task: MockTask) => void;
  onDeleted: () => void;
  onOpenTask: (task: MockTask) => void;
}) {
  const { message, modal } = App.useApp();
  const queryClient = useQueryClient();
  const [progress, setProgress] = useState(task.progress);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const dependencyBlocked = isTaskBlocked(task);
  const taskEventsQuery = useQuery({
    queryKey: ["requirements-board", "task-events", task.id],
    queryFn: () => requirementsBoardApi.listTaskEvents(task.id),
    enabled: Boolean(task.id),
    staleTime: 0,
    refetchOnMount: "always"
  });
  const assigneesQuery = useQuery({
    queryKey: ["requirements-board", "assignees"],
    queryFn: () => requirementsBoardApi.listAssignees(),
    enabled: Boolean(task.id),
    staleTime: 5 * 60_000
  });
  const eventUserNameMap = useMemo(
    () => buildWorkItemEventUserNameMap(assigneesQuery.data ?? [], undefined, task),
    [assigneesQuery.data, task]
  );
  const invalidateTaskEvents = (taskId = task.id) => {
    void queryClient.invalidateQueries({
      queryKey: ["requirements-board", "task-events", taskId]
    });
    void queryClient.invalidateQueries({
      queryKey: ["requirements-board", "requirement-events", task.requirement_id]
    });
  };
  useEffect(() => {
    setProgress(task.progress);
  }, [task.id, task.progress]);
  const refreshAfterConflict = (error: unknown, fallback: string) => {
    if (handleEditConflict(error, message, queryClient)) return;
    message.error(error instanceof Error ? error.message : fallback);
  };
  const deleteMutation = useMutation({
    mutationFn: () => requirementsBoardApi.deleteTask(task.id, task.version),
    onSuccess: () => {
      const deletedTaskId = task.id;
      const requirementId = task.requirement_id;
      message.success("任务已删除");
      void queryClient.cancelQueries({
        queryKey: ["requirements-board", "task-events", deletedTaskId]
      });
      queryClient.removeQueries({
        queryKey: ["requirements-board", "task-events", deletedTaskId]
      });
      queryClient.removeQueries({
        queryKey: ["requirements-board", "task", deletedTaskId]
      });
      onDeleted();
      void queryClient.invalidateQueries({ queryKey: ["requirements-board", "requirements"] });
      void queryClient.invalidateQueries({ queryKey: ["requirements-board", "tasks"] });
      void queryClient.invalidateQueries({ queryKey: ["requirements-board", "token-sources"] });
      void queryClient.invalidateQueries({
        queryKey: ["requirements-board", "requirement-events", requirementId]
      });
      void queryClient.invalidateQueries({ queryKey: ["dashboard"] });
      void queryClient.invalidateQueries({ queryKey: ["tasks"] });
      void queryClient.invalidateQueries({ queryKey: ["follows"] });
      void queryClient.invalidateQueries({ queryKey: ["sessions"] });
    },
    onError: (error) => refreshAfterConflict(error, "任务删除失败")
  });
  const handleDelete = () => {
    modal.confirm({
      title: "确认删除任务？",
      content: "删除后会自动解绑相关 Session/Token/文档，并重算需求进度，操作不可恢复。",
      okText: "删除任务",
      okButtonProps: { danger: true },
      cancelText: "取消",
      zIndex: 1500,
      onOk: () => deleteMutation.mutateAsync()
    });
  };
  const progressMutation = useMutation({
    mutationFn: () => requirementsBoardApi.updateTaskProgress(task.id, progress, task.version),
    onSuccess: (updated) => {
      message.success("任务进度已保存");
      onSaved(updated);
      invalidateTaskEvents(updated.id);
      void invalidateRequirementTaskWorkspace(queryClient, {
        requirementId: updated.requirement_id,
        taskId: updated.id
      });
    },
    onError: (error) => refreshAfterConflict(error, "进度保存失败")
  });
  const statusMutation = useMutation({
    mutationFn: (next: MockTaskStatus) =>
      requirementsBoardApi.updateTaskStatus(task.id, next, task.version),
    onSuccess: (updated) => {
      message.success("任务状态已更新");
      onSaved(updated);
      invalidateTaskEvents(updated.id);
      void invalidateRequirementTaskWorkspace(queryClient, {
        requirementId: updated.requirement_id,
        taskId: updated.id
      });
    },
    onError: (error) => refreshAfterConflict(error, "状态更新失败")
  });
  const requestStatusChange = (next: MockTaskStatus) => {
    if (next === task.status) return;
    statusMutation.mutate(next);
  };
  const linkMutation = useMutation({
    mutationFn: (sourceIds: string[]) =>
      requirementsBoardApi.setTaskTokenSources(task.id, sourceIds, task.token_source_ids),
    onSuccess: (updated) => {
      message.success("已关联 session");
      onSaved(updated);
      invalidateTaskEvents(updated.id);
      void invalidateRequirementTaskWorkspace(queryClient, {
        requirementId: updated.requirement_id,
        taskId: updated.id
      });
      setPickerOpen(false);
    },
    onError: (error) => message.error(error instanceof Error ? error.message : "关联失败")
  });
  const linkedSources = task.token_source_ids
    .map((id) => tokenSourceMap.get(id))
    .filter((source): source is MockTokenSource => Boolean(source));
  const linkedTotal = linkedSources.reduce((total, source) => total + source.token, 0);
  const taskRiskItems = getTaskRiskBadges(task).map((risk) => {
    if (risk.key === "overdue") {
      return {
        ...risk,
        description: `截止日期 ${formatDate(task.due_date)}，请确认是否需要调整计划或重新分配。`
      };
    }
    if (risk.key === "blocked") {
      return {
        ...risk,
        description: task.dependencies.length
          ? "存在未完成的上游依赖，完成前不建议继续推进当前任务。"
          : "任务存在阻塞风险，请确认阻塞原因。"
      };
    }
    return {
      ...risk,
      description: "依赖关系存在异常，请检查上游工作项是否重复、失效或互相冲突。"
    };
  });
  const visibleRiskItems = dependencyBlocked
    ? taskRiskItems.filter((risk) => risk.key !== "blocked")
    : taskRiskItems;
  const progressDirty = progress !== task.progress;
  const statusSegmentOptions = [
    { label: "未开始", value: "todo" },
    { label: "进行中", value: "in_progress" },
    { label: "已完成", value: "done" }
  ];
  if (editOpen) {
    return (
      <TaskEditModal
        embedded
        open={editOpen}
        task={task}
        existingTasks={dependencyTasks}
        onCancel={() => setEditOpen(false)}
        onSaved={(updated) => {
          setEditOpen(false);
          onSaved(updated);
          invalidateTaskEvents(updated.id);
        }}
      />
    );
  }

  return (
    <div className="requirements-task-detail">
      <section className="requirements-task-detail__hero">
        <div className="requirements-task-detail__hero-head">
          <div className="requirements-task-detail__status">
            <TaskStatusPill status={task.status} />
            <PriorityPill priority={task.priority} />
            {dependencyBlocked ? (
              <BlockingDependencyTrace
                task={task}
                dependencyTasks={dependencyTasks}
                onOpenTask={onOpenTask}
                compact
              />
            ) : null}
          </div>
          {task.can_update_meta || task.can_delete ? (
            <div className="requirements-drawer__actions">
              {task.can_update_meta ? (
                <Button icon={<EditOutlined />} onClick={() => setEditOpen(true)}>
                  编辑
                </Button>
              ) : null}
              {task.can_delete ? (
                <Button danger icon={<DeleteOutlined />} onClick={handleDelete}>
                  删除任务
                </Button>
              ) : null}
            </div>
          ) : null}
        </div>

        <div className="requirements-task-detail__meta-strip">
          <div>
            <span>负责人</span>
            <strong title={getTaskResponsibleTitle(task)}>{getTaskResponsibleLabel(task)}</strong>
          </div>
          <div>
            <span>截止日期</span>
            <strong title={formatDate(task.due_date)}>{formatDate(task.due_date)}</strong>
          </div>
          <div>
            <span>关联 session</span>
            <div className="requirements-task-detail__session-meta">
              <strong
                title={
                  linkedSources.length
                    ? `已关联 ${linkedSources.length} 条，约 ${formatTokens(linkedTotal)} Token`
                    : "未关联工作记录"
                }
              >
                {linkedSources.length
                  ? `${linkedSources.length} 条 · ${formatTokens(linkedTotal)} Token`
                  : "未关联"}
              </strong>
              {canManage ? (
                <Button
                  type="text"
                  size="small"
                  icon={<LinkOutlined />}
                  aria-label="管理关联 session"
                  onClick={() => setPickerOpen(true)}
                />
              ) : null}
            </div>
          </div>
        </div>

        <div className="requirements-task-detail__progress-control">
          <div className="requirements-task-detail__progress-row">
            <div className="requirements-task-detail__progress-label">
              <span>任务进度</span>
              <strong>{progress}%</strong>
            </div>
            <Slider
              className="requirements-task-detail__progress-slider"
              min={0}
              max={100}
              value={progress}
              disabled={!task.can_update_progress}
              styles={{
                track: { backgroundImage: "linear-gradient(180deg, #91caff, #1677ff)" },
                handle: { borderColor: "#1677ff", boxShadow: "0 2px 8px rgba(22, 119, 255, 0.28)" }
              }}
              tooltip={{ formatter: (value) => `${value ?? 0}%` }}
              onChange={setProgress}
            />
            <InputNumber
              min={0}
              max={100}
              value={progress}
              addonAfter="%"
              disabled={!task.can_update_progress}
              onChange={(value) => setProgress(value ?? 0)}
            />
            <Button
              type="primary"
              loading={progressMutation.isPending}
              disabled={!task.can_update_progress || !progressDirty}
              onClick={() => progressMutation.mutate()}
            >
              保存
            </Button>
            {task.can_update_status ? (
              <div className="requirements-task-detail__status-switch">
                {statusSegmentOptions.map((option) => {
                  const active = task.status === option.value;
                  return (
                    <Button
                      key={option.value}
                      size="small"
                      type={active ? "primary" : "default"}
                      disabled={statusMutation.isPending || active}
                      onClick={() => requestStatusChange(option.value as MockTaskStatus)}
                    >
                      {option.label}
                    </Button>
                  );
                })}
              </div>
            ) : (
              <TaskStatusPill status={task.status} />
            )}
          </div>
        </div>
      </section>

      <section className="requirements-drawer__section requirements-task-detail__detail-panel">
        <div className="requirements-drawer__section-head">
          <h3>任务信息</h3>
          <span>{taskRiskItems.length ? `${taskRiskItems.length} 项风险` : "风险正常"}</span>
        </div>
        <div className="requirements-task-detail__field-list">
          <div
            className={`requirements-task-detail__field-row requirements-task-detail__field-row--risk${
              taskRiskItems.length ? " has-risk" : ""
            }`}
          >
            <span>风险</span>
            {taskRiskItems.length ? (
              <div className="requirements-task-detail__risk-list">
                {dependencyBlocked ? (
                  <BlockingDependencyTrace
                    task={task}
                    dependencyTasks={dependencyTasks}
                    onOpenTask={onOpenTask}
                  />
                ) : null}
                {visibleRiskItems.map((risk) => (
                  <div className={`is-${risk.tone}`} key={risk.key}>
                    <WarningOutlined />
                    <div>
                      <strong>{risk.label}</strong>
                      <span title={risk.description}>{risk.description}</span>
                    </div>
                  </div>
                ))}
              </div>
            ) : (
              <div className="requirements-task-detail__healthy-risk">
                <strong>正常</strong>
                <span>当前任务未命中逾期、阻塞或依赖冲突。</span>
              </div>
            )}
          </div>
          <div className="requirements-task-detail__field-row">
            <span>所属需求</span>
            <strong title={task.requirement_title}>{task.requirement_title}</strong>
          </div>
          <div className="requirements-task-detail__field-row">
            <span>上游依赖</span>
            <TaskDependencyList
              task={task}
              dependencyTasks={dependencyTasks}
              onOpenTask={onOpenTask}
            />
          </div>
          <div className="requirements-task-detail__field-row">
            <span>验收标准</span>
            {task.acceptance_criteria.length ? (
              <ol className="requirements-drawer__ac-list requirements-task-detail__criteria-list">
                {task.acceptance_criteria.map((item, index) => (
                  <li key={`${index}-${item}`} title={item}>
                    <span>AC {index + 1}</span>
                    {item}
                  </li>
                ))}
              </ol>
            ) : (
              <p className="requirements-task-detail__muted">暂无任务验收标准</p>
            )}
          </div>
        </div>
      </section>

      <section className="requirements-drawer__section requirements-task-detail__events-panel">
        <div className="requirements-drawer__section-head">
          <h3>操作记录</h3>
          <span>
            {taskEventsQuery.data?.total ? `共 ${taskEventsQuery.data.total} 条` : "操作留痕"}
          </span>
        </div>
        <WorkItemEventTimeline
          events={taskEventsQuery.data?.items ?? []}
          loading={taskEventsQuery.isLoading}
          emptyText="暂无任务操作记录"
          compact
          userNameMap={eventUserNameMap}
        />
      </section>

      <TokenSourcePicker
        open={pickerOpen}
        selectedIds={task.token_source_ids}
        confirmLoading={linkMutation.isPending}
        onCancel={() => setPickerOpen(false)}
        onConfirm={async (ids) => {
          await linkMutation.mutateAsync(ids);
        }}
      />
    </div>
  );
}

function TaskEditModal({
  embedded = false,
  open,
  task,
  existingTasks,
  onCancel,
  onSaved
}: {
  embedded?: boolean;
  open: boolean;
  task: MockTask;
  existingTasks: MockTask[];
  onCancel: () => void;
  onSaved: (task: MockTask) => void;
}) {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [form] = Form.useForm<{
    title: string;
    responsible_user_ids?: string[];
    priority: MockTaskPriority;
    due_date?: dayjs.Dayjs;
    dependency_task_ids?: string[];
    acceptance_criteria?: string[];
  }>();

  const assigneesQuery = useQuery({
    queryKey: ["requirements-board", "assignees"],
    queryFn: () => requirementsBoardApi.listAssignees(),
    staleTime: 5 * 60_000
  });
  const currentDependencyTaskIds = useMemo(
    () =>
      task.dependencies
        .filter((dependency) => getDependencyType(dependency) === "task")
        .map(getDependencyId)
        .filter(Boolean)
        .filter((dependencyId) => dependencyId !== task.id),
    [task.dependencies, task.id]
  );
  const initialValues = useMemo(
    () => ({
      title: task.title,
      responsible_user_ids: task.responsible_user_ids,
      priority: task.priority,
      due_date: task.due_date ? dayjs(task.due_date) : undefined,
      dependency_task_ids: currentDependencyTaskIds,
      acceptance_criteria: task.acceptance_criteria.length ? task.acceptance_criteria : [""]
    }),
    [currentDependencyTaskIds, task]
  );
  const canReassign = task.can_reassign === true;

  useEffect(() => {
    if (open) {
      form.setFieldsValue(initialValues);
    } else {
      form.resetFields();
    }
  }, [form, initialValues, open]);

  const updateMutation = useMutation({
    mutationFn: (values: {
      title: string;
      responsible_user_ids?: string[];
      priority: MockTaskPriority;
      due_date?: dayjs.Dayjs;
      dependency_task_ids?: string[];
      acceptance_criteria?: string[];
    }) => {
      const payload = {
        title: normalizeRequiredText(values.title),
        priority: values.priority,
        due_date: values.due_date ? values.due_date.format("YYYY-MM-DD") : undefined,
        acceptance_criteria: normalizeCriteria(values.acceptance_criteria),
        base_version: task.version,
        ...(canReassign ? { responsible_user_ids: values.responsible_user_ids ?? [] } : {})
      };
      return requirementsBoardApi.updateTask(task.id, payload).then(async (updatedTask) => {
        const selectedDependencyIds = Array.from(new Set(values.dependency_task_ids ?? []))
          .filter(Boolean)
          .filter((dependencyId) => dependencyId !== task.id);
        const selectedDependencySet = new Set(selectedDependencyIds);
        const currentDependencySet = new Set(currentDependencyTaskIds);
        let latestTask = updatedTask;

        for (const dependencyId of currentDependencyTaskIds) {
          if (!selectedDependencySet.has(dependencyId)) {
            latestTask = await requirementsBoardApi.removeTaskDependency(
              task.id,
              dependencyId,
              latestTask.version,
              "task"
            );
          }
        }
        for (const dependencyId of selectedDependencyIds) {
          if (!currentDependencySet.has(dependencyId)) {
            latestTask = await requirementsBoardApi.addTaskDependency(
              task.id,
              dependencyId,
              latestTask.version,
              "task"
            );
          }
        }

        return latestTask;
      });
    },
    onSuccess: (updated) => {
      message.success("任务已更新");
      void invalidateRequirementTaskWorkspace(queryClient, {
        requirementId: updated.requirement_id,
        taskId: updated.id
      });
      onSaved(updated);
    },
    onError: (error) => {
      if (handleEditConflict(error, message, queryClient)) return;
      message.error(error instanceof Error ? error.message : "任务更新失败");
    }
  });

  const formContent = (
    <Form
      form={form}
      layout="vertical"
      initialValues={initialValues}
      onFinish={(values) => updateMutation.mutate(values)}
    >
      <Form.Item label="任务标题" name="title" rules={titleRules("任务标题")}>
        <Input placeholder="任务标题" />
      </Form.Item>
      <div className="requirements-task-edit-panel__grid">
        <Form.Item label="负责人" name="responsible_user_ids" rules={requiredSelectRules("负责人")}>
          <Select
            mode="multiple"
            showSearch
            maxTagCount="responsive"
            optionFilterProp="label"
            placeholder="选择负责人"
            loading={assigneesQuery.isLoading}
            disabled={!canReassign || assigneesQuery.isLoading || assigneesQuery.isError}
            getPopupContainer={getTaskModalPopupContainer}
            classNames={{ popup: { root: "requirements-task-detail-popup" } }}
            options={(assigneesQuery.data ?? []).map((item: MockAssignee) => ({
              value: item.id,
              label: item.name
            }))}
          />
        </Form.Item>
        <Form.Item label="优先级" name="priority" rules={requiredSelectRules("优先级")}>
          <Select
            getPopupContainer={getTaskModalPopupContainer}
            classNames={{ popup: { root: "requirements-task-detail-popup" } }}
            options={[
              { value: "low", label: "低" },
              { value: "medium", label: "中" },
              { value: "high", label: "高" }
            ]}
          />
        </Form.Item>
        <Form.Item label="截止日期" name="due_date">
          <DatePicker
            getPopupContainer={getTaskModalPopupContainer}
            classNames={{ popup: { root: "requirements-task-detail-popup" } }}
            style={{ width: "100%" }}
          />
        </Form.Item>
      </div>
      <Form.Item label="上游依赖任务" name="dependency_task_ids" rules={dependencyArrayRules()}>
        <DependencyTaskPicker
          tasks={existingTasks}
          currentTaskId={task.id}
          dependencyFallbacks={task.dependencies}
        />
      </Form.Item>
      <Form.Item label="标准列表" name="acceptance_criteria" rules={acceptanceCriteriaRules()}>
        <AcceptanceCriteriaEditor placeholder="输入一条可验证的任务验收标准" />
      </Form.Item>
    </Form>
  );

  if (embedded) {
    if (!open) return null;
    return (
      <section className="requirements-task-edit-panel">
        <div className="requirements-task-edit-panel__head">
          <div className="requirements-modal-title">
            <strong>编辑任务</strong>
            <span>{task.title}</span>
          </div>
          <Button onClick={onCancel}>返回详情</Button>
        </div>
        {formContent}
        <div className="requirements-task-edit-panel__footer">
          <Button onClick={onCancel}>取消</Button>
          <Button type="primary" loading={updateMutation.isPending} onClick={() => form.submit()}>
            保存
          </Button>
        </div>
      </section>
    );
  }

  return (
    <Modal
      className="requirements-task-modal"
      title={`编辑任务 · ${task.title}`}
      open={open}
      width={520}
      destroyOnHidden
      onCancel={() => {
        if (updateMutation.isPending) return;
        onCancel();
      }}
      onOk={() => form.submit()}
      okText="保存"
      cancelText="取消"
      confirmLoading={updateMutation.isPending}
    >
      {formContent}
    </Modal>
  );
}
