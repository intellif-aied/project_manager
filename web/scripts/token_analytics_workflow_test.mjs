/* global console, process */

import assert from "node:assert/strict";
import { Buffer } from "node:buffer";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import ts from "typescript";

const root = process.cwd();
const viewPath = resolve(root, "src/features/aidashboard/tokens/tokenAnalyticsView.ts");
const pagePath = resolve(root, "src/features/aidashboard/tokens/pages/TokenAnalyticsPage.tsx");
const apiTypesPath = resolve(root, "src/features/aidashboard/api/types.ts");
const memberPagePath = resolve(
  root,
  "src/features/aidashboard/tokens/pages/TokenMemberAnalyticsPage.tsx"
);
const pageCssPath = resolve(
  root,
  "src/features/aidashboard/tokens/pages/TokenAnalyticsPage.css"
);

const viewSource = readFileSync(viewPath, "utf8");
const pageSource = readFileSync(pagePath, "utf8");
const apiTypesSource = readFileSync(apiTypesPath, "utf8");
const memberPageSource = readFileSync(memberPagePath, "utf8");
const pageCssSource = readFileSync(pageCssPath, "utf8");

const transpiled = ts.transpileModule(viewSource, {
  compilerOptions: {
    module: ts.ModuleKind.ES2022,
    target: ts.ScriptTarget.ES2022,
    importsNotUsedAsValues: ts.ImportsNotUsedAsValues.Remove
  }
}).outputText;
const tempModuleUrl = `data:text/javascript;base64,${Buffer.from(transpiled).toString("base64")}`;
const {
  buildModelUsageTopN,
  claimExpiredSnapshotRefresh,
  getTokenAnalyticsPreset,
  getTokenAnalyticsPresetRange,
  isTokenAnalyticsDateRange,
  shouldRetryTokenAnalyticsSnapshotQuery
} = await import(tempModuleUrl);

const claimedSnapshotRefreshes = new Set();
const expiredSnapshotError = {
  payload: {
    code: "QUERY_SNAPSHOT_EXPIRED",
    error: "query snapshot expired; restart from summary"
  }
};
assert.equal(
  claimExpiredSnapshotRefresh(expiredSnapshotError, "snapshot-old", claimedSnapshotRefreshes),
  true,
  "the first expired child request should claim one Summary refresh"
);
assert.equal(
  claimExpiredSnapshotRefresh(expiredSnapshotError, "snapshot-old", claimedSnapshotRefreshes),
  false,
  "concurrent failures for the same snapshot must not trigger duplicate Summary refreshes"
);
assert.equal(
  claimExpiredSnapshotRefresh(new Error("network error"), "snapshot-network", claimedSnapshotRefreshes),
  false,
  "unrelated request failures must not rebuild the query snapshot"
);
assert.equal(
  shouldRetryTokenAnalyticsSnapshotQuery(0, expiredSnapshotError),
  false,
  "an expired token must not be retried against the same snapshot"
);
assert.equal(
  shouldRetryTokenAnalyticsSnapshotQuery(0, new Error("network error")),
  true,
  "ordinary transient failures should keep the existing retry policy"
);

assert.deepEqual(
  getTokenAnalyticsPresetRange("today", "2026-07-20"),
  { from: "2026-07-20", to: "2026-07-20" },
  "today should select only the current business date"
);
assert.deepEqual(
  getTokenAnalyticsPresetRange("last3days", "2026-07-20"),
  { from: "2026-07-18", to: "2026-07-20" },
  "last3days should include today and the previous two business dates"
);
assert.deepEqual(
  getTokenAnalyticsPresetRange("last7days", "2026-07-20"),
  { from: "2026-07-14", to: "2026-07-20" },
  "last7days should include today and the previous six business dates"
);
assert.equal(
  getTokenAnalyticsPreset({ from: "2026-07-18", to: "2026-07-20" }, "2026-07-20"),
  "last3days",
  "an exact preset range should keep its shortcut selected"
);
assert.equal(
  getTokenAnalyticsPreset({ from: "2026-07-02", to: "2026-07-09" }, "2026-07-20"),
  "custom",
  "a manually selected range should be treated as custom"
);
assert.equal(
  isTokenAnalyticsDateRange({ from: "2026-02-30", to: "2026-03-01" }),
  false,
  "impossible direct-link dates should be rejected"
);

assert.deepEqual(
  buildModelUsageTopN(
    [
      { key: "m1", label: "M1", total_tokens: "50" },
      { key: "m2", label: "M2", total_tokens: "40" },
      { key: "m3", label: "M3", total_tokens: "30" },
      { key: "m4", label: "M4", total_tokens: "20" }
    ],
    2
  ).map((item) => [item.label, item.total_tokens]),
  [
    ["M1", "50"],
    ["M2", "40"]
  ],
  "model usage should contain only the real Top N models"
);

assert.match(
  pageSource,
  /const \[dateRange, setDateRange\] = useState<TokenAnalyticsDateRange>/,
  "management drilldown should own the shared date range"
);
assert.match(
  pageSource,
  /refreshExpiredSnapshot\(\s*snapshotToken,[\s\S]*summaryQuery\.refetch/,
  "overview child failures should rebuild the overview Summary snapshot"
);
assert.match(
  pageSource,
  /refreshExpiredSnapshot\(\s*scopeSnapshotToken,[\s\S]*scopeSummaryQuery\.refetch/,
  "team option failures should rebuild the scope Summary snapshot"
);
assert.match(
  pageSource,
  /refreshExpiredSnapshot\(\s*sessionSnapshotToken,[\s\S]*sessionSummaryQuery\.refetch/,
  "filtered Session failures should rebuild the filtered Summary snapshot"
);
assert.ok(
  (pageSource.match(/onDateRangeChange=\{setDateRange\}/g) ?? []).length === 1,
  "only the overview should update the overview date range"
);
assert.match(
  pageSource,
  /const \[memberDateRange, setMemberDateRange\] = useState<TokenAnalyticsDateRange>/,
  "member detail should own an isolated date range"
);
assert.match(
  pageSource,
  /setMemberDateRange\(dateRange\)/,
  "opening a member should copy the current overview date range"
);
assert.match(
  pageSource,
  /dateRange=\{memberDateRange \?\? dateRange\}[\s\S]*onDateRangeChange=\{setMemberDateRange\}/,
  "member detail changes should update only the isolated member range"
);
assert.match(pageSource, /label: "当天"/, "Token Analytics should expose a today shortcut");
assert.match(pageSource, /label: "近 3 天"/, "Token Analytics should expose a three-day shortcut");
assert.match(pageSource, /label: "近 7 天"/, "Token Analytics should expose a seven-day shortcut");
assert.match(
  pageSource,
  /group_by: "model"/,
  "management analytics should request model rankings independently"
);
assert.match(pageSource, /模型使用 TOP 8/, "the model Top 8 should be visible as its own module");
assert.doesNotMatch(pageSource, /其余模型合并|__other__/, "the model ranking must not synthesize an other item");
assert.match(memberPageSource, /searchParams\.get\("from"\)/, "direct member links should accept from");
assert.match(memberPageSource, /searchParams\.get\("to"\)/, "direct member links should accept to");
assert.match(pageSource, /const \[isReturning, setIsReturning\] = useState\(false\)/, "member back navigation should have a reverse transition state");
assert.doesNotMatch(
  pageSource,
  /navigate\(-1\);\s*setIsReturning\(false\)/,
  "route navigation must not restart the detail transition before location state catches up"
);
assert.match(pageSource, /data-view=\{drilldownView\}/, "drilldown direction should be exposed to CSS");
assert.match(pageCssSource, /translateX\(/, "drilldown should use horizontal shared-axis motion");
assert.doesNotMatch(pageCssSource, /translateY\(|scale\(/, "drilldown must not jump vertically or scale");
assert.doesNotMatch(pageCssSource, /cubic-bezier\(0\.2, 0\.82, 0\.24, 1\.08\)/, "drilldown must not use an overshooting easing curve");
assert.match(pageCssSource, /prefers-reduced-motion:[\s\S]*transition: none/, "drilldown should honor reduced-motion preferences");
assert.match(
  apiTypesSource,
  /usage_status: "available" \| "unavailable"/,
  "session rows should expose whether Token usage is available"
);
assert.match(
  apiTypesSource,
  /total_tokens: string \| null/,
  "unavailable session Token values should remain null"
);
assert.match(
  pageSource,
  /record\.usage_status === "unavailable"[\s\S]*Token \u6682\u4e0d\u652f\u6301/,
  "unavailable session rows should explain why Token values are absent"
);

console.log("token analytics workflow contract checks passed");
