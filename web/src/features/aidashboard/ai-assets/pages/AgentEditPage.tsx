import { Alert, App, Form, Spin } from "antd";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo } from "react";
import { useNavigate, useParams } from "react-router-dom";

import {
  fetchManagedAgents,
  fetchManagedCredentials,
  fetchManagedMCPEntries,
  fetchManagedSkills,
  updateManagedAgent
} from "../../api/client";
import type { UpsertManagedAgentPayload } from "../../api/types";
import { AgentEditor, type AgentEditorValues } from "../components/AgentEditor";
import {
  AI_ASSETS_HOME,
  aiAssetsPath,
  currentMCPSelection,
  currentSkillKeys,
  errorMessage
} from "../utils/agentAssets";
import { PagePanel } from "@/shared/components/PagePanel/PagePanel";

const AI_ASSETS_RETURN_PATH = aiAssetsPath("agents");

export function AgentEditPage() {
  const navigate = useNavigate();
  const { agentId } = useParams<{ agentId: string }>();
  const queryClient = useQueryClient();
  const { message } = App.useApp();
  const [form] = Form.useForm<AgentEditorValues>();

  const agentsQuery = useQuery({
    queryKey: ["managed-agents"],
    queryFn: () => fetchManagedAgents(),
    staleTime: 30_000
  });
  const skillsQuery = useQuery({
    queryKey: ["managed-skills", "mine", "include-system"],
    queryFn: () => fetchManagedSkills(true),
    staleTime: 60_000
  });
  const mcpQuery = useQuery({
    queryKey: ["managed-mcp", "mine", "include-system"],
    queryFn: () => fetchManagedMCPEntries(true),
    staleTime: 60_000
  });
  const credentialsQuery = useQuery({
    queryKey: ["managed-credentials"],
    queryFn: () => fetchManagedCredentials(),
    staleTime: 30_000
  });

  const skills = useMemo(() => skillsQuery.data?.skills ?? [], [skillsQuery.data]);
  const mcpEntries = useMemo(() => mcpQuery.data?.entries ?? [], [mcpQuery.data]);
  const credentials = useMemo(
    () => credentialsQuery.data?.credentials ?? [],
    [credentialsQuery.data]
  );

  const agent = useMemo(
    () => agentsQuery.data?.agents.find((item) => item.agent_id === agentId) ?? null,
    [agentsQuery.data, agentId]
  );

  const updateMutation = useMutation({
    mutationFn: (payload: UpsertManagedAgentPayload) =>
      updateManagedAgent(agent?.agent_id || "", payload),
    onSuccess: () => {
      message.success("Agent 已更新");
      void queryClient.invalidateQueries({ queryKey: ["managed-agents"] });
      navigate(AI_ASSETS_RETURN_PATH);
    },
    onError: (err: unknown) => message.error(errorMessage(err))
  });

  useEffect(() => {
    if (!agent) return;
    form.setFieldsValue({
      name: agent.name,
      description: agent.description,
      engine: agent.engine,
      business_type: agent.business_type || "generic",
      instructions: agent.instructions,
      default_model_id: agent.default_model_id,
      start_prompt_template: agent.start_prompt_template,
      skills: currentSkillKeys(agent),
      mcp_bindings: currentMCPSelection(agent),
      slot_credentials: agent.default_bindings ?? {}
    });
  }, [agent, form]);

  if (agentsQuery.isLoading) {
    return (
      <PagePanel
        title="编辑 Managed Agent"
        description="加载 Agent 中…"
        backTo={AI_ASSETS_RETURN_PATH}
        onBack={() => navigate(AI_ASSETS_RETURN_PATH)}
        onNavigate={(path) => navigate(path)}
        breadcrumbs={[
          { title: "系统" },
          { title: "我的 AI 资产", path: AI_ASSETS_HOME },
          { title: "编辑 Agent" }
        ]}
      >
        <Spin />
      </PagePanel>
    );
  }

  if (!agent) {
    return (
      <PagePanel
        title="编辑 Managed Agent"
        description="未找到该 Agent"
        backTo={AI_ASSETS_RETURN_PATH}
        onBack={() => navigate(AI_ASSETS_RETURN_PATH)}
        onNavigate={(path) => navigate(path)}
        breadcrumbs={[
          { title: "系统" },
          { title: "我的 AI 资产", path: AI_ASSETS_HOME },
          { title: "编辑 Agent" }
        ]}
      >
        <Alert
          type="warning"
          showIcon
          message="未找到该 Agent"
          description="该 Agent 可能已被删除，请返回列表查看。"
        />
      </PagePanel>
    );
  }

  if (agent.permissions?.can_edit === false) {
    return (
      <PagePanel
        title="系统 Agent"
        description="系统 Agent 由管理员统一维护"
        backTo={AI_ASSETS_RETURN_PATH}
        onBack={() => navigate(AI_ASSETS_RETURN_PATH)}
        onNavigate={(path) => navigate(path)}
        breadcrumbs={[
          { title: "系统" },
          { title: "我的 AI 资产", path: AI_ASSETS_HOME },
          { title: agent.name }
        ]}
      >
        <Alert
          type="info"
          showIcon
          message="该 Agent 不支持查看或编辑"
          description="你可以返回 Agent 列表，将它设为默认报告 Agent。"
        />
      </PagePanel>
    );
  }

  return (
    <PagePanel
      title="编辑 Managed Agent"
      backTo={AI_ASSETS_RETURN_PATH}
      onBack={() => navigate(AI_ASSETS_RETURN_PATH)}
      onNavigate={(path) => navigate(path)}
      breadcrumbs={[
        { title: "系统" },
        { title: "我的 AI 资产", path: AI_ASSETS_HOME },
        { title: agent.name || "编辑 Agent" }
      ]}
    >
      <AgentEditor
        form={form}
        agent={agent}
        skills={skills}
        mcpEntries={mcpEntries}
        credentials={credentials}
        submitting={updateMutation.isPending}
        onCancel={() => navigate(AI_ASSETS_RETURN_PATH)}
        onSubmit={(payload: UpsertManagedAgentPayload) => updateMutation.mutate(payload)}
      />
    </PagePanel>
  );
}
