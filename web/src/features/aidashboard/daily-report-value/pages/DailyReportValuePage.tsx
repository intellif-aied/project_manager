import { DownloadOutlined, EyeOutlined } from "@ant-design/icons";
import { useQuery } from "@tanstack/react-query";
import {
  Alert,
  Button,
  Card,
  Col,
  DatePicker,
  Descriptions,
  Drawer,
  Flex,
  message,
  Row,
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
  summary_unchanged: "总结未改",
  summary_modified: "总结已改",
  summary_removed: "总结已删除",
  summary_reduced_30: "总结缩短 ≥30%",
  not_applicable: "不可比较"
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

function reportSummary(date: string, metrics: DailyReportValueMetrics) {
  return `${date}共有 ${metrics.total_reports} 份个人日报，其中 ${metrics.ai_reports} 份由 AI 生成，覆盖率 ${percent(metrics.ai_report_coverage.value)}。AI Report Run 成功 ${metrics.successful_runs}/${metrics.total_runs}。在 ${metrics.comparable_outcomes} 份有明确用户结果的日报中，${metrics.confirmed_direct_use.numerator} 份未修改直接使用，${metrics.light_or_less.numerator} 份修改不超过轻度；Draft 内容中位保留率为 ${percent(metrics.draft_retention_p50)}。有 ${metrics.significant_modification.numerator} 份发生显著修改，${metrics.summary_removed.numerator} 份删除“工作总结”，${metrics.regeneration.numerator} 个用户日发生重新生成。`;
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
  const [page, setPage] = useState(1);
  const [detailUserID, setDetailUserID] = useState<string>();

  const params = useMemo(
    () => ({
      report_date: reportDate,
      page: String(page),
      page_size: "20",
      trend_days: "14",
      ...(outcomeStatus ? { outcome_status: outcomeStatus } : {}),
      ...(changeBand ? { change_band: changeBand } : {}),
      ...(generationStatus ? { generation_status: generationStatus } : {}),
      ...(departmentID ? { department_id: departmentID } : {}),
      ...(teamID ? { team_id: teamID } : {}),
      ...(variantHash ? { variant_hash: variantHash } : {}),
      ...(regenerated ? { regenerated } : {})
    }),
    [
      changeBand,
      departmentID,
      generationStatus,
      outcomeStatus,
      page,
      regenerated,
      reportDate,
      teamID,
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
  const trendOption = useMemo<EChartsOption>(
    () => ({
      tooltip: { trigger: "axis" },
      legend: { data: ["AI 覆盖", "生成成功", "确认直接使用", "轻度及以下"] },
      grid: { left: 44, right: 22, top: 44, bottom: 32 },
      xAxis: { type: "category", data: data?.trend.map((item) => item.report_date.slice(5)) ?? [] },
      yAxis: { type: "value", min: 0, max: 100, axisLabel: { formatter: "{value}%" } },
      series: [
        ["AI 覆盖", "ai_report_coverage"],
        ["生成成功", "generation_success"],
        ["确认直接使用", "confirmed_direct_use"],
        ["轻度及以下", "light_or_less"]
      ].map(([name, key]) => ({
        name,
        type: "line",
        connectNulls: false,
        data: data?.trend.map((item) => {
          const ratio = item.metrics[key as keyof DailyReportValueMetrics] as DailyReportValueRatio;
          return ratio.value === undefined ? null : Number((ratio.value * 100).toFixed(1));
        })
      }))
    }),
    [data?.trend]
  );
  const distributionOption = useMemo<EChartsOption>(
    () => ({
      tooltip: { trigger: "axis", axisPointer: { type: "shadow" } },
      grid: { left: 72, right: 20, top: 18, bottom: 30 },
      xAxis: { type: "value", minInterval: 1 },
      yAxis: {
        type: "category",
        data: ["不可比较", "重度", "中度", "轻度", "未修改"]
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
      title: "生成",
      key: "runs",
      width: 100,
      render: (_, row) => `${row.successful_run_count}/${row.run_count}`
    },
    {
      title: "用户结果",
      dataIndex: "outcome_status",
      width: 130,
      render: (value: string) => <Tag>{outcomeLabels[value] ?? value}</Tag>
    },
    {
      title: "修改比例",
      width: 110,
      render: (_, row) => percent(rowDiff(row)?.text.text_diff_ratio)
    },
    {
      title: "Draft 保留",
      width: 110,
      render: (_, row) => percent(rowDiff(row)?.text.draft_retention_rate)
    },
    {
      title: "工作总结",
      width: 120,
      render: (_, row) => summaryLabels[row.diff?.summary.outcome ?? "not_applicable"]
    },
    {
      title: "主题变化",
      width: 110,
      render: (_, row) =>
        rowDiff(row)
          ? `-${rowDiff(row)?.topics.deleted.length} / +${rowDiff(row)?.topics.added.length}`
          : "—"
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
          <Select
            allowClear
            showSearch
            placeholder="Variant"
            value={variantHash}
            onChange={(value) => {
              setPage(1);
              setVariantHash(value);
            }}
            options={Array.from(
              new Set(
                (data?.items ?? [])
                  .map((item) => item.variant_hash)
                  .filter((value): value is string => Boolean(value))
              )
            ).map((value) => ({ value, label: value.slice(0, 12) }))}
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
        <Button
          icon={<DownloadOutlined />}
          onClick={() => {
            void downloadDailyReportValue(params).catch(() => message.error("导出失败"));
          }}
        >
          导出观察快照
        </Button>
      </Flex>

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
        <Row gutter={[12, 12]}>
          <Col xs={24} sm={12} xl={6}>
            <MetricCard title="AI 日报覆盖" ratio={metrics.ai_report_coverage} />
          </Col>
          <Col xs={24} sm={12} xl={6}>
            <MetricCard
              title="生成成功"
              ratio={metrics.generation_success}
              note={`平均 ${durationText(metrics.average_duration_ms)} / P95 ${durationText(metrics.p95_duration_ms)}`}
            />
          </Col>
          <Col xs={24} sm={12} xl={6}>
            <MetricCard title="确认直接使用" ratio={metrics.confirmed_direct_use} />
          </Col>
          <Col xs={24} sm={12} xl={6}>
            <MetricCard title="轻度及以下修改" ratio={metrics.light_or_less} />
          </Col>
          <Col xs={24} sm={12} xl={6}>
            <MetricCard title="显著修改" ratio={metrics.significant_modification} />
          </Col>
          <Col xs={24} sm={12} xl={6}>
            <MetricCard title="总结删除" ratio={metrics.summary_removed} />
          </Col>
          <Col xs={24} sm={12} xl={6}>
            <MetricCard title="重新生成" ratio={metrics.regeneration} />
          </Col>
          <Col xs={24} sm={12} xl={6}>
            <MetricCard title="下游采用" ratio={metrics.downstream_reuse} />
          </Col>
          <Col xs={24} sm={12} xl={6}>
            <MetricCard title="观察未变（无明确操作）" ratio={metrics.observed_unchanged} />
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
          <Card title="近 14 日趋势">
            <BaseEChart
              height={280}
              option={trendOption}
              loading={observation.isLoading}
              empty={!data?.trend.length}
            />
          </Card>
        </Col>
      </Row>

      {data ? (
        <Card title="工作总结结果">
          <Space wrap size="large">
            {Object.entries(summaryLabels).map(([key, label]) => (
              <Statistic key={key} title={label} value={data.summary_outcomes[key] ?? 0} />
            ))}
          </Space>
        </Card>
      ) : null}

      {metrics ? (
        <Card title="确定性汇报摘要">
          <Typography.Paragraph copyable>{reportSummary(reportDate, metrics)}</Typography.Paragraph>
          <Typography.Text type="secondary">
            行为指标表示使用和修改工作量，不代表内容事实完全正确。
          </Typography.Text>
        </Card>
      ) : null}

      <Card title="用户日明细">
        <Table<DailyReportValueUserDay>
          rowKey="user_id"
          loading={observation.isLoading}
          columns={columns}
          dataSource={data?.items ?? []}
          scroll={{ x: 1180 }}
          pagination={{
            current: data?.page ?? page,
            pageSize: data?.page_size ?? 20,
            total: data?.total ?? 0,
            showSizeChanger: false,
            onChange: setPage
          }}
        />
      </Card>

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
                  label: "工作总结",
                  children:
                    summaryLabels[rowDiff(detail.data.item)?.summary.outcome ?? "not_applicable"]
                }
              ]}
            />
            {generatedContent ? (
              <DiffComparison generated={generatedContent} user={userContent} />
            ) : (
              <Alert type="info" showIcon message="当前用户日没有可用 Generated Draft" />
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
