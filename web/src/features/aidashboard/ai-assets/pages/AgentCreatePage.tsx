import { App, Form } from "antd";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo } from "react";
import { useNavigate } from "react-router-dom";

import { createManagedAgent, fetchManagedCredentials, fetchManagedMCPEntries, fetchManagedSkills } from "../../api/client";
import type { UpsertManagedAgentPayload } from "../../api/types";
import { AgentEditor, type AgentEditorValues } from "../components/AgentEditor";
import { AI_ASSETS_HOME, aiAssetsPath, errorMessage } from "../utils/agentAssets";
import { PagePanel } from "@/shared/components/PagePanel/PagePanel";

const AI_ASSETS_RETURN_PATH = aiAssetsPath("agents");

export function AgentCreatePage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { message } = App.useApp();
  const [form] = Form.useForm<AgentEditorValues>();

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
  const credentials = useMemo(() => credentialsQuery.data?.credentials ?? [], [credentialsQuery.data]);

  const createMutation = useMutation({
    mutationFn: (payload: UpsertManagedAgentPayload) => createManagedAgent(payload),
    onSuccess: () => {
      message.success("Agent 已创建");
      void queryClient.invalidateQueries({ queryKey: ["managed-agents"] });
      navigate(AI_ASSETS_RETURN_PATH);
    },
    onError: (err: unknown) => message.error(errorMessage(err))
  });

  useEffect(() => {
    form.setFieldsValue({
      engine: "codex",
      business_type: "report",
      skills: [],
      mcp_bindings: {},
      slot_credentials: {}
    });
  }, [form]);

  return (
    <PagePanel
      title="新建 Managed Agent"
      backTo={AI_ASSETS_RETURN_PATH}
      onBack={() => navigate(AI_ASSETS_RETURN_PATH)}
      onNavigate={(path) => navigate(path)}
      breadcrumbs={[
        { title: "系统" },
        { title: "我的 AI 资产", path: AI_ASSETS_HOME },
        { title: "新建 Agent" }
      ]}
    >
      <AgentEditor
        form={form}
        agent={null}
        skills={skills}
        mcpEntries={mcpEntries}
        credentials={credentials}
        submitting={createMutation.isPending}
        onCancel={() => navigate(AI_ASSETS_RETURN_PATH)}
        onSubmit={(payload: UpsertManagedAgentPayload) => createMutation.mutate(payload)}
      />
    </PagePanel>
  );
}
