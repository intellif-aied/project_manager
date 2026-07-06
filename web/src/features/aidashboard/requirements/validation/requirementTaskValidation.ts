import type { Rule } from "antd/es/form";

import {
  normalizeAcceptanceCriteria,
  type AcceptanceCriteriaValue
} from "../components/acceptanceCriteriaUtils";

const TITLE_MAX_LENGTH = 120;
const DESCRIPTION_MAX_LENGTH = 10_000;
const URL_MAX_LENGTH = 2048;
const ACCEPTANCE_CRITERIA_MAX_ITEMS = 50;
const ACCEPTANCE_CRITERIA_MAX_LENGTH = 1000;
const DEPENDENCY_MAX_ITEMS = 50;

const TITLE_SEPARATOR_PATTERN = /[\r\n\t]/;

function unicodeLength(value: string) {
  return Array.from(value).length;
}

function hasControlCharacter(value: string) {
  return Array.from(value).some((char) => {
    const code = char.charCodeAt(0);
    return (
      (code >= 0x00 && code <= 0x08) ||
      code === 0x0b ||
      code === 0x0c ||
      (code >= 0x0e && code <= 0x1f) ||
      code === 0x7f
    );
  });
}

function requiredTrimmedTextRule(label: string): Rule {
  return {
    required: true,
    whitespace: true,
    message: `请输入${label}`
  };
}

export function titleRules(label: string): Rule[] {
  return [
    requiredTrimmedTextRule(label),
    {
      validator: async (_, value?: string) => {
        const text = (value ?? "").trim();
        if (!text) return;
        if (TITLE_SEPARATOR_PATTERN.test(text)) {
          throw new Error(`${label}不能包含换行或制表符`);
        }
        if (hasControlCharacter(text)) {
          throw new Error(`${label}不能包含不可见控制字符`);
        }
        if (unicodeLength(text) > TITLE_MAX_LENGTH) {
          throw new Error(`${label}不能超过 ${TITLE_MAX_LENGTH} 个字符`);
        }
      }
    }
  ];
}

export function descriptionRules(label = "描述"): Rule[] {
  return [
    requiredTrimmedTextRule(label),
    {
      validator: async (_, value?: string) => {
        const text = (value ?? "").trim();
        if (!text) return;
        if (hasControlCharacter(text)) {
          throw new Error(`${label}不能包含不可见控制字符`);
        }
        if (unicodeLength(text) > DESCRIPTION_MAX_LENGTH) {
          throw new Error(`${label}不能超过 ${DESCRIPTION_MAX_LENGTH} 个字符`);
        }
      }
    }
  ];
}

export function optionalUrlRules(label = "链接"): Rule[] {
  return [
    {
      validator: async (_, value?: string) => {
        const text = (value ?? "").trim();
        if (!text) return;
        if (text.length > URL_MAX_LENGTH) {
          throw new Error(`${label}不能超过 ${URL_MAX_LENGTH} 个字符`);
        }
        let parsed: URL;
        try {
          parsed = new URL(text);
        } catch {
          throw new Error(`${label}必须是 http 或 https 链接`);
        }
        if (!["http:", "https:"].includes(parsed.protocol) || !parsed.hostname) {
          throw new Error(`${label}必须是 http 或 https 链接`);
        }
      }
    }
  ];
}

export function acceptanceCriteriaRules(label = "验收标准"): Rule[] {
  return [
    {
      validator: async (_, value?: AcceptanceCriteriaValue) => {
        const items = normalizeAcceptanceCriteria(value);
        if (items.length > ACCEPTANCE_CRITERIA_MAX_ITEMS) {
          throw new Error(`${label}最多 ${ACCEPTANCE_CRITERIA_MAX_ITEMS} 条`);
        }
        const invalid = items.find((item) => hasControlCharacter(item));
        if (invalid) {
          throw new Error(`${label}不能包含不可见控制字符`);
        }
        const tooLong = items.find((item) => unicodeLength(item) > ACCEPTANCE_CRITERIA_MAX_LENGTH);
        if (tooLong) {
          throw new Error(`${label}单条不能超过 ${ACCEPTANCE_CRITERIA_MAX_LENGTH} 个字符`);
        }
      }
    }
  ];
}

export function requiredSelectRules(label: string): Rule[] {
  return [{ required: true, message: `请选择${label}` }];
}

export function requiredArrayRules(label: string): Rule[] {
  return [{ required: true, type: "array", min: 1, message: `至少选择一个${label}` }];
}

export function dependencyArrayRules(): Rule[] {
  return [
    {
      validator: async (_, value?: string[]) => {
        if ((value ?? []).length > DEPENDENCY_MAX_ITEMS) {
          throw new Error(`上游依赖最多 ${DEPENDENCY_MAX_ITEMS} 个`);
        }
      }
    }
  ];
}

export function normalizeOptionalText(value?: string) {
  const text = value?.trim();
  return text || undefined;
}

export function normalizeRequiredText(value: string) {
  return value.trim();
}

export function normalizeCriteria(value: AcceptanceCriteriaValue) {
  return normalizeAcceptanceCriteria(value);
}
