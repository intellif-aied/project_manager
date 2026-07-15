import { PlusOutlined, ReloadOutlined } from "@ant-design/icons";
import { Alert, Button, Empty, Pagination, Select } from "antd";
import type { TableProps } from "antd";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";

import { fetchTasks } from "../../api/client";
import type { Task, TaskPriority, TaskStatus } from "../../api/types";
import { useAuth } from "@/shared/auth/authContext";
import { PagePanel } from "@/shared/components/PagePanel/PagePanel";
import { ResourceActions, ResourceTable } from "@/shared/components/ResourceTable/ResourceTable";
import { TableLayout } from "@/shared/components/TableLayout/TableLayout";
import { appendSearch } from "@/shared/utils/urlQuery";

import "../../aidashboard-pattern.css";
import { TaskPriorityTag, TaskStatusTag } from "../../dashboard/shared";
import "./TasksListPage.css";

function taskResponsibleLabel(task: Pick<Task, "responsible_users" | "responsible_user_ids">) {
  const names = (task.responsible_users ?? []).map((responsible) => responsible.name || responsible.id).filter(Boolean);
  if (!names.length) return task.responsible_user_ids?.length ? task.responsible_user_ids.join("、") : "-";
  const visible = names.slice(0, 2);
  const restCount = names.length - visible.length;
  return restCount > 0 ? `${visible.join("、")} +${restCount}` : visible.join("、");
}

function taskDueDateLabel(value?: string) {
  return value ? value.slice(0, 10) : "未设置";
}

const STATUS_FILTER_OPTIONS: Array<{ value: TaskStatus; label: string }> = [
  { value: "todo", label: "未开始" },
  { value: "in_progress", label: "进行中" },
  { value: "done", label: "已完成" }
];

export function TasksListPage() {
  const { user } = useAuth();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const statusFilter = (searchParams.get("status") as TaskStatus | null) ?? "";
  const [mobilePage, setMobilePage] = useState(1);
  const mobilePageSize = 10;

  const updateParam = (key: string, value: string) => {
    setMobilePage(1);
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

  const canCreate = Boolean(
    user &&
      (user.role === "team_leader" ||
        user.role === "pm" ||
        user.role === "director" ||
        user.role === "admin" ||
        user.role === "employee")
  );

  const tasksQuery = useQuery<Task[]>({
    queryKey: ["tasks", { status: statusFilter }],
    queryFn: () => fetchTasks(statusFilter ? { status: statusFilter } : undefined),
    staleTime: 30_000
  });
  const tasks = tasksQuery.data ?? [];
  const mobileTasks = tasks.slice((mobilePage - 1) * mobilePageSize, mobilePage * mobilePageSize);

  const columns: TableProps<Task>["columns"] = [
    {
      title: "任务",
      dataIndex: "title",
      render: (title: string, t) => (
        <Link to={appendSearch(`/tasks/${t.id}`, searchParams)}>{title}</Link>
      )
    },
    {
      title: "所属需求",
      dataIndex: "requirement_title",
      render: (v?: string) => v || "-",
      width: 220
    },
    { title: "负责人", key: "responsibles", render: (_, task) => taskResponsibleLabel(task), width: 140 },
    {
      title: "状态",
      dataIndex: "status",
      render: (s: TaskStatus) => <TaskStatusTag status={s} />,
      width: 110
    },
    {
      title: "优先级",
      dataIndex: "priority",
      render: (p: TaskPriority) => <TaskPriorityTag priority={p} />,
      width: 100
    },
    { title: "截止", dataIndex: "due_date", render: (v?: string) => v || "-", width: 130 },
    {
      title: "操作",
      key: "actions",
      width: 120,
      render: (_, record) => (
        <ResourceActions
          actions={[
            {
              key: "detail",
              label: "详情",
              onClick: () => navigate(appendSearch(`/tasks/${record.id}`, searchParams))
            }
          ]}
        />
      )
    }
  ];

  return (
    <PagePanel
      title="任务"
      className="aidashboard-list"
      description="查看任务分配、优先级、状态和截止时间"
      breadcrumbs={[{ title: "任务" }]}
      actions={
        <Button
          icon={<ReloadOutlined />}
          loading={tasksQuery.isFetching}
          onClick={() => void tasksQuery.refetch()}
        >
          刷新
        </Button>
      }
    >
      <TableLayout
        operations={
          canCreate ? (
            <Button
              className="aidashboard-list__create"
              type="primary"
              icon={<PlusOutlined />}
              onClick={() => navigate(appendSearch("/tasks/create", searchParams))}
            >
              创建任务
            </Button>
          ) : null
        }
        search={
          <TableLayout.SearchGroup>
            <TableLayout.SelectItem size="md">
              <Select
                style={{ width: "100%" }}
                value={statusFilter || undefined}
                placeholder="全部状态"
                allowClear
                onChange={(v) => updateParam("status", (v ?? "") as string)}
                options={STATUS_FILTER_OPTIONS}
              />
            </TableLayout.SelectItem>
          </TableLayout.SearchGroup>
        }
      >
        {tasksQuery.isError ? (
          <Alert
            type="error"
            showIcon
            message="任务列表加载失败"
            description={
              tasksQuery.error instanceof Error ? tasksQuery.error.message : "请稍后重试"
            }
            action={<Button onClick={() => void tasksQuery.refetch()}>重试</Button>}
          />
        ) : null}
        <div className="tasks-list__desktop-table">
          <ResourceTable<Task>
            rowKey="id"
            columns={columns}
            dataSource={tasks}
            loading={tasksQuery.isLoading}
            pagination={{ pageSize: 20, showSizeChanger: false }}
          />
        </div>
        <div className="tasks-list__mobile" aria-label="任务列表">
          {!tasksQuery.isLoading && !mobileTasks.length ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无任务" /> : null}
          {mobileTasks.map((task) => (
            <article className="tasks-list__mobile-card" key={task.id}>
              <div className="tasks-list__mobile-head">
                <Link to={appendSearch(`/tasks/${task.id}`, searchParams)}>{task.title}</Link>
                <TaskStatusTag status={task.status} />
              </div>
              <p className="tasks-list__mobile-requirement">{task.requirement_title || "未关联需求"}</p>
              <dl className="tasks-list__mobile-meta">
                <div><dt>负责人</dt><dd>{taskResponsibleLabel(task)}</dd></div>
                <div><dt>优先级</dt><dd><TaskPriorityTag priority={task.priority} /></dd></div>
                <div><dt>截止时间</dt><dd>{taskDueDateLabel(task.due_date)}</dd></div>
              </dl>
              <Button block onClick={() => navigate(appendSearch(`/tasks/${task.id}`, searchParams))}>查看详情</Button>
            </article>
          ))}
          {tasks.length > mobilePageSize ? (
            <Pagination
              current={mobilePage}
              pageSize={mobilePageSize}
              total={tasks.length}
              showSizeChanger={false}
              hideOnSinglePage
              onChange={setMobilePage}
            />
          ) : null}
        </div>
      </TableLayout>
    </PagePanel>
  );
}
