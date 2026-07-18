import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Alert,
  App,
  Button,
  Card,
  DatePicker,
  Empty,
  Pagination,
  Popconfirm,
  Select,
  Space,
  Table,
  Tag,
  Tooltip,
  Typography
} from "antd";
import { DownloadOutlined } from "@ant-design/icons";
import { useState } from "react";
import dayjs from "dayjs";

import {
  downloadSessionLog,
  fetchSessions,
  fetchTasks,
  updateSessionTask,
  withdrawSession
} from "../../api/client";
import type { PaginatedSessions, Session, Task } from "../../api/types";
import { useAuth } from "@/shared/auth/authContext";
import { PagePanel } from "@/shared/components/PagePanel/PagePanel";
import { invalidateRequirementTaskWorkspace } from "../../requirements/queryInvalidation";
import "./SessionsPage.css";

const { Text } = Typography;

function formatDateTime(iso: string): string {
  const d = new Date(iso);
  return `${d.toLocaleDateString("zh-CN", { month: "2-digit", day: "2-digit" })} ${d.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" })}`;
}

function formatDuration(secs?: number): string {
  if (!secs) return "-";
  return `${Math.floor(secs / 60)}分 ${secs % 60}秒`;
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : "请稍后重试";
}

function ConfidenceTag({ value }: { value?: number }) {
  if (value == null) return <Text type="secondary">-</Text>;
  const color = value > 0.8 ? "success" : value > 0.5 ? "warning" : "error";
  return <Tag color={color}>{Math.round(value * 100)}%</Tag>;
}

export function SessionsPage() {
  const { user } = useAuth();
  const queryClient = useQueryClient();
  const { message } = App.useApp();
  const [date, setDate] = useState<dayjs.Dayjs | null>(null);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  const [pendingSessionTaskId, setPendingSessionTaskId] = useState<string | null>(null);
  const [pendingWithdrawId, setPendingWithdrawId] = useState<string | null>(null);

  const dateStr = date?.format("YYYY-MM-DD") || "";

  const sessionsQuery = useQuery<PaginatedSessions>({
    queryKey: ["sessions", { date: dateStr, page, pageSize }],
    queryFn: () =>
      fetchSessions({
        ...(dateStr ? { date: dateStr } : {}),
        page: String(page),
        page_size: String(pageSize)
      }),
    staleTime: 30_000
  });
  const tasksQuery = useQuery<Task[]>({
    queryKey: ["tasks"],
    queryFn: () => fetchTasks(),
    staleTime: 60_000
  });

  const sessions = sessionsQuery.data?.items ?? [];
  const total = sessionsQuery.data?.total ?? 0;
  const tasks = tasksQuery.data ?? [];

  const overrideMutation = useMutation({
    mutationFn: ({ sessionId, taskId }: { sessionId: string; taskId: string | null }) =>
      updateSessionTask(sessionId, taskId),
    onSuccess: () => {
      message.success("任务关联已更新");
      void invalidateRequirementTaskWorkspace(queryClient);
    },
    onError: (err: unknown) => message.error(err instanceof Error ? err.message : "更新失败")
  });

  const withdrawMutation = useMutation({
    mutationFn: (sessionId: string) => withdrawSession(sessionId),
    onSuccess: () => {
      message.success("Session 已撤回");
      void invalidateRequirementTaskWorkspace(queryClient);
    },
    onError: (err: unknown) => message.error(err instanceof Error ? err.message : "撤回失败")
  });

  const handleSessionTaskChange = async (sessionId: string, taskId: string) => {
    setPendingSessionTaskId(sessionId);
    try {
      await overrideMutation.mutateAsync({ sessionId, taskId: taskId || null });
    } finally {
      setPendingSessionTaskId(null);
    }
  };

  const handleWithdraw = async (sessionId: string) => {
    setPendingWithdrawId(sessionId);
    try {
      await withdrawMutation.mutateAsync(sessionId);
    } finally {
      setPendingWithdrawId(null);
    }
  };

  const handleDownload = async (sessionId: string) => {
    try {
      await downloadSessionLog(sessionId);
    } catch {
      message.error("原始日志不可用");
    }
  };

  const taskSelectDisabled = tasksQuery.isLoading || tasksQuery.isError;
  const taskSelectPlaceholder = tasksQuery.isError
    ? "任务加载失败"
    : tasksQuery.isLoading
      ? "任务加载中..."
      : "未匹配";

  return (
    <PagePanel
      title="Session 管理"
      description="查看和上报的 Claude Code session"
      breadcrumbs={[{ title: "Session 管理" }]}
      actions={
        <Space>
          <DatePicker
            value={date}
            onChange={(v) => {
              setDate(v);
              setPage(1);
            }}
            allowClear
            placeholder="按日期筛选"
          />
          {user?.role !== "employee" ? (
            <Text type="secondary" style={{ fontSize: 12 }}>
              共 {total} 条（含团队成员）
            </Text>
          ) : null}
        </Space>
      }
    >
      <Space orientation="vertical" size={16} style={{ width: "100%" }}>
        {sessionsQuery.isError ? (
          <Alert
            type="error"
            showIcon
            message="Session 加载失败"
            description={errorMessage(sessionsQuery.error)}
            action={<Button onClick={() => void sessionsQuery.refetch()}>重试</Button>}
          />
        ) : null}
        {tasksQuery.isError ? (
          <Alert
            type="warning"
            showIcon
            message="任务列表加载失败"
            description="暂时无法修改 Session 的任务关联。"
            action={<Button onClick={() => void tasksQuery.refetch()}>重试任务列表</Button>}
          />
        ) : null}

      <Card size="small" className="sessions-page__card">
        <div className="sessions-page__desktop-table">
        <Table<Session>
          rowKey="id"
          dataSource={sessions}
          loading={sessionsQuery.isLoading}
          pagination={{
            current: page,
            pageSize,
            total,
            showSizeChanger: true,
            pageSizeOptions: [10, 20, 50, 100],
            onChange: (next, size) => {
              setPage(next);
              if (size && size !== pageSize) {
                setPageSize(size);
              }
            }
          }}
          columns={[
            {
              title: "Session",
              key: "session",
              render: (_: unknown, s) => (
                <Space orientation="vertical" size={0}>
                  <Text code style={{ fontSize: 11 }}>
                    {s.session_ref.slice(0, 12)}
                  </Text>
                  {s.summary ? (
                    <Tooltip title={s.summary}>
                      <Text
                        type="secondary"
                        style={{
                          fontSize: 11,
                          maxWidth: 280,
                          display: "inline-block",
                          overflow: "hidden",
                          textOverflow: "ellipsis",
                          whiteSpace: "nowrap",
                          verticalAlign: "middle"
                        }}
                      >
                        {s.summary}
                      </Text>
                    </Tooltip>
                  ) : null}
                </Space>
              )
            },
            {
              title: "开始时间",
              dataIndex: "started_at",
              render: (v: string) => formatDateTime(v),
              width: 130
            },
            {
              title: "上报时间",
              dataIndex: "uploaded_at",
              render: (v: string) => formatDateTime(v),
              width: 130
            },
            {
              title: "模型",
              dataIndex: "model",
              width: 130,
              render: (v: string) => <Tag>{v}</Tag>
            },
            {
              title: "时长",
              dataIndex: "duration_secs",
              render: (v?: number) => formatDuration(v),
              width: 90
            },
            {
              title: "匹配任务",
              key: "task",
              width: 220,
              render: (_: unknown, s) => (
                <Select
                  size="small"
                  style={{ width: "100%" }}
                  value={s.task_id || ""}
                  loading={tasksQuery.isLoading || pendingSessionTaskId === s.id}
                  disabled={taskSelectDisabled || pendingSessionTaskId === s.id}
                  placeholder={taskSelectPlaceholder}
                  onChange={(v) => void handleSessionTaskChange(s.id, v)}
                  options={[
                    { value: "", label: "未匹配" },
                    ...tasks.map((t) => ({ value: t.id, label: t.title }))
                  ]}
                />
              )
            },
            {
              title: "置信度",
              dataIndex: "match_confidence",
              width: 90,
              render: (v?: number) => <ConfidenceTag value={v} />
            },
            {
              title: "操作",
              key: "actions",
              width: 140,
              render: (_: unknown, s) => (
                <Space>
                  {s.has_log_content ? (
                    <Button
                      size="small"
                      icon={<DownloadOutlined />}
                      onClick={() => handleDownload(s.id)}
                    >
                      日志
                    </Button>
                  ) : null}
                  <Popconfirm
                    title="撤回此 session？"
                    description="此操作将永久删除，包括 Token 统计。"
                    okText="确认撤回"
                    okButtonProps={{ danger: true }}
                    cancelText="取消"
                    onConfirm={() => void handleWithdraw(s.id)}
                  >
                    <Button
                      size="small"
                      danger
                      loading={pendingWithdrawId === s.id}
                      disabled={pendingWithdrawId === s.id}
                    >
                      撤回
                    </Button>
                  </Popconfirm>
                </Space>
              )
            }
          ]}
          locale={{
            emptyText: (
              <Empty
                description={
                  dateStr
                    ? `${dateStr} 当日无上报 session`
                    : "暂无 session，使用 CLI 上传：aida upload"
                }
              />
            )
          }}
        />
        </div>
        <div className="sessions-page__mobile-list" aria-label="Session 列表">
          {!sessionsQuery.isLoading && !sessions.length ? (
            <Empty
              image={Empty.PRESENTED_IMAGE_SIMPLE}
              description={dateStr ? `${dateStr} 当日无上报 session` : "暂无 session，使用 CLI 上传：aida upload"}
            />
          ) : null}
          {sessions.map((session) => (
            <article className="sessions-page__mobile-card" key={session.id}>
              <div className="sessions-page__mobile-head">
                <div>
                  <Text code>{session.session_ref.slice(0, 12)}</Text>
                  <p>{session.summary || "暂无摘要"}</p>
                </div>
                <ConfidenceTag value={session.match_confidence} />
              </div>
              <dl className="sessions-page__mobile-meta">
                <div><dt>开始时间</dt><dd>{formatDateTime(session.started_at)}</dd></div>
                <div><dt>上报时间</dt><dd>{formatDateTime(session.uploaded_at)}</dd></div>
                <div><dt>模型</dt><dd>{session.model || "-"}</dd></div>
                <div><dt>时长</dt><dd>{formatDuration(session.duration_secs)}</dd></div>
              </dl>
              <div className="sessions-page__mobile-task">
                <span>关联任务</span>
                <Select
                  size="middle"
                  value={session.task_id || ""}
                  loading={tasksQuery.isLoading || pendingSessionTaskId === session.id}
                  disabled={taskSelectDisabled || pendingSessionTaskId === session.id}
                  placeholder={taskSelectPlaceholder}
                  onChange={(value) => void handleSessionTaskChange(session.id, value)}
                  options={[{ value: "", label: "未匹配" }, ...tasks.map((task) => ({ value: task.id, label: task.title }))]}
                />
              </div>
              <div className="sessions-page__mobile-actions">
                {session.has_log_content ? <Button icon={<DownloadOutlined />} onClick={() => handleDownload(session.id)}>下载日志</Button> : <span />}
                <Popconfirm
                  title="撤回此 session？"
                  description="此操作将永久删除，包括 Token 统计。"
                  okText="确认撤回"
                  okButtonProps={{ danger: true }}
                  cancelText="取消"
                  onConfirm={() => void handleWithdraw(session.id)}
                >
                  <Button danger loading={pendingWithdrawId === session.id} disabled={pendingWithdrawId === session.id}>撤回</Button>
                </Popconfirm>
              </div>
            </article>
          ))}
          {total > pageSize ? (
            <Pagination
              className="sessions-page__mobile-pagination"
              current={page}
              pageSize={pageSize}
              total={total}
              showSizeChanger={false}
              hideOnSinglePage
              onChange={setPage}
            />
          ) : null}
        </div>
      </Card>
      </Space>
    </PagePanel>
  );
}
