package service

import "strings"

const (
	ProjectMemorySkillSlug    = "aida-project-memory"
	ProjectMemorySkillName    = "Aida Project Memory Skill"
	ProjectMemorySkillVersion = "project-memory-v1"
	ProjectMemoryMCPSlug      = "aida-project-memory-mcp"
	ProjectMemoryMCPVersion   = "project-memory-v1"
)

func ProjectMemorySkillMarkdown() string {
	return `---
name: aida-project-memory
description: Consolidate one Aida user's bounded daily-report themes into private Project Memory.
---

# Aida Project Memory Skill

Maintain naming and grouping memory for one user. This is not report writing.

1. Call get_project_memory_context exactly once with {}.
2. Treat the returned Context as the complete input. Do not read Sessions, Git, files, reports, or any other MCP.
3. Decide once for every current_theme:
   - link_existing: link to one supplied candidate project only.
   - create_project: create a stable project, product, protocol, or business-capability name.
   - unresolved: use when evidence is insufficient.
   - suggest_rename or suggest_merge: advisory only; do not rewrite existing memory.
4. Use accepted Brief parent-child relationships. A demo, scenario, module, candidate, experiment, document, test, deployment, or activity remains an alias/detail of its parent project when the input supports that relationship.
5. Preserve established literal names and technical terms. Do not translate or invent professional-sounding labels.
6. History is only a naming/grouping hint. Never infer release state, completion state, recommendations, risks, or new work facts.
7. Keep canonical_name concise and stable. Never use a generic activity such as 修复、测试、部署、调研、优化、开发、验证、发布 or 上线 as a project name.
8. Produce schema project-memory-proposal/v1 with exactly one decision per current theme. Use confidence from 0 to 1.
9. Call write_project_memory_result exactly once with {"proposal_json": <proposal object>}. Do not return the proposal as chat text and do not call unrelated tools.
`
}

func ProjectMemoryAgentInstructions() string {
	return strings.Join([]string{
		"AIDA_SYSTEM_ASSET:project-memory",
		"AIDA_MANAGED:TRUE",
		"你是 Aida Project Memory 系统 Agent。每次运行必须加载 aida-project-memory Skill，并严格通过绑定的 aida-project-memory-mcp 完成读取与写回。Prompt 不定义业务规则，所有规则以 Skill 为准。",
	}, "\n")
}
