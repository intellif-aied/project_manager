import { EditOutlined } from "@ant-design/icons";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Alert,
  Button,
  Card,
  DatePicker,
  Empty,
  Segmented,
  Space,
  Table,
  Tag,
  Typography
} from "antd";
import type { ColumnsType } from "antd/es/table";
import type { Dayjs } from "dayjs";
import { useState } from "react";
import { useParams, useSearchParams } from "react-router-dom";
import dayjs from "dayjs";

import { useAuth } from "@/shared/auth/authContext";
import { MarkdownViewer } from "@/shared/components/MarkdownViewer/MarkdownViewer";
import { PagePanel } from "@/shared/components/PagePanel/PagePanel";

import {
  fetchDepartmentReport,
  fetchDepartmentReports,
  fetchMyReports,
  fetchReport,
  fetchTeamReport,
  fetchTeamReports
} from "../../api/client";
import { DailyReportGenerateModal, type DailyGenerateScope } from "../components/DailyReportGenerateModal";
import type {
  DailyReportListItem,
  DepartmentReportListItem,
  TeamReportListItem
} from "../../api/types";

import "./ReportsPage.css";

const { Text } = Typography;
const { RangePicker } = DatePicker;
const pageSizeOptions = [10, 20, 50, 100];

type DailyTab = "personal" | "team" | "department";

function isDailyTab(value: string | null): value is DailyTab {
  return value === "personal" || value === "team" || value === "department";
}

function dailyReportsPath(tab: DailyTab) {
  return `/reports/daily?tab=${tab}`;
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : "请稍后重试";
}

function formatDateTime(value?: string) {
  return value ? dayjs(value).format("YYYY-MM-DD HH:mm") : "-";
}

function formatDate(value?: string) {
  return value ? dayjs(value).format("YYYY-MM-DD") : "-";
}

function personalStatus(record: DailyReportListItem, role?: string) {
  if (role === "director" || role === "admin") {
    return record.status === "saved" || record.status === "submitted" ? <Tag color="blue">已保存</Tag> : <Tag>暂无报告</Tag>;
  }
  if (record.status === "submitted") return <Tag color="green">已发送</Tag>;
  if (record.status === "saved" && record.submitted_at) return <Tag color="gold">已保存，未发送最新修改</Tag>;
  if (record.status === "saved") return <Tag color="blue">已保存</Tag>;
  return <Tag>暂无报告</Tag>;
}

function teamStatus(record: TeamReportListItem) {
  if (record.status === "submitted") return <Tag color="green">已发送</Tag>;
  if (record.status === "saved" && record.submitted_at) return <Tag color="gold">已保存，未发送最新修改</Tag>;
  if (record.status === "saved") return <Tag color="blue">已保存</Tag>;
  return <Tag>暂无报告</Tag>;
}

function departmentStatus(record: DepartmentReportListItem) {
  return record.status === "saved" || record.status === "archived" || record.archived_at
    ? <Tag color="green">已保存</Tag>
    : <Tag>暂无报告</Tag>;
}

function useTablePagination() {
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  return {
    page,
    pageSize,
    tablePagination: (total: number) => ({
      current: page,
      pageSize,
      total,
      showSizeChanger: true,
      pageSizeOptions,
      showTotal: (value: number) => `共 ${value} 条记录`,
      onChange: (next: number, size: number) => {
        setPage(size && size !== pageSize ? 1 : next);
        if (size && size !== pageSize) setPageSize(size);
      }
    })
  };
}

export function DailyReportsPage() {
  return <ReportsPage />;
}

export function ReportsPage() {
  const { user } = useAuth();
  const [searchParams, setSearchParams] = useSearchParams();
  const queryClient = useQueryClient();
  const [dateRange, setDateRange] = useState<[Dayjs, Dayjs] | null>(null);
  const [generateTarget, setGenerateTarget] = useState<{
    scope: DailyGenerateScope;
    reportId?: string;
    reportDate?: string;
    readOnly?: boolean;
    allowDateSwitch?: boolean;
  } | null>(null);

  const options =
    user?.role === "director" || user?.role === "admin"
      ? [
          { label: "我的日报记录", value: "personal" },
          { label: "部门日报记录", value: "department" }
        ]
      : user?.role === "team_leader"
        ? [
            { label: "我的日报记录", value: "personal" },
            { label: "小组日报记录", value: "team" }
          ]
        : [{ label: "我的日报记录", value: "personal" }];

  const queryTab = searchParams.get("tab");
  const queryTabIsValid = isDailyTab(queryTab);
  const queryTabIsAvailable = queryTabIsValid && options.some((item) => item.value === queryTab);
  const activeTab = queryTabIsAvailable ? queryTab : "personal";
  const from = dateRange?.[0].format("YYYY-MM-DD");
  const to = dateRange?.[1].format("YYYY-MM-DD");
  const openLabel =
    activeTab === "team" ? "填写小组日报" : activeTab === "department" ? "填写部门日报" : "填写日报";
  const canOpenCurrentReport = activeTab !== "team" || user?.role === "team_leader";

  const handleTabChange = (value: DailyTab) => {
    setSearchParams((current) => {
      const next = new URLSearchParams(current);
      next.set("tab", value);
      return next;
    });
  };

  if (!user) return null;

  return (
    <PagePanel
      title="日报"
      description="按记录列表打开日报；个人、小组、部门日报统一通过弹窗查看和编辑正文。"
      breadcrumbs={[{ title: "报告" }, { title: "日报" }]}
      className="reports-page aidashboard-list"
      showNav={false}
    >
      <Card className="reports-control-card">
        <Space wrap style={{ width: "100%", justifyContent: "space-between" }}>
          <Space wrap>
            {options.length > 1 ? (
              <Segmented value={activeTab} onChange={(value) => handleTabChange(value as DailyTab)} options={options} />
            ) : null}
            <RangePicker value={dateRange} onChange={(value) => setDateRange(value as [Dayjs, Dayjs] | null)} />
          </Space>
          {canOpenCurrentReport ? (
            <Button
              type="primary"
              icon={<EditOutlined />}
              onClick={() => setGenerateTarget({ scope: activeTab, allowDateSwitch: true })}
            >
              {openLabel}
            </Button>
          ) : null}
        </Space>
      </Card>
      {activeTab === "personal" ? (
        <PersonalDailyTable
          key={`personal:${from ?? ""}:${to ?? ""}`}
          from={from}
          to={to}
          onOpen={(record) =>
            setGenerateTarget({ scope: "personal", reportId: record.id, reportDate: record.report_date, readOnly: true })
          }
          onEdit={(record) =>
            setGenerateTarget({ scope: "personal", reportId: record.id, reportDate: record.report_date })
          }
        />
      ) : null}
      {activeTab === "team" ? (
        <TeamDailyTable
          key={`team:${from ?? ""}:${to ?? ""}`}
          from={from}
          to={to}
          onOpen={(record) =>
            setGenerateTarget({ scope: "team", reportId: record.id, reportDate: record.report_date, readOnly: true })
          }
          onEdit={(record) =>
            setGenerateTarget({ scope: "team", reportId: record.id, reportDate: record.report_date })
          }
        />
      ) : null}
      {activeTab === "department" ? (
        <DepartmentDailyTable
          key={`department:${from ?? ""}:${to ?? ""}`}
          from={from}
          to={to}
          onOpen={(record) =>
            setGenerateTarget({ scope: "department", reportId: record.id, reportDate: record.report_date, readOnly: true })
          }
          onEdit={(record) =>
            setGenerateTarget({ scope: "department", reportId: record.id, reportDate: record.report_date })
          }
        />
      ) : null}
      {generateTarget ? (
        <DailyReportGenerateModal
          open
          scope={generateTarget.scope}
          reportId={generateTarget.reportId}
          reportDate={generateTarget.reportDate}
          readOnly={generateTarget.readOnly}
          allowDateSwitch={generateTarget.allowDateSwitch}
          onClose={() => setGenerateTarget(null)}
          onDone={() => {
            void queryClient.invalidateQueries({ queryKey: ["reports", "daily"] });
            void queryClient.invalidateQueries({ queryKey: ["reports"] });
          }}
        />
      ) : null}
    </PagePanel>
  );
}

function PersonalDailyTable({
  from,
  to,
  onOpen,
  onEdit
}: {
  from?: string;
  to?: string;
  onOpen: (record: DailyReportListItem) => void;
  onEdit: (record: DailyReportListItem) => void;
}) {
  const { user } = useAuth();
  const { page, pageSize, tablePagination } = useTablePagination();

  const reportsQuery = useQuery({
    queryKey: ["reports", "daily", "personal-list", { from, to, page, pageSize }],
    queryFn: () =>
      fetchMyReports({
        ...(from ? { from } : {}),
        ...(to ? { to } : {}),
        page: String(page),
        page_size: String(pageSize)
      }),
    staleTime: 30_000
  });

  const columns: ColumnsType<DailyReportListItem> = [
    { title: "日期", dataIndex: "report_date", width: 140, render: formatDate },
    { title: "状态", key: "status", width: 180, render: (_, record) => personalStatus(record, user?.role) },
    { title: "更新时间", dataIndex: "updated_at", render: formatDateTime },
    {
      title: "操作",
      key: "actions",
      width: 140,
      render: (_, record) => (
        <Space size={4}>
          <Button size="small" type="link" onClick={() => onOpen(record)}>
            打开
          </Button>
          <Button size="small" type="link" onClick={() => onEdit(record)}>
            编辑
          </Button>
        </Space>
      )
    }
  ];

  return (
    <Card
      className="reports-list-card"
      title="我的日报记录"
    >
      {reportsQuery.isError ? (
        <Alert type="error" showIcon message="我的日报加载失败" description={errorMessage(reportsQuery.error)} />
      ) : (
        <Table<DailyReportListItem>
          rowKey="id"
          columns={columns}
          dataSource={reportsQuery.data?.items ?? []}
          loading={reportsQuery.isLoading}
          pagination={tablePagination(reportsQuery.data?.total ?? 0)}
        />
      )}
    </Card>
  );
}

function TeamDailyTable({
  from,
  to,
  onOpen,
  onEdit
}: {
  from?: string;
  to?: string;
  onOpen: (record: TeamReportListItem) => void;
  onEdit: (record: TeamReportListItem) => void;
}) {
  const { page, pageSize, tablePagination } = useTablePagination();

  const reportsQuery = useQuery({
    queryKey: ["reports", "daily", "team-list", { from, to, page, pageSize }],
    queryFn: () =>
      fetchTeamReports({
        ...(from ? { from } : {}),
        ...(to ? { to } : {}),
        page: String(page),
        page_size: String(pageSize)
      }),
    staleTime: 30_000
  });

  const columns: ColumnsType<TeamReportListItem> = [
    { title: "日期", dataIndex: "report_date", width: 130, render: formatDate },
    { title: "成员数", dataIndex: "member_count", width: 100 },
    { title: "已发送人数", dataIndex: "submitted_count", width: 120 },
    { title: "未发送人数", dataIndex: "missing_count", width: 120 },
    { title: "小组日报状态", key: "status", width: 190, render: (_, record) => teamStatus(record) },
    { title: "发送给总监时间", dataIndex: "submitted_at", render: formatDateTime },
    { title: "更新时间", dataIndex: "updated_at", render: formatDateTime },
    {
      title: "操作",
      key: "actions",
      width: 140,
      render: (_, record) => (
        <Space size={4}>
          <Button size="small" type="link" onClick={() => onOpen(record)}>
            打开
          </Button>
          <Button size="small" type="link" onClick={() => onEdit(record)}>
            编辑
          </Button>
        </Space>
      )
    }
  ];
  return (
    <Card
      className="reports-list-card"
      title="小组日报记录"
    >
      {reportsQuery.isError ? (
        <Alert type="error" showIcon message="小组日报加载失败" description={errorMessage(reportsQuery.error)} />
      ) : (
        <Table<TeamReportListItem>
          rowKey="id"
          columns={columns}
          dataSource={reportsQuery.data?.items ?? []}
          loading={reportsQuery.isLoading}
          pagination={tablePagination(reportsQuery.data?.total ?? 0)}
        />
      )}
    </Card>
  );
}

function DepartmentDailyTable({
  from,
  to,
  onOpen,
  onEdit
}: {
  from?: string;
  to?: string;
  onOpen: (record: DepartmentReportListItem) => void;
  onEdit: (record: DepartmentReportListItem) => void;
}) {
  const { page, pageSize, tablePagination } = useTablePagination();

  const reportsQuery = useQuery({
    queryKey: ["reports", "daily", "department-list", { from, to, page, pageSize }],
    queryFn: () =>
      fetchDepartmentReports({
        ...(from ? { from } : {}),
        ...(to ? { to } : {}),
        page: String(page),
        page_size: String(pageSize)
      }),
    staleTime: 30_000
  });

  const columns: ColumnsType<DepartmentReportListItem> = [
    { title: "日期", dataIndex: "report_date", width: 140, render: formatDate },
    { title: "小组总数", dataIndex: "team_count", width: 120 },
    { title: "已发送小组数", dataIndex: "submitted_team_count", width: 140 },
    { title: "未发送小组数", dataIndex: "missing_team_count", width: 140 },
    { title: "状态", key: "status", width: 120, render: (_, record) => departmentStatus(record) },
    { title: "更新时间", dataIndex: "updated_at", render: formatDateTime },
    {
      title: "操作",
      key: "actions",
      width: 140,
      render: (_, record) => (
        <Space size={4}>
          <Button size="small" type="link" onClick={() => onOpen(record)}>
            打开
          </Button>
          <Button size="small" type="link" onClick={() => onEdit(record)}>
            编辑
          </Button>
        </Space>
      )
    }
  ];

  return (
    <Card
      className="reports-list-card"
      title="部门日报记录"
    >
      {reportsQuery.isError ? (
        <Alert type="error" showIcon message="部门日报加载失败" description={errorMessage(reportsQuery.error)} />
      ) : (
        <Table<DepartmentReportListItem>
          rowKey="id"
          columns={columns}
          dataSource={reportsQuery.data?.items ?? []}
          loading={reportsQuery.isLoading}
          pagination={tablePagination(reportsQuery.data?.total ?? 0)}
        />
      )}
    </Card>
  );
}

export function PersonalDailyReportDetailPage() {
  const { id } = useParams<{ id: string }>();

  return <PersonalDailyReportDetailContent id={id} />;
}

function PersonalDailyReportDetailContent({
  id,
  embedded = false
}: {
  id?: string;
  embedded?: boolean;
}) {

  const reportQuery = useQuery({
    queryKey: ["reports", "daily", "personal-detail", id],
    queryFn: () => fetchReport(id ?? ""),
    enabled: Boolean(id),
    staleTime: 30_000
  });
  const report = reportQuery.data;

  const content = (
    <>
      {reportQuery.isError ? (
        <Alert type="error" showIcon message="个人日报加载失败" description={errorMessage(reportQuery.error)} />
      ) : !report ? (
        <Card loading={reportQuery.isLoading} />
      ) : (
        <Space orientation="vertical" size="middle" style={{ width: "100%" }}>
          <Card>
            <Space size="large" wrap>
              <Text>日期：{formatDate(report.report_date)}</Text>
              <Text>状态：{report.edited ? "已编辑" : "已保存"}</Text>
              <Text>更新时间：{formatDateTime(report.updated_at)}</Text>
            </Space>
          </Card>
          <Card title="日报正文">
            {report.content.trim() ? <MarkdownViewer value={report.content} /> : <Empty description="暂无日报内容" />}
          </Card>
        </Space>
      )}
    </>
  );

  if (embedded) {
    return content;
  }

  return (
    <PagePanel title="个人日报详情" breadcrumbs={[{ title: "报告" }, { title: "日报", path: dailyReportsPath("personal") }, { title: "个人日报详情" }]} showNav={false}>
      {content}
    </PagePanel>
  );
}

export function TeamDailyReportDetailPage() {
  const { id } = useParams<{ id: string }>();

  return <TeamDailyReportContent id={id} />;
}

function TeamDailyReportContent({
  id,
  embedded = false
}: {
  id?: string;
  embedded?: boolean;
}) {
  const reportQuery = useQuery({
    queryKey: ["reports", "daily", "team-detail", id],
    queryFn: () => fetchTeamReport(id ?? ""),
    enabled: Boolean(id),
    staleTime: 30_000
  });
  const report = reportQuery.data;

  const content = (
    <>
      {reportQuery.isError ? (
        <Alert type="error" showIcon message="小组日报加载失败" description={errorMessage(reportQuery.error)} />
      ) : !report ? (
        <Card loading={reportQuery.isLoading} />
      ) : (
        <Space orientation="vertical" size="middle" style={{ width: "100%" }}>
          <Card>
            <Space size="large" wrap>
              <Text>日期：{formatDate(report.report_date)}</Text>
              <Text>小组：{report.team_name}</Text>
              <Text>状态：{report.status === "submitted" ? "已发送" : report.status === "saved" && report.submitted_at ? "已保存，未发送最新修改" : "已保存"}</Text>
              <Text>发送时间：{formatDateTime(report.submitted_at)}</Text>
              <Text>更新时间：{formatDateTime(report.updated_at)}</Text>
            </Space>
          </Card>
          <Card title="小组日报正文">
            {report.content.trim() ? <MarkdownViewer value={report.content} /> : <Empty description="暂无小组日报内容" />}
          </Card>
        </Space>
      )}
    </>
  );

  if (embedded) {
    return content;
  }

  return (
    <PagePanel title="小组日报详情" breadcrumbs={[{ title: "报告" }, { title: "日报", path: dailyReportsPath("team") }, { title: "小组日报详情" }]} showNav={false}>
      {content}
    </PagePanel>
  );
}

export function DepartmentDailyReportDetailPage() {
  const { id } = useParams<{ id: string }>();

  return <DepartmentDailyReportContent id={id} />;
}

function DepartmentDailyReportContent({
  id,
  embedded = false
}: {
  id?: string;
  embedded?: boolean;
}) {

  const reportQuery = useQuery({
    queryKey: ["reports", "daily", "department-detail", id],
    queryFn: () => fetchDepartmentReport(id ?? ""),
    enabled: Boolean(id),
    staleTime: 30_000
  });
  const report = reportQuery.data;

  const content = (
    <>
      {reportQuery.isError ? (
        <Alert type="error" showIcon message="部门日报加载失败" description={errorMessage(reportQuery.error)} />
      ) : !report ? (
        <Card loading={reportQuery.isLoading} />
      ) : (
        <Space orientation="vertical" size="middle" style={{ width: "100%" }}>
          <Card>
            <Space size="large" wrap>
              <Text>日期：{formatDate(report.report_date)}</Text>
              <Text>状态：{report.status === "saved" || report.archived_at ? "已保存" : "暂无报告"}</Text>
              <Text>更新时间：{formatDateTime(report.updated_at)}</Text>
            </Space>
          </Card>
          <Card title="部门日报正文">
            {report.content.trim() ? <MarkdownViewer value={report.content} /> : <Empty description="暂无部门日报内容" />}
          </Card>
        </Space>
      )}
    </>
  );

  if (embedded) {
    return content;
  }

  return (
    <PagePanel title="部门日报详情" breadcrumbs={[{ title: "报告" }, { title: "日报", path: dailyReportsPath("department") }, { title: "部门日报详情" }]} showNav={false}>
      {content}
    </PagePanel>
  );
}
