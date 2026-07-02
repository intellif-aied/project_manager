import { Checkbox, Empty, Select } from "antd";

import type { ManagedCredential, ManagedMCPEntry } from "../../api/types";
import {
  defaultMcpCredentialSlot,
  getSystemBuiltinLabel,
  isSystemBuiltinMCP,
  mcpLabel,
  refKey
} from "../utils/agentAssets";

import "./ResourcePicker.css";

function isSelectableCredential(credential: ManagedCredential) {
  const purpose = credential.metadata?.purpose ?? "";
  return !credential.archived && !purpose.includes("report_mcp_auth");
}

export function MCPResourcePicker({
  value = {},
  onChange,
  entries,
  credentials,
  slotCredentials,
  onSlotCredentialChange
}: {
  value?: Record<string, string>;
  onChange?: (value: Record<string, string>) => void;
  entries: ManagedMCPEntry[];
  credentials: ManagedCredential[];
  slotCredentials: Record<string, string>;
  onSlotCredentialChange?: (slot: string, credentialId: string) => void;
}) {
  if (!entries.length) {
    return <Empty className="ai-assets-resource-empty" description="暂无可绑定 MCP Server" />;
  }
  return (
    <div className="ai-assets-resource-picker">
      {entries.map((entry) => {
        const key = refKey(entry.owner, entry.slug, entry.version);
        const ownerlessKey = refKey(undefined, entry.slug, entry.version);
        const selectedKey = Object.prototype.hasOwnProperty.call(value, key)
          ? key
          : Object.prototype.hasOwnProperty.call(value, ownerlessKey)
            ? ownerlessKey
            : "";
        const checked = Boolean(selectedKey);
        const slot = checked
          ? value[selectedKey]
          : entry.requires_credential
            ? defaultMcpCredentialSlot(entry.slug, entry.version)
            : "";
        const title = `${entry.name || entry.slug}${isSystemBuiltinMCP(entry) ? `（${getSystemBuiltinLabel()}）` : ""}`;
        const emptyCredentialOption = isSystemBuiltinMCP(entry)
          ? "不绑定默认凭证（Aida 运行时注入）"
          : "不绑定默认凭证";
        const toggle = () => {
          const next = { ...value };
          delete next[key];
          delete next[ownerlessKey];
          if (!checked) {
            next[key] = entry.requires_credential ? slot : "";
          }
          onChange?.(next);
        };
        return (
          <div key={key} className={`ai-assets-resource-card${checked ? " is-selected" : ""}`}>
            <Checkbox checked={checked} onChange={toggle} />
            <span className="ai-assets-resource-card__body">
              <strong>{title}</strong>
              <span>{entry.description || entry.url || entry.command || "-"}</span>
              <em>
                {mcpLabel(entry)}
                {entry.requires_credential ? " · 需要凭据" : ""}
              </em>
              {checked && entry.requires_credential ? (
                <div className="ai-assets-resource-card__credential-control">
                  <Select
                    className="ai-assets-resource-card__credential-select"
                    classNames={{ popup: { root: "ai-assets-resource-credential-popup" } }}
                    value={slotCredentials[slot] || ""}
                    popupMatchSelectWidth
                    options={[
                      { label: emptyCredentialOption, value: "" },
                      ...credentials.filter(isSelectableCredential).map((credential) => ({
                        label: credential.name,
                        value: credential.credential_id
                      }))
                    ]}
                    onChange={(credentialId) => onSlotCredentialChange?.(slot, credentialId)}
                  />
                </div>
              ) : null}
            </span>
          </div>
        );
      })}
    </div>
  );
}
