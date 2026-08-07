package reportreview

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/aidashboard/api/internal/reportbrief"
	"github.com/aidashboard/api/internal/reportcontext"
)

type contextEnvelope struct {
	WorkEvidence *struct {
		Facts []Fact `json:"facts"`
	} `json:"work_evidence"`
	ProjectMemoryContext *struct {
		Hints []struct {
			ProjectRef        string   `json:"project_ref"`
			CanonicalName     string   `json:"canonical_name"`
			MatchedFactRefs   []string `json:"matched_fact_refs"`
			SemanticFactRefs  []string `json:"semantic_fact_refs"`
			WorkspaceFactRefs []string `json:"workspace_fact_refs"`
			Confidence        float64  `json:"confidence"`
			CandidateOnly     bool     `json:"candidate_only"`
			MatchBasis        string   `json:"match_basis"`
			Aliases           []string `json:"aliases"`
			WorkstreamCues    []string `json:"workstream_cues"`
		} `json:"hints"`
	} `json:"project_memory_context"`
}

func BuildInput(runID string, context reportcontext.StoredContext, candidate reportbrief.Stored) (Input, []byte, error) {
	if strings.TrimSpace(runID) == "" || context.Hash == "" || candidate.BriefHash == "" ||
		candidate.ContextHash != context.Hash || candidate.Payload.ReportType != reportcontext.ReportTypePersonalDaily {
		return Input{}, nil, fmt.Errorf("invalid report review input identity")
	}
	var envelope contextEnvelope
	if err := json.Unmarshal(context.Payload, &envelope); err != nil || envelope.WorkEvidence == nil {
		return Input{}, nil, fmt.Errorf("invalid report review context")
	}
	byRef := make(map[string]Fact, len(envelope.WorkEvidence.Facts))
	for _, fact := range envelope.WorkEvidence.Facts {
		fact.FactRef = strings.TrimSpace(fact.FactRef)
		fact.Text = compactReviewFactText(fact.Text)
		if fact.FactRef == "" || fact.Text == "" {
			continue
		}
		byRef[fact.FactRef] = fact
	}
	selectedRefs := selectedFactRefs(candidate.Payload)
	selected := make([]Fact, 0, len(selectedRefs))
	allowed := map[string]struct{}{}
	for _, ref := range selectedRefs {
		fact, ok := byRef[ref]
		if !ok {
			return Input{}, nil, fmt.Errorf("candidate references unavailable fact %s", ref)
		}
		selected = append(selected, fact)
		allowed[ref] = struct{}{}
	}
	remaining := make([]Fact, 0, len(byRef)-len(selected))
	remainingSeen := map[string]struct{}{}
	appendRemaining := func(ref string) {
		ref = strings.TrimSpace(ref)
		if _, selected := allowed[ref]; selected {
			return
		}
		if _, seen := remainingSeen[ref]; seen {
			return
		}
		if fact, ok := byRef[ref]; ok {
			remaining = append(remaining, fact)
			remainingSeen[ref] = struct{}{}
		}
	}
	if envelope.ProjectMemoryContext != nil {
		for _, hint := range envelope.ProjectMemoryContext.Hints {
			for _, refs := range [][]string{hint.SemanticFactRefs, hint.WorkspaceFactRefs, hint.MatchedFactRefs} {
				for _, ref := range refs {
					appendRemaining(ref)
				}
			}
		}
	}
	projectFactCount := len(remaining)
	otherFacts := make([]Fact, 0, len(byRef)-len(selected)-projectFactCount)
	for _, original := range envelope.WorkEvidence.Facts {
		ref := strings.TrimSpace(original.FactRef)
		if _, selected := allowed[ref]; selected {
			continue
		}
		if _, seen := remainingSeen[ref]; seen {
			continue
		}
		if fact, ok := byRef[ref]; ok {
			otherFacts = append(otherFacts, fact)
		}
	}
	sort.SliceStable(otherFacts, func(i, j int) bool {
		return factPriority(otherFacts[i]) > factPriority(otherFacts[j])
	})
	remaining = append(remaining, otherFacts...)
	limit := maxReviewFacts - len(selected)
	if limit < 0 {
		limit = 0
	}
	if len(remaining) > limit {
		remaining = remaining[:limit]
	}
	for _, fact := range remaining {
		allowed[fact.FactRef] = struct{}{}
	}
	input := Input{
		SchemaVersion: ResolverVersion, RunID: runID, BriefHash: candidate.BriefHash,
		ContextHash: context.Hash, Candidate: candidate.Payload,
		SelectedFacts: selected, ReviewCandidates: remaining,
		AllowedFactRefs: sortedRefs(allowed),
	}
	if envelope.ProjectMemoryContext != nil {
		for _, hint := range envelope.ProjectMemoryContext.Hints {
			refs := append([]string{}, hint.SemanticFactRefs...)
			refs = append(refs, hint.WorkspaceFactRefs...)
			refs = append(refs, hint.MatchedFactRefs...)
			refs = intersectRefs(refs, allowed)
			if strings.TrimSpace(hint.CanonicalName) == "" || len(refs) == 0 {
				continue
			}
			candidate := ProjectCandidate{
				ProjectRef: hint.ProjectRef, CanonicalName: hint.CanonicalName,
				RelatedFactRefs: refs, MatchBasis: hint.MatchBasis,
				Confidence: hint.Confidence, CandidateOnly: hint.CandidateOnly,
				Aliases: boundedLabels(hint.Aliases), WorkstreamCues: boundedLabels(hint.WorkstreamCues),
			}
			if !hint.CandidateOnly && (len(candidate.Aliases) > 0 || len(candidate.WorkstreamCues) > 0) {
				candidate.IdentityUsage = "parent_label_for_matching_cues"
				candidate.ProposedTargets = matchingWorkstreamTargets(input.Candidate.Workstreams, candidate.WorkstreamCues)
				if len(candidate.ProposedTargets) < 2 {
					candidate.ProposedTargets = nil
				}
			}
			input.ProjectCandidates = append(input.ProjectCandidates, candidate)
		}
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return Input{}, nil, err
	}
	return input, payload, nil
}

func matchingWorkstreamTargets(workstreams []reportbrief.Workstream, cues []string) []string {
	targets := make([]string, 0, len(workstreams))
	for index, workstream := range workstreams {
		parts := []string{workstream.Subject, workstream.Title}
		for _, deliverable := range workstream.Deliverables {
			parts = append(parts, deliverable.Result)
		}
		text := strings.ToLower(strings.Join(parts, " "))
		for _, cue := range cues {
			cue = strings.ToLower(strings.TrimSpace(cue))
			if len([]rune(cue)) >= 3 && strings.Contains(text, cue) {
				targets = append(targets, fmt.Sprintf("w%d", index+1))
				break
			}
		}
	}
	return targets
}

func (input Input) AgentView() Input {
	input.Candidate = boundedCandidateForAgent(input.Candidate)
	input.Candidate.ExcludedFacts = nil
	return input
}

func boundedCandidateForAgent(payload reportbrief.Payload) reportbrief.Payload {
	result := payload
	result.Workstreams = make([]reportbrief.Workstream, len(payload.Workstreams))
	for workstreamIndex, workstream := range payload.Workstreams {
		workstream.Subject = compactReviewCandidateText(workstream.Subject, 36)
		workstream.Title = compactReviewCandidateText(workstream.Title, 52)
		workstream.Deliverables = make([]reportbrief.Deliverable, len(payload.Workstreams[workstreamIndex].Deliverables))
		for deliverableIndex, deliverable := range payload.Workstreams[workstreamIndex].Deliverables {
			deliverable.Result = compactReviewCandidateText(deliverable.Result, 120)
			deliverable.FactRefs = append([]string(nil), deliverable.FactRefs...)
			workstream.Deliverables[deliverableIndex] = deliverable
		}
		result.Workstreams[workstreamIndex] = workstream
	}
	return result
}

func compactReviewCandidateText(value string, limit int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	value = strings.NewReplacer("**", "", string(rune(96)), "").Replace(value)
	runes := []rune(value)
	if limit <= 0 || len(runes) <= limit {
		return value
	}
	for index := limit - 1; index >= limit/2; index-- {
		if strings.ContainsRune("。；;！!?", runes[index]) {
			return strings.TrimSpace(string(runes[:index+1]))
		}
	}
	end := limit
	if end > 1 {
		end--
	}
	return strings.TrimSpace(string(runes[:end])) + "…"
}

func compactReviewFactText(value string) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= maxReviewFactRunes {
		return value
	}
	const tailRunes = 240
	headRunes := maxReviewFactRunes - tailRunes - 1
	return strings.TrimSpace(string(runes[:headRunes])) + "…" + strings.TrimSpace(string(runes[len(runes)-tailRunes:]))
}

func boundedLabels(values []string) []string {
	result := make([]string, 0, min(len(values), 8))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len([]rune(value)) > 80 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
		if len(result) == 8 {
			break
		}
	}
	return result
}

func selectedFactRefs(payload reportbrief.Payload) []string {
	seen := map[string]struct{}{}
	refs := make([]string, 0)
	for _, workstream := range payload.Workstreams {
		for _, deliverable := range workstream.Deliverables {
			for _, ref := range deliverable.FactRefs {
				ref = strings.TrimSpace(ref)
				if ref == "" {
					continue
				}
				if _, ok := seen[ref]; !ok {
					seen[ref] = struct{}{}
					refs = append(refs, ref)
				}
			}
		}
	}
	return refs
}

func factPriority(fact Fact) int {
	source := strings.ToLower(strings.TrimSpace(fact.Source))
	switch {
	case strings.HasPrefix(source, "user"):
		return 4
	case strings.HasPrefix(source, "agent_claim_with_evidence"):
		return 2
	case strings.HasPrefix(source, "agent_claim"):
		return 1
	default:
		return 3
	}
}

func intersectRefs(values []string, allowed map[string]struct{}) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if _, ok := allowed[value]; !ok {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func sortedRefs(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
