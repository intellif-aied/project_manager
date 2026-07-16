/* global console, process */

import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const root = process.cwd();
const controls = readFileSync(
  resolve(root, "src/features/aidashboard/reports/components/ReportAIGenerateControls.tsx"),
  "utf8"
);
const client = readFileSync(resolve(root, "src/features/aidashboard/api/client.ts"), "utf8");
const agentAssets = readFileSync(
  resolve(root, "src/features/aidashboard/ai-assets/utils/agentAssets.ts"),
  "utf8"
);
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
const weeklyReportsPage = readFileSync(
  resolve(root, "src/features/aidashboard/reports/pages/WeeklyReportsPage.tsx"),
  "utf8"
);
const dashboardPage = readFileSync(
  resolve(root, "src/features/aidashboard/dashboard/DashboardPage.tsx"),
  "utf8"
);
const businessTime = readFileSync(resolve(root, "src/shared/utils/businessTime.ts"), "utf8");

assert.match(
  controls,
  /startReportAgentRun\(\s*"default"/,
  "reports must run the default Report Agent"
);
assert.match(
  controls,
  /createDefaultReportAgent\(\)/,
  "missing default Report Agent must be initialized in place"
);
assert.match(
  controls,
  /createReportSourceSelection\(/,
  "personal reports must create an immutable source selection"
);
assert.doesNotMatch(
  controls,
  /reportSourceSelectionEnabled|selected_session_slice_keys/,
  "current report controls must not branch back to legacy session slices"
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
