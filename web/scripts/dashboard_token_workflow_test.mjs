/* global console, process */

import { readFileSync } from "node:fs";
import { Buffer } from "node:buffer";
import { resolve } from "node:path";
import assert from "node:assert/strict";
import ts from "typescript";

const root = process.cwd();
const dashboardPath = resolve(root, "src/features/aidashboard/dashboard/DashboardPage.tsx");
const statsPath = resolve(root, "src/features/aidashboard/dashboard/dashboardTokenStats.ts");
const clientPath = resolve(root, "src/features/aidashboard/api/client.ts");
const tokensPagePath = resolve(root, "src/features/aidashboard/tokens/pages/TokensPage.tsx");

const dashboard = readFileSync(dashboardPath, "utf8");
const statsSource = readFileSync(statsPath, "utf8");
const client = readFileSync(clientPath, "utf8");
const tokensPage = readFileSync(tokensPagePath, "utf8");

assert.match(client, /fetchSessionTokens/, "API client should expose fetchSessionTokens");
assert.match(client, /fetchTokens/, "API client should expose fetchTokens");
assert.match(
  tokensPage,
  /<TokenAnalyticsPage scope="mine" \/>/,
  "Token page must use the current analytics implementation for every authenticated user"
);
assert.doesNotMatch(
  tokensPage,
  /fetchTokenAnalyticsCapability|LegacyTokensPage/,
  "Token page must not branch by a per-user capability allowlist"
);
assert.doesNotMatch(dashboard, /TOKEN_DATA/, "Dashboard Token card must not use TOKEN_DATA mock");
assert.doesNotMatch(dashboard, /previewRole/, "Dashboard should not keep prototype role switching state");
assert.doesNotMatch(dashboard, /ROLE_OPTIONS/, "Dashboard should not keep prototype role options");
assert.doesNotMatch(dashboard, /原型角色/, "Dashboard should not render prototype role switcher");
assert.match(dashboard, /const dashboardRole = getDashboardRole\(user\?\.role\)/, "Dashboard should derive modules from current user role");
assert.match(dashboard, /if \(role === "admin"\) return "director"/, "Admin should use director dashboard modules");
assert.match(dashboard, /fetchAllSessionTokens\(\{[\s\S]*from: tokenDateRange\.from,[\s\S]*to: tokenDateRange\.to/, "Dashboard should load all /tokens/sessions pages with from/to");
assert.match(dashboard, /fetchTokens\(\{[\s\S]*group_by: "team"/, "Director group tokens should use /tokens group_by=team");
assert.match(dashboard, /Token 数据加载失败/, "Dashboard should show a token-only error state");
assert.doesNotMatch(dashboard, /group\.sessions/, "Token group UI should not show unsupported group session_count");
assert.doesNotMatch(dashboard, /group\.uploaders/, "Token group UI should not show unsupported group uploader_count");

const transpiled = ts.transpileModule(statsSource, {
  compilerOptions: {
    module: ts.ModuleKind.ES2022,
    target: ts.ScriptTarget.ES2022,
    importsNotUsedAsValues: ts.ImportsNotUsedAsValues.Remove
  }
}).outputText;
const tempModuleUrl = `data:text/javascript;base64,${Buffer.from(transpiled).toString("base64")}`;
const {
  aggregateDashboardTokenReport,
  getDashboardTokenDateRange,
  formatDashboardTokens
} = await import(tempModuleUrl);

const range = { from: "2026-06-23", to: "2026-06-25" };
const report = aggregateDashboardTokenReport(
  [
    { session_id: "s1", user_id: "u1", user_name: "张三", started_at: "2026-06-23T09:00:00Z", total_tokens: 1200 },
    { session_id: "s2", user_id: "u2", user_name: "李四", started_at: "2026-06-23T12:00:00Z", total_tokens: 800 },
    { session_id: "s3", user_id: "u1", user_name: "张三", started_at: "2026-06-25T10:00:00Z", total_tokens: 1_000_000 },
    null,
    undefined
  ],
  range,
  {
    showUploaders: true,
    mineSessions: [
      { session_id: "s1", user_id: "u1", started_at: "2026-06-23T09:00:00Z", total_tokens: 1200 }
    ],
    teamAggregation: {
      total: 1_002_000,
      input_sum: 0,
      output_sum: 0,
      groups: [
        { key: "team-a", label: "芯片组", value: 700_000, percent: 69.86 },
        { key: "team-b", label: "平台组", value: 302_000, percent: 30.14 }
      ],
      series: [],
      period: "range",
      group_by: "team"
    }
  }
);

assert.equal(report.total, "1.00M", "totalTokens should sum session total_tokens");
assert.equal(report.sessions, 3, "sessionCount should use sessions.length");
assert.equal(report.uploaders, 2, "uploaderCount should distinct by user_id");
assert.deepEqual(
  report.bars.map((bar) => [bar.label, bar.value, bar.text]),
  [
    ["06-23", 2000, "2.0K"],
    ["06-24", 0, "0"],
    ["06-25", 1_000_000, "1.00M"]
  ],
  "dailyBars should aggregate by started_at date and fill missing days"
);
assert.deepEqual(report.mine, { sessions: 1, total: "1.2K" }, "mine token should use mine scoped sessions");
assert.deepEqual(
  report.groups?.map((group) => ({ name: group.name, total: group.total, note: group.note })),
  [
    { name: "芯片组", total: "700.0K", note: "占比 69.9%" },
    { name: "平台组", total: "302.0K", note: "占比 30.1%" }
  ],
  "group tokens should map real /tokens group_by=team fields only"
);
assert.equal(
  "sessions" in (report.groups?.[0] ?? {}),
  false,
  "group session_count should not be fabricated"
);
assert.equal(
  "uploaders" in (report.groups?.[0] ?? {}),
  false,
  "group uploader_count should not be fabricated"
);

const emptyReport = aggregateDashboardTokenReport([], range, { showUploaders: true, mineSessions: [] });
assert.equal(emptyReport.total, "0", "empty sessions should return zero total");
assert.equal(emptyReport.sessions, 0, "empty sessions should return zero sessions");
assert.equal(emptyReport.uploaders, 0, "empty sessions should return zero uploaders when requested");
assert.equal(emptyReport.mine.total, "0", "empty mine sessions should return zero mine total");
assert.equal(emptyReport.status, "暂无记录", "empty sessions should be an empty state");
assert.equal(emptyReport.bars.length, 3, "empty sessions should keep stable date bars");

assert.deepEqual(
  getDashboardTokenDateRange("yesterday", new Date("2026-06-25T12:00:00Z")),
  { from: "2026-06-24", to: "2026-06-24" },
  "yesterday should map to the previous day"
);
assert.deepEqual(
  getDashboardTokenDateRange("last3days", new Date("2026-06-25T12:00:00Z")),
  { from: "2026-06-23", to: "2026-06-25" },
  "last3days should include today and previous two days"
);
assert.equal(formatDashboardTokens(999), "999", "token formatter should keep small values");

console.log("dashboard token workflow contract checks passed");
