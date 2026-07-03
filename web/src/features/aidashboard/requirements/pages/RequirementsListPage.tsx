import {
  AppstoreOutlined,
  CalendarOutlined,
  CloseOutlined,
  ClockCircleOutlined,
  DeleteOutlined,
  EditOutlined,
  FileTextOutlined,
  LinkOutlined,
  MoreOutlined,
  PlusOutlined,
  ReloadOutlined,
  RollbackOutlined,
  StarFilled,
  StarOutlined,
  StopOutlined,
  TeamOutlined,
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
  Space,
  Tabs,
  Table,
  Tag
} from "antd";
import type { TableProps } from "antd";
import dayjs from "dayjs";
import type { ReactNode } from "react";
import { useEffect, useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";

import { useAuth } from "@/shared/auth/authContext";
import { ROLE_LABELS, type User, type UserRole } from "@/shared/auth/types";
import { PagePanel } from "@/shared/components/PagePanel/PagePanel";
import { isEditConflict } from "@/shared/request/apiError";
import { appendSearch } from "@/shared/utils/urlQuery";

import { fetchSessionTokens } from "../../api/client";
import type { SessionTokens } from "../../api/types";
import { TaskStatusTag } from "../../dashboard/shared";
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
  MockTaskPriority,
  MockTaskStatus,
  MockTokenSource,
  RequirementPriority,
  RequirementStage
} from "../types";
import "./RequirementsBoard.css";

type BoardView = "board" | "tree";
type RiskFilter = "blocked";
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
    description: "最近 5 个",
    emptyText: "暂无最近完成需求",
    tone: "green"
  }
];

const COMPLETED_RECENT_DAYS = 7;
const COMPLETED_RECENT_LIMIT = 10;

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
  { value: "blocked", label: "上游阻塞" }
];

const EMPTY_REQUIREMENTS: MockRequirement[] = [];
const EMPTY_TASKS: MockTask[] = [];
const EMPTY_TOKEN_SOURCES: MockTokenSource[] = [];
const EMPTY_FAVORITES: MockFavorite[] = [];

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
  todo: { label: "待办", tone: "neutral" },
  in_progress: { label: "进行中", tone: "info" },
  done: { label: "完成", tone: "success" },
  blocked: { label: "阻塞", tone: "danger" }
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

function RequirementTeamCell({ requirement }: { requirement: MockRequirement }) {
  const teams = requirement.team_names.length ? requirement.team_names : ["未指定参与团队"];
  const visibleTeams = teams.slice(0, 2);
  const restCount = Math.max(0, teams.length - visibleTeams.length);
  const title = teams.join("、");

  return (
    <span className="requirements-tree__team" title={title}>
      {visibleTeams.map((team) => (
        <span key={team}>{team}</span>
      ))}
      {restCount ? <em>+{restCount}</em> : null}
    </span>
  );
}

function RequirementPriorityTag({ priority }: { priority: RequirementPriority }) {
  const meta = PRIORITY_META[priority];
  return <Tag color={meta.color}>{meta.label}</Tag>;
}

function formatDateTime(value?: string) {
  return value ? dayjs(value).format("YYYY-MM-DD HH:mm") : "-";
}

function formatRecentUpdate(value?: string) {
  if (!value) return "未更新";
  const days = dayjs().startOf("day").diff(dayjs(value).startOf("day"), "day");
  if (days <= 0) return "今天更新";
  if (days === 1) return "昨天更新";
  return `${days} 天前更新`;
}

function formatDate(value?: string) {
  return value ? dayjs(value).format("YYYY-MM-DD") : "未设置";
}

function getRequirementOwnerLabel(requirement: MockRequirement) {
  return requirement.owner_name || "未指定负责人";
}

function getRequirementTeamLabel(requirement: MockRequirement) {
  return requirement.team_names.length ? requirement.team_names.join("、") : "未指定参与团队";
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

function aggregateRequirementTokens(
  requirement: MockRequirement,
  requirementTasks: MockTask[],
  sourceMap: Map<string, MockTokenSource>
) {
  const reqLevel = sumTokensFromSources(requirement.token_source_ids, sourceMap);
  const taskLevel = requirementTasks.reduce(
    (total, task) => total + sumTokensFromSources(task.token_source_ids, sourceMap),
    0
  );
  return reqLevel + taskLevel;
}

function formatTokenSourceTime(value: string) {
  return dayjs(value).format("MM-DD HH:mm");
}

function realSessionId(session: SessionTokens) {
  return session.local_session_id || session.session_ref;
}

function sessionRowKey(session: SessionTokens) {
  return session.slice_key || `${session.session_id}:${session.activity_date || session.activity_start_at || session.started_at}`;
}

function formatSessionActivityRange(session: SessionTokens) {
  const start = session.activity_start_at || session.started_at;
  const end = session.activity_end_at;
  if (!end || end === start) return formatTokenSourceTime(start);
  return `${formatTokenSourceTime(start)} ~ ${formatTokenSourceTime(end)}`;
}

function formatEvidenceCount(value: number) {
  if (!value) return "暂无关联 session";
  return `关联 session ${formatTokens(value)} Token`;
}

function getQuickEditPopupContainer(triggerNode: HTMLElement) {
  return triggerNode.parentElement ?? document.body;
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
  const { message } = App.useApp();
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
  const [creatorOpen, setCreatorOpen] = useState(false);

  const viewParam = searchParams.get("view");
  const view: BoardView = viewParam === "tree" || viewParam === "list" ? "tree" : "board";
  const keyword = searchParams.get("keyword") ?? "";
  const priority = (searchParams.get("priority") as RequirementPriority | null) ?? undefined;
  const status = (searchParams.get("status") as RequirementStage | null) ?? undefined;
  const riskParam = searchParams.get("risk");
  const risk: RiskFilter | undefined = riskParam === "blocked" ? "blocked" : undefined;
  const onlyFavorite = searchParams.get("favorite") === "1";

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

  const requirementsQuery = useQuery({
    queryKey: ["requirements-board", "requirements"],
    queryFn: () => requirementsBoardApi.listRequirements(),
    staleTime: 60_000
  });
  const tasksQuery = useQuery({
    queryKey: ["requirements-board", "tasks"],
    queryFn: () => requirementsBoardApi.listTasks(),
    staleTime: 30_000
  });
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
    onMutate: async ({ id, nextStatus }) => {
      const queryKey = ["requirements-board", "requirements"] as const;
      await queryClient.cancelQueries({ queryKey });
      const previous = queryClient.getQueryData<MockRequirement[]>(queryKey);
      queryClient.setQueryData<MockRequirement[]>(queryKey, (current = []) =>
        current.map((item) => (item.id === id ? { ...item, status: nextStatus } : item))
      );
      return { previous };
    },
    onError: (error, _variables, context) => {
      if (context?.previous) {
        queryClient.setQueryData(["requirements-board", "requirements"], context.previous);
      }
      if (handleEditConflict(error, message, queryClient)) return;
      message.error(error instanceof Error ? error.message : "需求阶段更新失败");
    },
    onSuccess: (updated, variables) => {
      const shouldPromptOwner = Boolean(variables.promptOwnerAfterSave && !updated.owner_id);
      if (!shouldPromptOwner) {
        message.success("需求阶段已更新");
      }
      queryClient.setQueryData<MockRequirement[]>(
        ["requirements-board", "requirements"],
        (current = []) => current.map((item) => (item.id === updated.id ? updated : item))
      );
      if (selectedRequirement?.id === updated.id) {
        setSelectedRequirement(updated);
      }
      if (shouldPromptOwner) {
        setOwnerPromptRequirement(updated);
      }
    }
  });

  const requirements = requirementsQuery.data ?? EMPTY_REQUIREMENTS;
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

  const navigationTask = tasks.find((item) => item.id === searchParams.get("taskId"));
  const navigationRequirement = navigationTask
    ? undefined
    : requirements.find((item) => item.id === searchParams.get("requirementId"));
  const selectedRequirementFromLatest = selectedRequirement
    ? (requirements.find((item) => item.id === selectedRequirement.id) ?? selectedRequirement)
    : undefined;
  const selectedTaskFromLatest = selectedTask
    ? (tasks.find((item) => item.id === selectedTask.id) ?? selectedTask)
    : undefined;
  const activeRequirement = selectedRequirementFromLatest ?? navigationRequirement;
  const activeTask = selectedTaskFromLatest ?? navigationTask;

  const clearNavigationTarget = () => {
    setSearchParams(
      (previous) => {
        const next = new URLSearchParams(previous);
        next.delete("requirementId");
        next.delete("taskId");
        return next;
      },
      { replace: true }
    );
  };

  const filteredRequirements = useMemo(() => {
    const normalizedKeyword = keyword.trim().toLowerCase();
    return requirements.filter((requirement) => {
      const requirementTasks = tasksByRequirement.get(requirement.id) ?? [];
      const searchContent = [
        requirement.title,
        requirement.description,
        requirement.creator_name,
        requirement.owner_name ?? "",
        ...requirement.team_names,
        ...requirement.acceptance_criteria,
        ...requirementTasks.flatMap((task) => [task.title, task.assignee_name ?? ""])
      ]
        .join(" ")
        .toLowerCase();
      const blocked = requirementTasks.some((task) => task.status === "blocked");
      const riskMatched = !risk || (risk === "blocked" && blocked);
      const statusMatched = status
        ? requirement.status === status
        : requirement.status !== "cancelled";
      const favoriteMatched =
        !onlyFavorite ||
        favoriteRequirementIds.has(requirement.id) ||
        requirementTasks.some((task) => favoriteTaskIds.has(task.id));
      return (
        (!normalizedKeyword || searchContent.includes(normalizedKeyword)) &&
        (!priority || requirement.priority === priority) &&
        statusMatched &&
        riskMatched &&
        favoriteMatched
      );
    });
  }, [
    favoriteRequirementIds,
    favoriteTaskIds,
    keyword,
    onlyFavorite,
    priority,
    requirements,
    risk,
    status,
    tasksByRequirement
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
        value: requirements.filter((item) => item.status !== "cancelled").length,
        description: "当前需要持续跟进",
        tone: "primary",
        icon: <UnorderedListOutlined />
      },
      {
        key: "review",
        title: "待确认",
        value: requirements.filter((item) => item.status === "review").length,
        description: "范围或验收标准待定",
        tone: "warning",
        icon: <FileTextOutlined />
      },
      {
        key: "active",
        title: "推进中",
        value: requirements.filter((item) => item.status === "active").length,
        description: "已有任务进入执行",
        tone: "info",
        icon: <ClockCircleOutlined />
      },
      {
        key: "blocked",
        title: "阻塞任务",
        value: tasks.filter((item) => item.status === "blocked").length,
        description: "上游未完成，需处理",
        tone: "danger",
        icon: <WarningOutlined />
      }
    ],
    [requirements, tasks]
  );

  const visibleColumns = status === "cancelled" ? [CANCELLED_COLUMN] : STATUS_COLUMNS;
  const shouldPromptOwnerAfterStatus = (requirement: MockRequirement, nextStatus: RequirementStage) =>
    !requirement.owner_id && ["review", "active", "completed"].includes(nextStatus);

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
    if (!canMoveRequirement || requirement.status === nextStatus || nextStatus === "cancelled") return;
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

  const addTask = (requirementId: string) => {
    const target = requirements.find((item) => item.id === requirementId);
    if (!target) return;
    setSelectedRequirement(target);
    setSelectedTask(undefined);
    setCreatorOpen(true);
  };

  const refreshAll = () =>
    void Promise.all([
      requirementsQuery.refetch(),
      tasksQuery.refetch(),
      tokenSourcesQuery.refetch()
    ]);
  const allExpanded =
    filteredRequirements.length > 0 && filteredRequirements.every((item) => expanded.has(item.id));
  const hasActiveFilters = Boolean(keyword || searchDraft || priority || status || risk || onlyFavorite);
  const resetFilters = () => {
    setSearchDraft("");
    setSearchParams(view === "board" ? {} : { view }, { replace: true });
  };

  return (
    <PagePanel
      title="需求推进"
      className="requirements-board-page"
      description="跟踪需求阶段、任务拆解、阻塞与关联 session"
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
            loading={requirementsQuery.isLoading || tasksQuery.isLoading}
          />
        ))}
      </RequirementMetricGrid>

      <section className="requirements-board__workspace">
        <div className="requirements-board__workspace-head">
          <div className="requirements-board__workspace-title">
            <div>
              <h2>{view === "board" ? "阶段看板" : "需求列表"}</h2>
              <p>
                {view === "board"
                  ? "按阶段推进需求，快速识别阻塞、截止和拆解状态。"
                  : "按需求查看阶段、任务进度、风险和截止时间。"}
              </p>
            </div>
            <div className="requirements-board__workspace-actions">
              <Segmented
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

          <div className="requirements-board__toolbar">
            <div className="requirements-board__filter-controls">
              <Input.Search
                allowClear
                value={searchDraft}
                placeholder="搜索需求、任务或负责人"
                onChange={(event) => setSearchDraft(event.target.value)}
                onSearch={(value) => updateParam("keyword", value.trim() || undefined)}
              />
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
                className={`requirements-board__filter-chip${onlyFavorite ? " is-active" : ""}`}
                icon={onlyFavorite ? <StarFilled /> : <StarOutlined />}
                onClick={() => updateParam("favorite", onlyFavorite ? undefined : "1")}
              >
                关注
              </Button>
            </div>
            <div className="requirements-board__toolbar-utilities">
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
              {hasActiveFilters ? (
                <Button className="requirements-board__utility-action" type="text" onClick={resetFilters}>
                  清除筛选
                </Button>
              ) : null}
              <Button
                className="requirements-board__refresh-action"
                aria-label="刷新"
                icon={<ReloadOutlined />}
                loading={requirementsQuery.isFetching || tasksQuery.isFetching}
                onClick={refreshAll}
              />
            </div>
          </div>
        </div>

        {requirementsQuery.isError || tasksQuery.isError ? (
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
          {requirementsQuery.isLoading ? (
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
                  const isCompletedColumn = column.value === "completed" && status !== "completed";
                  const recentCompleted = isCompletedColumn
                    ? allColumnRequirements
                        .filter(
                          (item) =>
                            dayjs().diff(dayjs(item.updated_at), "day") < COMPLETED_RECENT_DAYS
                        )
                        .sort((a, b) => dayjs(b.updated_at).diff(dayjs(a.updated_at)))
                        .slice(0, COMPLETED_RECENT_LIMIT)
                    : null;
                  const columnRequirements = recentCompleted ?? allColumnRequirements;
                  const hiddenCompletedCount = isCompletedColumn
                    ? allColumnRequirements.length - columnRequirements.length
                    : 0;
                  const headerCountBadge = isCompletedColumn
                    ? allColumnRequirements.length
                    : columnRequirements.length;
                  const completedShownCount = columnRequirements.length;
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
                            {isCompletedColumn ? (
                              hiddenCompletedCount > 0 ? (
                                <button
                                  type="button"
                                  className="requirements-board__column-head-link"
                                  onClick={() =>
                                    setSearchParams(
                                      (previous) => {
                                        const nextParams = new URLSearchParams(previous);
                                        nextParams.set("status", "completed");
                                        nextParams.set("view", "tree");
                                        return nextParams;
                                      },
                                      { replace: true }
                                    )
                                  }
                                >
                                  查看全部
                                </button>
                              ) : (
                                <span className="requirements-board__column-head-meta">
                                  最近 {completedShownCount} 个
                                </span>
                              )
                            ) : null}
                          </header>
                          <div className="requirements-board__card-list">
                            {columnRequirements.map((requirement, index) => (
                              <RequirementCard
                                key={requirement.id}
                                requirement={requirement}
                                tasks={tasksByRequirement.get(requirement.id) ?? []}
                                tokenSourceMap={tokenSourceMap}
                                index={index}
                                draggable={canMoveRequirement && column.value !== "cancelled"}
                                isCompletedColumn={column.value === "completed"}
                                isFavorite={favoriteRequirementIds.has(requirement.id)}
                                onToggleFavorite={() => toggleRequirementFavorite(requirement.id)}
                                onOpen={() => setSelectedRequirement(requirement)}
                              />
                            ))}
                            {provided.placeholder}
                            {!columnRequirements.length ? (
                              <div className="requirements-board__column-empty">
                                {column.emptyText}
                              </div>
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
              expanded={expanded}
              tasksByRequirement={tasksByRequirement}
              tokenSourceMap={tokenSourceMap}
              favoriteRequirementIds={favoriteRequirementIds}
              favoriteTaskIds={favoriteTaskIds}
              onToggle={toggleRequirement}
              onOpenRequirement={setSelectedRequirement}
              onOpenTask={(task) => setSelectedTask(task)}
              onAddTask={addTask}
              canUpdateRequirementStatus={canMoveRequirement}
              onUpdateRequirementStatus={updateRequirementStatus}
              onToggleRequirementFavorite={toggleRequirementFavorite}
              onToggleTaskFavorite={toggleTaskFavorite}
            />
          )}
        </div>
      </section>

      <RequirementDrawer
        requirement={activeRequirement}
        tasks={activeRequirement ? (tasksByRequirement.get(activeRequirement.id) ?? []) : []}
        tokenSources={tokenSources}
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
          clearNavigationTarget();
        }}
        onSaved={(updated) => setSelectedRequirement(updated)}
        onOpenTask={(task) => setSelectedTask(task)}
      />
      <TaskDrawer
        task={activeTask}
        requirementTasks={
          activeTask ? (tasksByRequirement.get(activeTask.requirement_id) ?? []) : []
        }
        tokenSources={tokenSources}
        tokenSourceMap={tokenSourceMap}
        isFavorite={activeTask ? favoriteTaskIds.has(activeTask.id) : false}
        canManage={canManageTaskForUser(user, activeTask)}
        onToggleFavorite={activeTask ? () => toggleTaskFavorite(activeTask.id) : undefined}
        onClose={() => {
          setSelectedTask(undefined);
          clearNavigationTarget();
        }}
        onSaved={(updated) => setSelectedTask(updated)}
        onDeleted={() => {
          setSelectedTask(undefined);
          clearNavigationTarget();
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
  tokenSourceMap,
  index,
  draggable,
  isCompletedColumn,
  isFavorite,
  onToggleFavorite,
  onOpen
}: {
  requirement: MockRequirement;
  tasks: MockTask[];
  tokenSourceMap: Map<string, MockTokenSource>;
  index: number;
  draggable: boolean;
  isCompletedColumn: boolean;
  isFavorite: boolean;
  onToggleFavorite: () => void;
  onOpen: () => void;
}) {
  const blockedTasks = tasks.filter((task) => task.status === "blocked").length;
  const completedTasks = tasks.filter((task) => task.status === "done").length;
  const tokenTotal = aggregateRequirementTokens(requirement, tasks, tokenSourceMap);
  const ownerLine = getRequirementOwnerLabel(requirement);
  const teamLine = getRequirementTeamLabel(requirement);
  const taskProgressLabel = tasks.length
    ? `${completedTasks}/${tasks.length} 个任务完成`
    : "待拆解";
  const evidenceLabel = tokenTotal > 0 ? `${formatTokens(tokenTotal)} Token` : "无关联 session";
  const showRiskRow = !isCompletedColumn && blockedTasks > 0;
  const dateLabel = isCompletedColumn
    ? `完成 ${formatDate(requirement.updated_at)}`
    : formatDate(requirement.deadline);
  const primaryRisk = blockedTasks ? `${blockedTasks} 个任务被上游阻塞` : undefined;

  return (
    <Draggable draggableId={requirement.id} index={index} isDragDisabled={!draggable}>
      {(provided, snapshot) => (
        <article
          className={`requirements-board__card${snapshot.isDragging ? " is-dragging" : ""}${
            blockedTasks ? " has-blocked" : ""
          }`}
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
                {isFavorite ? <StarFilled style={{ color: "#f59e0b" }} /> : <StarOutlined />}
              </button>
            </div>
          </div>

          {showRiskRow ? (
            <div className="requirements-board__card-risks">
              {primaryRisk ? <strong>{primaryRisk}</strong> : null}
              {blockedTasks && primaryRisk !== `${blockedTasks} 个任务被上游阻塞` ? (
                <Tag color="error">{blockedTasks} 个上游阻塞</Tag>
              ) : null}
            </div>
          ) : null}

          <div className="requirements-board__card-progress-block">
            <div>
              <span>推进进度</span>
              <strong title={taskProgressLabel}>{taskProgressLabel}</strong>
            </div>
            {tasks.length ? <RequirementProgress value={requirement.progress} /> : null}
          </div>

          <footer className="requirements-board__card-meta">
            <span title={`负责人：${ownerLine}；参与团队：${teamLine}`}>
              <UserOutlined /> {ownerLine}
            </span>
            <span title={dateLabel}>
              <CalendarOutlined /> {dateLabel}
            </span>
            <span title={formatEvidenceCount(tokenTotal)}>
              <FileTextOutlined /> {evidenceLabel}
            </span>
          </footer>
        </article>
      )}
    </Draggable>
  );
}

function RequirementTree({
  requirements,
  expanded,
  tasksByRequirement,
  tokenSourceMap,
  favoriteRequirementIds,
  favoriteTaskIds,
  onToggle,
  onOpenRequirement,
  onOpenTask,
  onAddTask,
  canUpdateRequirementStatus,
  onUpdateRequirementStatus,
  onToggleRequirementFavorite,
  onToggleTaskFavorite
}: {
  requirements: MockRequirement[];
  expanded: Set<string>;
  tasksByRequirement: Map<string, MockTask[]>;
  tokenSourceMap: Map<string, MockTokenSource>;
  favoriteRequirementIds: Set<string>;
  favoriteTaskIds: Set<string>;
  onToggle: (id: string) => void;
  onOpenRequirement: (requirement: MockRequirement) => void;
  onOpenTask: (task: MockTask) => void;
  onAddTask: (requirementId: string) => void;
  canUpdateRequirementStatus: boolean;
  onUpdateRequirementStatus: (requirement: MockRequirement, nextStatus: RequirementStage) => void;
  onToggleRequirementFavorite: (requirementId: string) => void;
  onToggleTaskFavorite: (taskId: string) => void;
}) {
  const columns: TableProps<MockRequirement>["columns"] = [
    {
      title: "需求",
      key: "title",
      width: 300,
      ellipsis: true,
      render: (_, requirement) => (
        <strong className="requirements-tree__title" title={requirement.title}>
          {requirement.title}
        </strong>
      )
    },
    {
      title: "负责人",
      key: "owner",
      width: 120,
      ellipsis: true,
      render: (_, requirement) => (
        <span className="requirements-tree__text" title={getRequirementOwnerLabel(requirement)}>
          {getRequirementOwnerLabel(requirement)}
        </span>
      )
    },
    {
      title: "参与团队",
      key: "teams",
      width: 150,
      ellipsis: true,
      render: (_, requirement) => <RequirementTeamCell requirement={requirement} />
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
            <strong>{requirementTasks.length ? `${doneTasks}/${requirementTasks.length} 已完成` : "待拆解"}</strong>
          </div>
        );
      }
    },
    {
      title: "风险",
      key: "risk",
      width: 112,
      render: (_, requirement) => {
        const requirementTasks = tasksByRequirement.get(requirement.id) ?? [];
        const blockedTasks = requirementTasks.filter((task) => task.status === "blocked").length;
        if (!blockedTasks) return <span className="requirements-tree__risk">正常</span>;
        return <span className="requirements-tree__risk is-danger">阻塞 {blockedTasks} 个</span>;
      }
    },
    {
      title: "截止",
      key: "deadline",
      width: 96,
      render: (_, requirement) => (
        <span className="requirements-tree__text">{formatDate(requirement.deadline)}</span>
      )
    },
    {
      title: "更新",
      key: "updated",
      width: 96,
      render: (_, requirement) => (
        <span className="requirements-tree__text">{formatRecentUpdate(requirement.updated_at)}</span>
      )
    },
    {
      title: "操作",
      key: "actions",
      width: 128,
      align: "right",
      render: (_, requirement) => {
        const requirementTasks = tasksByRequirement.get(requirement.id) ?? [];
        return (
          <Space size={2} className="requirements-tree__actions">
            {!requirementTasks.length && requirement.can_create_task ? (
              <Button
                className="requirements-tree__action-primary"
                size="small"
                type="link"
                onClick={(event) => {
                  event.stopPropagation();
                  onAddTask(requirement.id);
                }}
              >
                添加任务
              </Button>
            ) : null}
            <Button
              className={`requirements-tree__action-follow${
                favoriteRequirementIds.has(requirement.id) ? " is-active" : ""
              }`}
              size="small"
              type="link"
              onClick={(event) => {
                event.stopPropagation();
                onToggleRequirementFavorite(requirement.id);
              }}
            >
              {favoriteRequirementIds.has(requirement.id) ? "已关注" : "关注"}
            </Button>
            {canUpdateRequirementStatus && requirement.status !== "cancelled" ? (
              <Dropdown
                menu={{
                  items: STATUS_COLUMNS.map((item) => ({
                    key: item.value,
                    label: item.label,
                    disabled: item.value === requirement.status,
                    onClick: ({ domEvent }) => {
                      domEvent.stopPropagation();
                      onUpdateRequirementStatus(requirement, item.value);
                    }
                  }))
                }}
                trigger={["click"]}
              >
                <Button
                  size="small"
                  type="link"
                  onClick={(event) => event.stopPropagation()}
                >
                  修改进度
                </Button>
              </Dropdown>
            ) : null}
          </Space>
        );
      }
    }
  ];

  return (
    <Table<MockRequirement>
      className="requirements-tree"
      columns={columns}
      dataSource={requirements}
      pagination={false}
      rowKey="id"
      size="middle"
      scroll={{ x: 1080 }}
      onRow={(requirement) => ({
        onClick: () => onOpenRequirement(requirement)
      })}
      rowClassName={(requirement) => {
        const blockedTasks = (tasksByRequirement.get(requirement.id) ?? []).some(
          (task) => task.status === "blocked"
        );
        return blockedTasks ? "requirements-tree__table-row has-blocked" : "requirements-tree__table-row";
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
              {[...requirementTasks]
                .sort((a, b) => Number(b.status === "blocked") - Number(a.status === "blocked"))
                .map((task) => {
                  const taskTokens = sumTokensFromSources(task.token_source_ids, tokenSourceMap);
                  return (
                    <div
                      className={`requirements-tree__task-item${
                        task.status === "blocked" ? " has-blocked" : ""
                      }`}
                      key={task.id}
                      onClick={() => onOpenTask(task)}
                    >
                      <div className="requirements-tree__task-main">
                        <strong title={task.title}>{task.title}</strong>
                      </div>
                      <div className="requirements-tree__task-meta">
                        <TaskStatusPill status={task.status} />
                        <PriorityPill priority={task.priority} />
                      </div>
                      <div className="requirements-tree__task-progress">
                        <RequirementProgress value={task.progress} />
                      </div>
                      <div className="requirements-tree__task-detail">
                        <span>截止：{formatDate(task.due_date)}</span>
                        <span>{taskTokens > 0 ? formatEvidenceCount(taskTokens) : "暂无关联 session"}</span>
                      </div>
                      <Space size={2} className="requirements-tree__task-actions">
                        <Button
                          className="requirements-tree__action-secondary"
                          size="small"
                          type="link"
                          onClick={(event) => {
                            event.stopPropagation();
                            onOpenTask(task);
                          }}
                        >
                          详情
                        </Button>
                        <Button
                          className={`requirements-tree__action-follow${
                            favoriteTaskIds.has(task.id) ? " is-active" : ""
                          }`}
                          size="small"
                          type="link"
                          onClick={(event) => {
                            event.stopPropagation();
                            onToggleTaskFavorite(task.id);
                          }}
                        >
                          {favoriteTaskIds.has(task.id) ? "已关注" : "关注"}
                        </Button>
                      </Space>
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
  excludeIds: string[];
  onCancel: () => void;
  onConfirm: (ids: string[]) => Promise<void> | void;
  confirmLoading?: boolean;
}

function TokenSourcePicker({
  open,
  excludeIds,
  onCancel,
  onConfirm,
  confirmLoading
}: TokenSourcePickerProps) {
  const [selected, setSelected] = useState<string[]>([]);
  const [selectedRows, setSelectedRows] = useState<Record<string, SessionTokens>>({});
  const [dateRange, setDateRange] = useState<[string, string]>();
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  const excludeSet = useMemo(() => new Set(excludeIds), [excludeIds]);
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
  const sessions = useMemo(() => sessionsQuery.data?.items ?? [], [sessionsQuery.data]);
  const available = useMemo(
    () => sessions.filter((source) => !excludeSet.has(source.session_id) && source.total_tokens > 0),
    [sessions, excludeSet]
  );
  const selectedTokenTotal = useMemo(
    () =>
      selected.reduce((total, id) => {
        const source = selectedRows[id];
        return total + (source?.total_tokens ?? 0);
      }, 0),
    [selected, selectedRows]
  );
  const selectedSessionIds = useMemo(
    () => Array.from(new Set(selected.map((id) => selectedRows[id]?.session_id).filter(Boolean))),
    [selected, selectedRows]
  );

  const columns: TableProps<SessionTokens>["columns"] = [
    {
      title: "Session",
      key: "record",
      width: 260,
      render: (_, source) => <span className="tokens-session-id">{realSessionId(source)}</span>
    },
    {
      title: "摘要",
      dataIndex: "summary",
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
      width: 100,
      align: "right" as const,
      render: (value: number) => <span className="tokens-total-cell">{formatTokens(value)}</span>
    },
    {
      title: "日期范围",
      dataIndex: "activity_start_at",
      width: 170,
      render: (_, source) => <span className="tokens-activity-range">{formatSessionActivityRange(source)}</span>
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
      width={780}
      onCancel={() => {
        setSelected([]);
        setSelectedRows({});
        setDateRange(undefined);
        setPage(1);
        setPageSize(10);
        onCancel();
      }}
      okText={selectedSessionIds.length ? `关联 ${selectedSessionIds.length} 条记录` : "关联记录"}
      okButtonProps={{ disabled: !selectedSessionIds.length, loading: confirmLoading }}
      cancelText="取消"
      onOk={async () => {
        await onConfirm(selectedSessionIds);
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
            setSelected([]);
            setSelectedRows({});
            setPage(1);
          }}
        />
      </div>
      <div className="requirements-session-modal__table tokens-table-card">
        <Table<SessionTokens>
        rowKey={sessionRowKey}
        size="small"
        columns={columns}
        dataSource={available}
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
          scroll={{ y: 360 }}
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
          本页可选 {available.length} 条
          {dateRange ? ` · ${dateRange[0]} 至 ${dateRange[1]}` : ""}
        </span>
        <strong>已选 {selectedSessionIds.length} 条 · {formatTokens(selectedTokenTotal)} Token</strong>
      </div>
    </Modal>
  );
}

function RequirementDrawer({
  requirement,
  tasks,
  tokenSources,
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
  tokenSources: MockTokenSource[];
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
  const [draftOwnerId, setDraftOwnerId] = useState<string>();
  const [draftTeamIds, setDraftTeamIds] = useState<string[]>([]);

  const invalidateBoard = (requirementId = requirement?.id) =>
    invalidateRequirementTaskWorkspace(queryClient, { requirementId });

  const assigneesQuery = useQuery({
    queryKey: ["requirements-board", "assignees"],
    queryFn: () => requirementsBoardApi.listAssignees(),
    enabled: Boolean(requirement?.can_update),
    staleTime: 5 * 60_000
  });
  const teamsQuery = useQuery({
    queryKey: ["requirements-board", "teams"],
    queryFn: () => requirementsBoardApi.listTeams(),
    enabled: Boolean(requirement?.can_update),
    staleTime: 5 * 60_000
  });

  const quickAssigneeOptions = useMemo(() => {
    const options = (assigneesQuery.data ?? []).map((assignee) => ({
      value: assignee.id,
      label: assignee.name
    }));
    if (requirement?.owner_id && !options.some((item) => item.value === requirement.owner_id)) {
      options.push({
        value: requirement.owner_id,
        label: requirement.owner_name ?? requirement.owner_id
      });
    }
    return options;
  }, [assigneesQuery.data, requirement?.owner_id, requirement?.owner_name]);
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
    setDraftOwnerId(target.owner_id);
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
    onSuccess: () => {
      message.success("需求已删除");
      void invalidateBoard();
      onClose();
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
      content: "取消后，该需求不会出现在主看板和 Dashboard 风险/关注中，但历史数据会保留，可后续恢复。",
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
  const blockedCount = tasks.filter((task) => task.status === "blocked").length;
  const canQuickEditRequirement = Boolean(requirement?.can_update);

  const quickUpdateMutation = useMutation({
    mutationFn: ({
      field,
      data
    }: {
      field: RequirementQuickEditField;
      data: {
        deadline?: string;
        owner_id?: string;
        clear_owner?: boolean;
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
      data: draftOwnerId ? { owner_id: draftOwnerId } : { clear_owner: true }
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
      requirementsBoardApi.linkRequirementTokenSources(requirement!.id, sourceIds),
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
        getPopupContainer={getQuickEditPopupContainer}
        disabled={quickUpdateMutation.isPending && quickUpdateMutation.variables?.field === "deadline"}
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
        showSearch
        value={draftOwnerId}
        loading={assigneesQuery.isLoading}
        placeholder={assigneesQuery.isError ? "负责人加载失败" : "选择负责人"}
        optionFilterProp="label"
        options={quickAssigneeOptions}
        getPopupContainer={getQuickEditPopupContainer}
        onChange={setDraftOwnerId}
      />
      <div className="requirements-quick-edit__actions">
        <Button size="small" onClick={closeQuickEditor}>
          取消
        </Button>
        <Button
          size="small"
          type="primary"
          loading={quickUpdateMutation.isPending && quickUpdateMutation.variables?.field === "owner"}
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
        getPopupContainer={getQuickEditPopupContainer}
        onChange={setDraftTeamIds}
      />
      <div className="requirements-quick-edit__actions">
        <Button size="small" onClick={closeQuickEditor}>
          取消
        </Button>
        <Button
          size="small"
          type="primary"
          loading={quickUpdateMutation.isPending && quickUpdateMutation.variables?.field === "teams"}
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
      size={720}
      open={Boolean(requirement)}
      onClose={onClose}
      title={
        requirement ? (
          <div className="requirements-drawer__title-row">
            <div className="requirements-drawer__title">
              <strong>{requirement.title}</strong>
            </div>
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
          </div>
        ) : null
      }
    >
      {requirement ? (
        <div className="requirements-drawer__content">
          <section className="requirements-drawer__summary">
            <div className="requirements-drawer__summary-head">
              <div className="requirements-drawer__summary-tags">
                <RequirementStageTag stage={requirement.status} />
                <RequirementPriorityTag priority={requirement.priority} />
                {blockedCount ? <Tag color="error">{blockedCount} 个上游阻塞</Tag> : null}
              </div>
              {canManage || canUpdateStatus ? (
                <div className="requirements-drawer__actions">
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
                    <Button type="primary" icon={<EditOutlined />} onClick={() => setEditOpen(true)}>
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
                </div>
              ) : null}
            </div>
            <p>{requirement.description || "暂无需求描述"}</p>
            <div className="requirements-drawer__summary-strip">
              <div className="requirements-drawer__summary-card">
                <span>推进进度</span>
                {tasks.length ? (
                  <RequirementProgress value={requirement.progress} />
                ) : (
                  <strong>待拆解</strong>
                )}
              </div>
              <div className="requirements-drawer__summary-card">
                <span>任务完成</span>
                <strong>{tasks.length ? `${completedCount}/${tasks.length}` : "0/0"}</strong>
              </div>
              <div className="requirements-drawer__summary-card">
                <div className="requirements-drawer__summary-card-head">
                  <span>截止日期</span>
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
                      <Button
                        className="requirements-drawer__summary-card-action"
                        type="text"
                        size="small"
                        icon={<EditOutlined />}
                        aria-label="设置截止日期"
                        onClick={(event) => event.stopPropagation()}
                      />
                    </Popover>
                  ) : null}
                </div>
                <strong>{formatDate(requirement.deadline)}</strong>
              </div>
            </div>
            <div className="requirements-drawer__assignment-strip">
              <div className="requirements-drawer__summary-card requirements-drawer__summary-card--owner">
                <div className="requirements-drawer__summary-card-head">
                  <span>负责人</span>
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
                      <Button
                        className="requirements-drawer__summary-card-action"
                        type="text"
                        size="small"
                        icon={<EditOutlined />}
                        aria-label="设置负责人"
                        onClick={(event) => event.stopPropagation()}
                      />
                    </Popover>
                  ) : null}
                </div>
                <strong>{getRequirementOwnerLabel(requirement)}</strong>
              </div>
              <div className="requirements-drawer__summary-card requirements-drawer__summary-card--teams">
                <div className="requirements-drawer__summary-card-head">
                  <span>参与团队</span>
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
                      <Button
                        className="requirements-drawer__summary-card-action"
                        type="text"
                        size="small"
                        icon={<EditOutlined />}
                        aria-label="设置参与团队"
                        onClick={(event) => event.stopPropagation()}
                      />
                    </Popover>
                  ) : null}
                </div>
                <strong>{getRequirementTeamLabel(requirement)}</strong>
              </div>
            </div>
          </section>

          <Tabs
            key={requirement.id}
            className="requirements-drawer__tabs"
            defaultActiveKey="tasks"
            items={[
              {
                key: "tasks",
                label: (
                  <span className="requirements-drawer__tab-label">
                    任务 <Badge size="small" count={tasks.length} />
                  </span>
                ),
                children: (
                  <section className="requirements-drawer__section requirements-drawer__task-section">
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
                            onClick={() => onCreatorOpenChange(true)}
                          >
                            添加任务
                          </Button>
                        ) : null}
                      </div>
                    </div>
                    {!tasks.length ? (
                      <div className="requirements-drawer__execution-empty">
                        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="尚未拆解任务" />
                      </div>
                    ) : (
                      <div className="requirements-drawer__task-list">
                        {[...tasks]
                          .sort(
                            (a, b) =>
                              Number(b.status === "blocked") - Number(a.status === "blocked")
                          )
                          .map((task) => {
                            const tTokens = sumTokensFromSources(
                              task.token_source_ids,
                              tokenSourceMap
                            );
                            return (
                              <button
                                key={task.id}
                                type="button"
                                className={`requirements-drawer__task-item${
                                  task.status === "blocked" ? " has-blocked" : ""
                                }`}
                                onClick={() => onOpenTask(task)}
                              >
                                <div>
                                  <strong>{task.title}</strong>
                                  <span>
                                    {task.assignee_name || "未分配"} · {formatDate(task.due_date)}
                                  </span>
                                </div>
                                <TaskStatusTag status={task.status} />
                                <RequirementProgress value={task.progress} />
                                <small>
                                  {task.dependencies.length
                                    ? `${task.dependencies.length} 个上游依赖`
                                    : "无上游依赖"}
                                  {tTokens > 0 ? ` · ${formatEvidenceCount(tTokens)}` : ""}
                                </small>
                              </button>
                            );
                          })}
                      </div>
                    )}
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
                      <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无需求验收标准" />
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
                key: "overview",
                label: "信息",
                children: (
                  <section className="requirements-drawer__section">
                    <h3>基础信息</h3>
                    <Descriptions column={1} size="small" colon={false}>
                      <Descriptions.Item label="创建者">
                        {requirement.creator_name}（
                        {ROLE_LABELS[requirement.creator_role as UserRole] ??
                          requirement.creator_role}
                        ）
                      </Descriptions.Item>
                      <Descriptions.Item label="参与团队">
                        {requirement.team_names.join("、") || "-"}
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
            ]}
          />

          <TaskCreateModal
            open={creatorOpen}
            requirementId={requirement.id}
            requirementTitle={requirement.title}
            existingTasks={tasks}
            onCancel={() => onCreatorOpenChange(false)}
            onCreated={() => onCreatorOpenChange(false)}
          />

          <TokenSourcePicker
            open={pickerOpen}
            excludeIds={requirement.token_source_ids}
            confirmLoading={linkMutation.isPending}
            onCancel={() => setPickerOpen(false)}
            onConfirm={async (ids) => {
              await linkMutation.mutateAsync(ids);
            }}
          />

          <RequirementEditModal
            open={editOpen}
            requirement={requirement}
            onCancel={() => setEditOpen(false)}
            onSaved={(updated) => {
              onSaved(updated);
              setEditOpen(false);
            }}
          />
        </div>
      ) : null}
    </Drawer>
  );
}

function RequirementEditModal({
  open,
  requirement,
  onCancel,
  onSaved
}: {
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
    owner_id?: string;
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
      owner_id: requirement.owner_id,
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
      owner_id?: string;
      team_ids?: string[];
      feishu_doc_url?: string;
      acceptance_criteria: string[];
    }) =>
      requirementsBoardApi.updateRequirement(requirement.id, {
        title: normalizeRequiredText(values.title),
        description: normalizeRequiredText(values.description),
        priority: values.priority,
        deadline: values.deadline ? values.deadline.format("YYYY-MM-DD") : undefined,
        owner_id: values.owner_id,
        clear_owner: !values.owner_id,
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
    if (requirement.owner_id && !options.some((item) => item.value === requirement.owner_id)) {
      options.push({
        value: requirement.owner_id,
        label: requirement.owner_name ?? requirement.owner_id
      });
    }
    return options;
  }, [assigneesQuery.data, requirement.owner_id, requirement.owner_name]);
  const handleOwnerChange = (ownerId?: string) => {
    if (!ownerId) return;
    const owner = assigneesQuery.data?.find((item) => item.id === ownerId);
    if (!owner?.team_id) return;
    const currentTeamIds = form.getFieldValue("team_ids") ?? [];
    if (!currentTeamIds.includes(owner.team_id)) {
      form.setFieldValue("team_ids", [...currentTeamIds, owner.team_id]);
    }
  };

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
      onCancel={() => {
        if (updateMutation.isPending) return;
        onCancel();
      }}
      onOk={() => form.submit()}
      okText="保存"
      cancelText="取消"
      confirmLoading={updateMutation.isPending}
    >
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
          <Form.Item label="负责人" name="owner_id">
            <Select
              allowClear
              showSearch
              loading={assigneesQuery.isLoading}
              placeholder={assigneesQuery.isError ? "负责人加载失败" : "可稍后指定"}
              optionFilterProp="label"
              onChange={handleOwnerChange}
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
    </Modal>
  );
}

function TaskCreateModal({
  open,
  requirementId,
  requirementTitle,
  existingTasks,
  onCancel,
  onCreated
}: {
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
    assignee_id: string;
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

  const dependencyOptions = existingTasks.map((task) => ({
    value: task.id,
    label: task.title
  }));

  const createMutation = useMutation({
    mutationFn: (values: {
      title: string;
      assignee_id: string;
      priority: MockTaskPriority;
      due_date?: dayjs.Dayjs;
      dependency_task_ids?: string[];
      acceptance_criteria?: string[];
    }) =>
      requirementsBoardApi.createTask({
        requirement_id: requirementId,
        title: normalizeRequiredText(values.title),
        acceptance_criteria: normalizeCriteria(values.acceptance_criteria),
        assignee_id: values.assignee_id,
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
      <Form
        className="requirements-task-modal__form"
        form={form}
        layout="vertical"
        initialValues={{ priority: "medium" }}
        onFinish={(values) => createMutation.mutate(values)}
      >
        <section className="requirements-task-modal__section">
          <h4>基本信息</h4>
          <Form.Item
            label="任务标题"
            name="title"
            rules={titleRules("任务标题")}
          >
            <Input placeholder="输入清晰、可交付的任务标题" />
          </Form.Item>
          <div className="requirements-task-modal__grid">
            <Form.Item
              label="负责人"
              name="assignee_id"
              rules={requiredSelectRules("负责人")}
            >
              <Select
                placeholder="选择负责人"
                loading={assigneesQuery.isLoading}
                disabled={assigneesQuery.isLoading || assigneesQuery.isError}
                options={(assigneesQuery.data ?? []).map((item: MockAssignee) => ({
                  value: item.id,
                  label: `${item.name} (${item.employee_id})`
                }))}
              />
            </Form.Item>
            <Form.Item
              label="优先级"
              name="priority"
              rules={requiredSelectRules("优先级")}
            >
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
          {dependencyOptions.length ? (
            <Form.Item label="上游依赖" name="dependency_task_ids" rules={dependencyArrayRules()}>
              <Select
                mode="multiple"
                placeholder="选择上游依赖任务"
                options={dependencyOptions}
                allowClear
              />
            </Form.Item>
          ) : (
            <p className="requirements-task-modal__hint">当前需求暂无可选上游任务。</p>
          )}
        </section>
        <section className="requirements-task-modal__section">
          <h4>验收标准</h4>
          <Form.Item label="标准列表" name="acceptance_criteria" rules={acceptanceCriteriaRules()}>
            <AcceptanceCriteriaEditor placeholder="输入一条可验证的任务验收标准" />
          </Form.Item>
        </section>
      </Form>
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

function TaskDrawer({
  task,
  requirementTasks,
  tokenSources,
  tokenSourceMap,
  isFavorite,
  canManage,
  onToggleFavorite,
  onClose,
  onSaved,
  onDeleted
}: {
  task?: MockTask;
  requirementTasks: MockTask[];
  tokenSources: MockTokenSource[];
  tokenSourceMap: Map<string, MockTokenSource>;
  isFavorite: boolean;
  canManage: boolean;
  onToggleFavorite?: () => void;
  onClose: () => void;
  onSaved: (task: MockTask) => void;
  onDeleted: () => void;
}) {
  return (
    <Drawer
      className="requirements-drawer"
      size={640}
      open={Boolean(task)}
      onClose={onClose}
      title={
        task ? (
          <div className="requirements-drawer__title-row">
            <div className="requirements-drawer__title">
              <strong>{task.title}</strong>
            </div>
            {onToggleFavorite ? (
              <button
                type="button"
                className={`requirements-drawer__favorite${isFavorite ? " is-active" : ""}`}
                aria-label={isFavorite ? "取消关注" : "关注任务"}
                onClick={onToggleFavorite}
              >
                {isFavorite ? <StarFilled style={{ color: "#f59e0b" }} /> : <StarOutlined />}
              </button>
            ) : null}
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
          requirementTasks={requirementTasks}
          tokenSources={tokenSources}
          tokenSourceMap={tokenSourceMap}
          canManage={canManage}
          onSaved={onSaved}
          onDeleted={onDeleted}
        />
      ) : null}
    </Drawer>
  );
}

function TaskDrawerContent({
  task,
  requirementTasks,
  tokenSources,
  tokenSourceMap,
  canManage,
  onSaved,
  onDeleted
}: {
  task: MockTask;
  requirementTasks: MockTask[];
  tokenSources: MockTokenSource[];
  tokenSourceMap: Map<string, MockTokenSource>;
  canManage: boolean;
  onSaved: (task: MockTask) => void;
  onDeleted: () => void;
}) {
  const { message, modal } = App.useApp();
  const queryClient = useQueryClient();
  const [progress, setProgress] = useState(task.progress);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [dependencyDraft, setDependencyDraft] = useState<string>();
  const dependencyBlocked = task.status === "blocked";
  const refreshAfterConflict = (error: unknown, fallback: string) => {
    if (handleEditConflict(error, message, queryClient)) return;
    message.error(error instanceof Error ? error.message : fallback);
  };
  const deleteMutation = useMutation({
    mutationFn: () => requirementsBoardApi.deleteTask(task.id, task.version),
    onSuccess: () => {
      message.success("任务已删除");
      void invalidateRequirementTaskWorkspace(queryClient, {
        requirementId: task.requirement_id,
        taskId: task.id
      });
      onDeleted();
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
      onOk: () => deleteMutation.mutateAsync()
    });
  };
  const progressMutation = useMutation({
    mutationFn: () => requirementsBoardApi.updateTaskProgress(task.id, progress, task.version),
    onSuccess: (updated) => {
      message.success("任务进度已保存");
      onSaved(updated);
      void invalidateRequirementTaskWorkspace(queryClient, {
        requirementId: updated.requirement_id,
        taskId: updated.id
      });
    },
    onError: (error) => refreshAfterConflict(error, "进度保存失败")
  });
  const statusMutation = useMutation({
    mutationFn: (next: Exclude<MockTaskStatus, "blocked">) =>
      requirementsBoardApi.updateTaskStatus(task.id, next, task.version),
    onSuccess: (updated) => {
      message.success("任务状态已更新");
      onSaved(updated);
      void invalidateRequirementTaskWorkspace(queryClient, {
        requirementId: updated.requirement_id,
        taskId: updated.id
      });
    },
    onError: (error) => refreshAfterConflict(error, "状态更新失败")
  });
  const addDependencyMutation = useMutation({
    mutationFn: (dependsOnId: string) =>
      requirementsBoardApi.addTaskDependency(task.id, dependsOnId, task.version),
    onSuccess: (updated) => {
      message.success("上游依赖已更新");
      setDependencyDraft(undefined);
      onSaved(updated);
      void invalidateRequirementTaskWorkspace(queryClient, {
        requirementId: updated.requirement_id,
        taskId: updated.id
      });
    },
    onError: (error) => refreshAfterConflict(error, "依赖更新失败")
  });
  const removeDependencyMutation = useMutation({
    mutationFn: (dependsOnId: string) =>
      requirementsBoardApi.removeTaskDependency(task.id, dependsOnId, task.version),
    onSuccess: (updated) => {
      message.success("上游依赖已移除");
      onSaved(updated);
      void invalidateRequirementTaskWorkspace(queryClient, {
        requirementId: updated.requirement_id,
        taskId: updated.id
      });
    },
    onError: (error) => refreshAfterConflict(error, "依赖移除失败")
  });
  const requestStatusChange = (next: Exclude<MockTaskStatus, "blocked">) => {
    if (next === "done" || next === "todo") {
      modal.confirm({
        title: next === "done" ? "确认标记完成？" : "确认重新打开？",
        content: "状态变化会同步影响需求的聚合进度。",
        okText: next === "done" ? "标记完成" : "重新打开",
        cancelText: "取消",
        onOk: () => statusMutation.mutateAsync(next)
      });
      return;
    }
    statusMutation.mutate(next);
  };
  const linkMutation = useMutation({
    mutationFn: (sourceIds: string[]) =>
      requirementsBoardApi.linkTaskTokenSources(task.id, sourceIds),
    onSuccess: (updated) => {
      message.success("已关联 session");
      onSaved(updated);
      void invalidateRequirementTaskWorkspace(queryClient, {
        requirementId: updated.requirement_id,
        taskId: updated.id
      });
      setPickerOpen(false);
    },
    onError: (error) => message.error(error instanceof Error ? error.message : "关联失败")
  });
  const unlinkMutation = useMutation({
    mutationFn: (sourceId: string) => requirementsBoardApi.unlinkTaskTokenSource(task.id, sourceId),
    onSuccess: (updated) => {
      message.success("已移除 session");
      onSaved(updated);
      void invalidateRequirementTaskWorkspace(queryClient, {
        requirementId: updated.requirement_id,
        taskId: updated.id
      });
    },
    onError: (error) => message.error(error instanceof Error ? error.message : "移除失败")
  });

  const linkedSources = task.token_source_ids
    .map((id) => tokenSourceMap.get(id))
    .filter((source): source is MockTokenSource => Boolean(source));
  const linkedTotal = linkedSources.reduce((total, source) => total + source.token, 0);
  const primaryStatusAction = (() => {
    if (!task.can_update_status) return undefined;
    if (task.status === "done") {
      return {
        label: "重新打开",
        onClick: () => requestStatusChange("todo")
      };
    }
    if (dependencyBlocked) return undefined;
    if (task.status === "todo") {
      return {
        label: "开始任务",
        onClick: () => requestStatusChange("in_progress")
      };
    }
    return {
      label: "标记完成",
      onClick: () => requestStatusChange("done")
    };
  })();
  const moreActionItems = task.can_delete
    ? [
        {
          key: "delete",
          danger: true,
          icon: <DeleteOutlined />,
          label: "删除任务",
          onClick: handleDelete
        }
      ]
    : [];

  return (
    <Space orientation="vertical" size={16} style={{ width: "100%" }}>
      <section className="requirements-drawer__summary requirements-task-detail__summary">
        <div className="requirements-drawer__summary-head">
          <div className="requirements-drawer__summary-tags">
            <TaskStatusTag status={task.status} />
            <RequirementPriorityTag priority={task.priority} />
            {dependencyBlocked ? <Tag color="error">上游阻塞</Tag> : null}
          </div>
          {(task.can_update_status || task.can_update_meta || task.can_delete) ? (
            <div className="requirements-drawer__actions">
              {primaryStatusAction ? (
                <Button
                  type="primary"
                  loading={statusMutation.isPending}
                  onClick={primaryStatusAction.onClick}
                >
                  {primaryStatusAction.label}
                </Button>
              ) : null}
              {task.can_update_meta ? (
                <Button icon={<EditOutlined />} onClick={() => setEditOpen(true)}>
                  编辑
                </Button>
              ) : null}
              {moreActionItems.length ? (
                <Dropdown menu={{ items: moreActionItems }} trigger={["click"]}>
                  <Button icon={<MoreOutlined />} aria-label="更多操作" />
                </Dropdown>
              ) : null}
            </div>
          ) : null}
        </div>
        {dependencyBlocked ? (
          <Alert
            type="warning"
            showIcon
            message="上游任务未完成，当前任务暂不能推进"
            style={{ marginBottom: 12 }}
          />
        ) : null}
        <div className="requirements-drawer__summary-strip">
          <div>
            <span>任务进度</span>
            <RequirementProgress value={task.progress} />
          </div>
          <div>
            <span>负责人</span>
            <strong>{task.assignee_name || "未分配"}</strong>
          </div>
          <div>
            <span>截止日期</span>
            <strong>{formatDate(task.due_date)}</strong>
          </div>
        </div>
        <div className="requirements-drawer__progress-editor">
          <Slider min={0} max={100} value={progress} disabled={!task.can_update_progress} onChange={setProgress} />
          <Space.Compact>
            <InputNumber
              min={0}
              max={100}
              value={progress}
              disabled={!task.can_update_progress}
              onChange={(value) => setProgress(value ?? 0)}
            />
            <Button disabled>%</Button>
          </Space.Compact>
          <Button
            type="primary"
            loading={progressMutation.isPending}
            disabled={!task.can_update_progress || progress === task.progress}
            onClick={() => progressMutation.mutate()}
          >
            保存进度
          </Button>
        </div>
      </section>

      <section className="requirements-drawer__section">
        <h3>任务信息</h3>
        <div className="requirements-task-detail__meta">
          <div>
            <span>所属需求</span>
            <strong>{task.requirement_title}</strong>
          </div>
          <div>
            <span>最近更新</span>
            <strong>{formatDateTime(task.updated_at)}</strong>
          </div>
        </div>
        {task.acceptance_criteria.length ? (
          <ol className="requirements-drawer__ac-list">
            {task.acceptance_criteria.map((item, index) => (
              <li key={`${index}-${item}`}>
                <span>AC {index + 1}</span>
                {item}
              </li>
            ))}
          </ol>
        ) : (
          <p style={{ margin: "12px 0 0", color: "#7a879a" }}>暂无任务验收标准</p>
        )}
      </section>

      <section className="requirements-drawer__section">
        <div className="requirements-drawer__section-head">
          <h3>上游依赖</h3>
          <span>{task.dependencies.length ? `${task.dependencies.length} 个依赖` : "无上游依赖"}</span>
        </div>
        {task.dependencies.length ? (
          <Space wrap size={6}>
            {task.dependencies.map((dependency) => (
              <Tag
                key={dependency.task_id}
                color={dependency.status === "done" ? "success" : "error"}
                closable={Boolean(task.can_manage_dependencies)}
                onClose={(event) => {
                  event.preventDefault();
                  removeDependencyMutation.mutate(dependency.task_id);
                }}
              >
                {dependency.task_title}
              </Tag>
            ))}
          </Space>
        ) : null}
        <Space.Compact style={{ width: "100%", marginTop: 12 }}>
          <Select
            allowClear
            showSearch
            value={dependencyDraft}
            disabled={!task.can_manage_dependencies}
            placeholder="选择同一需求内的上游任务"
            optionFilterProp="label"
            options={requirementTasks
              .filter(
                (candidate) =>
                  candidate.id !== task.id &&
                  !task.dependencies.some((dependency) => dependency.task_id === candidate.id)
              )
              .map((candidate) => ({ value: candidate.id, label: candidate.title }))}
            onChange={setDependencyDraft}
          />
          <Button
            loading={addDependencyMutation.isPending}
            disabled={!task.can_manage_dependencies || !dependencyDraft}
            onClick={() => dependencyDraft && addDependencyMutation.mutate(dependencyDraft)}
          >
            添加依赖
          </Button>
        </Space.Compact>
      </section>

      <section className="requirements-drawer__section">
        <div className="requirements-drawer__section-head">
          <h3>关联 session</h3>
          <div className="requirements-drawer__section-actions">
            {linkedTotal > 0 ? (
              <span>已关联 {formatTokens(linkedTotal)} Token</span>
            ) : null}
            {canManage ? (
              <Button size="small" icon={<LinkOutlined />} onClick={() => setPickerOpen(true)}>
                关联 session
              </Button>
            ) : null}
          </div>
        </div>
        {linkedSources.length ? (
          <Space orientation="vertical" size={8} style={{ width: "100%" }}>
            {linkedSources.map((source) => (
              <div key={source.id} className="requirements-drawer__token-row">
                <div className="requirements-drawer__token-row-main">
                  <strong>{source.summary || "（无摘要）"}</strong>
                  <span>
                    {formatTokenSourceTime(source.recorded_at)} · {source.tool} · {source.uploader}
                  </span>
                </div>
                <div className="requirements-drawer__token-row-meta">
                  <span>{formatTokens(source.token)} Token</span>
                  <Button
                    size="small"
                    type="text"
                    icon={<CloseOutlined />}
                    loading={unlinkMutation.isPending && unlinkMutation.variables === source.id}
                    disabled={!canManage}
                    onClick={() => unlinkMutation.mutate(source.id)}
                    aria-label="移除"
                  />
                </div>
              </div>
            ))}
          </Space>
        ) : (
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无关联 session" />
        )}
      </section>

      <TokenSourcePicker
        open={pickerOpen}
        excludeIds={task.token_source_ids}
        confirmLoading={linkMutation.isPending}
        onCancel={() => setPickerOpen(false)}
        onConfirm={async (ids) => {
          await linkMutation.mutateAsync(ids);
        }}
      />

      <TaskEditModal
        open={editOpen}
        task={task}
        onCancel={() => setEditOpen(false)}
        onSaved={(updated) => {
          setEditOpen(false);
          onSaved(updated);
        }}
      />
    </Space>
  );
}

function TaskEditModal({
  open,
  task,
  onCancel,
  onSaved
}: {
  open: boolean;
  task: MockTask;
  onCancel: () => void;
  onSaved: (task: MockTask) => void;
}) {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [form] = Form.useForm<{
    title: string;
    assignee_id?: string;
    priority: MockTaskPriority;
    due_date?: dayjs.Dayjs;
    acceptance_criteria?: string[];
  }>();

  const assigneesQuery = useQuery({
    queryKey: ["requirements-board", "assignees"],
    queryFn: () => requirementsBoardApi.listAssignees(),
    staleTime: 5 * 60_000
  });

  const initialValues = useMemo(
    () => ({
      title: task.title,
      assignee_id: task.assignee_id,
      priority: task.priority,
      due_date: task.due_date ? dayjs(task.due_date) : undefined,
      acceptance_criteria: task.acceptance_criteria.length ? task.acceptance_criteria : [""]
    }),
    [task]
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
      assignee_id?: string;
      priority: MockTaskPriority;
      due_date?: dayjs.Dayjs;
      acceptance_criteria?: string[];
    }) => {
      const payload = {
        title: normalizeRequiredText(values.title),
        priority: values.priority,
        due_date: values.due_date ? values.due_date.format("YYYY-MM-DD") : undefined,
        acceptance_criteria: normalizeCriteria(values.acceptance_criteria),
        base_version: task.version,
        ...(canReassign ? { assignee_id: values.assignee_id } : {})
      };
      return requirementsBoardApi.updateTask(task.id, payload);
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

  return (
    <Modal
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
      <Form
        form={form}
        layout="vertical"
        initialValues={initialValues}
        onFinish={(values) => updateMutation.mutate(values)}
      >
        <Form.Item
          label="任务标题"
          name="title"
          rules={titleRules("任务标题")}
        >
          <Input placeholder="任务标题" />
        </Form.Item>
        <Form.Item label="负责人" name="assignee_id" rules={requiredSelectRules("负责人")}>
          <Select
            placeholder="选择负责人"
            loading={assigneesQuery.isLoading}
            disabled={!canReassign || assigneesQuery.isLoading || assigneesQuery.isError}
            options={(assigneesQuery.data ?? []).map((item: MockAssignee) => ({
              value: item.id,
              label: `${item.name} (${item.employee_id})`
            }))}
          />
        </Form.Item>
        <Form.Item
          label="优先级"
          name="priority"
          rules={requiredSelectRules("优先级")}
        >
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
        <Form.Item label="标准列表" name="acceptance_criteria" rules={acceptanceCriteriaRules()}>
          <AcceptanceCriteriaEditor placeholder="输入一条可验证的任务验收标准" />
        </Form.Item>
      </Form>
    </Modal>
  );
}
