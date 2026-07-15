import { CalendarOutlined, EditOutlined, SaveOutlined } from "@ant-design/icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, App, Button, DatePicker, Empty, Input, Modal, Space, Tag } from "antd";
import { useEffect, useMemo, useState } from "react";
import dayjs from "dayjs";

import {
  fetchDepartmentReport,
  fetchDepartmentReportTodayOrNull,
  fetchMyReports,
  fetchReport,
  fetchTeamReport,
  fetchTeamReportTodayOrNull,
  fetchTodayReport,
  saveDepartmentReportCurrent,
  saveReport,
  saveTeamReportCurrent,
  updateDepartmentReport,
  updateTeamReport
} from "../../api/client";
import type {
  DailyReport,
  DepartmentReport,
  ReportSourceInput,
  ReportType,
  TeamReport
} from "../../api/types";
import { ReportAIGenerateControls, ReportAISettingsPanel } from "./ReportAIGenerateControls";

import "./DailyReportGenerateModal.css";

const { TextArea } = Input;

export type DailyGenerateScope = "personal" | "team" | "department";

interface DailyReportGenerateModalProps {
  open: boolean;
  scope: DailyGenerateScope;
  departmentId?: string;
  reportId?: string;
  reportDate?: string;
  title?: string;
  readOnly?: boolean;
  allowDateSwitch?: boolean;
  onClose: () => void;
  onDone?: (result: DailyReport | TeamReport | DepartmentReport, scope: DailyGenerateScope) => void;
}

function normalizedDate(value?: string) {
  return value ? dayjs(value).format("YYYY-MM-DD") : dayjs().format("YYYY-MM-DD");
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : "请稍后重试";
}

function scopeName(scope: DailyGenerateScope) {
  if (scope === "team") return "小组日报";
  if (scope === "department") return "部门日报";
  return "我的日报";
}

function dailyReportType(scope: DailyGenerateScope): ReportType {
  if (scope === "team") return "team_daily";
  if (scope === "department") return "department_daily";
  return "personal_daily";
}

function dailyReportTarget(scope: DailyGenerateScope, departmentId?: string) {
  if (scope === "team") return { type: "team" as const };
  if (scope === "department") {
    return { type: "department" as const, department_id: departmentId };
  }
  return { type: "self" as const };
}

function reportStatus(report: DailyReport | TeamReport | DepartmentReport | null) {
  if (report && "product_status" in report && report.product_status) {
    if (report.product_status === "generation_failed") return <Tag color="red">生成失败</Tag>;
    if (report.product_status === "missing") return <Tag>暂无报告</Tag>;
    if (report.product_status === "modified") return <Tag color="orange">已编辑</Tag>;
    if (report.product_status === "ai_generated" || report.product_status === "manual") {
      return <Tag color="blue">已保存</Tag>;
    }
  }
  if (!report || !report.content?.trim()) return <Tag>暂无报告</Tag>;
  if ("generation_mode" in report && report.generation_mode === "managed_agent") {
    return report.edited ? <Tag color="orange">已编辑</Tag> : <Tag color="blue">已保存</Tag>;
  }
  if ("submitted_at" in report && report.submitted_at) return <Tag color="green">已保存</Tag>;
  if ("status" in report && report.status === "saved") return <Tag color="blue">已保存</Tag>;
  return <Tag color="blue">已保存</Tag>;
}

export function DailyReportGenerateModal({
  open,
  scope,
  departmentId,
  reportId,
  reportDate,
  title,
  readOnly = false,
  allowDateSwitch = false,
  onClose,
  onDone
}: DailyReportGenerateModalProps) {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [selectedDate, setSelectedDate] = useState(() => normalizedDate(reportDate));
  const date = selectedDate;
  const [content, setContent] = useState("");
  const [contentTouched, setContentTouched] = useState(false);
  const [nextDayPlan, setNextDayPlan] = useState("");
  const [nextDayPlanTouched, setNextDayPlanTouched] = useState(false);
  const [manualMode, setManualMode] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [selectedSessionSources, setSelectedSessionSources] = useState<ReportSourceInput[]>([]);

  const existingPersonalListQuery = useQuery({
    queryKey: ["reports", "daily", "manage-modal", "personal-existing", date],
    queryFn: () =>
      fetchMyReports({
        from: date,
        to: date,
        page: "1",
        page_size: "1"
      }),
    enabled: open && scope === "personal" && !reportId,
    staleTime: 0
  });

  const personalReportId =
    scope === "personal"
      ? (reportId ??
        (existingPersonalListQuery.isFetching ? undefined : existingPersonalListQuery.data?.items[0]?.id))
      : undefined;
  const personalReportQuery = useQuery({
    queryKey: ["reports", "daily", "manage-modal", "personal-report", personalReportId],
    queryFn: () => fetchReport(personalReportId ?? ""),
    enabled: open && scope === "personal" && Boolean(personalReportId),
    staleTime: 0
  });

  const teamReportQuery = useQuery({
    queryKey: ["reports", "daily", "manage-modal", "team-report", reportId, date],
    queryFn: () => (reportId ? fetchTeamReport(reportId) : fetchTeamReportTodayOrNull(date)),
    enabled: open && scope === "team",
    staleTime: 0
  });

  const departmentReportQuery = useQuery({
    queryKey: [
      "reports",
      "daily",
      "manage-modal",
      "department-report",
      reportId,
      date,
      departmentId
    ],
    queryFn: () =>
      reportId
        ? fetchDepartmentReport(reportId, departmentId)
        : fetchDepartmentReportTodayOrNull(date, departmentId),
    enabled: open && scope === "department",
    staleTime: 0
  });

  const currentReport = useMemo(() => {
    if (scope === "personal") return personalReportQuery.data ?? null;
    if (scope === "team") return teamReportQuery.data ?? null;
    return departmentReportQuery.data ?? null;
  }, [departmentReportQuery.data, personalReportQuery.data, scope, teamReportQuery.data]);

  const loading =
    (scope === "personal" &&
      (existingPersonalListQuery.isFetching || personalReportQuery.isLoading)) ||
    (scope === "team" && teamReportQuery.isLoading) ||
    (scope === "department" && departmentReportQuery.isLoading);

  const loadError =
    (scope === "personal" && (existingPersonalListQuery.isError || personalReportQuery.isError)) ||
    (scope === "team" && teamReportQuery.isError) ||
    (scope === "department" && departmentReportQuery.isError);

  const hasContent = Boolean(currentReport?.content?.trim());
  const showEditor = hasContent || manualMode;
  const editorContent = contentTouched ? content : (currentReport?.content ?? "");
  const editorNextDayPlan = nextDayPlanTouched ? nextDayPlan : (currentReport?.next_day_plan ?? "");
  const personalReport = scope === "personal" ? (personalReportQuery.data ?? null) : null;
  const hasUnsavedContentChange =
    (contentTouched && editorContent !== (currentReport?.content ?? "")) ||
    (nextDayPlanTouched && editorNextDayPlan !== (currentReport?.next_day_plan ?? ""));
  const canSwitchDate = allowDateSwitch && !readOnly && !reportId;
  const allowSessionSettings = scope === "personal" && !readOnly;
  const showSessionSettings = allowSessionSettings && settingsOpen;
  const canEditContent = !readOnly;
  const shouldShowEditor = readOnly ? hasContent : showEditor;

  useEffect(() => {
    if (!open) return;
    const timer = window.setTimeout(() => setSelectedDate(normalizedDate(reportDate)), 0);
    return () => window.clearTimeout(timer);
  }, [open, reportDate]);

  useEffect(() => {
    const timer = window.setTimeout(() => {
      if (!open) {
        return;
      }
      setManualMode(false);
      setContent("");
      setContentTouched(false);
      setNextDayPlan("");
      setNextDayPlanTouched(false);
      setSettingsOpen(false);
      setSelectedSessionSources([]);
    }, 0);
    return () => window.clearTimeout(timer);
  }, [date, open, reportId, scope]);

  const changeDate = (nextDate: string) => {
    if (nextDate === date) return;
    setSelectedDate(nextDate);
  };

  const handleDateChange = (value: dayjs.Dayjs | null) => {
    if (!value) return;
    const nextDate = value.format("YYYY-MM-DD");
    if (!hasUnsavedContentChange) {
      changeDate(nextDate);
      return;
    }
    Modal.confirm({
      title: "当前内容尚未保存",
      content: "切换日期后会重新加载报告内容，未保存的修改将丢失。是否继续？",
      okText: "切换",
      cancelText: "继续编辑",
      onOk: () => changeDate(nextDate)
    });
  };

  const saveMutation = useMutation({
    mutationFn: async () => {
      const nextContent = editorContent.trim();
      if (!nextContent) {
        throw new Error("请先填写日报内容");
      }

      if (scope === "personal") {
        const report = personalReport ?? (await fetchTodayReport(date));
        return saveReport(report.id, {
          content: nextContent,
          next_day_plan: editorNextDayPlan.trim(),
          session_ids: report.session_ids ?? []
        });
      }

      if (scope === "team") {
        if (currentReport?.id) {
          return updateTeamReport(currentReport.id, {
            content: nextContent,
            next_day_plan: editorNextDayPlan.trim()
          });
        }
        return saveTeamReportCurrent({
          report_date: date,
          content: nextContent,
          next_day_plan: editorNextDayPlan.trim()
        });
      }

      if (currentReport?.id) {
        return updateDepartmentReport(currentReport.id, {
          content: nextContent,
          next_day_plan: editorNextDayPlan.trim()
        }, departmentId);
      }
      return saveDepartmentReportCurrent({
        department_id: departmentId,
        report_date: date,
        content: nextContent,
        next_day_plan: editorNextDayPlan.trim()
      });
    },
    onSuccess: (result) => {
      setContentTouched(false);
      setNextDayPlanTouched(false);
      void queryClient.invalidateQueries({ queryKey: ["reports", "daily"] });
      void queryClient.invalidateQueries({ queryKey: ["reports"] });
      message.success("报告已保存");
      onDone?.(result, scope);
      onClose();
    },
    onError: (error: unknown) => message.error(errorMessage(error))
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
    setManualMode(false);
    setContent("");
    setContentTouched(false);
    void queryClient.invalidateQueries({ queryKey: ["reports", "daily"] });
    void queryClient.invalidateQueries({ queryKey: ["reports"] });
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
      title={title ?? `${scopeName(scope)}内容管理`}
      open={open}
      width={860}
      onCancel={handleClose}
      footer={
        <Space>
          {canEditContent ? (
            <ReportAIGenerateControls
              reportType={dailyReportType(scope)}
              period={{ date }}
              target={dailyReportTarget(scope, departmentId)}
              allowSessionSelection={allowSessionSettings}
              settingsOpen={showSessionSettings}
              selectedSessionSources={selectedSessionSources}
              onToggleSettings={() => setSettingsOpen((value) => !value)}
              disabled={loading || saveMutation.isPending}
              onBeforeGenerate={confirmBeforeAIGenerate}
              onGenerated={handleAIGenerated}
            />
          ) : null}
          <Button onClick={handleClose} disabled={saveMutation.isPending}>
            {readOnly ? "关闭" : "取消"}
          </Button>
          {canEditContent && showEditor ? (
            <Button
              type="primary"
              icon={<SaveOutlined />}
              loading={saveMutation.isPending}
              disabled={loading}
              onClick={() => saveMutation.mutate()}
            >
              保存
            </Button>
          ) : canEditContent ? (
            <Button
              type="primary"
              icon={<EditOutlined />}
              loading={loading}
              onClick={() => {
                setManualMode(true);
                setContent(currentReport?.content ?? "");
                setContentTouched(true);
              }}
            >
              直接填写
            </Button>
          ) : null}
        </Space>
      }
    >
      <div className="console-report-modal console-report-management">
        {loadError ? (
          <Alert type="error" showIcon message="报告加载失败" description="请稍后重试" />
        ) : null}
        <div className="console-report-management__summary">
          <span>
            <strong>{date}</strong>
            {canSwitchDate ? (
              <DatePicker
                className="console-report-inline-picker"
                value={dayjs(date)}
                allowClear={false}
                suffixIcon={<CalendarOutlined />}
                inputReadOnly
                onChange={handleDateChange}
              />
            ) : null}
            <em>{scopeName(scope)}</em>
          </span>
          {reportStatus(currentReport)}
        </div>
        <div className="console-report-management__content">
          <div className="console-report-management__main">
            {loading ? (
              <div className="console-session-empty">正在加载报告内容...</div>
            ) : shouldShowEditor ? (
              <div className="console-report-editor-layout">
                <div className="console-report-editor-layout__main">
                  <div className="console-session-modal__section">
                    <strong>报告正文</strong>
                  </div>
                  <TextArea
                    className="console-report-editor-layout__content-input"
                    rows={18}
                    readOnly={readOnly}
                    value={editorContent}
                    onChange={(event) => {
                      if (readOnly) return;
                      setContent(event.target.value);
                      setContentTouched(true);
                    }}
                    placeholder="请输入报告内容"
                  />
                  <div className="console-session-modal__section console-report-editor-layout__next-day-heading">
                    <strong>明日计划（可选）</strong>
                  </div>
                  <TextArea
                    className="console-report-editor-layout__next-day-input"
                    rows={2}
                    autoSize={{ minRows: 2, maxRows: 2 }}
                    readOnly={readOnly}
                    value={editorNextDayPlan}
                    onChange={(event) => {
                      if (readOnly) return;
                      setNextDayPlan(event.target.value.split(/\r?\n/).slice(0, 2).join("\n"));
                      setNextDayPlanTouched(true);
                    }}
                    placeholder="请输入明日计划"
                  />
                </div>
              </div>
            ) : (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无日报，可直接填写。" />
            )}
          </div>
          {allowSessionSettings ? (
            <ReportAISettingsPanel
              key={`daily:${date}`}
              open={settingsOpen}
              reportType="personal_daily"
              period={{ date }}
              selectedSources={selectedSessionSources}
              onSelectedSourcesChange={setSelectedSessionSources}
              onClose={() => setSettingsOpen(false)}
              variant="drawer"
            />
          ) : null}
        </div>
      </div>
    </Modal>
  );
}
