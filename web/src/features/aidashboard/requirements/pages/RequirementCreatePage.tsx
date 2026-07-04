import { PlusOutlined } from "@ant-design/icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, Button, DatePicker, Form, Input, Result, Select, Spin } from "antd";
import dayjs from "dayjs";
import { useEffect, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";

import { FormPageWrap } from "@/shared/components/FormPageWrap/FormPageWrap";
import { FormSubmitButton } from "@/shared/components/FormSubmitButton/FormSubmitButton";
import { PagePanel } from "@/shared/components/PagePanel/PagePanel";
import { useFormLeaveConfirm } from "@/shared/hooks/useFormLeaveConfirm";
import { buildCreateSuccessUrl } from "@/shared/utils/urlQuery";

import "../../aidashboard-pattern.css";
import { requirementsBoardApi } from "../api/requirementsBoardApi";
import { AcceptanceCriteriaEditor } from "../components/AcceptanceCriteriaEditor";
import { invalidateRequirementTaskWorkspace } from "../queryInvalidation";
import type { MockRequirement, RequirementPriority } from "../types";
import {
  acceptanceCriteriaRules,
  descriptionRules,
  normalizeCriteria,
  normalizeOptionalText,
  normalizeRequiredText,
  optionalUrlRules,
  requiredSelectRules,
  titleRules
} from "../validation/requirementTaskValidation";

interface CreateFormValues {
  title: string;
  description: string;
  priority: RequirementPriority;
  deadline?: dayjs.Dayjs;
  owner_ids?: string[];
  team_ids?: string[];
  feishu_doc_url?: string;
  acceptance_criteria: string[];
}

export function RequirementCreatePage() {
  const navigate = useNavigate();
  const location = useLocation();
  const queryClient = useQueryClient();
  const [form] = Form.useForm<CreateFormValues>();
  const [formError, setFormError] = useState<string>();
  const [createdRequirement, setCreatedRequirement] = useState<MockRequirement>();
  const backTo = buildCreateSuccessUrl("/requirements", location.search);

  const teamsQuery = useQuery({
    queryKey: ["requirements-board", "teams"],
    queryFn: () => requirementsBoardApi.listTeams(),
    staleTime: 5 * 60_000
  });
  const assigneesQuery = useQuery({
    queryKey: ["requirements-board", "assignees"],
    queryFn: () => requirementsBoardApi.listAssignees(),
    staleTime: 5 * 60_000
  });

  const createMutation = useMutation({
    mutationFn: (values: CreateFormValues) =>
      requirementsBoardApi.createRequirement({
        title: normalizeRequiredText(values.title),
        description: normalizeRequiredText(values.description),
        priority: values.priority,
        deadline: values.deadline?.format("YYYY-MM-DD"),
        owner_ids: values.owner_ids ?? [],
        team_ids: values.team_ids ?? [],
        feishu_doc_url: normalizeOptionalText(values.feishu_doc_url),
        acceptance_criteria: normalizeCriteria(values.acceptance_criteria)
      })
  });
  const submitting = createMutation.isPending;
  const { markClean, markDirty, confirmLeave } = useFormLeaveConfirm({ form, submitting });
  const handleNavigate = (url: string) => confirmLeave(() => navigate(url));
  const handleCancel = () => handleNavigate(backTo);

  useEffect(() => {
    form.setFieldsValue({ priority: "medium", acceptance_criteria: [""] });
    markClean();
  }, [form, markClean]);

  const handleSubmit = async (values: CreateFormValues) => {
    setFormError(undefined);
    try {
      const created = await createMutation.mutateAsync(values);
      await invalidateRequirementTaskWorkspace(queryClient, { requirementId: created.id });
      markClean();
      setCreatedRequirement(created);
    } catch (error) {
      setFormError(error instanceof Error ? error.message : "创建需求失败，请稍后重试");
    }
  };

  if (createdRequirement) {
    return (
      <PagePanel
        title="需求已创建"
        description="下一步将需求拆解为可执行任务"
        className="aidashboard-form-page"
        breadcrumbs={[
          { title: "业务" },
          { title: "需求看板", path: "/requirements" },
          { title: "需求已创建" }
        ]}
      >
        <FormPageWrap className="aidashboard-form-wrap" maxWidth="100%" density="cozy" card>
          <Result
            status="success"
            title="需求创建成功"
            subTitle={`“${createdRequirement.title}”已进入待开始阶段。继续拆分任务后，团队才能开始推进。`}
            extra={[
              <Button
                key="split"
                type="primary"
                icon={<PlusOutlined />}
                onClick={() =>
                  navigate(
                    `/tasks/create?requirement_id=${encodeURIComponent(createdRequirement.id)}`
                  )
                }
              >
                继续拆分任务
              </Button>,
              <Button key="board" onClick={() => navigate(backTo)}>
                返回需求看板
              </Button>
            ]}
          />
        </FormPageWrap>
      </PagePanel>
    );
  }

  return (
    <PagePanel
      title="新建需求"
      description="定义业务目标、验收标准、负责人和协作范围"
      className="aidashboard-form-page"
      backTo={backTo}
      onBack={handleCancel}
      onNavigate={handleNavigate}
      breadcrumbs={[
        { title: "业务" },
        { title: "需求看板", path: "/requirements" },
        { title: "新建需求" }
      ]}
    >
      <FormPageWrap className="aidashboard-form-wrap" maxWidth="100%" density="cozy" card>
        <Spin spinning={submitting}>
          {formError ? (
            <Alert className="aidashboard-form__error" type="error" showIcon message={formError} />
          ) : null}
          {teamsQuery.isError ? (
            <Alert
              className="aidashboard-form__error"
              type="error"
              showIcon
              message="参与团队加载失败"
              description="暂时无法选择参与团队，可重试后补充，也可以先创建需求。"
              action={<Button onClick={() => void teamsQuery.refetch()}>重试</Button>}
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
                <h2>基础信息</h2>
                <p>说明需求背景、目标与交付范围。</p>
              </div>
              <div className="aidashboard-form__grid">
                <Form.Item
                  className="aidashboard-form__full-row"
                  label="标题"
                  name="title"
                  rules={titleRules("标题")}
                >
                  <Input placeholder="例如：控制台日报任务进展上报" />
                </Form.Item>
                <Form.Item
                  className="aidashboard-form__full-row"
                  label="描述"
                  name="description"
                  rules={descriptionRules("描述")}
                >
                  <Input.TextArea rows={5} placeholder="详细描述需求背景、目标和范围" />
                </Form.Item>
              </div>
            </section>

            <section className="aidashboard-form__section">
              <div className="aidashboard-form__section-head">
                <h2>需求验收标准（可选）</h2>
                <p>逐条定义需求级完成条件；P0 不要求任务关联或覆盖这些标准。</p>
              </div>
              <Form.Item label="标准列表" name="acceptance_criteria" rules={acceptanceCriteriaRules()}>
                <AcceptanceCriteriaEditor placeholder="例如：用户可以完成日报生成并发送" />
              </Form.Item>
            </section>

            <section className="aidashboard-form__section">
              <div className="aidashboard-form__section-head">
                <h2>交付信息</h2>
                <p>负责人和参与团队都可以稍后补充。</p>
              </div>
              <div className="aidashboard-form__grid aidashboard-form__grid--simple">
                <Form.Item
                  label="优先级"
                  name="priority"
                  rules={requiredSelectRules("优先级")}
                >
                  <Select
                    options={[
                      { value: "low", label: "低" },
                      { value: "medium", label: "中" },
                      { value: "high", label: "高" },
                      { value: "urgent", label: "紧急" }
                    ]}
                  />
                </Form.Item>
                <Form.Item label="截止日期" name="deadline">
                  <DatePicker />
                </Form.Item>
                <Form.Item label="负责人" name="owner_ids">
                  <Select
                    allowClear
                    mode="multiple"
                    showSearch
                    loading={assigneesQuery.isLoading}
                    placeholder={assigneesQuery.isError ? "负责人加载失败" : "可稍后指定"}
                    optionFilterProp="label"
                    maxTagCount="responsive"
                    options={(assigneesQuery.data ?? []).map((assignee) => ({
                      value: assignee.id,
                      label: assignee.name
                    }))}
                  />
                </Form.Item>
                <Form.Item
                  label="飞书文档"
                  name="feishu_doc_url"
                  rules={optionalUrlRules("飞书文档链接")}
                >
                  <Input placeholder="https://..." />
                </Form.Item>
              </div>
              <Form.Item
                label="参与团队"
                name="team_ids"
                extra="未明确协作团队时可以先留空"
              >
                <Select
                  mode="multiple"
                  loading={teamsQuery.isLoading}
                  disabled={teamsQuery.isLoading || teamsQuery.isError}
                  placeholder={teamsQuery.isError ? "团队加载失败" : "可稍后指定"}
                  options={(teamsQuery.data ?? []).map((team) => ({
                    value: team.id,
                    label: team.name
                  }))}
                />
              </Form.Item>
            </section>

            <FormSubmitButton
              submitText="创建需求"
              loading={submitting}
              onCancel={handleCancel}
              sticky
            />
          </Form>
        </Spin>
      </FormPageWrap>
    </PagePanel>
  );
}
