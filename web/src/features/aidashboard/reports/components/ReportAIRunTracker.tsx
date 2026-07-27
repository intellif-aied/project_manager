import { Button, App } from "antd";
import { useQueries, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useRef } from "react";
import { useNavigate } from "react-router-dom";

import { useAuth } from "@/shared/auth/authContext";
import { HttpError } from "@/shared/request/types";

import { fetchManagedAgentRun } from "../../api/client";
import type { AIRun, ReportType } from "../../api/types";
import {
  clearReportAIRun,
  REPORT_AI_RUN_STORAGE_PREFIX,
  useReportAIRunStore,
  type ReportAIRunEntry
} from "../reportAIRunTracking";
import { reportNotificationDestination } from "../reportNotificationNavigation";

function reportName(reportType: ReportType) {
  if (reportType === "personal_weekly") return "个人周报";
  if (reportType === "team_daily") return "小组日报";
  if (reportType === "team_weekly") return "小组周报";
  if (reportType === "department_daily") return "部门日报";
  if (reportType === "department_weekly") return "部门周报";
  return "日报";
}

function periodLabel(entry: ReportAIRunEntry) {
  if (entry.period.date) return entry.period.date;
  if (entry.period.week_start && entry.period.week_end) {
    return `${entry.period.week_start} 至 ${entry.period.week_end}`;
  }
  return entry.period.week_start ?? "当前周期";
}

function isTerminalRun(run?: AIRun) {
  return run?.status === "succeeded" || run?.status === "failed" || run?.status === "timeout";
}

export function ReportAIRunTracker() {
  const { notification } = App.useApp();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { user } = useAuth();
  const runs = useReportAIRunStore((state) => state.runs);
  const syncFromStorage = useReportAIRunStore((state) => state.syncFromStorage);
  const handledRunIds = useRef(new Set<string>());
  const currentUserId = user?.id;
  const entries = useMemo(
    () =>
      Object.values(runs)
        .filter((entry) => entry.userId === currentUserId)
        .sort((left, right) => left.storageKey.localeCompare(right.storageKey)),
    [currentUserId, runs]
  );

  const runQueries = useQueries({
    queries: entries.map((entry) => ({
      queryKey: ["managed-agent-run", entry.runId],
      queryFn: () => fetchManagedAgentRun(entry.runId, { skipErrorHandler: true }),
      retry: (failureCount: number, error: Error) => {
        if (error instanceof HttpError && (error.status === 403 || error.status === 404)) {
          return false;
        }
        return failureCount < 2;
      },
      refetchInterval: (query: { state: { data?: AIRun } }) => {
        const status = query.state.data?.status;
        return status === "succeeded" || status === "failed" || status === "timeout" ? false : 2500;
      }
    }))
  });

  useEffect(() => {
    const handleStorage = (event: StorageEvent) => {
      if (event.key && !event.key.startsWith(REPORT_AI_RUN_STORAGE_PREFIX)) return;
      syncFromStorage();
    };
    window.addEventListener("storage", handleStorage);
    return () => window.removeEventListener("storage", handleStorage);
  }, [syncFromStorage]);

  useEffect(() => {
    entries.forEach((entry, index) => {
      const query = runQueries[index];
      const error = query?.error;
      if (error instanceof HttpError && (error.status === 403 || error.status === 404)) {
        clearReportAIRun(entry.storageKey, entry.runId);
        return;
      }

      const run = query?.data;
      if (!isTerminalRun(run) || !run || handledRunIds.current.has(run.id)) return;
      handledRunIds.current.add(run.id);
      const key = `report-ai-run:${run.id}`;
      const destination = reportNotificationDestination(entry, run);
      const name = reportName(entry.reportType);
      const openReport = () => {
        notification.destroy(key);
        navigate(destination);
      };

      if (run.status === "succeeded") {
        void Promise.all([
          queryClient.invalidateQueries({ queryKey: ["reports"] }),
          queryClient.invalidateQueries({ queryKey: ["team-report-today"] }),
          queryClient.invalidateQueries({ queryKey: ["department-report-today"] }),
          queryClient.invalidateQueries({ queryKey: ["team-report-sources"] }),
          queryClient.invalidateQueries({ queryKey: ["department-report-sources"] }),
          queryClient.invalidateQueries({ queryKey: ["managed-agent-runs"] })
        ]).finally(() => clearReportAIRun(entry.storageKey, run.id));
        notification.success({
          key,
          title: `${name}已生成`,
          description: `${periodLabel(entry)} 的报告已经生成完成，可以打开查看或继续编辑。`,
          actions: (
            <Button type="primary" size="small" onClick={openReport}>
              打开报告
            </Button>
          ),
          duration: false,
          placement: "topRight"
        });
        return;
      }

      clearReportAIRun(entry.storageKey, run.id);
      notification.error({
        key,
        title: `${name}生成失败`,
        description: run.error_message || "后台生成未完成，请打开报告后重新尝试。",
        actions: (
          <Button size="small" onClick={openReport}>
            打开并重试
          </Button>
        ),
        duration: false,
        placement: "topRight"
      });
    });
  }, [entries, navigate, notification, queryClient, runQueries]);

  return null;
}
