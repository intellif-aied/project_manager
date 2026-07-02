import {
  Alert,
  App,
  Button,
  Card,
  DatePicker,
  Empty,
  Input,
  Modal,
  Select,
  Space,
  Spin,
  Tag,
  Table,
  Tooltip
} from "antd";
import { PlayCircleOutlined } from "@ant-design/icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import dayjs from "dayjs";
import type { Dayjs } from "dayjs";
import { useEffect, useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";

import {
  createManagedCredential,
  fetchManagedAgentRun,
  fetchManagedAgents,
  fetchManagedCredentials,
  fetchSessionTokens,
  startManagedAgentRun,
  startReportAgentRun
} from "../../api/client";
import type {
  AIRun,
  ManagedAgent,
  ManagedCredential,
  ManagedReportAgentUnavailable,
  ManagedReportAgentRunResponse,
  ReportType,
  SessionTokens
} from "../../api/types";
import {
  AI_ASSETS_HOME,
  aiAssetsPath,
  errorMessage,
  extractPromptVariables,
  isReportAgentAsset,
  REPORT_AGENT_MARKER,
  REPORT_SYSTEM_MCP_SLUG,
  REPORT_SYSTEM_MCP_VERSION,
  reportAgentMarkerText,
  renderPromptPreview
} from "../utils/agentAssets";
import { PagePanel } from "@/shared/components/PagePanel/PagePanel";
import { useAuth } from "@/shared/auth/authContext";
import type { UserRole } from "@/shared/auth/types";

import "../components/AgentWorkspace.css";

const AI_ASSETS_RETURN_PATH = aiAssetsPath("agents");

const REPORT_TYPES_MARKER = "AIDA_REPORT_AGENT_TYPES:";
const REPORT_SYSTEM_CREDENTIAL_SLOT = "AIDA_REPORT_MCP_AUTH";
const REPORT_SYSTEM_PROMPT_KEYS = new Set([
  "report_type",
  "period_json",
  "target_json",
  "selected_session_slice_keys",
  "selected_session_slice_keys_json",
  "run_id",
  "mcp_url",
  "credential_slot",
  REPORT_SYSTEM_CREDENTIAL_SLOT
]);

const REPORT_TYPE_OPTIONS: Array<{ label: string; value: ReportType; roles: UserRole[] }> = [
  {
    label: "个人日报",
    value: "personal_daily",
    roles: ["employee", "pm", "team_leader", "director", "admin"]
  },
  {
    label: "个人周报",
    value: "personal_weekly",
    roles: ["employee", "pm", "team_leader", "director", "admin"]
  },
  { label: "小组日报", value: "team_daily", roles: ["team_leader", "admin"] },
  { label: "小组周报", value: "team_weekly", roles: ["team_leader", "admin"] },
  { label: "部门日报", value: "department_daily", roles: ["director", "admin"] },
  { label: "部门周报", value: "department_weekly", roles: ["director", "admin"] }
];

function isWeeklyReportType(type: ReportType) {
  return type.endsWith("_weekly");
}

function supportedReportTypes(agent: ManagedAgent): ReportType[] {
  if (agent.business_type === "report") {
    return agent.report_types?.length
      ? agent.report_types
      : REPORT_TYPE_OPTIONS.map((item) => item.value);
  }
  if (agent.business_type === "generic") {
    return [];
  }
  const text = reportAgentMarkerText(agent);
  if (!text.includes(REPORT_AGENT_MARKER)) return [];
  const markerLine = text
    .split("\n")
    .map((line) => line.trim())
    .find((line) => line.startsWith(REPORT_TYPES_MARKER));
  if (!markerLine) return REPORT_TYPE_OPTIONS.map((item) => item.value);
  const supported = markerLine
    .slice(REPORT_TYPES_MARKER.length)
    .split(",")
    .map((item) => item.trim())
    .filter((item): item is ReportType =>
      REPORT_TYPE_OPTIONS.some((option) => option.value === item)
    );
  return supported.length > 0 ? supported : REPORT_TYPE_OPTIONS.map((item) => item.value);
}

function reportTypeOptionsForUser(agent: ManagedAgent, role?: UserRole) {
  const supported = supportedReportTypes(agent);
  if (supported.length === 0 || !role) return [];
  return REPORT_TYPE_OPTIONS.filter(
    (option) => supported.includes(option.value) && option.roles.includes(role)
  );
}

function isReportAgent(agent: ManagedAgent) {
  return isReportAgentAsset(agent) || supportedReportTypes(agent).length > 0;
}

function defaultWeekRange(): [Dayjs, Dayjs] {
  const today = dayjs();
  const weekday = today.day() === 0 ? 7 : today.day();
  const weekStart = today.subtract(weekday - 1, "day").startOf("day");
  return [weekStart, weekStart.add(6, "day")];
}

function reportPeriodPayload(reportType: ReportType, reportDate: Dayjs, weekRange: [Dayjs, Dayjs]) {
  return isWeeklyReportType(reportType)
    ? {
        week_start: weekRange[0].format("YYYY-MM-DD"),
        week_end: weekRange[1].format("YYYY-MM-DD")
      }
    : { date: reportDate.format("YYYY-MM-DD") };
}

function reportSessionRange(reportType: ReportType, reportDate: Dayjs, weekRange: [Dayjs, Dayjs]) {
  if (isWeeklyReportType(reportType)) {
    return {
      from: weekRange[0].format("YYYY-MM-DD"),
      to: weekRange[1].format("YYYY-MM-DD")
    };
  }
  const date = reportDate.format("YYYY-MM-DD");
  return { from: date, to: date };
}

function isReportAgentUnavailable(
  response: ManagedReportAgentRunResponse
): response is ManagedReportAgentUnavailable {
  return "available" in response && response.available === false;
}

function cleanAgentDescription(agent: ManagedAgent, reportAgent: boolean) {
  const description = (agent.description || "")
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line && !line.startsWith("AIDA_"))
    .join("\n");
  if (description) return description;
  if (reportAgent)
    return "标准 Agent 的 Aida 报告业务用途，运行时由 Aida 注入报告上下文和 Report MCP。";
  return "暂无描述";
}

function missingPromptReason(keys: string[]) {
  if (keys.length === 0) return "";
  const shown = keys.slice(0, 3).join("、");
  const suffix = keys.length > 3 ? ` 等 ${keys.length} 项` : "";
  return `请先填写必填的 Start Prompt Values：${shown}${suffix}`;
}

function modelCardTitle(required: boolean) {
  return (
    <span className="ai-assets-required-title">
      模型
      {required ? <em>*</em> : null}
    </span>
  );
}

function reportRuntimeCredentialSlots(agent: ManagedAgent) {
  const slots = new Set<string>();
  if (isReportAgent(agent)) {
    slots.add(REPORT_SYSTEM_CREDENTIAL_SLOT);
  }
  for (const server of agent.mcp_servers ?? []) {
    const slot = server.credential_slot?.trim();
    if (server.name === REPORT_SYSTEM_MCP_SLUG && slot) slots.add(slot);
  }
  for (const binding of agent.mcp_bindings ?? []) {
    const isReportMCP =
      binding.slug === REPORT_SYSTEM_MCP_SLUG &&
      (!binding.version || binding.version === REPORT_SYSTEM_MCP_VERSION);
    const slot = binding.credential_slot?.trim();
    if (isReportMCP && slot) slots.add(slot);
  }
  return slots;
}

interface RuntimeCredentialSlot {
  name: string;
  label: string;
  defaultCredentialId: string;
}

function runtimeCredentialSlots(agent: ManagedAgent): RuntimeCredentialSlot[] {
  const reportSlots = reportRuntimeCredentialSlots(agent);
  const defaultBindings = agent.default_bindings ?? {};
  const mcpLabels = new Map<string, string>();
  for (const server of agent.mcp_servers ?? []) {
    const slot = server.credential_slot?.trim();
    if (!slot) continue;
    mcpLabels.set(slot, server.name);
  }
  for (const binding of agent.mcp_bindings ?? []) {
    const slot = binding.credential_slot?.trim();
    if (!slot) continue;
    const owner = binding.owner ? `${binding.owner}/` : "";
    mcpLabels.set(slot, `${owner}${binding.slug}@${binding.version}`);
  }
  return (agent.credential_slots ?? []).flatMap((slot) => {
    const name = slot.name.trim();
    if (!slot.required || !name || reportSlots.has(name)) return [];
    return [
      {
        name,
        label: mcpLabels.get(name) ?? name,
        defaultCredentialId: defaultBindings[name]?.trim() ?? ""
      }
    ];
  });
}

function formatCredentialSlots(slots: string[]) {
  if (slots.length <= 3) return slots.join("、");
  return `${slots.slice(0, 3).join("、")} 等 ${slots.length} 项`;
}

function isSelectableCredential(credential: ManagedCredential) {
  const purpose = credential.metadata?.purpose ?? "";
  return !credential.archived && !purpose.includes("report_mcp_auth");
}

function missingRuntimeCredentialSlots(
  slots: RuntimeCredentialSlot[],
  overrides: Record<string, string>
) {
  return slots
    .filter((slot) => !slot.defaultCredentialId && !overrides[slot.name]?.trim())
    .map((slot) => slot.name);
}

function credentialDisabledReason(slots: string[], credentialsLoading: boolean) {
  if (credentialsLoading) return "正在加载 MCP 凭证，请稍候。";
  return `请先为以下 MCP 凭证项选择已保存凭证：${formatCredentialSlots(slots)}。`;
}

function compactCredentialOverrides(overrides: Record<string, string>) {
  return Object.fromEntries(
    Object.entries(overrides)
      .map(([slot, credentialId]) => [slot.trim(), credentialId.trim()])
      .filter(([slot, credentialId]) => slot && credentialId)
  );
}

function formatTokens(value: number) {
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(2)}M`;
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}K`;
  return String(value);
}

function formatDateTime(iso?: string) {
  if (!iso) return "-";
  return new Date(iso).toLocaleString("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit"
  });
}

function realSessionId(session: SessionTokens) {
  return session.local_session_id || session.session_ref || session.session_id;
}

function sessionSliceKey(session: SessionTokens) {
  return (
    session.slice_key ||
    `${session.session_id}:${session.activity_date || session.activity_start_at || session.started_at}`
  );
}

function formatActivityRange(session: SessionTokens) {
  const start = session.activity_start_at || session.started_at;
  const end = session.activity_end_at;
  if (!end || end === start) return formatDateTime(start);
  return `${formatDateTime(start)} ~ ${formatDateTime(end)}`;
}

function sessionSelectLabel(session: SessionTokens) {
  const summary = session.summary ? ` · ${session.summary}` : "";
  return `${realSessionId(session)} · ${formatActivityRange(session)}${summary}`;
}

function compactSelectedRecords(
  keys: string[],
  records: Record<string, SessionTokens>
): Record<string, SessionTokens> {
  const next: Record<string, SessionTokens> = {};
  for (const key of keys) {
    if (records[key]) next[key] = records[key];
  }
  return next;
}

function SessionSliceSelector({
  from,
  to,
  selectedKeys,
  selectedRecords,
  onChange
}: {
  from: string;
  to: string;
  selectedKeys: string[];
  selectedRecords: Record<string, SessionTokens>;
  onChange: (keys: string[], records: Record<string, SessionTokens>) => void;
}) {
  const [open, setOpen] = useState(false);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(8);

  useEffect(() => {
    setPage(1);
  }, [from, to]);

  const sessionsQuery = useQuery({
    queryKey: ["report-session-slices", from, to, page, pageSize],
    queryFn: () =>
      fetchSessionTokens({
        from,
        to,
        scope: "mine",
        page: String(page),
        page_size: String(pageSize)
      }),
    enabled: open,
    placeholderData: (previousData) => previousData,
    staleTime: 30_000
  });

  const sessions = sessionsQuery.data?.items ?? [];
  const selectedItems = useMemo(
    () =>
      selectedKeys.map((key) => ({
        key,
        label: selectedRecords[key] ? sessionSelectLabel(selectedRecords[key]) : key
      })),
    [selectedKeys, selectedRecords]
  );
  const removeSelectedKey = (key: string) => {
    const normalized = selectedKeys.filter((item) => item !== key);
    onChange(normalized, compactSelectedRecords(normalized, selectedRecords));
  };

  return (
    <Card title="Session 切片（可选）" className="ai-assets-editor-section">
      <div className="ai-assets-session-selector">
        <div className="ai-assets-session-selector__control">
          <div className="ai-assets-session-selector__panel">
            {selectedItems.length > 0 ? (
              <div className="ai-assets-session-selector__tags">
                {selectedItems.map((item) => (
                  <Tag
                    key={item.key}
                    closable
                    className="ai-assets-session-selector__tag"
                    onClose={(event) => {
                      event.preventDefault();
                      removeSelectedKey(item.key);
                    }}
                  >
                    <span className="ai-assets-session-selector__tag-text">{item.label}</span>
                  </Tag>
                ))}
              </div>
            ) : (
              <span className="ai-assets-session-selector__empty">
                默认使用当前报告周期内的全部 Session
              </span>
            )}
          </div>
          <Button onClick={() => setOpen(true)}>选择 Session</Button>
        </div>
        <p className="ai-assets-field-help">
          不选择时，报告会按当前日期或周期自动取数；选择后只使用选中的 Session 切片。
        </p>
      </div>

      <Modal
        title="选择 Session 切片"
        open={open}
        width={920}
        className="ai-assets-session-modal"
        onCancel={() => setOpen(false)}
        onOk={() => setOpen(false)}
        okText="完成"
        cancelText="取消"
      >
        <div className="ai-assets-session-modal__summary">
          <span>
            {from === to ? from : `${from} ~ ${to}`} · 已选 {selectedKeys.length} 条
          </span>
          {selectedKeys.length > 0 ? (
            <Button size="small" onClick={() => onChange([], {})}>
              清空选择
            </Button>
          ) : null}
        </div>
        <Table<SessionTokens>
          rowKey={sessionSliceKey}
          dataSource={sessions}
          loading={sessionsQuery.isLoading || sessionsQuery.isFetching}
          size="middle"
          scroll={{ x: 820 }}
          rowSelection={{
            selectedRowKeys: selectedKeys,
            preserveSelectedRowKeys: true,
            onChange: (keys, rows) => {
              const normalized = keys.map(String);
              const nextRecords = { ...selectedRecords };
              rows.forEach((row) => {
                nextRecords[sessionSliceKey(row)] = row;
              });
              Object.keys(nextRecords).forEach((key) => {
                if (!normalized.includes(key)) delete nextRecords[key];
              });
              onChange(normalized, nextRecords);
            }
          }}
          pagination={{
            current: sessionsQuery.data?.page ?? page,
            pageSize: sessionsQuery.data?.page_size ?? pageSize,
            total: sessionsQuery.data?.total ?? 0,
            showSizeChanger: true,
            showTotal: (total) => `共 ${total} 条`,
            onChange: (nextPage, nextPageSize) => {
              setPage(nextPage);
              setPageSize(nextPageSize);
            }
          }}
          columns={[
            {
              title: "真实 Session ID",
              key: "session",
              width: 300,
              render: (_: unknown, session) => (
                <span className="ai-assets-session-id">{realSessionId(session)}</span>
              )
            },
            {
              title: "摘要",
              dataIndex: "summary",
              width: 260,
              render: (summary?: string) =>
                summary ? (
                  <span className="ai-assets-session-summary" title={summary}>
                    {summary}
                  </span>
                ) : (
                  "-"
                )
            },
            {
              title: "活动时间",
              key: "activity",
              width: 190,
              render: (_: unknown, session) => formatActivityRange(session)
            },
            {
              title: "Total",
              dataIndex: "total_tokens",
              align: "right" as const,
              width: 90,
              render: (value: number) => formatTokens(value)
            }
          ]}
          locale={{ emptyText: <Empty description="当前报告周期暂无 Session 切片" /> }}
        />
      </Modal>
    </Card>
  );
}

function RuntimeCredentialCard({
  slots,
  credentials,
  values,
  loading,
  onChange
}: {
  slots: RuntimeCredentialSlot[];
  credentials: ManagedCredential[];
  values: Record<string, string>;
  loading: boolean;
  onChange: (slot: string, credentialId: string) => void;
}) {
  const queryClient = useQueryClient();
  const { message } = App.useApp();
  const [credentialName, setCredentialName] = useState("");
  const [credentialValue, setCredentialValue] = useState("");
  const createCredentialMutation = useMutation({
    mutationFn: () =>
      createManagedCredential({
        name: credentialName.trim(),
        value: credentialValue
      }),
    onSuccess: (result) => {
      message.success("凭证已保存");
      setCredentialName("");
      setCredentialValue("");
      const firstMissing = slots.find((slot) => !slot.defaultCredentialId && !values[slot.name]);
      if (firstMissing) {
        onChange(firstMissing.name, result.credential_id);
      }
      void queryClient.invalidateQueries({ queryKey: ["managed-credentials"] });
    },
    onError: (err: unknown) => message.error(errorMessage(err))
  });
  if (slots.length === 0) return null;
  const selectableCredentials = credentials.filter(isSelectableCredential);
  const canCreateCredential = credentialName.trim().length > 0 && credentialValue.trim().length > 0;
  return (
    <Card title="MCP 凭证" className="ai-assets-editor-section">
      <div className="ai-assets-runtime-credentials">
        {slots.map((slot) => {
          const hasDefault = Boolean(slot.defaultCredentialId);
          const value = values[slot.name] ?? "";
          return (
            <label key={slot.name} className="ai-assets-runtime-credential">
              <span>
                <strong>{slot.label}</strong>
                <em>{slot.name}</em>
              </span>
              <Select
                loading={loading}
                value={value}
                showSearch
                optionFilterProp="label"
                placeholder="选择已保存凭证"
                options={[
                  {
                    label: hasDefault ? "使用 Agent 默认凭证" : "请选择已保存凭证",
                    value: "",
                    disabled: !hasDefault
                  },
                  ...selectableCredentials.map((credential) => ({
                    label: credential.name,
                    value: credential.credential_id
                  }))
                ]}
                onChange={(credentialId) => onChange(slot.name, credentialId)}
              />
            </label>
          );
        })}
      </div>
      <p className="ai-assets-field-help">
        报告能力所需凭证由平台处理；如果 Agent 还连接了其他 MCP，可在这里选择对应凭证。
      </p>
      <div className="ai-assets-runtime-credential-create">
        <Input
          value={credentialName}
          onChange={(event) => setCredentialName(event.target.value)}
          placeholder="凭证名称"
        />
        <Input.Password
          value={credentialValue}
          onChange={(event) => setCredentialValue(event.target.value)}
          placeholder="凭证值"
        />
        <Button
          loading={createCredentialMutation.isPending}
          disabled={!canCreateCredential || createCredentialMutation.isPending}
          onClick={() => createCredentialMutation.mutate()}
        >
          保存凭证
        </Button>
      </div>
      {!loading && selectableCredentials.length === 0 ? (
        <Alert type="warning" showIcon message="当前没有可用的已保存凭证" />
      ) : null}
    </Card>
  );
}

function RunStatusCard({ run }: { run?: AIRun }) {
  return (
    <Card title="运行状态" className="ai-assets-editor-section">
      <div className="ai-assets-runner__status">
        <strong>状态</strong>
        <Tag
          color={run?.status === "succeeded" ? "green" : run?.status === "failed" ? "red" : "blue"}
        >
          {run?.status || "未提交"}
        </Tag>
        {run?.external_task_id ? <span>Task: {run.external_task_id}</span> : null}
        {run?.external_session_id ? <span>Session: {run.external_session_id}</span> : null}
      </div>
      {run?.error_message ? (
        <pre className="ai-assets-runner__result is-error">{run.error_message}</pre>
      ) : (
        <pre className="ai-assets-runner__result">
          {run?.result || "运行完成后，结果会显示在这里。"}
        </pre>
      )}
    </Card>
  );
}

function AgentContextCard({ agent, reportAgent }: { agent: ManagedAgent; reportAgent: boolean }) {
  const description = cleanAgentDescription(agent, reportAgent);
  const version = agent.current_version_id || agent.managed_version || "-";
  return (
    <Card className="ai-assets-run-context">
      <div className="ai-assets-run-context__head">
        <h3>{agent.name}</h3>
        {reportAgent ? <Tag color="purple">Report Agent</Tag> : <Tag>普通 Agent</Tag>}
      </div>
      <p>{description}</p>
      <dl className="ai-assets-run-meta-list">
        <div>
          <dt>Engine</dt>
          <dd>{agent.engine || "-"}</dd>
        </div>
        <div>
          <dt>默认模型</dt>
          <dd>{agent.default_model_id || "未配置"}</dd>
        </div>
        <div>
          <dt>版本</dt>
          <dd>{version}</dd>
        </div>
      </dl>
    </Card>
  );
}

function GenericAgentRunForm({ agent }: { agent: ManagedAgent }) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { message } = App.useApp();

  const template = agent.start_prompt_template?.trim() || "";
  const promptVariables = useMemo(() => extractPromptVariables(template), [template]);

  const [startPromptValues, setStartPromptValues] = useState<Record<string, string>>(() =>
    Object.fromEntries(promptVariables.map((key) => [key, ""]))
  );
  const [runMessage, setRunMessage] = useState("");
  const [runModelId, setRunModelId] = useState("");
  const [activeRunId, setActiveRunId] = useState<string>();
  const [credentialOverrides, setCredentialOverrides] = useState<Record<string, string>>({});

  const activeRunQuery = useQuery<AIRun>({
    queryKey: ["managed-agent-run", activeRunId],
    queryFn: () => fetchManagedAgentRun(activeRunId as string),
    enabled: Boolean(activeRunId),
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status === "pending" || status === "running" ? 2500 : false;
    }
  });
  const credentialsQuery = useQuery({
    queryKey: ["managed-credentials"],
    queryFn: () => fetchManagedCredentials(),
    staleTime: 30_000
  });

  const defaultModelId = agent.default_model_id?.trim() || "";
  const modelId = runModelId.trim() || defaultModelId;
  const missingPromptVariables = promptVariables.filter((key) => !startPromptValues[key]?.trim());
  const promptPreview = template ? renderPromptPreview(template, startPromptValues) : "";
  const hasPromptInput =
    promptVariables.length > 0 ? missingPromptVariables.length === 0 : runMessage.trim().length > 0;
  const credentialSlots = useMemo(() => runtimeCredentialSlots(agent), [agent]);
  const credentials = useMemo(
    () => credentialsQuery.data?.credentials ?? [],
    [credentialsQuery.data]
  );
  const cleanedCredentialOverrides = useMemo(
    () => compactCredentialOverrides(credentialOverrides),
    [credentialOverrides]
  );
  const missingCredentialSlots = useMemo(
    () => missingRuntimeCredentialSlots(credentialSlots, cleanedCredentialOverrides),
    [cleanedCredentialOverrides, credentialSlots]
  );
  const needsRuntimeCredential = credentialSlots.some((slot) => !slot.defaultCredentialId);
  const credentialReason =
    needsRuntimeCredential && credentialsQuery.isLoading
      ? credentialDisabledReason(missingCredentialSlots, true)
      : missingCredentialSlots.length
        ? credentialDisabledReason(missingCredentialSlots, false)
        : "";
  const baseRunDisabledReason = agent.archived
    ? "该 Agent 已删除，不能发起新的运行。"
    : credentialReason
      ? credentialReason
      : activeRunQuery.isFetching
        ? "正在同步运行状态，请稍候。"
        : !modelId
          ? "请先填写模型 ID，或在 Agent 配置中设置默认模型。"
          : missingPromptVariables.length > 0
            ? missingPromptReason(missingPromptVariables)
            : !hasPromptInput
              ? "请先填写 Initial Message。"
              : "";
  const canRun = !baseRunDisabledReason;

  const runMutation = useMutation({
    mutationFn: () => {
      const messageText = template ? renderPromptPreview(template, startPromptValues) : runMessage;
      return startManagedAgentRun(agent.agent_id, {
        message: messageText,
        model_id: modelId,
        params: promptVariables.length ? startPromptValues : undefined,
        credential_overrides:
          Object.keys(cleanedCredentialOverrides).length > 0
            ? cleanedCredentialOverrides
            : undefined
      });
    },
    onSuccess: (run) => {
      message.success("Agent 已提交运行");
      setActiveRunId(run.id);
      void queryClient.invalidateQueries({ queryKey: ["managed-agent-runs"] });
    },
    onError: (err: unknown) => message.error(errorMessage(err))
  });
  const runDisabledReason = runMutation.isPending
    ? "正在提交运行请求，请稍候。"
    : baseRunDisabledReason;

  return (
    <section className="ai-assets-workspace">
      <div className="ai-assets-run-layout">
        <AgentContextCard agent={agent} reportAgent={false} />
        <div className="ai-assets-run-form">
          <RuntimeCredentialCard
            slots={credentialSlots}
            credentials={credentials}
            values={credentialOverrides}
            loading={credentialsQuery.isLoading}
            onChange={(slot, credentialId) =>
              setCredentialOverrides((current) => ({ ...current, [slot]: credentialId }))
            }
          />

          {promptVariables.length > 0 ? (
            <Card title="Start Prompt Values" className="ai-assets-editor-section">
              <div className="ai-assets-prompt-values">
                {promptVariables.map((key) => (
                  <label key={key} className="ai-assets-prompt-field">
                    <span>
                      {key}
                      <em>*</em>
                    </span>
                    <Input.TextArea
                      rows={2}
                      value={startPromptValues[key] || ""}
                      onChange={(event) =>
                        setStartPromptValues((current) => ({
                          ...current,
                          [key]: event.target.value
                        }))
                      }
                    />
                  </label>
                ))}
              </div>
            </Card>
          ) : (
            <Card title="Initial Message" className="ai-assets-editor-section">
              <Input.TextArea
                rows={5}
                value={runMessage}
                onChange={(event) => setRunMessage(event.target.value)}
                placeholder="这个 Agent 未配置 Start Prompt Template，请直接输入初始消息。"
              />
            </Card>
          )}

          <Card title={modelCardTitle(!defaultModelId)} className="ai-assets-editor-section">
            <Input
              value={runModelId}
              onChange={(event) => setRunModelId(event.target.value)}
              placeholder={
                defaultModelId
                  ? `留空使用 Agent 默认模型：${defaultModelId}`
                  : "必填：请输入模型 ID"
              }
            />
            <p className="ai-assets-field-help">
              {defaultModelId
                ? "已配置默认模型，留空将使用 Agent 默认模型。"
                : "此 Agent 未配置默认模型，本次运行必须填写模型 ID。"}
            </p>
          </Card>

          {template ? (
            <Card title="Prompt 预览" className="ai-assets-editor-section">
              <pre className="ai-assets-prompt-preview">{promptPreview}</pre>
            </Card>
          ) : null}

          <RunStatusCard run={activeRunQuery.data} />

          <div className="ai-assets-workspace__actions">
            {runDisabledReason ? (
              <span className="ai-assets-run-disabled-message">{runDisabledReason}</span>
            ) : null}
            <Space>
              <Button onClick={() => navigate(AI_ASSETS_RETURN_PATH)}>返回</Button>
              <Tooltip title={runDisabledReason || undefined}>
                <span className="ai-assets-run-disabled-tip" aria-disabled={!canRun}>
                  <Button
                    type="primary"
                    icon={<PlayCircleOutlined />}
                    loading={runMutation.isPending}
                    disabled={!canRun || runMutation.isPending}
                    onClick={() => runMutation.mutate()}
                  >
                    运行
                  </Button>
                </span>
              </Tooltip>
            </Space>
          </div>
        </div>
      </div>
    </section>
  );
}

function ReportAgentRunForm({ agent }: { agent: ManagedAgent }) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { user } = useAuth();
  const { message } = App.useApp();

  const template = agent.start_prompt_template?.trim() || "";
  const promptVariables = useMemo(() => extractPromptVariables(template), [template]);
  const userPromptVariables = useMemo(
    () => promptVariables.filter((key) => !REPORT_SYSTEM_PROMPT_KEYS.has(key)),
    [promptVariables]
  );
  const options = useMemo(() => reportTypeOptionsForUser(agent, user?.role), [agent, user?.role]);
  const [startPromptValues, setStartPromptValues] = useState<Record<string, string>>(() =>
    Object.fromEntries(userPromptVariables.map((key) => [key, ""]))
  );
  const [runMessage, setRunMessage] = useState("");
  const [reportTypeInput, setReportTypeInput] = useState<ReportType>("personal_daily");
  const [reportDate, setReportDate] = useState<Dayjs>(dayjs());
  const [weekRange, setWeekRange] = useState<[Dayjs, Dayjs]>(() => defaultWeekRange());
  const [runModelId, setRunModelId] = useState("");
  const [activeRunId, setActiveRunId] = useState<string>();
  const [credentialOverrides, setCredentialOverrides] = useState<Record<string, string>>({});
  const [selectedSessionSliceKeys, setSelectedSessionSliceKeys] = useState<string[]>([]);
  const [selectedSessionRecords, setSelectedSessionRecords] = useState<
    Record<string, SessionTokens>
  >({});

  const activeRunQuery = useQuery<AIRun>({
    queryKey: ["managed-agent-run", activeRunId],
    queryFn: () => fetchManagedAgentRun(activeRunId as string),
    enabled: Boolean(activeRunId),
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status === "pending" || status === "running" ? 2500 : false;
    }
  });
  const credentialsQuery = useQuery({
    queryKey: ["managed-credentials"],
    queryFn: () => fetchManagedCredentials(),
    staleTime: 30_000
  });

  const reportType = options.some((option) => option.value === reportTypeInput)
    ? reportTypeInput
    : (options[0]?.value ?? reportTypeInput);
  const period = reportPeriodPayload(reportType, reportDate, weekRange);
  const sessionRange = reportSessionRange(reportType, reportDate, weekRange);
  useEffect(() => {
    setSelectedSessionSliceKeys([]);
    setSelectedSessionRecords({});
  }, [sessionRange.from, sessionRange.to]);
  const defaultModelId = agent.default_model_id?.trim() || "";
  const modelId = runModelId.trim() || defaultModelId;
  const missingPromptVariables = userPromptVariables.filter(
    (key) => !startPromptValues[key]?.trim()
  );
  const promptPreviewValues = useMemo(
    () => ({
      ...startPromptValues,
      report_type: reportType,
      period_json: JSON.stringify(period),
      target_json: JSON.stringify({ type: "self" }),
      selected_session_slice_keys: selectedSessionSliceKeys.join("\n"),
      selected_session_slice_keys_json: JSON.stringify(selectedSessionSliceKeys),
      run_id: "运行时生成",
      credential_slot: REPORT_SYSTEM_CREDENTIAL_SLOT
    }),
    [period, reportType, selectedSessionSliceKeys, startPromptValues]
  );
  const promptPreview = template ? renderPromptPreview(template, promptPreviewValues) : "";
  const credentialSlots = useMemo(() => runtimeCredentialSlots(agent), [agent]);
  const credentials = useMemo(
    () => credentialsQuery.data?.credentials ?? [],
    [credentialsQuery.data]
  );
  const cleanedCredentialOverrides = useMemo(
    () => compactCredentialOverrides(credentialOverrides),
    [credentialOverrides]
  );
  const missingCredentialSlots = useMemo(
    () => missingRuntimeCredentialSlots(credentialSlots, cleanedCredentialOverrides),
    [cleanedCredentialOverrides, credentialSlots]
  );
  const needsRuntimeCredential = credentialSlots.some((slot) => !slot.defaultCredentialId);
  const credentialReason =
    needsRuntimeCredential && credentialsQuery.isLoading
      ? credentialDisabledReason(missingCredentialSlots, true)
      : missingCredentialSlots.length
        ? credentialDisabledReason(missingCredentialSlots, false)
        : "";
  const baseRunDisabledReason = agent.archived
    ? "该 Agent 已删除，不能发起新的运行。"
    : credentialReason
      ? credentialReason
      : activeRunQuery.isFetching
        ? "正在同步运行状态，请稍候。"
        : options.length === 0
          ? "当前账号没有该 Agent 支持的报告类型运行权限。"
          : !modelId
            ? "请先填写模型 ID，或在 Agent 配置中设置默认模型。"
            : missingPromptVariables.length > 0
              ? missingPromptReason(missingPromptVariables)
              : "";
  const canRun = !baseRunDisabledReason;

  const runMutation = useMutation({
    mutationFn: () => {
      return startReportAgentRun(agent.agent_id, {
        report_type: reportType,
        period,
        target: { type: "self" },
        model_id: modelId,
        selected_session_slice_keys: selectedSessionSliceKeys.length
          ? selectedSessionSliceKeys
          : undefined,
        start_prompt_values: userPromptVariables.length ? startPromptValues : undefined,
        message: runMessage.trim() || undefined,
        credential_overrides:
          Object.keys(cleanedCredentialOverrides).length > 0
            ? cleanedCredentialOverrides
            : undefined
      });
    },
    onSuccess: (run) => {
      if (isReportAgentUnavailable(run)) {
        message.warning(run.message || "未配置默认报告 Agent");
        return;
      }
      message.success("Report Agent 已提交运行");
      setActiveRunId(run.id);
      void queryClient.invalidateQueries({ queryKey: ["managed-agent-runs"] });
    },
    onError: (err: unknown) => message.error(errorMessage(err))
  });
  const runDisabledReason = runMutation.isPending
    ? "正在提交运行请求，请稍候。"
    : baseRunDisabledReason;

  return (
    <section className="ai-assets-workspace">
      <div className="ai-assets-run-layout">
        <AgentContextCard agent={agent} reportAgent />
        <div className="ai-assets-run-form">
          <RuntimeCredentialCard
            slots={credentialSlots}
            credentials={credentials}
            values={credentialOverrides}
            loading={credentialsQuery.isLoading}
            onChange={(slot, credentialId) =>
              setCredentialOverrides((current) => ({ ...current, [slot]: credentialId }))
            }
          />

          <Card title="报告参数" className="ai-assets-editor-section">
            {options.length === 0 ? (
              <Alert type="warning" showIcon message="当前账号没有可运行的报告类型" />
            ) : (
              <div className="ai-assets-editor-grid">
                <label className="ai-assets-prompt-field">
                  <span>
                    报告类型<em>*</em>
                  </span>
                  <Select
                    value={reportType}
                    options={options}
                    onChange={(value) => setReportTypeInput(value)}
                  />
                </label>
                {isWeeklyReportType(reportType) ? (
                  <label className="ai-assets-prompt-field">
                    <span>
                      报告周期<em>*</em>
                    </span>
                    <DatePicker.RangePicker
                      value={weekRange}
                      onChange={(value) => {
                        if (value?.[0] && value[1]) {
                          setWeekRange([value[0], value[1]]);
                        }
                      }}
                    />
                  </label>
                ) : (
                  <label className="ai-assets-prompt-field">
                    <span>
                      报告日期<em>*</em>
                    </span>
                    <DatePicker
                      value={reportDate}
                      onChange={(value) => {
                        if (value) setReportDate(value);
                      }}
                    />
                  </label>
                )}
              </div>
            )}
          </Card>

          {userPromptVariables.length > 0 ? (
            <Card title="Start Prompt Values" className="ai-assets-editor-section">
              <div className="ai-assets-prompt-values">
                {userPromptVariables.map((key) => (
                  <label key={key} className="ai-assets-prompt-field">
                    <span>
                      {key}
                      <em>*</em>
                    </span>
                    <Input.TextArea
                      rows={2}
                      value={startPromptValues[key] || ""}
                      onChange={(event) =>
                        setStartPromptValues((current) => ({
                          ...current,
                          [key]: event.target.value
                        }))
                      }
                    />
                  </label>
                ))}
              </div>
            </Card>
          ) : null}

          <SessionSliceSelector
            from={sessionRange.from}
            to={sessionRange.to}
            selectedKeys={selectedSessionSliceKeys}
            selectedRecords={selectedSessionRecords}
            onChange={(keys, records) => {
              setSelectedSessionSliceKeys(keys);
              setSelectedSessionRecords(records);
            }}
          />

          <Card title="Initial Message" className="ai-assets-editor-section">
            <Input.TextArea
              rows={4}
              value={runMessage}
              onChange={(event) => setRunMessage(event.target.value)}
              placeholder="可选：补充本次报告生成要求。"
            />
          </Card>

          <Card title={modelCardTitle(!defaultModelId)} className="ai-assets-editor-section">
            <Input
              value={runModelId}
              onChange={(event) => setRunModelId(event.target.value)}
              placeholder={
                defaultModelId
                  ? `留空使用 Agent 默认模型：${defaultModelId}`
                  : "必填：请输入模型 ID"
              }
            />
            <p className="ai-assets-field-help">
              {defaultModelId
                ? "已配置默认模型，留空将使用 Agent 默认模型。"
                : "此 Agent 未配置默认模型，本次运行必须填写模型 ID。"}
            </p>
          </Card>

          {template ? (
            <Card title="Prompt 预览" className="ai-assets-editor-section">
              <pre className="ai-assets-prompt-preview">{promptPreview}</pre>
            </Card>
          ) : null}

          <RunStatusCard run={activeRunQuery.data} />

          <div className="ai-assets-workspace__actions">
            {runDisabledReason ? (
              <span className="ai-assets-run-disabled-message">{runDisabledReason}</span>
            ) : null}
            <Space>
              <Button onClick={() => navigate(AI_ASSETS_RETURN_PATH)}>返回</Button>
              <Tooltip title={runDisabledReason || undefined}>
                <span className="ai-assets-run-disabled-tip" aria-disabled={!canRun}>
                  <Button
                    type="primary"
                    icon={<PlayCircleOutlined />}
                    loading={runMutation.isPending}
                    disabled={!canRun || runMutation.isPending}
                    onClick={() => runMutation.mutate()}
                  >
                    运行
                  </Button>
                </span>
              </Tooltip>
            </Space>
          </div>
        </div>
      </div>
    </section>
  );
}

export function AgentRunPage() {
  const navigate = useNavigate();
  const { agentId } = useParams<{ agentId: string }>();

  const agentsQuery = useQuery({
    queryKey: ["managed-agents"],
    queryFn: () => fetchManagedAgents(),
    staleTime: 30_000
  });

  const agent = useMemo(
    () => agentsQuery.data?.agents.find((item) => item.agent_id === agentId) ?? null,
    [agentsQuery.data, agentId]
  );

  if (agentsQuery.isLoading) {
    return (
      <PagePanel
        title="运行 Managed Agent"
        description="加载 Agent 中…"
        backTo={AI_ASSETS_RETURN_PATH}
        onBack={() => navigate(AI_ASSETS_RETURN_PATH)}
        onNavigate={(path) => navigate(path)}
        breadcrumbs={[
          { title: "系统" },
          { title: "我的 AI 资产", path: AI_ASSETS_HOME },
          { title: "运行 Agent" }
        ]}
      >
        <Spin />
      </PagePanel>
    );
  }

  if (!agent) {
    return (
      <PagePanel
        title="运行 Managed Agent"
        description="未找到该 Agent"
        backTo={AI_ASSETS_RETURN_PATH}
        onBack={() => navigate(AI_ASSETS_RETURN_PATH)}
        onNavigate={(path) => navigate(path)}
        breadcrumbs={[
          { title: "系统" },
          { title: "我的 AI 资产", path: AI_ASSETS_HOME },
          { title: "运行 Agent" }
        ]}
      >
        <Alert
          type="warning"
          showIcon
          message="未找到该 Agent"
          description="该 Agent 可能已被删除，请返回列表查看。"
        />
      </PagePanel>
    );
  }

  const reportAgent = isReportAgent(agent);

  return (
    <PagePanel
      title={`运行 ${agent.name}`}
      description={
        reportAgent ? "选择报告业务参数后运行 Report Agent。" : "根据 Agent 输入契约填写运行参数。"
      }
      backTo={AI_ASSETS_RETURN_PATH}
      onBack={() => navigate(AI_ASSETS_RETURN_PATH)}
      onNavigate={(path) => navigate(path)}
      breadcrumbs={[
        { title: "系统" },
        { title: "我的 AI 资产", path: AI_ASSETS_HOME },
        { title: agent.name || "运行 Agent" },
        { title: "运行" }
      ]}
    >
      {reportAgent ? (
        <ReportAgentRunForm key={agent.agent_id} agent={agent} />
      ) : (
        <GenericAgentRunForm key={agent.agent_id} agent={agent} />
      )}
    </PagePanel>
  );
}
