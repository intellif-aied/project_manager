import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Alert,
  App,
  Button,
  Card,
  DatePicker,
  Empty,
  Popconfirm,
  Segmented,
  Select,
  Space,
  Tag,
  Typography
} from "antd";
import { FileTextOutlined, PlusOutlined, RobotOutlined } from "@ant-design/icons";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useState } from "react";
import dayjs from "dayjs";

import {
  deleteDocument,
  downloadSessionLog,
  fetchDocuments,
  fetchSessions,
  fetchTasks,
  updateSessionTask,
  withdrawSession
} from "../../api/client";
import type { Document, PaginatedSessions, Session, Task } from "../../api/types";
import { useAuth } from "@/shared/auth/authContext";
import { PagePanel } from "@/shared/components/PagePanel/PagePanel";
import { ResourceTable } from "@/shared/components/ResourceTable/ResourceTable";
import { appendSearch } from "@/shared/utils/urlQuery";
import { invalidateRequirementTaskWorkspace } from "../../requirements/queryInvalidation";
import "./ProductsPage.css";

const { Text } = Typography;

function formatDateTime(iso: string): string {
  const d = new Date(iso);
  return `${d.toLocaleDateString("zh-CN", { month: "2-digit", day: "2-digit" })} ${d.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" })}`;
}

function formatDuration(secs?: number): string {
  if (!secs) return "时长未知";
  return `${Math.floor(secs / 60)}分${secs % 60}秒`;
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : "请稍后重试";
}

export function ProductsPage() {
  const { user } = useAuth();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { message } = App.useApp();
  const [searchParams, setSearchParams] = useSearchParams();
  const dateParam = searchParams.get("date");
  const tabParam = searchParams.get("tab");
  const date = dateParam ? dayjs(dateParam) : null;
  const [pendingSessionTaskId, setPendingSessionTaskId] = useState<string | null>(null);
  const [pendingWithdrawId, setPendingWithdrawId] = useState<string | null>(null);

  const isManager = Boolean(
    user &&
      (user.role === "team_leader" ||
        user.role === "pm" ||
        user.role === "director" ||
        user.role === "admin")
  );
  const tab: "mine" | "team" = tabParam === "team" && isManager ? "team" : "mine";

  const updateParam = (key: string, value: string) => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (value) next.set(key, value);
        else next.delete(key);
        return next;
      },
      { replace: true }
    );
  };

  const dateStr = date?.format("YYYY-MM-DD") || "";

  const sessionsQuery = useQuery<PaginatedSessions>({
    queryKey: ["sessions", { date: dateStr, page: 1, pageSize: 100 }],
    queryFn: () =>
      fetchSessions({
        ...(dateStr ? { date: dateStr } : {}),
        page: "1",
        page_size: "100"
      }),
    staleTime: 30_000
  });
  const documentsQuery = useQuery<Document[]>({
    queryKey: ["documents", { date: dateStr }],
    queryFn: () => fetchDocuments(dateStr ? { date: dateStr } : undefined),
    staleTime: 30_000
  });
  const tasksQuery = useQuery<Task[]>({
    queryKey: ["tasks"],
    queryFn: () => fetchTasks(),
    staleTime: 60_000
  });

  const sessions = sessionsQuery.data?.items ?? [];
  const documents = documentsQuery.data ?? [];
  const tasks = tasksQuery.data ?? [];

  const visibleDocuments = isManager
    ? documents.filter((d) => (tab === "mine" ? d.user_id === user?.id : d.user_id !== user?.id))
    : documents;
  const visibleSessions = isManager
    ? sessions.filter((s) => (tab === "mine" ? s.user_id === user?.id : s.user_id !== user?.id))
    : sessions;

  const sortedDocuments = [...visibleDocuments].sort(
    (a, b) => new Date(b.uploaded_at).getTime() - new Date(a.uploaded_at).getTime()
  );
  const sortedSessions = [...visibleSessions].sort(
    (a, b) => new Date(b.uploaded_at).getTime() - new Date(a.uploaded_at).getTime()
  );

  const deleteDocMutation = useMutation({
    mutationFn: (id: string) => deleteDocument(id),
    onSuccess: () => {
      message.success("已删除");
      void queryClient.invalidateQueries({ queryKey: ["documents"] });
    },
    onError: (err: unknown) => message.error(err instanceof Error ? err.message : "删除失败")
  });

  const overrideTaskMutation = useMutation({
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
      await overrideTaskMutation.mutateAsync({ sessionId, taskId: taskId || null });
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
      : "无关联任务";

  return (
    <PagePanel
      title="我的工作"
      description="沉淀工作文档，并自动关联 AI 工作记录"
      breadcrumbs={[{ title: "我的工作" }]}
      actions={
        <Space>
          <Button
            icon={<PlusOutlined />}
            onClick={() =>
              navigate(appendSearch("/products/documents/create", searchParams))
            }
          >
            添加文档
          </Button>
          <DatePicker
            value={date}
            onChange={(v) => updateParam("date", v ? v.format("YYYY-MM-DD") : "")}
            allowClear
            placeholder="按日期筛选"
          />
        </Space>
      }
    >
      <Space orientation="vertical" size={16} style={{ width: "100%" }}>

      {isManager ? (
        <Segmented
          value={tab}
          onChange={(v) => updateParam("tab", String(v))}
          options={[
            { label: "我的工作", value: "mine" },
            { label: "团队工作", value: "team" }
          ]}
        />
      ) : null}

      <Card
        title={
          <Space>
            <FileTextOutlined />
            <span>文档</span>
            <Tag color="blue">{sortedDocuments.length}</Tag>
          </Space>
        }
        size="small"
      >
        {documentsQuery.isError ? (
          <Alert
            type="error"
            showIcon
            message="文档加载失败"
            description={errorMessage(documentsQuery.error)}
            action={<Button onClick={() => void documentsQuery.refetch()}>重试</Button>}
          />
        ) : (
          <div className="products-page__desktop-table">
          <ResourceTable<Document>
            rowKey="id"
            dataSource={sortedDocuments.slice(0, 8)}
            pagination={false}
            loading={documentsQuery.isLoading}
            size="small"
            locale={{
              emptyText: (
                <Empty
                  description={dateStr ? `${dateStr} 无上传文档` : "暂无文档"}
                  image={Empty.PRESENTED_IMAGE_SIMPLE}
                />
              )
            }}
            columns={[
              {
                title: "标题",
                dataIndex: "title",
                render: (title: string, d) => (
                  <Space orientation="vertical" size={0}>
                    <a href={d.url} target="_blank" rel="noreferrer">
                      {title}
                    </a>
                    <Text type="secondary" style={{ fontSize: 11 }}>
                      {formatDateTime(d.uploaded_at)}
                      {d.description ? ` · ${d.description}` : ""}
                    </Text>
                  </Space>
                )
              },
              {
                title: "任务",
                dataIndex: "task_title",
                width: 180,
                render: (v?: string) => v || <Text type="secondary">-</Text>
              },
              {
                title: "操作",
                key: "actions",
                width: 80,
                render: (_: unknown, d) => (
                  <Popconfirm
                    title="删除此文档？"
                    okText="删除"
                    okButtonProps={{ danger: true }}
                    cancelText="取消"
                    onConfirm={() => deleteDocMutation.mutate(d.id)}
                  >
                    <Button size="small" type="link" danger>
                      删除
                    </Button>
                  </Popconfirm>
                )
              }
            ]}
          />
          </div>
        )}
        {!documentsQuery.isError ? (
          <div className="products-page__mobile-list" aria-label="文档列表">
            {!documentsQuery.isLoading && !sortedDocuments.length ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={dateStr ? `${dateStr} 无上传文档` : "暂无文档"} /> : null}
            {sortedDocuments.slice(0, 8).map((document) => (
              <article className="products-page__mobile-card" key={document.id}>
                <div className="products-page__mobile-head">
                  <a href={document.url} target="_blank" rel="noreferrer">{document.title}</a>
                  <Popconfirm title="删除此文档？" okText="删除" okButtonProps={{ danger: true }} cancelText="取消" onConfirm={() => deleteDocMutation.mutate(document.id)}>
                    <Button size="small" type="link" danger>删除</Button>
                  </Popconfirm>
                </div>
                {document.description ? <p>{document.description}</p> : null}
                <div className="products-page__mobile-foot">
                  <span>{formatDateTime(document.uploaded_at)}</span>
                  <span>{document.task_title || "未关联任务"}</span>
                </div>
              </article>
            ))}
          </div>
        ) : null}
        {sortedDocuments.length > 8 ? (
          <div style={{ marginTop: 8 }}>
            <Text type="secondary" style={{ fontSize: 12 }}>
              还有 {sortedDocuments.length - 8} 条文档...
            </Text>
          </div>
        ) : null}
      </Card>

      <Card
        title={
          <Space>
            <RobotOutlined />
            <span>AI 工作记录</span>
            <Tag color="purple">{sortedSessions.length}</Tag>
          </Space>
        }
        size="small"
      >
        {sessionsQuery.isError ? (
          <Alert
            type="error"
            showIcon
            message="AI 工作记录加载失败"
            description={errorMessage(sessionsQuery.error)}
            action={<Button onClick={() => void sessionsQuery.refetch()}>重试</Button>}
          />
        ) : (
          <div className="products-page__desktop-table">
          <ResourceTable<Session>
            rowKey="id"
            dataSource={sortedSessions.slice(0, 8)}
            pagination={false}
            loading={sessionsQuery.isLoading}
            size="small"
            locale={{
              emptyText: (
                <Empty
                  description={
                    dateStr
                      ? `${dateStr} 无 AI 工作记录`
                      : "暂无 AI 工作记录，使用 aida upload 上传"
                  }
                  image={Empty.PRESENTED_IMAGE_SIMPLE}
                />
              )
            }}
            columns={[
              {
                title: "记录",
                key: "session",
                render: (_: unknown, s) => (
                  <Space orientation="vertical" size={0}>
                    <Text strong>{s.summary || s.session_ref.slice(0, 12)}</Text>
                    <Text type="secondary" style={{ fontSize: 11 }}>
                      {s.model || "Claude Code"} · {formatDuration(s.duration_secs)}
                      {user?.role !== "employee" ? ` · ${s.user_name}` : ""}
                    </Text>
                  </Space>
                )
              },
              {
                title: "上报时间",
                dataIndex: "uploaded_at",
                width: 130,
                render: (v: string) => formatDateTime(v)
              },
              {
                title: "任务",
                key: "task",
                width: 200,
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
                      { value: "", label: "无关联任务" },
                      ...tasks.map((t) => ({ value: t.id, label: t.title }))
                    ]}
                  />
                )
              },
              {
                title: "操作",
                key: "actions",
                width: 140,
                render: (_: unknown, s) => (
                  <Space>
                    {s.has_log_content ? (
                      <Button size="small" onClick={() => handleDownload(s.id)}>
                        日志
                      </Button>
                    ) : null}
                    <Popconfirm
                      title="撤回此 AI 工作记录？"
                      okText="撤回"
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
          />
          </div>
        )}
        {!sessionsQuery.isError ? (
          <div className="products-page__mobile-list" aria-label="AI 工作记录列表">
            {!sessionsQuery.isLoading && !sortedSessions.length ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={dateStr ? `${dateStr} 无 AI 工作记录` : "暂无 AI 工作记录，使用 aida upload 上传"} /> : null}
            {sortedSessions.slice(0, 8).map((session) => (
              <article className="products-page__mobile-card" key={session.id}>
                <div className="products-page__mobile-head">
                  <div>
                    <strong>{session.summary || session.session_ref.slice(0, 12)}</strong>
                    <span>{session.model || "Claude Code"} · {formatDuration(session.duration_secs)}{user?.role !== "employee" ? ` · ${session.user_name}` : ""}</span>
                  </div>
                  <span className="products-page__mobile-time">{formatDateTime(session.uploaded_at)}</span>
                </div>
                <div className="products-page__mobile-task">
                  <span>关联任务</span>
                  <Select
                    value={session.task_id || ""}
                    loading={tasksQuery.isLoading || pendingSessionTaskId === session.id}
                    disabled={taskSelectDisabled || pendingSessionTaskId === session.id}
                    placeholder={taskSelectPlaceholder}
                    onChange={(value) => void handleSessionTaskChange(session.id, value)}
                    options={[{ value: "", label: "无关联任务" }, ...tasks.map((task) => ({ value: task.id, label: task.title }))]}
                  />
                </div>
                <div className="products-page__mobile-actions">
                  {session.has_log_content ? <Button onClick={() => handleDownload(session.id)}>下载日志</Button> : <span />}
                  <Popconfirm title="撤回此 AI 工作记录？" okText="撤回" okButtonProps={{ danger: true }} cancelText="取消" onConfirm={() => void handleWithdraw(session.id)}>
                    <Button danger loading={pendingWithdrawId === session.id} disabled={pendingWithdrawId === session.id}>撤回</Button>
                  </Popconfirm>
                </div>
              </article>
            ))}
          </div>
        ) : null}
        {sortedSessions.length > 8 ? (
          <div style={{ marginTop: 8 }}>
            <Text type="secondary" style={{ fontSize: 12 }}>
              还有 {sortedSessions.length - 8} 条 AI 工作记录...
            </Text>
          </div>
        ) : null}
      </Card>
      </Space>
    </PagePanel>
  );
}
