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

assert.match(controls, /startReportAgentRun\("default"/, "reports must run the default Report Agent");
assert.match(client, /\/ai-assets\/report-agents\/\$\{agentId\}\/runs/, "client must use Report Agent runs");
assert.doesNotMatch(client, /\/reports\/today\/draft/, "legacy report draft endpoint must stay removed");
assert.doesNotMatch(client, /\/reports\/.+\/generate/, "legacy report generation endpoints must stay removed");

console.log("report agent workflow contract checks passed");
