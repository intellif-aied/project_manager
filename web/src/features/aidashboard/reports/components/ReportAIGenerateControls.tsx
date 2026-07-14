import {
  CheckCircleOutlined,
  ClearOutlined,
  CloseCircleOutlined,
  FileSearchOutlined,
  LoadingOutlined,
  RobotOutlined
} from "@ant-design/icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { App, Button, DatePicker, Drawer, Modal, Space, Table, Tooltip } from "antd";
import type { ColumnsType } from "antd/es/table";
import type { Dayjs } from "dayjs";
import { useEffect, useMemo, useState } from "react";

import {
  createDefaultReportAgent,
  fetchManagedAgentRun,
  fetchSessionTokens,
  startReportAgentRun
} from "../../api/client";
import type {
  AIRun,
  ManagedReportAgentUnavailable,
  ManagedReportAgentRunResponse,
  ManagedReportAgentRunPayload,
  ReportType,
  SessionTokens
} from "../../api/types";
import { errorMessage } from "../../ai-assets/utils/agentAssets";
import { useAuth } from "@/shared/auth/authContext";
import { HttpError } from "@/shared/request/types";

import "./ReportAIGenerateControls.css";

type ReportPeriodPayload = ManagedReportAgentRunPayload["period"];
type ReportTargetPayload = NonNullable<ManagedReportAgentRunPayload["target"]>;

interface ReportAIGenerateControlsProps {
  reportType: ReportType;
  period: ReportPeriodPayload;
  target: ReportTargetPayload;
  disabled?: boolean;
  allowSessionSelection?: boolean;
  settingsOpen?: boolean;
  selectedSessionSliceKeys?: string[];
  onToggleSettings?: () => void;
  onBeforeGenerate?: () => boolean | Promise<boolean>;
  onGenerated?: (run: AIRun) => void;
}

interface ReportAISettingsPanelProps {
  open: boolean;
  selectedKeys: string[];
  onSelectedKeysChange: (keys: string[]) => void;
  onClose: () => void;
  variant?: "panel" | "drawer";
}

const MAX_SELECTED_SESSION_SLICES = 200;

function formatNumber(value?: number) {
  return typeof value === "number" ? value.toLocaleString() : "-";
}

function sessionSliceKey(record: SessionTokens) {
  if (record.slice_key) return record.slice_key;
  if (record.activity_date) return `${record.session_id}:${record.activity_date}`;
  return record.session_id;
}

function sessionActivityDate(record: SessionTokens) {
  return (
    record.activity_date ||
    record.activity_start_at?.slice(0, 10) ||
    record.started_at?.slice(0, 10) ||
    "-"
  );
}

function reportRunStorageKey(
  userId: string,
  reportType: ReportType,
  period: ReportPeriodPayload,
  target: ReportTargetPayload
) {
  return `aida:report-ai-run:${JSON.stringify({ userId, reportType, period, target })}`;
}

function reportPeriodLabel(period: ReportPeriodPayload) {
  if (period.date) return period.date;
  if (period.week_start && period.week_end) return `${period.week_start} 至 ${period.week_end}`;
  if (period.week_start) return period.week_start;
  return "当前周期";
}

function elapsedLabel(seconds: number) {
  if (seconds < 60) return `${seconds} 秒`;
  const minutes = Math.floor(seconds / 60);
  const rest = seconds % 60;
  return rest > 0 ? `${minutes} 分 ${rest} 秒` : `${minutes} 分钟`;
}

function isReportAgentUnavailable(
  response: ManagedReportAgentRunResponse
): response is ManagedReportAgentUnavailable {
  return "available" in response && response.available === false;
}

function confirmDefaultReportInitialization() {
  return new Promise<boolean>((resolve) => {
    Modal.confirm({
      title: "首次使用需要初始化",
      content: "系统将自动准备日报生成能力，完成后立即生成报告。",
      okText: "立即初始化并生成",
      cancelText: "取消",
      onOk: () => resolve(true),
      onCancel: () => resolve(false)
    });
  });
}

function readStoredRunId(key: string) {
  try {
    return window.localStorage.getItem(key) || undefined;
  } catch {
    return undefined;
  }
}

function storeRunId(key: string, runId: string) {
  try {
    window.localStorage.setItem(key, runId);
  } catch {
    // localStorage can be unavailable in hardened browsers; the in-memory guard still works.
  }
}

function clearStoredRunId(key: string, runId?: string) {
  try {
    if (!runId || window.localStorage.getItem(key) === runId) {
      window.localStorage.removeItem(key);
    }
  } catch {
    // Ignore storage failures; they should not affect report generation.
  }
}

export function ReportAIGenerateControls({
  reportType,
  period,
  target,
  disabled,
  allowSessionSelection = false,
  settingsOpen = false,
  selectedSessionSliceKeys = [],
  onToggleSettings,
  onBeforeGenerate,
  onGenerated
}: ReportAIGenerateControlsProps) {
  const { user } = useAuth();
  const queryClient = useQueryClient();
  const [activeRunId, setActiveRunId] = useState<string>();
  const [handledRunId, setHandledRunId] = useState<string>();
  const [runStartedAt, setRunStartedAt] = useState<number>();
  const [elapsedSeconds, setElapsedSeconds] = useState(0);
  const [initializingDefault, setInitializingDefault] = useState(false);
  const [lastOutcome, setLastOutcome] = useState<
    { type: "success" | "error"; text: string } | undefined
  >();

  const currentUserId = user?.id ?? "anonymous";
  const storageKey = useMemo(
    () => reportRunStorageKey(currentUserId, reportType, period, target),
    [currentUserId, period, reportType, target]
  );
  const periodLabel = useMemo(() => reportPeriodLabel(period), [period]);

  useEffect(() => {
    const storedRunId = readStoredRunId(storageKey);
    const timer = window.setTimeout(() => {
      setActiveRunId(storedRunId);
      setHandledRunId(undefined);
      setLastOutcome(undefined);
      setRunStartedAt(storedRunId ? Date.now() : undefined);
      setElapsedSeconds(0);
    }, 0);
    return () => window.clearTimeout(timer);
  }, [storageKey]);

  const activeRunQuery = useQuery<AIRun>({
    queryKey: ["managed-agent-run", activeRunId],
    queryFn: () => fetchManagedAgentRun(activeRunId as string, { skipErrorHandler: true }),
    enabled: Boolean(activeRunId),
    retry: (failureCount, err) => {
      if (err instanceof HttpError && (err.status === 403 || err.status === 404)) {
        return false;
      }
      return failureCount < 2;
    },
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status === "pending" || status === "running" ? 2500 : false;
    }
  });

  useEffect(() => {
    const err = activeRunQuery.error;
    if (!activeRunId || !(err instanceof HttpError)) return;
    if (err.status !== 403 && err.status !== 404) return;
    clearStoredRunId(storageKey, activeRunId);
    const timer = window.setTimeout(() => {
      setActiveRunId(undefined);
      setHandledRunId(undefined);
    }, 0);
    return () => window.clearTimeout(timer);
  }, [activeRunId, activeRunQuery.error, storageKey]);

  const runMutation = useMutation({
    mutationFn: async () => {
      if (onBeforeGenerate) {
        const ok = await onBeforeGenerate();
        if (!ok) {
          throw new Error("__AIDA_REPORT_AI_CANCELLED__");
        }
      }
      const payload: ManagedReportAgentRunPayload = {
        report_type: reportType,
        period,
        target,
        selected_session_slice_keys:
          allowSessionSelection && selectedSessionSliceKeys.length > 0
            ? selectedSessionSliceKeys
            : undefined
      };
      let run = await startReportAgentRun("default", payload, { skipErrorHandler: true });
      if (!isReportAgentUnavailable(run)) return run;

      const confirmed = await confirmDefaultReportInitialization();
      if (!confirmed) throw new Error("__AIDA_REPORT_AI_CANCELLED__");

      setInitializingDefault(true);
      try {
        await createDefaultReportAgent();
      } catch (err) {
        throw new Error(`日报生成能力初始化失败：${errorMessage(err)}`);
      }
      void queryClient.invalidateQueries({ queryKey: ["managed-agents"] });
      run = await startReportAgentRun("default", payload, { skipErrorHandler: true });
      if (isReportAgentUnavailable(run)) {
        throw new Error("日报生成能力初始化完成，但默认 Agent 仍不可用，请稍后重试");
      }
      return run;
    },
    onSuccess: (run) => {
      storeRunId(storageKey, run.id);
      setActiveRunId(run.id);
      setHandledRunId(undefined);
      setRunStartedAt(Date.now());
      setElapsedSeconds(0);
      setLastOutcome(undefined);
      void queryClient.invalidateQueries({ queryKey: ["managed-agent-runs"] });
    },
    onError: (err: unknown) => {
      if (err instanceof Error && err.message === "__AIDA_REPORT_AI_CANCELLED__") return;
      const text = errorMessage(err);
      setLastOutcome({ type: "error", text });
    },
    onSettled: () => setInitializingDefault(false)
  });

  const activeStatus = activeRunQuery.data?.status;
  const generating =
    runMutation.isPending ||
    Boolean(activeRunId && activeRunQuery.isLoading) ||
    activeStatus === "pending" ||
    activeStatus === "running";

  useEffect(() => {
    if (!generating || !runStartedAt) return;
    const update = () =>
      setElapsedSeconds(Math.max(0, Math.floor((Date.now() - runStartedAt) / 1000)));
    update();
    const timer = window.setInterval(update, 1000);
    return () => window.clearInterval(timer);
  }, [generating, runStartedAt]);

  useEffect(() => {
    const run = activeRunQuery.data;
    if (!run || handledRunId === run.id) return;
    const timer = window.setTimeout(() => {
      if (run.status === "succeeded") {
        setHandledRunId(run.id);
        clearStoredRunId(storageKey, run.id);
        setActiveRunId(undefined);
        setLastOutcome({ type: "success", text: `${periodLabel} 生成完成，报告正文已刷新` });
        void queryClient.invalidateQueries({ queryKey: ["reports"] });
        void queryClient.invalidateQueries({ queryKey: ["managed-agent-runs"] });
        onGenerated?.(run);
        return;
      }
      if (run.status === "failed" || run.status === "timeout") {
        setHandledRunId(run.id);
        clearStoredRunId(storageKey, run.id);
        setActiveRunId(undefined);
        const text = run.error_message || `${periodLabel} AI 生成失败`;
        setLastOutcome({ type: "error", text });
      }
    }, 0);
    return () => window.clearTimeout(timer);
  }, [activeRunQuery.data, handledRunId, onGenerated, periodLabel, queryClient, storageKey]);

  const runningTitle = initializingDefault
    ? "正在初始化日报生成能力"
    : runMutation.isPending
      ? "正在提交生成任务"
      : activeStatus === "pending"
        ? "任务已提交，等待模型开始"
        : elapsedSeconds >= 45
          ? "AI 正在处理报告上下文，请继续等待"
          : "AI 正在读取数据并生成报告";
  const runningDetail = initializingDefault
    ? "初始化完成后将自动开始生成"
    : `已等待 ${elapsedLabel(elapsedSeconds)}，完成后正文会自动刷新`;

  return (
    <div className="report-ai-generate-shell">
      {generating ? (
        <div className="report-ai-run-status is-running" role="status" aria-live="polite">
          <LoadingOutlined spin />
          <span>
            <strong>{runningTitle}</strong>
            <em>{runningDetail}</em>
          </span>
        </div>
      ) : lastOutcome ? (
        <div
          className={`report-ai-run-status is-${lastOutcome.type}`}
          role="status"
          aria-live="polite"
        >
          {lastOutcome.type === "success" ? <CheckCircleOutlined /> : <CloseCircleOutlined />}
          <span>
            <strong>{lastOutcome.type === "success" ? "AI 生成完成" : "AI 生成失败"}</strong>
            <em>{lastOutcome.text}</em>
          </span>
        </div>
      ) : null}
      <Space.Compact className="report-ai-generate-controls">
        <Button
          icon={<RobotOutlined />}
          loading={generating}
          disabled={disabled || generating}
          onClick={() => runMutation.mutate()}
        >
          {generating ? "正在生成" : lastOutcome?.type === "error" ? "重新生成" : "AI 生成"}
        </Button>
        {allowSessionSelection ? (
          <Button
            icon={<FileSearchOutlined />}
            className={settingsOpen ? "is-active" : undefined}
            disabled={disabled || generating}
            title="选择参与生成的 session"
            onClick={onToggleSettings}
          >
            {selectedSessionSliceKeys.length > 0
              ? `已选 ${selectedSessionSliceKeys.length} 个 session`
              : "选择 session"}
          </Button>
        ) : null}
      </Space.Compact>
    </div>
  );
}

export function ReportAISettingsPanel({
  open,
  selectedKeys,
  onSelectedKeysChange,
  onClose,
  variant = "panel"
}: ReportAISettingsPanelProps) {
  const { message } = App.useApp();
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  const [queryRange, setQueryRange] = useState<[Dayjs, Dayjs] | null>(null);
  const queryFrom = queryRange?.[0].format("YYYY-MM-DD");
  const queryTo = queryRange?.[1].format("YYYY-MM-DD");
  const rangeParams = queryFrom && queryTo ? { from: queryFrom, to: queryTo } : {};

  const sessionsQuery = useQuery({
    queryKey: ["report-ai-session-slices", queryFrom, queryTo, page, pageSize],
    queryFn: () =>
      fetchSessionTokens({
        ...rangeParams,
        scope: "mine",
        page: String(page),
        page_size: String(pageSize)
      }),
    enabled: open,
    staleTime: 15_000
  });
  const columns = useMemo<ColumnsType<SessionTokens>>(
    () => [
      {
        title: "日期",
        key: "activity_date",
        width: 104,
        render: (_, record) => sessionActivityDate(record)
      },
      {
        title: "session / 摘要",
        key: "session",
        render: (_, record) => (
          <span className="report-ai-session-cell">
            <span className="report-ai-session-id">{record.session_id}</span>
            <span className="report-ai-session-summary">{record.summary || "暂无摘要"}</span>
          </span>
        )
      },
      {
        title: "Token",
        dataIndex: "total_tokens",
        width: 88,
        align: "right",
        render: formatNumber
      }
    ],
    []
  );

  const selectionSummary =
    selectedKeys.length > 0 ? `已选 ${selectedKeys.length} 个 session` : "默认按日报日期取数";
  const toolbar = (
    <div className="report-ai-settings-panel__toolbar">
      <DatePicker.RangePicker
        size="small"
        allowClear
        placeholder={["开始日期", "结束日期"]}
        value={queryRange}
        onChange={(value) => {
          setQueryRange(value?.[0] && value[1] ? [value[0], value[1]] : null);
          setPage(1);
        }}
      />
      <Tooltip title="清空已选 Session">
        <Button
          size="small"
          className="report-ai-settings-panel__clear"
          type="text"
          icon={<ClearOutlined />}
          disabled={selectedKeys.length === 0}
          aria-label="清空已选 Session"
          onClick={() => onSelectedKeysChange([])}
        />
      </Tooltip>
    </div>
  );
  const sessionTable = (
    <Table<SessionTokens>
      rowKey={sessionSliceKey}
      size="small"
      columns={columns}
      dataSource={sessionsQuery.data?.items ?? []}
      loading={sessionsQuery.isLoading}
      rowSelection={{
        preserveSelectedRowKeys: true,
        selectedRowKeys: selectedKeys,
        onChange: (keys) => {
          const normalized = keys.map(String);
          if (normalized.length > MAX_SELECTED_SESSION_SLICES) {
            message.warning(`最多选择 ${MAX_SELECTED_SESSION_SLICES} 个 Session`);
          }
          onSelectedKeysChange(normalized.slice(0, MAX_SELECTED_SESSION_SLICES));
        }
      }}
      pagination={{
        current: page,
        pageSize,
        total: sessionsQuery.data?.total ?? 0,
        size: "small",
        showSizeChanger: true,
        pageSizeOptions: [5, 10, 20],
        onChange: (nextPage, nextPageSize) => {
          setPage(nextPage);
          setPageSize(nextPageSize);
        }
      }}
      scroll={{
        y:
          variant === "drawer"
            ? "calc(100dvh - 230px)"
            : "clamp(150px, calc(100dvh - 560px), 230px)"
      }}
    />
  );

  if (!open) return null;

  if (variant === "drawer") {
    return (
      <Drawer
        className="report-ai-settings-drawer"
        title={
          <span className="report-ai-settings-drawer__title">
            <strong>选择 session</strong>
            <em>{selectionSummary}</em>
          </span>
        }
        open={open}
        placement="right"
        width={520}
        mask
        maskClosable
        zIndex={1100}
        onClose={onClose}
      >
        <div className="report-ai-settings-drawer__body">
          {toolbar}
          {sessionTable}
        </div>
      </Drawer>
    );
  }

  return (
    <aside className="report-ai-settings-panel">
      <div className="report-ai-settings-panel__head">
        <span>
          <strong>选择参与生成的 session</strong>
          <em>{selectionSummary}</em>
        </span>
        <Button size="small" type="text" onClick={onClose}>
          收起
        </Button>
      </div>
      {toolbar}
      {sessionTable}
    </aside>
  );
}
