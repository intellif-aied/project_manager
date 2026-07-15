import {
  App,
  Alert,
  Button,
  Card,
  Empty,
  Form,
  Input,
  Select,
  Space,
  Switch,
  Tag,
  TimePicker
} from "antd";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import dayjs from "dayjs";
import type { Dayjs } from "dayjs";
import { useEffect, useMemo } from "react";
import { useNavigate, useParams } from "react-router-dom";

import {
  createManagedAgentSchedule,
  fetchManagedAgentSchedules,
  fetchManagedAgents,
  previewManagedAgentSchedule,
  runManagedAgentScheduleNow,
  updateManagedAgentSchedule
} from "../../api/client";
import type {
  ManagedAgent,
  ManagedAgentSchedule,
  PreviewManagedAgentSchedulePayload,
  ReportType,
  UpsertManagedAgentSchedulePayload
} from "../../api/types";
import { PagePanel } from "@/shared/components/PagePanel/PagePanel";
import {
  AI_ASSETS_HOME,
  aiAssetsPath,
  errorMessage,
  extractPromptVariables,
  isReportAgentAsset,
  REPORT_SYSTEM_PROMPT_KEYS
} from "../utils/agentAssets";
import { ManagedModelSelect } from "../components/ManagedModelSelect";

import "../components/AgentWorkspace.css";

const AI_ASSETS_RETURN_PATH = aiAssetsPath("schedules");

const WEEKDAY_OPTIONS = [
  { label: "周一", value: 1 },
  { label: "周二", value: 2 },
  { label: "周三", value: 3 },
  { label: "周四", value: 4 },
  { label: "周五", value: 5 },
  { label: "周六", value: 6 },
  { label: "周日", value: 7 }
];

const REPORT_TYPE_OPTIONS: Array<{ label: string; value: ReportType }> = [
  { label: "个人日报", value: "personal_daily" },
  { label: "个人周报", value: "personal_weekly" },
  { label: "小组日报", value: "team_daily" },
  { label: "小组周报", value: "team_weekly" },
  { label: "部门日报", value: "department_daily" },
  { label: "部门周报", value: "department_weekly" }
];
const TIME_OF_DAY_FORMAT = "HH:mm";

interface AgentScheduleFormValues {
  name: string;
  enabled: boolean;
  agent_id: string;
  schedule_type: "daily" | "weekly";
  weekdays?: number[];
  time_of_day: string;
  model_id?: string;
  initial_message?: string;
  report_type?: ReportType;
  start_prompt_values?: Record<string, string>;
}

function selectedRunKind(agent?: ManagedAgent): "generic_agent" | "report_agent" {
  return agent && isReportAgentAsset(agent) ? "report_agent" : "generic_agent";
}

function scheduleToValues(schedule: ManagedAgentSchedule): AgentScheduleFormValues {
  return {
    name: schedule.name,
    enabled: schedule.enabled,
    agent_id: schedule.agent_id,
    schedule_type: schedule.schedule_type,
    weekdays: schedule.weekdays,
    time_of_day: schedule.time_of_day,
    model_id: schedule.model_id,
    initial_message: schedule.initial_message || schedule.message,
    report_type: schedule.report_config?.report_type,
    start_prompt_values: schedule.start_prompt_values || schedule.params || {}
  };
}

function buildPayload(
  values: AgentScheduleFormValues,
  agent?: ManagedAgent
): UpsertManagedAgentSchedulePayload {
  const runKind = selectedRunKind(agent);
  return {
    name: values.name,
    agent_id: values.agent_id,
    run_kind: runKind,
    enabled: values.enabled,
    trigger_config: {
      schedule_type: values.schedule_type,
      weekdays: values.schedule_type === "weekly" ? values.weekdays || [] : undefined,
      time_of_day: values.time_of_day
    },
    run_config: {
      model_id: values.model_id?.trim() || undefined,
      initial_message: values.initial_message?.trim() || undefined,
      start_prompt_values: values.start_prompt_values || {},
      report_config:
        runKind === "report_agent"
          ? { report_type: values.report_type || "personal_daily" }
          : undefined
    }
  };
}

function scheduleRuleText(values: Partial<AgentScheduleFormValues>) {
  if (!values.time_of_day) return "-";
  if (values.schedule_type === "weekly") {
    const labels = (values.weekdays || [])
      .map((day) => WEEKDAY_OPTIONS.find((item) => item.value === day)?.label)
      .filter(Boolean)
      .join("、");
    return `${labels || "每周"} ${values.time_of_day}`;
  }
  return `每天 ${values.time_of_day}`;
}

function timeStringToDayjs(value?: string): Dayjs | null {
  const match = /^([01]\d|2[0-3]):([0-5]\d)$/.exec(value || "");
  if (!match) return null;
  return dayjs()
    .hour(Number(match[1]))
    .minute(Number(match[2]))
    .second(0)
    .millisecond(0);
}

function normalizeTimeOfDay(value?: Dayjs | string | null) {
  if (!value) return "";
  if (typeof value === "string") return value;
  return value.format(TIME_OF_DAY_FORMAT);
}

function formatDateTime(value?: string) {
  return value ? new Date(value).toLocaleString() : "-";
}

export function AgentScheduleFormPage() {
  const { scheduleId } = useParams();
  const editing = Boolean(scheduleId);
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { message } = App.useApp();
  const [form] = Form.useForm<AgentScheduleFormValues>();

  const agentsQuery = useQuery({
    queryKey: ["managed-agents"],
    queryFn: () => fetchManagedAgents(),
    staleTime: 30_000
  });
  const schedulesQuery = useQuery({
    queryKey: ["managed-agent-schedules"],
    queryFn: () => fetchManagedAgentSchedules(),
    staleTime: 15_000
  });

  const agents = useMemo(() => agentsQuery.data?.agents ?? [], [agentsQuery.data]);
  const schedules = useMemo(() => schedulesQuery.data?.schedules ?? [], [schedulesQuery.data]);
  const schedule = useMemo(
    () => schedules.find((item) => item.id === scheduleId),
    [scheduleId, schedules]
  );

  const agentId = Form.useWatch("agent_id", form);
  const scheduleType = Form.useWatch("schedule_type", form);
  const weekdays = Form.useWatch("weekdays", form);
  const timeOfDay = Form.useWatch("time_of_day", form);
  const reportType = Form.useWatch("report_type", form);
  const selectedAgent = useMemo(
    () => agents.find((item) => item.agent_id === agentId),
    [agentId, agents]
  );
  const runKind = selectedRunKind(selectedAgent);
  const promptVariables = useMemo(() => {
    const keys = extractPromptVariables(selectedAgent?.start_prompt_template);
    if (runKind !== "report_agent") return keys;
    return keys.filter((key) => !REPORT_SYSTEM_PROMPT_KEYS.has(key));
  }, [runKind, selectedAgent?.start_prompt_template]);

  const previewPayload = useMemo<PreviewManagedAgentSchedulePayload | null>(() => {
    if (!agentId || !timeOfDay || !scheduleType) return null;
    if (scheduleType === "weekly" && (!weekdays || weekdays.length === 0)) return null;
    return {
      agent_id: agentId,
      run_kind: runKind,
      schedule_type: scheduleType,
      weekdays: scheduleType === "weekly" ? weekdays : undefined,
      time_of_day: timeOfDay,
      report_type: runKind === "report_agent" ? reportType || "personal_daily" : undefined
    };
  }, [agentId, reportType, runKind, scheduleType, timeOfDay, weekdays]);

  const previewQuery = useQuery({
    queryKey: ["managed-agent-schedule-preview", previewPayload],
    queryFn: () =>
      previewManagedAgentSchedule(previewPayload as PreviewManagedAgentSchedulePayload),
    enabled: Boolean(previewPayload),
    staleTime: 10_000,
    retry: false
  });

  useEffect(() => {
    if (!editing) {
      form.setFieldsValue({
        enabled: true,
        schedule_type: "daily",
        time_of_day: "19:00",
        report_type: "personal_daily",
        start_prompt_values: {}
      });
    }
  }, [editing, form]);

  useEffect(() => {
    if (editing && schedule) {
      form.setFieldsValue(scheduleToValues(schedule));
    }
  }, [editing, form, schedule]);

  useEffect(() => {
    if (!selectedAgent) return;
    const current = form.getFieldValue("model_id");
    if (!current && selectedAgent.default_model_id) {
      form.setFieldValue("model_id", selectedAgent.default_model_id);
    }
    if (selectedRunKind(selectedAgent) === "report_agent" && !form.getFieldValue("report_type")) {
      form.setFieldValue("report_type", "personal_daily");
    }
  }, [form, selectedAgent]);

  const saveMutation = useMutation({
    mutationFn: async (payload: { values: AgentScheduleFormValues; runNow: boolean }) => {
      const built = buildPayload(payload.values, selectedAgent);
      const saved =
        editing && scheduleId
          ? await updateManagedAgentSchedule(scheduleId, built)
          : await createManagedAgentSchedule(built);
      if (payload.runNow) {
        await runManagedAgentScheduleNow(saved.id, "save_and_run");
      }
      return saved;
    },
    onSuccess: (_saved, variables) => {
      message.success(
        variables.runNow ? "定时报告任务已保存并提交生成" : "定时报告任务已保存"
      );
      void queryClient.invalidateQueries({ queryKey: ["managed-agent-schedules"] });
      void queryClient.invalidateQueries({ queryKey: ["managed-agent-runs"] });
      navigate(AI_ASSETS_RETURN_PATH);
    },
    onError: (err: unknown) => message.error(errorMessage(err))
  });

  const agentOptions = agents
    .filter((agent) => !agent.archived)
    .map((agent) => ({
      label: `${agent.name || agent.agent_id} (${agent.agent_id})`,
      value: agent.agent_id
    }));

  if (editing && !schedulesQuery.isLoading && !schedule) {
    return (
      <PagePanel
        title="编辑定时报告任务"
        backTo={AI_ASSETS_RETURN_PATH}
        onBack={() => navigate(AI_ASSETS_RETURN_PATH)}
        onNavigate={(path) => navigate(path)}
        breadcrumbs={[
          { title: "系统" },
          { title: "我的 AI 资产", path: AI_ASSETS_HOME },
          { title: "编辑定时报告任务" }
        ]}
      >
        <Empty description="定时报告任务不存在或已删除" />
      </PagePanel>
    );
  }

  const submit = async (runNow: boolean) => {
    const values = await form.validateFields();
    saveMutation.mutate({ values, runNow });
  };

  return (
    <PagePanel
      title={editing ? "编辑定时报告任务" : "新建定时报告任务"}
      backTo={AI_ASSETS_RETURN_PATH}
      onBack={() => navigate(AI_ASSETS_RETURN_PATH)}
      onNavigate={(path) => navigate(path)}
      breadcrumbs={[
        { title: "系统" },
        { title: "我的 AI 资产", path: AI_ASSETS_HOME },
        { title: "定时报告任务" },
        { title: editing ? "编辑" : "新建" }
      ]}
    >
      <section className="ai-assets-workspace">
        <Form
          form={form}
          layout="vertical"
          className="ai-assets-agent-editor ai-assets-schedule-form"
        >
          <Card title="报告任务设置" className="ai-assets-editor-section">
            <div className="ai-assets-schedule-form__grid">
              <Form.Item
                name="name"
                label="报告任务名称"
                rules={[{ required: true, message: "请输入报告任务名称" }]}
              >
                <Input placeholder="例如：每日个人日报" />
              </Form.Item>
              <Form.Item
                name="enabled"
                label="启用状态"
                valuePropName="checked"
                className="ai-assets-schedule-form__switch"
              >
                <Switch checkedChildren="启用" unCheckedChildren="停用" />
              </Form.Item>

              <Form.Item
                name="agent_id"
                label="Agent"
                className="ai-assets-editor-grid__wide"
                rules={[{ required: true, message: "请选择 Agent" }]}
              >
                <Select
                  showSearch
                  loading={agentsQuery.isLoading}
                  options={agentOptions}
                  optionFilterProp="label"
                  placeholder="选择要定时生成报告的 Agent"
                />
              </Form.Item>

              <Form.Item label="Agent 类型">
                {runKind === "report_agent" ? (
                  <Tag color="purple">Report Agent</Tag>
                ) : (
                  <Tag>普通 Agent</Tag>
                )}
              </Form.Item>

              <Form.Item name="schedule_type" label="报告频率" rules={[{ required: true }]}>
                <Select
                  options={[
                    { label: "每天", value: "daily" },
                    { label: "每周", value: "weekly" }
                  ]}
                />
              </Form.Item>

              {scheduleType === "weekly" ? (
                <Form.Item
                  name="weekdays"
                  label="周几生成"
                  rules={[{ required: true, message: "请选择周几生成" }]}
                >
                  <Select mode="multiple" options={WEEKDAY_OPTIONS} />
                </Form.Item>
              ) : null}

              <Form.Item
                name="time_of_day"
                label="生成时间"
                getValueProps={(value?: string) => ({ value: timeStringToDayjs(value) })}
                normalize={normalizeTimeOfDay}
                rules={[
                  { required: true, message: "请选择生成时间" },
                  { pattern: /^([01]\d|2[0-3]):[0-5]\d$/, message: "格式应为 HH:mm" }
                ]}
              >
                <TimePicker
                  format={TIME_OF_DAY_FORMAT}
                  inputReadOnly
                  placeholder="选择生成时间"
                  showNow={false}
                />
              </Form.Item>

              {runKind === "report_agent" ? (
                <Form.Item
                  name="report_type"
                  label="报告类型"
                  rules={[{ required: true, message: "请选择报告类型" }]}
                >
                  <Select options={REPORT_TYPE_OPTIONS} />
                </Form.Item>
              ) : null}

              <Form.Item name="model_id" label="模型" extra="不选择时使用 Agent 默认模型">
                <ManagedModelSelect
                  allowClear
                  preservedModelId={selectedAgent?.default_model_id}
                  placeholder={
                    selectedAgent?.default_model_id
                      ? `留空使用 Agent 默认模型：${selectedAgent.default_model_id}`
                      : "选择模型"
                  }
                />
              </Form.Item>

              <Form.Item
                name="initial_message"
                label={runKind === "report_agent" ? "补充要求" : "运行消息"}
                className="ai-assets-editor-grid__wide"
              >
                <Input.TextArea rows={4} placeholder="可选：补充本次运行要求" />
              </Form.Item>

              {promptVariables.length > 0 ? (
                <div className="ai-assets-schedule-form__prompt-block">
                  <div className="ai-assets-schedule-form__subhead">运行参数</div>
                  <div className="ai-assets-prompt-values">
                    {promptVariables.map((key) => (
                      <Form.Item key={key} name={["start_prompt_values", key]} label={key}>
                        <Input.TextArea rows={2} />
                      </Form.Item>
                    ))}
                  </div>
                </div>
              ) : null}
            </div>
          </Card>

          <Card title="定时报告预览" className="ai-assets-editor-section">
            <dl className="ai-assets-schedule-preview">
              <div>
                <dt>生成规则</dt>
                <dd>
                  {scheduleRuleText({
                    schedule_type: scheduleType,
                    weekdays,
                    time_of_day: timeOfDay
                  })}
                </dd>
              </div>
              <div>
                <dt>下次生成时间</dt>
                <dd>{formatDateTime(previewQuery.data?.next_run_at)}</dd>
              </div>
              <div>
                <dt>Agent 类型</dt>
                <dd>{runKind === "report_agent" ? "Report Agent" : "普通 Agent"}</dd>
              </div>
              {runKind === "report_agent" ? (
                <>
                  <div>
                    <dt>报告类型</dt>
                    <dd>
                      {REPORT_TYPE_OPTIONS.find((item) => item.value === reportType)?.label ||
                        "-"}
                    </dd>
                  </div>
                  <div>
                    <dt>报告对象</dt>
                    <dd>{previewQuery.data?.report_target_display || "-"}</dd>
                  </div>
                  <div>
                    <dt>预计报告周期</dt>
                    <dd>{previewQuery.data?.period_display || "-"}</dd>
                  </div>
                </>
              ) : null}
            </dl>
            {previewQuery.error ? (
              <Alert
                type="warning"
                showIcon
                message="预览失败"
                description={errorMessage(previewQuery.error)}
              />
            ) : null}
          </Card>

          <div className="ai-assets-workspace__actions">
            <Space>
              <Button onClick={() => navigate(AI_ASSETS_RETURN_PATH)}>取消</Button>
              <Button loading={saveMutation.isPending} onClick={() => void submit(false)}>
                保存
              </Button>
              <Button
                type="primary"
                loading={saveMutation.isPending}
                onClick={() => void submit(true)}
              >
                保存并立即生成报告
              </Button>
            </Space>
          </div>
        </Form>
      </section>
    </PagePanel>
  );
}
