import type { ReactNode } from "react";

type ReportWorkspaceHeaderProps = {
  periodLabel: "日报" | "周报";
  scopeLabel: string;
  description: string;
  controls: ReactNode;
  action?: ReactNode;
};

/**
 * Shared report-workspace entry point. It deliberately owns only page
 * composition; report queries, permissions and mutations stay with daily and
 * weekly feature adapters.
 */
export function ReportWorkspaceHeader({
  periodLabel,
  scopeLabel,
  description,
  controls,
  action
}: ReportWorkspaceHeaderProps) {
  return (
    <section className="report-workspace-header">
      <div className="report-workspace-header__intro">
        <span className="report-workspace-header__eyebrow">报告工作台 · {periodLabel}</span>
        <div>
          <h2>{scopeLabel}</h2>
          <p>{description}</p>
        </div>
      </div>
      <div className="report-workspace-header__actions">{action}</div>
      <div className="report-workspace-header__controls">{controls}</div>
    </section>
  );
}
