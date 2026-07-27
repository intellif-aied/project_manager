import type { AIRun, ReportType } from "../api/types";

import type { ReportAIRunEntry } from "./reportAIRunTracking";

function reportID(run: AIRun) {
  if (run.business_id) return run.business_id;
  const outputReportID = run.output_ref_json?.report_id;
  return typeof outputReportID === "string" && outputReportID ? outputReportID : undefined;
}

function dailyTab(reportType: ReportType) {
  if (reportType === "team_daily") return "team";
  if (reportType === "department_daily") return "department";
  return "personal";
}

function weeklyTab(reportType: ReportType) {
  if (reportType === "team_weekly") return "team";
  if (reportType === "department_weekly") return "department";
  return "mine";
}

export function reportNotificationDestination(entry: ReportAIRunEntry, run: AIRun) {
  const params = new URLSearchParams();
  const isWeekly = entry.reportType.endsWith("_weekly");
  params.set("tab", isWeekly ? weeklyTab(entry.reportType) : dailyTab(entry.reportType));
  params.set("open", "report");

  if (isWeekly) {
    if (entry.period.week_start) params.set("week_start", entry.period.week_start);
  } else if (entry.period.date) {
    params.set("date", entry.period.date);
  }

  const completedReportID = reportID(run);
  if (completedReportID && !isWeekly) params.set("report_id", completedReportID);
  if (entry.target.department_id) params.set("department_id", entry.target.department_id);

  return `${isWeekly ? "/reports/weekly" : "/reports/daily"}?${params.toString()}`;
}
