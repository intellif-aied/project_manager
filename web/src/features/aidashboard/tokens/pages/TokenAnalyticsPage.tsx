import {
  ApiOutlined,
  ArrowRightOutlined,
  BarChartOutlined,
  CalendarOutlined,
  CheckCircleOutlined,
  ClockCircleOutlined,
  DollarOutlined,
  SearchOutlined,
  TeamOutlined,
  ThunderboltOutlined,
  UserOutlined,
  WarningOutlined
} from "@ant-design/icons";
import { useQuery } from "@tanstack/react-query";
import {
  Alert,
  Button,
  DatePicker,
  Empty,
  Input,
  Segmented,
  Select,
  Space,
  Spin,
  Table,
  Tag
} from "antd";
import type { EChartsOption } from "echarts";
import dayjs from "dayjs";
import { useMemo, useState } from "react";

import {
  fetchDepartments,
  fetchTeams,
  fetchTokenAnalyticsCapability,
  fetchTokenAnalyticsRankings,
  fetchTokenAnalyticsSessions,
  fetchTokenAnalyticsSummary,
  fetchTokenAnalyticsTrends
} from "../../api/client";
import type {
  TokenAnalyticsFilters,
  TokenAnalyticsRankingItem,
  TokenAnalyticsSessionItem
} from "../../api/types";
import {
  RequirementMetricCard,
  RequirementMetricGrid
} from "../../requirements/components/RequirementMetricCard";
import { useAuth } from "@/shared/auth/authContext";
import { BaseEChart } from "@/shared/charts/BaseEChart";
import { PagePanel } from "@/shared/components/PagePanel/PagePanel";

import "./TokenAnalyticsPage.css";

type DateRange = [dayjs.Dayjs, dayjs.Dayjs];

interface TokenAnalyticsPageProps {
  scope: "mine" | "management";
}

function defaultRange(): DateRange {
  const end = dayjs();
  return [end.subtract(6, "day"), end];
}

function formatTokenValue(raw: string | undefined) {
  if (!raw) return "0";
  let value: bigint;
  try {
    value = BigInt(raw);
  } catch {
    return raw;
  }
  const million = 1_000_000n;
  const thousand = 1_000n;
  if (value >= million) {
    return `${value / million}.${String((value % million) / 10_000n).padStart(2, "0")}M`;
  }
  if (value >= thousand) {
    return `${value / thousand}.${String((value % thousand) / 100n).padStart(1, "0")}K`;
  }
  return value.toString();
}

function formatCost(raw: string | undefined) {
  if (!raw) return "--";
  const [integer, decimal = ""] = raw.split(".");
  return `¥${integer}.${decimal.padEnd(2, "0").slice(0, 2)}`;
}

function formatAverage(total: string | undefined, count: number) {
  if (!total || count <= 0) return "--";
  try {
    return formatTokenValue((BigInt(total) / BigInt(count)).toString());
  } catch {
    return "--";
  }
}

function formatSnapshotTime(raw: string | undefined) {
  if (!raw) return "--";
  const value = dayjs(raw);
  return value.isValid() ? value.format("MM-DD HH:mm") : "--";
}

function statusTag(status: string) {
  const config: Record<string, { color: string; label: string }> = {
    priced: { color: "success", label: "已计价" },
    partially_priced: { color: "warning", label: "部分计价" },
    pricing_pending: { color: "processing", label: "计价中" },
    unpriced: { color: "default", label: "未计价" },
    exact: { color: "success", label: "准确" },
    estimated: { color: "warning", label: "估算" },
    incomplete: { color: "warning", label: "不完整" },
    conflict: { color: "error", label: "冲突" }
  };
  const item = config[status] ?? { color: "default", label: status || "未知" };
  return <Tag color={item.color}>{item.label}</Tag>;
}

function errorText(error: unknown) {
  return error instanceof Error ? error.message : "请稍后重试";
}

export function TokenAnalyticsPage({ scope }: TokenAnalyticsPageProps) {
  const { user } = useAuth();
  const [range, setRange] = useState<DateRange>(() => defaultRange());
  const [queryInput, setQueryInput] = useState("");
  const [query, setQuery] = useState("");
  const [teamID, setTeamID] = useState<string>();
  const [departmentID, setDepartmentID] = useState<string>();
  const [userID, setUserID] = useState<string>();
  const [rankingGroup, setRankingGroup] = useState<"department" | "team" | "user" | "model">(
    scope === "mine" ? "model" : user?.role === "team_leader" ? "user" : "team"
  );
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);

  const capabilityQuery = useQuery({
    queryKey: ["token-analytics-capability", user?.id],
    queryFn: fetchTokenAnalyticsCapability,
    staleTime: 60_000
  });
  const isAdminManagement = scope === "management" && user?.role === "admin";
  const teamsQuery = useQuery({
    queryKey: ["token-analytics-teams"],
    queryFn: fetchTeams,
    enabled: isAdminManagement,
    staleTime: 60_000
  });
  const departmentsQuery = useQuery({
    queryKey: ["token-analytics-departments"],
    queryFn: fetchDepartments,
    enabled: isAdminManagement,
    staleTime: 60_000
  });

  const overviewFilters = useMemo<TokenAnalyticsFilters>(
    () => ({
      scope,
      from: range[0].format("YYYY-MM-DD"),
      to: range[1].format("YYYY-MM-DD"),
      ...(teamID ? { team_id: teamID } : {}),
      ...(departmentID ? { department_id: departmentID } : {})
    }),
    [departmentID, range, scope, teamID]
  );
  const sessionFilters = useMemo<TokenAnalyticsFilters>(
    () => ({
      ...overviewFilters,
      ...(userID ? { user_id: userID } : {}),
      ...(query ? { q: query } : {})
    }),
    [overviewFilters, query, userID]
  );

  const canLoad = Boolean(
    capabilityQuery.data?.enabled && (scope === "mine" || capabilityQuery.data?.can_manage)
  );
  const summaryQuery = useQuery({
    queryKey: ["token-analytics-summary", overviewFilters],
    queryFn: () => fetchTokenAnalyticsSummary(overviewFilters),
    enabled: canLoad,
    staleTime: 30_000
  });
  const snapshotToken = summaryQuery.data?.query_snapshot_token;
  const hasSessionFilter = Boolean(userID || query);
  const sessionSummaryQuery = useQuery({
    queryKey: ["token-analytics-session-summary", sessionFilters],
    queryFn: () => fetchTokenAnalyticsSummary(sessionFilters),
    enabled: canLoad && hasSessionFilter,
    staleTime: 30_000
  });
  const sessionSnapshotToken = hasSessionFilter
    ? sessionSummaryQuery.data?.query_snapshot_token
    : snapshotToken;
  const trendsQuery = useQuery({
    queryKey: ["token-analytics-trends", snapshotToken],
    queryFn: () =>
      fetchTokenAnalyticsTrends({ ...overviewFilters, query_snapshot_token: snapshotToken! }),
    enabled: Boolean(snapshotToken)
  });
  const rankingsQuery = useQuery({
    queryKey: ["token-analytics-rankings", snapshotToken, rankingGroup],
    queryFn: () =>
      fetchTokenAnalyticsRankings({
        ...overviewFilters,
        query_snapshot_token: snapshotToken!,
        group_by: rankingGroup
      }),
    enabled: Boolean(snapshotToken)
  });
  const peopleRankingsQuery = useQuery({
    queryKey: ["token-analytics-people-rankings", snapshotToken],
    queryFn: () =>
      fetchTokenAnalyticsRankings({
        ...overviewFilters,
        query_snapshot_token: snapshotToken!,
        group_by: "user"
      }),
    enabled: scope === "management" && Boolean(snapshotToken)
  });
  const overviewSessionsQuery = useQuery({
    queryKey: ["token-analytics-overview-sessions", snapshotToken],
    queryFn: () =>
      fetchTokenAnalyticsSessions({
        ...overviewFilters,
        query_snapshot_token: snapshotToken!,
        page: "1",
        page_size: "1"
      }),
    enabled: scope === "mine" && Boolean(snapshotToken)
  });
  const sessionsQuery = useQuery({
    queryKey: ["token-analytics-sessions", sessionSnapshotToken, page, pageSize],
    queryFn: () =>
      fetchTokenAnalyticsSessions({
        ...sessionFilters,
        query_snapshot_token: sessionSnapshotToken!,
        page: String(page),
        page_size: String(pageSize)
      }),
    enabled: Boolean(sessionSnapshotToken),
    placeholderData: (previous) => previous
  });

  const trendOption = useMemo<EChartsOption>(() => {
    const points = trendsQuery.data?.items ?? [];
    return {
      tooltip: {
        trigger: "axis",
        formatter: (params: unknown) => {
          const item = Array.isArray(params) ? params[0] : params;
          const index =
            item && typeof item === "object" && "dataIndex" in item
              ? Number((item as { dataIndex: number }).dataIndex)
              : 0;
          const point = points[index];
          return point ? `${point.date}<br />Token：${formatTokenValue(point.total_tokens)}` : "";
        }
      },
      grid: { left: 66, right: 24, top: 24, bottom: 38 },
      xAxis: {
        type: "category",
        data: points.map((item) => item.date),
        boundaryGap: false,
        axisTick: { show: false },
        axisLine: { lineStyle: { color: "#d8e0ea" } },
        axisLabel: {
          color: "#748195",
          formatter: (value: string) => dayjs(value).format("MM-DD"),
          hideOverlap: true
        }
      },
      yAxis: {
        type: "value",
        axisLine: { show: false },
        axisTick: { show: false },
        splitLine: { lineStyle: { color: "#edf1f5" } },
        axisLabel: {
          color: "#748195",
          formatter: (value: number) => formatTokenValue(String(Math.round(value)))
        }
      },
      series: [
        {
          type: "line",
          smooth: false,
          symbolSize: 6,
          data: points.map((item) => Number(item.total_tokens)),
          lineStyle: { width: 2.5, color: "#1677ff" },
          itemStyle: { color: "#1677ff" },
          areaStyle: { color: "rgba(22, 119, 255, 0.055)" }
        }
      ]
    };
  }, [trendsQuery.data]);

  const rankingOption = useMemo<EChartsOption>(() => {
    const items = (rankingsQuery.data?.items ?? []).slice(0, 10);
    return {
      tooltip: {
        trigger: "axis",
        axisPointer: { type: "shadow" },
        formatter: (params: unknown) => {
          const item = Array.isArray(params) ? params[0] : params;
          const index =
            item && typeof item === "object" && "dataIndex" in item
              ? Number((item as { dataIndex: number }).dataIndex)
              : 0;
          const ranking = items[index];
          return ranking
            ? `${ranking.label}<br />Token：${formatTokenValue(ranking.total_tokens)}`
            : "";
        }
      },
      grid: { left: 132, right: 28, top: 22, bottom: 40 },
      xAxis: {
        type: "value",
        axisLine: { lineStyle: { color: "#d8e0ea" } },
        axisTick: { show: false },
        splitLine: { lineStyle: { color: "#edf1f5" } },
        axisLabel: {
          color: "#748195",
          formatter: (value: number) => formatTokenValue(String(Math.round(value)))
        }
      },
      yAxis: {
        type: "category",
        inverse: true,
        data: items.map((item) => item.label),
        axisTick: { show: false },
        axisLine: { show: false },
        axisLabel: { color: "#526173", margin: 12 }
      },
      series: [
        {
          type: "bar",
          data: items.map((item) => Number(item.total_tokens)),
          barMaxWidth: 18,
          itemStyle: { color: "#269a9a", borderRadius: [0, 3, 3, 0] }
        }
      ]
    };
  }, [rankingsQuery.data]);

  if (capabilityQuery.isLoading) {
    return (
      <PagePanel title={scope === "mine" ? "我的 Token" : "团队 AI 使用分析"}>
        <Spin />
      </PagePanel>
    );
  }
  if (
    !capabilityQuery.data?.enabled ||
    (scope === "management" && !capabilityQuery.data.can_manage)
  ) {
    return (
      <PagePanel title={scope === "mine" ? "我的 Token" : "团队 AI 使用分析"}>
        <Alert type="info" showIcon message="该功能尚未对当前账号开放" />
      </PagePanel>
    );
  }

  const summary = summaryQuery.data;
  const title = scope === "mine" ? "我的 Token" : "团队 AI 使用分析";
  const rankingLabel = {
    department: "部门",
    team: "小组",
    user: "人员",
    model: "模型"
  }[rankingGroup];
  const peopleItems = peopleRankingsQuery.data?.items ?? [];
  const activePeople = peopleItems.filter((item) => !item.is_zero_usage);
  const zeroUsagePeople = peopleItems.filter((item) => item.is_zero_usage);
  const highUsagePeople = activePeople.slice(0, 5);
  const lowUsagePeople = [...activePeople].reverse().slice(0, 5);
  const selectedMember = peopleItems.find((item) => item.key === userID);
  const scopeLabel =
    scope === "mine"
      ? "仅本人数据"
      : user?.role === "team_leader"
        ? "当前小组"
        : user?.role === "director"
          ? "当前部门"
          : "全平台管理范围";

  return (
    <PagePanel
      title={title}
      className="token-analytics-page"
      description={
        scope === "mine"
          ? "查看个人 AI 使用趋势、模型分布和使用记录"
          : "判断团队 AI 使用情况，优先发现低用量与零用量成员"
      }
      breadcrumbs={[{ title }]}
      showNav={false}
    >
      <div className="token-analytics-filterbar">
        <div className="token-analytics-scope">
          {scope === "mine" ? <UserOutlined /> : <TeamOutlined />}
          <span>统计范围</span>
          <strong>{scopeLabel}</strong>
        </div>
        <div className="token-analytics-toolbar">
          <DatePicker.RangePicker
            value={range}
            allowClear={false}
            onChange={(value) => {
              if (!value?.[0] || !value[1]) return;
              setRange([value[0], value[1]]);
              setUserID(undefined);
              setQuery("");
              setQueryInput("");
              setPage(1);
            }}
          />
          {isAdminManagement ? (
            <>
              <Select
                allowClear
                value={departmentID}
                placeholder="全部部门"
                options={(departmentsQuery.data ?? []).map((item) => ({
                  label: item.name,
                  value: item.id
                }))}
                onChange={(value) => {
                  setDepartmentID(value);
                  setUserID(undefined);
                  setPage(1);
                }}
                style={{ minWidth: 160 }}
              />
              <Select
                allowClear
                value={teamID}
                placeholder="全部小组"
                options={(teamsQuery.data ?? []).map((item) => ({
                  label: item.name,
                  value: item.id
                }))}
                onChange={(value) => {
                  setTeamID(value);
                  setUserID(undefined);
                  setPage(1);
                }}
                style={{ minWidth: 160 }}
              />
            </>
          ) : null}
        </div>
      </div>

      <div className="token-analytics-meta">
        <span>
          <ClockCircleOutlined /> 数据截至 {formatSnapshotTime(summary?.metrics_snapshot_at)}
        </span>
        <span>{overviewFilters.from} 至 {overviewFilters.to}</span>
        {summary ? statusTag(summary.quality_status) : null}
      </div>

      {summaryQuery.isError ? (
        <Alert
          type="error"
          showIcon
          message="Token 分析加载失败"
          description={errorText(summaryQuery.error)}
          action={<Button onClick={() => void summaryQuery.refetch()}>重试</Button>}
        />
      ) : null}
      {summary?.data_freshness === "pending" ? (
        <Alert
          type="warning"
          showIcon
          message={`${summary.pending_source_count} 个 Session 来源仍在统计，当前结果为已完成部分`}
        />
      ) : null}
      {scope === "management" && summary && summary.pricing_status !== "priced" ? (
        <Alert
          type="info"
          showIcon
          message={`成本未完全计价：${formatTokenValue(summary.unpriced_tokens)} Token 暂未计入金额`}
        />
      ) : null}

      <RequirementMetricGrid>
        <RequirementMetricCard
          tone="primary"
          icon={<ThunderboltOutlined />}
          loading={summaryQuery.isLoading}
          metric={{
            key: "total",
            title: "总 Token",
            value: formatTokenValue(summary?.total_tokens),
            description: scope === "mine" ? "我的全部 AI 使用量" : "当前管理范围合计"
          }}
        />
        {scope === "management" ? (
          <>
            <RequirementMetricCard
              tone="success"
              icon={<CheckCircleOutlined />}
              loading={peopleRankingsQuery.isLoading}
              metric={{
                key: "active_people",
                title: "有使用成员",
                value: activePeople.length,
                description: `当前成员共 ${peopleItems.length} 人`
              }}
            />
            <RequirementMetricCard
              tone={zeroUsagePeople.length ? "danger" : "success"}
              icon={zeroUsagePeople.length ? <WarningOutlined /> : <CheckCircleOutlined />}
              loading={peopleRankingsQuery.isLoading}
              metric={{
                key: "zero_people",
                title: "零用量成员",
                value: zeroUsagePeople.length,
                description: zeroUsagePeople.length ? "建议优先确认使用情况" : "当前范围暂无零用量成员"
              }}
            />
            <RequirementMetricCard
              tone="info"
              icon={<BarChartOutlined />}
              loading={summaryQuery.isLoading || peopleRankingsQuery.isLoading}
              metric={{
                key: "average",
                title: "人均 Token",
                value: formatAverage(summary?.total_tokens, peopleItems.length),
                description: "按当前成员人数计算"
              }}
            />
          </>
        ) : (
          <>
            <RequirementMetricCard
              tone="info"
              icon={<CalendarOutlined />}
              loading={summaryQuery.isLoading}
              metric={{
                key: "days",
                title: "活跃天数",
                value: summary?.active_days ?? "0",
                description: "所选范围内有 AI 使用的日期"
              }}
            />
            <RequirementMetricCard
              tone="info"
              icon={<ApiOutlined />}
              loading={overviewSessionsQuery.isLoading}
              metric={{
                key: "sessions",
                title: "Session 数量",
                value: overviewSessionsQuery.data?.total ?? 0,
                description: "所选范围内的工作记录"
              }}
            />
          </>
        )}
        <RequirementMetricCard
          tone="success"
          icon={<DollarOutlined />}
          loading={summaryQuery.isLoading}
          metric={{
            key: "cost",
            title: "API 等价成本",
            value: formatCost(summary?.estimated_cost_cny),
            description:
              scope === "mine" && summary && summary.pricing_status !== "priced"
                ? `另有 ${formatTokenValue(summary.unpriced_tokens)} Token 待计价`
                : "按已发布价格与汇率估算"
          }}
        />
      </RequirementMetricGrid>

      <div className="token-analytics-composition">
        <span>Token 构成</span>
        <span>输入 {formatTokenValue(summary?.uncached_input_tokens)}</span>
        <span>
          缓存 {formatTokenValue(summary ? (BigInt(summary.cache_read_tokens) + BigInt(summary.cache_write_5m_tokens) + BigInt(summary.cache_write_1h_tokens)).toString() : "0")}
        </span>
        <span>输出 {formatTokenValue(summary?.output_tokens)}</span>
      </div>

      {scope === "management" ? (
        <section className="token-analytics-section token-analytics-attention">
          <header>
            <div>
              <h3>重点关注</h3>
              <p>先看使用最充分和使用偏低的成员，再进入 Session 核实具体情况</p>
            </div>
            {zeroUsagePeople.length ? <Tag color="error">{zeroUsagePeople.length} 人零用量</Tag> : null}
          </header>
          <div className="token-analytics-attention-grid">
            <div className="token-analytics-ranking-list">
              <div className="token-analytics-ranking-list__title">
                <strong>高用量 Top 5</strong>
                <span>了解高频使用者</span>
              </div>
              {highUsagePeople.length ? highUsagePeople.map((item, index) => (
                <button key={item.key} type="button" onClick={() => { setUserID(item.key); setPage(1); }}>
                  <span className="token-analytics-rank-number">{index + 1}</span>
                  <span className="token-analytics-rank-person"><strong>{item.label}</strong><small>{formatCost(item.estimated_cost_cny)}</small></span>
                  <span className="token-analytics-rank-value">{formatTokenValue(item.total_tokens)}<ArrowRightOutlined /></span>
                </button>
              )) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无成员用量" />}
            </div>
            <div className="token-analytics-ranking-list token-analytics-ranking-list--attention">
              <div className="token-analytics-ranking-list__title">
                <strong>低用量与零用量</strong>
                <span>建议优先确认是否正常使用</span>
              </div>
              {[...zeroUsagePeople, ...lowUsagePeople].slice(0, 5).length ? [...zeroUsagePeople, ...lowUsagePeople].slice(0, 5).map((item) => (
                <button key={item.key} type="button" onClick={() => { setUserID(item.key); setPage(1); }}>
                  <span className={`token-analytics-status-dot${item.is_zero_usage ? " is-zero" : ""}`} />
                  <span className="token-analytics-rank-person"><strong>{item.label}</strong><small>{item.is_zero_usage ? "所选周期无用量" : `最近活跃 ${item.last_activity_at ? dayjs(item.last_activity_at).format("MM-DD HH:mm") : "--"}`}</small></span>
                  <span className="token-analytics-rank-value">{formatTokenValue(item.total_tokens)}<ArrowRightOutlined /></span>
                </button>
              )) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无低用量成员" />}
            </div>
          </div>
        </section>
      ) : null}

      <div className="token-analytics-charts">
        <section>
          <header>
            <h3>Token 趋势</h3>
          </header>
          <BaseEChart
            option={trendOption}
            height={280}
            loading={trendsQuery.isLoading}
            empty={!trendsQuery.data?.items.length}
            renderer="svg"
          />
        </section>
        <section>
          <header>
            <h3>{scope === "mine" ? "模型构成" : `${rankingLabel}对比`}</h3>
            {scope === "management" ? (
              <Segmented
                size="small"
                value={rankingGroup}
                onChange={(value) => setRankingGroup(value as typeof rankingGroup)}
                options={[
                  ...(user?.role === "admin" ? [{ label: "部门", value: "department" }] : []),
                  { label: "小组", value: "team" },
                  { label: "人员", value: "user" },
                  { label: "模型", value: "model" }
                ]}
              />
            ) : null}
          </header>
          <BaseEChart
            option={rankingOption}
            height={280}
            loading={rankingsQuery.isLoading}
            empty={!rankingsQuery.data?.items.length}
            renderer="svg"
          />
        </section>
      </div>

      {scope === "management" ? (
        <section className="token-analytics-section">
          <header>
            <div>
              <h3>{rankingLabel}完整排名</h3>
              <p>{rankingGroup === "user" ? "查看 Session 后，顶部统计和排名保持当前管理范围不变" : `按 ${rankingLabel} 汇总当前范围用量`}</p>
            </div>
          </header>
          <Table<TokenAnalyticsRankingItem>
            rowKey="key"
            size="small"
            pagination={false}
            loading={rankingsQuery.isLoading}
            dataSource={rankingsQuery.data?.items ?? []}
            columns={[
              { title: rankingLabel, dataIndex: "label" },
              {
                title: "Token",
                dataIndex: "total_tokens",
                align: "right",
                render: (value: string) => formatTokenValue(value)
              },
              {
                title: "API 等价成本",
                dataIndex: "estimated_cost_cny",
                align: "right",
                render: (value?: string) => formatCost(value)
              },
              {
                title: "计价状态",
                dataIndex: "pricing_status",
                render: (value: string) => statusTag(value)
              },
              ...(rankingGroup === "user" ? [{
                title: "操作",
                width: 120,
                align: "right" as const,
                render: (_: unknown, record: TokenAnalyticsRankingItem) => (
                  <Button type="link" onClick={() => { setUserID(record.key); setPage(1); }}>
                    查看 Session <ArrowRightOutlined />
                  </Button>
                )
              }] : [])
            ]}
          />
        </section>
      ) : null}

      <section className="token-analytics-section">
        <header className="token-analytics-session-header">
          <div>
            <h3>{scope === "mine" ? "使用记录" : "使用明细"}</h3>
            <p>{selectedMember ? `当前查看：${selectedMember.label}` : scope === "mine" ? "查看每次 AI 使用的模型、Token 和成本" : "按成员或关键词查看具体 Session"}</p>
          </div>
          <Space size={8} wrap className="token-analytics-session-actions">
            {selectedMember ? <Tag color="blue" closable onClose={() => { setUserID(undefined); setPage(1); }}>成员：{selectedMember.label}</Tag> : null}
            {query ? <Tag closable onClose={() => { setQuery(""); setQueryInput(""); setPage(1); }}>关键词：{query}</Tag> : null}
            {sessionSummaryQuery.data?.search_mode === "exact_session_ref" ? (
              <Tag color="blue">精确 ID 定位</Tag>
            ) : null}
            <Input.Search
              value={queryInput}
              allowClear
              enterButton={<SearchOutlined />}
              placeholder="搜索 Session ID 或摘要"
              onChange={(event) => setQueryInput(event.target.value)}
              onSearch={(value) => { setQuery(value.trim()); setPage(1); }}
              style={{ width: 320 }}
            />
          </Space>
        </header>
        <Table<TokenAnalyticsSessionItem>
          rowKey="session_id"
          loading={sessionsQuery.isLoading}
          dataSource={sessionsQuery.data?.items ?? []}
          pagination={{
            current: sessionsQuery.data?.page ?? page,
            pageSize: sessionsQuery.data?.page_size ?? pageSize,
            total: sessionsQuery.data?.total ?? 0,
            showSizeChanger: true,
            showTotal: (total) => `共 ${total} 条`,
            onChange: (nextPage, nextPageSize) => {
              setPage(nextPage);
              setPageSize(nextPageSize);
            }
          }}
          locale={{ emptyText: <Empty description="所选范围暂无 Session" /> }}
          columns={[
            {
              title: "Session",
              dataIndex: "session_ref",
              width: 330,
              render: (value: string, record) => (
                <div className="token-analytics-session-cell">
                  <strong>{value}</strong>
                  {record.summary ? <span title={record.summary}>{record.summary}</span> : null}
                </div>
              )
            },
            ...(scope === "management"
              ? [{ title: "成员", dataIndex: "user_name", width: 130 }]
              : []),
            { title: "模型", dataIndex: "model", width: 180 },
            {
              title: "活动日期",
              width: 190,
              render: (_: unknown, record: TokenAnalyticsSessionItem) =>
                `${record.activity_from} ~ ${record.activity_to}`
            },
            {
              title: "Token",
              dataIndex: "total_tokens",
              width: 110,
              align: "right",
              render: (value: string) => formatTokenValue(value)
            },
            {
              title: "API 等价成本",
              dataIndex: "estimated_cost_cny",
              width: 130,
              align: "right",
              render: (value?: string) => formatCost(value)
            },
            {
              title: "状态",
              dataIndex: "pricing_status",
              width: 100,
              render: (value: string) => statusTag(value)
            }
          ]}
        />
      </section>
    </PagePanel>
  );
}
