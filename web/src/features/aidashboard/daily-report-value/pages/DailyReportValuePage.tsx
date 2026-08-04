import { DownloadOutlined, EyeOutlined } from "@ant-design/icons";
import { useQuery } from "@tanstack/react-query";
import {
  Button,
  Card,
  DatePicker,
  Descriptions,
  Drawer,
  Flex,
  message,
  Select,
  Space,
  Table,
  Tag,
  Typography
} from "antd";
import type { TableProps } from "antd";
import dayjs from "dayjs";
import { useMemo, useState } from "react";

import {
  downloadDailyReportValue,
  fetchDailyReportValue,
  fetchDailyReportValueDetail,
  fetchDepartments,
  fetchTeams
} from "../../api/client";
import type { DailyReportValueUserDay } from "../../api/types";
import { PagePanel } from "@/shared/components/PagePanel/PagePanel";

import "./DailyReportValuePage.css";

type DiffLine = { left?: string; right?: string; kind: "same" | "removed" | "added" | "changed" };

function rowDiff(row: DailyReportValueUserDay) {
  return row.diff ?? row.observed_diff;
}

function reportModeLabel(mode: DailyReportValueUserDay["report_mode"]) {
  if (mode === "ai_generated") return "AI生成";
  if (mode === "handwritten") return "手工填写";
  return "尚无日报";
}

function reportModeColor(mode: DailyReportValueUserDay["report_mode"]) {
  if (mode === "ai_generated") return "blue";
  if (mode === "handwritten") return "default";
  return "orange";
}

function contentChange(row: DailyReportValueUserDay) {
  if (row.report_mode !== "ai_generated") return { label: "—", rank: 4 };
  const band = rowDiff(row)?.text.change_band;
  if (band === "unchanged") return { label: "未修改", rank: 2 };
  if (band === "light") return { label: "修改较少", color: "blue", rank: 1 };
  if (band === "medium" || band === "heavy") {
    return { label: "修改较多", color: "orange", rank: 0 };
  }
  return { label: "—", rank: 3 };
}

function percentage(value: number, total: number) {
  return total > 0 ? `${(value / total) * 100}%` : "0%";
}

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

function DiffComparison({ generated, current }: { generated: string; current: string }) {
  const rows = useMemo(() => alignDiffLines(generated, current), [generated, current]);
  return (
    <div className="daily-value__comparison">
      <Card size="small" title="AI生成内容">
        <div className="daily-value__diff-pane">
          {rows.map((row, index) => (
            <div key={`${index}-${row.kind}`} className={`daily-value__diff-line is-${row.kind}`}>
              {row.left ?? " "}
            </div>
          ))}
        </div>
      </Card>
      <Card size="small" title="当前日报内容">
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

export function DailyReportValuePage() {
  const [reportDate, setReportDate] = useState(dayjs().subtract(1, "day").format("YYYY-MM-DD"));
  const [departmentID, setDepartmentID] = useState<string>();
  const [teamID, setTeamID] = useState<string>();
  const [page, setPage] = useState(1);
  const [detailUserID, setDetailUserID] = useState<string>();

  const params = useMemo(
    () => ({
      report_date: reportDate,
      page: String(page),
      page_size: "20",
      trend_days: "0",
      ...(departmentID ? { department_id: departmentID } : {}),
      ...(teamID ? { team_id: teamID } : {})
    }),
    [departmentID, page, reportDate, teamID]
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
  const changedCount = (metrics?.content_light ?? 0) + (metrics?.content_significant ?? 0);
  const reportDateLabel = dayjs(reportDate).format("M月D日");
  const summaryText = `${reportDateLabel}，${metrics?.total_reports ?? 0}人填写日报，${metrics?.ai_reports ?? 0}人使用AI生成；其中${metrics?.content_unchanged ?? 0}份未修改，${changedCount}份有调整。`;
  const sortedItems = useMemo(
    () =>
      [...(data?.items ?? [])].sort((left, right) => {
        const rankDifference = contentChange(left).rank - contentChange(right).rank;
        if (rankDifference !== 0) return rankDifference;
        return left.user_name.localeCompare(right.user_name, "zh-CN");
      }),
    [data?.items]
  );

  const columns: TableProps<DailyReportValueUserDay>["columns"] = [
    {
      title: "员工",
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
      title: "日报填写方式",
      dataIndex: "report_mode",
      width: 150,
      render: (value: DailyReportValueUserDay["report_mode"]) => (
        <Tag color={reportModeColor(value)}>{reportModeLabel(value)}</Tag>
      )
    },
    {
      title: "AI生成后修改情况",
      width: 180,
      render: (_, row) => {
        const change = contentChange(row);
        return <Tag color={change.color}>{change.label}</Tag>;
      }
    },
    {
      title: "操作",
      width: 120,
      render: (_, row) =>
        row.report_id ? (
          <Button type="link" icon={<EyeOutlined />} onClick={() => setDetailUserID(row.user_id)}>
            查看对比
          </Button>
        ) : (
          <Typography.Text type="secondary">—</Typography.Text>
        )
    }
  ];

  const detailItem = detail.data?.item;
  const currentRun = detailItem?.runs?.find((run) => run.run_id === detailItem.current_run_id);
  const generatedContent = currentRun?.snapshot?.generated_content ?? "";
  const currentContent = currentRun?.current_outcome?.content ?? detailItem?.current_content ?? "";

  return (
    <PagePanel
      title="AI日报当日汇总"
      description="汇总当天的日报填写方式和AI生成后的内容变化"
      breadcrumbs={[{ title: "AI日报当日汇总" }]}
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
        </Space>
        <Button
          icon={<DownloadOutlined />}
          onClick={() => {
            void downloadDailyReportValue(params).catch(() => message.error("导出失败"));
          }}
        >
          导出当日数据
        </Button>
      </Flex>

      <Card className="daily-value__summary-card" loading={observation.isLoading}>
        <Typography.Text type="secondary">
          {dayjs(reportDate).format("YYYY年MM月DD日")}
        </Typography.Text>
        <Typography.Title level={3}>{summaryText}</Typography.Title>
        <div className="daily-value__summary-panels">
          <section className="daily-value__summary-panel">
            <div className="daily-value__panel-heading">
              <Typography.Text type="secondary">日报填写方式</Typography.Text>
              <strong>
                {metrics?.total_reports ?? 0}
                <small>人</small>
              </strong>
            </div>
            <div className="daily-value__distribution-bar" aria-label="日报填写方式分布">
              <span
                className="daily-value__bar-segment is-ai"
                style={{ width: percentage(metrics?.ai_reports ?? 0, metrics?.total_reports ?? 0) }}
              />
              <span
                className="daily-value__bar-segment is-handwritten"
                style={{
                  width: percentage(metrics?.handwritten_reports ?? 0, metrics?.total_reports ?? 0)
                }}
              />
            </div>
            <div className="daily-value__legend">
              <div>
                <span className="daily-value__legend-dot is-ai" />
                <span>AI生成</span>
                <strong>{metrics?.ai_reports ?? 0}人</strong>
              </div>
              <div>
                <span className="daily-value__legend-dot is-handwritten" />
                <span>手工填写</span>
                <strong>{metrics?.handwritten_reports ?? 0}人</strong>
              </div>
            </div>
          </section>

          <section className="daily-value__summary-panel">
            <div className="daily-value__panel-heading">
              <Typography.Text type="secondary">AI日报修改情况</Typography.Text>
              <strong>
                {metrics?.ai_reports ?? 0}
                <small>份</small>
              </strong>
            </div>
            <div className="daily-value__distribution-bar" aria-label="AI生成后的内容变化分布">
              <span
                className="daily-value__bar-segment is-unchanged"
                style={{
                  width: percentage(metrics?.content_unchanged ?? 0, metrics?.ai_reports ?? 0)
                }}
              />
              <span
                className="daily-value__bar-segment is-light"
                style={{ width: percentage(metrics?.content_light ?? 0, metrics?.ai_reports ?? 0) }}
              />
              <span
                className="daily-value__bar-segment is-significant"
                style={{
                  width: percentage(metrics?.content_significant ?? 0, metrics?.ai_reports ?? 0)
                }}
              />
            </div>
            <div className="daily-value__legend is-content-change">
              <div>
                <span className="daily-value__legend-dot is-unchanged" />
                <span>未修改</span>
                <strong>{metrics?.content_unchanged ?? 0}份</strong>
              </div>
              <div>
                <span className="daily-value__legend-dot is-light" />
                <span>修改较少</span>
                <strong>{metrics?.content_light ?? 0}份</strong>
              </div>
              <div>
                <span className="daily-value__legend-dot is-significant" />
                <span>修改较多</span>
                <strong>{metrics?.content_significant ?? 0}份</strong>
              </div>
            </div>
          </section>
        </div>
      </Card>

      <Card
        title="当日日报明细"
        extra={<Typography.Text type="secondary">修改较多的AI日报优先展示</Typography.Text>}
      >
        <Table<DailyReportValueUserDay>
          rowKey="user_id"
          loading={observation.isLoading}
          columns={columns}
          dataSource={sortedItems}
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
        title={`${detailItem?.user_name ?? "员工"} · ${reportDate} · 内容对比`}
        open={Boolean(detailUserID)}
        onClose={() => setDetailUserID(undefined)}
        loading={detail.isLoading}
      >
        {detailItem ? (
          <Space direction="vertical" size="large" className="daily-value__drawer-content">
            <Descriptions
              size="small"
              column={2}
              items={[
                {
                  key: "mode",
                  label: "日报填写方式",
                  children: reportModeLabel(detailItem.report_mode)
                },
                {
                  key: "change",
                  label: "AI生成后修改情况",
                  children: contentChange(detailItem).label
                }
              ]}
            />
            {generatedContent ? (
              <DiffComparison generated={generatedContent} current={currentContent} />
            ) : (
              <Card size="small" title="当前日报内容">
                <div className="daily-value__diff-pane">
                  <div className="daily-value__diff-line">
                    {currentContent || "当前日报内容为空"}
                  </div>
                </div>
              </Card>
            )}
          </Space>
        ) : null}
      </Drawer>
    </PagePanel>
  );
}
