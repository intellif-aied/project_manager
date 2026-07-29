import type {
  ManagedAgent,
  ManagedCredentialSlot,
  ManagedMCPBinding,
  ManagedMCPEntry,
  ManagedSkill,
  ManagedSkillRef,
  UpsertManagedAgentPayload
} from "../../api/types";

export type AssetTab = "agents" | "skills" | "mcp" | "schedules" | "runs";

export const AI_ASSETS_HOME = "/ai-assets";
export const AI_ASSETS_TAB_QUERY_PARAM = "tab";

const AI_ASSET_TABS = new Set<AssetTab>(["agents", "skills", "mcp", "schedules", "runs"]);

export function isAssetTab(value?: string | null): value is AssetTab {
  return Boolean(value && AI_ASSET_TABS.has(value as AssetTab));
}

export function getAIAssetsTabFromSearch(params: URLSearchParams): AssetTab {
  const tab = params.get(AI_ASSETS_TAB_QUERY_PARAM);
  return isAssetTab(tab) ? tab : "agents";
}

export function aiAssetsPath(tab: AssetTab) {
  const params = new URLSearchParams({ [AI_ASSETS_TAB_QUERY_PARAM]: tab });
  return `${AI_ASSETS_HOME}?${params.toString()}`;
}

export function aiAssetsChildPath(path: string, tab: AssetTab) {
  const params = new URLSearchParams({ [AI_ASSETS_TAB_QUERY_PARAM]: tab });
  return `${path}?${params.toString()}`;
}

export const START_PROMPT_PLACEHOLDER_RE = /\{\{\s*([a-zA-Z0-9_]+)\s*\}\}/g;
export const REPORT_SYSTEM_MARKER = "AIDA_REPORT_DEFAULT:true";
export const REPORT_AGENT_MARKER = "AIDA_REPORT_AGENT:default";
export const REPORT_MANAGED_AGENT_MARKER = "AIDA_MANAGED_DEFAULT_AGENT:true";
export const REPORT_SYSTEM_SKILL_SLUG = "aida-report";
export const REPORT_SYSTEM_SKILL_VERSION = "1.0.0";
export const REPORT_SYSTEM_MCP_SLUG = "aida-report-mcp";
export const REPORT_SYSTEM_MCP_VERSION = "report-v1";
export const REPORT_SYSTEM_MCP_CREDENTIAL_SLOT = "AIDA_REPORT_MCP_AUTH";
export const REPORT_SYSTEM_PROMPT_KEYS = new Set([
  "report_type",
  "target_json",
  "period_json",
  "calendar_context_json",
  "selected_session_slice_keys",
  "selected_session_slice_keys_json",
  "report_source_selection_id",
  "period_start",
  "period_end",
  "scheduled_trigger_at",
  "run_id",
  "mcp_url",
  "credential",
  "credential_slot",
  "AIDA_REPORT_MCP_AUTH"
]);

export function extractPromptVariables(template?: string) {
  const seen = new Set<string>();
  const keys: string[] = [];
  if (!template) return keys;
  for (const match of template.matchAll(START_PROMPT_PLACEHOLDER_RE)) {
    const key = match[1];
    if (!seen.has(key)) {
      seen.add(key);
      keys.push(key);
    }
  }
  return keys;
}

export function renderPromptPreview(template: string, values: Record<string, string>) {
  return template.replace(START_PROMPT_PLACEHOLDER_RE, (match, key: string) => {
    const value = values[key];
    return value && value.trim() ? value : match;
  });
}

export function refKey(owner: string | undefined, slug: string, version: string) {
  return [owner || "", slug, version].join("/");
}

export function parseRefKey(value: string): ManagedSkillRef {
  const [owner, slug, version] = value.split("/");
  return { owner: owner || undefined, slug, version };
}

export function parseMCPBindingKey(value: string): ManagedMCPBinding {
  return parseRefKey(value);
}

export function defaultMcpCredentialSlot(slug: string, version?: string) {
  if (slug === REPORT_SYSTEM_MCP_SLUG && (!version || version === REPORT_SYSTEM_MCP_VERSION)) {
    return REPORT_SYSTEM_MCP_CREDENTIAL_SLOT;
  }
  return `mcp-${slug}`;
}

export interface ManagedMCPResourceSelection extends ManagedMCPBinding {
  requiresCredential: boolean;
  credentialId?: string;
}

export function buildAgentResourcePayload(selection: {
  skills?: ManagedSkillRef[];
  mcps?: ManagedMCPResourceSelection[];
}): Pick<
  UpsertManagedAgentPayload,
  "skills" | "mcp_bindings" | "credential_slots" | "default_bindings"
> {
  const skills =
    selection.skills?.map((item) => ({
      ...(item.owner ? { owner: item.owner } : {}),
      slug: item.slug,
      version: item.version
    })) ?? [];

  const usedSlots = new Set<string>();
  const credentialSlots: ManagedCredentialSlot[] = [];
  const defaultBindings: Record<string, string> = {};
  const mcpBindings = (selection.mcps ?? []).map((item) => {
    const binding: ManagedMCPBinding = {
      ...(item.owner ? { owner: item.owner } : {}),
      slug: item.slug,
      version: item.version
    };
    if (!item.requiresCredential) {
      return binding;
    }
    const baseSlot = item.credential_slot || defaultMcpCredentialSlot(item.slug, item.version);
    let slot = baseSlot;
    for (let idx = 2; usedSlots.has(slot); idx += 1) {
      slot = `${baseSlot}-${idx}`;
    }
    usedSlots.add(slot);
    binding.credential_slot = slot;
    credentialSlots.push({ name: slot, required: true });
    if (item.credentialId) {
      defaultBindings[slot] = item.credentialId;
    }
    return binding;
  });

  return {
    skills,
    mcp_bindings: mcpBindings,
    credential_slots: credentialSlots,
    default_bindings: defaultBindings
  };
}

export function skillLabel(item: ManagedSkillRef) {
  return `${item.slug}@${item.version}`;
}

export function mcpLabel(item: ManagedMCPBinding) {
  return `${item.slug}@${item.version}`;
}

export function currentSkillKeys(agent?: ManagedAgent | null) {
  return agent?.skills?.map((item) => refKey(item.owner, item.slug, item.version)) ?? [];
}

export function currentMCPKeys(agent?: ManagedAgent | null) {
  return agent?.mcp_bindings?.map((item) => refKey(item.owner, item.slug, item.version)) ?? [];
}

export function currentMCPSelection(agent?: ManagedAgent | null) {
  return (agent?.mcp_bindings ?? []).reduce<Record<string, string>>((acc, item) => {
    acc[refKey(item.owner, item.slug, item.version)] = item.credential_slot || "";
    return acc;
  }, {});
}

export function isSystemBuiltinSkill(item: Pick<ManagedSkill, "slug" | "version" | "description">) {
  return (
    (item.slug === REPORT_SYSTEM_SKILL_SLUG && item.version === REPORT_SYSTEM_SKILL_VERSION) ||
    Boolean(item.description?.includes(REPORT_SYSTEM_MARKER))
  );
}

export function isSystemBuiltinMCP(
  item: Pick<ManagedMCPEntry, "slug" | "version" | "description">
) {
  return (
    (item.slug === REPORT_SYSTEM_MCP_SLUG && item.version === REPORT_SYSTEM_MCP_VERSION) ||
    Boolean(item.description?.includes(REPORT_SYSTEM_MARKER))
  );
}

export function getSystemBuiltinLabel() {
  return "系统内置";
}

export const isReportSystemSkill = isSystemBuiltinSkill;
export const isReportSystemMCP = isSystemBuiltinMCP;

export function reportAgentMarkerText(agent: ManagedAgent) {
  return [agent.description, agent.instructions, agent.start_prompt_template]
    .filter(Boolean)
    .join("\n");
}

export function isReportAgentAsset(agent: ManagedAgent) {
  if (agent.business_type === "report") return true;
  if (agent.business_type === "generic") return false;
  const text = reportAgentMarkerText(agent);
  return text.includes(REPORT_AGENT_MARKER) && text.includes(REPORT_MANAGED_AGENT_MARKER);
}

export function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : "请求失败";
}
