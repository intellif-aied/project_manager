import { create } from "zustand";

import type { ManagedReportAgentRunPayload, ReportType } from "../api/types";

export type ReportPeriodPayload = ManagedReportAgentRunPayload["period"];
export type ReportTargetPayload = NonNullable<ManagedReportAgentRunPayload["target"]>;

export interface ReportAIRunEntry {
  storageKey: string;
  runId: string;
  userId: string;
  reportType: ReportType;
  period: ReportPeriodPayload;
  target: ReportTargetPayload;
  startedAt: number;
  largeContextWarningShown?: boolean;
}

interface ReportAIRunStore {
  runs: Record<string, ReportAIRunEntry>;
  register: (entry: ReportAIRunEntry) => void;
  clear: (storageKey: string, runId?: string) => void;
  syncFromStorage: () => void;
}

interface StoredReportAIRunValue {
  runId: string;
  startedAt: number;
  largeContextWarningShown?: boolean;
}

interface ReportAIRunCriteria {
  userId: string;
  reportType: ReportType;
  date?: string;
  weekStart?: string;
  weekEnd?: string;
}

export const REPORT_AI_RUN_STORAGE_PREFIX = "aida:report-ai-run:";

export function reportRunStorageKey(
  userId: string,
  reportType: ReportType,
  period: ReportPeriodPayload,
  target: ReportTargetPayload
) {
  return `${REPORT_AI_RUN_STORAGE_PREFIX}${JSON.stringify({
    userId,
    reportType,
    period,
    target
  })}`;
}

function parseStorageKey(storageKey: string) {
  if (!storageKey.startsWith(REPORT_AI_RUN_STORAGE_PREFIX)) return undefined;
  try {
    const parsed = JSON.parse(storageKey.slice(REPORT_AI_RUN_STORAGE_PREFIX.length)) as Partial<
      Pick<ReportAIRunEntry, "userId" | "reportType" | "period" | "target">
    >;
    if (!parsed.userId || !parsed.reportType || !parsed.period || !parsed.target) return undefined;
    return {
      userId: parsed.userId,
      reportType: parsed.reportType,
      period: parsed.period,
      target: parsed.target
    };
  } catch {
    return undefined;
  }
}

function parseStoredValue(value: string): StoredReportAIRunValue | undefined {
  const trimmed = value.trim();
  if (!trimmed) return undefined;
  try {
    const parsed = JSON.parse(trimmed) as Partial<StoredReportAIRunValue>;
    if (typeof parsed.runId === "string" && parsed.runId) {
      return {
        runId: parsed.runId,
        startedAt:
          typeof parsed.startedAt === "number" && Number.isFinite(parsed.startedAt)
            ? parsed.startedAt
            : Date.now(),
        largeContextWarningShown: parsed.largeContextWarningShown === true
      };
    }
  } catch {
    // Older versions stored only the run id. Keep accepting that value during the transition.
  }
  return { runId: trimmed, startedAt: Date.now() };
}

export function readStoredReportAIRun(storageKey: string): ReportAIRunEntry | undefined {
  if (typeof window === "undefined") return undefined;
  const metadata = parseStorageKey(storageKey);
  if (!metadata) return undefined;
  try {
    const value = window.localStorage.getItem(storageKey);
    if (!value) return undefined;
    const stored = parseStoredValue(value);
    if (!stored) return undefined;
    return { storageKey, ...metadata, ...stored };
  } catch {
    return undefined;
  }
}

function readStoredReportAIRuns() {
  const runs: Record<string, ReportAIRunEntry> = {};
  if (typeof window === "undefined") return runs;
  try {
    for (let index = 0; index < window.localStorage.length; index += 1) {
      const storageKey = window.localStorage.key(index);
      if (!storageKey?.startsWith(REPORT_AI_RUN_STORAGE_PREFIX)) continue;
      const entry = readStoredReportAIRun(storageKey);
      if (entry) runs[storageKey] = entry;
    }
  } catch {
    // Storage can be unavailable in hardened browsers; report generation still works in memory.
  }
  return runs;
}

function persistReportAIRun(entry: ReportAIRunEntry) {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(
      entry.storageKey,
      JSON.stringify({
        runId: entry.runId,
        startedAt: entry.startedAt,
        largeContextWarningShown: entry.largeContextWarningShown === true
      })
    );
  } catch {
    // Storage failures must not block report generation.
  }
}

function removePersistedReportAIRun(storageKey: string, runId?: string) {
  if (typeof window === "undefined") return;
  try {
    const stored = readStoredReportAIRun(storageKey);
    if (runId && stored && stored.runId !== runId) return;
    window.localStorage.removeItem(storageKey);
  } catch {
    // Storage failures must not block report completion handling.
  }
}

export const useReportAIRunStore = create<ReportAIRunStore>((set, get) => ({
  runs: readStoredReportAIRuns(),
  register: (entry) => {
    persistReportAIRun(entry);
    set((state) => ({ runs: { ...state.runs, [entry.storageKey]: entry } }));
  },
  clear: (storageKey, runId) => {
    const current = get().runs[storageKey] ?? readStoredReportAIRun(storageKey);
    if (runId && current && current.runId !== runId) return;
    removePersistedReportAIRun(storageKey, runId);
    set((state) => {
      if (!state.runs[storageKey]) return state;
      const runs = { ...state.runs };
      delete runs[storageKey];
      return { runs };
    });
  },
  syncFromStorage: () => set({ runs: readStoredReportAIRuns() })
}));

export function registerReportAIRun(entry: ReportAIRunEntry) {
  useReportAIRunStore.getState().register(entry);
}

export function clearReportAIRun(storageKey: string, runId?: string) {
  useReportAIRunStore.getState().clear(storageKey, runId);
}

export function markReportAIRunLargeContextWarningShown(storageKey: string, runId: string) {
  const current =
    useReportAIRunStore.getState().runs[storageKey] ?? readStoredReportAIRun(storageKey);
  if (!current || current.runId !== runId || current.largeContextWarningShown) return false;
  const next = { ...current, largeContextWarningShown: true };
  persistReportAIRun(next);
  useReportAIRunStore.setState((state) => ({
    runs: { ...state.runs, [storageKey]: next }
  }));
  return true;
}

export function hasActiveReportAIRun(
  runs: Record<string, ReportAIRunEntry>,
  criteria: ReportAIRunCriteria
) {
  return Object.values(runs).some((entry) => {
    if (entry.userId !== criteria.userId || entry.reportType !== criteria.reportType) return false;
    if (criteria.date && entry.period.date !== criteria.date) return false;
    if (criteria.weekStart && entry.period.week_start !== criteria.weekStart) return false;
    if (criteria.weekEnd && entry.period.week_end !== criteria.weekEnd) return false;
    return true;
  });
}
