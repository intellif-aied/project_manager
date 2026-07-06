import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Alert,
  App,
  Button,
  Card,
  Col,
  InputNumber,
  Progress,
  Result,
  Row,
  Segmented,
  Slider,
  Space,
  Tag,
  Typography
} from "antd";
import dayjs from "dayjs";
import { useMemo, useState } from "react";
import { Link, useLocation, useParams } from "react-router-dom";

import { PagePanel } from "@/shared/components/PagePanel/PagePanel";
import { isEditConflict } from "@/shared/request/apiError";
import { buildListReturnUrl } from "@/shared/utils/urlQuery";

import "../../aidashboard-pattern.css";
import type { TaskDisplayStatus } from "../../api/types";
import { TaskPriorityTag, TaskStatusTag } from "../../dashboard/shared";
import { requirementsBoardApi } from "../../requirements/api/requirementsBoardApi";
import { invalidateRequirementTaskWorkspace } from "../../requirements/queryInvalidation";
import type { MockTask, MockTaskDependency, MockTaskStatus, MockTokenSource } from "../../requirements/types";

const { Text } = Typography;

const TASK_STATUS_OPTIONS: Array<{ label: string; value: MockTaskStatus }> = [
  { label: "未开始", value: "todo" },
  { label: "进行中", value: "in_progress" },
  { label: "已完成", value: "done" }
];

function dependencyKey(dependency: MockTaskDependency) {
  return `${dependency.item_type ?? "task"}:${dependency.item_id || dependency.task_id}`;
}

function dependencyTitle(dependency: MockTaskDependency) {
  return dependency.title || dependency.task_title || dependency.item_id || dependency.task_id || "未命名工作项";
}

function dependencyPath(dependency: MockTaskDependency) {
  const targetId = dependency.item_id || dependency.task_id;
  if ((dependency.item_type ?? "task") === "requirement") {
    return `/requirements/${targetId}`;
  }
  return `/tasks/${targetId}`;
}

function isTaskDependencyStatus(status: string): status is TaskDisplayStatus {
  return status === "todo" || status === "in_progress" || status === "done" || status === "blocked";
}

function DependencyList({ deps, empty }: { deps: MockTaskDependency[]; empty: string }) {
  if (!deps.length) return <Text type="secondary">{empty}</Text>;
  return (
    <Space orientation="vertical" size={6} style={{ width: "100%" }}>
      {deps.map((dependency) => (
        <Space key={dependencyKey(dependency)} size={8}>
          {isTaskDependencyStatus(dependency.status) ? (
            <TaskStatusTag status={dependency.status} />
          ) : (
            <Tag color={dependency.status === "completed" ? "success" : "warning"}>需求</Tag>
          )}
          <Link to={dependencyPath(dependency)}>{dependencyTitle(dependency)}</Link>
        </Space>
      ))}
    </Space>
  );
}

function taskResponsibleLabel(task: Pick<MockTask, "responsible_users" | "responsible_user_ids">) {
  const names = task.responsible_users.map((responsible) => responsible.name || responsible.id).filter(Boolean);
  if (!names.length) return task.responsible_user_ids.length ? task.responsible_user_ids.join("、") : "未分配";
  const visible = names.slice(0, 2);
  const restCount = names.length - visible.length;
  return restCount > 0 ? `${visible.join("、")} +${restCount}` : visible.join("、");
}

function formatTokens(value: number) {
  if (!value) return "0";
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`;
  if (value >= 1_000) return `${Math.round(value / 1_000)}K`;
  return String(value);
}

function formatTokenSourceTime(value: string) {
  return dayjs(value).format("MM-DD HH:mm");
}

function formatDate(value?: string) {
  if (!value) return "-";
  return dayjs(value).format("YYYY-MM-DD");
}

export function TaskDetailPage() {
  const { id = "" } = useParams<{ id: string }>();
  const location = useLocation();
  const queryClient = useQueryClient();
  const { message } = App.useApp();
  const [progressOverride, setProgressOverride] = useState<number | null>(null);
  const backTo = buildListReturnUrl("/requirements", location.search);

  const taskQuery = useQuery({
    queryKey: ["requirements-board", "task", id],
    queryFn: () => requirementsBoardApi.getTask(id),
    enabled: Boolean(id)
  });
  const task = taskQuery.data;

  const tokenSourcesQuery = useQuery({
    queryKey: ["requirements-board", "token-sources"],
    queryFn: () => requirementsBoardApi.listTokenSources(),
    staleTime: 60_000
  });
  const tokenSourceMap = useMemo(
    () =>
      new Map(
        (tokenSourcesQuery.data ?? []).map((source: MockTokenSource) => [source.id, source])
      ),
    [tokenSourcesQuery.data]
  );

  const statusMutation = useMutation({
    mutationFn: (status: MockTaskStatus) =>
      requirementsBoardApi.updateTaskStatus(id, status, task?.version ?? 0),
    onSuccess: (updated) => {
      message.success("任务状态已更新");
      queryClient.setQueryData(["requirements-board", "task", id], updated);
      void invalidateRequirementTaskWorkspace(queryClient, {
        requirementId: updated.requirement_id,
        taskId: updated.id
      });
    },
    onError: (error) => {
      if (isEditConflict(error)) {
        message.warning("内容已被其他人更新，请刷新后再操作");
        void invalidateRequirementTaskWorkspace(queryClient, {
          requirementId: task?.requirement_id,
          taskId: id
        });
        return;
      }
      message.error(error instanceof Error ? error.message : "状态更新失败");
    }
  });

  const progressMutation = useMutation({
    mutationFn: (nextProgress: number) =>
      requirementsBoardApi.updateTaskProgress(id, nextProgress, task?.version ?? 0),
    onSuccess: (updated) => {
      setProgressOverride(null);
      message.success("任务进度已保存");
      queryClient.setQueryData(["requirements-board", "task", id], updated);
      void invalidateRequirementTaskWorkspace(queryClient, {
        requirementId: updated.requirement_id,
        taskId: updated.id
      });
    },
    onError: (error) => {
      if (isEditConflict(error)) {
        message.warning("内容已被其他人更新，请刷新后再操作");
        void invalidateRequirementTaskWorkspace(queryClient, {
          requirementId: task?.requirement_id,
          taskId: id
        });
        return;
      }
      message.error(error instanceof Error ? error.message : "进度保存失败");
    }
  });

  const requestStatusChange = (status: MockTaskStatus) => {
    if (status === task?.status || statusMutation.isPending) return;
    statusMutation.mutate(status);
  };

  if (!id) {
    return (
      <Result
        status="404"
        title="任务不存在"
        subTitle="缺少有效的任务 ID。"
        extra={<Link to={backTo}>返回需求看板</Link>}
      />
    );
  }
  if (taskQuery.isLoading) return <Card loading />;
  if (taskQuery.isError) {
    return (
      <Alert
        type="error"
        showIcon
        message="任务加载失败"
        description={taskQuery.error instanceof Error ? taskQuery.error.message : "请稍后重试"}
        action={<Button onClick={() => void taskQuery.refetch()}>重试</Button>}
      />
    );
  }
  if (!task) {
    return (
      <Result
        status="404"
        title="任务不存在"
        subTitle="未找到该任务或当前用户无权查看。"
        extra={<Link to={backTo}>返回需求看板</Link>}
      />
    );
  }

  const dependencyBlocked = task.risk_types?.includes("blocked") ?? false;
  const progress = progressOverride ?? task.progress;
  const canUpdateStatus = Boolean(task.can_update_status);
  const canUpdateProgress = Boolean(task.can_update_progress);

  const linkedSources = task.token_source_ids
    .map((id) => tokenSourceMap.get(id))
    .filter((source): source is MockTokenSource => Boolean(source));
  const linkedTotal = linkedSources.reduce((total, source) => total + source.token, 0);
  const statusActions = canUpdateStatus ? (
    <div className="aidashboard-task-detail__status-control">
      <span>任务状态</span>
      <Segmented
        disabled={statusMutation.isPending}
        options={TASK_STATUS_OPTIONS}
        value={task.status}
        onChange={(value) => {
          requestStatusChange(value as MockTaskStatus);
        }}
      />
    </div>
  ) : null;

  return (
    <PagePanel
      title={task.title}
      description={`所属需求：${task.requirement_title} · 负责人：${taskResponsibleLabel(task)}`}
      backTo={backTo}
      actions={statusActions}
      breadcrumbs={[
        { title: "业务" },
        { title: "需求看板", path: backTo },
        { title: task.title }
      ]}
    >
      <div className="aidashboard-task-detail">
        {dependencyBlocked ? (
          <Alert
            type="warning"
            showIcon
            message="上游依赖未完成"
            description="当前任务暂不能推进。"
          />
        ) : null}

        <section className="aidashboard-task-detail__overview">
          <div className="aidashboard-task-detail__overview-main">
            <div className="aidashboard-task-detail__status-line">
              <TaskStatusTag status={task.status} />
              <TaskPriorityTag priority={task.priority} />
            </div>
            <div>
              <span>任务进度</span>
              <strong>{progress}%</strong>
            </div>
            <Progress percent={progress} showInfo={false} />
          </div>
          <div className="aidashboard-task-detail__meta-grid">
            <div>
              <span>负责人</span>
              <strong>{taskResponsibleLabel(task)}</strong>
            </div>
            <div>
              <span>截止日期</span>
              <strong>{formatDate(task.due_date)}</strong>
            </div>
            <div>
              <span>工作记录</span>
              <strong>{linkedTotal > 0 ? `${formatTokens(linkedTotal)} Token` : "暂无"}</strong>
            </div>
          </div>
        </section>

        <Row gutter={[16, 16]}>
          <Col xs={24} lg={12}>
            <Card size="small" title="验收标准" className="aidashboard-task-detail__card">
              {task.acceptance_criteria.length ? (
                <div className="aidashboard-task-detail__criteria">
                  {task.acceptance_criteria.map((criterion, index) => (
                    <div key={`${index}-${criterion}`}>
                      <span>{index + 1}</span>
                      <p>{criterion}</p>
                    </div>
                  ))}
                </div>
              ) : (
                <Text type="secondary">暂无任务验收标准</Text>
              )}
            </Card>
          </Col>
          <Col xs={24} lg={12}>
            <Card size="small" title="依赖关系" className="aidashboard-task-detail__card">
              <Space orientation="vertical" size={12} style={{ width: "100%" }}>
                <div>
                  <Text type="secondary" className="aidashboard-task-detail__label">
                    依赖于
                  </Text>
                  <DependencyList deps={task.dependencies} empty="无上游依赖" />
                </div>
                {task.blocking.length ? (
                  <div>
                    <Text type="secondary" className="aidashboard-task-detail__label">
                      阻塞了
                    </Text>
                    <DependencyList deps={task.blocking} empty="" />
                  </div>
                ) : null}
              </Space>
            </Card>
          </Col>
        </Row>

        <Card size="small" title="工作记录" className="aidashboard-task-detail__card">
          {linkedSources.length ? (
            <Space orientation="vertical" size={8} style={{ width: "100%" }}>
              {linkedSources.map((source) => (
                <div key={source.id} className="aidashboard-task-detail__record">
                  <div>
                    <strong>{source.summary || "（无摘要）"}</strong>
                    <span>{formatTokenSourceTime(source.recorded_at)} · {source.tool} · {source.uploader}</span>
                  </div>
                  <span>{formatTokens(source.token)} Token</span>
                </div>
              ))}
            </Space>
          ) : (
            <Text type="secondary">暂无关联工作记录。</Text>
          )}
        </Card>

        <Card title="任务进度" className="aidashboard-task-detail__progress-card aidashboard-task-detail__card">
          <p>{canUpdateProgress ? "拖动滑块或输入百分比后保存。" : "当前任务为只读。"} </p>
          <div className="aidashboard-task-detail__progress-editor">
            <Slider
              className="aidashboard-task-detail__progress-slider"
              min={0}
              max={100}
              value={progress}
              disabled={!canUpdateProgress}
              onChange={setProgressOverride}
            />
            <Space.Compact>
              <InputNumber
                min={0}
                max={100}
                value={progress}
                disabled={!canUpdateProgress}
                onChange={(value) => setProgressOverride(value ?? 0)}
              />
              <Button disabled>%</Button>
            </Space.Compact>
            <Button
              type="primary"
              loading={progressMutation.isPending}
              disabled={!canUpdateProgress || progress === task.progress}
              onClick={() => progressMutation.mutate(progress)}
            >
              保存进度
            </Button>
          </div>
        </Card>
      </div>
    </PagePanel>
  );
}
