import {
  CheckCircleOutlined,
  ClearOutlined,
  CloseCircleOutlined,
  FileSearchOutlined,
  LoadingOutlined,
  RobotOutlined
} from "@ant-design/icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  App,
  Button,
  Checkbox,
  DatePicker,
  Drawer,
  Empty,
  Modal,
  Pagination,
  Space,
  Tag
} from "antd";
import type { Dayjs } from "dayjs";
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
  ManagedReportAgentConfirmationRequired,
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
import { businessDateKey } from "@/shared/utils/businessTime";
import {
  clearReportAIRun,
  readStoredReportAIRun,
  registerReportAIRun,
  reportRunStorageKey,
  type ReportPeriodPayload,
  type ReportTargetPayload
} from "../reportAIRunTracking";

import "./ReportAIGenerateControls.css";

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
  onGeneratingChange?: (generating: boolean) => void;
}

interface ReportAIGenerateControlsStateProps extends ReportAIGenerateControlsProps {
  storageKey: string;
  currentUserId: string;
}

interface ReportAISettingsPanelProps {
  open: boolean;
  reportType: "personal_daily" | "personal_weekly";
  period: ReportPeriodPayload;
  selectedSources: ReportSourceInput[];
  onSelectedSourcesChange: (sources: ReportSourceInput[]) => void;
  onClose: () => void;
  variant?: "panel" | "drawer";
}

const MAX_SELECTED_SESSIONS = 200;
function selectedSourceKey(source: ReportSourceInput) {
  return source.slice_key;
}

function sessionActivityRange(record: ReportSourceCandidate) {
  const start = record.activity_start_at ? businessDateKey(record.activity_start_at) : undefined;
  const end = record.activity_end_at ? businessDateKey(record.activity_end_at) : undefined;
  if (!start) return end || "-";
  if (!end || end === start) return start;
  return `${start} 至 ${end}`;
}

function sessionSourceInput(record: ReportSourceCandidate): ReportSourceInput {
  return { slice_key: record.slice_key };
}

function sessionSourceKey(record: ReportSourceCandidate) {
  return record.slice_key;
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

function confirmLargeReportContext() {
  return new Promise<boolean>((resolve) => {
    Modal.confirm({
      title: "所选会话内容较多",
      content:
        "所选会话内容较多，可能消耗较多 Token，部分模型可能无法完整处理。你可以更换模型、减少所选会话，或继续生成。",
      okText: "继续生成",
      cancelText: "返回调整",
      onOk: () => resolve(true),
      onCancel: () => resolve(false)
    });
  });
}

function isReportAgentConfirmationRequired(
  response: ManagedReportAgentRunResponse
): response is ManagedReportAgentConfirmationRequired {
  return "status" in response && response.status === "confirmation_required";
}

export function ReportAIGenerateControls(props: ReportAIGenerateControlsProps) {
  const { user } = useAuth();
  const currentUserId = user?.id ?? "anonymous";
  const storageKey = useMemo(
    () => reportRunStorageKey(currentUserId, props.reportType, props.period, props.target),
    [currentUserId, props.period, props.reportType, props.target]
  );
  return (
    <ReportAIGenerateControlsState
      key={storageKey}
      {...props}
      storageKey={storageKey}
      currentUserId={currentUserId}
    />
  );
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
  onGeneratingChange,
  storageKey,
  currentUserId
}: ReportAIGenerateControlsStateProps) {
  const queryClient = useQueryClient();
  const [activeRunId, setActiveRunId] = useState<string | undefined>(
    () => readStoredReportAIRun(storageKey)?.runId
  );
  const [handledRunId, setHandledRunId] = useState<string>();
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
        clearReportAIRun(storageKey, activeRunId);
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
    clearReportAIRun(storageKey, activeRunId);
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
      let reportSourceSelectionId: string | undefined;
      let largeContextConfirmed = false;
      if (allowSessionSelection && selectedSessionSources.length > 0) {
        const selection = await createReportSourceSelection({
          report_type: reportType as "personal_daily" | "personal_weekly",
          period,
          selected_slice_keys: selectedSessionSources.map((source) => source.slice_key)
        });
        reportSourceSelectionId = selection.selection_id;
        if (selection.warning_required && !largeContextConfirmed) {
          const confirmed = await confirmLargeReportContext();
          if (!confirmed) throw new Error("__AIDA_REPORT_AI_CANCELLED__");
          largeContextConfirmed = true;
        }
      }
      const payload: ManagedReportAgentRunPayload = {
        report_type: reportType,
        period,
        target,
        report_source_selection_id: reportSourceSelectionId,
        large_context_confirmed: largeContextConfirmed || undefined
      };
      const startRun = async () => {
        const response = await startReportAgentRun("default", payload, { skipErrorHandler: true });
        if (isReportAgentConfirmationRequired(response)) {
          const confirmed = await confirmLargeReportContext();
          if (!confirmed) throw new Error("__AIDA_REPORT_AI_CANCELLED__");
          payload.report_source_selection_id = response.report_source_selection_id;
          payload.large_context_confirmed = true;
          const retry = await startReportAgentRun("default", payload, { skipErrorHandler: true });
          if (isReportAgentConfirmationRequired(retry)) {
            throw new Error("大上下文确认未生效，请重试");
          }
          return retry;
        }
        return response;
      };
      let run = await startRun();
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
      run = await startRun();
      if (isReportAgentUnavailable(run)) {
        throw new Error("日报生成能力初始化完成，但默认 Agent 仍不可用，请稍后重试");
      }
      return run;
    },
    onSuccess: (run) => {
      registerReportAIRun({
        storageKey,
        runId: run.id,
        userId: currentUserId,
        reportType,
        period,
        target,
        startedAt: Date.now()
      });
      setActiveRunId(run.id);
      setHandledRunId(undefined);
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
    onGeneratingChange?.(generating);
  }, [generating, onGeneratingChange]);

  useEffect(() => {
    const run = activeRunQuery.data;
    if (!run || handledRunId === run.id) return;
    const timer = window.setTimeout(() => {
      if (run.status === "succeeded") {
        setHandledRunId(run.id);
        setActiveRunId(undefined);
        setLastOutcome({ type: "success", text: `${periodLabel} 生成完成，报告正文已刷新` });
        void queryClient.invalidateQueries({ queryKey: ["reports"] });
        void queryClient.invalidateQueries({ queryKey: ["managed-agent-runs"] });
        onGenerated?.(run);
        return;
      }
      if (run.status === "failed" || run.status === "timeout") {
        setHandledRunId(run.id);
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
        : "AI 正在整理数据并生成报告";
  const runningDetail = initializingDefault
    ? "初始化完成后将自动开始生成"
    : "通常需要几十秒，可以关闭窗口，任务会在后台继续";

  return (
    <div
      className={`report-ai-generate-shell${generating ? " report-ai-generate-shell--running" : ""}`}
    >
      {generating ? (
        <div className="report-ai-run-status is-running" role="status" aria-live="polite">
          <span className="report-ai-run-status__visual">
            <LoadingOutlined spin />
          </span>
          <span className="report-ai-run-status__copy">
            <strong>{runningTitle}</strong>
            <em>{runningDetail}</em>
          </span>
          <Tag className="report-ai-run-status__badge" color="blue" variant="filled">
            后台任务
          </Tag>
        </div>
      ) : lastOutcome ? (
        <div
          className={`report-ai-run-status is-${lastOutcome.type}`}
          role="status"
          aria-live="polite"
        >
          <span className="report-ai-run-status__visual">
            {lastOutcome.type === "success" ? <CheckCircleOutlined /> : <CloseCircleOutlined />}
          </span>
          <span className="report-ai-run-status__copy">
            <strong>{lastOutcome.type === "success" ? "AI 生成完成" : "AI 生成失败"}</strong>
            <em>{lastOutcome.text}</em>
          </span>
        </div>
      ) : null}
      {!generating ? (
        <Space.Compact className="report-ai-generate-controls">
          <Button icon={<RobotOutlined />} disabled={disabled} onClick={() => runMutation.mutate()}>
            {selectedSessionSources.length > 0
              ? `使用 ${selectedSessionSources.length} 个 Session 生成`
              : lastOutcome?.type === "error"
                ? "重新生成"
                : "AI 生成"}
          </Button>
          {allowSessionSelection && !settingsOpen ? (
            <Button
              icon={<FileSearchOutlined />}
              className={settingsOpen ? "is-active" : undefined}
              disabled={disabled}
              title="选择参与生成的 Session"
              onClick={onToggleSettings}
            >
              {selectedSessionSources.length > 0 ? "调整 Session" : "选择 Session"}
            </Button>
          ) : null}
        </Space.Compact>
      ) : null}
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
  onClose,
  variant = "panel"
}: ReportAISettingsPanelProps) {
  const { message } = App.useApp();
  const periodStart = period.date ?? period.week_start ?? "";
  const periodEnd = period.date ?? period.week_end ?? periodStart;
  const [page, setPage] = useState(1);
  const pageSize = 10;
  const [queryRange, setQueryRange] = useState<[Dayjs, Dayjs] | null>(null);
  const queryFrom = queryRange?.[0].format("YYYY-MM-DD");
  const queryTo = queryRange?.[1].format("YYYY-MM-DD");

  const sessionsQuery = useQuery({
    queryKey: ["report-ai-session-slices", reportType, queryFrom, queryTo, page, pageSize],
    queryFn: () =>
      fetchReportSourceCandidates({
        report_type: reportType,
        period_start: periodStart,
        period_end: periodEnd,
        ...(queryFrom && queryTo ? { activity_from: queryFrom, activity_to: queryTo } : {}),
        page: String(page),
        page_size: String(pageSize)
      }),
    enabled: open,
    staleTime: 15_000
  });

  const updateSourceSelection = (records: ReportSourceCandidate[], selected: boolean) => {
    const changedSources = records.map(sessionSourceInput);
    const changedKeys = new Set(changedSources.map(selectedSourceKey));
    const retained = selectedSources.filter(
      (source) => !changedKeys.has(selectedSourceKey(source))
    );
    if (!selected) {
      onSelectedSourcesChange(retained);
      return;
    }
    const next = [...retained, ...changedSources];
    if (next.length > MAX_SELECTED_SESSIONS) {
      message.warning(`最多选择 ${MAX_SELECTED_SESSIONS} 个 Session`);
    }
    onSelectedSourcesChange(next.slice(0, MAX_SELECTED_SESSIONS));
  };
  const candidates = sessionsQuery.data?.items ?? [];
  const totalCandidates = sessionsQuery.data?.total ?? 0;
  const selectedKeys = useMemo(
    () => new Set(selectedSources.map(selectedSourceKey)),
    [selectedSources]
  );
  const selectedOnPage = candidates.filter((record) => selectedKeys.has(sessionSourceKey(record)));
  const allOnPageSelected = candidates.length > 0 && selectedOnPage.length === candidates.length;
  const someOnPageSelected = selectedOnPage.length > 0 && !allOnPageSelected;

  const toggleSourceSelection = (record: ReportSourceCandidate) => {
    updateSourceSelection([record], !selectedKeys.has(sessionSourceKey(record)));
  };

  const selectionSummary =
    selectedSources.length > 0
      ? `已选择 ${selectedSources.length} 个 Session，生成时仅使用这些内容`
      : "未选择时，将自动汇总当前报告周期";
  const settingsBody = (
    <div className="report-ai-session-browser">
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
        <Button
          size="small"
          className="report-ai-settings-panel__clear"
          type="text"
          icon={<ClearOutlined />}
          disabled={selectedSources.length === 0}
          aria-label="清空已选 Session"
          onClick={() => onSelectedSourcesChange([])}
        >
          清空已选
        </Button>
      </div>
      <div className="report-ai-session-browser__selection-bar">
        <Checkbox
          checked={allOnPageSelected}
          indeterminate={someOnPageSelected}
          disabled={candidates.length === 0}
          onChange={(event) => updateSourceSelection(candidates, event.target.checked)}
        >
          选择本页
        </Checkbox>
        <span>共 {totalCandidates} 个 Session</span>
      </div>
      <div className="report-ai-session-list" aria-busy={sessionsQuery.isLoading}>
        {sessionsQuery.isLoading ? (
          <div className="report-ai-session-list__state">
            <LoadingOutlined spin />
            <span>正在加载 Session</span>
          </div>
        ) : sessionsQuery.isError ? (
          <div className="report-ai-session-list__state is-error">
            <span>Session 加载失败</span>
            <Button size="small" type="link" onClick={() => void sessionsQuery.refetch()}>
              重新加载
            </Button>
          </div>
        ) : candidates.length === 0 ? (
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="当前范围没有可用 Session" />
        ) : (
          candidates.map((record) => {
            const selected = selectedKeys.has(sessionSourceKey(record));
            return (
              <div
                key={sessionSourceKey(record)}
                className={`report-ai-session-option${selected ? " is-selected" : ""}`}
                role="checkbox"
                aria-checked={selected}
                tabIndex={0}
                onClick={() => toggleSourceSelection(record)}
                onKeyDown={(event) => {
                  if (event.key !== "Enter" && event.key !== " ") return;
                  event.preventDefault();
                  toggleSourceSelection(record);
                }}
              >
                <Checkbox
                  checked={selected}
                  tabIndex={-1}
                  onClick={(event) => event.stopPropagation()}
                  onChange={() => toggleSourceSelection(record)}
                  aria-label={`选择 ${record.session_ref}`}
                />
                <span className="report-ai-session-option__content">
                  <span className="report-ai-session-option__head">
                    <strong>{record.session_ref}</strong>
                    <time>{sessionActivityRange(record)}</time>
                  </span>
                  <span
                    className="report-ai-session-option__summary"
                    title={record.summary || "暂无摘要"}
                  >
                    {record.summary || "暂无摘要"}
                  </span>
                </span>
              </div>
            );
          })
        )}
      </div>
      {totalCandidates > pageSize ? (
        <Pagination
          className="report-ai-session-browser__pagination"
          current={page}
          pageSize={pageSize}
          total={totalCandidates}
          size="small"
          showLessItems
          showSizeChanger={false}
          onChange={setPage}
        />
      ) : null}
    </div>
  );

  if (!open) return null;

  if (variant === "drawer") {
    return (
      <Drawer
        className="report-ai-settings-drawer"
        title={
          <span className="report-ai-settings-drawer__title">
            <strong>选择 Session</strong>
            <em>{selectionSummary}</em>
          </span>
        }
        open={open}
        placement="right"
        size="min(680px, 94vw)"
        mask
        maskClosable
        keyboard
        autoFocus
        destroyOnHidden
        zIndex={1100}
        onClose={onClose}
        footer={
          <div className="report-ai-settings-drawer__footer">
            <span>{selectionSummary}</span>
            <Button type="primary" onClick={onClose}>
              完成
            </Button>
          </div>
        }
      >
        <div className="report-ai-settings-drawer__body">{settingsBody}</div>
      </Drawer>
    );
  }

  return (
    <aside className="report-ai-settings-panel">
      <div className="report-ai-settings-panel__head">
        <span>
          <strong>选择参与生成的 Session</strong>
          <em>{selectionSummary}</em>
        </span>
        <Button size="small" type="text" onClick={onClose}>
          收起
        </Button>
      </div>
      {settingsBody}
    </aside>
  );
}
