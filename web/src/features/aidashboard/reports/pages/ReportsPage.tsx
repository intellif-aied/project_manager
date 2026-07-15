import { CopyOutlined, DeleteOutlined, DownOutlined, EditOutlined, UpOutlined } from "@ant-design/icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Alert,
  App,
  Button,
  Card,
  DatePicker,
  Empty,
  Pagination,
  Segmented,
  Select,
  Space,
  Table,
  Tag,
  Tooltip,
  Typography
} from "antd";
import type { ColumnsType } from "antd/es/table";
import type { Dayjs } from "dayjs";
import { useState, type ReactNode } from "react";
import { useParams, useSearchParams } from "react-router-dom";
import dayjs from "dayjs";

import { useAuth } from "@/shared/auth/authContext";
import { MarkdownViewer } from "@/shared/components/MarkdownViewer/MarkdownViewer";
import { PagePanel } from "@/shared/components/PagePanel/PagePanel";

import {
  fetchDepartmentReport,
  fetchDepartmentReports,
  fetchDepartments,
  fetchMemberDailyReport,
  fetchMemberDailyReports,
  fetchMyReports,
  fetchReport,
  fetchTeamReport,
  fetchTeamReports,
  deleteDepartmentReport,
  deleteReport,
  deleteTeamReport
} from "../../api/client";
import { DailyReportGenerateModal, type DailyGenerateScope } from "../components/DailyReportGenerateModal";
import { MemberReportBrowser } from "../MemberReportBrowser";
import type {
  DailyReport,
  DailyReportListItem,
  DepartmentReport,
  DepartmentReportListItem,
  TeamReportListItem
} from "../../api/types";

import "./ReportsPage.css";

const { Text } = Typography;
const { RangePicker } = DatePicker;
const pageSizeOptions = [10, 20, 50, 100];

type DailyTab = "personal" | "member" | "team" | "department";

function isDailyTab(value: string | null): value is DailyTab {
  return value === "personal" || value === "member" || value === "team" || value === "department";
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

function planPreview(value?: string) {
  const plan = value?.trim();
  return plan ? <span title={plan}>{plan}</span> : "-";
}

function teamStatus(record: TeamReportListItem) {
  if (record.status === "submitted") return <Tag color="green">已发送</Tag>;
  if (record.status === "saved" && record.submitted_at) return <Tag color="gold">已保存，未发送最新修改</Tag>;
  if (record.status === "saved") return <Tag color="blue">已保存</Tag>;
  return <Tag>暂无报告</Tag>;
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
  const { message, modal } = App.useApp();
  const [searchParams, setSearchParams] = useSearchParams();
  const queryClient = useQueryClient();
  const [dateRange, setDateRange] = useState<[Dayjs, Dayjs] | null>(null);
  const [memberDate, setMemberDate] = useState<Dayjs>(() => dayjs());
  const [selectedDepartmentID, setSelectedDepartmentID] = useState<string>();
  const [generateTarget, setGenerateTarget] = useState<{
    scope: DailyGenerateScope;
    reportId?: string;
    reportDate?: string;
    departmentId?: string;
    readOnly?: boolean;
    allowDateSwitch?: boolean;
  } | null>(null);

  const canSelectDepartment = user?.role === "admin";
  const departmentsQuery = useQuery({
    queryKey: ["departments", "reports-daily"],
    queryFn: fetchDepartments,
    enabled: canSelectDepartment,
    staleTime: 60_000
  });
  const effectiveDepartmentID = canSelectDepartment
    ? selectedDepartmentID ?? departmentsQuery.data?.[0]?.id : undefined;

  const deleteMutation = useMutation({
    mutationFn: ({
      scope,
      id,
      departmentId
    }: {
      scope: "personal" | "team" | "department";
      id: string;
      departmentId?: string;
    }) => {
      if (scope === "team") return deleteTeamReport(id);
      if (scope === "department") return deleteDepartmentReport(id, departmentId);
      return deleteReport(id);
    },
    onSuccess: () => {
      message.success("日报已删除");
      void queryClient.invalidateQueries({ queryKey: ["reports", "daily"] });
      void queryClient.invalidateQueries({ queryKey: ["reports"] });
    },
    onError: (error) => message.error(errorMessage(error))
  });

  const confirmDelete = (
    scope: "personal" | "team" | "department",
    id: string,
    departmentId?: string
  ) => {
    modal.confirm({
      title: "删除日报？",
      content: "删除后不可恢复。",
      okText: "删除",
      okButtonProps: { danger: true },
      cancelText: "取消",
      onOk: () => deleteMutation.mutateAsync({ scope, id, departmentId })
    });
  };

  const options =
    user?.role === "director" || user?.role === "admin"
      ? [
          { label: "我的日报记录", value: "personal" },
          { label: "部门成员日报", value: "member" },
          { label: "部门汇总日报", value: "department" }
        ]
      : user?.role === "team_leader"
        ? [
            { label: "我的日报记录", value: "personal" },
            { label: "小组成员日报", value: "member" },
            { label: "小组汇总日报", value: "team" }
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
  const canOpenCurrentReport =
    activeTab !== "member" && (activeTab !== "team" || user?.role === "team_leader");

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
            {activeTab === "member" ? (
              <DatePicker
                value={memberDate}
                allowClear={false}
                onChange={(value) => value && setMemberDate(value)}
              />
            ) : (
              <RangePicker value={dateRange} onChange={(value) => setDateRange(value as [Dayjs, Dayjs] | null)} />
            )}
            {canSelectDepartment && (activeTab === "member" || activeTab === "department") ? (
              <Select
                className="reports-member-team-select"
                value={effectiveDepartmentID}
                loading={departmentsQuery.isLoading}
                placeholder="选择部门"
                options={(departmentsQuery.data ?? []).map((item) => ({ label: item.name, value: item.id }))}
                onChange={setSelectedDepartmentID}
              />
            ) : null}
          </Space>
          {canOpenCurrentReport ? (
            <Button
              type="primary"
              icon={<EditOutlined />}
              disabled={activeTab === "department" && canSelectDepartment && !effectiveDepartmentID}
              onClick={() =>
                setGenerateTarget({
                  scope: activeTab,
                  allowDateSwitch: true,
                  departmentId: activeTab === "department" ? effectiveDepartmentID : undefined
                })
              }
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
          onEdit={(record) =>
            setGenerateTarget({ scope: "personal", reportId: record.id, reportDate: record.report_date })
          }
          onDelete={(record) => confirmDelete("personal", record.id)}
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
          onDelete={(record) => confirmDelete("team", record.id)}
        />
      ) : null}
      {activeTab === "member" ? (
        <MemberDailyTable
          date={memberDate.format("YYYY-MM-DD")}
          departmentId={effectiveDepartmentID}
          requireDepartmentId={canSelectDepartment}
        />
      ) : null}
      {activeTab === "department" ? (
        <DepartmentDailyTable
          key={`department:${effectiveDepartmentID ?? "own"}:${from ?? ""}:${to ?? ""}`}
          from={from}
          to={to}
          departmentId={effectiveDepartmentID}
          requireDepartmentId={canSelectDepartment}
          onEdit={(record) =>
            setGenerateTarget({
              scope: "department",
              reportId: record.id,
              reportDate: record.report_date,
              departmentId: effectiveDepartmentID
            })
          }
          onDelete={(record) => confirmDelete("department", record.id, effectiveDepartmentID)}
        />
      ) : null}
      {generateTarget ? (
        <DailyReportGenerateModal
          open
          scope={generateTarget.scope}
          departmentId={generateTarget.departmentId}
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

function MemberDailyTable({
  date,
  departmentId,
  requireDepartmentId
}: {
  date: string;
  departmentId?: string;
  requireDepartmentId?: boolean;
}) {
  const reportsQuery = useQuery({
    queryKey: ["reports", "daily", "member-list", date, departmentId],
    queryFn: () => fetchMemberDailyReports(date, departmentId),
    staleTime: 30_000,
    enabled: !requireDepartmentId || Boolean(departmentId)
  });
  return (
    <MemberReportBrowser items={reportsQuery.data ?? []} loading={reportsQuery.isLoading}
      error={reportsQuery.isError ? errorMessage(reportsQuery.error) : undefined} showNextDayPlan
      queryKey={`daily:${date}`} fetchDetail={fetchMemberDailyReport} displayMode="content-list" />
  );
}

type InlineDailyRecord = {
  id: string;
  report_date: string;
  next_day_plan: string;
  updated_at: string;
};

async function copyReportText(value: string) {
  if (navigator.clipboard && window.isSecureContext) {
    const copied = await navigator.clipboard.writeText(value).then(() => true, () => false);
    if (copied) return;
  }
  const textarea = document.createElement("textarea");
  textarea.value = value;
  textarea.setAttribute("readonly", "");
  textarea.style.position = "fixed";
  textarea.style.left = "-9999px";
  document.body.appendChild(textarea);
  textarea.focus();
  textarea.select();
  const copied = document.execCommand("copy");
  document.body.removeChild(textarea);
  if (!copied) throw new Error("copy failed");
}

function InlineDailyContentList<TRecord extends InlineDailyRecord, TDetail extends { content: string }>({
  title,
  items,
  total,
  loading,
  error,
  pagination,
  fetchDetail,
  renderStatus,
  renderMeta,
  onEdit,
  onDelete
}: {
  title: string;
  items: TRecord[];
  total: number;
  loading: boolean;
  error?: string;
  pagination: (total: number) => Parameters<typeof Pagination>[0];
  fetchDetail: (id: string) => Promise<TDetail>;
  renderStatus: (record: TRecord) => ReactNode;
  renderMeta: (record: TRecord) => string;
  onEdit: (record: TRecord) => void;
  onDelete: (record: TRecord) => void;
}) {
  return (
    <Card className="member-report-content-list reports-inline-content-list" title={title}>
      {error ? (
        <Alert type="error" showIcon message={error} />
      ) : loading ? (
        <div className="member-report-content-list__loading">正在加载日报…</div>
      ) : items.length === 0 ? (
        <Empty description="暂无日报记录" />
      ) : (
        <div className="member-report-content-list__items">
          {items.map((record) => (
            <InlineDailyContentItem
              key={record.id}
              record={record}
              title={title}
              fetchDetail={fetchDetail}
              status={renderStatus(record)}
              meta={renderMeta(record)}
              onEdit={() => onEdit(record)}
              onDelete={() => onDelete(record)}
            />
          ))}
        </div>
      )}
      {!error && !loading && total > 0 ? <Pagination className="reports-inline-content-list__pagination" {...pagination(total)} /> : null}
    </Card>
  );
}

function InlineDailyContentItem<TRecord extends InlineDailyRecord, TDetail extends { content: string }>({
  record,
  title,
  fetchDetail,
  status,
  meta,
  onEdit,
  onDelete
}: {
  record: TRecord;
  title: string;
  fetchDetail: (id: string) => Promise<TDetail>;
  status: ReactNode;
  meta: string;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const { message } = App.useApp();
  const [expanded, setExpanded] = useState(false);
  const detailQuery = useQuery({
    queryKey: ["reports", "daily-inline-detail", title, record.id],
    queryFn: () => fetchDetail(record.id),
    enabled: expanded,
    staleTime: 30_000
  });
  const copyCurrentReport = async () => {
    const content = detailQuery.data?.content?.trim();
    if (!content) return;
    try {
      await copyReportText(content);
      void message.success("日报全文已复制");
    } catch {
      void message.error("复制失败，请稍后重试");
    }
  };

  return (
    <article className={`member-report-content-item${expanded ? " is-expanded" : ""}`}>
      <header className="member-report-content-item__head">
        <div>
          <div className="member-report-content-item__identity">
            <strong>{formatDate(record.report_date)}</strong>
            <span>{meta}</span>
          </div>
          <small>{formatDateTime(record.updated_at)}</small>
        </div>
        <div className="member-report-content-item__actions">
          {status}
          <Button className="member-report-content-item__edit" type="text" size="small" onClick={onEdit}>
            编辑
          </Button>
          <Button type="text" size="small" danger icon={<DeleteOutlined />} onClick={onDelete}>
            删除
          </Button>
          <Button className="member-report-content-item__toggle" type="text" size="small" aria-expanded={expanded} onClick={() => setExpanded((value) => !value)}>
            {expanded ? <UpOutlined /> : <DownOutlined />}
            {expanded ? "收起" : "展开"}
          </Button>
        </div>
      </header>
      {expanded ? (
        <div className="member-report-content-item__detail">
          <div className="member-report-content-item__detail-bar">
            <span>日报全文</span>
            <Tooltip title="复制全文（保留 Markdown 格式）">
              <Button className="member-report-content-item__copy" type="text" size="small" icon={<CopyOutlined />} aria-label="复制日报全文" disabled={!detailQuery.data?.content?.trim()} onClick={() => void copyCurrentReport()} />
            </Tooltip>
          </div>
          {detailQuery.isLoading ? <div className="member-report-content-item__loading">正在加载日报全文…</div> : null}
          {detailQuery.isError ? <Alert type="error" showIcon message="日报加载失败" /> : null}
          {!detailQuery.isLoading && !detailQuery.isError && detailQuery.data?.content?.trim() ? <MarkdownViewer value={detailQuery.data.content} /> : null}
          {!detailQuery.isLoading && !detailQuery.isError && !detailQuery.data?.content?.trim() ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无日报内容" /> : null}
          <div className="member-report-content-item__plan">
            <strong>明日计划</strong>
            <p>{record.next_day_plan?.trim() || "未填写"}</p>
          </div>
        </div>
      ) : null}
    </article>
  );
}

function PersonalDailyTable({
  from,
  to,
  onEdit,
  onDelete
}: {
  from?: string;
  to?: string;
  onEdit: (record: DailyReportListItem) => void;
  onDelete: (record: DailyReportListItem) => void;
}) {
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

  return (
    <InlineDailyContentList<DailyReportListItem, DailyReport>
      title="我的日报记录"
      items={reportsQuery.data?.items ?? []}
      total={reportsQuery.data?.total ?? 0}
      loading={reportsQuery.isLoading}
      error={reportsQuery.isError ? `我的日报加载失败：${errorMessage(reportsQuery.error)}` : undefined}
      pagination={tablePagination}
      fetchDetail={fetchReport}
      renderStatus={() => null}
      renderMeta={() => "我的日报"}
      onEdit={onEdit}
      onDelete={onDelete}
    />
  );
}

function TeamDailyTable({
  from,
  to,
  onOpen,
  onEdit,
  onDelete
}: {
  from?: string;
  to?: string;
  onOpen: (record: TeamReportListItem) => void;
  onEdit: (record: TeamReportListItem) => void;
  onDelete: (record: TeamReportListItem) => void;
}) {
  const { user } = useAuth();
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
    { title: "明日计划", dataIndex: "next_day_plan", width: 260, ellipsis: true, render: planPreview },
    { title: "成员数", dataIndex: "member_count", width: 100 },
    { title: "已发送人数", dataIndex: "submitted_count", width: 120 },
    { title: "未发送人数", dataIndex: "missing_count", width: 120 },
    { title: "小组日报状态", key: "status", width: 190, render: (_, record) => teamStatus(record) },
    { title: "发送给总监时间", dataIndex: "submitted_at", render: formatDateTime },
    { title: "更新时间", dataIndex: "updated_at", render: formatDateTime },
    {
      title: "操作",
      key: "actions",
      width: 190,
      render: (_, record) => (
        <Space size={4}>
          <Button size="small" type="link" onClick={() => onOpen(record)}>
            打开
          </Button>
          <Button size="small" type="link" onClick={() => onEdit(record)}>
            编辑
          </Button>
          {record.leader_id === user?.id ? (
            <Button size="small" type="link" danger onClick={() => onDelete(record)}>
              删除
            </Button>
          ) : null}
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
  departmentId,
  requireDepartmentId,
  onEdit,
  onDelete
}: {
  from?: string;
  to?: string;
  departmentId?: string;
  requireDepartmentId?: boolean;
  onEdit: (record: DepartmentReportListItem) => void;
  onDelete: (record: DepartmentReportListItem) => void;
}) {
  const { page, pageSize, tablePagination } = useTablePagination();

  const reportsQuery = useQuery({
    queryKey: ["reports", "daily", "department-list", { departmentId, from, to, page, pageSize }],
    queryFn: () =>
      fetchDepartmentReports({
        ...(from ? { from } : {}),
        ...(to ? { to } : {}),
        ...(departmentId ? { department_id: departmentId } : {}),
        page: String(page),
        page_size: String(pageSize)
      }),
    staleTime: 30_000,
    enabled: !requireDepartmentId || Boolean(departmentId)
  });

  return (
    <InlineDailyContentList<DepartmentReportListItem, DepartmentReport>
      title="部门日报记录"
      items={reportsQuery.data?.items ?? []}
      total={reportsQuery.data?.total ?? 0}
      loading={reportsQuery.isLoading}
      error={reportsQuery.isError ? `部门日报加载失败：${errorMessage(reportsQuery.error)}` : undefined}
      pagination={tablePagination}
      fetchDetail={(id) => fetchDepartmentReport(id, departmentId)}
      renderStatus={() => null}
      renderMeta={(record) => `共 ${record.team_count} 个小组 · 已提交 ${record.submitted_team_count} · 未提交 ${record.missing_team_count}`}
      onEdit={onEdit}
      onDelete={onDelete}
    />
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
