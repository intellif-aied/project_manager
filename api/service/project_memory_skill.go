package service

import "strings"

const (
	ProjectMemorySkillSlug    = "aida-project-memory"
	ProjectMemorySkillName    = "Aida Project Memory Skill"
	ProjectMemorySkillVersion = "project-memory-v4"
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
3. First compare all current_theme items and build a parent map. Themes that clearly serve the same named product, project, protocol, or business capability must use the same parent canonical_name even when one theme is documentation, deployment, a module, or a fix. Do not let one report create separate parent projects for a parent and its child work.
4. Decide once for every current_theme:
   - link_existing: link to one supplied candidate project only.
   - create_new: create a stable project, product, protocol, or business-capability name.
   - unresolved: use when evidence is insufficient.
   - suggest_rename or suggest_merge: advisory only; do not rewrite existing memory.
5. Use accepted Brief parent-child relationships. A demo, scenario, module, candidate, experiment, document, test, deployment, or activity remains a detail of its parent project when the input supports that relationship.
6. Preserve established literal names and technical terms. Do not translate or invent professional-sounding labels.
7. History is only a naming/grouping hint. Never infer release state, completion state, recommendations, risks, or new work facts.
8. Keep canonical_name concise and stable. Never use a generic activity such as 修复、测试、部署、调研、优化、开发、验证、发布 or 上线 as a project name.
9. For link_existing and create_new, include aliases only for another short, reusable name of the same work object or a stable child capability explicitly present in the theme. A product manual or named module can be an alias; a script, component implementation, document rewrite, deployment, test, metric, state, date, outcome sentence, or invented synonym cannot. When a stable child name is the canonical parent name followed by a child phrase, include both the complete child name and the remaining child phrase when that phrase has at least three characters; for example, parent 芯片验证平台 and literal child 芯片验证平台使用手册 produce aliases 芯片验证平台使用手册 and 使用手册. Keep at most three aliases per decision and omit aliases when no safe name exists.
10. Produce exactly this root shape, with one decision per current theme: {"schema_version":"project-memory-proposal/v1","decisions":[{"theme_ref":"theme-001","action":"link_existing|create_new|unresolved|suggest_rename|suggest_merge","project_ref":"only for an existing project","canonical_name":"only for create_new","aliases":["optional stable name"],"confidence":0.9,"reason":"brief explanation"}]}.
11. Before writing, compare every pair of decisions again. If their themes belong to one parent, their project_ref or canonical_name must be identical. Remove artifact or activity aliases.
12. Call write_project_memory_result exactly once with {"proposal_json": <proposal object>}. Do not use proposals or decision fields in place of decisions and action. Do not return the proposal as chat text and do not call unrelated tools.
`
}

func ProjectMemoryAgentInstructions() string {
	return strings.Join([]string{
		"AIDA_SYSTEM_ASSET:project-memory",
		"AIDA_MANAGED:TRUE",
		"你是 Aida Project Memory 系统 Agent。每次运行必须加载 aida-project-memory Skill，并严格通过绑定的 aida-project-memory-mcp 完成读取与写回。Prompt 不定义业务规则，所有规则以 Skill 为准。",
	}, "\n")
}
