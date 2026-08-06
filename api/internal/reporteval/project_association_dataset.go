package reporteval

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const ProjectAssociationDatasetSchemaVersion = "project-association-regression/v1"

type ProjectAssociationDataset struct {
	SchemaVersion            string                   `json:"schema_version"`
	DatasetVersion           string                   `json:"dataset_version"`
	ControlledArchiveVersion string                   `json:"controlled_archive_version"`
	Cases                    []ProjectAssociationCase `json:"cases"`
}

type ProjectAssociationCase struct {
	CaseID              string                      `json:"case_id"`
	ReportDate          string                      `json:"report_date"`
	SourceSetSHA256     string                      `json:"source_set_sha256"`
	SourceItemCount     int                         `json:"source_item_count"`
	Tags                []string                    `json:"tags"`
	ExpectedWorkstreams []ExpectedProjectWorkstream `json:"expected_workstreams"`
	ForbiddenMerges     []ForbiddenProjectMerge     `json:"forbidden_merges"`
}

type ExpectedProjectWorkstream struct {
	CanonicalName string   `json:"canonical_name"`
	Aliases       []string `json:"aliases"`
	Requirement   string   `json:"requirement"`
	FactScope     string   `json:"fact_scope"`
}

type ForbiddenProjectMerge struct {
	Left  string `json:"left"`
	Right string `json:"right"`
}

const ProjectAssociationCandidatesSchemaVersion = "project-association-candidates/v1"

// ProjectAssociationCandidates records the structured Brief subjects produced by
// a candidate run. It deliberately excludes prose scoring so association gates
// can be evaluated without asking another model to judge the output.
type ProjectAssociationCandidates struct {
	SchemaVersion  string                        `json:"schema_version"`
	DatasetVersion string                        `json:"dataset_version"`
	Cases          []ProjectAssociationCandidate `json:"cases"`
}

type ProjectAssociationCandidate struct {
	CaseID             string   `json:"case_id"`
	RunID              string   `json:"run_id"`
	WorkstreamSubjects []string `json:"workstream_subjects"`
}

type ProjectAssociationEvaluation struct {
	DatasetVersion string                             `json:"dataset_version"`
	Passed         bool                               `json:"passed"`
	Cases          []ProjectAssociationCaseEvaluation `json:"cases"`
}

type ProjectAssociationCaseEvaluation struct {
	CaseID string   `json:"case_id"`
	RunID  string   `json:"run_id"`
	Passed bool     `json:"passed"`
	Errors []string `json:"errors,omitempty"`
}

func LoadProjectAssociationDataset(path string) (ProjectAssociationDataset, error) {
	var dataset ProjectAssociationDataset
	if err := decodeStrictJSONFile(path, &dataset); err != nil {
		return dataset, err
	}
	if err := dataset.Validate(); err != nil {
		return dataset, fmt.Errorf("invalid Project Association dataset: %w", err)
	}
	return dataset, nil
}

func LoadProjectAssociationCandidates(path string) (ProjectAssociationCandidates, error) {
	var candidates ProjectAssociationCandidates
	if err := decodeStrictJSONFile(path, &candidates); err != nil {
		return candidates, err
	}
	if candidates.SchemaVersion != ProjectAssociationCandidatesSchemaVersion || strings.TrimSpace(candidates.DatasetVersion) == "" {
		return candidates, errors.New("candidate identity is invalid")
	}
	seen := map[string]bool{}
	for index, candidate := range candidates.Cases {
		if !safeIdentifierPattern.MatchString(candidate.CaseID) || seen[candidate.CaseID] {
			return candidates, fmt.Errorf("cases[%d].case_id must be unique and non-empty", index)
		}
		seen[candidate.CaseID] = true
		if strings.TrimSpace(candidate.RunID) == "" {
			return candidates, fmt.Errorf("cases[%d].run_id is required", index)
		}
		if len(uniqueNonEmpty(candidate.WorkstreamSubjects)) != len(candidate.WorkstreamSubjects) {
			return candidates, fmt.Errorf("cases[%d].workstream_subjects must be normalized and unique", index)
		}
	}
	return candidates, nil
}

func EvaluateProjectAssociation(dataset ProjectAssociationDataset, candidates ProjectAssociationCandidates) (ProjectAssociationEvaluation, error) {
	if err := dataset.Validate(); err != nil {
		return ProjectAssociationEvaluation{}, err
	}
	if candidates.DatasetVersion != dataset.DatasetVersion {
		return ProjectAssociationEvaluation{}, errors.New("candidate dataset_version does not match manifest")
	}
	caseByID := make(map[string]ProjectAssociationCase, len(dataset.Cases))
	for _, item := range dataset.Cases {
		caseByID[item.CaseID] = item
	}
	result := ProjectAssociationEvaluation{DatasetVersion: dataset.DatasetVersion, Passed: true}
	for _, candidate := range candidates.Cases {
		item, ok := caseByID[candidate.CaseID]
		if !ok {
			return ProjectAssociationEvaluation{}, fmt.Errorf("candidate references unknown case %s", candidate.CaseID)
		}
		caseResult := ProjectAssociationCaseEvaluation{CaseID: candidate.CaseID, RunID: candidate.RunID, Passed: true}
		matchIndexes := map[string][]int{}
		for _, expected := range item.ExpectedWorkstreams {
			matchIndexes[expected.CanonicalName] = matchingSubjectIndexes(candidate.WorkstreamSubjects, expected)
			if expected.Requirement == "required" && len(matchIndexes[expected.CanonicalName]) == 0 {
				caseResult.Errors = append(caseResult.Errors, "missing required workstream: "+expected.CanonicalName)
			}
			if expected.Requirement == "forbidden" && len(matchIndexes[expected.CanonicalName]) > 0 {
				caseResult.Errors = append(caseResult.Errors, "found forbidden workstream: "+expected.CanonicalName)
			}
		}
		for _, forbidden := range item.ForbiddenMerges {
			if sharesSubject(matchIndexes[forbidden.Left], matchIndexes[forbidden.Right]) {
				caseResult.Errors = append(caseResult.Errors, fmt.Sprintf("forbidden merge: %s + %s", forbidden.Left, forbidden.Right))
			}
		}
		caseResult.Passed = len(caseResult.Errors) == 0
		if !caseResult.Passed {
			result.Passed = false
		}
		result.Cases = append(result.Cases, caseResult)
	}
	return result, nil
}

func matchingSubjectIndexes(subjects []string, expected ExpectedProjectWorkstream) []int {
	labels := append([]string{expected.CanonicalName}, expected.Aliases...)
	var indexes []int
	for index, subject := range subjects {
		normalizedSubject := strings.ToLower(strings.TrimSpace(subject))
		for _, label := range labels {
			normalizedLabel := strings.ToLower(strings.TrimSpace(label))
			if normalizedLabel != "" && strings.Contains(normalizedSubject, normalizedLabel) {
				indexes = append(indexes, index)
				break
			}
		}
	}
	return indexes
}

func sharesSubject(left, right []int) bool {
	for _, leftIndex := range left {
		for _, rightIndex := range right {
			if leftIndex == rightIndex {
				return true
			}
		}
	}
	return false
}

func (dataset ProjectAssociationDataset) Validate() error {
	if dataset.SchemaVersion != ProjectAssociationDatasetSchemaVersion ||
		strings.TrimSpace(dataset.DatasetVersion) == "" || strings.TrimSpace(dataset.ControlledArchiveVersion) == "" {
		return errors.New("dataset identity is invalid")
	}
	if len(dataset.Cases) == 0 {
		return errors.New("at least one Project Association case is required")
	}
	seenCases := map[string]bool{}
	for index, item := range dataset.Cases {
		prefix := fmt.Sprintf("cases[%d]", index)
		if !safeIdentifierPattern.MatchString(item.CaseID) || seenCases[item.CaseID] {
			return fmt.Errorf("%s.case_id must be unique and non-empty", prefix)
		}
		seenCases[item.CaseID] = true
		if _, err := time.Parse("2006-01-02", item.ReportDate); err != nil {
			return fmt.Errorf("%s.report_date must be YYYY-MM-DD", prefix)
		}
		if !isSHA256(item.SourceSetSHA256) || item.SourceItemCount <= 0 {
			return fmt.Errorf("%s source identity is invalid", prefix)
		}
		if len(uniqueNonEmpty(item.Tags)) != len(item.Tags) || len(item.Tags) == 0 {
			return fmt.Errorf("%s.tags must be non-empty and unique", prefix)
		}
		knownProjects := map[string]bool{}
		for projectIndex, project := range item.ExpectedWorkstreams {
			projectPrefix := fmt.Sprintf("%s.expected_workstreams[%d]", prefix, projectIndex)
			project.CanonicalName = strings.TrimSpace(project.CanonicalName)
			if project.CanonicalName == "" || knownProjects[project.CanonicalName] {
				return fmt.Errorf("%s.canonical_name must be unique and non-empty", projectPrefix)
			}
			knownProjects[project.CanonicalName] = true
			if project.Requirement != "required" && project.Requirement != "allowed" && project.Requirement != "forbidden" {
				return fmt.Errorf("%s.requirement is invalid", projectPrefix)
			}
			if project.FactScope != "all_reportable" && project.FactScope != "matching_facts" {
				return fmt.Errorf("%s.fact_scope is invalid", projectPrefix)
			}
			if len(uniqueNonEmpty(project.Aliases)) != len(project.Aliases) {
				return fmt.Errorf("%s.aliases must be normalized and unique", projectPrefix)
			}
		}
		if len(knownProjects) == 0 {
			return fmt.Errorf("%s.expected_workstreams is required", prefix)
		}
		seenPairs := map[string]bool{}
		for mergeIndex, merge := range item.ForbiddenMerges {
			mergePrefix := fmt.Sprintf("%s.forbidden_merges[%d]", prefix, mergeIndex)
			left, right := strings.TrimSpace(merge.Left), strings.TrimSpace(merge.Right)
			if left == right || !knownProjects[left] || !knownProjects[right] {
				return fmt.Errorf("%s must reference two distinct expected workstreams", mergePrefix)
			}
			if left > right {
				left, right = right, left
			}
			key := left + "\x00" + right
			if seenPairs[key] {
				return fmt.Errorf("%s duplicates a forbidden merge", mergePrefix)
			}
			seenPairs[key] = true
		}
	}
	return nil
}
