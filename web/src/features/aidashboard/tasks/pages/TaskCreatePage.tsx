import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Alert,
  Button,
  DatePicker,
  Form,
  Input,
  Select,
  Spin
} from "antd";
import dayjs from "dayjs";
import { useEffect, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";

import { FormPageWrap } from "@/shared/components/FormPageWrap/FormPageWrap";
import { FormSubmitButton } from "@/shared/components/FormSubmitButton/FormSubmitButton";
import { useAuth } from "@/shared/auth/authContext";
import { PagePanel } from "@/shared/components/PagePanel/PagePanel";
import { useFormLeaveConfirm } from "@/shared/hooks/useFormLeaveConfirm";
import { buildCreateSuccessUrl } from "@/shared/utils/urlQuery";

import "../../aidashboard-pattern.css";
import { AcceptanceCriteriaEditor } from "../../requirements/components/AcceptanceCriteriaEditor";
import { requirementsBoardApi } from "../../requirements/api/requirementsBoardApi";
import { invalidateRequirementTaskWorkspace } from "../../requirements/queryInvalidation";
import type { MockTaskPriority } from "../../requirements/types";
import {
  acceptanceCriteriaRules,
  dependencyArrayRules,
  normalizeCriteria,
  normalizeRequiredText,
  requiredSelectRules,
  titleRules
} from "../../requirements/validation/requirementTaskValidation";

interface CreateTaskFormValues {
  title: string;
  requirement_id: string;
  responsible_user_ids: string[];
  priority: MockTaskPriority;
  due_date?: dayjs.Dayjs;
  dependency_task_ids?: string[];
  acceptance_criteria?: string[];
}

export function TaskCreatePage() {
  const navigate = useNavigate();
  const location = useLocation();
  const queryClient = useQueryClient();
  const { user } = useAuth();
  const [form] = Form.useForm<CreateTaskFormValues>();
  const [formError, setFormError] = useState<string>();
  const initialRequirementId =
    new URLSearchParams(location.search).get("requirement_id") ?? undefined;
  const backTo = buildCreateSuccessUrl(
    initialRequirementId ? "/requirements" : "/tasks",
    location.search
  );

  const requirementsQuery = useQuery({
    queryKey: ["requirements-board", "requirements"],
    queryFn: () => requirementsBoardApi.listRequirements(),
    staleTime: 60_000
  });
  const assigneesQuery = useQuery({
    queryKey: ["requirements-board", "assignees"],
    queryFn: () => requirementsBoardApi.listAssignees(),
    staleTime: 5 * 60_000
  });
  const tasksQuery = useQuery({
    queryKey: ["requirements-board", "tasks"],
    queryFn: () => requirementsBoardApi.listTasks(),
    staleTime: 30_000
  });

  const requirements = requirementsQuery.data ?? [];
  const assignees =
    user?.role === "employee"
      ? (assigneesQuery.data ?? []).filter((item) => item.id === user.id)
      : assigneesQuery.data ?? [];
  const allTasks = tasksQuery.data ?? [];
  const selectedRequirementId = Form.useWatch("requirement_id", form);
  const dependencyOptions = allTasks
    .filter((task) => task.requirement_id === selectedRequirementId)
    .map((task) => ({ value: task.id, label: task.title }));

  const createMutation = useMutation({
    mutationFn: (values: CreateTaskFormValues) =>
      requirementsBoardApi.createTask({
        requirement_id: values.requirement_id,
        title: normalizeRequiredText(values.title),
        acceptance_criteria: normalizeCriteria(values.acceptance_criteria),
        responsible_user_ids: values.responsible_user_ids ?? [],
        priority: values.priority,
        due_date: values.due_date?.format("YYYY-MM-DD"),
        dependency_task_ids: values.dependency_task_ids
      })
  });
  const submitting = createMutation.isPending;
  const { markClean, markDirty, confirmLeave } = useFormLeaveConfirm({ form, submitting });
  const handleNavigate = (url: string) => confirmLeave(() => navigate(url));
  const handleCancel = () => handleNavigate(backTo);

  useEffect(() => {
    form.setFieldsValue({
      priority: "medium",
      dependency_task_ids: [],
      requirement_id: initialRequirementId,
      responsible_user_ids: user?.role === "employee" && user.id ? [user.id] : []
    });
    markClean();
  }, [form, initialRequirementId, markClean, user?.id, user?.role]);

  const handleSubmit = async (values: CreateTaskFormValues) => {
    setFormError(undefined);
    try {
      const created = await createMutation.mutateAsync(values);
      await invalidateRequirementTaskWorkspace(queryClient, {
        requirementId: created.requirement_id,
        taskId: created.id
      });
      markClean();
      navigate(`/tasks/${created.id}`, { replace: true });
    } catch (error) {
      setFormError(error instanceof Error ? error.message : "创建任务失败，请稍后重试");
    }
  };

  return (
    <PagePanel
      title="创建任务"
      description="将需求拆解为可执行任务，并关联负责人和上游依赖"
      className="aidashboard-form-page"
      backTo={backTo}
      onBack={handleCancel}
      onNavigate={handleNavigate}
      breadcrumbs={[
        { title: "业务" },
        { title: "需求看板", path: "/requirements" },
        { title: "创建任务" }
      ]}
    >
      <FormPageWrap className="aidashboard-form-wrap" maxWidth="100%" density="cozy" card>
        <Spin spinning={submitting}>
          {formError ? (
            <Alert className="aidashboard-form__error" type="error" showIcon message={formError} />
          ) : null}
          {requirementsQuery.isError || assigneesQuery.isError ? (
            <Alert
              className="aidashboard-form__error"
              type="error"
              showIcon
              message="创建任务所需数据加载失败"
              action={
                <Button
                  onClick={() => {
                    void requirementsQuery.refetch();
                    void assigneesQuery.refetch();
                  }}
                >
                  重试
                </Button>
              }
            />
          ) : null}
          <Form
            form={form}
            labelCol={{ flex: "104px" }}
            wrapperCol={{ flex: "1" }}
            labelAlign="left"
            onFinish={handleSubmit}
            onValuesChange={markDirty}
            onFieldsChange={markDirty}
          >
            <section className="aidashboard-form__section">
              <div className="aidashboard-form__section-head">
                <h2>任务信息</h2>
                <p>任务进度创建后在任务详情中维护。</p>
              </div>
              <div className="aidashboard-form__grid">
                <Form.Item
                  className="aidashboard-form__full-row"
                  label="任务标题"
                  name="title"
                  rules={titleRules("任务标题")}
                >
                  <Input placeholder="例如：实现日报聚合接口" />
                </Form.Item>
                <Form.Item
                  label="所属需求"
                  name="requirement_id"
                  rules={requiredSelectRules("需求")}
                >
                  <Select
                    placeholder="选择需求"
                    loading={requirementsQuery.isLoading}
                    disabled={requirementsQuery.isLoading || requirementsQuery.isError}
                    showSearch
                    optionFilterProp="label"
                    options={requirements
                      .filter((item) => item.can_create_task)
                      .map((item) => ({ value: item.id, label: item.title }))}
                  />
                </Form.Item>
                <Form.Item
                  label="负责人"
                  name="responsible_user_ids"
                  rules={requiredSelectRules("负责人")}
                >
                  <Select
                    mode="multiple"
                    placeholder="选择负责人"
                    loading={assigneesQuery.isLoading}
                    disabled={
                      user?.role === "employee" ||
                      assigneesQuery.isLoading ||
                      assigneesQuery.isError
                    }
                    options={assignees.map((item) => ({
                      value: item.id,
                      label: item.name
                    }))}
                  />
                </Form.Item>
                <Form.Item
                  label="优先级"
                  name="priority"
                  rules={requiredSelectRules("优先级")}
                >
                  <Select
                    options={[
                      { value: "low", label: "低" },
                      { value: "medium", label: "中" },
                      { value: "high", label: "高" }
                    ]}
                  />
                </Form.Item>
                <Form.Item label="截止日期" name="due_date">
                  <DatePicker />
                </Form.Item>
                <Form.Item
                  className="aidashboard-form__full-row"
                  label="上游依赖"
                  name="dependency_task_ids"
                  rules={dependencyArrayRules()}
                  tooltip="选择当前任务依赖的同需求下其它任务，被依赖任务未完成时本任务会显示为阻塞"
                >
                  <Select
                    mode="multiple"
                    placeholder={
                      selectedRequirementId
                        ? dependencyOptions.length
                          ? "选择上游依赖任务"
                          : "当前需求暂无可选任务"
                        : "先选择所属需求"
                    }
                    disabled={!selectedRequirementId || !dependencyOptions.length}
                    options={dependencyOptions}
                    allowClear
                  />
                </Form.Item>
                <Form.Item
                  className="aidashboard-form__full-row"
                  label="验收标准"
                  name="acceptance_criteria"
                  rules={acceptanceCriteriaRules()}
                >
                  <AcceptanceCriteriaEditor placeholder="例如：接口返回字段符合前端展示需要" />
                </Form.Item>
              </div>
            </section>

            <FormSubmitButton
              submitText="创建任务"
              loading={submitting}
              disabled={
                requirementsQuery.isLoading ||
                assigneesQuery.isLoading ||
                requirementsQuery.isError ||
                assigneesQuery.isError
              }
              onCancel={handleCancel}
              sticky
            />
          </Form>
        </Spin>
      </FormPageWrap>
    </PagePanel>
  );
}
