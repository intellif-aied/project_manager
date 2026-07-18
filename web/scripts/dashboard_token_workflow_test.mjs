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
  client,
  /firstPage\.query_snapshot_token[\s\S]*query_snapshot_token: firstPage\.query_snapshot_token/,
  "Full Session pagination should reuse the first page Rollup snapshot"
);
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
assert.doesNotMatch(
  dashboard,
  /fetchAllSessionTokens/,
  "Dashboard must not full-page /tokens/sessions in the browser"
);
assert.match(dashboard, /fetchTokenAnalyticsSummary/, "Dashboard should load server-side Token summary");
assert.match(dashboard, /fetchTokenAnalyticsTrends/, "Dashboard should load server-side Token trends");
assert.match(
  dashboard,
  /fetchTokenAnalyticsRankings\(\{ \.\.\.snapshotParams, group_by: "user" \}\)/,
  "Dashboard member statistics should use server-side rankings"
);
assert.match(
  dashboard,
  /fetchTokenAnalyticsRankings\(\{ \.\.\.snapshotParams, group_by: "team" \}\)/,
  "Director group tokens should use server-side team rankings"
);
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
  aggregateDashboardTokenAnalyticsReport,
  getDashboardTokenDateRange,
  formatDashboardTokens
} = await import(tempModuleUrl);

const range = { from: "2026-06-23", to: "2026-06-25" };
const report = aggregateDashboardTokenAnalyticsReport(
  {
    summary: { total_tokens: "1002000", session_count: "3", quality_status: "exact" },
    trends: [
      { date: "2026-06-23", total_tokens: "2000" },
      { date: "2026-06-25", total_tokens: "1000000" }
    ],
    members: [
      { key: "u1", label: "张三", total_tokens: "1001200", session_count: "2" },
      { key: "u2", label: "李四", total_tokens: "800", session_count: "1" },
      { key: "u3", label: "王五", total_tokens: "0", session_count: "0" }
    ],
    teams: [
      { key: "team-a", label: "芯片组", total_tokens: "700000", session_count: "2" },
      { key: "team-b", label: "平台组", total_tokens: "302000", session_count: "1" }
    ],
    mineSummary: { total_tokens: "1200", session_count: "1", quality_status: "exact" }
  },
  range,
  { showUploaders: true }
);

assert.equal(report.total, "1.00M", "totalTokens should use server-side summary");
assert.equal(report.sessions, 3, "sessionCount should use distinct root Session count");
assert.equal(report.uploaders, 2, "uploaderCount should use non-zero member rankings");
assert.deepEqual(
  report.bars.map((bar) => [bar.label, bar.value, bar.text]),
  [
    ["06-23", 2000, "2.0K"],
    ["06-24", 0, "0"],
    ["06-25", 1_000_000, "1.00M"]
  ],
  "dailyBars should use server-side trends and fill missing days"
);
assert.deepEqual(report.mine, { sessions: 1, total: "1.2K" }, "mine token should use mine scoped sessions");
assert.deepEqual(
  report.groups?.map((group) => ({ name: group.name, total: group.total, note: group.note })),
  [
    { name: "芯片组", total: "700.0K", note: "占比 69.9%" },
    { name: "平台组", total: "302.0K", note: "占比 30.1%" }
  ],
  "group tokens should map server-side team rankings"
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

const emptyReport = aggregateDashboardTokenAnalyticsReport(undefined, range, { showUploaders: true });
assert.equal(emptyReport.total, "0", "missing analytics should return zero total");
assert.equal(emptyReport.sessions, 0, "missing analytics should return zero sessions");
assert.equal(emptyReport.uploaders, 0, "missing analytics should return zero uploaders when requested");
assert.equal(emptyReport.status, "暂无记录", "missing analytics should be an empty state");
assert.equal(emptyReport.bars.length, 3, "missing analytics should keep stable date bars");

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
