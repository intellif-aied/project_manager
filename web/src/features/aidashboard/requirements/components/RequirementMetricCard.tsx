import type { ReactNode } from "react";

import "./RequirementMetricCard.css";

export type RequirementMetricTone = "primary" | "success" | "warning" | "danger" | "info";

export interface RequirementMetric {
  key: string;
  title: string;
  value: ReactNode;
  description: string;
}

export function RequirementMetricGrid({ children }: { children: ReactNode }) {
  return <section className="requirements-metric-grid">{children}</section>;
}

export function RequirementMetricCard({
  metric,
  icon,
  tone = "primary",
  loading = false
}: {
  metric: RequirementMetric;
  icon: ReactNode;
  tone?: RequirementMetricTone;
  loading?: boolean;
}) {
  return (
    <article className={`requirements-metric-card requirements-metric-card--tone-${tone}`}>
      <div className="requirements-metric-card__header">
        {loading ? (
          <>
            <span className="requirements-metric-card__skeleton requirements-metric-card__skeleton--title" />
            <span className="requirements-metric-card__skeleton requirements-metric-card__skeleton--icon" />
          </>
        ) : (
          <>
            <span className="requirements-metric-card__title">{metric.title}</span>
            <span className="requirements-metric-card__icon" aria-hidden="true">
              {icon}
            </span>
          </>
        )}
      </div>
      <strong className="requirements-metric-card__value">
        {loading ? (
          <span className="requirements-metric-card__skeleton requirements-metric-card__skeleton--value" />
        ) : (
          metric.value
        )}
      </strong>
      <span className="requirements-metric-card__description">
        {loading ? (
          <span className="requirements-metric-card__skeleton requirements-metric-card__skeleton--line" />
        ) : (
          metric.description
        )}
      </span>
    </article>
  );
}
