package service

const (
	ReportReviewSkillSlug    = "aida-report-review"
	ReportReviewSkillName    = "Aida Report Review Skill"
	ReportReviewSkillVersion = "report-review-v8"
	ReportReviewMCPSlug      = "aida-report-review-mcp"
	ReportReviewMCPVersion   = "report-review-v2"
)

func ReportReviewSkillMarkdown() string {
	return `---
name: aida-report-review
description: Independently verify one frozen Aida personal daily report proposal and submit one bounded semantic review.
---

# Aida Report Review Skill

Review one candidate report. This is an independent verifier, not another report generator.

1. Call get_report_review_context exactly once with {}.
2. Treat selected_facts and review_candidates as the only evidence about today's work.
3. Treat project_candidates only as optional user-established project labels. A canonical_name is an opaque business name: do not translate it or reject it from the everyday meaning of its words. It is not completion evidence and must never add work facts.
4. Check every candidate result for direct support, preserved uncertainty, environment and enablement limits, project contamination, incorrect merges, project fragmentation, and obvious omission of a higher-value frozen review candidate.
5. Independently compare candidate Workstreams with project_candidates. aliases and workstream_cues explain the established scope of a canonical_name; use them for identity matching, never as work evidence. identity_usage=parent_label_for_matching_cues means the Resolver has established how to use that opaque label: when related Facts and cues match at least two Workstreams or most substantive work, attach those Workstreams to canonical_name unless a current Fact explicitly names a conflicting parent. Do not override this mapping from the everyday meaning of the project name. Keep each title as its concrete subtopic. Merge only genuinely duplicate Workstreams within schema limits. Candidates without this identity_usage remain weak optional history.
6. Never add advice, release judgement, recommendations, future work, internal identifiers, paths, hashes, commands, or facts outside allowed_fact_refs.
7. proposed_targets are Resolver proposals based on current Workstream cues, not historical facts. decision=accept with project_attachments=[] approves those proposals and the service applies them deterministically. If a proposal conflicts with a current Fact or joins unrelated work, do not accept it: use decision=repair or conservative with unsupported_project issues. You may submit explicit project_attachments to correct or extend a supported proposal. Do not emit replace_subject patches for an attachment.
8. If the candidate is otherwise faithful, write decision=accept with no patches. project_attachments may still be non-empty.
9. If it needs other correction, write decision=repair and use at most 8 patches. Allowed ops are replace_subject, replace_title, replace_result, add_qualifier, drop_deliverable, drop_workstream, merge_workstream, add_deliverable, and add_workstream.
10. Targets use w1..w5 and w1.d1..w5.d3 from candidate order. replace_* and add_qualifier use value plus supporting_fact_refs. add_workstream/add_deliverable use result plus fact_refs. merge_workstream uses only target as the child source and destination as the parent Workstream. All refs must come from allowed_fact_refs.
11. add_workstream/add_deliverable may restore at most one obvious omitted outcome from review_candidates. Do not turn every Fact into report content.
12. If risk is certain but no safe correction can be written, use decision=conservative with issues identifying exact targets and project_attachments=[].
13. Call write_report_review exactly once. Do not retry, write files, call unrelated tools, or return the review as chat text.
`
}

func ReportReviewAgentInstructions() string {
	return "Use the installed aida-report-review Skill for every run. Read only the bound Aida Report Review MCP context and submit exactly one review decision."
}

func ReportReviewAgentStartPrompt() string {
	return "/aida-report-review"
}
