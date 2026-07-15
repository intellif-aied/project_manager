import type { SessionTokens, TokenAggregation } from "../api/types";

export type DashboardTokenRange = "yesterday" | "last3days" | "last7days";

export interface DashboardTokenBar {
  label: string;
  value: number;
  text: string;
}

export interface DashboardTokenGroup {
  name: string;
  total: string;
  value: number;
  note?: string;
}

export interface DashboardTokenReport {
  total: string;
  sessions: number;
  uploaders?: number;
  bars: DashboardTokenBar[];
  groups?: DashboardTokenGroup[];
  memberGroups?: DashboardTokenGroup[];
  mine?: { sessions: number; total: string };
  status: "上报完整" | "有上报记录" | "暂无记录" | "解析异常";
}

export interface TokenDateRange {
  from: string;
  to: string;
}

const DAY_MS = 24 * 60 * 60 * 1000;

function toDateKey(date: Date) {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

function parseDateKey(value: string) {
  const [year, month, day] = value.split("-").map(Number);
  return new Date(year, month - 1, day);
}

function addDays(date: Date, days: number) {
  return new Date(date.getTime() + days * DAY_MS);
}

function labelDate(value: string) {
  return value.slice(5);
}

export function getDashboardTokenDateRange(range: DashboardTokenRange, now = new Date()): TokenDateRange {
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  if (range === "yesterday") {
    const yesterday = addDays(today, -1);
    const value = toDateKey(yesterday);
    return { from: value, to: value };
  }

  const days = range === "last3days" ? 3 : 7;
  return {
    from: toDateKey(addDays(today, -(days - 1))),
    to: toDateKey(today)
  };
}

export function formatDashboardTokens(value: number): string {
  if (value >= 1_000_000_000_000) return `${(value / 1_000_000_000_000).toFixed(2)}T`;
  if (value >= 1_000_000_000) return `${(value / 1_000_000_000).toFixed(2)}B`;
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(2)}M`;
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}K`;
  return String(value);
}

export function aggregateDashboardTokenReport(
  sessionsInput: Array<Partial<SessionTokens> | null | undefined>,
  range: TokenDateRange,
  options?: {
    mineSessions?: Array<Partial<SessionTokens> | null | undefined>;
    teamAggregation?: TokenAggregation | null;
    showUploaders?: boolean;
  }
): DashboardTokenReport {
  const sessions = sessionsInput.filter((item): item is Partial<SessionTokens> => Boolean(item));
  const totalTokens = sessions.reduce((sum, session) => sum + Math.max(0, Number(session.total_tokens ?? 0)), 0);
  const userIds = new Set<string>();
  sessions.forEach((session) => {
    const key = session.user_id || session.user_name;
    if (key) userIds.add(key);
  });

  const tokenByMember = new Map<string, { name: string; total: number; sessions: number }>();
  sessions.forEach((session) => {
    const key = session.user_id || session.user_name || "unknown";
    const name = session.user_name || session.user_id || "未命名成员";
    const current = tokenByMember.get(key) ?? { name, total: 0, sessions: 0 };
    current.total += Math.max(0, Number(session.total_tokens ?? 0));
    current.sessions += 1;
    tokenByMember.set(key, current);
  });

  const tokenByDate = new Map<string, number>();
  sessions.forEach((session) => {
    const key =
      session.activity_date ||
      (session.activity_start_at ? toDateKey(new Date(session.activity_start_at)) : undefined) ||
      (session.started_at ? toDateKey(new Date(session.started_at)) : undefined);
    if (!key) return;
    tokenByDate.set(key, (tokenByDate.get(key) ?? 0) + Math.max(0, Number(session.total_tokens ?? 0)));
  });

  const bars: DashboardTokenBar[] = [];
  for (let day = parseDateKey(range.from); day <= parseDateKey(range.to); day = addDays(day, 1)) {
    const key = toDateKey(day);
    const value = tokenByDate.get(key) ?? 0;
    bars.push({
      label: labelDate(key),
      value,
      text: formatDashboardTokens(value)
    });
  }

  const hasMineSessions = Boolean(options && "mineSessions" in options);
  const mineSessions = options?.mineSessions?.filter((item): item is Partial<SessionTokens> => Boolean(item)) ?? [];
  const mineTotal = mineSessions.reduce(
    (sum, session) => sum + Math.max(0, Number(session.total_tokens ?? 0)),
    0
  );
  const mine = hasMineSessions ? { sessions: mineSessions.length, total: formatDashboardTokens(mineTotal) } : undefined;

  const groups = options?.teamAggregation?.groups?.map((group) => ({
    name: group.label,
    total: formatDashboardTokens(group.value),
    value: group.value,
    note: typeof group.percent === "number" ? `占比 ${group.percent.toFixed(1)}%` : undefined
  }));
  const memberGroups = Array.from(tokenByMember.values())
    .sort((a, b) => b.total - a.total)
    .map((member) => ({
      name: member.name,
      total: formatDashboardTokens(member.total),
      value: member.total,
      note: `${member.sessions} 个 session`
    }));

  return {
    total: formatDashboardTokens(totalTokens),
    sessions: sessions.length,
    uploaders: options?.showUploaders ? userIds.size : undefined,
    bars,
    groups: groups && groups.length > 0 ? groups : undefined,
    memberGroups: memberGroups.length > 0 ? memberGroups : undefined,
    mine,
    status: sessions.length > 0 ? "有上报记录" : "暂无记录"
  };
}
