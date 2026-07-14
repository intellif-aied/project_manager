import {
  ApiOutlined,
  CalendarOutlined,
  DatabaseOutlined,
  DollarOutlined,
  SearchOutlined,
  ThunderboltOutlined
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
    scope === "mine" ? "model" : "user"
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

  const filters = useMemo<TokenAnalyticsFilters>(
    () => ({
      scope,
      from: range[0].format("YYYY-MM-DD"),
      to: range[1].format("YYYY-MM-DD"),
      ...(teamID ? { team_id: teamID } : {}),
      ...(departmentID ? { department_id: departmentID } : {}),
      ...(userID ? { user_id: userID } : {}),
      ...(query ? { q: query } : {})
    }),
    [departmentID, query, range, scope, teamID, userID]
  );

  const canLoad = Boolean(
    capabilityQuery.data?.enabled && (scope === "mine" || capabilityQuery.data?.can_manage)
  );
  const summaryQuery = useQuery({
    queryKey: ["token-analytics-summary", filters],
    queryFn: () => fetchTokenAnalyticsSummary(filters),
    enabled: canLoad,
    staleTime: 30_000
  });
  const snapshotToken = summaryQuery.data?.query_snapshot_token;
  const trendsQuery = useQuery({
    queryKey: ["token-analytics-trends", snapshotToken],
    queryFn: () => fetchTokenAnalyticsTrends({ ...filters, query_snapshot_token: snapshotToken! }),
    enabled: Boolean(snapshotToken)
  });
  const rankingsQuery = useQuery({
    queryKey: ["token-analytics-rankings", snapshotToken, rankingGroup],
    queryFn: () =>
      fetchTokenAnalyticsRankings({
        ...filters,
        query_snapshot_token: snapshotToken!,
        group_by: rankingGroup
      }),
    enabled: Boolean(snapshotToken)
  });
  const sessionsQuery = useQuery({
    queryKey: ["token-analytics-sessions", snapshotToken, page, pageSize],
    queryFn: () =>
      fetchTokenAnalyticsSessions({
        ...filters,
        query_snapshot_token: snapshotToken!,
        page: String(page),
        page_size: String(pageSize)
      }),
    enabled: Boolean(snapshotToken),
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
      grid: { left: 52, right: 20, top: 28, bottom: 38 },
      xAxis: { type: "category", data: points.map((item) => item.date), boundaryGap: false },
      yAxis: {
        type: "value",
        axisLabel: {
          formatter: (value: number) => `≈${formatTokenValue(String(Math.round(value)))}`
        }
      },
      series: [
        {
          type: "line",
          smooth: true,
          symbolSize: 7,
          data: points.map((item) => Number(item.total_tokens)),
          lineStyle: { width: 3, color: "#1677ff" },
          itemStyle: { color: "#1677ff" },
          areaStyle: { color: "rgba(22, 119, 255, 0.08)" }
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
      grid: { left: 118, right: 24, top: 20, bottom: 28 },
      xAxis: {
        type: "value",
        axisLabel: {
          formatter: (value: number) => `≈${formatTokenValue(String(Math.round(value)))}`
        }
      },
      yAxis: { type: "category", inverse: true, data: items.map((item) => item.label) },
      series: [
        {
          type: "bar",
          data: items.map((item) => Number(item.total_tokens)),
          barMaxWidth: 18,
          itemStyle: { color: "#13a8a8", borderRadius: [0, 3, 3, 0] }
        }
      ]
    };
  }, [rankingsQuery.data]);

  if (capabilityQuery.isLoading) {
    return (
      <PagePanel title={scope === "mine" ? "Token 用量" : "Token 使用分析"}>
        <Spin />
      </PagePanel>
    );
  }
  if (
    !capabilityQuery.data?.enabled ||
    (scope === "management" && !capabilityQuery.data.can_manage)
  ) {
    return (
      <PagePanel title={scope === "mine" ? "Token 用量" : "Token 使用分析"}>
        <Alert type="info" showIcon message="该功能尚未对当前账号开放" />
      </PagePanel>
    );
  }

  const summary = summaryQuery.data;
  const title = scope === "mine" ? "Token 用量" : "Token 使用分析";
  const rankingLabel = {
    department: "部门",
    team: "小组",
    user: "人员",
    model: "模型"
  }[rankingGroup];

  return (
    <PagePanel
      title={title}
      className="token-analytics-page"
      description={
        scope === "mine" ? "我的 Token 与 API 等价成本" : "管理范围内的 Token、排名与 Session"
      }
      breadcrumbs={[{ title }]}
      showNav={false}
    >
      <div className="token-analytics-toolbar">
        <DatePicker.RangePicker
          value={range}
          allowClear={false}
          onChange={(value) => {
            if (!value?.[0] || !value[1]) return;
            setRange([value[0], value[1]]);
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
                setPage(1);
              }}
              style={{ minWidth: 160 }}
            />
          </>
        ) : null}
        <Input.Search
          value={queryInput}
          allowClear
          enterButton={<SearchOutlined />}
          placeholder="搜索 Session ID 或概览"
          onChange={(event) => setQueryInput(event.target.value)}
          onSearch={(value) => {
            setQuery(value.trim());
            setPage(1);
          }}
          style={{ width: 300 }}
        />
        {userID ? (
          <Tag closable onClose={() => setUserID(undefined)}>
            已筛选成员 {userID}
          </Tag>
        ) : null}
      </div>

      <div className="tokens-range-hint">
        默认最近 7 天，统计、排名与 Session 明细使用同一数据快照。
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
          message="部分 Session 仍在统计中"
          description={`当前展示已统计部分，${summary.pending_source_count} 个来源尚未追平。`}
        />
      ) : null}
      {summary && summary.pricing_status !== "priced" ? (
        <Alert
          type="info"
          showIcon
          message="成本尚未完全计价"
          description={`未计价 Token ${formatTokenValue(summary.unpriced_tokens)}，金额不会以 0 元代替。`}
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
            description: `${filters.from} ~ ${filters.to}`
          }}
        />
        <RequirementMetricCard
          tone="info"
          icon={<ApiOutlined />}
          loading={summaryQuery.isLoading}
          metric={{
            key: "input",
            title: "输入 Token",
            value: formatTokenValue(summary?.uncached_input_tokens),
            description: "未命中缓存的输入"
          }}
        />
        <RequirementMetricCard
          tone="info"
          icon={<DatabaseOutlined />}
          loading={summaryQuery.isLoading}
          metric={{
            key: "cache",
            title: "缓存 Token",
            value: formatTokenValue(
              summary
                ? (
                    BigInt(summary.cache_read_tokens) +
                    BigInt(summary.cache_write_5m_tokens) +
                    BigInt(summary.cache_write_1h_tokens)
                  ).toString()
                : "0"
            ),
            description: "读取与写入合计"
          }}
        />
        <RequirementMetricCard
          tone="success"
          icon={<DollarOutlined />}
          loading={summaryQuery.isLoading}
          metric={{
            key: "cost",
            title: "API 等价成本",
            value: formatCost(summary?.estimated_cost_cny),
            description: "按已发布价格与汇率估算"
          }}
        />
        <RequirementMetricCard
          tone="warning"
          icon={<CalendarOutlined />}
          loading={summaryQuery.isLoading}
          metric={{
            key: "days",
            title: "活跃天数",
            value: summary?.active_days ?? "0",
            description: "所选范围内有 Token 的日期"
          }}
        />
      </RequirementMetricGrid>

      <div className="token-analytics-charts">
        <section>
          <header>
            <h3>Token 趋势</h3>
            {summary ? statusTag(summary.quality_status) : null}
          </header>
          <BaseEChart
            option={trendOption}
            height={300}
            loading={trendsQuery.isLoading}
            empty={!trendsQuery.data?.items.length}
            renderer="svg"
          />
        </section>
        <section>
          <header>
            <h3>{rankingLabel}分布</h3>
            <Segmented
              size="small"
              value={rankingGroup}
              onChange={(value) => setRankingGroup(value as typeof rankingGroup)}
              options={
                scope === "mine"
                  ? [{ label: "模型", value: "model" }]
                  : [
                      ...(user?.role === "admin" ? [{ label: "部门", value: "department" }] : []),
                      { label: "小组", value: "team" },
                      { label: "人员", value: "user" },
                      { label: "模型", value: "model" }
                    ]
              }
            />
          </header>
          <BaseEChart
            option={rankingOption}
            height={300}
            loading={rankingsQuery.isLoading}
            empty={!rankingsQuery.data?.items.length}
            renderer="svg"
          />
        </section>
      </div>

      {scope === "management" && rankingGroup === "user" ? (
        <section className="token-analytics-section">
          <header>
            <h3>人员排名</h3>
            <span>点击人员查看对应 Session</span>
          </header>
          <Table<TokenAnalyticsRankingItem>
            rowKey="key"
            size="small"
            pagination={false}
            loading={rankingsQuery.isLoading}
            dataSource={rankingsQuery.data?.items ?? []}
            onRow={(record) => ({
              onClick: () => {
                setUserID(record.key);
                setPage(1);
              },
              className: "token-analytics-clickable-row"
            })}
            columns={[
              { title: "成员", dataIndex: "label" },
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
              }
            ]}
          />
        </section>
      ) : null}

      <section className="token-analytics-section">
        <header>
          <h3>Session 明细</h3>
          <Space size={8}>
            {summary?.search_mode === "exact_session_ref" ? (
              <Tag color="blue">精确 ID 定位</Tag>
            ) : null}
            <span>{sessionsQuery.data?.total ?? 0} 条</span>
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
            onChange: (nextPage, nextPageSize) => {
              setPage(nextPage);
              setPageSize(nextPageSize);
            }
          }}
          locale={{ emptyText: <Empty description="所选范围暂无 Session" /> }}
          scroll={{ x: 1080 }}
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
