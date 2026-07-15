import { Button, Card, Form, Input, Select, Space } from "antd";
import type { FormInstance } from "antd";

import type {
  ManagedAgent,
  ManagedCredential,
  ManagedMCPEntry,
  ManagedSkill,
  UpsertManagedAgentPayload
} from "../../api/types";
import { MCPResourcePicker } from "./MCPResourcePicker";
import { ManagedModelSelect } from "./ManagedModelSelect";
import { SkillResourcePicker } from "./SkillResourcePicker";
import {
  buildAgentResourcePayload,
  isSystemBuiltinMCP,
  parseRefKey,
  refKey
} from "../utils/agentAssets";

import "./AgentWorkspace.css";

export type AgentEditorSubmitPayload = UpsertManagedAgentPayload;

export interface AgentEditorValues {
  name: string;
  description?: string;
  engine: string;
  business_type?: "generic" | "report";
  instructions?: string;
  default_model_id?: string;
  start_prompt_template?: string;
  skills?: string[];
  mcp_bindings?: Record<string, string>;
  slot_credentials?: Record<string, string>;
}

interface AgentEditorProps {
  form: FormInstance<AgentEditorValues>;
  agent: ManagedAgent | null;
  skills: ManagedSkill[];
  mcpEntries: ManagedMCPEntry[];
  credentials: ManagedCredential[];
  submitting: boolean;
  onCancel: () => void;
  onSubmit: (payload: AgentEditorSubmitPayload) => void;
}

export function AgentEditor({
  form,
  agent,
  skills,
  mcpEntries,
  credentials,
  submitting,
  onCancel,
  onSubmit
}: AgentEditorProps) {
  const slotCredentials = Form.useWatch("slot_credentials", { form, preserve: true }) ?? {};
  const configurableMCPEntries = mcpEntries.filter((entry) => !isSystemBuiltinMCP(entry));

  return (
    <section className="ai-assets-workspace ai-assets-agent-editor">
      <Form
        form={form}
        layout="vertical"
        initialValues={{ engine: "codex", mcp_bindings: {}, slot_credentials: {} }}
        onFinish={(values: AgentEditorValues) => {
          const businessType = values.business_type || "generic";
          const slotCredentialValues =
            values.slot_credentials ?? form.getFieldValue("slot_credentials") ?? {};
          const mcpSelection = Object.entries(values.mcp_bindings ?? {}).map(([key, slot]) => {
            const ref = parseRefKey(key);
            const entry = mcpEntries.find(
              (item) => refKey(item.owner, item.slug, item.version) === key
            );
            return {
              ...ref,
              requiresCredential: entry ? entry.requires_credential : Boolean(slot),
              credential_slot: slot || undefined,
              credentialId: slot ? slotCredentialValues[slot] || "" : ""
            };
          });
          const resources = buildAgentResourcePayload({
            skills: values.skills?.map(parseRefKey) ?? [],
            mcps: mcpSelection
          });
          const payload: AgentEditorSubmitPayload = {
            name: values.name,
            description: values.description,
            engine: values.engine,
            business_type: businessType,
            instructions: values.instructions,
            default_model_id: values.default_model_id?.trim() || undefined,
            start_prompt_template: values.start_prompt_template,
            ...resources
          };
          onSubmit(payload);
        }}
      >
        <Card title="基础信息" className="ai-assets-editor-section">
          <div className="ai-assets-editor-grid">
            <Form.Item name="name" label="名称" rules={[{ required: true, message: "请输入名称" }]}>
              <Input placeholder="Agent 名称" />
            </Form.Item>
            <Form.Item name="business_type" label="Agent 类型" initialValue="report">
              <Select
                options={[
                  { label: "普通 Agent", value: "generic" },
                  { label: "报告 Agent", value: "report" }
                ]}
              />
            </Form.Item>
            <Form.Item name="description" label="描述" className="ai-assets-editor-grid__wide">
              <Input.TextArea rows={3} placeholder="这个 Agent 能做什么" />
            </Form.Item>
          </div>
        </Card>

        <Card title="运行配置" className="ai-assets-editor-section">
          <div className="ai-assets-editor-grid">
            <Form.Item
              name="engine"
              label="Engine"
              rules={[{ required: true, message: "请选择 engine" }]}
            >
              <Select
                options={[
                  { label: "codex", value: "codex" },
                  { label: "claude-code", value: "claude-code" }
                ]}
              />
            </Form.Item>
            <Form.Item
              name="default_model_id"
              label="默认模型"
              extra="可选。选择后运行页默认使用该模型；未选择时，运行前需要选择模型。"
            >
              <ManagedModelSelect
                allowClear
                preservedModelId={agent?.default_model_id}
                placeholder="选择默认模型"
              />
            </Form.Item>
          </div>
        </Card>

        <Card title="Prompt 配置" className="ai-assets-editor-section">
          <Form.Item name="instructions" label="Instructions">
            <Input.TextArea rows={6} placeholder="系统指令" />
          </Form.Item>
          <Form.Item name="start_prompt_template" label="Start Prompt 模板">
            <Input.TextArea
              rows={5}
              placeholder="例如：请帮我分析 {{ topic }}，并输出 {{ format }}。变量名仅支持英文、数字和下划线。"
            />
          </Form.Item>
        </Card>

        <Card title="资源绑定" className="ai-assets-editor-section">
          <Form.Item name="slot_credentials" hidden preserve />
          <Form.Item name="skills" label="Skills">
            <SkillResourcePicker skills={skills} />
          </Form.Item>
          <Form.Item name="mcp_bindings" label="MCP Servers">
            <MCPResourcePicker
              entries={configurableMCPEntries}
              credentials={credentials}
              slotCredentials={slotCredentials}
              onSlotCredentialChange={(slot, credentialId) => {
                const next = { ...slotCredentials };
                if (credentialId) {
                  next[slot] = credentialId;
                } else {
                  delete next[slot];
                }
                form.setFieldsValue({ slot_credentials: next });
              }}
            />
          </Form.Item>
        </Card>

        <div className="ai-assets-workspace__actions">
          <Space>
            <Button onClick={onCancel}>取消</Button>
            <Button type="primary" loading={submitting} onClick={() => form.submit()}>
              {agent ? "保存" : "创建 Agent"}
            </Button>
          </Space>
        </div>
      </Form>
    </section>
  );
}
