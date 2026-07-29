import {
  ApiOutlined,
  ClockCircleOutlined,
  CloudServerOutlined,
  DeleteOutlined,
  DownOutlined,
  EditOutlined,
  EyeOutlined,
  PlayCircleOutlined,
  PlusOutlined,
  ReloadOutlined,
  RobotOutlined,
  StarFilled,
  StarOutlined,
  ToolOutlined
} from "@ant-design/icons";
import { Alert, App, Button, Dropdown, Empty, Modal, Popconfirm, Space, Tabs, Tag } from "antd";
import type { MenuProps, TableProps } from "antd";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";

import { ResourceTable } from "@/shared/components/ResourceTable/ResourceTable";

import {
  archiveManagedAgent,
  archiveManagedMCPEntry,
  archiveManagedSkill,
  createDefaultReportAgent,
  deleteManagedMCPEntry,
  deleteManagedSkill,
  deleteManagedAgentSchedule,
  fetchManagedAgentRuns,
  fetchManagedAgentSchedules,
  fetchManagedAgents,
  fetchManagedMCPEntries,
  fetchManagedSkillMarkdown,
  fetchManagedSkills,
  runManagedAgentScheduleNow,
  setDefaultReportAgent,
  updateManagedAgentSchedule
} from "../../api/client";
import type {
  AIRun,
  ManagedAgent,
  ManagedAgentSchedule,
  ManagedMCPBinding,
  ManagedMCPEntry,
  ManagedSkill,
  ManagedSkillRef,
  UpsertManagedAgentSchedulePayload
} from "../../api/types";
import {
  RequirementMetricCard,
  RequirementMetricGrid
} from "../../requirements/components/RequirementMetricCard";
import { PagePanel } from "@/shared/components/PagePanel/PagePanel";
import { HttpError } from "@/shared/request/types";
import {
  AI_ASSETS_TAB_QUERY_PARAM,
  aiAssetsChildPath,
  type AssetTab,
  errorMessage,
  getAIAssetsTabFromSearch,
  isReportAgentAsset,
  isSystemBuiltinMCP,
  isSystemBuiltinSkill,
  refKey
} from "../utils/agentAssets";

import "./AIAssetsPage.css";

const PERSONAL_RESOURCE_MANAGEMENT_VISIBLE = true;

const WEEKDAY_OPTIONS = [
  { label: "周一", value: 1 },
  { label: "周二", value: 2 },
  { label: "周三", value: 3 },
  { label: "周四", value: 4 },
  { label: "周五", value: 5 },
  { label: "周六", value: 6 },
  { label: "周日", value: 7 }
];

function aiAssetsTablePagination(total: number): TableProps<object>["pagination"] {
  return {
    pageSize: 10,
    showSizeChanger: true,
    showTotal: (value) => `共 ${value} 条记录`,
    total
  };
}

function unixTime(value?: number) {
  return formatDateTime(value);
}

function formatDateTime(value?: string | number) {
  if (!value) return "-";
  const date = typeof value === "number" ? new Date(value * 1000) : new Date(value);
  if (Number.isNaN(date.getTime())) return "-";
  const pad = (input: number) => String(input).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(
    date.getHours()
  )}:${pad(date.getMinutes())}`;
}

function runTypeLabel(value: string) {
  if (value === "report_agent_run") return "报告运行";
  if (value === "manual_agent_run") return "手动运行";
  if (value === "scheduled_agent_run") return "定时报告任务";
  return value || "-";
}

function runStatusMeta(value: AIRun["status"] | ManagedAgentSchedule["last_run_status"]) {
  const meta: Record<string, { label: string; color: string }> = {
    pending: { label: "等待", color: "blue" },
    running: { label: "运行中", color: "processing" },
    succeeded: { label: "成功", color: "green" },
    failed: { label: "失败", color: "red" },
    timeout: { label: "超时", color: "orange" }
  };
  return value ? meta[value] || { label: value, color: "default" } : null;
}

function runStatusTag(value: AIRun["status"] | ManagedAgentSchedule["last_run_status"]) {
  const meta = runStatusMeta(value);
  if (!meta) return <Tag>未运行</Tag>;
  return <Tag color={meta.color}>{meta.label}</Tag>;
}

function scheduleRuleText(schedule: ManagedAgentSchedule) {
  const weekdays =
    schedule.schedule_type === "weekly"
      ? schedule.weekdays
          .map((day) => WEEKDAY_OPTIONS.find((item) => item.value === day)?.label)
          .filter(Boolean)
          .join("、")
      : "每天";
  return `${weekdays} ${schedule.time_of_day}`;
}

function MobileMeta({ label, value }: { label: string; value: ReactNode }) {
  return (
    <span className="ai-assets-mobile-meta">
      <span>{label}</span>
      <strong>{value || "-"}</strong>
    </span>
  );
}

function AssetEmptyState({
  icon,
  title,
  description,
  action
}: {
  icon: ReactNode;
  title: string;
  description?: string;
  action?: ReactNode;
}) {
  return (
    <div className="ai-assets-empty-state">
      <span className="ai-assets-empty-state__icon" aria-hidden="true">
        {icon}
      </span>
      <strong>{title}</strong>
      {description ? <span>{description}</span> : null}
      {action ? <div className="ai-assets-empty-state__action">{action}</div> : null}
    </div>
  );
}

type ManagedAgentPlatformIssue = {
  code:
    | "MANAGED_AGENT_NOT_CONFIGURED"
    | "MANAGED_AGENT_UNREACHABLE"
    | "MANAGED_AGENT_UPSTREAM_ERROR"
    | "UNKNOWN";
  title: string;
  description: string;
};

function managedAgentPayloadCode(error: unknown) {
  if (!(error instanceof HttpError) || !error.payload || typeof error.payload !== "object") {
    return undefined;
  }
  const payload = error.payload as { code?: unknown };
  return typeof payload.code === "string" ? payload.code : undefined;
}

function managedAgentPlatformIssue(error: unknown): ManagedAgentPlatformIssue | null {
  const code = managedAgentPayloadCode(error);
  if (code === "MANAGED_AGENT_NOT_CONFIGURED") {
    return {
      code,
      title: "Managed Agent 平台未配置",
      description: "Managed Agent 平台未配置，请联系管理员配置服务地址和 Token。"
    };
  }
  if (code === "MANAGED_AGENT_UNREACHABLE") {
    return {
      code,
      title: "Managed Agent 平台不可达",
      description: "Managed Agent 平台暂时不可用，请稍后重试或联系管理员检查服务连通性。"
    };
  }
  if (code === "MANAGED_AGENT_UPSTREAM_ERROR") {
    return {
      code,
      title: "Managed Agent 平台返回错误",
      description: errorMessage(error)
    };
  }
  if (error) {
    return {
      code: "UNKNOWN",
      title: "Managed Agent 平台请求失败",
      description: errorMessage(error)
    };
  }
  return null;
}

function schedulePayloadFromRecord(
  schedule: ManagedAgentSchedule,
  enabled = schedule.enabled
): UpsertManagedAgentSchedulePayload {
  return {
    name: schedule.name,
    agent_id: schedule.agent_id,
    run_kind: schedule.run_kind || "generic_agent",
    enabled,
    trigger_config: {
      schedule_type: schedule.schedule_type,
      weekdays: schedule.schedule_type === "weekly" ? schedule.weekdays : undefined,
      time_of_day: schedule.time_of_day
    },
    run_config: {
      model_id: schedule.model_id,
      initial_message: schedule.initial_message || schedule.message,
      start_prompt_values: schedule.start_prompt_values || schedule.params || {},
      report_config:
        schedule.run_kind === "report_agent" && schedule.report_config?.report_type
          ? { report_type: schedule.report_config.report_type }
          : undefined
    }
  };
}

const AGENTS_PATH = "/ai-assets/agents";

export function AIAssetsPage() {
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const queryClient = useQueryClient();
  const { message } = App.useApp();
  const tab = getAIAssetsTabFromSearch(searchParams);
  const [viewingSkill, setViewingSkill] = useState<ManagedSkill | null>(null);

  const setAssetTab = (nextTab: AssetTab) => {
    setSearchParams(
      (current) => {
        const next = new URLSearchParams(current);
        next.set(AI_ASSETS_TAB_QUERY_PARAM, nextTab);
        return next;
      },
      { replace: true }
    );
  };

  const skillsQuery = useQuery({
    queryKey: ["managed-skills", "mine"],
    queryFn: () => fetchManagedSkills(),
    enabled: PERSONAL_RESOURCE_MANAGEMENT_VISIBLE,
    staleTime: 60_000
  });
  const mcpQuery = useQuery({
    queryKey: ["managed-mcp", "mine"],
    queryFn: () => fetchManagedMCPEntries(),
    enabled: PERSONAL_RESOURCE_MANAGEMENT_VISIBLE,
    staleTime: 60_000
  });
  const agentsQuery = useQuery({
    queryKey: ["managed-agents"],
    queryFn: () => fetchManagedAgents(),
    staleTime: 30_000
  });
  const runsQuery = useQuery({
    queryKey: ["managed-agent-runs"],
    queryFn: () => fetchManagedAgentRuns({ page_size: "50" }),
    staleTime: 15_000
  });
  const schedulesQuery = useQuery({
    queryKey: ["managed-agent-schedules"],
    queryFn: () => fetchManagedAgentSchedules(),
    staleTime: 15_000
  });
  const skillMarkdownQuery = useQuery({
    queryKey: [
      "managed-skill-md",
      viewingSkill?.owner || "_mine",
      viewingSkill?.slug,
      viewingSkill?.version
    ],
    queryFn: () =>
      fetchManagedSkillMarkdown(
        viewingSkill?.owner,
        viewingSkill?.slug || "",
        viewingSkill?.version || ""
      ),
    enabled: Boolean(viewingSkill),
    staleTime: 30_000
  });

  const skills = useMemo(() => skillsQuery.data?.skills ?? [], [skillsQuery.data]);
  const mcpEntries = useMemo(() => mcpQuery.data?.entries ?? [], [mcpQuery.data]);
  const agents = useMemo(() => agentsQuery.data?.agents ?? [], [agentsQuery.data]);
  const runs = useMemo(() => runsQuery.data?.runs ?? [], [runsQuery.data]);
  const schedules = useMemo(() => schedulesQuery.data?.schedules ?? [], [schedulesQuery.data]);
  const visibleSkills = useMemo(
    () => skills.filter((item) => !isSystemBuiltinSkill(item)),
    [skills]
  );
  const visibleMCPEntries = useMemo(
    () => mcpEntries.filter((item) => !isSystemBuiltinMCP(item)),
    [mcpEntries]
  );
  const platformIssue = useMemo(
    () =>
      managedAgentPlatformIssue(skillsQuery.error) ??
      managedAgentPlatformIssue(mcpQuery.error) ??
      managedAgentPlatformIssue(agentsQuery.error),
    [agentsQuery.error, mcpQuery.error, skillsQuery.error]
  );
  const platformBlocked = Boolean(platformIssue);
  const platformActionTitle = platformIssue?.description;
  const agentNameByID = useMemo(() => {
    const names = new Map<string, string>();
    for (const agent of agents) {
      names.set(agent.agent_id, agent.name || agent.agent_id);
    }
    return names;
  }, [agents]);

  const updateScheduleMutation = useMutation({
    mutationFn: (payload: { schedule: ManagedAgentSchedule; enabled: boolean }) =>
      updateManagedAgentSchedule(
        payload.schedule.id,
        schedulePayloadFromRecord(payload.schedule, payload.enabled)
      ),
    onSuccess: (_data, variables) => {
      message.success(variables.enabled ? "定时报告任务已启用" : "定时报告任务已停用");
      void queryClient.invalidateQueries({ queryKey: ["managed-agent-schedules"] });
    },
    onError: (err: unknown) => message.error(errorMessage(err))
  });

  const deleteScheduleMutation = useMutation({
    mutationFn: (scheduleId: string) => deleteManagedAgentSchedule(scheduleId),
    onSuccess: () => {
      message.success("定时报告任务已删除");
      void queryClient.invalidateQueries({ queryKey: ["managed-agent-schedules"] });
    },
    onError: (err: unknown) => message.error(errorMessage(err))
  });

  const runScheduleMutation = useMutation({
    mutationFn: (scheduleId: string) => runManagedAgentScheduleNow(scheduleId),
    onSuccess: () => {
      message.success("定时报告任务已提交生成");
      void queryClient.invalidateQueries({ queryKey: ["managed-agent-runs"] });
      void queryClient.invalidateQueries({ queryKey: ["managed-agent-schedules"] });
    },
    onError: (err: unknown) => message.error(errorMessage(err))
  });

  const createDefaultReportAgentMutation = useMutation({
    mutationFn: createDefaultReportAgent,
    onSuccess: () => {
      message.success("默认报告 Agent 已创建");
      void queryClient.invalidateQueries({ queryKey: ["managed-agents"] });
      void queryClient.invalidateQueries({ queryKey: ["managed-skills"] });
      void queryClient.invalidateQueries({ queryKey: ["managed-mcp"] });
    },
    onError: (err: unknown) => message.error(errorMessage(err))
  });

  const setDefaultReportAgentMutation = useMutation({
    mutationFn: setDefaultReportAgent,
    onSuccess: () => {
      message.success("默认报告 Agent 已更新");
      void queryClient.invalidateQueries({ queryKey: ["managed-agents"] });
    },
    onError: (err: unknown) => message.error(errorMessage(err))
  });

  const archiveSkillMutation = useMutation({
    mutationFn: (payload: { slug: string; version: string; archived: boolean }) =>
      archiveManagedSkill(payload.slug, payload.version, payload.archived),
    onSuccess: (_data, variables) => {
      message.success(variables.archived ? "Skill 已归档" : "Skill 已恢复");
      void queryClient.invalidateQueries({ queryKey: ["managed-skills"] });
    },
    onError: (err: unknown) => message.error(errorMessage(err))
  });

  const deleteSkillMutation = useMutation({
    mutationFn: (payload: { slug: string; version: string }) =>
      deleteManagedSkill(payload.slug, payload.version),
    onSuccess: () => {
      message.success("Skill 已删除");
      void queryClient.invalidateQueries({ queryKey: ["managed-skills"] });
    },
    onError: (err: unknown) => message.error(errorMessage(err))
  });

  const archiveMCPMutation = useMutation({
    mutationFn: (payload: { slug: string; version: string; archived: boolean }) =>
      archiveManagedMCPEntry(payload.slug, payload.version, payload.archived),
    onSuccess: (_data, variables) => {
      message.success(variables.archived ? "MCP 已归档" : "MCP 已恢复");
      void queryClient.invalidateQueries({ queryKey: ["managed-mcp"] });
    },
    onError: (err: unknown) => message.error(errorMessage(err))
  });

  const deleteMCPMutation = useMutation({
    mutationFn: (payload: { slug: string; version: string }) =>
      deleteManagedMCPEntry(payload.slug, payload.version),
    onSuccess: () => {
      message.success("MCP 已删除");
      void queryClient.invalidateQueries({ queryKey: ["managed-mcp"] });
    },
    onError: (err: unknown) => message.error(errorMessage(err))
  });

  const archiveAgentMutation = useMutation({
    mutationFn: (payload: { agentId: string; archived: boolean }) =>
      archiveManagedAgent(payload.agentId, payload.archived),
    onSuccess: (_data, variables) => {
      message.success(variables.archived ? "Agent 已删除" : "Agent 已恢复");
      void queryClient.invalidateQueries({ queryKey: ["managed-agents"] });
      void queryClient.invalidateQueries({ queryKey: ["managed-agent-runs"] });
    },
    onError: (err: unknown) => message.error(errorMessage(err))
  });

  const createAssetItems: MenuProps["items"] = [
    ...(PERSONAL_RESOURCE_MANAGEMENT_VISIBLE
      ? [
          {
            key: "skill",
            icon: <ToolOutlined />,
            label: "新建 Skill",
            disabled: platformBlocked
          },
          {
            key: "mcp",
            icon: <ApiOutlined />,
            label: "新建 MCP",
            disabled: platformBlocked
          }
        ]
      : []),
    {
      key: "schedule",
      icon: <ClockCircleOutlined />,
      label: "新建定时报告任务"
    }
  ];

  const handleCreateAsset: MenuProps["onClick"] = ({ key }) => {
    if (key === "skill") {
      navigate(aiAssetsChildPath("/ai-assets/skills/new", "skills"));
    } else if (key === "mcp") {
      navigate(aiAssetsChildPath("/ai-assets/mcp/new", "mcp"));
    } else if (key === "schedule") {
      navigate(aiAssetsChildPath("/ai-assets/agent-schedules/new", "schedules"));
    }
  };

  const renderAssetEmpty = (
    title: string,
    icon: ReactNode,
    action?: ReactNode,
    description?: string
  ) => {
    if (platformIssue) {
      return (
        <AssetEmptyState
          icon={<CloudServerOutlined />}
          title={platformIssue.title}
          description={platformIssue.description}
        />
      );
    }
    return <AssetEmptyState icon={icon} title={title} description={description} action={action} />;
  };

  const skillColumns: TableProps<ManagedSkill>["columns"] = [
    {
      title: "Skill",
      dataIndex: "name",
      render: (_: string, record) => (
        <span className="ai-assets-name">
          <strong>{record.name || record.slug}</strong>
          <span>{record.description || `${record.slug}@${record.version}`}</span>
        </span>
      )
    },
    { title: "归属", dataIndex: "owner", width: 140, render: (v?: string) => v || "我的" },
    { title: "Slug", dataIndex: "slug", width: 160 },
    { title: "版本", dataIndex: "version", width: 120 },
    {
      title: "状态",
      dataIndex: "archived",
      width: 100,
      render: (archived: boolean) =>
        archived ? <Tag color="default">已归档</Tag> : <Tag color="green">可用</Tag>
    },
    { title: "创建时间", dataIndex: "created_at", width: 180, render: unixTime },
    {
      title: "操作",
      key: "actions",
      width: 220,
      render: (_: unknown, record) => (
        <Space size={4} className="resource-actions">
          <Button
            type="link"
            className="resource-action"
            icon={<EyeOutlined />}
            disabled={platformBlocked}
            title={platformActionTitle}
            onClick={() => setViewingSkill(record)}
          >
            查看
          </Button>
          <Button
            type="link"
            className="resource-action"
            disabled={platformBlocked}
            title={platformActionTitle}
            loading={
              archiveSkillMutation.isPending &&
              archiveSkillMutation.variables?.slug === record.slug &&
              archiveSkillMutation.variables?.version === record.version
            }
            onClick={() =>
              archiveSkillMutation.mutate({
                slug: record.slug,
                version: record.version,
                archived: !record.archived
              })
            }
          >
            {record.archived ? "恢复" : "归档"}
          </Button>
          <Popconfirm
            title="删除 Skill"
            description="删除后无法继续绑定该版本；如果已有 Agent 引用，平台会拒绝删除。"
            okText="删除"
            cancelText="取消"
            onConfirm={() =>
              deleteSkillMutation.mutate({ slug: record.slug, version: record.version })
            }
          >
            <Button
              type="link"
              className="resource-action resource-action--danger"
              danger
              icon={<DeleteOutlined />}
              disabled={platformBlocked}
              title={platformActionTitle}
              loading={
                deleteSkillMutation.isPending &&
                deleteSkillMutation.variables?.slug === record.slug &&
                deleteSkillMutation.variables?.version === record.version
              }
            >
              删除
            </Button>
          </Popconfirm>
        </Space>
      )
    }
  ];

  const mcpColumns: TableProps<ManagedMCPEntry>["columns"] = [
    {
      title: "MCP",
      dataIndex: "name",
      render: (_: string, record) => (
        <span className="ai-assets-name">
          <strong>{record.name || record.slug}</strong>
          <span>{record.description || record.url || record.command || "-"}</span>
        </span>
      )
    },
    { title: "协议", dataIndex: "transport", width: 130 },
    { title: "Slug", dataIndex: "slug", width: 160 },
    { title: "版本", dataIndex: "version", width: 110 },
    {
      title: "凭据",
      dataIndex: "requires_credential",
      width: 100,
      render: (required: boolean) => (required ? <Tag color="orange">需要</Tag> : <Tag>无</Tag>)
    },
    {
      title: "状态",
      dataIndex: "archived",
      width: 100,
      render: (archived: boolean) =>
        archived ? <Tag color="default">已归档</Tag> : <Tag color="blue">可用</Tag>
    },
    {
      title: "操作",
      key: "actions",
      width: 190,
      render: (_: unknown, record) => (
        <Space size={4} className="resource-actions">
          <Button
            type="link"
            className="resource-action"
            disabled={platformBlocked}
            title={platformActionTitle}
            loading={
              archiveMCPMutation.isPending &&
              archiveMCPMutation.variables?.slug === record.slug &&
              archiveMCPMutation.variables?.version === record.version
            }
            onClick={() =>
              archiveMCPMutation.mutate({
                slug: record.slug,
                version: record.version,
                archived: !record.archived
              })
            }
          >
            {record.archived ? "恢复" : "归档"}
          </Button>
          <Popconfirm
            title="删除 MCP"
            description="删除后无法继续绑定该版本；如果已有 Agent 引用，平台会拒绝删除。"
            okText="删除"
            cancelText="取消"
            onConfirm={() =>
              deleteMCPMutation.mutate({ slug: record.slug, version: record.version })
            }
          >
            <Button
              type="link"
              className="resource-action resource-action--danger"
              danger
              icon={<DeleteOutlined />}
              disabled={platformBlocked}
              title={platformActionTitle}
              loading={
                deleteMCPMutation.isPending &&
                deleteMCPMutation.variables?.slug === record.slug &&
                deleteMCPMutation.variables?.version === record.version
              }
            >
              删除
            </Button>
          </Popconfirm>
        </Space>
      )
    }
  ];

  const agentColumns: TableProps<ManagedAgent>["columns"] = [
    {
      title: "Agent",
      dataIndex: "name",
      render: (_: string, record) => (
        <span className="ai-assets-name">
          <strong>
            {record.name} {record.source === "system" ? <Tag color="gold">系统</Tag> : null}
          </strong>
          <span>{record.description || record.agent_id}</span>
        </span>
      )
    },
    { title: "Engine", dataIndex: "engine", width: 130 },
    {
      title: "版本",
      dataIndex: "current_version_id",
      width: 100,
      render: (v: number | undefined, record) => v || record.managed_version || "-"
    },
    {
      title: "Skill",
      dataIndex: "skills",
      width: 120,
      render: (items?: ManagedSkillRef[]) => items?.length ?? 0
    },
    {
      title: "MCP",
      dataIndex: "mcp_bindings",
      width: 120,
      render: (items: ManagedMCPBinding[] | undefined, record) =>
        (items?.length ?? 0) + (record.mcp_servers?.length ?? 0)
    },
    {
      title: "状态",
      dataIndex: "archived",
      width: 150,
      render: (archived: boolean, record) => (
        <Space size={4} wrap>
          {archived ? <Tag color="default">已删除</Tag> : <Tag color="blue">可用</Tag>}
          {record.is_default_report ? <Tag color="purple">默认报告</Tag> : null}
        </Space>
      )
    },
    {
      title: "操作",
      key: "actions",
      width: 300,
      render: (_: unknown, record) => (
        <Space size={4} className="resource-actions">
          {record.permissions?.can_run !== false ? (
            <Button
              type="link"
              className="resource-action"
              icon={<PlayCircleOutlined />}
              disabled={platformBlocked || record.archived}
              title={record.archived ? "已删除 Agent 不能发起新运行" : platformActionTitle}
              onClick={() =>
                navigate(aiAssetsChildPath(`${AGENTS_PATH}/${record.agent_id}/run`, "agents"))
              }
            >
              运行
            </Button>
          ) : null}
          {record.permissions?.can_edit !== false ? (
            <Button
              type="link"
              className="resource-action"
              icon={<EditOutlined />}
              onClick={() =>
                navigate(aiAssetsChildPath(`${AGENTS_PATH}/${record.agent_id}/edit`, "agents"))
              }
            >
              编辑
            </Button>
          ) : null}
          {isReportAgentAsset(record) && record.permissions?.can_set_default !== false ? (
            <Button
              type="link"
              className="resource-action"
              icon={record.is_default_report ? <StarFilled /> : <StarOutlined />}
              disabled={platformBlocked || record.archived || record.is_default_report}
              title={
                record.is_default_report
                  ? "当前默认报告 Agent"
                  : "设为日报、周报弹窗默认使用的报告 Agent"
              }
              loading={
                setDefaultReportAgentMutation.isPending &&
                setDefaultReportAgentMutation.variables === record.agent_id
              }
              onClick={() => setDefaultReportAgentMutation.mutate(record.agent_id)}
            >
              {record.is_default_report ? "默认" : "设为默认"}
            </Button>
          ) : null}
          {record.permissions?.can_archive === false ? null : record.archived ? (
            <Button
              type="link"
              className="resource-action"
              disabled={platformBlocked}
              title={platformActionTitle}
              loading={
                archiveAgentMutation.isPending &&
                archiveAgentMutation.variables?.agentId === record.agent_id
              }
              onClick={() =>
                archiveAgentMutation.mutate({
                  agentId: record.agent_id,
                  archived: false
                })
              }
            >
              恢复
            </Button>
          ) : (
            <Popconfirm
              title="删除 Agent"
              description="删除后该 Agent 将从当前列表移除，不能继续发起新运行。确认删除吗？"
              okText="删除"
              cancelText="取消"
              okButtonProps={{ danger: true }}
              onConfirm={() =>
                archiveAgentMutation.mutate({
                  agentId: record.agent_id,
                  archived: true
                })
              }
            >
              <Button
                type="link"
                className="resource-action resource-action--danger"
                danger
                icon={<DeleteOutlined />}
                disabled={platformBlocked}
                title={platformActionTitle}
                loading={
                  archiveAgentMutation.isPending &&
                  archiveAgentMutation.variables?.agentId === record.agent_id
                }
              >
                删除
              </Button>
            </Popconfirm>
          )}
        </Space>
      )
    }
  ];

  const runColumns: TableProps<AIRun>["columns"] = [
    {
      title: "Agent",
      dataIndex: "agent_id",
      width: 220,
      render: (value: string) => <span className="ai-assets-run-agent">{value}</span>
    },
    {
      title: "类型",
      dataIndex: "business_type",
      width: 150,
      render: (value: string) => runTypeLabel(value)
    },
    {
      title: "状态",
      dataIndex: "status",
      width: 110,
      render: (value: AIRun["status"]) => runStatusTag(value)
    },
    { title: "模型", dataIndex: "model_id", width: 140, render: (value?: string) => value || "-" },
    {
      title: "Task",
      dataIndex: "external_task_id",
      width: 240,
      render: (value?: string) => value || "-"
    },
    {
      title: "结果",
      dataIndex: "result",
      render: (_: string, record) => (
        <span
          className={
            record.error_message
              ? "ai-assets-run-summary ai-assets-run-summary--error"
              : "ai-assets-run-summary"
          }
        >
          {record.error_message || record.result || "-"}
        </span>
      )
    },
    {
      title: "时间",
      dataIndex: "created_at",
      width: 180,
      render: (value: string) => formatDateTime(value)
    }
  ];

  const scheduleColumns: TableProps<ManagedAgentSchedule>["columns"] = [
    {
      title: "报告任务名称",
      dataIndex: "name",
      render: (_: string, record) => (
        <span className="ai-assets-name">
          <strong>{record.name}</strong>
        </span>
      )
    },
    {
      title: "Agent",
      dataIndex: "agent_id",
      render: (value: string) => agentNameByID.get(value) || value
    },
    {
      title: "Agent 类型",
      dataIndex: "run_kind",
      width: 130,
      render: (value: ManagedAgentSchedule["run_kind"]) =>
        value === "report_agent" ? <Tag color="purple">报告 Agent</Tag> : <Tag>普通 Agent</Tag>
    },
    {
      title: "生成规则",
      dataIndex: "schedule_type",
      width: 180,
      render: (_: string, record) => scheduleRuleText(record)
    },
    {
      title: "下次生成时间",
      dataIndex: "next_run_at",
      width: 180,
      render: (value?: string) => formatDateTime(value)
    },
    {
      title: "最近生成结果",
      dataIndex: "last_run_status",
      width: 140,
      render: (_: string, record) => {
        if (record.last_skip_reason) return <Tag color="default">已跳过</Tag>;
        return runStatusTag(record.last_run_status);
      }
    },
    {
      title: "启用状态",
      dataIndex: "enabled",
      width: 100,
      render: (enabled: boolean) =>
        enabled ? <Tag color="green">启用</Tag> : <Tag color="default">停用</Tag>
    },
    {
      title: "操作",
      width: 260,
      render: (_: unknown, record) => (
        <Space size={4} className="resource-actions">
          <Button
            type="link"
            className="resource-action"
            icon={<PlayCircleOutlined />}
            loading={runScheduleMutation.isPending && runScheduleMutation.variables === record.id}
            disabled={platformBlocked}
            title={platformActionTitle}
            onClick={() => runScheduleMutation.mutate(record.id)}
          >
            运行
          </Button>
          <Button
            type="link"
            className="resource-action"
            icon={<EditOutlined />}
            onClick={() =>
              navigate(
                aiAssetsChildPath(`/ai-assets/agent-schedules/${record.id}/edit`, "schedules")
              )
            }
          >
            编辑
          </Button>
          <Button
            type="link"
            className="resource-action"
            loading={
              updateScheduleMutation.isPending &&
              updateScheduleMutation.variables?.schedule.id === record.id
            }
            onClick={() =>
              updateScheduleMutation.mutate({ schedule: record, enabled: !record.enabled })
            }
          >
            {record.enabled ? "停用" : "启用"}
          </Button>
          <Popconfirm
            title="删除定时报告任务"
            description="删除后不会再自动生成对应报告。"
            okText="删除"
            cancelText="取消"
            onConfirm={() => deleteScheduleMutation.mutate(record.id)}
          >
            <Button
              type="link"
              className="resource-action resource-action--danger"
              danger
              icon={<DeleteOutlined />}
            >
              删除
            </Button>
          </Popconfirm>
        </Space>
      )
    }
  ];

  const operations = (
    <Space className="ai-assets-toolbar" wrap>
      <Button
        type="primary"
        icon={<PlusOutlined />}
        disabled={platformBlocked}
        title={platformActionTitle}
        onClick={() => navigate(aiAssetsChildPath(`${AGENTS_PATH}/new`, "agents"))}
      >
        新建 Agent
      </Button>
      <Dropdown menu={{ items: createAssetItems, onClick: handleCreateAsset }} trigger={["click"]}>
        <Button icon={<PlusOutlined />} title={platformActionTitle}>
          新建
          <DownOutlined />
        </Button>
      </Dropdown>
    </Space>
  );

  const agentEmptyState = renderAssetEmpty(
    "暂无 Agent",
    <RobotOutlined />,
    <Space className="ai-assets-empty-state__buttons" wrap>
      <Button
        type="primary"
        icon={<RobotOutlined />}
        loading={createDefaultReportAgentMutation.isPending}
        disabled={platformBlocked}
        onClick={() => createDefaultReportAgentMutation.mutate()}
      >
        创建默认 Agent
      </Button>
      <Button
        icon={<PlusOutlined />}
        disabled={platformBlocked}
        onClick={() => navigate(aiAssetsChildPath(`${AGENTS_PATH}/new`, "agents"))}
      >
        新建 Agent
      </Button>
    </Space>
  );

  const skillEmptyState = renderAssetEmpty(
    "暂无 Skill",
    <ToolOutlined />,
    <Button
      type="primary"
      icon={<PlusOutlined />}
      disabled={platformBlocked}
      onClick={() => navigate(aiAssetsChildPath("/ai-assets/skills/new", "skills"))}
    >
      新建 Skill
    </Button>
  );

  const mcpEmptyState = renderAssetEmpty(
    "暂无 MCP",
    <ApiOutlined />,
    <Button
      type="primary"
      icon={<PlusOutlined />}
      disabled={platformBlocked}
      onClick={() => navigate(aiAssetsChildPath("/ai-assets/mcp/new", "mcp"))}
    >
      新建 MCP
    </Button>
  );

  const scheduleEmptyState = renderAssetEmpty(
    "暂无定时报告任务",
    <ClockCircleOutlined />,
    <Button
      type="primary"
      icon={<PlusOutlined />}
      onClick={() => navigate(aiAssetsChildPath("/ai-assets/agent-schedules/new", "schedules"))}
    >
      新建定时报告任务
    </Button>
  );

  const agentMobileList =
    agents.length === 0 ? (
      <div className="ai-assets-mobile-list">{agentEmptyState}</div>
    ) : (
      <div className="ai-assets-mobile-list">
        {agents.map((agent) => (
          <article className="ai-assets-mobile-card" key={agent.agent_id}>
            <header className="ai-assets-mobile-card__header">
              <span className="ai-assets-name">
                <strong>{agent.name}</strong>
                <span>{agent.description || agent.agent_id}</span>
              </span>
              <span className="ai-assets-mobile-card__tags">
                {agent.source === "system" ? <Tag color="gold">系统</Tag> : null}
                {agent.archived ? <Tag color="default">已删除</Tag> : <Tag color="blue">可用</Tag>}
                {agent.is_default_report ? <Tag color="purple">默认报告</Tag> : null}
              </span>
            </header>
            <div className="ai-assets-mobile-card__meta">
              <MobileMeta label="Engine" value={agent.engine} />
              <MobileMeta
                label="版本"
                value={agent.current_version_id || agent.managed_version || "-"}
              />
              <MobileMeta label="Skill" value={agent.skills?.length ?? 0} />
              <MobileMeta
                label="MCP"
                value={(agent.mcp_bindings?.length ?? 0) + (agent.mcp_servers?.length ?? 0)}
              />
            </div>
            <div className="ai-assets-mobile-card__actions">
              {agent.permissions?.can_run !== false ? (
                <Button
                  type="primary"
                  size="small"
                  icon={<PlayCircleOutlined />}
                  disabled={platformBlocked || agent.archived}
                  onClick={() =>
                    navigate(aiAssetsChildPath(`${AGENTS_PATH}/${agent.agent_id}/run`, "agents"))
                  }
                >
                  运行
                </Button>
              ) : null}
              {agent.permissions?.can_edit !== false ? (
                <Button
                  size="small"
                  icon={<EditOutlined />}
                  onClick={() =>
                    navigate(aiAssetsChildPath(`${AGENTS_PATH}/${agent.agent_id}/edit`, "agents"))
                  }
                >
                  编辑
                </Button>
              ) : null}
              {isReportAgentAsset(agent) && agent.permissions?.can_set_default !== false ? (
                <Button
                  size="small"
                  icon={agent.is_default_report ? <StarFilled /> : <StarOutlined />}
                  loading={
                    setDefaultReportAgentMutation.isPending &&
                    setDefaultReportAgentMutation.variables === agent.agent_id
                  }
                  disabled={platformBlocked || agent.archived || agent.is_default_report}
                  onClick={() => setDefaultReportAgentMutation.mutate(agent.agent_id)}
                >
                  {agent.is_default_report ? "默认" : "设为默认"}
                </Button>
              ) : null}
              {agent.permissions?.can_archive === false ? null : agent.archived ? (
                <Button
                  size="small"
                  loading={
                    archiveAgentMutation.isPending &&
                    archiveAgentMutation.variables?.agentId === agent.agent_id
                  }
                  disabled={platformBlocked}
                  onClick={() =>
                    archiveAgentMutation.mutate({
                      agentId: agent.agent_id,
                      archived: false
                    })
                  }
                >
                  恢复
                </Button>
              ) : (
                <Popconfirm
                  title="删除 Agent"
                  description="删除后该 Agent 将从当前列表移除，不能继续发起新运行。确认删除吗？"
                  okText="删除"
                  cancelText="取消"
                  okButtonProps={{ danger: true }}
                  onConfirm={() =>
                    archiveAgentMutation.mutate({
                      agentId: agent.agent_id,
                      archived: true
                    })
                  }
                >
                  <Button
                    size="small"
                    danger
                    icon={<DeleteOutlined />}
                    loading={
                      archiveAgentMutation.isPending &&
                      archiveAgentMutation.variables?.agentId === agent.agent_id
                    }
                    disabled={platformBlocked}
                  >
                    删除
                  </Button>
                </Popconfirm>
              )}
            </div>
          </article>
        ))}
      </div>
    );

  const runMobileList =
    runs.length === 0 ? (
      <div className="ai-assets-mobile-list">
        <AssetEmptyState icon={<PlayCircleOutlined />} title="暂无运行记录" />
      </div>
    ) : (
      <div className="ai-assets-mobile-list">
        {runs.map((run) => (
          <article className="ai-assets-mobile-card" key={run.id}>
            <header className="ai-assets-mobile-card__header">
              <span className="ai-assets-name">
                <strong>{agentNameByID.get(run.agent_id) || run.agent_id}</strong>
                <span>{run.agent_id}</span>
              </span>
              {runStatusTag(run.status)}
            </header>
            <div className="ai-assets-mobile-card__meta">
              <MobileMeta label="类型" value={runTypeLabel(run.business_type)} />
              <MobileMeta label="模型" value={run.model_id || "-"} />
              <MobileMeta label="时间" value={formatDateTime(run.created_at)} />
            </div>
            <p
              className={
                run.error_message
                  ? "ai-assets-mobile-card__summary ai-assets-mobile-card__summary--error"
                  : "ai-assets-mobile-card__summary"
              }
            >
              {run.error_message || run.result || "无结果摘要"}
            </p>
          </article>
        ))}
      </div>
    );

  const skillMobileList =
    visibleSkills.length === 0 ? (
      <div className="ai-assets-mobile-list">{skillEmptyState}</div>
    ) : (
      <div className="ai-assets-mobile-list">
        {visibleSkills.map((skill) => (
          <article
            className="ai-assets-mobile-card"
            key={skill.skill_id || refKey(skill.owner, skill.slug, skill.version)}
          >
            <header className="ai-assets-mobile-card__header">
              <span className="ai-assets-name">
                <strong>{skill.name || skill.slug}</strong>
                <span>{skill.description || `${skill.slug}@${skill.version}`}</span>
              </span>
              {skill.archived ? <Tag color="default">已归档</Tag> : <Tag color="green">可用</Tag>}
            </header>
            <div className="ai-assets-mobile-card__meta">
              <MobileMeta label="归属" value={skill.owner || "我的"} />
              <MobileMeta label="版本" value={skill.version} />
              <MobileMeta label="创建" value={unixTime(skill.created_at)} />
            </div>
            <div className="ai-assets-mobile-card__actions">
              <Button
                size="small"
                icon={<EyeOutlined />}
                disabled={platformBlocked}
                onClick={() => setViewingSkill(skill)}
              >
                查看
              </Button>
              <Button
                size="small"
                disabled={platformBlocked}
                loading={
                  archiveSkillMutation.isPending &&
                  archiveSkillMutation.variables?.slug === skill.slug &&
                  archiveSkillMutation.variables?.version === skill.version
                }
                onClick={() =>
                  archiveSkillMutation.mutate({
                    slug: skill.slug,
                    version: skill.version,
                    archived: !skill.archived
                  })
                }
              >
                {skill.archived ? "恢复" : "归档"}
              </Button>
              <Popconfirm
                title="删除 Skill"
                description="删除后无法继续绑定该版本。"
                okText="删除"
                cancelText="取消"
                onConfirm={() =>
                  deleteSkillMutation.mutate({ slug: skill.slug, version: skill.version })
                }
              >
                <Button size="small" danger icon={<DeleteOutlined />} disabled={platformBlocked}>
                  删除
                </Button>
              </Popconfirm>
            </div>
          </article>
        ))}
      </div>
    );

  const mcpMobileList =
    visibleMCPEntries.length === 0 ? (
      <div className="ai-assets-mobile-list">{mcpEmptyState}</div>
    ) : (
      <div className="ai-assets-mobile-list">
        {visibleMCPEntries.map((entry) => (
          <article
            className="ai-assets-mobile-card"
            key={entry.entry_id || refKey(entry.owner, entry.slug, entry.version)}
          >
            <header className="ai-assets-mobile-card__header">
              <span className="ai-assets-name">
                <strong>{entry.name || entry.slug}</strong>
                <span>{entry.description || entry.url || entry.command || "-"}</span>
              </span>
              {entry.archived ? <Tag color="default">已归档</Tag> : <Tag color="blue">可用</Tag>}
            </header>
            <div className="ai-assets-mobile-card__meta">
              <MobileMeta label="协议" value={entry.transport} />
              <MobileMeta label="版本" value={entry.version} />
              <MobileMeta label="凭据" value={entry.requires_credential ? "需要" : "无"} />
            </div>
            <div className="ai-assets-mobile-card__actions">
              <Button
                size="small"
                disabled={platformBlocked}
                loading={
                  archiveMCPMutation.isPending &&
                  archiveMCPMutation.variables?.slug === entry.slug &&
                  archiveMCPMutation.variables?.version === entry.version
                }
                onClick={() =>
                  archiveMCPMutation.mutate({
                    slug: entry.slug,
                    version: entry.version,
                    archived: !entry.archived
                  })
                }
              >
                {entry.archived ? "恢复" : "归档"}
              </Button>
              <Popconfirm
                title="删除 MCP"
                description="删除后无法继续绑定该版本。"
                okText="删除"
                cancelText="取消"
                onConfirm={() =>
                  deleteMCPMutation.mutate({ slug: entry.slug, version: entry.version })
                }
              >
                <Button size="small" danger icon={<DeleteOutlined />} disabled={platformBlocked}>
                  删除
                </Button>
              </Popconfirm>
            </div>
          </article>
        ))}
      </div>
    );

  const scheduleMobileList =
    schedules.length === 0 ? (
      <div className="ai-assets-mobile-list">{scheduleEmptyState}</div>
    ) : (
      <div className="ai-assets-mobile-list">
        {schedules.map((schedule) => (
          <article className="ai-assets-mobile-card" key={schedule.id}>
            <header className="ai-assets-mobile-card__header">
              <span className="ai-assets-name">
                <strong>{schedule.name}</strong>
                <span>{agentNameByID.get(schedule.agent_id) || schedule.agent_id}</span>
              </span>
              {schedule.enabled ? <Tag color="green">启用</Tag> : <Tag color="default">停用</Tag>}
            </header>
            <div className="ai-assets-mobile-card__meta">
              <MobileMeta
                label="类型"
                value={schedule.run_kind === "report_agent" ? "报告 Agent" : "普通 Agent"}
              />
              <MobileMeta label="生成规则" value={scheduleRuleText(schedule)} />
              <MobileMeta label="下次生成" value={formatDateTime(schedule.next_run_at)} />
              <MobileMeta
                label="最近"
                value={
                  schedule.last_skip_reason
                    ? "已跳过"
                    : runStatusMeta(schedule.last_run_status)?.label || "未运行"
                }
              />
            </div>
            <div className="ai-assets-mobile-card__actions">
              <Button
                type="primary"
                size="small"
                icon={<PlayCircleOutlined />}
                loading={
                  runScheduleMutation.isPending && runScheduleMutation.variables === schedule.id
                }
                disabled={platformBlocked}
                onClick={() => runScheduleMutation.mutate(schedule.id)}
              >
                运行
              </Button>
              <Button
                size="small"
                icon={<EditOutlined />}
                onClick={() =>
                  navigate(
                    aiAssetsChildPath(`/ai-assets/agent-schedules/${schedule.id}/edit`, "schedules")
                  )
                }
              >
                编辑
              </Button>
              <Button
                size="small"
                loading={
                  updateScheduleMutation.isPending &&
                  updateScheduleMutation.variables?.schedule.id === schedule.id
                }
                onClick={() =>
                  updateScheduleMutation.mutate({ schedule, enabled: !schedule.enabled })
                }
              >
                {schedule.enabled ? "停用" : "启用"}
              </Button>
              <Popconfirm
                title="删除定时报告任务"
                description="删除后不会再自动生成对应报告。"
                okText="删除"
                cancelText="取消"
                onConfirm={() => deleteScheduleMutation.mutate(schedule.id)}
              >
                <Button size="small" danger icon={<DeleteOutlined />}>
                  删除
                </Button>
              </Popconfirm>
            </div>
          </article>
        ))}
      </div>
    );

  return (
    <PagePanel
      title="我的 AI 资产"
      description="管理个人 Agent、定时报告任务和运行记录"
      breadcrumbs={[{ title: "系统" }, { title: "我的 AI 资产" }]}
      className="ai-assets-page aidashboard-list"
      actions={operations}
    >
      <RequirementMetricGrid>
        <RequirementMetricCard
          tone="success"
          icon={<RobotOutlined />}
          loading={agentsQuery.isLoading}
          metric={{
            key: "agents",
            title: "Agents",
            value: agents.length,
            description: "个人 + 系统"
          }}
        />
        {PERSONAL_RESOURCE_MANAGEMENT_VISIBLE ? (
          <>
            <RequirementMetricCard
              tone="primary"
              icon={<ToolOutlined />}
              loading={skillsQuery.isLoading}
              metric={{
                key: "skills",
                title: "Skills",
                value: visibleSkills.length,
                description: "我的"
              }}
            />
            <RequirementMetricCard
              tone="info"
              icon={<CloudServerOutlined />}
              loading={mcpQuery.isLoading}
              metric={{
                key: "mcp",
                title: "MCP",
                value: visibleMCPEntries.length,
                description: "我的"
              }}
            />
          </>
        ) : null}
        <RequirementMetricCard
          tone="warning"
          icon={<ClockCircleOutlined />}
          loading={schedulesQuery.isLoading}
          metric={{
            key: "schedules",
            title: "定时报告任务",
            value: schedules.length,
            description: `${schedules.filter((item) => item.enabled).length} 个启用`
          }}
        />
      </RequirementMetricGrid>

      {platformIssue ? (
        <Alert
          className="ai-assets-platform-alert"
          type={platformIssue.code === "MANAGED_AGENT_NOT_CONFIGURED" ? "warning" : "error"}
          showIcon
          message={platformIssue.title}
          description={platformIssue.description}
        />
      ) : null}

      <Tabs
        className="ai-assets-tabs"
        activeKey={tab}
        onChange={(key) => setAssetTab(key as AssetTab)}
        items={[
          {
            key: "agents",
            label: "Agents",
            children: (
              <>
                <div className="ai-assets-table-card ai-assets-table-card--desktop">
                  <ResourceTable<ManagedAgent>
                    rowKey="agent_id"
                    columns={agentColumns}
                    dataSource={agents}
                    loading={agentsQuery.isLoading}
                    pagination={aiAssetsTablePagination(agents.length)}
                    locale={{ emptyText: agentEmptyState }}
                  />
                </div>
                {agentMobileList}
              </>
            )
          },
          ...(PERSONAL_RESOURCE_MANAGEMENT_VISIBLE
            ? [
                {
                  key: "skills",
                  label: "我的 Skills",
                  children: (
                    <>
                      <div className="ai-assets-table-card ai-assets-table-card--desktop">
                        <ResourceTable<ManagedSkill>
                          rowKey={(record) =>
                            record.skill_id || refKey(record.owner, record.slug, record.version)
                          }
                          columns={skillColumns}
                          dataSource={visibleSkills}
                          loading={skillsQuery.isLoading}
                          pagination={aiAssetsTablePagination(visibleSkills.length)}
                          locale={{ emptyText: skillEmptyState }}
                        />
                      </div>
                      {skillMobileList}
                    </>
                  )
                },
                {
                  key: "mcp",
                  label: "我的 MCP",
                  children: (
                    <>
                      <div className="ai-assets-table-card ai-assets-table-card--desktop">
                        <ResourceTable<ManagedMCPEntry>
                          rowKey={(record) =>
                            record.entry_id || refKey(record.owner, record.slug, record.version)
                          }
                          columns={mcpColumns}
                          dataSource={visibleMCPEntries}
                          loading={mcpQuery.isLoading}
                          pagination={aiAssetsTablePagination(visibleMCPEntries.length)}
                          locale={{ emptyText: mcpEmptyState }}
                        />
                      </div>
                      {mcpMobileList}
                    </>
                  )
                }
              ]
            : []),
          {
            key: "schedules",
            label: "定时报告任务",
            children: (
              <>
                <div className="ai-assets-table-card ai-assets-table-card--desktop">
                  <ResourceTable<ManagedAgentSchedule>
                    rowKey="id"
                    columns={scheduleColumns}
                    dataSource={schedules}
                    loading={schedulesQuery.isLoading}
                    pagination={aiAssetsTablePagination(schedules.length)}
                    locale={{ emptyText: scheduleEmptyState }}
                  />
                </div>
                {scheduleMobileList}
              </>
            )
          },
          {
            key: "runs",
            label: "运行历史",
            children: (
              <section className="ai-assets-history">
                <header>
                  <strong>最近运行记录</strong>
                  <Button
                    size="small"
                    icon={<ReloadOutlined />}
                    onClick={() => void runsQuery.refetch()}
                  >
                    刷新
                  </Button>
                </header>
                <div className="ai-assets-table-card ai-assets-table-card--desktop">
                  <ResourceTable<AIRun>
                    rowKey="id"
                    columns={runColumns}
                    dataSource={runs}
                    loading={runsQuery.isLoading}
                    pagination={aiAssetsTablePagination(runs.length)}
                    locale={{
                      emptyText: (
                        <AssetEmptyState icon={<PlayCircleOutlined />} title="暂无运行记录" />
                      )
                    }}
                  />
                </div>
                {runMobileList}
              </section>
            )
          }
        ]}
      />

      <Modal
        className="ai-assets-skill-modal"
        title={viewingSkill ? `${viewingSkill.name || viewingSkill.slug} / SKILL.md` : "SKILL.md"}
        open={Boolean(viewingSkill)}
        width={860}
        footer={<Button onClick={() => setViewingSkill(null)}>关闭</Button>}
        onCancel={() => setViewingSkill(null)}
        destroyOnHidden
      >
        {skillMarkdownQuery.isLoading ? (
          <Empty description="正在加载 SKILL.md" />
        ) : skillMarkdownQuery.error ? (
          <Alert
            type="error"
            showIcon
            message="读取 SKILL.md 失败"
            description={errorMessage(skillMarkdownQuery.error)}
          />
        ) : (
          <pre className="ai-assets-skill-md-viewer">
            {skillMarkdownQuery.data?.content || "SKILL.md 为空"}
          </pre>
        )}
      </Modal>
    </PagePanel>
  );
}
