import {
  CheckCircleOutlined,
  ClearOutlined,
  CloseCircleOutlined,
  DatabaseOutlined,
  DeploymentUnitOutlined,
  LoadingOutlined
} from "@ant-design/icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { App, Button, DatePicker, Input, Modal, Space, Table, Tooltip } from "antd";
import type { ColumnsType } from "antd/es/table";
import type { Dayjs } from "dayjs";
import dayjs from "dayjs";
import { useEffect, useMemo, useState } from "react";

import {
  createDefaultReportAgent,
  createReportSourceSelection,
  fetchManagedAgentRun,
  fetchReportSourceCandidates,
  startReportAgentRun
} from "../../api/client";
import type {
  AIRun,
  ManagedReportAgentUnavailable,
  ManagedReportAgentRunResponse,
  ManagedReportAgentRunPayload,
  ReportSourceCandidate,
  ReportSourceInput,
  ReportType
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
  selectedSessionSources?: ReportSourceInput[];
  onToggleSettings?: () => void;
  onBeforeGenerate?: () => boolean | Promise<boolean>;
  onGenerated?: (run: AIRun) => void;
}

interface ReportAIGenerateControlsStateProps extends ReportAIGenerateControlsProps {
  storageKey: string;
}

interface ReportAISettingsPanelProps {
  open: boolean;
  reportType: "personal_daily" | "personal_weekly";
  period: ReportPeriodPayload;
  selectedSources: ReportSourceInput[];
  onSelectedSourcesChange: (sources: ReportSourceInput[]) => void;
  onClose: () => void;
}

const MAX_SELECTED_SESSION_RANGES = 200;

function candidateKey(record: Pick<ReportSourceCandidate, "agent_type" | "session_ref">) {
  return `${record.agent_type}:${record.session_ref}`;
}

function sourceKey(record: Pick<ReportSourceInput, "agent_type" | "session_ref">) {
  return `${record.agent_type}:${record.session_ref}`;
}

function sourceRangeLabel(source: ReportSourceInput) {
  return `${dayjs(source.activity_start_at).format("MM-DD HH:mm")} 至 ${dayjs(source.activity_end_at).format("MM-DD HH:mm")}`;
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

export function ReportAIGenerateControls(props: ReportAIGenerateControlsProps) {
  const { user } = useAuth();
  const currentUserId = user?.id ?? "anonymous";
  const storageKey = useMemo(
    () => reportRunStorageKey(currentUserId, props.reportType, props.period, props.target),
    [currentUserId, props.period, props.reportType, props.target]
  );
  return <ReportAIGenerateControlsState key={storageKey} {...props} storageKey={storageKey} />;
}

function ReportAIGenerateControlsState({
  reportType,
  period,
  target,
  disabled,
  allowSessionSelection = false,
  settingsOpen = false,
  selectedSessionSources = [],
  onToggleSettings,
  onBeforeGenerate,
  onGenerated,
  storageKey
}: ReportAIGenerateControlsStateProps) {
  const queryClient = useQueryClient();
  const [activeRunId, setActiveRunId] = useState<string | undefined>(() =>
    readStoredRunId(storageKey)
  );
  const [handledRunId, setHandledRunId] = useState<string>();
  const [runStartedAt, setRunStartedAt] = useState<number | undefined>(() =>
    activeRunId ? Date.now() : undefined
  );
  const [elapsedSeconds, setElapsedSeconds] = useState(0);
  const [initializingDefault, setInitializingDefault] = useState(false);
  const [lastOutcome, setLastOutcome] = useState<
    { type: "success" | "error"; text: string } | undefined
  >();

  const periodLabel = useMemo(() => reportPeriodLabel(period), [period]);

  const activeRunQuery = useQuery<AIRun>({
    queryKey: ["managed-agent-run", activeRunId],
    queryFn: () => fetchManagedAgentRun(activeRunId as string, { skipErrorHandler: true }),
    enabled: Boolean(activeRunId),
    retry: (failureCount, err) => {
      if (err instanceof HttpError && (err.status === 403 || err.status === 404)) {
        clearStoredRunId(storageKey, activeRunId);
        return false;
      }
      return failureCount < 2;
    },
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status === "pending" || status === "running" ? 2500 : false;
    }
  });

  const runMutation = useMutation({
    mutationFn: async () => {
      if (onBeforeGenerate) {
        const ok = await onBeforeGenerate();
        if (!ok) {
          throw new Error("__AIDA_REPORT_AI_CANCELLED__");
        }
      }
      let reportSourceSelectionId: string | undefined;
      if (allowSessionSelection && selectedSessionSources.length > 0) {
        const selection = await createReportSourceSelection({
          report_type: reportType as "personal_daily" | "personal_weekly",
          period,
          selected_session_sources: selectedSessionSources
        });
        reportSourceSelectionId = selection.selection_id;
      }
      const payload: ManagedReportAgentRunPayload = {
        report_type: reportType,
        period,
        target,
        report_source_selection_id: reportSourceSelectionId
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
          icon={<DeploymentUnitOutlined />}
          loading={generating}
          disabled={disabled || generating}
          onClick={() => runMutation.mutate()}
        >
          {generating ? "正在生成" : lastOutcome?.type === "error" ? "重新生成" : "AI 生成"}
        </Button>
        {allowSessionSelection ? (
          <Button
            icon={<DatabaseOutlined />}
            className={settingsOpen ? "is-active" : undefined}
            disabled={disabled || generating}
            title="选择参与生成的 session"
            onClick={onToggleSettings}
          >
            {selectedSessionSources.length > 0
              ? `已选 ${selectedSessionSources.length} 个范围`
              : "选择 session"}
          </Button>
        ) : null}
      </Space.Compact>
    </div>
  );
}

export function ReportAISettingsPanel(props: ReportAISettingsPanelProps) {
  return <SnapshotReportAISettingsPanel {...props} />;
}

function SnapshotReportAISettingsPanel({
  open,
  reportType,
  period,
  selectedSources,
  onSelectedSourcesChange,
  onClose
}: ReportAISettingsPanelProps) {
  const { message } = App.useApp();
  const periodStart = period.date ?? period.week_start ?? "";
  const periodEnd = period.date ?? period.week_end ?? periodStart;
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(5);
  const [queryRange, setQueryRange] = useState<[Dayjs, Dayjs] | null>(() =>
    periodStart && periodEnd ? [dayjs(periodStart), dayjs(periodEnd)] : null
  );
  const [search, setSearch] = useState("");
  const queryFrom = queryRange?.[0].format("YYYY-MM-DD");
  const queryTo = queryRange?.[1].format("YYYY-MM-DD");

  const sessionsQuery = useQuery({
    queryKey: [
      "report-ai-session-sources",
      reportType,
      periodStart,
      periodEnd,
      search,
      queryFrom,
      queryTo,
      page,
      pageSize
    ],
    queryFn: () =>
      fetchReportSourceCandidates({
        report_type: reportType,
        period_start: periodStart,
        period_end: periodEnd,
        q: search || undefined,
        activity_from: queryFrom,
        activity_to: queryTo,
        page: String(page),
        page_size: String(pageSize)
      }),
    enabled: open,
    staleTime: 15_000
  });
  const columns = useMemo<ColumnsType<ReportSourceCandidate>>(
    () => [
      {
        title: "活动范围",
        key: "activity_range",
        width: 168,
        render: (_, record) =>
          `${dayjs(record.activity_start_at).format("MM-DD HH:mm")} - ${dayjs(record.activity_end_at).format("MM-DD HH:mm")}`
      },
      {
        title: "session / 摘要",
        key: "session",
        render: (_, record) => (
          <span className="report-ai-session-cell">
            <span className="report-ai-session-id">{record.session_ref}</span>
            <span className="report-ai-session-summary">{record.summary || "暂无摘要"}</span>
          </span>
        )
      },
      {
        title: "项目 / 模型",
        key: "context",
        width: 180,
        render: (_, record) => `${record.cwd || "-"} · ${record.models?.join(", ") || "-"}`
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
            {selectedSources.length > 0
              ? `已选 ${selectedSources.length} 个活动范围`
              : "未选择时按报告周期自动取数"}
          </em>
        </span>
        <Button size="small" type="text" onClick={onClose}>
          收起
        </Button>
      </div>
      <div className="report-ai-settings-panel__toolbar">
        <Input.Search
          size="small"
          allowClear
          placeholder="搜索 Session ID 或摘要"
          onSearch={(value) => {
            setSearch(value.trim());
            setPage(1);
          }}
        />
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
            disabled={selectedSources.length === 0}
            aria-label="清空已选 Session"
            onClick={() => onSelectedSourcesChange([])}
          />
        </Tooltip>
      </div>
      <Table<ReportSourceCandidate>
        rowKey={candidateKey}
        size="small"
        columns={columns}
        dataSource={sessionsQuery.data?.items ?? []}
        loading={sessionsQuery.isLoading}
        rowSelection={{
          preserveSelectedRowKeys: true,
          selectedRowKeys: Array.from(new Set(selectedSources.map(sourceKey))),
          onSelect: (record, selected) => {
            const key = candidateKey(record);
            const retained = selectedSources.filter((source) => sourceKey(source) !== key);
            if (!selected) {
              onSelectedSourcesChange(retained);
              return;
            }
            const next = [
              ...retained,
              {
                session_ref: record.session_ref,
                agent_type: record.agent_type,
                activity_start_at: record.activity_start_at,
                activity_end_at: record.activity_end_at
              }
            ];
            if (next.length > MAX_SELECTED_SESSION_RANGES) {
              message.warning(`最多选择 ${MAX_SELECTED_SESSION_RANGES} 个 Session`);
            }
            onSelectedSourcesChange(next.slice(0, MAX_SELECTED_SESSION_RANGES));
          }
        }}
        expandable={{
          expandedRowRender: (record) => {
            const key = candidateKey(record);
            const ranges = selectedSources
              .map((source, sourceIndex) => ({ source, sourceIndex }))
              .filter(({ source }) => sourceKey(source) === key);
            return (
              <div className="report-ai-source-ranges">
                <DatePicker.RangePicker
                  size="small"
                  showTime
                  allowClear
                  minDate={dayjs(record.activity_start_at)}
                  maxDate={dayjs(record.activity_end_at)}
                  placeholder={["范围开始", "范围结束"]}
                  onChange={(value) => {
                    if (!value?.[0] || !value[1]) return;
                    const availableStart = dayjs(record.activity_start_at);
                    const availableEnd = dayjs(record.activity_end_at);
                    if (
                      value[0].isBefore(availableStart) ||
                      value[1].isAfter(availableEnd) ||
                      value[1].isBefore(value[0])
                    ) {
                      message.warning("选择范围必须位于该 Session 的可用活动范围内");
                      return;
                    }
                    const nextRange: ReportSourceInput = {
                      session_ref: record.session_ref,
                      agent_type: record.agent_type,
                      activity_start_at: value[0].toISOString(),
                      activity_end_at: value[1].toISOString()
                    };
                    const withoutFullRange = selectedSources.filter(
                      (source) =>
                        sourceKey(source) !== key ||
                        source.activity_start_at !== record.activity_start_at ||
                        source.activity_end_at !== record.activity_end_at
                    );
                    if (withoutFullRange.length >= MAX_SELECTED_SESSION_RANGES) {
                      message.warning(`最多选择 ${MAX_SELECTED_SESSION_RANGES} 个活动范围`);
                      return;
                    }
                    onSelectedSourcesChange([...withoutFullRange, nextRange]);
                  }}
                />
                <div className="report-ai-source-ranges__list">
                  {ranges.map(({ source, sourceIndex }) => (
                    <span
                      key={`${source.activity_start_at}:${source.activity_end_at}:${sourceIndex}`}
                    >
                      {sourceRangeLabel(source)}
                      <Button
                        type="text"
                        size="small"
                        aria-label="删除活动范围"
                        onClick={() =>
                          onSelectedSourcesChange(
                            selectedSources.filter((_, index) => index !== sourceIndex)
                          )
                        }
                      >
                        删除
                      </Button>
                    </span>
                  ))}
                </div>
              </div>
            );
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
        scroll={{ y: "clamp(150px, calc(100dvh - 560px), 230px)" }}
      />
    </aside>
  );
}
