export type TokenAnalyticsDatePreset = "today" | "last3days" | "last7days" | "custom";

export interface TokenAnalyticsDateRange {
  from: string;
  to: string;
}

export interface ModelUsageItem {
  key: string;
  label: string;
  total_tokens: string;
}

const PRESET_DAYS: Record<Exclude<TokenAnalyticsDatePreset, "custom">, number> = {
  today: 1,
  last3days: 3,
  last7days: 7
};

function snapshotErrorCode(value: unknown): string | undefined {
  if (!value || typeof value !== "object") return undefined;
  const record = value as Record<string, unknown>;
  if (typeof record.code === "string") return record.code;
  return snapshotErrorCode(record.payload) ?? snapshotErrorCode(record.data);
}

export function isQuerySnapshotExpired(error: unknown) {
  return snapshotErrorCode(error) === "QUERY_SNAPSHOT_EXPIRED";
}

export function claimExpiredSnapshotRefresh(
  error: unknown,
  snapshotToken: string | undefined,
  claimedTokens: Set<string>
) {
  if (!snapshotToken || !isQuerySnapshotExpired(error) || claimedTokens.has(snapshotToken)) {
    return false;
  }
  claimedTokens.add(snapshotToken);
  return true;
}

export function shouldRetryTokenAnalyticsSnapshotQuery(failureCount: number, error: unknown) {
  return !isQuerySnapshotExpired(error) && failureCount < 3;
}

export const tokenAnalyticsSnapshotChildQueryOptions = {
  retry: shouldRetryTokenAnalyticsSnapshotQuery,
  refetchOnMount: false,
  refetchOnReconnect: false,
  refetchOnWindowFocus: false
} as const;

function shiftDateKey(value: string, days: number) {
  const date = new Date(`${value}T00:00:00Z`);
  date.setUTCDate(date.getUTCDate() + days);
  return date.toISOString().slice(0, 10);
}

export function getTokenAnalyticsPresetRange(
  preset: Exclude<TokenAnalyticsDatePreset, "custom">,
  today: string
): TokenAnalyticsDateRange {
  return {
    from: shiftDateKey(today, -(PRESET_DAYS[preset] - 1)),
    to: today
  };
}

export function getTokenAnalyticsPreset(
  range: TokenAnalyticsDateRange,
  today: string
): TokenAnalyticsDatePreset {
  for (const preset of ["today", "last3days", "last7days"] as const) {
    const expected = getTokenAnalyticsPresetRange(preset, today);
    if (range.from === expected.from && range.to === expected.to) return preset;
  }
  return "custom";
}

export function isTokenAnalyticsDateRange(range: TokenAnalyticsDateRange) {
  const isDateKey = (value: string) => {
    if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false;
    const date = new Date(`${value}T00:00:00Z`);
    return !Number.isNaN(date.getTime()) && date.toISOString().slice(0, 10) === value;
  };
  return isDateKey(range.from) && isDateKey(range.to) && range.from <= range.to;
}

export function buildModelUsageTopN<T extends ModelUsageItem>(items: T[], limit: number) {
  const safeLimit = Math.max(1, limit);
  return items.slice(0, safeLimit).map<ModelUsageItem>((item) => ({
    key: item.key,
    label: item.label,
    total_tokens: item.total_tokens
  }));
}
