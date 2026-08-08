package service

import "strings"

const (
	ProjectMemorySkillSlug    = "aida-project-memory"
	ProjectMemorySkillName    = "Aida Project Memory Skill"
	ProjectMemorySkillVersion = "project-memory-v11"
	ProjectMemoryMCPSlug      = "aida-project-memory-mcp"
	ProjectMemoryMCPVersion   = "project-memory-v1"
)

func ProjectMemorySkillMarkdown() string {
	return `---
name: aida-project-memory
description: Incrementally maintain one Aida user's private Project Memory from bounded evidence.
---

# Aida Project Memory Skill

Maintain naming and grouping memory for one user. This is not report writing.

1. Call get_project_memory_context exactly once with {}. Treat it as the complete bounded input; do not read Sessions, Git, files, reports, or unrelated MCPs.
2. Compare current_themes with current_memory and candidate_projects. Every current_theme carries an evidence_ref, report_date, source_type, and bounded workspace_refs. History and workspace refs are advisory continuity signals only. They never prove today's work and never override conflicting current evidence.
3. Preserve literal project names and technical terms. Do not translate, invent professional labels, or use activities such as 修复、测试、部署、调研、优化、开发、验证、发布、上线 as project names.
4. Prefer a stable parent project when several themes clearly serve the same named product, protocol, platform, or business capability. A module, model, demo, document, test, deployment, defect, or optimization is normally a workstream cue, not a new project.
5. Return only useful changes. It is valid and preferred to return operations: [] when current Memory already fits or evidence is insufficient. Do not produce one forced decision per theme.
6. Every operation must include evidence_refs from current_themes. A theme operation must include that theme's evidence_ref. Never create a Signal or Workspace link without cited current input evidence.
7. Supported operations:
   - create_project: requires theme_ref, a unique temp_ref, and canonical_name.
   - link_existing: requires theme_ref and one supplied project_ref; use confidence >= 0.88.
   - upsert_signal / retire_signal: maintain alias or workstream_cue on an existing project or a prior temp_ref.
   - link_workspace / unlink_workspace: maintain an opaque workspace_ref only when current evidence supports it.
   - archive_project: retire an AI-created project only when later cited evidence shows it is obsolete or erroneous.
   - noop / unresolved: explicitly settle a theme when useful; these are optional.
8. An alias is another short literal name of the same project. A workstream cue is a reusable literal module, model, tool, protocol, workflow, or durable work object. Never store result sentences, dates, metrics, states, recommendations, file names, commands, hashes, or invented synonyms.
9. Human-edited and manual-report names are authoritative. Never retire, remove, merge, or overwrite them. AI signals may be retired when later evidence contradicts them.
10. Operations execute in array order. Give every operation a unique operation_id. A create_project may expose temp_ref; dependent operations must list the creator in depends_on and use that temp_ref as project_ref.
11. Produce exactly: {"schema_version":"project-memory-maintenance/v2","operations":[{"operation_id":"op-001","operation":"create_project|link_existing|upsert_signal|retire_signal|link_workspace|unlink_workspace|archive_project|noop|unresolved","theme_ref":"when applicable","evidence_refs":["required input evidence_ref"],"project_ref":"existing project_ref or prior temp_ref","temp_ref":"create_project only","depends_on":["prior operation_id"],"canonical_name":"create_project only","signal_type":"alias|workstream_cue","value":"signal value","workspace_ref":"workspace operation only","confidence":0.9,"reason":"brief evidence-based reason"}]}.
12. Call write_project_memory_result exactly once with {"proposal_json": <proposal object>}. Do not return the proposal as chat text and do not call unrelated tools.
`
}

func ProjectMemoryAgentInstructions() string {
	return strings.Join([]string{
		"AIDA_SYSTEM_ASSET:project-memory",
		"AIDA_MANAGED:TRUE",
		"你是 Aida Project Memory 系统 Agent。每次运行必须加载 aida-project-memory Skill，并严格通过绑定的 aida-project-memory-mcp 完成读取与写回。Prompt 不定义业务规则，所有规则以 Skill 为准。",
	}, "\n")
}
