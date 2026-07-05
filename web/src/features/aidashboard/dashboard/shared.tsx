import { Progress, Segmented, Tag, Tooltip } from "antd";
import type { ReactNode } from "react";
import type { SegmentedOptions } from "antd/es/segmented";
import type {
  RequirementPriority,
  RequirementStatus,
  TaskDisplayStatus,
  TaskPriority,
  TaskStatus
} from "../api/types";

import "./metric-card.css";

export type StatCardTone = "primary" | "success" | "warning" | "danger" | "info";

interface StatCardProps {
  label: string;
  value: string | number;
  sub?: string;
  icon?: ReactNode;
  tone?: StatCardTone;
  loading?: boolean;
}

/**
 * Top-line metric card for dashboards. Mirrors the structure of the locked
 * `MetricCard` template (title-with-top-right-icon, value, meta, tone variants,
 * loading skeleton). Values are rendered as pre-formatted strings/numbers from
 * the calling dashboard rather than animated, because business cards combine
 * counts, ratios (`"3/5"`), percentages (`"70%"`), and formatted token sizes.
 */
export function StatCard({
  label,
  value,
  sub,
  icon,
  tone = "primary",
  loading = false
}: StatCardProps) {
  if (loading) {
    return (
      <div className={`aidashboard-metric-card aidashboard-metric-card--tone-${tone}`}>
        <div className="aidashboard-metric-card__title">
          <span className="aidashboard-metric-card-skeleton aidashboard-metric-card-skeleton--title" />
          <span className="aidashboard-metric-card-skeleton aidashboard-metric-card-skeleton--icon" />
        </div>
        <span className="aidashboard-metric-card-skeleton aidashboard-metric-card-skeleton--value" />
        <span className="aidashboard-metric-card-skeleton aidashboard-metric-card-skeleton--meta" />
      </div>
    );
  }

  return (
    <div className={`aidashboard-metric-card aidashboard-metric-card--tone-${tone}`}>
      <div className="aidashboard-metric-card__title">
        <span>{label}</span>
        {icon && (
          <span className="aidashboard-metric-card__icon" aria-hidden="true">
            {icon}
          </span>
        )}
      </div>
      <div className="aidashboard-metric-card__value">
        <span className="aidashboard-metric-card__value-text">{value}</span>
      </div>
      <div className="aidashboard-metric-card__meta">
        <span>{sub ?? ""}</span>
      </div>
    </div>
  );
}

export function ProgressBar({ value, showLabel = true }: { value: number; showLabel?: boolean }) {
  const color: "success" | "normal" | "exception" | "active" =
    value >= 80 ? "success" : value >= 40 ? "active" : "exception";
  return (
    <div style={{ display: "flex", alignItems: "center", gap: 8, minWidth: 120 }}>
      <Progress
        percent={value}
        size="small"
        strokeColor={undefined}
        status={color}
        style={{ flex: 1, minWidth: 80 }}
      />
      {showLabel ? <span style={{ fontSize: 12, color: "#6b7280" }}>{value}%</span> : null}
    </div>
  );
}

export function DeadlineCell({ deadline }: { deadline?: string }) {
  if (!deadline) return <span style={{ color: "#9ca3af", fontSize: 12 }}>-</span>;
  const today = new Date();
  today.setHours(0, 0, 0, 0);
  const d = new Date(deadline);
  d.setHours(0, 0, 0, 0);
  const days = Math.round((d.getTime() - today.getTime()) / (24 * 60 * 60 * 1000));
  const urgent = days <= 3;
  return (
    <Tooltip title={urgent ? `剩 ${days} 天` : undefined}>
      <span
        style={{
          fontSize: 12,
          color: urgent ? "#ff4d4f" : "#6b7280",
          fontWeight: urgent ? 600 : 400
        }}
      >
        {deadline}
        {urgent && days >= 0 ? ` (${days}天)` : ""}
      </span>
    </Tooltip>
  );
}

const TASK_STATUS_META: Record<TaskDisplayStatus, { color: string; label: string }> = {
  todo: { color: "default", label: "未开始" },
  in_progress: { color: "processing", label: "进行中" },
  done: { color: "success", label: "已完成" },
  blocked: { color: "error", label: "阻塞" }
};

const REQ_STATUS_META: Record<RequirementStatus, { color: string; label: string }> = {
	todo: { color: "default", label: "待开始" },
	review: { color: "purple", label: "评审" },
	active: { color: "processing", label: "进行中" },
  completed: { color: "success", label: "已完成" },
  cancelled: { color: "default", label: "已取消" }
};

export function TaskStatusTag({ status }: { status: TaskDisplayStatus | TaskStatus }) {
  const meta = TASK_STATUS_META[status] ?? { color: "default", label: status || "未知" };
  return <Tag color={meta.color}>{meta.label}</Tag>;
}

export function RequirementStatusTag({ status }: { status: RequirementStatus }) {
  const meta = REQ_STATUS_META[status] ?? { color: "default", label: status || "未知" };
  return <Tag color={meta.color}>{meta.label}</Tag>;
}

const REQ_PRIORITY_META: Record<RequirementPriority, { color: string; label: string }> = {
  low: { color: "default", label: "低" },
  medium: { color: "gold", label: "中" },
  high: { color: "orange", label: "高" },
  urgent: { color: "red", label: "紧急" }
};

const TASK_PRIORITY_META: Record<TaskPriority, { color: string; label: string }> = {
  low: { color: "default", label: "低" },
  medium: { color: "gold", label: "中" },
  high: { color: "orange", label: "高" }
};

export function RequirementPriorityTag({ priority }: { priority: RequirementPriority }) {
  const meta = REQ_PRIORITY_META[priority] ?? { color: "default", label: priority || "未知" };
  return <Tag color={meta.color}>{meta.label}</Tag>;
}

export function TaskPriorityTag({ priority }: { priority: TaskPriority }) {
  const meta = TASK_PRIORITY_META[priority] ?? { color: "default", label: priority || "未知" };
  return <Tag color={meta.color}>{meta.label}</Tag>;
}

const PERIOD_OPTIONS: SegmentedOptions = [
  { label: "日", value: "today" },
  { label: "周", value: "week" },
  { label: "月", value: "month" }
];

export function PeriodTabs({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <Segmented
      size="small"
      options={PERIOD_OPTIONS}
      value={value}
      onChange={(v) => onChange(String(v))}
    />
  );
}
