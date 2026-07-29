package reporteval

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type ReviewIssue struct {
	ErrorType     string   `json:"error_type"`
	Severity      string   `json:"severity"`
	FirstBadStage string   `json:"first_bad_stage"`
	EvidenceRefs  []string `json:"evidence_refs"`
	FinalRefs     []string `json:"affected_final_refs"`
	Explanation   string   `json:"explanation"`
}

type CaseReview struct {
	CaseID           string        `json:"case_id"`
	Repetition       int           `json:"repetition"`
	VariantVersion   string        `json:"variant_version"`
	CandidateAlias   string        `json:"candidate_alias,omitempty"`
	Grade            string        `json:"grade"`
	DirectlyUsable   bool          `json:"directly_usable"`
	Issues           []ReviewIssue `json:"issues"`
	Confidence       float64       `json:"confidence"`
	NeedsHumanReview bool          `json:"needs_human_review"`
	ReviewSource     string        `json:"review_source"`
	ReviewerModel    string        `json:"reviewer_model"`
	RubricVersion    string        `json:"rubric_version,omitempty"`
	PromptSHA256     string        `json:"prompt_sha256,omitempty"`
	SkillSHA256      string        `json:"skill_sha256,omitempty"`
	ElapsedMS        int64         `json:"elapsed_ms,omitempty"`
	InputSHA256      string        `json:"input_sha256"`
	OutputSHA256     string        `json:"output_sha256"`
}

type VariantScorecard struct {
	VariantVersion     string         `json:"variant_version"`
	Pass               int            `json:"pass"`
	Minor              int            `json:"minor"`
	Unacceptable       int            `json:"unacceptable"`
	ReviewedCompleted  int            `json:"reviewed_completed"`
	DirectlyUsableRate float64        `json:"directly_usable_rate"`
	CleanPassRate      float64        `json:"clean_pass_rate"`
	RunTotal           int            `json:"run_total"`
	RunSucceeded       int            `json:"run_succeeded"`
	SuccessRate        float64        `json:"success_rate"`
	AverageDurationMS  float64        `json:"average_duration_ms"`
	P95DurationMS      float64        `json:"p95_duration_ms"`
	ErrorTypes         map[string]int `json:"error_types"`
	FirstBadStages     map[string]int `json:"first_bad_stages"`
}

type VariantComparison struct {
	CandidateVariant string   `json:"candidate_variant"`
	Wins             int      `json:"wins"`
	Ties             int      `json:"ties"`
	Losses           int      `json:"losses"`
	FixedCases       []string `json:"fixed_cases"`
	RegressedCases   []string `json:"regressed_cases"`
	Conclusion       string   `json:"conclusion"`
}

type EvaluationResult struct {
	SchemaVersion   string              `json:"schema_version"`
	BaselineVariant string              `json:"baseline_variant"`
	Scorecards      []VariantScorecard  `json:"scorecards"`
	Comparisons     []VariantComparison `json:"comparisons"`
	AIReviews       []CaseReview        `json:"ai_reviews"`
	GoldReviews     []CaseReview        `json:"gold_reviews"`
	Missing         []string            `json:"missing"`
}

var allowedErrorTypes = map[string]bool{
	"FACT_OMISSION": true, "FACT_HALLUCINATION": true, "STATUS_UPGRADE": true,
	"ENVIRONMENT_MIX": true, "WRONG_GROUPING": true, "OVER_COMPRESSION": true,
	"NOISE_RETENTION": true, "INTERNAL_LEAKAGE": true, "REPETITION": true,
	"POOR_READABILITY": true,
}

func AggregateReviews(bundleDir, aiReviewPath, goldReviewPath string) (EvaluationResult, error) {
	var manifest BundleManifest
	if err := decodeJSONFile(filepath.Join(bundleDir, "manifest.json"), &manifest); err != nil {
		return EvaluationResult{}, err
	}
	if len(manifest.Variants) < 2 {
		return EvaluationResult{}, fmt.Errorf("bundle has no candidate variant")
	}
	aiReviews, err := readReviewJSONL(aiReviewPath, "ai")
	if err != nil {
		return EvaluationResult{}, err
	}
	goldReviews := []CaseReview{}
	if goldReviewPath != "" {
		goldReviews, err = readReviewJSONL(goldReviewPath, "gold")
		if err != nil {
			return EvaluationResult{}, err
		}
	}
	result := EvaluationResult{
		SchemaVersion:   "daily-report-evaluation-result/v1",
		BaselineVariant: manifest.Variants[0].VariantVersion,
		AIReviews:       aiReviews, GoldReviews: goldReviews, Missing: []string{},
	}
	aiByKey := reviewMap(aiReviews)
	goldByKey := reviewMap(goldReviews)
	effective := map[string]CaseReview{}
	for _, run := range manifest.Runs {
		if run.Status != "succeeded" {
			continue
		}
		key := reviewKey(run.CaseID, run.Repetition, run.VariantVersion)
		review, ok := aiByKey[key]
		if !ok {
			result.Missing = append(result.Missing, "missing_ai_review:"+key)
			continue
		}
		effective[key] = review
		if gold, ok := goldByKey[key]; ok {
			effective[key] = gold
		} else if review.Grade == "unacceptable" || review.Confidence < 0.8 || review.NeedsHumanReview {
			result.Missing = append(result.Missing, "missing_gold_review:"+key)
		}
	}
	for _, variant := range manifest.Variants {
		result.Scorecards = append(result.Scorecards, buildScorecard(bundleDir, manifest.Runs, variant.VariantVersion, effective))
		hasGoldSample := false
		for _, review := range goldReviews {
			if review.VariantVersion == variant.VariantVersion {
				hasGoldSample = true
				break
			}
		}
		if !hasGoldSample {
			result.Missing = append(result.Missing, "missing_gold_sample:"+variant.VariantVersion)
		}
	}
	baselineScores := result.Scorecards[0]
	for index := 1; index < len(manifest.Variants); index++ {
		candidate := manifest.Variants[index].VariantVersion
		comparison := compareVariant(manifest.Runs, result.BaselineVariant, candidate, effective)
		candidateScores := result.Scorecards[index]
		for _, caseID := range comparison.RegressedCases {
			separator := strings.LastIndex(caseID, "/")
			repetition, _ := strconv.Atoi(caseID[separator+1:])
			key := reviewKey(caseID[:separator], repetition, candidate)
			if _, hasGold := goldByKey[key]; !hasGold {
				result.Missing = append(result.Missing, "missing_gold_regression:"+caseID+":"+candidate)
			}
		}
		switch {
		case len(result.Missing) > 0:
			comparison.Conclusion = "evidence_insufficient"
		case len(comparison.RegressedCases) > 0:
			comparison.Conclusion = "improvement_not_supported"
		case candidateScores.DirectlyUsableRate > baselineScores.DirectlyUsableRate ||
			candidateScores.DirectlyUsableRate == baselineScores.DirectlyUsableRate &&
				candidateScores.CleanPassRate > baselineScores.CleanPassRate:
			comparison.Conclusion = "improvement_supported"
		default:
			comparison.Conclusion = "improvement_not_supported"
		}
		result.Comparisons = append(result.Comparisons, comparison)
	}
	sort.Strings(result.Missing)
	return result, nil
}

func buildScorecard(bundleDir string, runs []RunReceipt, variant string, reviews map[string]CaseReview) VariantScorecard {
	score := VariantScorecard{VariantVersion: variant, ErrorTypes: map[string]int{}, FirstBadStages: map[string]int{}}
	durations := []float64{}
	for _, run := range runs {
		if run.VariantVersion != variant {
			continue
		}
		score.RunTotal++
		if run.Status == "succeeded" {
			score.RunSucceeded++
		}
		var metrics RunMetrics
		path := filepath.Join(bundleDir, "cases", run.CaseID, "runs", run.RunID, "run-metrics.json")
		if decodeJSONFile(path, &metrics) == nil && metrics.DurationMS != nil {
			durations = append(durations, float64(*metrics.DurationMS))
		}
		review, ok := reviews[reviewKey(run.CaseID, run.Repetition, variant)]
		if !ok {
			continue
		}
		score.ReviewedCompleted++
		switch review.Grade {
		case "pass":
			score.Pass++
		case "minor":
			score.Minor++
		case "unacceptable":
			score.Unacceptable++
		}
		for _, issue := range review.Issues {
			score.ErrorTypes[issue.ErrorType]++
			score.FirstBadStages[issue.FirstBadStage]++
		}
	}
	if score.RunTotal > 0 {
		score.SuccessRate = float64(score.RunSucceeded) / float64(score.RunTotal)
	}
	if score.ReviewedCompleted > 0 {
		score.DirectlyUsableRate = float64(score.Pass+score.Minor) / float64(score.ReviewedCompleted)
		score.CleanPassRate = float64(score.Pass) / float64(score.ReviewedCompleted)
	}
	if len(durations) > 0 {
		sort.Float64s(durations)
		total := 0.0
		for _, value := range durations {
			total += value
		}
		score.AverageDurationMS = total / float64(len(durations))
		index := int(math.Ceil(float64(len(durations))*0.95)) - 1
		score.P95DurationMS = durations[index]
	}
	return score
}

func compareVariant(runs []RunReceipt, baseline, candidate string, reviews map[string]CaseReview) VariantComparison {
	result := VariantComparison{CandidateVariant: candidate, FixedCases: []string{}, RegressedCases: []string{}}
	type pair struct{ baseline, candidate int }
	pairs := map[string]pair{}
	for _, run := range runs {
		if run.VariantVersion != baseline && run.VariantVersion != candidate {
			continue
		}
		key := fmt.Sprintf("%s/%d", run.CaseID, run.Repetition)
		value := pairs[key]
		rank := 3
		if run.Status == "succeeded" {
			rank = gradeRank(reviews[reviewKey(run.CaseID, run.Repetition, run.VariantVersion)].Grade)
		}
		if run.VariantVersion == baseline {
			value.baseline = rank
		} else {
			value.candidate = rank
		}
		pairs[key] = value
	}
	for key, value := range pairs {
		switch {
		case value.candidate < value.baseline:
			result.Wins++
			result.FixedCases = append(result.FixedCases, key)
		case value.candidate > value.baseline:
			result.Losses++
			result.RegressedCases = append(result.RegressedCases, key)
		default:
			result.Ties++
		}
	}
	sort.Strings(result.FixedCases)
	sort.Strings(result.RegressedCases)
	return result
}

func readReviewJSONL(path, source string) ([]CaseReview, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	result := []CaseReview{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 2<<20)
	for scanner.Scan() {
		var review CaseReview
		if err := json.Unmarshal(scanner.Bytes(), &review); err != nil {
			return nil, err
		}
		review.ReviewSource = source
		if !validReview(review) {
			return nil, fmt.Errorf("invalid %s review for %s", source, review.CaseID)
		}
		result = append(result, review)
	}
	return result, scanner.Err()
}

func validReview(review CaseReview) bool {
	if review.CaseID == "" || review.Repetition < 1 || review.VariantVersion == "" ||
		(review.Grade != "pass" && review.Grade != "minor" && review.Grade != "unacceptable") ||
		review.Confidence < 0 || review.Confidence > 1 ||
		review.DirectlyUsable != (review.Grade == "pass" || review.Grade == "minor") ||
		review.ReviewerModel == "" || !isSHA256(review.InputSHA256) || !isSHA256(review.OutputSHA256) {
		return false
	}
	for _, issue := range review.Issues {
		if !allowedErrorTypes[issue.ErrorType] || issue.FirstBadStage == "" {
			return false
		}
	}
	return true
}

func reviewMap(values []CaseReview) map[string]CaseReview {
	result := map[string]CaseReview{}
	for _, value := range values {
		result[reviewKey(value.CaseID, value.Repetition, value.VariantVersion)] = value
	}
	return result
}

func reviewKey(caseID string, repetition int, variant string) string {
	return fmt.Sprintf("%s/%s/%d", caseID, variant, repetition)
}

func gradeRank(grade string) int {
	switch grade {
	case "pass":
		return 0
	case "minor":
		return 1
	case "unacceptable":
		return 2
	default:
		return 3
	}
}

func RenderEvaluationMarkdown(result EvaluationResult) string {
	var output strings.Builder
	output.WriteString("# 日报生成方案评测结果\n\n")
	output.WriteString("本报告是开发质量证据，不决定是否发布。\n\n")
	output.WriteString("## Variant Scorecard\n\n")
	output.WriteString("| Variant | Pass | Minor | Unacceptable | Directly Usable | Clean Pass | Success | Avg ms | P95 ms |\n")
	output.WriteString("|---|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, score := range result.Scorecards {
		fmt.Fprintf(&output, "| %s | %d | %d | %d | %.1f%% | %.1f%% | %.1f%% | %.0f | %.0f |\n",
			score.VariantVersion, score.Pass, score.Minor, score.Unacceptable,
			score.DirectlyUsableRate*100, score.CleanPassRate*100, score.SuccessRate*100,
			score.AverageDurationMS, score.P95DurationMS)
	}
	output.WriteString("\n## Comparison\n\n")
	for _, comparison := range result.Comparisons {
		fmt.Fprintf(&output, "- `%s`: %s（win/tie/loss = %d/%d/%d）\n",
			comparison.CandidateVariant, comparison.Conclusion, comparison.Wins, comparison.Ties, comparison.Losses)
	}
	if len(result.Missing) > 0 {
		output.WriteString("\n## Missing evidence\n\n")
		for _, item := range result.Missing {
			fmt.Fprintf(&output, "- %s\n", item)
		}
	}
	return output.String()
}
