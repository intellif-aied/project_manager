/* global console, process */

import assert from "node:assert/strict";
import { Buffer } from "node:buffer";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import ts from "typescript";

const root = process.cwd();
const controls = readFileSync(
  resolve(root, "src/features/aidashboard/reports/components/ReportAIGenerateControls.tsx"),
  "utf8"
);
const runTracking = readFileSync(
  resolve(root, "src/features/aidashboard/reports/reportAIRunTracking.ts"),
  "utf8"
);
const runTracker = readFileSync(
  resolve(root, "src/features/aidashboard/reports/components/ReportAIRunTracker.tsx"),
  "utf8"
);
const notificationNavigation = readFileSync(
  resolve(root, "src/features/aidashboard/reports/reportNotificationNavigation.ts"),
  "utf8"
);
const client = readFileSync(resolve(root, "src/features/aidashboard/api/client.ts"), "utf8");
const agentAssets = readFileSync(
  resolve(root, "src/features/aidashboard/ai-assets/utils/agentAssets.ts"),
  "utf8"
);
const aiAssetsPage = readFileSync(
  resolve(root, "src/features/aidashboard/ai-assets/pages/AIAssetsPage.tsx"),
  "utf8"
);
const routes = readFileSync(resolve(root, "src/router/routes.tsx"), "utf8");
const agentRunPage = readFileSync(
  resolve(root, "src/features/aidashboard/ai-assets/pages/AgentRunPage.tsx"),
  "utf8"
);
const requirementsPage = readFileSync(
  resolve(root, "src/features/aidashboard/requirements/pages/RequirementsListPage.tsx"),
  "utf8"
);
const dailyReportModal = readFileSync(
  resolve(root, "src/features/aidashboard/reports/components/DailyReportGenerateModal.tsx"),
  "utf8"
);
const reportsPage = readFileSync(
  resolve(root, "src/features/aidashboard/reports/pages/ReportsPage.tsx"),
  "utf8"
);
const weeklyReportsPage = readFileSync(
  resolve(root, "src/features/aidashboard/reports/pages/WeeklyReportsPage.tsx"),
  "utf8"
);
const dashboardPage = readFileSync(
  resolve(root, "src/features/aidashboard/dashboard/DashboardPage.tsx"),
  "utf8"
);
const businessTime = readFileSync(resolve(root, "src/shared/utils/businessTime.ts"), "utf8");

const notificationNavigationModule = await import(
  `data:text/javascript;base64,${Buffer.from(
    ts.transpileModule(notificationNavigation, {
      compilerOptions: {
        module: ts.ModuleKind.ES2022,
        target: ts.ScriptTarget.ES2022,
        importsNotUsedAsValues: ts.ImportsNotUsedAsValues.Remove
      }
    }).outputText
  ).toString("base64")}`
);
const { reportNotificationDestination } = notificationNavigationModule;

const personalDailyEntry = {
  reportType: "personal_daily",
  period: { date: "2026-07-26" },
  target: { type: "self" }
};
assert.equal(
  reportNotificationDestination(personalDailyEntry, {
    status: "succeeded",
    business_id: "report-123",
    output_ref_json: { report_id: "report-fallback" }
  }),
  "/reports/daily?tab=personal&open=report&date=2026-07-26&report_id=report-123",
  "successful daily notifications must open the exact report and period"
);
assert.equal(
  reportNotificationDestination(personalDailyEntry, { status: "failed" }),
  "/reports/daily?tab=personal&open=report&date=2026-07-26",
  "failed daily notifications must open the exact period for retry"
);
assert.equal(
  reportNotificationDestination(
    {
      reportType: "department_weekly",
      period: { week_start: "2026-07-20", week_end: "2026-07-26" },
      target: { type: "department", department_id: "department-1" }
    },
    { status: "succeeded", business_id: "weekly-report-1" }
  ),
  "/reports/weekly?tab=department&open=report&week_start=2026-07-20&department_id=department-1",
  "weekly notifications must open the exact scope and week"
);

assert.match(
  controls,
  /startReportAgentRun\(\s*"default"/,
  "reports must run the default Report Agent"
);
assert.doesNotMatch(
  controls,
  /run\.error_message\s*\|\|/,
  "report controls must not expose raw Agent platform errors"
);
assert.match(
  controls,
  /日报生成未完成，请重新生成/,
  "report controls must show a stable business failure message"
);
assert.doesNotMatch(
  runTracker,
  /run\.error_message\s*\|\|/,
  "background report notifications must not expose raw Agent platform errors"
);
assert.match(
  controls,
  /createDefaultReportAgent\(\)/,
  "missing default Report Agent must be initialized in place"
);
assert.match(
  controls,
  /idempotency_key:\s*idempotencyKey/,
  "each click must submit one idempotent Run request"
);
assert.match(
  controls,
  /uuidv4\(\)/,
  "manual Report Run idempotency keys must use the UUID library"
);
assert.match(agentRunPage, /uuidv4\(\)/, "Agent Run report requests must use the UUID library");
assert.doesNotMatch(
  `${controls}\n${agentRunPage}`,
  /crypto\.randomUUID\(\)/,
  "Report Run creation must not depend on secure-context browser UUID APIs"
);
assert.match(
  controls,
  /selected_session_slice_keys:[\s\S]*selectedSessionSources\.map/,
  "the Run request must carry the actual selected slice identities directly"
);
assert.doesNotMatch(
  controls,
  /createReportSourceSelection\(/,
  "the frontend must not prepare a Selection before creating a Run"
);
assert.doesNotMatch(
  controls,
  /large_context_confirmed|confirmLargeReportContext|confirmation_required/,
  "large context must not add a second frontend-driven workflow"
);
assert.match(
  controls,
  /input_ref_json\?\.large_context_warning\s*!==\s*true/,
  "large context warning must be read from the existing Run response"
);
assert.match(
  controls,
  /markReportAIRunLargeContextWarningShown/,
  "large context warning must be marked once per Run"
);
assert.match(
  controls,
  /报告上下文较大，可能消耗较多 Token，请确认所选模型支持足够的上下文长度。/,
  "large context warning must be advisory and must not expose Digest state"
);
assert.match(
  controls,
  /code === "REPORT_SOURCE_UNAVAILABLE"/,
  "report generation must map the stable source-unavailable error code"
);
assert.match(
  controls,
  /所选日期暂无可用 Session。请先上传当天 Session，或手动选择其他 Session 后重试。/,
  "source-unavailable errors must show an actionable Chinese message"
);
assert.match(
  controls,
  /const text = reportRunErrorMessage\(err\)/,
  "report submission errors must use the report error-code mapper"
);
assert.match(
  runTracking,
  /largeContextWarningShown/,
  "large context warning marker must survive a page refresh"
);
assert.match(
  runTracker,
  /reportNotificationDestination\(entry, run\)/,
  "terminal Run notifications must use the report deep-link contract"
);
assert.doesNotMatch(
  runTracker,
  /return "\/reports\/weekly"|return "\/reports\/daily/,
  "Run notifications must not fall back to report list-only destinations"
);
assert.match(
  reportsPage,
  /activeGenerateTarget = notificationTarget \?\? generateTarget/,
  "daily notification links must open the existing report editor"
);
assert.match(
  weeklyReportsPage,
  /activeModalTarget = notificationTarget \?\? modalTarget/,
  "weekly notification links must open the existing report editor"
);
assert.doesNotMatch(controls, /mock_large_report_context|largeReportContextMockEnabled/);
assert.doesNotMatch(
  controls,
  /清洗.{0,20}(?:弹窗|提示|确认)|Digest.{0,20}(?:弹窗|提示|确认)/,
  "large-context confirmation must not expose internal cleaning details"
);
assert.doesNotMatch(
  controls,
  /reportSourceSelectionEnabled/,
  "source handling must not depend on a rollout branch"
);
assert.doesNotMatch(
  `${dailyReportModal}\n${weeklyReportsPage}`,
  /fetchReportSourceCapability|reportSourceSelectionEnabled|selectedSessionSliceKeys/,
  "daily and weekly report entry points must use one source-selection path for every user"
);
assert.match(
  controls,
  /立即初始化并生成/,
  "first-use flow must initialize and continue generation without navigation"
);
assert.doesNotMatch(
  controls,
  /navigate\(aiAssetsPath\("agents"\)\)/,
  "first-use report generation must not redirect users to AI assets"
);
assert.match(
  client,
  /\/ai-assets\/report-agents\/\$\{agentId\}\/runs/,
  "client must use Report Agent runs"
);
assert.doesNotMatch(
  client,
  /\/reports\/today\/draft/,
  "legacy report draft endpoint must stay removed"
);
assert.doesNotMatch(
  client,
  /\/reports\/.+\/generate/,
  "legacy report generation endpoints must stay removed"
);
assert.match(
  agentAssets,
  /"calendar_context_json"/,
  "calendar context is an Aida-managed prompt key"
);
assert.match(
  agentAssets,
  /"selected_session_slice_keys_json"/,
  "session slice keys are Aida-managed prompt keys"
);
assert.match(
  agentAssets,
  /"report_source_selection_id"/,
  "report source selection id is an Aida-managed prompt key"
);
assert.match(
  agentAssets,
  /new Set<AssetTab>\(\["agents", "schedules", "runs"\]\)/,
  "personal Skill and MCP tabs must not be addressable"
);
assert.match(
  aiAssetsPage,
  /const PERSONAL_RESOURCE_MANAGEMENT_VISIBLE = false/,
  "personal Skill and MCP management must stay hidden"
);
assert.match(
  routes,
  /path: "\/ai-assets\/skills\/new"[\s\S]*?element: <Navigate to="\/ai-assets" replace \/>/,
  "the legacy Skill create URL must redirect to AI assets"
);
assert.match(
  routes,
  /path: "\/ai-assets\/mcp\/new"[\s\S]*?element: <Navigate to="\/ai-assets" replace \/>/,
  "the legacy MCP create URL must redirect to AI assets"
);
assert.doesNotMatch(
  agentRunPage,
  /const REPORT_SYSTEM_PROMPT_KEYS = new Set/,
  "Agent run page must reuse shared managed prompt keys"
);
assert.match(
  agentRunPage,
  /calendar_context_json: JSON\.stringify\(\s*reportCalendarContextPayload/,
  "Report Agent prompt preview must show the injected calendar context"
);
assert.match(
  agentRunPage,
  /report_source_selection_id: "运行时生成"/,
  "Report Agent prompt preview must mark the source selection id as runtime-managed"
);
assert.match(
  agentRunPage,
  /fetchReportSourceCandidates\(/,
  "Agent run session picker must use report-source slice candidates"
);
assert.doesNotMatch(
  agentRunPage,
  /fetchSessionTokens\(|session_id.*activity_date/,
  "Agent run session picker must not recreate legacy session_id:date keys"
);
assert.doesNotMatch(
  controls,
  /全选查询结果/,
  "report settings must rely on the table checkbox for current-page selection"
);
assert.match(
  agentRunPage,
  /全选查询结果/,
  "Agent run session picker must select all query results"
);
assert.match(
  requirementsPage,
  /全选查询结果/,
  "requirement session picker must select all query results"
);
assert.match(
  client,
  /fetchAllSessionTokens\(params: \{\s*from\?: string;\s*to\?: string;/,
  "all-session pagination helper must support both bounded and unbounded pickers"
);
assert.match(
  businessTime,
  /BUSINESS_TIME_ZONE = "Asia\/Shanghai"/,
  "report UI business time must use an explicit Shanghai timezone"
);
assert.match(
  controls,
  /businessDateKey\(record\.activity_start_at\)/,
  "report source dates must use the shared Shanghai formatter"
);
assert.doesNotMatch(
  controls,
  /activity_(?:start|end)_at\?\.slice\(0, 10\)/,
  "report source dates must not slice UTC RFC3339 strings"
);
assert.doesNotMatch(
  agentRunPage,
  /new Date\(iso\)\.toLocaleString/,
  "Agent run source timestamps must not depend on the browser timezone"
);
assert.match(
  `${dailyReportModal}\n${dashboardPage}`,
  /businessDateKey/,
  "daily report defaults must use the Shanghai business date"
);

const shanghaiParts = Object.fromEntries(
  new Intl.DateTimeFormat("en-US", {
    timeZone: "Asia/Shanghai",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23"
  })
    .formatToParts(new Date("2026-07-15T16:11:00Z"))
    .filter((part) => part.type !== "literal")
    .map((part) => [part.type, part.value])
);
assert.deepEqual(
  [
    shanghaiParts.year,
    shanghaiParts.month,
    shanghaiParts.day,
    shanghaiParts.hour,
    shanghaiParts.minute
  ],
  ["2026", "07", "16", "00", "11"],
  "Shanghai midnight conversion must be deterministic"
);

console.log("report agent workflow contract checks passed");
