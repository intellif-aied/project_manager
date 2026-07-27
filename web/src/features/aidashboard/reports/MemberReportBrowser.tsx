import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Alert, App, Button, Card, Empty, Select, Space, Table, Tag, Tooltip } from "antd";
import type { ColumnsType } from "antd/es/table";
import { CopyOutlined, LeftOutlined, RightOutlined } from "@ant-design/icons";
import dayjs from "dayjs";

import type { MemberPersonalReport } from "../api/types";
import { MarkdownViewer } from "@/shared/components/MarkdownViewer/MarkdownViewer";
import { reportContentSummary } from "./utils/reportSummary";

const roleLabels: Record<string, string> = {
  director: "总监",
  pm: "PM",
  team_leader: "TL",
  employee: "员工",
  admin: "管理员"
};

type MemberReportBrowserProps<T extends { content: string; next_day_plan?: string }> = {
  items: MemberPersonalReport[];
  loading: boolean;
  error?: string;
  queryKey: string;
  fetchDetail: (id: string) => Promise<T>;
  displayMode?: "split" | "content-list";
  showNextDayPlan?: boolean;
  reportLabel?: "日报" | "周报";
  contentListTitle?: string;
  emptyPeriodLabel?: string;
};

function formatMemberUpdatedAt(value?: string) {
  return value ? dayjs(value).format("YYYY-MM-DD HH:mm") : "";
}

async function copyText(value: string) {
  if (navigator.clipboard && window.isSecureContext) {
    const copied = await navigator.clipboard.writeText(value).then(
      () => true,
      () => false
    );
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

export function MemberReportBrowser<T extends { content: string; next_day_plan?: string }>({
  displayMode = "split",
  showNextDayPlan = false,
  reportLabel = "日报",
  contentListTitle = "部门成员日报",
  emptyPeriodLabel = "当日",
  ...props
}: MemberReportBrowserProps<T>) {
  if (displayMode === "content-list") {
    return (
      <MemberReportContentList
        {...props}
        showNextDayPlan={showNextDayPlan}
        reportLabel={reportLabel}
        contentListTitle={contentListTitle}
        emptyPeriodLabel={emptyPeriodLabel}
      />
    );
  }
  return <MemberReportSplitBrowser {...props} />;
}

function MemberReportSplitBrowser<T extends { content: string; next_day_plan?: string }>({
  items,
  loading,
  error,
  queryKey,
  fetchDetail
}: Omit<MemberReportBrowserProps<T>, "displayMode">) {
  const [teamID, setTeamID] = useState("all");
  const [selectedID, setSelectedID] = useState<string>();
  const teams = useMemo(
    () =>
      Array.from(
        new Map(items.filter((x) => x.team_id).map((x) => [x.team_id!, x.team_name])).entries()
      ),
    [items]
  );
  const filtered = useMemo(
    () => (teamID === "all" ? items : items.filter((x) => x.team_id === teamID)),
    [items, teamID]
  );
  const available = filtered.filter((x) => x.report_id);

  const effectiveSelectedID = available.some((x) => x.report_id === selectedID)
    ? selectedID
    : available[0]?.report_id;

  const detail = useQuery({
    queryKey: ["reports", "member-browser", queryKey, effectiveSelectedID],
    queryFn: () => fetchDetail(effectiveSelectedID!),
    enabled: Boolean(effectiveSelectedID),
    staleTime: 30_000
  });
  const currentIndex = available.findIndex((x) => x.report_id === effectiveSelectedID);
  const columns: ColumnsType<MemberPersonalReport> = [
    { title: "成员", dataIndex: "user_name", width: 100, ellipsis: true },
    {
      title: "角色/小组",
      width: 120,
      render: (_, x) => (
        <span>
          {roleLabels[x.role] ?? x.role}
          <br />
          <small>{x.team_name || "直属部门"}</small>
        </span>
      )
    },
    {
      title: "状态",
      width: 85,
      render: (_, x) => (x.has_report ? <Tag color="green">已填写</Tag> : <Tag>未填写</Tag>)
    },
    {
      title: "更新时间",
      dataIndex: "saved_at",
      width: 110,
      render: (v?: string) =>
        v
          ? new Intl.DateTimeFormat("zh-CN", {
              month: "2-digit",
              day: "2-digit",
              hour: "2-digit",
              minute: "2-digit",
              hour12: false
            }).format(new Date(v))
          : "-"
    },
    {
      title: "正文摘要",
      dataIndex: "content_preview",
      ellipsis: true,
      render: (v?: string) => v?.replace(/\s+/g, " ") || "-"
    }
  ];

  return (
    <div className="member-report-browser">
      <section className="member-report-browser__list">
        <div className="member-report-browser__toolbar">
          <strong>成员报告</strong>
          {teams.length > 1 ? (
            <Select
              value={teamID}
              onChange={setTeamID}
              options={[
                { value: "all", label: "全部小组" },
                ...teams.map(([value, label]) => ({ value, label }))
              ]}
            />
          ) : null}
        </div>
        {error ? (
          <Alert type="error" showIcon message={error} />
        ) : (
          <Table
            rowKey="user_id"
            size="small"
            loading={loading}
            columns={columns}
            dataSource={filtered}
            pagination={{
              pageSize: 10,
              showSizeChanger: true,
              pageSizeOptions: [10, 20, 50],
              showTotal: (n) => `共 ${n} 人`
            }}
            onRow={(record) => ({
              className: record.report_id === effectiveSelectedID ? "is-selected" : "",
              onClick: () => record.report_id && setSelectedID(record.report_id)
            })}
          />
        )}
      </section>
      <section className="member-report-browser__detail">
        <header>
          <strong>{available[currentIndex]?.user_name || "报告正文"}</strong>
          <Space>
            <Button
              icon={<LeftOutlined />}
              disabled={currentIndex <= 0}
              onClick={() => setSelectedID(available[currentIndex - 1]?.report_id)}
            />
            <Button
              icon={<RightOutlined />}
              disabled={currentIndex < 0 || currentIndex >= available.length - 1}
              onClick={() => setSelectedID(available[currentIndex + 1]?.report_id)}
            />
          </Space>
        </header>
        <div className="member-report-browser__content">
          {detail.isLoading ? (
            "正在加载..."
          ) : detail.isError ? (
            <Alert type="error" message="报告加载失败" />
          ) : detail.data?.content?.trim() ? (
            <MarkdownViewer value={detail.data.content} />
          ) : (
            <Empty description="请选择已填写的报告" />
          )}
        </div>
      </section>
    </div>
  );
}

function MemberReportContentList<T extends { content: string; next_day_plan?: string }>({
  items,
  loading,
  error,
  queryKey,
  fetchDetail,
  showNextDayPlan,
  reportLabel = "日报",
  contentListTitle = "部门成员日报",
  emptyPeriodLabel = "当日"
}: Omit<MemberReportBrowserProps<T>, "displayMode">) {
  const { message } = App.useApp();
  const [teamID, setTeamID] = useState("all");
  const [selectedID, setSelectedID] = useState<string>();
  const returnFocusRef = useRef<HTMLElement | null>(null);
  const teams = useMemo(
    () =>
      Array.from(
        new Map(
          items.filter((item) => item.team_id).map((item) => [item.team_id!, item.team_name])
        ).entries()
      ),
    [items]
  );
  const filtered = useMemo(
    () => (teamID === "all" ? items : items.filter((item) => item.team_id === teamID)),
    [items, teamID]
  );
  const available = filtered.filter((item) => item.report_id);
  const missing = filtered.filter((item) => !item.report_id);
  const effectiveSelectedID = available.some((item) => item.report_id === selectedID)
    ? selectedID
    : undefined;
  const detail = useQuery({
    queryKey: ["reports", "member-content-list", queryKey, effectiveSelectedID],
    queryFn: () => fetchDetail(effectiveSelectedID!),
    enabled: Boolean(effectiveSelectedID),
    staleTime: 30_000
  });
  const submittedCount = available.length;
  const missingCount = missing.length;
  const submittedNames = available.map((item) => item.user_name).join("、");
  const missingNames = missing.map((item) => item.user_name).join("、");
  const currentIndex = available.findIndex((item) => item.report_id === effectiveSelectedID);
  const currentItem = currentIndex >= 0 ? available[currentIndex] : undefined;

  const copyCurrentReport = async () => {
    const content = detail.data?.content?.trim();
    if (!content) return;
    try {
      await copyText(content);
      void message.success(`${reportLabel}全文已复制`);
    } catch {
      void message.error("复制失败，请稍后重试");
    }
  };

  const closeReport = () => {
    setSelectedID(undefined);
    requestAnimationFrame(() => returnFocusRef.current?.focus({ preventScroll: true }));
  };

  const toggleReport = (reportID: string) => {
    if (selectedID === reportID) {
      closeReport();
      return;
    }
    returnFocusRef.current =
      document.activeElement instanceof HTMLElement ? document.activeElement : null;
    setSelectedID(reportID);
  };

  useEffect(() => {
    if (!effectiveSelectedID) return;
    const handleEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") closeReport();
    };
    document.addEventListener("keydown", handleEscape);
    return () => {
      document.removeEventListener("keydown", handleEscape);
    };
  }, [effectiveSelectedID]);

  return (
    <Card
      className="member-report-content-list reports-inline-content-list reports-member-content-list"
      title={
        <div className="member-report-content-list__title">
          <span>{contentListTitle}</span>
          <small className="member-report-submission-summary">
            <span className="is-submitted" title={submittedNames || undefined}>
              <b>已提交 {submittedCount} 人</b>
              {submittedNames ? `：${submittedNames}` : ""}
            </span>
            <i aria-hidden="true">·</i>
            <span
              className={missingCount > 0 ? "has-missing" : undefined}
              title={missingNames || undefined}
            >
              <b>未提交 {missingCount} 人</b>
              {missingNames ? `：${missingNames}` : ""}
            </span>
          </small>
        </div>
      }
      extra={
        teams.length > 1 ? (
          <Select
            value={teamID}
            onChange={setTeamID}
            options={[
              { value: "all", label: "全部小组" },
              ...teams.map(([value, label]) => ({ value, label }))
            ]}
          />
        ) : null
      }
    >
      {error ? (
        <Alert type="error" showIcon message={error} />
      ) : loading ? (
        <div className="member-report-content-list__loading">正在加载成员{reportLabel}…</div>
      ) : filtered.length === 0 ? (
        <Empty description={`${emptyPeriodLabel}暂无成员${reportLabel}`} />
      ) : (
        <>
          {missingCount > 0 ? (
            <section className="member-report-pending" aria-label={`未提交${reportLabel}成员`}>
              <div className="member-report-pending__head">
                <div>
                  <strong>未提交成员</strong>
                  <p>
                    以下成员尚未提交{emptyPeriodLabel}的{reportLabel}。
                  </p>
                </div>
                <span>{missingCount} 人</span>
              </div>
              <div className="member-report-pending__people">
                {missing.map((item) => (
                  <span className="member-report-pending__person" key={item.user_id}>
                    <b>{item.user_name}</b>
                    <small>{item.team_name || item.department_name || "直属部门"}</small>
                  </span>
                ))}
              </div>
            </section>
          ) : null}
          {available.length === 0 ? (
            <Empty description={`${emptyPeriodLabel}暂无已提交成员${reportLabel}`} />
          ) : (
            <div className="member-report-content-list__items">
              {available.map((item) => {
                const isExpanded =
                  Boolean(item.report_id) && item.report_id === effectiveSelectedID;
                const preview = reportContentSummary(item.content_preview ?? "");
                const memberContext = item.team_name || item.department_name || "直属部门";
                return (
                  <article
                    className={`member-report-content-item${isExpanded ? " is-expanded" : ""}${item.has_report ? "" : " is-missing"}`}
                    key={item.user_id}
                  >
                    <header className="member-report-content-item__head">
                      <button
                        className="member-report-content-item__summary"
                        type="button"
                        disabled={!item.report_id}
                        aria-expanded={item.report_id ? isExpanded : undefined}
                        onClick={() => item.report_id && toggleReport(item.report_id)}
                      >
                        <span className="member-report-content-item__date">
                          <strong>{item.user_name}</strong>
                          <small>{roleLabels[item.role] ?? item.role}</small>
                        </span>
                        <span className="member-report-content-item__overview">
                          <span className="member-report-content-item__identity">
                            <span>{memberContext}</span>
                          </span>
                          <span
                            className={`member-report-content-item__preview${preview ? "" : " member-report-content-item__preview--empty"}`}
                          >
                            {preview ||
                              (item.has_report
                                ? `${reportLabel}已提交，查看全文获取完整内容。`
                                : `本期尚未提交${reportLabel}`)}
                          </span>
                          <small>
                            {item.saved_at
                              ? `更新于 ${formatMemberUpdatedAt(item.saved_at)}`
                              : "暂无更新时间"}
                          </small>
                        </span>
                      </button>
                      <div
                        className="member-report-content-item__actions"
                        role="group"
                        aria-label={`成员${reportLabel}操作`}
                      >
                        {item.report_id ? (
                          <Button
                            className="member-report-content-item__toggle"
                            type="text"
                            size="small"
                            aria-expanded={isExpanded}
                            onClick={() => toggleReport(item.report_id!)}
                          >
                            {isExpanded ? "收起正文" : "查看全文"}
                          </Button>
                        ) : (
                          <span className="member-report-content-item__missing-state">未提交</span>
                        )}
                      </div>
                    </header>
                    {isExpanded ? (
                      <div className="member-report-content-item__detail">
                        <div className="member-report-content-item__detail-bar">
                          <Tooltip title="复制全文（保留 Markdown 格式）">
                            <Button
                              className="member-report-content-item__copy"
                              type="text"
                              size="small"
                              icon={<CopyOutlined />}
                              aria-label={`复制${reportLabel}全文`}
                              disabled={!detail.data?.content?.trim()}
                              onClick={() => void copyCurrentReport()}
                            />
                          </Tooltip>
                        </div>
                        {detail.isLoading ? (
                          <div className="member-report-content-item__loading">
                            正在加载{reportLabel}全文…
                          </div>
                        ) : detail.isError ? (
                          <Alert type="error" showIcon message={`${reportLabel}加载失败`} />
                        ) : detail.data?.content?.trim() ? (
                          <MarkdownViewer value={detail.data.content} />
                        ) : (
                          <Empty
                            image={Empty.PRESENTED_IMAGE_SIMPLE}
                            description={`暂无${reportLabel}内容`}
                          />
                        )}
                        {showNextDayPlan && !detail.isLoading && !detail.isError ? (
                          <div className="member-report-content-item__plan">
                            <strong>明日计划</strong>
                            <p>{detail.data?.next_day_plan?.trim() || "未填写"}</p>
                          </div>
                        ) : null}
                      </div>
                    ) : null}
                  </article>
                );
              })}
            </div>
          )}
        </>
      )}
      {currentItem && effectiveSelectedID ? (
        <section
          className="member-report-mobile-detail"
          role="dialog"
          aria-modal="true"
          aria-labelledby={`member-report-mobile-title-${currentItem.user_id}`}
        >
          <header className="member-report-mobile-detail__header">
            <Button
              className="member-report-mobile-detail__back"
              type="text"
              icon={<LeftOutlined />}
              aria-label={`返回成员${reportLabel}列表`}
              onClick={closeReport}
            >
              返回
            </Button>
            <div className="member-report-mobile-detail__identity">
              <strong id={`member-report-mobile-title-${currentItem.user_id}`}>
                {currentItem.user_name}
              </strong>
              <span>
                {roleLabels[currentItem.role] ?? currentItem.role} ·{" "}
                {currentItem.team_name || "直属部门"}
              </span>
            </div>
            <Button
              className="member-report-mobile-detail__copy"
              type="text"
              icon={<CopyOutlined />}
              aria-label={`复制${reportLabel}全文`}
              disabled={!detail.data?.content?.trim()}
              onClick={() => void copyCurrentReport()}
            >
              复制
            </Button>
          </header>
          <div className="member-report-mobile-detail__meta">
            <span>{reportLabel}全文</span>
            <span>
              {currentItem.saved_at
                ? new Intl.DateTimeFormat("zh-CN", {
                    year: "numeric",
                    month: "2-digit",
                    day: "2-digit",
                    hour: "2-digit",
                    minute: "2-digit",
                    hour12: false
                  }).format(new Date(currentItem.saved_at))
                : "未记录更新时间"}
            </span>
          </div>
          <div className="member-report-mobile-detail__body">
            {detail.isLoading ? (
              <div className="member-report-content-item__loading">正在加载{reportLabel}全文…</div>
            ) : detail.isError ? (
              <Alert type="error" showIcon message={`${reportLabel}加载失败`} />
            ) : detail.data?.content?.trim() ? (
              <MarkdownViewer value={detail.data.content} />
            ) : (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={`暂无${reportLabel}内容`} />
            )}
            {showNextDayPlan && !detail.isLoading && !detail.isError ? (
              <div className="member-report-content-item__plan">
                <strong>明日计划</strong>
                <p>{detail.data?.next_day_plan?.trim() || "未填写"}</p>
              </div>
            ) : null}
          </div>
          <footer className="member-report-mobile-detail__footer">
            <Button
              icon={<LeftOutlined />}
              disabled={currentIndex <= 0}
              onClick={() => setSelectedID(available[currentIndex - 1]?.report_id)}
            >
              上一位
            </Button>
            <span>
              {currentIndex + 1} / {available.length}
            </span>
            <Button
              icon={<RightOutlined />}
              iconPosition="end"
              disabled={currentIndex < 0 || currentIndex >= available.length - 1}
              onClick={() => setSelectedID(available[currentIndex + 1]?.report_id)}
            >
              下一位
            </Button>
          </footer>
        </section>
      ) : null}
    </Card>
  );
}
