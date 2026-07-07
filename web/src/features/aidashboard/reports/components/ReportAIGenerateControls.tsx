import { DatabaseOutlined, DeploymentUnitOutlined } from "@ant-design/icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { App, Button, Modal, Space, Table } from "antd";
import type { ColumnsType } from "antd/es/table";
import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";

import {
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
import { aiAssetsPath, errorMessage } from "../../ai-assets/utils/agentAssets";
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
  from: string;
  to: string;
  selectedKeys: string[];
  onSelectedKeysChange: (keys: string[]) => void;
  onClose: () => void;
}

function formatNumber(value?: number) {
  return typeof value === "number" ? value.toLocaleString() : "-";
}

function sessionSliceKey(record: SessionTokens) {
  if (record.slice_key) return record.slice_key;
  if (record.activity_date) return `${record.session_id}:${record.activity_date}`;
  return record.session_id;
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

function isReportAgentUnavailable(
  response: ManagedReportAgentRunResponse
): response is ManagedReportAgentUnavailable {
  return "available" in response && response.available === false;
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
  const { message } = App.useApp();
  const { user } = useAuth();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [activeRunId, setActiveRunId] = useState<string>();
  const [handledRunId, setHandledRunId] = useState<string>();

  const currentUserId = user?.id ?? "anonymous";
  const storageKey = useMemo(
    () => reportRunStorageKey(currentUserId, reportType, period, target),
    [currentUserId, period, reportType, target]
  );
  const periodLabel = useMemo(() => reportPeriodLabel(period), [period]);

  useEffect(() => {
    setActiveRunId(readStoredRunId(storageKey));
    setHandledRunId(undefined);
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
    setActiveRunId(undefined);
    setHandledRunId(undefined);
  }, [activeRunId, activeRunQuery.error, storageKey]);

  const runMutation = useMutation({
    mutationFn: async () => {
      if (onBeforeGenerate) {
        const ok = await onBeforeGenerate();
        if (!ok) {
          throw new Error("__AIDA_REPORT_AI_CANCELLED__");
        }
      }
      return startReportAgentRun("default", {
        report_type: reportType,
        period,
        target,
        selected_session_slice_keys:
          allowSessionSelection && selectedSessionSliceKeys.length > 0
            ? selectedSessionSliceKeys
            : undefined
      }, { skipErrorHandler: true });
    },
    onSuccess: (run) => {
      if (isReportAgentUnavailable(run)) {
        Modal.confirm({
          title: "未配置默认报告 Agent",
          content: run.message || "请先在 AI 资产中创建或设置默认报告 Agent。",
          okText: "去配置",
          cancelText: "取消",
          onOk: () => navigate(aiAssetsPath("agents"))
        });
        return;
      }
      storeRunId(storageKey, run.id);
      setActiveRunId(run.id);
      setHandledRunId(undefined);
      void queryClient.invalidateQueries({ queryKey: ["managed-agent-runs"] });
      message.loading({
        content: `${periodLabel} AI 生成已开始，可关闭弹窗稍后查看。`,
        duration: 2
      });
    },
    onError: (err: unknown) => {
      if (err instanceof Error && err.message === "__AIDA_REPORT_AI_CANCELLED__") return;
      if (err instanceof HttpError && err.status === 404) {
        Modal.confirm({
          title: "未配置默认报告 Agent",
          content: "请先在 AI 资产中创建或设置默认报告 Agent。",
          okText: "去配置",
          cancelText: "取消",
          onOk: () => navigate(aiAssetsPath("agents"))
        });
        return;
      }
      message.error(errorMessage(err));
    }
  });

  useEffect(() => {
    const run = activeRunQuery.data;
    if (!run || handledRunId === run.id) return;
    if (run.status === "succeeded") {
      setHandledRunId(run.id);
      clearStoredRunId(storageKey, run.id);
      message.success({ content: `${periodLabel} AI 生成完成` });
      void queryClient.invalidateQueries({ queryKey: ["reports"] });
      void queryClient.invalidateQueries({ queryKey: ["managed-agent-runs"] });
      onGenerated?.(run);
      return;
    }
    if (run.status === "failed" || run.status === "timeout") {
      setHandledRunId(run.id);
      clearStoredRunId(storageKey, run.id);
      message.error({
        content: run.error_message || `${periodLabel} AI 生成失败`
      });
    }
  }, [activeRunQuery.data, handledRunId, message, onGenerated, periodLabel, queryClient, storageKey]);

  const generating =
    runMutation.isPending ||
    Boolean(activeRunId && activeRunQuery.isLoading) ||
    activeRunQuery.data?.status === "pending" ||
    activeRunQuery.data?.status === "running";

  return (
    <>
      <Space.Compact className="report-ai-generate-controls">
        <Button
          icon={<DeploymentUnitOutlined />}
          loading={generating}
          disabled={disabled || generating}
          onClick={() => runMutation.mutate()}
        >
          AI 生成
        </Button>
        {allowSessionSelection ? (
          <Button
            icon={<DatabaseOutlined />}
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
    </>
  );
}

export function ReportAISettingsPanel({
  open,
  from,
  to,
  selectedKeys,
  onSelectedKeysChange,
  onClose
}: ReportAISettingsPanelProps) {
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);

  useEffect(() => {
    if (open) setPage(1);
  }, [from, open, to]);

  const sessionsQuery = useQuery({
    queryKey: ["report-ai-session-slices", from, to, page, pageSize],
    queryFn: () =>
      fetchSessionTokens({
        from,
        to,
        scope: "mine",
        page: String(page),
        page_size: String(pageSize)
      }),
    enabled: open && Boolean(from && to),
    staleTime: 15_000
  });

  const columns = useMemo<ColumnsType<SessionTokens>>(
    () => [
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

  if (!open) return null;

  return (
    <aside className="report-ai-settings-panel">
      <div className="report-ai-settings-panel__head">
        <span>
          <strong>选择参与生成的 session</strong>
          <em>
            {from} 至 {to} ·{" "}
            {selectedKeys.length > 0
              ? `已选 ${selectedKeys.length} 个 session`
              : "默认全部 session"}
          </em>
        </span>
        <Button size="small" type="text" onClick={onClose}>
          收起
        </Button>
      </div>
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
            onSelectedKeysChange(keys.map(String));
          }
        }}
        pagination={{
          current: page,
          pageSize,
          total: sessionsQuery.data?.total ?? 0,
          showSizeChanger: true,
          onChange: (nextPage, nextPageSize) => {
            setPage(nextPage);
            setPageSize(nextPageSize);
          }
        }}
        scroll={{ y: "min(41vh, 370px)" }}
      />
    </aside>
  );
}
