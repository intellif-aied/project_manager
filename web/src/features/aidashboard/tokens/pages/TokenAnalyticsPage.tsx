import {
  ApiOutlined,
  ArrowLeftOutlined,
  ArrowRightOutlined,
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
import { useEffect, useMemo, useRef, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";

import {
  fetchDepartments,
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

export interface TokenAnalyticsMember {
  id: string;
  name: string;
}

interface TokenAnalyticsPageProps {
  scope: "mine" | "management";
  member?: TokenAnalyticsMember;
  onOpenMember?: (member: TokenAnalyticsMember) => void;
  onBack?: () => void;
}

interface TokenAnalyticsDrilldownState {
  tokenAnalyticsMember?: TokenAnalyticsMember;
}

function defaultRange(): DateRange {
  const end = dayjs();
  return [end.subtract(2, "day"), end];
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

export function TokenAnalyticsManagementPage() {
  const location = useLocation();
  const navigate = useNavigate();
  const returnScrollTopRef = useRef(0);
  const returnFocusRef = useRef<HTMLElement | null>(null);
  const previousMemberRef = useRef<TokenAnalyticsMember>();
  const [hasOpenedMember, setHasOpenedMember] = useState(false);
  const locationState = (location.state ?? {}) as TokenAnalyticsDrilldownState;
  const selectedMember = locationState.tokenAnalyticsMember;

  const openMember = (nextMember: TokenAnalyticsMember) => {
    const scrollContainer = document.getElementById("main-content-scroll-container");
    returnScrollTopRef.current = scrollContainer?.scrollTop ?? 0;
    returnFocusRef.current =
      document.activeElement instanceof HTMLElement ? document.activeElement : null;
    setHasOpenedMember(true);
    navigate(`${location.pathname}${location.search}`, {
      state: { ...locationState, tokenAnalyticsMember: nextMember }
    });
  };

  const closeMember = () => navigate(-1);

  useEffect(() => {
    const scrollContainer = document.getElementById("main-content-scroll-container");
    if (selectedMember) {
      previousMemberRef.current = selectedMember;
      scrollContainer?.scrollTo({ top: 0, left: 0 });
      return;
    }
    if (!previousMemberRef.current) return;
    previousMemberRef.current = undefined;
    requestAnimationFrame(() => {
      scrollContainer?.scrollTo({ top: returnScrollTopRef.current, left: 0 });
      returnFocusRef.current?.focus({ preventScroll: true });
    });
  }, [selectedMember]);

  return (
    <div className="token-analytics-drilldown">
      <div
        className={`token-analytics-drilldown__view token-analytics-drilldown__view--overview${
          !selectedMember && hasOpenedMember ? " is-entering" : ""
        }`}
        hidden={Boolean(selectedMember)}
      >
        <TokenAnalyticsPage scope="management" onOpenMember={openMember} />
      </div>
      {selectedMember ? (
        <div className="token-analytics-drilldown__view token-analytics-drilldown__view--detail is-entering">
          <TokenAnalyticsPage scope="management" member={selectedMember} onBack={closeMember} />
        </div>
      ) : null}
    </div>
  );
}

export function TokenAnalyticsPage({
  scope,
  member,
  onOpenMember,
  onBack
}: TokenAnalyticsPageProps) {
  const { user } = useAuth();
  const navigate = useNavigate();
  const isMemberDetail = scope === "management" && Boolean(member);
  const isPersonalView = scope === "mine" || isMemberDetail;
  const [range, setRange] = useState<DateRange>(() => defaultRange());
  const [queryInput, setQueryInput] = useState("");
  const [query, setQuery] = useState("");
  const [teamID, setTeamID] = useState<string>();
  const [departmentID, setDepartmentID] = useState<string>();
  const [showAllRankings, setShowAllRankings] = useState(false);
  const [memberQuery, setMemberQuery] = useState("");
  const [memberPage, setMemberPage] = useState(1);
  const [memberPageSize, setMemberPageSize] = useState(10);
  const [rankingGroup, setRankingGroup] = useState<"department" | "team" | "user" | "model">(
    isPersonalView ? "model" : user?.role === "team_leader" ? "user" : "team"
  );
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);

  const capabilityQuery = useQuery({
    queryKey: ["token-analytics-capability", user?.id],
    queryFn: fetchTokenAnalyticsCapability,
    staleTime: 60_000
  });
  const isAdminManagement = scope === "management" && !isMemberDetail && user?.role === "admin";
  const canFilterByTeam = scope === "management" && !isMemberDetail && user?.role !== "team_leader";
  const departmentsQuery = useQuery({
    queryKey: ["token-analytics-departments"],
    queryFn: fetchDepartments,
    enabled: isAdminManagement,
    staleTime: 60_000
  });

  const baseOverviewFilters = useMemo<TokenAnalyticsFilters>(
    () => ({
      scope,
      from: range[0].format("YYYY-MM-DD"),
      to: range[1].format("YYYY-MM-DD"),
      ...(departmentID ? { department_id: departmentID } : {}),
      ...(member ? { user_id: member.id } : {})
    }),
    [departmentID, member, range, scope]
  );
  const overviewFilters = useMemo<TokenAnalyticsFilters>(
    () => ({
      ...baseOverviewFilters,
      ...(teamID ? { team_id: teamID } : {})
    }),
    [baseOverviewFilters, teamID]
  );
  const sessionFilters = useMemo<TokenAnalyticsFilters>(
    () => ({
      ...overviewFilters,
      ...(query ? { q: query } : {})
    }),
    [overviewFilters, query]
  );

  const canLoad = Boolean(
    capabilityQuery.data?.enabled && (scope === "mine" || capabilityQuery.data?.can_manage)
  );
  const scopeSummaryQuery = useQuery({
    queryKey: ["token-analytics-summary", baseOverviewFilters],
    queryFn: () => fetchTokenAnalyticsSummary(baseOverviewFilters),
    enabled: canLoad && canFilterByTeam,
    staleTime: 30_000
  });
  const summaryQuery = useQuery({
    queryKey: ["token-analytics-summary", overviewFilters],
    queryFn: () => fetchTokenAnalyticsSummary(overviewFilters),
    enabled: canLoad,
    staleTime: 30_000
  });
  const scopeSnapshotToken = scopeSummaryQuery.data?.query_snapshot_token;
  const snapshotToken = summaryQuery.data?.query_snapshot_token;
  const hasSessionFilter = Boolean(query);
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
    enabled: scope === "management" && !isMemberDetail && Boolean(snapshotToken)
  });
  const teamOptionsQuery = useQuery({
    queryKey: ["token-analytics-team-options", scopeSnapshotToken],
    queryFn: () =>
      fetchTokenAnalyticsRankings({
        ...baseOverviewFilters,
        query_snapshot_token: scopeSnapshotToken!,
        group_by: "team"
      }),
    enabled: canFilterByTeam && Boolean(scopeSnapshotToken)
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
    enabled: isPersonalView && Boolean(snapshotToken)
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
    enabled: isPersonalView && Boolean(sessionSnapshotToken),
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
        <Alert type="info" showIcon title="该功能尚未对当前账号开放" />
      </PagePanel>
    );
  }

  const summary = summaryQuery.data;
  const title = scope === "mine" ? "我的 Token" : isMemberDetail ? `${member?.name ?? "成员"} 的 Token` : "团队 AI 使用分析";
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
  const filteredPeopleItems = memberQuery.trim()
    ? peopleItems.filter((item) => item.label.toLowerCase().includes(memberQuery.trim().toLowerCase()))
    : peopleItems;
  const usageCoverage = peopleItems.length
    ? `${Math.round((activePeople.length / peopleItems.length) * 100)}%`
    : "--";
  const openMember = (item: TokenAnalyticsRankingItem) => {
    if (onOpenMember) {
      onOpenMember({ id: item.key, name: item.label });
      return;
    }
    const params = new URLSearchParams({ name: item.label });
    navigate(`/token-analytics/member/${encodeURIComponent(item.key)}?${params.toString()}`);
  };
  const toggleMemberList = () => {
    const nextShowMemberList = !showAllRankings;
    setShowAllRankings(nextShowMemberList);
    setMemberPage(1);
  };
  const availableTeams = teamOptionsQuery.data?.items ?? [];
  const selectedTeamName = availableTeams.find((item) => item.key === teamID)?.label;
  const scopeLabel =
    scope === "mine"
      ? "仅本人数据"
      : isMemberDetail
        ? `成员：${member?.name ?? "--"}`
      : selectedTeamName
        ? `小组：${selectedTeamName}`
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
          : isMemberDetail
            ? "查看该成员的 AI 使用趋势、模型分布和使用记录"
            : "对比成员使用量，定位需要进一步了解的人员"
      }
      breadcrumbs={isMemberDetail ? [{ title: "团队 AI 使用分析" }, { title }] : [{ title }]}
      showNav={false}
    >
      <div className="token-analytics-filterbar">
        <div className="token-analytics-filterbar__context">
          {isMemberDetail ? (
            <Button
              type="text"
              icon={<ArrowLeftOutlined />}
              onClick={onBack ?? (() => navigate("/token-analytics"))}
            >
              返回
            </Button>
          ) : null}
          <div className="token-analytics-scope">
            {isPersonalView ? <UserOutlined /> : <TeamOutlined />}
            <span>统计范围</span>
            <strong>{scopeLabel}</strong>
          </div>
        </div>
        <div className="token-analytics-toolbar">
          <DatePicker.RangePicker
            value={range}
            allowClear={false}
            onChange={(value) => {
              if (!value?.[0] || !value[1]) return;
              setRange([value[0], value[1]]);
              setQuery("");
              setQueryInput("");
              setPage(1);
              setMemberPage(1);
            }}
          />
          {isAdminManagement ? (
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
                setTeamID(undefined);
                setPage(1);
                setMemberPage(1);
              }}
              style={{ minWidth: 160 }}
            />
          ) : null}
          {canFilterByTeam ? (
            <Select
              value={teamID ?? "all"}
              options={[
                { label: "全部小组（全局）", value: "all" },
                ...availableTeams.map((item) => ({
                  label: item.label,
                  value: item.key
                })).filter((item) => item.value !== "unknown")
              ]}
              onChange={(value) => {
                setTeamID(value === "all" ? undefined : value);
                setShowAllRankings(false);
                setMemberQuery("");
                setPage(1);
                setMemberPage(1);
              }}
              loading={teamOptionsQuery.isLoading}
              style={{ minWidth: 180 }}
            />
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
          title="Token 分析加载失败"
          description={errorText(summaryQuery.error)}
          action={<Button onClick={() => void summaryQuery.refetch()}>重试</Button>}
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
            description: scope === "mine" ? "我的全部 AI 使用量" : isMemberDetail ? "该成员的全部 AI 使用量" : "当前管理范围合计"
          }}
        />
        {!isPersonalView ? (
          <>
            <RequirementMetricCard
              tone="success"
              icon={<CheckCircleOutlined />}
              loading={peopleRankingsQuery.isLoading}
              metric={{
                key: "active_people",
                title: "有使用成员",
                value: `${activePeople.length} / ${peopleItems.length}`,
                description: `使用覆盖率 ${usageCoverage}`
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
                description: zeroUsagePeople.length ? "所选周期暂无 Token 记录" : "当前范围暂无零用量成员"
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
              isPersonalView && summary && summary.pricing_status !== "priced"
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

      {scope === "management" && !isMemberDetail ? (
        <section className="token-analytics-section token-analytics-attention">
          <header className={showAllRankings ? "token-analytics-ranking-header" : undefined}>
            <div>
              <h3 aria-live="polite">
                {showAllRankings ? "成员列表" : "成员用量"}
              </h3>
              <p>
                {showAllRankings
                  ? "按本期 Token 用量从高到低排列，默认每页 10 人"
                  : "先看本期用量前 5 和后 5，再进入成员页查看趋势与使用记录"}
              </p>
            </div>
            <Space size={8} wrap className="token-analytics-member-actions">
              {showAllRankings ? (
                <>
                  <Tag>{filteredPeopleItems.length} 人</Tag>
                  <Input
                    className="token-analytics-member-search"
                    allowClear
                    prefix={<SearchOutlined />}
                    placeholder="搜索成员姓名"
                    value={memberQuery}
                    onChange={(event) => {
                      setMemberQuery(event.target.value);
                      setMemberPage(1);
                    }}
                    style={{ width: 240 }}
                  />
                </>
              ) : zeroUsagePeople.length ? (
                <Tag>{zeroUsagePeople.length} 人本期未使用</Tag>
              ) : null}
              <Button
                type={showAllRankings ? "link" : "primary"}
                size="small"
                icon={showAllRankings ? undefined : <TeamOutlined />}
                aria-expanded={showAllRankings}
                aria-controls="token-analytics-member-content"
                onClick={toggleMemberList}
              >
                {showAllRankings ? "返回摘要" : "查看成员列表"}
              </Button>
            </Space>
          </header>
          {showAllRankings ? (
            <div id="token-analytics-member-content" className="token-analytics-member-list-panel">
              <Table<TokenAnalyticsRankingItem>
                rowKey="key"
                size="small"
                pagination={{
                  current: memberPage,
                  pageSize: memberPageSize,
                  showSizeChanger: true,
                  pageSizeOptions: [10, 20, 50],
                  showTotal: (total) => `共 ${total} 人`,
                  onChange: (nextPage, nextPageSize) => {
                    setMemberPage(nextPageSize === memberPageSize ? nextPage : 1);
                    setMemberPageSize(nextPageSize);
                  }
                }}
                loading={peopleRankingsQuery.isLoading}
                dataSource={filteredPeopleItems}
                columns={[
                  {
                    title: "成员",
                    dataIndex: "label",
                    className: "token-analytics-member-column token-analytics-member-column--name"
                  },
                  {
                    title: "Token",
                    dataIndex: "total_tokens",
                    className: "token-analytics-member-column token-analytics-member-column--tokens",
                    align: "right",
                    render: (value: string) => formatTokenValue(value)
                  },
                  {
                    title: "API 等价成本",
                    dataIndex: "estimated_cost_cny",
                    className: "token-analytics-member-column token-analytics-member-column--cost",
                    align: "right",
                    render: (value?: string) => formatCost(value)
                  },
                  {
                    title: "计价状态",
                    dataIndex: "pricing_status",
                    className: "token-analytics-member-column token-analytics-member-column--status",
                    render: (value: string, record: TokenAnalyticsRankingItem) =>
                      record.is_zero_usage ? <Tag>无用量</Tag> : statusTag(value)
                  },
                  {
                    title: "操作",
                    className: "token-analytics-member-column token-analytics-member-column--action",
                    width: 120,
                    align: "right" as const,
                    render: (_: unknown, record: TokenAnalyticsRankingItem) => (
                      <Button type="link" onClick={() => openMember(record)}>
                        查看详情 <ArrowRightOutlined />
                      </Button>
                    )
                  }
                ]}
              />
            </div>
          ) : (
            <div id="token-analytics-member-content" className="token-analytics-attention-grid">
              <div className="token-analytics-ranking-list">
                <div className="token-analytics-ranking-list__title">
                  <strong>用量前 5</strong>
                  <span>按 Token 从高到低</span>
                </div>
                {highUsagePeople.length ? highUsagePeople.map((item, index) => (
                  <button key={item.key} type="button" onClick={() => openMember(item)}>
                    <span className="token-analytics-rank-number">{index + 1}</span>
                    <span className="token-analytics-rank-person"><strong>{item.label}</strong><small>{formatCost(item.estimated_cost_cny)}</small></span>
                    <span className="token-analytics-rank-value">{formatTokenValue(item.total_tokens)}<ArrowRightOutlined /></span>
                  </button>
                )) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无成员用量" />}
              </div>
              <div className="token-analytics-ranking-list token-analytics-ranking-list--attention">
                <div className="token-analytics-ranking-list__title">
                  <strong>用量后 5</strong>
                  <span>仅统计本期有记录的成员</span>
                </div>
                {lowUsagePeople.length ? lowUsagePeople.map((item) => (
                  <button key={item.key} type="button" onClick={() => openMember(item)}>
                    <span className="token-analytics-status-dot" />
                    <span className="token-analytics-rank-person"><strong>{item.label}</strong><small>{`最近活跃 ${item.last_activity_at ? dayjs(item.last_activity_at).format("MM-DD HH:mm") : "--"}`}</small></span>
                    <span className="token-analytics-rank-value">{formatTokenValue(item.total_tokens)}<ArrowRightOutlined /></span>
                  </button>
                )) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无可比较成员" />}
              </div>
            </div>
          )}
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
            <h3>
              {isPersonalView ? "模型构成" : `${rankingLabel}对比`}
            </h3>
            {scope === "management" && !isMemberDetail ? (
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

      {isPersonalView ? <section className="token-analytics-section">
        <header className="token-analytics-session-header">
          <div>
            <h3>使用记录</h3>
            <p>查看每次 AI 使用的模型、Token 和成本</p>
          </div>
          <Space size={8} wrap className="token-analytics-session-actions">
            {query ? <Tag closable onClose={() => { setQuery(""); setQueryInput(""); setPage(1); }}>关键词：{query}</Tag> : null}
            {sessionSummaryQuery.data?.search_mode === "exact_session_ref" ? (
              <Tag color="blue">精确 ID 定位</Tag>
            ) : null}
            <Input.Search
              className="token-analytics-session-search"
              value={queryInput}
              allowClear
              enterButton={<SearchOutlined />}
              placeholder="搜索 Session ID 或摘要"
              aria-label="搜索 Session ID 或摘要"
              onChange={(event) => {
                const value = event.target.value;
                setQueryInput(value);
                if (!value && query) {
                  setQuery("");
                  setPage(1);
                }
              }}
              onSearch={(value) => { setQuery(value.trim()); setPage(1); }}
              style={{ width: 320 }}
            />
          </Space>
        </header>
        <Table<TokenAnalyticsSessionItem>
          className="token-analytics-session-table"
          rowKey="session_id"
          loading={sessionsQuery.isLoading}
          dataSource={sessionsQuery.data?.items ?? []}
          pagination={{
            current: sessionsQuery.data?.page ?? page,
            pageSize: sessionsQuery.data?.page_size ?? pageSize,
            total: sessionsQuery.data?.total ?? 0,
            showSizeChanger: false,
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
              className: "token-analytics-session-column token-analytics-session-column--session",
              width: 330,
              render: (value: string, record) => (
                <div className="token-analytics-session-cell">
                  <strong>{value}</strong>
                  {record.summary ? <span title={record.summary}>{record.summary}</span> : null}
                </div>
              )
            },
            {
              title: "模型",
              dataIndex: "model",
              className: "token-analytics-session-column token-analytics-session-column--model",
              width: 180
            },
            {
              title: "活动日期",
              className: "token-analytics-session-column token-analytics-session-column--activity",
              width: 190,
              render: (_: unknown, record: TokenAnalyticsSessionItem) =>
                `${record.activity_from} ~ ${record.activity_to}`
            },
            {
              title: "Token",
              dataIndex: "total_tokens",
              className: "token-analytics-session-column token-analytics-session-column--tokens",
              width: 110,
              align: "right",
              render: (value: string) => formatTokenValue(value)
            },
            {
              title: "API 等价成本",
              dataIndex: "estimated_cost_cny",
              className: "token-analytics-session-column token-analytics-session-column--cost",
              width: 130,
              align: "right",
              render: (value?: string) => formatCost(value)
            },
            {
              title: "状态",
              dataIndex: "pricing_status",
              className: "token-analytics-session-column token-analytics-session-column--status",
              width: 100,
              render: (value: string) => statusTag(value)
            }
          ]}
        />
      </section> : null}
    </PagePanel>
  );
}
