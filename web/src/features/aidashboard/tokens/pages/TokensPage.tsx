import {
  CloudOutlined,
  DatabaseOutlined,
  ImportOutlined,
  ThunderboltOutlined,
  UploadOutlined
} from "@ant-design/icons";
import { useQuery } from "@tanstack/react-query";
import { Alert, Button, DatePicker, Empty, Segmented, Space, Table } from "antd";
import { useMemo, useState } from "react";
import dayjs from "dayjs";

import { fetchSessionTokens, fetchTokens } from "../../api/client";
import type { SessionTokens } from "../../api/types";
import {
  RequirementMetricCard,
  RequirementMetricGrid
} from "../../requirements/components/RequirementMetricCard";
import { useAuth } from "@/shared/auth/authContext";
import { PagePanel } from "@/shared/components/PagePanel/PagePanel";

import "./TokensPage.css";

function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return String(n);
}

function formatDateTime(iso: string): string {
  return new Date(iso).toLocaleString("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit"
  });
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : "请稍后重试";
}

function agentTagClass(agent: string) {
  const value = (agent || "").toLowerCase();
  if (value.includes("claude")) return "tokens-agent-tag is-claude";
  if (value.includes("codex") || value.includes("gpt")) return "tokens-agent-tag is-codex";
  return "tokens-agent-tag is-other";
}

function realSessionId(session: SessionTokens) {
  return session.local_session_id || session.session_ref;
}

function formatActivityRange(session: SessionTokens) {
  const start = session.activity_start_at || session.started_at;
  const end = session.activity_end_at;
  if (!end || end === start) return formatDateTime(start);
  return `${formatDateTime(start)} ~ ${formatDateTime(end)}`;
}

export function TokensPage() {
  const { user } = useAuth();
  const today = dayjs();
  const firstOfMonth = today.startOf("month");
  const [range, setRange] = useState<[dayjs.Dayjs, dayjs.Dayjs]>([firstOfMonth, today]);
  const canViewTeam = Boolean(user && user.role !== "employee");
  const [scope, setScope] = useState<"mine" | "team">("mine");
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const effectiveScope = canViewTeam ? scope : "mine";

  const from = range[0].format("YYYY-MM-DD");
  const to = range[1].format("YYYY-MM-DD");

  const tokensQuery = useQuery({
    queryKey: ["session-tokens", from, to, effectiveScope, page, pageSize],
    queryFn: () =>
      fetchSessionTokens({
        from,
        to,
        scope: effectiveScope,
        page: String(page),
        page_size: String(pageSize)
      }),
    placeholderData: (previousData) => previousData,
    staleTime: 60_000
  });
  const aggregationQuery = useQuery({
    queryKey: ["token-aggregation", from, to, effectiveScope],
    queryFn: () =>
      fetchTokens({
        period: "range",
        from,
        to,
        group_by: "model",
        scope: effectiveScope
      }),
    staleTime: 60_000
  });
  const sessions = useMemo(() => tokensQuery.data?.items ?? [], [tokensQuery.data]);
  const total = tokensQuery.data?.total ?? 0;

  const pageTotals = useMemo(() => {
    return sessions.reduce(
      (acc, s) => {
        acc.input += s.input_tokens;
        acc.output += s.output_tokens;
        acc.cacheCreate += s.cache_creation_tokens || 0;
        acc.cacheRead += s.cache_read_tokens || 0;
        acc.total += s.total_tokens;
        return acc;
      },
      { input: 0, output: 0, cacheCreate: 0, cacheRead: 0, total: 0 }
    );
  }, [sessions]);

  const totals = {
    input: aggregationQuery.data?.input_sum ?? 0,
    output: aggregationQuery.data?.output_sum ?? 0,
    cacheCreate: aggregationQuery.data?.cache_creation_sum ?? 0,
    cacheRead: aggregationQuery.data?.cache_read_sum ?? 0,
    total: aggregationQuery.data?.total ?? 0
  };

  const cacheHitRate =
    totals.input + totals.cacheCreate + totals.cacheRead > 0
      ? (totals.cacheRead * 100) / (totals.input + totals.cacheCreate + totals.cacheRead)
      : 0;

  const teamLabel = user?.role === "director" || user?.role === "admin" ? "全团队" : "团队";
  const scopeLabel = effectiveScope === "mine" ? "我的 Token 明细" : `${teamLabel} Token 明细`;
  const dateLabel = `${from} ~ ${to}`;

  return (
    <PagePanel
      title="Token 明细"
      className="tokens-page"
      description={`${scopeLabel} · 按 AI 工作记录查看 Input / Output / Cache / Total`}
      breadcrumbs={[{ title: "Token 明细" }]}
      showNav={false}
    >
      <div className="tokens-toolbar">
        <div className="tokens-toolbar__left">
          <span>{scopeLabel}</span>
          <span>·</span>
          <span>{dateLabel}</span>
          <span>·</span>
          <span>{total} 条工作记录</span>
        </div>
        <div className="tokens-toolbar__right">
          {canViewTeam ? (
            <Segmented
              value={scope}
              onChange={(v) => {
                setScope(v as "mine" | "team");
                setPage(1);
              }}
              options={[
                { label: "我的", value: "mine" },
                { label: teamLabel, value: "team" }
              ]}
            />
          ) : null}
          <DatePicker.RangePicker
            value={range}
            onChange={(v) => {
              if (!v || !v[0] || !v[1]) return;
              setRange([v[0], v[1]]);
              setPage(1);
            }}
          />
        </div>
      </div>

      {tokensQuery.isError ? (
        <Alert
          type="error"
          showIcon
          message="Token 明细加载失败"
          description={errorMessage(tokensQuery.error)}
          action={<Button onClick={() => void tokensQuery.refetch()}>重试</Button>}
        />
      ) : null}

      {aggregationQuery.isError ? (
        <Alert
          type="error"
          showIcon
          message="Token 统计加载失败"
          description={errorMessage(aggregationQuery.error)}
          action={<Button onClick={() => void aggregationQuery.refetch()}>重试</Button>}
        />
      ) : null}

      <RequirementMetricGrid>
        <RequirementMetricCard
          tone="primary"
          icon={<ThunderboltOutlined />}
          loading={aggregationQuery.isLoading}
          metric={{
            key: "total",
            title: "总 Token",
            value: formatTokens(totals.total),
            description: `${total} 条工作记录合计`
          }}
        />
        <RequirementMetricCard
          tone="info"
          icon={<ImportOutlined />}
          loading={aggregationQuery.isLoading}
          metric={{
            key: "input",
            title: "输入 Token",
            value: formatTokens(totals.input),
            description: "Input · 未命中缓存的请求量"
          }}
        />
        <RequirementMetricCard
          tone="info"
          icon={<UploadOutlined />}
          loading={aggregationQuery.isLoading}
          metric={{
            key: "output",
            title: "输出 Token",
            value: formatTokens(totals.output),
            description: "Output · 模型回复量"
          }}
        />
        <RequirementMetricCard
          tone="success"
          icon={<DatabaseOutlined />}
          loading={aggregationQuery.isLoading}
          metric={{
            key: "cache-read",
            title: "缓存读取",
            value: formatTokens(totals.cacheRead),
            description: `命中率 ${cacheHitRate.toFixed(1)}% · 越高越省`
          }}
        />
        <RequirementMetricCard
          tone="warning"
          icon={<CloudOutlined />}
          loading={aggregationQuery.isLoading}
          metric={{
            key: "cache-create",
            title: "缓存创建",
            value: formatTokens(totals.cacheCreate),
            description: "Cache Create · 一次性写入成本"
          }}
        />
      </RequirementMetricGrid>

      <div className="tokens-table-card">
        <Table<SessionTokens>
          rowKey={(record) =>
            record.slice_key ||
            `${record.session_id}:${record.activity_date || record.activity_start_at || record.started_at}`
          }
          dataSource={sessions}
          loading={tokensQuery.isLoading}
          pagination={{
            current: tokensQuery.data?.page ?? page,
            pageSize: tokensQuery.data?.page_size ?? pageSize,
            total,
            showSizeChanger: true,
            showTotal: (value) => `共 ${value} 条工作记录`,
            onChange: (nextPage, nextPageSize) => {
              setPage(nextPage);
              setPageSize(nextPageSize);
            }
          }}
          scroll={{ x: "max-content" }}
          columns={[
            {
              title: "真实 Session ID",
              key: "record",
              width: 380,
              render: (_: unknown, s) => (
                <Space className="tokens-session-cell" orientation="vertical" size={2}>
                  <span className="tokens-session-id">{realSessionId(s)}</span>
                  {s.summary ? (
                    <span
                      className="tokens-session-summary"
                      title={s.summary}
                    >
                      {s.summary}
                    </span>
                  ) : null}
                </Space>
              )
            },
            ...(effectiveScope === "team"
              ? [
                  {
                    title: "成员",
                    dataIndex: "user_name",
                    width: 120,
                    render: (v: string) => v || "-"
                  }
                ]
              : []),
            {
              title: "Agent",
              dataIndex: "agent_type",
              width: 110,
              render: (v: string) => <span className={agentTagClass(v)}>{v}</span>
            },
            {
              title: "Models",
              dataIndex: "models",
              render: (v: string[]) => (v?.length ? v.join(", ") : "-"),
              width: 180
            },
            {
              title: "Total",
              dataIndex: "total_tokens",
              align: "right" as const,
              width: 110,
              render: (v: number) => <span className="tokens-total-cell">{formatTokens(v)}</span>
            },
            {
              title: "活动时间",
              dataIndex: "activity_start_at",
              render: (_: string, s) => <span className="tokens-activity-range">{formatActivityRange(s)}</span>,
              width: 230
            }
          ]}
          locale={{
            emptyText: (
              <Empty
                description={
                  effectiveScope === "mine"
                    ? "所选范围暂无我的 Token 记录"
                    : "所选范围暂无团队 Token 记录"
                }
              />
            )
          }}
          summary={() => (
            <Table.Summary fixed>
              <Table.Summary.Row>
                <Table.Summary.Cell index={0} colSpan={effectiveScope === "team" ? 4 : 3}>
                  本页小计（{sessions.length}）
                </Table.Summary.Cell>
                <Table.Summary.Cell index={effectiveScope === "team" ? 4 : 3} align="right">
                  <span className="tokens-total-cell">{formatTokens(pageTotals.total)}</span>
                </Table.Summary.Cell>
                <Table.Summary.Cell index={effectiveScope === "team" ? 5 : 4} />
              </Table.Summary.Row>
            </Table.Summary>
          )}
        />
      </div>
      <Space />
    </PagePanel>
  );
}
