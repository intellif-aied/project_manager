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

console.log("report agent workflow contract checks passed");
