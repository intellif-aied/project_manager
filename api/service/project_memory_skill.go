package service

import "strings"

const (
	ProjectMemorySkillSlug    = "aida-project-memory"
	ProjectMemorySkillName    = "Aida Project Memory Skill"
	ProjectMemorySkillVersion = "project-memory-v10"
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
3. First compare all current_theme items and build a parent map. Themes that clearly serve the same named product, project, protocol, or business capability must use the same parent canonical_name even when one theme is documentation, deployment, a module, or a fix. Do not let one report create separate parent projects for a parent and its child work. workspace_refs are opaque continuity evidence supplied by Aida: repeated refs may support linking related themes, but never override a conflicting current title or create a project name by themselves.
4. candidate_projects is the stable parent-project pool recalled from up to 20 prior report snapshots. matched_theme_refs lists current themes matched by the candidate's canonical name or aliases. A candidate matching multiple current themes is strong parent-scope evidence. Prefer that parent over narrower candidates that each match only one child theme, especially when the parent has higher source_weight or a human source_type. An exact child-title match alone does not beat this parent evidence. recent_overviews contains up to 10 recent report overviews; historical_project_anchors contains up to 10 older, name-only anchors, for at most 20 historical reports in total. Never replace an established parent with a narrower daily task, model, module, experiment, defect, or optimization topic. History can support naming continuity but cannot override conflicting current-day evidence.
5. Decide once for every current_theme:
   - link_existing: link to one supplied candidate project only. Use confidence >= 0.88 when current-day evidence is strong enough to link; lower confidence is treated as unresolved by Aida.
   - create_new: create a stable project, product, protocol, or business-capability name.
   - unresolved: use when evidence is insufficient.
   - suggest_rename or suggest_merge: advisory only; do not rewrite existing memory.
6. Use accepted Brief parent-child relationships. A demo, scenario, module, candidate, experiment, document, test, deployment, or activity remains a detail of its parent project when the input supports that relationship.
7. Preserve established literal names and technical terms. Do not translate or invent professional-sounding labels.
8. History is only a naming/grouping hint. Never infer release state, completion state, recommendations, risks, or new work facts.
9. Keep canonical_name concise and stable. Never use a generic activity such as 修复、测试、部署、调研、优化、开发、验证、发布 or 上线 as a project name.
10. For link_existing and create_new, include aliases only for another short, reusable name of the same project or a stable child capability that can itself name the work. Keep at most three aliases per decision and omit aliases when no safe name exists.
11. Also extract workstream_cues from current_theme titles and brief_workstreams deliverables. A cue is a short literal module, tool, protocol, workflow, or durable work object that would help recognize the same parent project on another day, such as 调用执行、ctp CLI、版本流、调度器 or 用例筛选工作台. A script filename, implementation action, document rewrite, deployment, test result, metric, state, date, outcome sentence, generic activity, or invented synonym is not a cue. Keep at most five cues per decision. Never copy cues only from history without current-day support.
12. Produce exactly this root shape, with one decision per current theme: {"schema_version":"project-memory-proposal/v1","decisions":[{"theme_ref":"theme-001","action":"link_existing|create_new|unresolved|suggest_rename|suggest_merge","project_ref":"only for an existing project","canonical_name":"only for create_new","aliases":["optional stable name"],"workstream_cues":["optional stable work object"],"confidence":0.9,"reason":"brief explanation"}]}.
13. Before writing, compare every pair of decisions again. If their themes belong to one parent, their project_ref or canonical_name must be identical. Remove artifact, result, state, metric, and activity text from aliases and workstream_cues.
14. Call write_project_memory_result exactly once with {"proposal_json": <proposal object>}. Do not use proposals or decision fields in place of decisions and action. Do not return the proposal as chat text and do not call unrelated tools.
`
}

func ProjectMemoryAgentInstructions() string {
	return strings.Join([]string{
		"AIDA_SYSTEM_ASSET:project-memory",
		"AIDA_MANAGED:TRUE",
		"你是 Aida Project Memory 系统 Agent。每次运行必须加载 aida-project-memory Skill，并严格通过绑定的 aida-project-memory-mcp 完成读取与写回。Prompt 不定义业务规则，所有规则以 Skill 为准。",
	}, "\n")
}
