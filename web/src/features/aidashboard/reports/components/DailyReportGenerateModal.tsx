import { EditOutlined, SaveOutlined } from "@ant-design/icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, App, Button, Empty, Input, Modal, Space, Tag } from "antd";
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
  TeamReport
} from "../../api/types";

import "./DailyReportGenerateModal.css";

const { TextArea } = Input;

export type DailyGenerateScope = "personal" | "team" | "department";

interface DailyReportGenerateModalProps {
  open: boolean;
  scope: DailyGenerateScope;
  reportId?: string;
  reportDate?: string;
  title?: string;
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
  reportId,
  reportDate,
  title,
  onClose,
  onDone
}: DailyReportGenerateModalProps) {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const date = normalizedDate(reportDate);
  const [content, setContent] = useState("");
  const [contentTouched, setContentTouched] = useState(false);
  const [manualMode, setManualMode] = useState(false);

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

  const personalReportId = scope === "personal" ? reportId ?? existingPersonalListQuery.data?.items[0]?.id : undefined;
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
    queryKey: ["reports", "daily", "manage-modal", "department-report", reportId, date],
    queryFn: () => (reportId ? fetchDepartmentReport(reportId) : fetchDepartmentReportTodayOrNull(date)),
    enabled: open && scope === "department",
    staleTime: 0
  });

  const currentReport = useMemo(() => {
    if (scope === "personal") return personalReportQuery.data ?? null;
    if (scope === "team") return teamReportQuery.data ?? null;
    return departmentReportQuery.data ?? null;
  }, [departmentReportQuery.data, personalReportQuery.data, scope, teamReportQuery.data]);

  const loading =
    (scope === "personal" && (existingPersonalListQuery.isLoading || personalReportQuery.isLoading)) ||
    (scope === "team" && teamReportQuery.isLoading) ||
    (scope === "department" && departmentReportQuery.isLoading);

  const loadError =
    (scope === "personal" && (existingPersonalListQuery.isError || personalReportQuery.isError)) ||
    (scope === "team" && teamReportQuery.isError) ||
    (scope === "department" && departmentReportQuery.isError);

  const hasContent = Boolean(currentReport?.content?.trim());
  const showEditor = hasContent || manualMode;
  const editorContent = contentTouched ? content : currentReport?.content ?? "";
  const personalReport = scope === "personal" ? personalReportQuery.data ?? null : null;
  const hasUnsavedContentChange = contentTouched && editorContent !== (currentReport?.content ?? "");

  useEffect(() => {
    const timer = window.setTimeout(() => {
      if (!open) {
        return;
      }
      setManualMode(false);
      setContent("");
      setContentTouched(false);
    }, 0);
    return () => window.clearTimeout(timer);
  }, [date, open, reportId, scope]);

  const saveMutation = useMutation({
    mutationFn: async () => {
      const nextContent = editorContent.trim();
      if (!nextContent) {
        throw new Error("请先填写日报内容");
      }

      if (scope === "personal") {
        const report = personalReport ?? await fetchTodayReport();
        return saveReport(report.id, {
          content: nextContent,
          session_ids: report.session_ids ?? []
        });
      }

      if (scope === "team") {
        if (currentReport) {
          return updateTeamReport(currentReport.id, { content: nextContent });
        }
        return saveTeamReportCurrent({ report_date: date, content: nextContent });
      }

      if (currentReport) {
        return updateDepartmentReport(currentReport.id, { content: nextContent });
      }
      return saveDepartmentReportCurrent({ report_date: date, content: nextContent });
    },
    onSuccess: (result) => {
      setContentTouched(false);
      void queryClient.invalidateQueries({ queryKey: ["reports", "daily"] });
      void queryClient.invalidateQueries({ queryKey: ["reports"] });
      message.success("报告已保存");
      onDone?.(result, scope);
      onClose();
    },
    onError: (error: unknown) => message.error(errorMessage(error))
  });

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
          <Button onClick={handleClose} disabled={saveMutation.isPending}>
            取消
          </Button>
          {showEditor ? (
            <Button
              type="primary"
              icon={<SaveOutlined />}
              loading={saveMutation.isPending}
              disabled={loading}
              onClick={() => saveMutation.mutate()}
            >
              保存
            </Button>
          ) : (
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
          )}
        </Space>
      }
    >
      <div className="console-report-modal console-report-management">
        {loadError ? <Alert type="error" showIcon message="报告加载失败" description="请稍后重试" /> : null}
        <div className="console-report-management__summary">
          <span>
            <strong>{date}</strong>
            <em>{scopeName(scope)}</em>
          </span>
          {reportStatus(currentReport)}
        </div>
        {loading ? (
          <div className="console-session-empty">正在加载报告内容...</div>
        ) : showEditor ? (
          <div className="console-report-editor-layout">
            <div className="console-report-editor-layout__main">
              <div className="console-session-modal__section">
                <strong>报告正文</strong>
                <span>可编辑保存当前报告内容。</span>
              </div>
              <TextArea
                rows={18}
                value={editorContent}
                onChange={(event) => {
                  setContent(event.target.value);
                  setContentTouched(true);
                }}
                placeholder="请输入报告内容"
              />
            </div>
          </div>
        ) : (
          <Empty
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description="暂无日报，可直接填写。"
          />
        )}
      </div>
    </Modal>
  );
}
