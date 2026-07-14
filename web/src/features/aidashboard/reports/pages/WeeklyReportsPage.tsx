import { useMutation, useQuery, useQueryClient, type UseQueryResult } from "@tanstack/react-query";
import {
  Alert,
  App,
  Button,
  Card,
  DatePicker,
  Empty,
  Input,
  Modal,
  Pagination,
  Segmented,
  Skeleton,
  Space,
  Tag,
  Tooltip
} from "antd";
import {
  CalendarOutlined,
  CopyOutlined,
  DownOutlined,
  EditOutlined,
  UpOutlined,
  FileTextOutlined
} from "@ant-design/icons";
import { useEffect, useState, type ReactNode } from "react";
import dayjs from "dayjs";

import {
  fetchDepartmentWeeklyReportCurrentOrNull,
  fetchDepartmentWeeklyReports,
  fetchMemberWeeklyReport,
  fetchMemberWeeklyReports,
  fetchPersonalWeeklyReportCurrentOrNull,
  fetchPersonalWeeklyReports,
  fetchTeamWeeklyReportCurrentOrNull,
  fetchTeamWeeklyReports,
  saveDepartmentWeeklyReportCurrent,
  savePersonalWeeklyReport,
  saveTeamWeeklyReport
} from "../../api/client";
import type {
  DepartmentWeeklyReport,
  PaginatedPersonalWeeklyReports,
  PersonalWeeklyReport,
  ReportType,
  TeamWeeklyReport
} from "../../api/types";
import {
  ReportAIGenerateControls,
  ReportAISettingsPanel
} from "../components/ReportAIGenerateControls";
import {
  RequirementMetricCard,
  RequirementMetricGrid
} from "../../requirements/components/RequirementMetricCard";
import { useAuth } from "@/shared/auth/authContext";
import { MarkdownViewer } from "@/shared/components/MarkdownViewer/MarkdownViewer";
import { PagePanel } from "@/shared/components/PagePanel/PagePanel";
import { MemberReportBrowser } from "../MemberReportBrowser";

import "../components/DailyReportGenerateModal.css";
import "./ReportsPage.css";

const { TextArea } = Input;

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : "请稍后重试";
}

function weekStartOf(value: dayjs.Dayjs) {
  const day = value.day();
  const diff = day === 0 ? -6 : 1 - day;
  return value.add(diff, "day").format("YYYY-MM-DD");
}

function weekEndOf(weekStart: string) {
  return dayjs(weekStart).add(6, "day").format("YYYY-MM-DD");
}

function ReportsSkeleton({ rows = 6 }: { rows?: number }) {
  return (
    <div className="reports-loading-frame">
      <Skeleton active paragraph={{ rows }} />
    </div>
  );
}

function ReportsEmpty({ description }: { description: string }) {
  return (
    <div className="reports-empty-frame">
      <Empty description={description} />
    </div>
  );
}

function formatDateTime(value?: string) {
  return value ? dayjs(value).format("YYYY-MM-DD HH:mm") : "-";
}

function formatWeekDate(value: string) {
  return dayjs(value).format("YYYY-MM-DD");
}

function weeklyRange(weekStart: string, weekEnd?: string) {
  const start = formatWeekDate(weekStart);
  return `${start} 至 ${weekEnd ? formatWeekDate(weekEnd) : weekEndOf(start)}`;
}

type WeeklyReportScope = "personal" | "team" | "department";
type WeeklyReportData = PersonalWeeklyReport | TeamWeeklyReport | DepartmentWeeklyReport;

function weeklyReportModalTitle(scope: WeeklyReportScope) {
  if (scope === "team") return "小组周报";
  if (scope === "department") return "部门周报";
  return "我的周报";
}

function weeklyReportType(scope: WeeklyReportScope): ReportType {
  if (scope === "team") return "team_weekly";
  if (scope === "department") return "department_weekly";
  return "personal_weekly";
}

function weeklyReportTarget(scope: WeeklyReportScope) {
  if (scope === "team") return { type: "team" as const };
  if (scope === "department") return { type: "department" as const };
  return { type: "self" as const };
}

function weeklyReportStatusTag(scope: WeeklyReportScope, report: WeeklyReportData | null) {
  if (!report || !report.content?.trim()) return <Tag>暂无报告</Tag>;
  if (scope === "personal" && "status" in report && report.status === "submitted") {
    return <Tag color="green">已保存</Tag>;
  }
  return <Tag color="blue">已保存</Tag>;
}

function weeklyReportContent(report: WeeklyReportData | null) {
  return report?.content ?? "";
}

export function PersonalWeeklyReportsView({
  weekStart,
  weekEnd,
  weekPicker,
  scopeTabs,
  modalMode = false,
  readOnly = false,
  onDone
}: {
  weekStart: string;
  weekEnd: string;
  weekPicker: ReactNode;
  scopeTabs?: ReactNode;
  modalMode?: boolean;
  readOnly?: boolean;
  onDone?: () => void;
}) {
  const queryClient = useQueryClient();
  const { message } = App.useApp();
  const [tab, setTab] = useState<"draft" | "history">("draft");
  const step = "draft" as const;
  const [content, setContent] = useState("");
  const [contentTouched, setContentTouched] = useState(false);
  const showHistory = !modalMode;

  const reportQuery = useQuery<PersonalWeeklyReport | null>({
    queryKey: ["reports", "weekly", "mine", "current", weekStart],
    queryFn: () => fetchPersonalWeeklyReportCurrentOrNull(weekStart),
    staleTime: 30_000
  });
  const historyQuery = useQuery<PaginatedPersonalWeeklyReports>({
    queryKey: ["reports", "weekly", "mine", "history"],
    queryFn: () => fetchPersonalWeeklyReports({ page: "1", page_size: "20" }),
    staleTime: 30_000,
    enabled: showHistory
  });

  const report = reportQuery.data ?? null;

  const effectiveTab = modalMode
    ? step
    : !showHistory && tab === "history"
      ? "draft"
      : tab;
  const editorContent = contentTouched
    ? content
    : (report?.content ?? "");
  const displayWeekStart = formatWeekDate(weekStart);
  const displayWeekEnd = formatWeekDate(weekEnd);

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ["reports", "weekly", "mine"] });
  };

  const saveMutation = useMutation({
    mutationFn: () =>
      savePersonalWeeklyReport({ week_start: weekStart, content: editorContent }),
    onSuccess: (saved) => {
      setContent(saved.content);
      setContentTouched(true);
      invalidate();
      onDone?.();
      message.success("周报已保存");
    },
    onError: (err: unknown) => message.error(errorMessage(err))
  });

  const openManualEditor = () => {
    setContent(report?.content ?? "");
    setContentTouched(true);
    setTab("draft");
  };

  if (readOnly) {
    return (
      <PagePanel
        title="我的周报"
        description="周报详情"
        breadcrumbs={[{ title: "报告" }, { title: "周报" }]}
        className="reports-page aidashboard-list"
        showNav={false}
      >
        {reportQuery.isError ? (
          <Alert type="error" showIcon message="我的周报加载失败" description={errorMessage(reportQuery.error)} />
        ) : reportQuery.isLoading ? (
          <ReportsSkeleton />
        ) : !report ? (
          <ReportsEmpty description="暂无周报详情" />
        ) : (
          <Space direction="vertical" size="middle" style={{ width: "100%" }}>
            <Card>
              <Space size="large" wrap>
                <span>周期：{weeklyRange(report.week_start, report.week_end)}</span>
                <span>状态：{report.status === "submitted" ? "已发送" : "已保存"}</span>
                <span>更新时间：{formatDateTime(report.updated_at)}</span>
              </Space>
            </Card>
            <Card title="周报正文">
              {report.content.trim() ? <MarkdownViewer value={report.content} /> : <Empty description="暂无周报内容" />}
            </Card>
          </Space>
        )}
      </PagePanel>
    );
  }

  return (
    <PagePanel
      title="我的周报"
      description="管理我的周报正文，支持直接填写和保存修改。"
      breadcrumbs={[{ title: "报告" }, { title: "周报" }]}
      className="reports-page aidashboard-list"
      showNav={false}
    >
      {!modalMode ? (
        <RequirementMetricGrid>
          <RequirementMetricCard
            tone="primary"
            icon={<CalendarOutlined />}
            loading={reportQuery.isLoading}
            metric={{
              key: "week",
              title: "周报周期",
              value: dayjs(weekStart).format("MM-DD"),
              description: `${displayWeekStart} 至 ${displayWeekEnd}`
            }}
          />
          <RequirementMetricCard
            tone="success"
            icon={<FileTextOutlined />}
            loading={reportQuery.isLoading}
            metric={{
              key: "content",
              title: "正文状态",
              value: report?.content?.trim() ? 1 : 0,
              description: report?.content?.trim() ? "已保存" : "暂无报告"
            }}
          />
        </RequirementMetricGrid>
      ) : null}

      <div className="reports-toolbar">
        <div className="reports-toolbar__meta">
          <strong>
            {effectiveTab === "draft" ? "我的周报正文" : "我的周报历史"}
          </strong>
          <span>·</span>
          <span>
            {displayWeekStart} 至 {displayWeekEnd}
          </span>
        </div>
        <div className="reports-toolbar__right">
          {scopeTabs}
          {modalMode ? null : (
            <Segmented
              value={effectiveTab}
              onChange={(v) => {
                setTab(v as "draft" | "history");
              }}
              options={[
                { label: "周报正文", value: "draft" },
                ...(showHistory ? [{ label: "历史", value: "history" }] : [])
              ]}
            />
          )}
          {weekPicker}
          {effectiveTab === "draft" && !editorContent.trim() ? (
            <Button onClick={openManualEditor}>直接填写</Button>
          ) : null}
        </div>
      </div>

      {effectiveTab === "history" && showHistory ? (
        <PersonalWeeklyHistory query={historyQuery} />
      ) : reportQuery.isError ? (
        <Alert
          type="error"
          showIcon
          message="我的周报加载失败"
          description={errorMessage(reportQuery.error)}
        />
      ) : reportQuery.isLoading ? (
        <ReportsSkeleton />
      ) : !modalMode && !editorContent.trim() && !report && !contentTouched ? (
        <ReportsEmpty description="暂无本周周报，可直接填写。" />
      ) : (
        <section className="reports-team-card">
          <header className="reports-team-card__head">
            <span className="reports-team-card__title">
              编辑周报 · {displayWeekStart} 至 {displayWeekEnd}
            </span>
            <span className="reports-team-card__meta">
              <span
                className={`reports-tag ${report?.status === "submitted" ? "is-submitted" : "is-team"}`}
              >
                {report?.status === "submitted"
                    ? "已发送"
                    : report?.status === "saved"
                      ? "已保存"
                      : "预览"}
              </span>
              <span>{displayWeekStart}</span>
            </span>
          </header>
          <div className="reports-edit-shell">
            <TextArea
              rows={14}
              className="reports-weekly-editor"
              value={editorContent}
              onChange={(e) => {
                setContent(e.target.value);
                setContentTouched(true);
              }}
            />
            <div className="reports-edit-shell__actions">
              <Button loading={saveMutation.isPending} onClick={() => saveMutation.mutate()}>
                保存周报
              </Button>
            </div>
          </div>
        </section>
      )}
    </PagePanel>
  );
}

export function PersonalWeeklyReportModal({
  open,
  weekStart,
  weekEnd,
  readOnly = false,
  allowWeekSwitch = false,
  onClose,
  onDone
}: {
  open: boolean;
  weekStart: string;
  weekEnd: string;
  readOnly?: boolean;
  allowWeekSwitch?: boolean;
  onClose: () => void;
  onDone?: () => void;
}) {
  return (
    <WeeklyReportEditorModal
      open={open}
      scope="personal"
      weekStart={weekStart}
      weekEnd={weekEnd}
      readOnly={readOnly}
      allowWeekSwitch={allowWeekSwitch}
      onClose={onClose}
      onDone={onDone}
    />
  );
}

function WeeklyReportEditorModal({
  open,
  scope,
  weekStart,
  weekEnd,
  readOnly = false,
  allowWeekSwitch = false,
  onClose,
  onDone
}: {
  open: boolean;
  scope: WeeklyReportScope;
  weekStart: string;
  weekEnd: string;
  readOnly?: boolean;
  allowWeekSwitch?: boolean;
  onClose: () => void;
  onDone?: () => void;
}) {
  const queryClient = useQueryClient();
  const { message } = App.useApp();
  const [selectedWeekStart, setSelectedWeekStart] = useState(weekStart);
  const [content, setContent] = useState("");
  const [contentTouched, setContentTouched] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [selectedSessionSliceKeys, setSelectedSessionSliceKeys] = useState<string[]>([]);
  const title = weeklyReportModalTitle(scope);
  const selectedWeekEnd = selectedWeekStart === weekStart ? weekEnd : weekEndOf(selectedWeekStart);

  const reportQuery = useQuery<WeeklyReportData | null>({
    queryKey: ["reports", "weekly", "editor", scope, selectedWeekStart],
    queryFn: async () => {
      if (scope === "team") return fetchTeamWeeklyReportCurrentOrNull(selectedWeekStart);
      if (scope === "department") return fetchDepartmentWeeklyReportCurrentOrNull(selectedWeekStart);
      return fetchPersonalWeeklyReportCurrentOrNull(selectedWeekStart);
    },
    enabled: open,
    staleTime: 0
  });

  const report = reportQuery.data ?? null;
  const editorContent = contentTouched ? content : weeklyReportContent(report);
  const hasUnsavedContentChange = contentTouched && editorContent !== weeklyReportContent(report);
  const allowSessionSettings = scope === "personal" && !readOnly;
  const showSessionSettings = allowSessionSettings && settingsOpen;
  const canSwitchWeek = allowWeekSwitch && !readOnly;

  useEffect(() => {
    if (!open) return;
    // Modal state intentionally resets when the requested week changes.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setSelectedWeekStart(weekStart);
  }, [open, weekStart]);

  useEffect(() => {
    const timer = window.setTimeout(() => {
      if (!open) return;
      setContent("");
      setContentTouched(false);
      setSettingsOpen(false);
      setSelectedSessionSliceKeys([]);
    }, 0);
    return () => window.clearTimeout(timer);
  }, [open, scope, selectedWeekStart]);

  const changeWeek = (nextWeekStart: string) => {
    if (nextWeekStart === selectedWeekStart) return;
    setSelectedWeekStart(nextWeekStart);
  };

  const handleWeekChange = (value: dayjs.Dayjs | null) => {
    if (!value) return;
    const nextWeekStart = weekStartOf(value);
    if (!hasUnsavedContentChange) {
      changeWeek(nextWeekStart);
      return;
    }
    Modal.confirm({
      title: "当前内容尚未保存",
      content: "切换周后会重新加载报告内容，未保存的修改将丢失。是否继续？",
      okText: "切换",
      cancelText: "继续编辑",
      onOk: () => changeWeek(nextWeekStart)
    });
  };

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ["reports", "weekly"] });
    void queryClient.invalidateQueries({ queryKey: ["reports", "weekly", "editor", scope, selectedWeekStart] });
  };

  const saveMutation = useMutation({
    mutationFn: async () => {
      const nextContent = editorContent.trim();
      if (!nextContent) {
        throw new Error("请先填写周报内容");
      }
      if (scope === "team") {
        return saveTeamWeeklyReport({ week_start: selectedWeekStart, content: nextContent });
      }
      if (scope === "department") {
        return saveDepartmentWeeklyReportCurrent({ week_start: selectedWeekStart, content: nextContent });
      }
      return savePersonalWeeklyReport({ week_start: selectedWeekStart, content: nextContent });
    },
    onSuccess: (saved) => {
      setContent(saved.content);
      setContentTouched(false);
      invalidate();
      message.success("周报已保存");
      onDone?.();
    },
    onError: (err: unknown) => message.error(errorMessage(err))
  });

  const confirmBeforeAIGenerate = () => {
    if (!hasUnsavedContentChange) return true;
    return new Promise<boolean>((resolve) => {
      Modal.confirm({
        title: "当前内容尚未保存",
        content: "AI 生成完成后会刷新报告正文，未保存的修改可能被覆盖。是否继续？",
        okText: "继续生成",
        cancelText: "继续编辑",
        onOk: () => resolve(true),
        onCancel: () => resolve(false)
      });
    });
  };

  const handleAIGenerated = () => {
    setContent("");
    setContentTouched(false);
    invalidate();
    void reportQuery.refetch();
    onDone?.();
  };

  const handleClose = () => {
    if (hasUnsavedContentChange) {
      Modal.confirm({
        title: "当前内容尚未保存，关闭后将丢失，是否关闭？",
        okText: "确认关闭",
        cancelText: "继续编辑",
        onOk: onClose
      });
      return;
    }
    onClose();
  };

  return (
    <Modal
      className="console-report-workflow-modal"
      title={`${title}内容管理`}
      open={open}
      width={860}
      onCancel={handleClose}
      destroyOnHidden
      footer={
        <Space>
          {!readOnly ? (
            <ReportAIGenerateControls
              reportType={weeklyReportType(scope)}
              period={{ week_start: selectedWeekStart, week_end: selectedWeekEnd }}
              target={weeklyReportTarget(scope)}
              allowSessionSelection={allowSessionSettings}
              settingsOpen={showSessionSettings}
              selectedSessionSliceKeys={selectedSessionSliceKeys}
              onToggleSettings={() => setSettingsOpen((value) => !value)}
              disabled={reportQuery.isLoading || saveMutation.isPending}
              onBeforeGenerate={confirmBeforeAIGenerate}
              onGenerated={handleAIGenerated}
            />
          ) : null}
          <Button onClick={handleClose} disabled={saveMutation.isPending}>
            {readOnly ? "关闭" : "取消"}
          </Button>
          {!readOnly ? (
            <Button
              type="primary"
              loading={saveMutation.isPending}
              disabled={reportQuery.isLoading}
              onClick={() => saveMutation.mutate()}
            >
              保存
            </Button>
          ) : null}
        </Space>
      }
    >
      <div className="console-report-modal console-report-management">
        {reportQuery.isError ? (
          <Alert type="error" showIcon message={`${title}加载失败`} description={errorMessage(reportQuery.error)} />
        ) : null}
        <div className="console-report-management__summary">
          <span>
            <strong>{weeklyRange(selectedWeekStart, selectedWeekEnd)}</strong>
            {canSwitchWeek ? (
              <DatePicker
                className="console-report-inline-picker"
                picker="week"
                value={dayjs(selectedWeekStart)}
                allowClear={false}
                suffixIcon={<CalendarOutlined />}
                inputReadOnly
                onChange={handleWeekChange}
              />
            ) : null}
            <em>{title}</em>
          </span>
          {weeklyReportStatusTag(scope, report)}
        </div>
        <div className="console-report-management__content">
          <div className="console-report-management__main">
            {reportQuery.isLoading ? (
              <div className="console-session-empty">正在加载报告内容...</div>
            ) : (
              <div className="console-report-editor-layout">
                <div className="console-report-editor-layout__main">
                  <div className="console-session-modal__section">
                    <strong>报告正文</strong>
                    <span>
                      {editorContent.trim()
                        ? readOnly
                          ? "已加载报告内容。"
                          : "已加载报告，可编辑后保存。"
                        : readOnly
                          ? "暂无报告内容"
                          : "暂无报告，可直接填写。"}
                    </span>
                  </div>
                  <TextArea
                    rows={18}
                    readOnly={readOnly}
                    value={editorContent}
                    onChange={(event) => {
                      if (readOnly) return;
                      setContent(event.target.value);
                      setContentTouched(true);
                    }}
                    placeholder={readOnly ? "暂无报告内容" : "暂无报告，可直接填写。"}
                  />
                </div>
              </div>
            )}
          </div>
          {allowSessionSettings ? (
            <ReportAISettingsPanel
              key={`weekly:${selectedWeekStart}:${selectedWeekEnd}`}
              open={settingsOpen}
              selectedKeys={selectedSessionSliceKeys}
              onSelectedKeysChange={setSelectedSessionSliceKeys}
              onClose={() => setSettingsOpen(false)}
              variant="drawer"
            />
          ) : null}
        </div>
      </div>
    </Modal>
  );
}

function PersonalWeeklyHistory({
  query
}: {
  query: UseQueryResult<PaginatedPersonalWeeklyReports>;
}) {
  const reports = query.data?.items ?? [];
  if (query.isError)
    return (
      <Alert
        type="error"
        showIcon
        message="我的周报历史加载失败"
        description={errorMessage(query.error)}
      />
    );
  if (query.isLoading) return <ReportsSkeleton />;
  if (reports.length === 0) return <ReportsEmpty description="暂无我的周报历史" />;
  return (
    <WeeklyReportCards
      reports={reports.map((r) => ({
        id: r.id,
        title: "我的周报",
        date: formatWeekDate(r.week_start),
        content: weeklyRange(r.week_start, r.week_end),
        done: r.status === "submitted"
      }))}
    />
  );
}

export function WeeklyReportsPage() {
  const { user } = useAuth();
  const queryClient = useQueryClient();
  const [roleTab, setRoleTab] = useState<"mine" | "member" | "team" | "department">("mine");
  const [memberWeekStart, setMemberWeekStart] = useState(() => weekStartOf(dayjs()));
  const [modalTarget, setModalTarget] = useState<{
    scope: "mine" | "team" | "department";
    weekStart: string;
    mode: "view" | "edit";
    allowWeekSwitch?: boolean;
  } | null>(null);

  if (!user) return null;

  const tabOptions =
    user.role === "director" || user.role === "admin"
      ? [
          { label: "我的周报记录", value: "mine" },
          { label: "部门成员周报", value: "member" },
          { label: "部门汇总周报", value: "department" }
        ]
      : user.role === "team_leader"
        ? [
            { label: "我的周报记录", value: "mine" },
            { label: "小组成员周报", value: "member" },
            { label: "小组汇总周报", value: "team" }
          ]
        : [{ label: "我的周报记录", value: "mine" }];
  const activeTab = tabOptions.some((item) => item.value === roleTab) ? roleTab : "mine";
  const currentWeekStart = weekStartOf(dayjs());
  const openLabel =
    activeTab === "team"
      ? "填写小组周报"
      : activeTab === "department"
        ? "填写部门周报"
        : "填写周报";
  const invalidateWeekly = () => {
    void queryClient.invalidateQueries({ queryKey: ["reports", "weekly"] });
  };

  return (
    <PagePanel
      title="周报"
      description="按记录列表打开周报；当前周编辑与保存统一通过弹窗处理。"
      breadcrumbs={[{ title: "报告" }, { title: "周报" }]}
      className="reports-page aidashboard-list"
      showNav={false}
    >
      <Card className="reports-control-card">
        <Space wrap style={{ width: "100%", justifyContent: "space-between" }}>
          <Space wrap>
            {tabOptions.length > 1 ? (
              <Segmented
                value={activeTab}
                onChange={(value) => setRoleTab(value as "mine" | "member" | "team" | "department")}
                options={tabOptions}
              />
            ) : null}
            {activeTab === "member" ? <DatePicker picker="week" allowClear={false} value={dayjs(memberWeekStart)} onChange={(value) => value && setMemberWeekStart(weekStartOf(value))} /> : null}
          </Space>
          {activeTab !== "member" ? <Button
            type="primary"
            icon={<FileTextOutlined />}
            onClick={() => setModalTarget({
              scope: activeTab,
              weekStart: currentWeekStart,
              mode: "edit",
              allowWeekSwitch: true
            })}
          >
            {openLabel}
          </Button> : null}
        </Space>
      </Card>

      {activeTab === "mine" ? (
        <PersonalWeeklyRecordsTable
          onEdit={(recordWeekStart) => setModalTarget({ scope: "mine", weekStart: recordWeekStart, mode: "edit" })}
        />
      ) : null}
      {activeTab === "team" ? (
        <TeamWeeklyRecordsTable
          onEdit={(recordWeekStart) => setModalTarget({ scope: "team", weekStart: recordWeekStart, mode: "edit" })}
        />
      ) : null}
      {activeTab === "member" ? <MemberWeeklyTable weekStart={memberWeekStart} /> : null}
      {activeTab === "department" ? (
        <DepartmentWeeklyRecordsTable
          onEdit={(recordWeekStart) =>
            setModalTarget({ scope: "department", weekStart: recordWeekStart, mode: "edit" })
          }
        />
      ) : null}

      {modalTarget?.scope === "mine" ? (
        <PersonalWeeklyReportModal
          open
          weekStart={modalTarget.weekStart}
          weekEnd={weekEndOf(modalTarget.weekStart)}
          readOnly={modalTarget.mode === "view"}
          allowWeekSwitch={modalTarget.allowWeekSwitch}
          onClose={() => setModalTarget(null)}
          onDone={invalidateWeekly}
        />
      ) : null}
      {modalTarget?.scope === "team" ? (
        <TeamWeeklyReportModal
          open
          weekStart={modalTarget.weekStart}
          weekEnd={weekEndOf(modalTarget.weekStart)}
          readOnly={modalTarget.mode === "view"}
          allowWeekSwitch={modalTarget.allowWeekSwitch}
          onClose={() => setModalTarget(null)}
          onDone={invalidateWeekly}
        />
      ) : null}
      {modalTarget?.scope === "department" ? (
        <DepartmentWeeklyReportModal
          open
          weekStart={modalTarget.weekStart}
          weekEnd={weekEndOf(modalTarget.weekStart)}
          readOnly={modalTarget.mode === "view"}
          allowWeekSwitch={modalTarget.allowWeekSwitch}
          onClose={() => setModalTarget(null)}
          onDone={invalidateWeekly}
        />
      ) : null}
    </PagePanel>
  );
}

type InlineWeeklyRecord = {
  id: string;
  week_start: string;
  updated_at: string;
};

async function copyWeeklyReportText(value: string) {
  if (navigator.clipboard && window.isSecureContext) {
    const copied = await navigator.clipboard.writeText(value).then(() => true, () => false);
    if (copied) return;
  }

  const textarea = document.createElement("textarea");
  textarea.value = value;
  textarea.setAttribute("readonly", "");
  textarea.style.position = "fixed";
  textarea.style.left = "-9999px";
  document.body.appendChild(textarea);
  textarea.focus();
  textarea.select();
  const copied = document.execCommand("copy");
  document.body.removeChild(textarea);
  if (!copied) throw new Error("copy failed");
}

function InlineWeeklyContentList<TRecord extends InlineWeeklyRecord>({
  title,
  items,
  total,
  loading,
  error,
  pagination,
  getRange,
  getMeta,
  getPreview,
  fetchDetail,
  onEdit
}: {
  title: string;
  items: TRecord[];
  total: number;
  loading: boolean;
  error?: string;
  pagination: Parameters<typeof Pagination>[0];
  getRange: (record: TRecord) => string;
  getMeta: (record: TRecord) => string;
  getPreview: (record: TRecord) => string;
  fetchDetail: (record: TRecord) => Promise<{ content: string } | null>;
  onEdit: (record: TRecord) => void;
}) {
  return (
    <Card className="reports-list-card reports-inline-content-list reports-weekly-inline-content-list" title={title}>
      {error ? <Alert type="error" showIcon message={error} /> : null}
      {!error && loading ? <ReportsSkeleton rows={4} /> : null}
      {!error && !loading && items.length === 0 ? <ReportsEmpty description={`暂无${title}`} /> : null}
      {!error && !loading && items.length > 0 ? (
        <div className="member-report-content-list">
          {items.map((record) => (
            <InlineWeeklyContentItem
              key={record.id}
              record={record}
              getRange={getRange}
              meta={getMeta(record)}
              preview={getPreview(record)}
              fetchDetail={fetchDetail}
              onEdit={() => onEdit(record)}
            />
          ))}
        </div>
      ) : null}
      {!error && !loading && total > 0 ? <Pagination className="reports-inline-content-list__pagination" total={total} {...pagination} /> : null}
    </Card>
  );
}

function InlineWeeklyContentItem<TRecord extends InlineWeeklyRecord>({
  record,
  getRange,
  meta,
  preview,
  fetchDetail,
  onEdit
}: {
  record: TRecord;
  getRange: (record: TRecord) => string;
  meta: string;
  preview: string;
  fetchDetail: (record: TRecord) => Promise<{ content: string } | null>;
  onEdit: () => void;
}) {
  const { message } = App.useApp();
  const [expanded, setExpanded] = useState(false);
  const detailQuery = useQuery({
    queryKey: ["reports", "weekly-inline-detail", record.id],
    queryFn: () => fetchDetail(record),
    enabled: expanded,
    staleTime: 30_000
  });
  const copyCurrentReport = async () => {
    const content = detailQuery.data?.content?.trim();
    if (!content) return;
    try {
      await copyWeeklyReportText(content);
      void message.success("周报全文已复制");
    } catch {
      void message.error("复制失败，请稍后重试");
    }
  };

  return (
    <article className={`member-report-content-item${expanded ? " is-expanded" : ""}`}>
      <header className="member-report-content-item__head">
        <div>
          <div className="member-report-content-item__identity">
            <strong>{getRange(record)}</strong>
            <span>{meta}</span>
          </div>
          <small>{formatDateTime(record.updated_at)}</small>
        </div>
        <div className="member-report-content-item__actions">
          <Button className="member-report-content-item__edit" type="text" size="small" icon={<EditOutlined />} onClick={onEdit}>
            编辑
          </Button>
          <Button className="member-report-content-item__toggle" type="text" size="small" aria-expanded={expanded} onClick={() => setExpanded((value) => !value)}>
            {expanded ? <UpOutlined /> : <DownOutlined />}
            {expanded ? "收起" : "展开"}
          </Button>
        </div>
      </header>
      <p className="member-report-content-item__preview">{preview}</p>
      {expanded ? (
        <div className="member-report-content-item__detail">
          <div className="member-report-content-item__detail-bar">
            <span>周报全文</span>
            <Tooltip title="复制全文（保留 Markdown 格式）">
              <Button className="member-report-content-item__copy" type="text" size="small" icon={<CopyOutlined />} aria-label="复制周报全文" disabled={!detailQuery.data?.content?.trim()} onClick={() => void copyCurrentReport()} />
            </Tooltip>
          </div>
          {detailQuery.isLoading ? <div className="member-report-content-item__loading">正在加载周报全文…</div> : null}
          {detailQuery.isError ? <Alert type="error" showIcon message="周报加载失败" /> : null}
          {!detailQuery.isLoading && !detailQuery.isError && detailQuery.data?.content?.trim() ? <MarkdownViewer value={detailQuery.data.content} /> : null}
          {!detailQuery.isLoading && !detailQuery.isError && !detailQuery.data?.content?.trim() ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无周报内容" /> : null}
        </div>
      ) : null}
    </article>
  );
}

function PersonalWeeklyRecordsTable({
  onEdit
}: {
  onEdit: (weekStart: string) => void;
}) {
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  const reportsQuery = useQuery<PaginatedPersonalWeeklyReports>({
    queryKey: ["reports", "weekly", "mine", "history", { page, pageSize }],
    queryFn: () => fetchPersonalWeeklyReports({ page: String(page), page_size: String(pageSize) }),
    staleTime: 30_000
  });

  return (
    <InlineWeeklyContentList
      title="我的周报记录"
      items={reportsQuery.data?.items ?? []}
      total={reportsQuery.data?.total ?? 0}
      loading={reportsQuery.isLoading}
      error={reportsQuery.isError ? `我的周报记录加载失败：${errorMessage(reportsQuery.error)}` : undefined}
      pagination={{
        current: page,
        pageSize,
        showSizeChanger: true,
        showTotal: (total) => `共 ${total} 条记录`,
        onChange: (next, size) => {
          setPage(size !== pageSize ? 1 : next);
          setPageSize(size);
        }
      }}
      getRange={(record) => weeklyRange(record.week_start, record.week_end)}
      getMeta={() => "我的周报"}
      getPreview={(record) => `已关联 ${record.source_daily_count} 份日报 · ${record.source_session_count} 个 session`}
      fetchDetail={async (record) => fetchPersonalWeeklyReportCurrentOrNull(formatWeekDate(record.week_start))}
      onEdit={(record) => onEdit(formatWeekDate(record.week_start))}
    />
  );
}

function MemberWeeklyTable({ weekStart }: { weekStart: string }) {
  const reportsQuery = useQuery({
    queryKey: ["reports", "weekly", "member-list", weekStart],
    queryFn: () => fetchMemberWeeklyReports(weekStart),
    staleTime: 30_000
  });
  return <MemberReportBrowser
    items={reportsQuery.data ?? []}
    loading={reportsQuery.isLoading}
    error={reportsQuery.isError ? errorMessage(reportsQuery.error) : undefined}
    queryKey={`weekly:${weekStart}`}
    fetchDetail={fetchMemberWeeklyReport}
    displayMode="content-list"
  />;
}

function TeamWeeklyRecordsTable({
  onEdit
}: {
  onEdit: (weekStart: string) => void;
}) {
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  const reportsQuery = useQuery<TeamWeeklyReport[]>({
    queryKey: ["reports", "weekly", "team", "history"],
    queryFn: () => fetchTeamWeeklyReports(),
    staleTime: 30_000
  });
  const reports = reportsQuery.data ?? [];
  return (
    <InlineWeeklyContentList
      title="小组周报记录"
      items={reports.slice((page - 1) * pageSize, page * pageSize)}
      total={reports.length}
      loading={reportsQuery.isLoading}
      error={reportsQuery.isError ? `小组周报记录加载失败：${errorMessage(reportsQuery.error)}` : undefined}
      pagination={{
        current: page,
        pageSize,
        showSizeChanger: true,
        showTotal: (total) => `共 ${total} 条记录`,
        onChange: (next, size) => {
          setPage(size !== pageSize ? 1 : next);
          setPageSize(size);
        }
      }}
      getRange={(record) => weeklyRange(record.week_start)}
      getMeta={(record) => record.team_name}
      getPreview={(record) => record.content?.trim() || `已关联 ${record.source_personal_weekly_report_ids.length} 份成员周报`}
      fetchDetail={async (record) => fetchTeamWeeklyReportCurrentOrNull(formatWeekDate(record.week_start))}
      onEdit={(record) => onEdit(formatWeekDate(record.week_start))}
    />
  );
}

function DepartmentWeeklyRecordsTable({
  onEdit
}: {
  onEdit: (weekStart: string) => void;
}) {
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  const reportsQuery = useQuery<DepartmentWeeklyReport[]>({
    queryKey: ["reports", "weekly", "department", "history"],
    queryFn: () => fetchDepartmentWeeklyReports(),
    staleTime: 30_000
  });
  const reports = reportsQuery.data ?? [];
  return (
    <InlineWeeklyContentList
      title="部门周报记录"
      items={reports.slice((page - 1) * pageSize, page * pageSize)}
      total={reports.length}
      loading={reportsQuery.isLoading}
      error={reportsQuery.isError ? `部门周报记录加载失败：${errorMessage(reportsQuery.error)}` : undefined}
      pagination={{
        current: page,
        pageSize,
        showSizeChanger: true,
        showTotal: (total) => `共 ${total} 条记录`,
        onChange: (next, size) => {
          setPage(size !== pageSize ? 1 : next);
          setPageSize(size);
        }
      }}
      getRange={(record) => weeklyRange(record.week_start)}
      getMeta={() => "部门周报"}
      getPreview={(record) => record.content?.trim() || `已汇总 ${record.source_team_weekly_report_ids.length} 个小组周报`}
      fetchDetail={async (record) => fetchDepartmentWeeklyReportCurrentOrNull(formatWeekDate(record.week_start))}
      onEdit={(record) => onEdit(formatWeekDate(record.week_start))}
    />
  );
}

export function TeamWeeklyReportsView({
  weekStart,
  weekEnd,
  weekPicker,
  canEdit = false,
  scopeTabs,
  modalMode = false,
  readOnly = false,
  onDone
}: {
  weekStart: string;
  weekEnd: string;
  weekPicker: ReactNode;
  canEdit?: boolean;
  scopeTabs?: ReactNode;
  modalMode?: boolean;
  readOnly?: boolean;
  onDone?: () => void;
}) {
  const queryClient = useQueryClient();
  const { message } = App.useApp();
  const [tab, setTab] = useState<"draft" | "history">("draft");
  const [content, setContent] = useState("");
  const [contentTouched, setContentTouched] = useState(false);
  const showHistory = !modalMode;

  const reportQuery = useQuery<TeamWeeklyReport | null>({
    queryKey: ["reports", "weekly", "team", "current", weekStart],
    queryFn: () => fetchTeamWeeklyReportCurrentOrNull(weekStart),
    staleTime: 30_000
  });
  const historyQuery = useQuery<TeamWeeklyReport[]>({
    queryKey: ["reports", "weekly", "team", "history"],
    queryFn: () => fetchTeamWeeklyReports(),
    staleTime: 30_000,
    enabled: showHistory
  });

  const report = reportQuery.data ?? null;
  const history = historyQuery.data ?? [];
  const editorContent = contentTouched
    ? content
    : (report?.content ?? "");
  const submittedLocked = Boolean(report?.submitted_at);
  const effectiveTab = !showHistory && tab === "history" ? "draft" : tab;
  const displayWeekStart = formatWeekDate(weekStart);
  const displayWeekEnd = formatWeekDate(weekEnd);

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ["reports", "weekly", "team"] });
    void queryClient.invalidateQueries({ queryKey: ["reports", "weekly", "department"] });
  };
  const openManualEditor = () => {
    setContent(report?.content ?? "");
    setContentTouched(true);
    setTab("draft");
  };

  const saveMutation = useMutation({
    mutationFn: () =>
      saveTeamWeeklyReport({ week_start: weekStart, content: editorContent }),
    onSuccess: (saved) => {
      setContent(saved.content);
      setContentTouched(true);
      message.success("已保存");
      invalidate();
      onDone?.();
    },
    onError: (err: unknown) => message.error(err instanceof Error ? err.message : "保存失败")
  });

  if (readOnly) {
    return (
      <PagePanel
        title="小组周报"
        description="周报详情"
        breadcrumbs={[{ title: "报告" }, { title: "周报" }]}
        className="reports-page aidashboard-list"
        showNav={false}
      >
        {reportQuery.isError ? (
          <Alert type="error" showIcon message="小组周报加载失败" description={errorMessage(reportQuery.error)} />
        ) : reportQuery.isLoading ? (
          <ReportsSkeleton />
        ) : !report ? (
          <ReportsEmpty description="暂无周报详情" />
        ) : (
          <Space direction="vertical" size="middle" style={{ width: "100%" }}>
            <Card>
              <Space size="large" wrap>
                <span>周期：{weeklyRange(report.week_start)}</span>
                <span>小组：{report.team_name}</span>
                <span>状态：{report.submitted_at ? "已提交" : "已保存"}</span>
                <span>提交时间：{formatDateTime(report.submitted_at)}</span>
                <span>更新时间：{formatDateTime(report.updated_at)}</span>
              </Space>
            </Card>
            <Card title="周报正文">
              {report.content.trim() ? <MarkdownViewer value={report.content} /> : <Empty description="暂无周报内容" />}
            </Card>
          </Space>
        )}
      </PagePanel>
    );
  }

  return (
    <PagePanel
      title="小组周报"
      description="管理小组周报正文，支持直接填写和保存修改。"
      breadcrumbs={[{ title: "报告" }, { title: "周报" }]}
      className="reports-page aidashboard-list"
      showNav={false}
    >
      {!modalMode ? (
      <RequirementMetricGrid>
        <RequirementMetricCard
          tone="primary"
          icon={<CalendarOutlined />}
          loading={reportQuery.isLoading}
          metric={{
            key: "week",
            title: "周报周期",
            value: dayjs(weekStart).format("MM-DD"),
            description: `${displayWeekStart} 至 ${displayWeekEnd}`
          }}
        />
        <RequirementMetricCard
          tone="success"
          icon={<FileTextOutlined />}
          loading={reportQuery.isLoading}
          metric={{
            key: "content",
            title: "正文状态",
            value: report?.content?.trim() ? 1 : 0,
            description: report?.content?.trim() ? "已保存" : "暂无报告"
          }}
        />
      </RequirementMetricGrid>
      ) : null}

      <div className="reports-toolbar">
        <div className="reports-toolbar__meta">
          <strong>
            {effectiveTab === "draft" ? "小组周报正文" : "小组周报历史"}
          </strong>
          <span>·</span>
          <span>
            {displayWeekStart} 至 {displayWeekEnd}
          </span>
        </div>
        <div className="reports-toolbar__right">
          {scopeTabs}
          <Segmented
            value={effectiveTab}
            onChange={(v) => setTab(v as "draft" | "history")}
            options={[
              { label: "周报正文", value: "draft" },
              ...(showHistory ? [{ label: "历史", value: "history" }] : [])
            ]}
          />
          {weekPicker}
          {canEdit && effectiveTab === "draft" && !editorContent.trim() ? (
            <Space>
              <Button onClick={openManualEditor}>直接填写</Button>
            </Space>
          ) : null}
        </div>
      </div>

      {effectiveTab === "history" && showHistory ? (
        <TeamWeeklyHistory query={historyQuery} reports={history} />
      ) : reportQuery.isError ? (
        <Alert
          type="error"
          showIcon
          message="小组周报加载失败"
          description={errorMessage(reportQuery.error)}
        />
      ) : reportQuery.isLoading ? (
        <ReportsSkeleton />
      ) : !editorContent.trim() && !report && !contentTouched ? (
        <ReportsEmpty description="暂无小组周报，可直接填写。" />
      ) : (
        <section className="reports-team-card">
          <header className="reports-team-card__head">
            <span className="reports-team-card__title">
              {report?.team_name ?? "小组周报"}
            </span>
            <span className="reports-team-card__meta">
              <span className={`reports-tag ${submittedLocked ? "is-submitted" : "is-team"}`}>
                {submittedLocked ? "已保存" : "正文"}
              </span>
              <span>{formatWeekDate(report?.week_start ?? weekStart)}</span>
              {canEdit && !submittedLocked ? (
                <Button loading={saveMutation.isPending} onClick={() => saveMutation.mutate()}>
                  保存
                </Button>
              ) : null}
            </span>
          </header>
          {canEdit && !submittedLocked ? (
            <div className="reports-edit-shell">
              <TextArea
                rows={12}
                value={editorContent}
                onChange={(e) => {
                  setContent(e.target.value);
                  setContentTouched(true);
                }}
              />
            </div>
          ) : (
            <p className="reports-team-card__body">{editorContent}</p>
          )}
        </section>
      )}
    </PagePanel>
  );
}

export function TeamWeeklyReportModal({
  open,
  weekStart,
  weekEnd,
  readOnly = false,
  allowWeekSwitch = false,
  onClose,
  onDone
}: {
  open: boolean;
  weekStart: string;
  weekEnd: string;
  readOnly?: boolean;
  allowWeekSwitch?: boolean;
  onClose: () => void;
  onDone?: () => void;
}) {
  return (
    <WeeklyReportEditorModal
      open={open}
      scope="team"
      weekStart={weekStart}
      weekEnd={weekEnd}
      readOnly={readOnly}
      allowWeekSwitch={allowWeekSwitch}
      onClose={onClose}
      onDone={onDone}
    />
  );
}

export function TeamWeeklyHistory({
  query,
  reports
}: {
  query: UseQueryResult<TeamWeeklyReport[]>;
  reports: TeamWeeklyReport[];
}) {
  if (query.isError)
    return (
      <Alert
        type="error"
        showIcon
        message="小组周报历史加载失败"
        description={errorMessage(query.error)}
      />
    );
  if (query.isLoading) return <ReportsSkeleton />;
  if (reports.length === 0) return <ReportsEmpty description="暂无小组周报历史" />;
  return (
    <WeeklyReportCards
      reports={reports.map((r) => ({
        id: r.id,
        title: r.team_name,
        date: formatWeekDate(r.week_start),
        content: r.content,
        done: Boolean(r.submitted_at)
      }))}
    />
  );
}

export function DirectorWeeklyReportsView({
  weekStart,
  weekEnd,
  weekPicker,
  scopeTabs,
  modalMode = false,
  readOnly = false,
  onDone
}: {
  weekStart: string;
  weekEnd: string;
  weekPicker: React.ReactNode;
  scopeTabs?: ReactNode;
  modalMode?: boolean;
  readOnly?: boolean;
  onDone?: () => void;
}) {
  const queryClient = useQueryClient();
  const { message } = App.useApp();
  const [tab, setTab] = useState<"draft" | "history" | "teams">("draft");
  const [step] = useState<"draft">("draft");
  const [editing, setEditing] = useState(false);
  const [content, setContent] = useState("");
  const [contentTouched, setContentTouched] = useState(false);

  const reportQuery = useQuery<DepartmentWeeklyReport | null>({
    queryKey: ["reports", "weekly", "department", "current", weekStart],
    queryFn: () => fetchDepartmentWeeklyReportCurrentOrNull(weekStart),
    staleTime: 30_000
  });
  const historyQuery = useQuery<DepartmentWeeklyReport[]>({
    queryKey: ["reports", "weekly", "department", "history"],
    queryFn: () => fetchDepartmentWeeklyReports(),
    staleTime: 30_000,
    enabled: !modalMode
  });
  const teamHistoryQuery = useQuery<TeamWeeklyReport[]>({
    queryKey: ["reports", "weekly", "team", "history", "director"],
    queryFn: () => fetchTeamWeeklyReports(),
    staleTime: 30_000,
    enabled: !modalMode
  });

  const report = reportQuery.data ?? null;
  const history = historyQuery.data ?? [];
  const teamHistory = teamHistoryQuery.data ?? [];
  const showHistory = !modalMode;
  const effectiveTab = modalMode
    ? step
    : !showHistory && (tab === "history" || tab === "teams") ? "draft" : tab;
  const editorContent = contentTouched ? content : (report?.content ?? "");
  const displayWeekStart = formatWeekDate(weekStart);
  const displayWeekEnd = formatWeekDate(weekEnd);
  const openManualEditor = () => {
    setContent(report?.content ?? "");
    setContentTouched(true);
    setTab("draft");
    setEditing(true);
  };

  const invalidate = () => void queryClient.invalidateQueries({ queryKey: ["reports", "weekly"] });
  const saveMutation = useMutation({
    mutationFn: () =>
      saveDepartmentWeeklyReportCurrent({ week_start: weekStart, content: editorContent }),
    onSuccess: () => {
      message.success("已保存");
      setEditing(false);
      setContentTouched(false);
      invalidate();
      onDone?.();
    },
    onError: (err: unknown) => message.error(err instanceof Error ? err.message : "保存失败")
  });
  if (readOnly) {
    return (
      <PagePanel
        title="部门周报"
        description="周报详情"
        breadcrumbs={[{ title: "报告" }, { title: "周报" }]}
        className="reports-page aidashboard-list"
        showNav={false}
      >
        {reportQuery.isError ? (
          <Alert type="error" showIcon message="部门周报加载失败" description={errorMessage(reportQuery.error)} />
        ) : reportQuery.isLoading ? (
          <ReportsSkeleton />
        ) : !report ? (
          <ReportsEmpty description="暂无周报详情" />
        ) : (
          <Space direction="vertical" size="middle" style={{ width: "100%" }}>
            <Card>
              <Space size="large" wrap>
                <span>周期：{weeklyRange(report.week_start)}</span>
                <span>状态：已保存</span>
                <span>更新时间：{formatDateTime(report.updated_at)}</span>
              </Space>
            </Card>
            <Card title="周报正文">
              {report.content.trim() ? <MarkdownViewer value={report.content} /> : <Empty description="暂无周报内容" />}
            </Card>
          </Space>
        )}
      </PagePanel>
    );
  }

  return (
    <PagePanel
      title="部门周报"
      description="管理部门周报正文，支持直接填写和保存修改。"
      breadcrumbs={[{ title: "报告" }, { title: "周报" }]}
      className="reports-page aidashboard-list"
      showNav={false}
    >
      {!modalMode ? (
      <RequirementMetricGrid>
        <RequirementMetricCard
          tone="primary"
          icon={<CalendarOutlined />}
          loading={reportQuery.isLoading}
          metric={{
            key: "week",
            title: "周报周期",
            value: dayjs(weekStart).format("MM-DD"),
              description: `${displayWeekStart} 至 ${displayWeekEnd}`
          }}
        />
        <RequirementMetricCard
          tone="info"
          icon={<FileTextOutlined />}
          loading={reportQuery.isLoading}
          metric={{
            key: "saved",
            title: "保存状态",
            value: report?.content?.trim() ? 1 : 0,
            description: report?.content?.trim() ? "已保存" : "暂无报告"
          }}
        />
      </RequirementMetricGrid>
      ) : null}

      <div className="reports-toolbar">
        <div className="reports-toolbar__meta">
          <strong>
            {effectiveTab === "draft"
              ? "部门周报"
              : effectiveTab === "teams"
                ? "小组周报记录"
                : "部门周报历史"}
          </strong>
          <span>·</span>
          <span>
            {displayWeekStart} 至 {displayWeekEnd}
          </span>
        </div>
        <div className="reports-toolbar__right">
          {scopeTabs}
          {modalMode ? null : (
            <Segmented
              value={effectiveTab}
              onChange={(v) => setTab(v as "draft" | "history" | "teams")}
              options={[
                { label: "周报正文", value: "draft" },
                ...(showHistory
                  ? [
                      { label: "小组记录", value: "teams" },
                      { label: "部门历史", value: "history" }
                    ]
                  : [])
              ]}
            />
          )}
          {weekPicker}
          {effectiveTab === "draft" && !editorContent.trim() ? (
            <Button onClick={openManualEditor}>直接填写</Button>
          ) : null}
        </div>
      </div>

      {effectiveTab === "teams" && showHistory ? (
        <TeamWeeklyHistory query={teamHistoryQuery} reports={teamHistory} />
      ) : effectiveTab === "history" && showHistory ? (
        historyQuery.isError ? (
          <Alert
            type="error"
            showIcon
            message="部门周报历史加载失败"
            description={errorMessage(historyQuery.error)}
          />
        ) : historyQuery.isLoading ? (
          <ReportsSkeleton />
        ) : history.length === 0 ? (
          <ReportsEmpty description="暂无部门周报历史" />
        ) : (
          <WeeklyReportCards
            reports={history.map((r) => ({
              id: r.id,
              title: "部门周报",
              date: formatWeekDate(r.week_start),
              content: r.content,
              done: Boolean(r.content?.trim())
            }))}
          />
        )
      ) : reportQuery.isError ? (
        <Alert
          type="error"
          showIcon
          message="部门周报加载失败"
          description={errorMessage(reportQuery.error)}
        />
      ) : reportQuery.isLoading ? (
        <ReportsSkeleton />
      ) : !modalMode && !report && !editorContent.trim() && !contentTouched ? (
        <ReportsEmpty description="暂无部门周报，可直接填写。" />
      ) : (
        <section className="reports-team-card">
          <header className="reports-team-card__head">
            <span className="reports-team-card__title">部门周报</span>
            <span className="reports-team-card__meta">
              <span className={`reports-tag ${report?.content?.trim() ? "is-submitted" : "is-team"}`}>
                {report?.content?.trim() ? "已保存" : "暂无报告"}
              </span>
              <span>{formatWeekDate(report?.week_start ?? weekStart)}</span>
              {!modalMode && !editing ? (
                <Button
                  size="small"
                  onClick={() => {
                    setEditing(true);
                    setContent(editorContent);
                    setContentTouched(true);
                  }}
                >
                  编辑
                </Button>
              ) : null}
            </span>
          </header>
          {modalMode || editing || !report ? (
            <div className="reports-edit-shell">
              <TextArea
                rows={12}
                className="reports-weekly-editor"
                value={editorContent}
                onChange={(e) => {
                  setContent(e.target.value);
                  setContentTouched(true);
                }}
              />
              <div className="reports-edit-shell__actions">
                <Button
                  onClick={() => {
                    setEditing(false);
                  }}
                >
                  取消
                </Button>
                <Button
                  loading={saveMutation.isPending}
                  onClick={() => saveMutation.mutate()}
                >
                  保存周报
                </Button>
              </div>
            </div>
          ) : (
            <p className="reports-team-card__body">{report.content}</p>
          )}
        </section>
      )}
    </PagePanel>
  );
}

export function DepartmentWeeklyReportModal({
  open,
  weekStart,
  weekEnd,
  readOnly = false,
  allowWeekSwitch = false,
  onClose,
  onDone
}: {
  open: boolean;
  weekStart: string;
  weekEnd: string;
  readOnly?: boolean;
  allowWeekSwitch?: boolean;
  onClose: () => void;
  onDone?: () => void;
}) {
  return (
    <WeeklyReportEditorModal
      open={open}
      scope="department"
      weekStart={weekStart}
      weekEnd={weekEnd}
      readOnly={readOnly}
      allowWeekSwitch={allowWeekSwitch}
      onClose={onClose}
      onDone={onDone}
    />
  );
}

function WeeklyReportCards({
  reports
}: {
  reports: Array<{ id: string; title: string; date: string; content: string; done: boolean }>;
}) {
  return (
    <div className="reports-day-grid">
      {reports.map((report) => (
        <article key={report.id} className="reports-report-card is-auto">
          <header className="reports-report-card__head">
            <span className="reports-report-card__head-left">
              <span className="reports-report-card__author">{report.title}</span>
              <span className={`reports-tag ${report.done ? "is-submitted" : "is-team"}`}>
                {report.done ? "已保存" : "暂无报告"}
              </span>
            </span>
            <span className="reports-report-card__date">{report.date}</span>
          </header>
          <p className="reports-report-card__content">{report.content}</p>
        </article>
      ))}
    </div>
  );
}
