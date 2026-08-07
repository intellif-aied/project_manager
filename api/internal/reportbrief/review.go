package reportbrief

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	ReviewDecisionAccept       = "accept"
	ReviewDecisionRepair       = "repair"
	ReviewDecisionConservative = "conservative"
	ReviewModeAccepted         = "accepted"
	ReviewModeRepaired         = "repaired"
	ReviewModeConservative     = "conservative"
	maxReviewPatches           = 8
)

var reviewTargetPattern = regexp.MustCompile(`^w([1-5])(?:\.d([1-3]))?$`)

type ReviewDecision struct {
	Decision           string                    `json:"decision"`
	Issues             []ReviewIssue             `json:"issues,omitempty"`
	Patches            []ReviewPatch             `json:"patches,omitempty"`
	ProjectAttachments []ReviewProjectAttachment `json:"project_attachments,omitempty"`
}

type ReviewProjectAttachment struct {
	CanonicalName string   `json:"canonical_name"`
	Targets       []string `json:"targets"`
	FactRefs      []string `json:"fact_refs"`
}

type ReviewIssue struct {
	Code     string   `json:"code"`
	Target   string   `json:"target"`
	FactRefs []string `json:"fact_refs,omitempty"`
	Reason   string   `json:"reason,omitempty"`
}

type ReviewPatch struct {
	Op                 string   `json:"op"`
	Target             string   `json:"target,omitempty"`
	Value              string   `json:"value,omitempty"`
	Subject            string   `json:"subject,omitempty"`
	Title              string   `json:"title,omitempty"`
	Result             string   `json:"result,omitempty"`
	FactRefs           []string `json:"fact_refs,omitempty"`
	SupportingFactRefs []string `json:"supporting_fact_refs,omitempty"`
	Destination        string   `json:"destination,omitempty"`
}

type ReviewFinalized struct {
	Stored   Stored
	Mode     string
	Warnings []string
}

func FinalizeReview(candidate Stored, decision ReviewDecision, allowedFactRefs map[string]struct{}) (ReviewFinalized, error) {
	if candidate.Payload.ReportType != personalDaily || candidate.Payload.Period.Start == "" ||
		candidate.Payload.Period.End == "" || candidate.Payload.Period.Start != candidate.Payload.Period.End {
		return ReviewFinalized{}, fmt.Errorf("%w: personal daily candidate is required", ErrInvalid)
	}
	allowed := cloneRefSet(allowedFactRefs)
	for _, workstream := range candidate.Payload.Workstreams {
		for _, deliverable := range workstream.Deliverables {
			for _, ref := range deliverable.FactRefs {
				allowed[ref] = struct{}{}
			}
		}
	}

	switch strings.TrimSpace(decision.Decision) {
	case ReviewDecisionAccept:
		if len(decision.Issues) > 0 || len(decision.Patches) > 0 {
			return ReviewFinalized{}, fmt.Errorf("%w: accept cannot contain issues or patches", ErrInvalid)
		}
		return ReviewFinalized{Stored: candidate, Mode: ReviewModeAccepted}, nil
	case ReviewDecisionRepair:
		workstreams, err := applyReviewPatches(candidate.Payload.Workstreams, decision.Patches, allowed)
		if err == nil {
			return buildReviewedStored(candidate, workstreams, ReviewModeRepaired, nil)
		}
		workstreams = conservativeWorkstreams(candidate.Payload.Workstreams, decision.Issues)
		if len(workstreams) == 0 && len(candidate.Payload.Workstreams) > 0 {
			return ReviewFinalized{}, err
		}
		return buildReviewedStored(candidate, workstreams, ReviewModeConservative, []string{"review_patch_invalid"})
	case ReviewDecisionConservative:
		if len(decision.Issues) == 0 {
			return ReviewFinalized{}, fmt.Errorf("%w: conservative decision requires issues", ErrInvalid)
		}
		workstreams := conservativeWorkstreams(candidate.Payload.Workstreams, decision.Issues)
		return buildReviewedStored(candidate, workstreams, ReviewModeConservative, []string{"review_conservative"})
	default:
		return ReviewFinalized{}, fmt.Errorf("%w: unknown review decision", ErrInvalid)
	}
}

func applyReviewPatches(workstreams []Workstream, patches []ReviewPatch, allowed map[string]struct{}) ([]Workstream, error) {
	if len(patches) == 0 || len(patches) > maxReviewPatches {
		return nil, fmt.Errorf("%w: repair requires one to %d patches", ErrInvalid, maxReviewPatches)
	}
	result := cloneWorkstreams(workstreams)
	drops := make([]ReviewPatch, 0)
	merges := make([]ReviewPatch, 0)
	for _, patch := range patches {
		if patch.Op == "drop_deliverable" || patch.Op == "drop_workstream" {
			drops = append(drops, patch)
			continue
		}
		if patch.Op == "merge_workstream" {
			merges = append(merges, patch)
			continue
		}
		workstreamIndex, deliverableIndex, err := parseReviewTarget(patch.Target)
		if err != nil && patch.Op != "add_workstream" {
			return nil, err
		}
		if patch.Op != "add_workstream" && (workstreamIndex < 0 || workstreamIndex >= len(result)) {
			return nil, fmt.Errorf("%w: review workstream target is out of range", ErrInvalid)
		}
		switch patch.Op {
		case "replace_subject":
			value, refs := reviewReplacement(patch)
			if deliverableIndex >= 0 || !reviewTextSupported(value, refs, allowed) {
				return nil, fmt.Errorf("%w: invalid replace_subject patch", ErrInvalid)
			}
			result[workstreamIndex].Subject = value
		case "replace_title":
			value, refs := reviewReplacement(patch)
			if deliverableIndex >= 0 || !reviewTextSupported(value, refs, allowed) {
				return nil, fmt.Errorf("%w: invalid replace_title patch", ErrInvalid)
			}
			result[workstreamIndex].Title = value
		case "replace_text", "replace_result", "add_qualifier":
			value, refs := reviewReplacement(patch)
			if deliverableIndex < 0 || deliverableIndex >= len(result[workstreamIndex].Deliverables) ||
				!reviewTextSupported(value, refs, allowed) {
				return nil, fmt.Errorf("%w: invalid result patch", ErrInvalid)
			}
			result[workstreamIndex].Deliverables[deliverableIndex].Result = value
			result[workstreamIndex].Deliverables[deliverableIndex].FactRefs = normalizeFactRefs(refs)
		case "add_deliverable":
			if deliverableIndex >= 0 || len(result[workstreamIndex].Deliverables) >= readerMaxDeliverables ||
				!reviewTextSupported(patch.Result, patch.FactRefs, allowed) {
				return nil, fmt.Errorf("%w: invalid add_deliverable patch", ErrInvalid)
			}
			result[workstreamIndex].Deliverables = append(result[workstreamIndex].Deliverables, Deliverable{
				Result: patch.Result, FactRefs: normalizeFactRefs(patch.FactRefs),
			})
		case "add_workstream":
			if len(result) >= MaxWorkstreams || !reviewTextSupported(patch.Result, patch.FactRefs, allowed) ||
				!reviewTextSupported(patch.Subject, patch.FactRefs, allowed) ||
				!reviewTextSupported(patch.Title, patch.FactRefs, allowed) {
				return nil, fmt.Errorf("%w: invalid add_workstream patch", ErrInvalid)
			}
			result = append(result, Workstream{
				Subject: patch.Subject, Title: patch.Title,
				Deliverables: []Deliverable{{Result: patch.Result, FactRefs: normalizeFactRefs(patch.FactRefs)}},
			})
		default:
			return nil, fmt.Errorf("%w: unsupported review patch %q", ErrInvalid, patch.Op)
		}
	}
	var err error
	result, drops, err = applyReviewMerges(result, merges, drops)
	if err != nil {
		return nil, err
	}
	return applyReviewDrops(result, drops)
}

func reviewReplacement(patch ReviewPatch) (string, []string) {
	value := strings.TrimSpace(patch.Value)
	if value == "" {
		value = strings.TrimSpace(patch.Result)
	}
	refs := patch.SupportingFactRefs
	if len(refs) == 0 {
		refs = patch.FactRefs
	}
	return value, refs
}

func applyReviewMerges(workstreams []Workstream, merges, drops []ReviewPatch) ([]Workstream, []ReviewPatch, error) {
	mergedSources := map[int]bool{}
	for _, patch := range merges {
		source, sourceDeliverable, err := parseReviewTarget(patch.Target)
		if err != nil || sourceDeliverable >= 0 || source < 0 || source >= len(workstreams) {
			return nil, nil, fmt.Errorf("%w: merge source must be an existing workstream", ErrInvalid)
		}
		destination, destinationDeliverable, err := parseReviewTarget(patch.Destination)
		if err != nil || destinationDeliverable >= 0 || destination < 0 || destination >= len(workstreams) || destination == source {
			return nil, nil, fmt.Errorf("%w: merge destination must be another existing workstream", ErrInvalid)
		}
		if mergedSources[source] || mergedSources[destination] {
			return nil, nil, fmt.Errorf("%w: chained or duplicate workstream merge is not allowed", ErrInvalid)
		}
		if len(workstreams[destination].Deliverables)+len(workstreams[source].Deliverables) > MaxDeliverables {
			return nil, nil, fmt.Errorf("%w: merged workstream has too many deliverables", ErrInvalid)
		}
		workstreams[destination].Deliverables = append(
			workstreams[destination].Deliverables, workstreams[source].Deliverables...,
		)
		mergedSources[source] = true
		drops = append(drops, ReviewPatch{Op: "drop_workstream", Target: patch.Target})
	}
	return workstreams, drops, nil
}

func applyReviewDrops(workstreams []Workstream, drops []ReviewPatch) ([]Workstream, error) {
	workstreamDrops := map[int]bool{}
	deliverableDrops := map[int]map[int]bool{}
	for _, patch := range drops {
		wi, di, err := parseReviewTarget(patch.Target)
		if err != nil {
			return nil, err
		}
		switch patch.Op {
		case "drop_workstream":
			if di >= 0 {
				return nil, fmt.Errorf("%w: drop_workstream target must be a workstream", ErrInvalid)
			}
			workstreamDrops[wi] = true
		case "drop_deliverable":
			if di < 0 {
				return nil, fmt.Errorf("%w: drop_deliverable target must be a deliverable", ErrInvalid)
			}
			if deliverableDrops[wi] == nil {
				deliverableDrops[wi] = map[int]bool{}
			}
			deliverableDrops[wi][di] = true
		}
	}
	result := make([]Workstream, 0, len(workstreams))
	for wi, workstream := range workstreams {
		if workstreamDrops[wi] {
			continue
		}
		kept := workstream.Deliverables[:0]
		for di, deliverable := range workstream.Deliverables {
			if !deliverableDrops[wi][di] {
				kept = append(kept, deliverable)
			}
		}
		workstream.Deliverables = kept
		if len(kept) > 0 {
			result = append(result, workstream)
		}
	}
	return result, nil
}

func conservativeWorkstreams(workstreams []Workstream, issues []ReviewIssue) []Workstream {
	result := cloneWorkstreams(workstreams)
	drops := make([]ReviewPatch, 0)
	for _, issue := range issues {
		wi, di, err := parseReviewTarget(issue.Target)
		if err != nil || wi >= len(result) {
			continue
		}
		switch issue.Code {
		case "overclaim", "lost_qualifier":
			if di >= 0 && di < len(result[wi].Deliverables) {
				drops = append(drops, ReviewPatch{Op: "drop_deliverable", Target: issue.Target})
			} else if di < 0 {
				drops = append(drops, ReviewPatch{Op: "drop_workstream", Target: issue.Target})
			}
		case "unsupported_project", "memory_injection":
			if len(result[wi].Deliverables) > 0 {
				fallback := result[wi].Deliverables[0].Result
				result[wi].Subject = compactReaderText(fallback, readerSubjectRuneLimit)
				result[wi].Title = compactReaderText(fallback, readerHeadlineRuneLimit)
			}
		case "cross_project_merge":
			if di >= 0 && di < len(result[wi].Deliverables) {
				item := result[wi].Deliverables[di]
				result = append(result, Workstream{
					Subject:      compactReaderText(item.Result, readerSubjectRuneLimit),
					Title:        compactReaderText(item.Result, readerHeadlineRuneLimit),
					Deliverables: []Deliverable{item},
				})
				drops = append(drops, ReviewPatch{Op: "drop_deliverable", Target: issue.Target})
			} else if di < 0 {
				for _, item := range result[wi].Deliverables {
					result = append(result, Workstream{
						Subject:      compactReaderText(item.Result, readerSubjectRuneLimit),
						Title:        compactReaderText(item.Result, readerHeadlineRuneLimit),
						Deliverables: []Deliverable{item},
					})
				}
				drops = append(drops, ReviewPatch{Op: "drop_workstream", Target: issue.Target})
			}
		}
	}
	result, _ = applyReviewDrops(result, drops)
	if len(result) > MaxWorkstreams {
		result = result[:MaxWorkstreams]
	}
	return result
}

func buildReviewedStored(candidate Stored, workstreams []Workstream, mode string, warnings []string) (ReviewFinalized, error) {
	draft := Draft{Workstreams: workstreams}
	if len(workstreams) == 0 {
		draft.NoReportableWork = true
	}
	available := map[string]struct{}{}
	for _, workstream := range workstreams {
		for _, deliverable := range workstream.Deliverables {
			for _, ref := range deliverable.FactRefs {
				available[ref] = struct{}{}
			}
		}
	}
	payload, err := normalizeDraft(draft, personalDaily, candidate.Payload.Period, available)
	if err != nil {
		return ReviewFinalized{}, err
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return ReviewFinalized{}, err
	}
	sum := sha256.Sum256(encoded)
	return ReviewFinalized{
		Stored: Stored{Payload: payload, BriefHash: hex.EncodeToString(sum[:]), ContextHash: candidate.ContextHash},
		Mode:   mode, Warnings: warnings,
	}, nil
}

func reviewTextSupported(text string, refs []string, allowed map[string]struct{}) bool {
	text = strings.TrimSpace(text)
	refs = normalizeFactRefs(refs)
	if text == "" || len(refs) == 0 || !ReaderFacingTextSafe(text) {
		return false
	}
	for _, ref := range refs {
		if _, ok := allowed[ref]; !ok {
			return false
		}
	}
	return true
}

func parseReviewTarget(target string) (int, int, error) {
	match := reviewTargetPattern.FindStringSubmatch(strings.TrimSpace(target))
	if match == nil {
		return -1, -1, fmt.Errorf("%w: invalid review target", ErrInvalid)
	}
	wi, _ := strconv.Atoi(match[1])
	di := -1
	if match[2] != "" {
		di, _ = strconv.Atoi(match[2])
		di--
	}
	return wi - 1, di, nil
}

func cloneWorkstreams(values []Workstream) []Workstream {
	result := make([]Workstream, len(values))
	for i, value := range values {
		result[i] = value
		result[i].Deliverables = append([]Deliverable(nil), value.Deliverables...)
		for j := range result[i].Deliverables {
			result[i].Deliverables[j].FactRefs = append([]string(nil), value.Deliverables[j].FactRefs...)
		}
	}
	return result
}

func cloneRefSet(values map[string]struct{}) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result[value] = struct{}{}
		}
	}
	return result
}
