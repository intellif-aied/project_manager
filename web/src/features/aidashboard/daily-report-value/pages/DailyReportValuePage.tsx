import {
  CheckCircleOutlined,
  DownloadOutlined,
  ExclamationCircleOutlined,
  EyeOutlined,
  LineChartOutlined
} from "@ant-design/icons";
import { useQuery } from "@tanstack/react-query";
import {
  Alert,
  Button,
  Card,
  Col,
  Collapse,
  DatePicker,
  Descriptions,
  Drawer,
  Empty,
  Flex,
  message,
  Row,
  Segmented,
  Select,
  Space,
  Statistic,
  Table,
  Tag,
  Typography
} from "antd";
import type { TableProps } from "antd";
import dayjs from "dayjs";
import type { EChartsOption } from "echarts";
import { useMemo, useState } from "react";

import {
  downloadDailyReportValue,
  fetchDepartments,
  fetchDailyReportValue,
  fetchDailyReportValueDetail,
  fetchTeams
} from "../../api/client";
import type {
  DailyReportValueMetrics,
  DailyReportValueRatio,
  DailyReportValueUserDay
} from "../../api/types";
import { BaseEChart } from "@/shared/charts/BaseEChart";
import { PagePanel } from "@/shared/components/PagePanel/PagePanel";

import "./DailyReportValuePage.css";

const outcomeLabels: Record<string, string> = {
  confirmed_direct_use: "确认直接使用",
  modified: "已修改",
  observed_unchanged: "观察未变",
  no_explicit_outcome: "无明确结果",
  not_comparable: "历史未采集",
  deleted: "已删除",
  handwritten: "手写日报",
  no_result: "无结果"
};

const summaryLabels: Record<string, string> = {
  summary_unchanged: "工作概览未改",
  summary_modified: "工作概览已改",
  summary_removed: "工作概览已删除",
  summary_reduced_30: "工作概览缩短 ≥30%",
  not_applicable: "不可比较"
};

const changeBandLabels: Record<string, string> = {
  unchanged: "未修改",
  light: "轻度修改",
  medium: "中度修改",
  heavy: "重度修改",
  not_applicable: "无可比结果"
};

function percent(value?: number) {
  return value === undefined ? "—" : `${(value * 100).toFixed(1)}%`;
}

function ratioText(ratio: DailyReportValueRatio) {
  return `${ratio.numerator}/${ratio.denominator}`;
}

function durationText(value?: number) {
  return value === undefined ? "—" : `${(value / 1000).toFixed(1)}s`;
}

function rowDiff(row: DailyReportValueUserDay) {
  return row.diff ?? row.observed_diff;
}

type DiffLine = { left?: string; right?: string; kind: "same" | "removed" | "added" | "changed" };

function alignDiffLines(left: string, right: string): DiffLine[] {
  const leftLines = left.split("\n");
  const rightLines = right.split("\n");
  const table = Array.from({ length: leftLines.length + 1 }, () =>
    Array<number>(rightLines.length + 1).fill(0)
  );
  for (let leftIndex = leftLines.length - 1; leftIndex >= 0; leftIndex -= 1) {
    for (let rightIndex = rightLines.length - 1; rightIndex >= 0; rightIndex -= 1) {
      table[leftIndex][rightIndex] =
        leftLines[leftIndex] === rightLines[rightIndex]
          ? table[leftIndex + 1][rightIndex + 1] + 1
          : Math.max(table[leftIndex + 1][rightIndex], table[leftIndex][rightIndex + 1]);
    }
  }
  const rows: DiffLine[] = [];
  let leftIndex = 0;
  let rightIndex = 0;
  while (leftIndex < leftLines.length || rightIndex < rightLines.length) {
    if (
      leftIndex < leftLines.length &&
      rightIndex < rightLines.length &&
      leftLines[leftIndex] === rightLines[rightIndex]
    ) {
      rows.push({ left: leftLines[leftIndex], right: rightLines[rightIndex], kind: "same" });
      leftIndex += 1;
      rightIndex += 1;
    } else if (
      leftIndex < leftLines.length &&
      rightIndex < rightLines.length &&
      table[leftIndex + 1][rightIndex] === table[leftIndex][rightIndex + 1]
    ) {
      rows.push({ left: leftLines[leftIndex], right: rightLines[rightIndex], kind: "changed" });
      leftIndex += 1;
      rightIndex += 1;
    } else if (
      rightIndex >= rightLines.length ||
      (leftIndex < leftLines.length &&
        table[leftIndex + 1][rightIndex] > table[leftIndex][rightIndex + 1])
    ) {
      rows.push({ left: leftLines[leftIndex], kind: "removed" });
      leftIndex += 1;
    } else {
      rows.push({ right: rightLines[rightIndex], kind: "added" });
      rightIndex += 1;
    }
  }
  return rows;
}

function DiffComparison({ generated, user }: { generated: string; user: string }) {
  const rows = useMemo(() => alignDiffLines(generated, user), [generated, user]);
  return (
    <div className="daily-value__comparison">
      <Card size="small" title="Generated Draft">
        <div className="daily-value__diff-pane">
          {rows.map((row, index) => (
            <div key={`${index}-${row.kind}`} className={`daily-value__diff-line is-${row.kind}`}>
              {row.left ?? " "}
            </div>
          ))}
        </div>
      </Card>
      <Card size="small" title="用户保存 / 提交稿">
        <div className="daily-value__diff-pane">
          {rows.map((row, index) => (
            <div key={`${index}-${row.kind}`} className={`daily-value__diff-line is-${row.kind}`}>
              {row.right ?? " "}
            </div>
          ))}
        </div>
      </Card>
    </div>
  );
}

function MetricCard({
  title,
  ratio,
  note
}: {
  title: string;
  ratio: DailyReportValueRatio;
  note?: string;
}) {
  return (
    <Card size="small" className="daily-value__metric-card">
      <Statistic title={title} value={percent(ratio.value)} />
      <Typography.Text type="secondary">
        {ratioText(ratio)}
        {note ? ` · ${note}` : ""}
      </Typography.Text>
    </Card>
  );
}

function ratioValue(numerator: number, denominator: number) {
  return denominator > 0 ? numerator / denominator : undefined;
}

function signedDelta(current?: number, previous?: number) {
  if (current === undefined || previous === undefined) return "—";
  const points = (current - previous) * 100;
  if (Math.abs(points) < 0.05) return "持平";
  return `${points > 0 ? "+" : ""}${points.toFixed(1)} 个百分点`;
}

function ExecutiveMetric({
  title,
  value,
  numerator,
  denominator,
  detail,
  tone
}: {
  title: string;
  value?: number;
  numerator: number;
  denominator: number;
  detail: string;
  tone: "positive" | "neutral" | "attention";
}) {
  return (
    <Card className={`daily-value__executive-metric is-${tone}`}>
      <Typography.Text className="daily-value__executive-metric-label">{title}</Typography.Text>
      <Flex align="baseline" gap={10}>
        <Typography.Title level={2}>{percent(value)}</Typography.Title>
        <Typography.Text type="secondary">
          {numerator}/{denominator}
        </Typography.Text>
      </Flex>
      <Typography.Text type="secondary">{detail}</Typography.Text>
    </Card>
  );
}

function ComparisonMetric({
  title,
  current,
  previous
}: {
  title: string;
  current?: number;
  previous?: number;
}) {
  return (
    <div className="daily-value__comparison-metric">
      <Typography.Text type="secondary">{title}</Typography.Text>
      <Flex align="baseline" gap={10}>
        <Typography.Title level={3}>{percent(current)}</Typography.Title>
        <Typography.Text type="secondary">上一观察日 {percent(previous)}</Typography.Text>
      </Flex>
      <Typography.Text>{signedDelta(current, previous)}</Typography.Text>
    </div>
  );
}

function executiveConclusion(
  date: string,
  metrics: DailyReportValueMetrics,
  participantCount: number
) {
  if (participantCount === 0) {
    return `${date} 暂无进入个人日报生成或保存链路的用户，当前不能形成价值判断。`;
  }
  const explicit = metrics.comparable_outcomes;
  const usable = metrics.light_or_less.numerator;
  const significant = metrics.significant_modification.numerator;
  const noDraft = Math.max(participantCount - metrics.ai_reports, 0);
  const evidence = explicit
    ? `在 ${explicit} 份有明确保存结果的日报中，${usable} 份直接使用或只做轻度修改，${significant} 份仍需显著修改。`
    : "当前还没有明确的用户保存结果，不能判断生成稿是否被接受。";
  const judgment =
    explicit > 0 && usable * 2 > explicit
      ? "多数明确结果已达到低修改工作量，但仍需结合更大样本持续观察。"
      : "当前尚不能证明多数用户可以不经明显编辑直接使用 AI 日报。";
  return `${date} 共 ${participantCount} 名用户进入观察，其中 ${metrics.ai_reports} 名获得 AI 成稿${noDraft ? `，${noDraft} 名未形成 AI 成稿` : ""}。${evidence}${judgment}`;
}

export function DailyReportValuePage() {
  const [reportDate, setReportDate] = useState(dayjs().subtract(1, "day").format("YYYY-MM-DD"));
  const [outcomeStatus, setOutcomeStatus] = useState<string>();
  const [changeBand, setChangeBand] = useState<string>();
  const [generationStatus, setGenerationStatus] = useState<string>();
  const [departmentID, setDepartmentID] = useState<string>();
  const [teamID, setTeamID] = useState<string>();
  const [variantHash, setVariantHash] = useState<string>();
  const [regenerated, setRegenerated] = useState<string>();
  const [summaryOutcome, setSummaryOutcome] = useState<string>();
  const [missingData, setMissingData] = useState<string>();
  const [trendDays, setTrendDays] = useState<14 | 30>(14);
  const [advancedFiltersOpen, setAdvancedFiltersOpen] = useState(false);
  const [page, setPage] = useState(1);
  const [detailUserID, setDetailUserID] = useState<string>();

  const params = useMemo(
    () => ({
      report_date: reportDate,
      page: String(page),
      page_size: "20",
      trend_days: String(trendDays),
      ...(outcomeStatus ? { outcome_status: outcomeStatus } : {}),
      ...(changeBand ? { change_band: changeBand } : {}),
      ...(generationStatus ? { generation_status: generationStatus } : {}),
      ...(departmentID ? { department_id: departmentID } : {}),
      ...(teamID ? { team_id: teamID } : {}),
      ...(variantHash ? { variant_hash: variantHash } : {}),
      ...(regenerated ? { regenerated } : {}),
      ...(summaryOutcome ? { summary_outcome: summaryOutcome } : {}),
      ...(missingData ? { missing: missingData } : {})
    }),
    [
      changeBand,
      departmentID,
      generationStatus,
      outcomeStatus,
      page,
      regenerated,
      reportDate,
      missingData,
      summaryOutcome,
      teamID,
      trendDays,
      variantHash
    ]
  );
  const departments = useQuery({ queryKey: ["departments"], queryFn: fetchDepartments });
  const teams = useQuery({ queryKey: ["teams"], queryFn: fetchTeams });
  const observation = useQuery({
    queryKey: ["daily-report-value", params],
    queryFn: () => fetchDailyReportValue(params)
  });
  const detail = useQuery({
    queryKey: ["daily-report-value-detail", detailUserID, reportDate],
    queryFn: () => fetchDailyReportValueDetail(detailUserID ?? "", reportDate),
    enabled: Boolean(detailUserID)
  });

  const data = observation.data;
  const metrics = data?.metrics;
  const participantCount = data?.total ?? 0;
  const aiDraftRate = metrics ? ratioValue(metrics.ai_reports, participantCount) : undefined;
  const priorTrendPoint = useMemo(() => {
    const points =
      data?.trend.filter(
        (item) =>
          item.report_date < reportDate &&
          (item.metrics.total_reports > 0 || item.metrics.total_runs > 0)
      ) ?? [];
    return points[points.length - 1];
  }, [data?.trend, reportDate]);
  const populatedTrend = useMemo(
    () =>
      data?.trend.filter((item) => item.metrics.total_reports > 0 || item.metrics.total_runs > 0) ??
      [],
    [data?.trend]
  );
  const sortedItems = useMemo(() => {
    const attentionScore = (item: DailyReportValueUserDay) => {
      if (item.successful_run_count === 0) return 0;
      const band = rowDiff(item)?.text.change_band;
      if (band === "heavy") return 1;
      if (band === "medium") return 2;
      if (
        item.outcome_status === "observed_unchanged" ||
        item.outcome_status === "no_explicit_outcome"
      ) {
        return 3;
      }
      if (band === "light") return 4;
      if (band === "unchanged") return 5;
      return 6;
    };
    return [...(data?.items ?? [])].sort(
      (left, right) => attentionScore(left) - attentionScore(right)
    );
  }, [data?.items]);
  const trendOption = useMemo<EChartsOption>(() => {
    const series = [
      {
        name: "AI 覆盖",
        value: (metrics: DailyReportValueMetrics) => metrics.ai_report_coverage.value
      },
      {
        name: "生成成功",
        value: (metrics: DailyReportValueMetrics) => metrics.generation_success.value
      },
      {
        name: "确认直接使用",
        value: (metrics: DailyReportValueMetrics) => metrics.confirmed_direct_use.value
      },
      {
        name: "轻度及以下",
        value: (metrics: DailyReportValueMetrics) => metrics.light_or_less.value
      },
      {
        name: "显著修改",
        value: (metrics: DailyReportValueMetrics) => metrics.significant_modification.value
      },
      {
        name: "Draft 保留 P50",
        value: (metrics: DailyReportValueMetrics) => metrics.draft_retention_p50
      },
      {
        name: "工作概览删除",
        value: (metrics: DailyReportValueMetrics) => metrics.summary_removed.value
      },
      { name: "重新生成", value: (metrics: DailyReportValueMetrics) => metrics.regeneration.value },
      {
        name: "下游采用",
        value: (metrics: DailyReportValueMetrics) => metrics.downstream_reuse.value
      },
      { name: "生成后删除", value: (metrics: DailyReportValueMetrics) => metrics.deletion.value }
    ];
    return {
      tooltip: { trigger: "axis" },
      legend: { type: "scroll", data: series.map((item) => item.name) },
      grid: { left: 44, right: 22, top: 44, bottom: 32 },
      xAxis: { type: "category", data: populatedTrend.map((item) => item.report_date.slice(5)) },
      yAxis: { type: "value", min: 0, max: 100, axisLabel: { formatter: "{value}%" } },
      series: series.map((item) => ({
        name: item.name,
        type: "line",
        connectNulls: false,
        data: populatedTrend.map((point) => {
          const value = item.value(point.metrics);
          return value === undefined ? null : Number((value * 100).toFixed(1));
        })
      }))
    };
  }, [populatedTrend]);
  const distributionOption = useMemo<EChartsOption>(
    () => ({
      tooltip: { trigger: "axis", axisPointer: { type: "shadow" } },
      grid: { left: 72, right: 20, top: 18, bottom: 30 },
      xAxis: { type: "value", minInterval: 1 },
      yAxis: {
        type: "category",
        data: ["无可比结果", "重度", "中度", "轻度", "未修改"]
      },
      series: [
        {
          type: "bar",
          data: ["not_comparable", "heavy", "medium", "light", "unchanged"].map(
            (key) => data?.change_bands[key] ?? 0
          )
        }
      ]
    }),
    [data?.change_bands]
  );

  const columns: TableProps<DailyReportValueUserDay>["columns"] = [
    {
      title: "用户",
      key: "user",
      render: (_, row) => (
        <Space direction="vertical" size={0}>
          <Typography.Text>{row.user_name}</Typography.Text>
          <Typography.Text type="secondary" className="daily-value__secondary">
            {[row.department_name, row.team_name].filter(Boolean).join(" / ") || "未归属组织"}
          </Typography.Text>
        </Space>
      )
    },
    {
      title: "AI 成稿",
      key: "runs",
      width: 120,
      render: (_, row) => (
        <Space direction="vertical" size={0}>
          <Tag color={row.successful_run_count > 0 ? "green" : "red"}>
            {row.successful_run_count > 0 ? "已成稿" : "生成失败"}
          </Tag>
          <Typography.Text type="secondary" className="daily-value__secondary">
            Run {row.successful_run_count}/{row.run_count}
          </Typography.Text>
        </Space>
      )
    },
    {
      title: "用户结果",
      dataIndex: "outcome_status",
      width: 130,
      render: (value: string) => <Tag>{outcomeLabels[value] ?? value}</Tag>
    },
    {
      title: "修改程度",
      width: 130,
      render: (_, row) => {
        const band = rowDiff(row)?.text.change_band ?? "not_applicable";
        const color =
          band === "heavy"
            ? "red"
            : band === "medium"
              ? "orange"
              : band === "unchanged"
                ? "green"
                : undefined;
        return <Tag color={color}>{changeBandLabels[band] ?? band}</Tag>;
      }
    },
    {
      title: "编辑工作量",
      width: 150,
      render: (_, row) => (
        <Space direction="vertical" size={0}>
          <Typography.Text>修改 {percent(rowDiff(row)?.text.text_diff_ratio)}</Typography.Text>
          <Typography.Text type="secondary" className="daily-value__secondary">
            Draft 保留 {percent(rowDiff(row)?.text.draft_retention_rate)}
          </Typography.Text>
        </Space>
      )
    },
    {
      title: "信号",
      width: 140,
      render: (_, row) => (
        <Space wrap>
          {row.regenerated ? <Tag color="orange">重新生成</Tag> : null}
          {row.downstream_reuse ? <Tag color="green">下游采用</Tag> : null}
          {row.missing_reason ? <Tag color="red">数据缺失</Tag> : null}
        </Space>
      )
    },
    {
      title: "操作",
      fixed: "right",
      width: 100,
      render: (_, row) => (
        <Button type="link" icon={<EyeOutlined />} onClick={() => setDetailUserID(row.user_id)}>
          对比
        </Button>
      )
    }
  ];

  const currentRun = detail.data?.item.runs?.find(
    (run) => run.run_id === detail.data?.item.current_run_id
  );
  const generatedContent = currentRun?.snapshot?.generated_content ?? "";
  const userContent =
    currentRun?.current_outcome?.content ?? detail.data?.item.current_content ?? "";

  return (
    <PagePanel
      title="生产日报价值观察"
      description="按报告日期观察 AI 日报的生成稳定性、用户修改工作量与内容保留"
      breadcrumbs={[{ title: "生产日报价值观察" }]}
      showNav={false}
      className="daily-value"
    >
      <Flex justify="space-between" align="center" wrap gap={12}>
        <Space wrap>
          <DatePicker
            allowClear={false}
            value={dayjs(reportDate)}
            onChange={(value) => {
              if (!value) return;
              setPage(1);
              setReportDate(value.format("YYYY-MM-DD"));
            }}
          />
          <Select
            allowClear
            showSearch
            optionFilterProp="label"
            placeholder="部门"
            value={departmentID}
            onChange={(value) => {
              setPage(1);
              setDepartmentID(value);
              setTeamID(undefined);
            }}
            options={(departments.data ?? []).map((item) => ({ value: item.id, label: item.name }))}
          />
          <Select
            allowClear
            showSearch
            optionFilterProp="label"
            placeholder="小组"
            value={teamID}
            onChange={(value) => {
              setPage(1);
              setTeamID(value);
            }}
            options={(teams.data ?? [])
              .filter(
                (team) =>
                  !departmentID ||
                  departments.data
                    ?.find((department) => department.id === departmentID)
                    ?.team_ids.includes(team.id)
              )
              .map((item) => ({ value: item.id, label: item.name }))}
          />
          <Button onClick={() => setAdvancedFiltersOpen((value) => !value)}>
            {advancedFiltersOpen ? "收起筛选" : "更多筛选"}
          </Button>
        </Space>
        <Button
          icon={<DownloadOutlined />}
          onClick={() => {
            void downloadDailyReportValue(params).catch(() => message.error("导出失败"));
          }}
        >
          导出观察快照
        </Button>
      </Flex>

      {advancedFiltersOpen ? (
        <Card size="small" className="daily-value__advanced-filters">
          <Space wrap>
            <Select
              allowClear
              showSearch
              placeholder="Variant"
              value={variantHash}
              onChange={(value) => {
                setPage(1);
                setVariantHash(value);
              }}
              options={(data?.variants ?? []).map((value) => ({
                value,
                label: value.slice(0, 12)
              }))}
            />
            <Select
              allowClear
              placeholder="用户结果"
              value={outcomeStatus}
              onChange={(value) => {
                setPage(1);
                setOutcomeStatus(value);
              }}
              options={Object.entries(outcomeLabels).map(([value, label]) => ({ value, label }))}
            />
            <Select
              allowClear
              placeholder="修改程度"
              value={changeBand}
              onChange={(value) => {
                setPage(1);
                setChangeBand(value);
              }}
              options={[
                { value: "unchanged", label: "未修改" },
                { value: "light", label: "轻度" },
                { value: "medium", label: "中度" },
                { value: "heavy", label: "重度" }
              ]}
            />
            <Select
              allowClear
              placeholder="工作概览结果"
              value={summaryOutcome}
              onChange={(value) => {
                setPage(1);
                setSummaryOutcome(value);
              }}
              options={Object.entries(summaryLabels)
                .filter(([value]) => value !== "summary_reduced_30")
                .map(([value, label]) => ({ value, label }))}
            />
            <Select
              allowClear
              placeholder="数据完整性"
              value={missingData}
              onChange={(value) => {
                setPage(1);
                setMissingData(value);
              }}
              options={[
                { value: "true", label: "数据缺失" },
                { value: "false", label: "数据完整" }
              ]}
            />
            <Select
              allowClear
              placeholder="是否重新生成"
              value={regenerated}
              onChange={(value) => {
                setPage(1);
                setRegenerated(value);
              }}
              options={[
                { value: "true", label: "已重新生成" },
                { value: "false", label: "未重新生成" }
              ]}
            />
            <Select
              allowClear
              placeholder="生成状态"
              value={generationStatus}
              onChange={(value) => {
                setPage(1);
                setGenerationStatus(value);
              }}
              options={[
                { value: "succeeded", label: "全部成功" },
                { value: "partial", label: "部分失败" },
                { value: "failed", label: "全部失败" }
              ]}
            />
          </Space>
        </Card>
      ) : null}

      {data ? (
        <Alert
          type={data.data_completeness === "complete" ? "success" : "warning"}
          showIcon
          message={`数据截止 ${dayjs(data.observed_at).format("YYYY-MM-DD HH:mm:ss")} · ${
            data.data_completeness === "complete"
              ? "采集完整"
              : `${data.missing_count} 条历史数据未采集`
          }`}
        />
      ) : null}

      {metrics ? (
        <>
          <Card className="daily-value__executive-summary">
            <Flex justify="space-between" align="flex-start" gap={24} wrap>
              <div className="daily-value__executive-copy">
                <Space size={8}>
                  {metrics.comparable_outcomes > 0 &&
                  metrics.light_or_less.numerator * 2 > metrics.comparable_outcomes ? (
                    <CheckCircleOutlined className="daily-value__executive-icon is-positive" />
                  ) : (
                    <ExclamationCircleOutlined className="daily-value__executive-icon is-attention" />
                  )}
                  <Typography.Text className="daily-value__eyebrow">管理结论</Typography.Text>
                </Space>
                <Typography.Title level={3}>
                  {metrics.comparable_outcomes > 0
                    ? `${metrics.light_or_less.numerator}/${metrics.comparable_outcomes} 份明确结果可直接使用或仅需轻度修改`
                    : "当前缺少明确用户结果，暂不能评价 AI 日报可用性"}
                </Typography.Title>
                <Typography.Paragraph>
                  {executiveConclusion(reportDate, metrics, participantCount)}
                </Typography.Paragraph>
              </div>
              <Space wrap className="daily-value__evidence-tags">
                <Tag>观察用户 {participantCount}</Tag>
                <Tag color="blue">明确保存结果 {metrics.comparable_outcomes}</Tag>
                <Tag color={participantCount > metrics.ai_reports ? "red" : "green"}>
                  未形成 AI 成稿 {Math.max(participantCount - metrics.ai_reports, 0)}
                </Tag>
                <Tag color="orange">重新生成 {metrics.regeneration.numerator}</Tag>
              </Space>
            </Flex>
            <Typography.Text type="secondary">
              本页衡量 AI 成稿覆盖与用户修改工作量，不把“未修改”直接等同于内容事实完全正确。
            </Typography.Text>
          </Card>

          <Row gutter={[16, 16]}>
            <Col xs={24} lg={8}>
              <ExecutiveMetric
                title="AI 成稿用户"
                value={aiDraftRate}
                numerator={metrics.ai_reports}
                denominator={participantCount}
                detail={`按用户观察；Run 成功 ${metrics.successful_runs}/${metrics.total_runs}`}
                tone="neutral"
              />
            </Col>
            <Col xs={24} lg={8}>
              <ExecutiveMetric
                title="直接使用或轻改"
                value={metrics.light_or_less.value}
                numerator={metrics.light_or_less.numerator}
                denominator={metrics.light_or_less.denominator}
                detail={`其中 ${metrics.confirmed_direct_use.numerator} 份确认原样使用`}
                tone="positive"
              />
            </Col>
            <Col xs={24} lg={8}>
              <ExecutiveMetric
                title="需要显著修改"
                value={metrics.significant_modification.value}
                numerator={metrics.significant_modification.numerator}
                denominator={metrics.significant_modification.denominator}
                detail={`${metrics.significant_modification.numerator} 份需要中度或重度修改`}
                tone="attention"
              />
            </Col>
          </Row>

          <Card
            title="效果变化"
            extra={
              priorTrendPoint ? (
                <Typography.Text type="secondary">
                  相较上一有数据日期 {priorTrendPoint.report_date}
                </Typography.Text>
              ) : null
            }
          >
            {priorTrendPoint ? (
              <Row gutter={[24, 20]}>
                <Col xs={24} md={8}>
                  <ComparisonMetric
                    title="Run 生成成功"
                    current={metrics.generation_success.value}
                    previous={priorTrendPoint.metrics.generation_success.value}
                  />
                </Col>
                <Col xs={24} md={8}>
                  <ComparisonMetric
                    title="直接使用或轻改"
                    current={metrics.light_or_less.value}
                    previous={priorTrendPoint.metrics.light_or_less.value}
                  />
                </Col>
                <Col xs={24} md={8}>
                  <ComparisonMetric
                    title="显著修改"
                    current={metrics.significant_modification.value}
                    previous={priorTrendPoint.metrics.significant_modification.value}
                  />
                </Col>
              </Row>
            ) : (
              <Alert
                type="info"
                showIcon
                message="当前日期作为效果比较基线"
                description={`近 ${trendDays} 日没有更早的可比数据；后续版本或日期产生数据后，将在这里直接展示变化量。`}
              />
            )}
          </Card>
        </>
      ) : null}

      <Card
        title="用户结果与重点问题"
        extra={
          <Typography.Text type="secondary">优先展示生成失败和修改工作量较高的用户</Typography.Text>
        }
      >
        {metrics ? (
          <div className="daily-value__attention-strip">
            <div>
              <strong>{Math.max(participantCount - metrics.ai_reports, 0)}</strong>
              <span>未形成 AI 成稿</span>
            </div>
            <div>
              <strong>{metrics.significant_modification.numerator}</strong>
              <span>需要显著修改</span>
            </div>
            <div>
              <strong>{metrics.observed_unchanged.denominator}</strong>
              <span>暂无明确用户操作</span>
            </div>
            <div>
              <strong>{metrics.regeneration.numerator}</strong>
              <span>发生重新生成</span>
            </div>
          </div>
        ) : null}
        <Table<DailyReportValueUserDay>
          rowKey="user_id"
          loading={observation.isLoading}
          columns={columns}
          dataSource={sortedItems}
          scroll={{ x: 980 }}
          pagination={{
            current: data?.page ?? page,
            pageSize: data?.page_size ?? 20,
            total: data?.total ?? 0,
            showSizeChanger: false,
            onChange: setPage
          }}
        />
      </Card>

      <Collapse
        className="daily-value__diagnostics"
        items={[
          {
            key: "diagnostics",
            label: (
              <Space>
                <LineChartOutlined />
                研发诊断与运行指标
                <Typography.Text type="secondary">耗时、趋势、内容结构和运行信号</Typography.Text>
              </Space>
            ),
            children: (
              <Space direction="vertical" size="large" className="daily-value__diagnostic-content">
                {metrics ? (
                  <Row gutter={[12, 12]}>
                    <Col xs={24} sm={12} xl={6}>
                      <MetricCard
                        title="Run 生成成功"
                        ratio={metrics.generation_success}
                        note={`平均 ${durationText(metrics.average_duration_ms)} / P95 ${durationText(metrics.p95_duration_ms)}`}
                      />
                    </Col>
                    <Col xs={24} sm={12} xl={6}>
                      <MetricCard title="重新生成" ratio={metrics.regeneration} />
                    </Col>
                    <Col xs={24} sm={12} xl={6}>
                      <MetricCard title="下游采用" ratio={metrics.downstream_reuse} />
                    </Col>
                    <Col xs={24} sm={12} xl={6}>
                      <MetricCard title="生成后删除" ratio={metrics.deletion} />
                    </Col>
                  </Row>
                ) : null}
                <Row gutter={[16, 16]}>
                  <Col xs={24} xl={9}>
                    <Card title="修改程度分布">
                      <BaseEChart
                        height={280}
                        option={distributionOption}
                        loading={observation.isLoading}
                        empty={!data?.total}
                      />
                    </Card>
                  </Col>
                  <Col xs={24} xl={15}>
                    <Card
                      title={`近 ${trendDays} 日趋势`}
                      extra={
                        <Segmented<14 | 30>
                          size="small"
                          value={trendDays}
                          options={[
                            { label: "14 日", value: 14 },
                            { label: "30 日", value: 30 }
                          ]}
                          onChange={setTrendDays}
                        />
                      }
                    >
                      {populatedTrend.length >= 2 ? (
                        <BaseEChart
                          height={280}
                          option={trendOption}
                          loading={observation.isLoading}
                          empty={false}
                        />
                      ) : (
                        <Empty
                          image={Empty.PRESENTED_IMAGE_SIMPLE}
                          description="至少需要两个有数据日期才能形成趋势。"
                        />
                      )}
                    </Card>
                  </Col>
                </Row>
                {data ? (
                  <Card title="工作概览结果">
                    <Space wrap size="large">
                      {Object.entries(summaryLabels).map(([key, label]) => (
                        <Statistic
                          key={key}
                          title={label}
                          value={data.summary_outcomes[key] ?? 0}
                        />
                      ))}
                    </Space>
                  </Card>
                ) : null}
              </Space>
            )
          }
        ]}
      />

      <Drawer
        width="min(1100px, 94vw)"
        title={`${detail.data?.item.user_name ?? "用户"} · ${reportDate}`}
        open={Boolean(detailUserID)}
        onClose={() => setDetailUserID(undefined)}
        loading={detail.isLoading}
      >
        {detail.data ? (
          <Space direction="vertical" size="large" className="daily-value__drawer-content">
            <Descriptions
              size="small"
              column={3}
              items={[
                {
                  key: "outcome",
                  label: "用户结果",
                  children:
                    outcomeLabels[detail.data.item.outcome_status] ??
                    detail.data.item.outcome_status
                },
                { key: "runs", label: "生成次数", children: detail.data.item.run_count },
                {
                  key: "variant",
                  label: "Variant",
                  children: detail.data.item.variant_hash?.slice(0, 12) ?? "—"
                },
                {
                  key: "diff",
                  label: "修改比例",
                  children: percent(rowDiff(detail.data.item)?.text.text_diff_ratio)
                },
                {
                  key: "retention",
                  label: "Draft 保留",
                  children: percent(rowDiff(detail.data.item)?.text.draft_retention_rate)
                },
                {
                  key: "summary",
                  label: "工作概览",
                  children:
                    summaryLabels[rowDiff(detail.data.item)?.summary.outcome ?? "not_applicable"]
                }
              ]}
            />
            {generatedContent ? (
              <DiffComparison generated={generatedContent} user={userContent} />
            ) : (
              <Space direction="vertical" size="middle" className="daily-value__current-only">
                <Alert
                  type="info"
                  showIcon
                  message="历史 Generated Draft 未采集，以下展示当前日报内容"
                />
                <Card size="small" title="当前日报内容">
                  <div className="daily-value__diff-pane">
                    <div className="daily-value__diff-line">
                      {userContent || "当前日报内容为空"}
                    </div>
                  </div>
                </Card>
              </Space>
            )}
            <Card size="small" title="Run 时间线">
              <Table
                rowKey="run_id"
                size="small"
                pagination={false}
                dataSource={detail.data.item.runs ?? []}
                columns={[
                  {
                    title: "时间",
                    dataIndex: "created_at",
                    render: (value: string) => dayjs(value).format("MM-DD HH:mm:ss")
                  },
                  {
                    title: "Run",
                    dataIndex: "run_id",
                    render: (value: string) => value.slice(0, 12)
                  },
                  { title: "状态", dataIndex: "status" },
                  {
                    title: "用户动作",
                    dataIndex: "current_outcome",
                    render: (value?: { action: string }) => value?.action ?? "—"
                  },
                  {
                    title: "阶段",
                    dataIndex: "failure_stage",
                    render: (value?: string) => value || "—"
                  },
                  {
                    title: "模型",
                    dataIndex: "model_id",
                    render: (value?: string) => value || "—"
                  },
                  { title: "来源 Session", dataIndex: "source_session_count" },
                  {
                    title: "耗时",
                    dataIndex: "duration_ms",
                    render: (value?: number) =>
                      value === undefined ? "—" : `${(value / 1000).toFixed(1)}s`
                  }
                ]}
              />
            </Card>
          </Space>
        ) : null}
      </Drawer>
    </PagePanel>
  );
}
