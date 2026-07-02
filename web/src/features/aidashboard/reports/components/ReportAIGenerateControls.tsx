import { DeploymentUnitOutlined, SettingOutlined } from "@ant-design/icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { App, Button, Modal, Space, Table } from "antd";
import type { ColumnsType } from "antd/es/table";
import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import dayjs from "dayjs";

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
  sessionRange?: {
    from: string;
    to: string;
  };
  onBeforeGenerate?: () => boolean | Promise<boolean>;
  onGenerated?: (run: AIRun) => void;
}

function formatDateTime(value?: string) {
  return value ? dayjs(value).format("YYYY-MM-DD HH:mm") : "-";
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
  sessionRange,
  onBeforeGenerate,
  onGenerated
}: ReportAIGenerateControlsProps) {
  const { message } = App.useApp();
  const { user } = useAuth();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  const [selectedKeys, setSelectedKeys] = useState<string[]>([]);
  const [activeRunId, setActiveRunId] = useState<string>();
  const [handledRunId, setHandledRunId] = useState<string>();

  const from = sessionRange?.from ?? "";
  const to = sessionRange?.to ?? "";
  const currentUserId = user?.id ?? "anonymous";
  const storageKey = useMemo(
    () => reportRunStorageKey(currentUserId, reportType, period, target),
    [currentUserId, period, reportType, target]
  );

  useEffect(() => {
    setSelectedKeys([]);
    setPage(1);
  }, [from, to]);

  useEffect(() => {
    setActiveRunId(readStoredRunId(storageKey));
    setHandledRunId(undefined);
  }, [storageKey]);

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
    enabled: settingsOpen && allowSessionSelection && Boolean(from && to),
    staleTime: 15_000
  });

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

  const columns = useMemo<ColumnsType<SessionTokens>>(
    () => [
      {
        title: "Session ID",
        dataIndex: "session_id",
        width: 260,
        render: (value: string) => <span className="report-ai-session-id">{value}</span>
      },
      {
        title: "活动时间",
        key: "activity",
        width: 260,
        render: (_, record) =>
          `${formatDateTime(record.activity_start_at)} ~ ${formatDateTime(record.activity_end_at)}`
      },
      {
        title: "摘要",
        dataIndex: "summary",
        render: (value?: string) => value || "-"
      },
      {
        title: "Total",
        dataIndex: "total_tokens",
        width: 120,
        align: "right",
        render: formatNumber
      }
    ],
    []
  );

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
          allowSessionSelection && selectedKeys.length > 0 ? selectedKeys : undefined
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
      message.loading({ content: "AI 正在生成报告...", key: "report-ai-generate", duration: 0 });
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
      message.success({ content: "AI 生成完成", key: "report-ai-generate" });
      void queryClient.invalidateQueries({ queryKey: ["reports"] });
      void queryClient.invalidateQueries({ queryKey: ["managed-agent-runs"] });
      onGenerated?.(run);
      return;
    }
    if (run.status === "failed" || run.status === "timeout") {
      setHandledRunId(run.id);
      clearStoredRunId(storageKey, run.id);
      message.error({
        content: run.error_message || "AI 生成失败",
        key: "report-ai-generate"
      });
    }
  }, [activeRunQuery.data, handledRunId, message, onGenerated, queryClient, storageKey]);

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
            icon={<SettingOutlined />}
            disabled={disabled || generating}
            title="生成设置"
            onClick={() => setSettingsOpen(true)}
          />
        ) : null}
      </Space.Compact>
      {allowSessionSelection ? (
        <Modal
          className="report-ai-settings-modal"
          title="生成设置"
          open={settingsOpen}
          width={920}
          onCancel={() => setSettingsOpen(false)}
          onOk={() => setSettingsOpen(false)}
          okText="确定"
          cancelText="取消"
          destroyOnHidden
        >
          <div className="report-ai-settings">
            <div className="report-ai-settings__summary">
              <strong>Session 片段</strong>
              <span>
                {from} 至 {to} · 已选择 {selectedKeys.length} 个
              </span>
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
                  setSelectedKeys(keys.map(String));
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
            />
          </div>
        </Modal>
      ) : null}
    </>
  );
}
