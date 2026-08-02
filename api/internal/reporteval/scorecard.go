package reporteval

import (
	"bufio"
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
	ReviewSource     string        `json:"review_source,omitempty"`
	ReviewerKind     string        `json:"reviewer_kind"`
	ReviewerModel    string        `json:"reviewer_model,omitempty"`
	ReviewerID       string        `json:"reviewer_id,omitempty"`
	HumanConfirmed   bool          `json:"human_confirmed,omitempty"`
	RubricVersion    string        `json:"rubric_version"`
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
	Pattern            PatternScore   `json:"production_pattern_comparison"`
}

type VariantComparison struct {
	CandidateVariant            string   `json:"candidate_variant"`
	Wins                        int      `json:"wins"`
	Ties                        int      `json:"ties"`
	Losses                      int      `json:"losses"`
	FixedCases                  []string `json:"fixed_cases"`
	RegressedCases              []string `json:"regressed_cases"`
	GoldUnacceptableRegressions []string `json:"gold_unacceptable_regressions"`
	OperationalRegressions      []string `json:"operational_regressions"`
	Conclusion                  string   `json:"conclusion"`
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
	"POOR_READABILITY":    true,
	"PROJECT_FALSE_SPLIT": true, "PROJECT_FALSE_MERGE": true,
	"HISTORY_FACT_LEAK": true, "SUPPORTING_DETAIL_PROMOTED": true,
	"TERM_DISTORTION": true,
}

func AggregateReviews(bundleDir, aiReviewPath, goldReviewPath string) (EvaluationResult, error) {
	verification, err := VerifyBundle(bundleDir)
	if err != nil {
		return EvaluationResult{}, err
	}
	if !verification.Valid {
		return EvaluationResult{}, fmt.Errorf("bundle verification failed with %d errors", len(verification.Errors))
	}
	var manifest BundleManifest
	if err := decodeJSONFile(filepath.Join(bundleDir, "manifest.json"), &manifest); err != nil {
		return EvaluationResult{}, err
	}
	if len(manifest.Variants) < 2 {
		return EvaluationResult{}, fmt.Errorf("bundle has no candidate variant")
	}
	frozen, err := LoadFrozenDataset(filepath.Join(bundleDir, "dataset-manifest.json"))
	if err != nil {
		return EvaluationResult{}, err
	}
	stages, succeededRuns, err := loadActualRunStages(bundleDir, manifest.Runs)
	if err != nil {
		return EvaluationResult{}, err
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
	evidenceRefs := reviewEvidenceReferences(frozen)
	if err := validateReviewsAgainstBundle(aiReviews, frozen.Manifest.RubricVersion, succeededRuns, stages, evidenceRefs); err != nil {
		return EvaluationResult{}, fmt.Errorf("AI reviews: %w", err)
	}
	if err := validateReviewsAgainstBundle(goldReviews, frozen.Manifest.RubricVersion, succeededRuns, stages, evidenceRefs); err != nil {
		return EvaluationResult{}, fmt.Errorf("Gold reviews: %w", err)
	}
	result := EvaluationResult{
		SchemaVersion: "daily-report-evaluation-result/v2", BaselineVariant: manifest.Variants[0].VariantVersion,
		AIReviews: aiReviews, GoldReviews: goldReviews, Missing: []string{},
	}
	aiByKey := reviewMap(aiReviews)
	goldByKey := reviewMap(goldReviews)
	effective := map[string]CaseReview{}
	for key := range succeededRuns {
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
		result.Scorecards = append(result.Scorecards, buildScorecard(
			bundleDir, manifest.Runs, variant.VariantVersion, effective, frozen.Pattern,
		))
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
	for index := 1; index < len(manifest.Variants); index++ {
		candidate := manifest.Variants[index].VariantVersion
		comparison := compareVariant(manifest.Runs, result.BaselineVariant, candidate, effective)
		comparison.OperationalRegressions = operationalRegressions(manifest.Runs, result.BaselineVariant, candidate)
		regressionsNeedingGold := append([]string(nil), comparison.RegressedCases...)
		aiComparison := compareVariant(manifest.Runs, result.BaselineVariant, candidate, aiByKey)
		regressionsNeedingGold = uniqueStrings(append(regressionsNeedingGold, aiComparison.RegressedCases...))
		for _, caseRepetition := range regressionsNeedingGold {
			caseID, repetition, parseErr := parseCaseRepetition(caseRepetition)
			if parseErr != nil {
				return EvaluationResult{}, parseErr
			}
			baselineKey := reviewKey(caseID, repetition, result.BaselineVariant)
			candidateKey := reviewKey(caseID, repetition, candidate)
			if !succeededRuns[baselineKey] || !succeededRuns[candidateKey] {
				continue
			}
			baselineGold, baselineOK := goldByKey[baselineKey]
			candidateGold, candidateOK := goldByKey[candidateKey]
			if !baselineOK {
				result.Missing = append(result.Missing, "missing_gold_regression:"+baselineKey)
			}
			if !candidateOK {
				result.Missing = append(result.Missing, "missing_gold_regression:"+candidateKey)
			}
			if baselineOK && candidateOK && baselineGold.Grade != "unacceptable" && candidateGold.Grade == "unacceptable" {
				comparison.GoldUnacceptableRegressions = append(comparison.GoldUnacceptableRegressions, caseRepetition)
			}
		}
		sort.Strings(comparison.GoldUnacceptableRegressions)
		result.Comparisons = append(result.Comparisons, comparison)
	}
	result.Missing = uniqueStrings(result.Missing)
	sort.Strings(result.Missing)
	baselineScores := result.Scorecards[0]
	for index := range result.Comparisons {
		candidateScores := result.Scorecards[index+1]
		comparison := &result.Comparisons[index]
		comparison.Conclusion = concludeComparison(result.Missing, baselineScores, candidateScores, *comparison)
	}
	return result, nil
}

func concludeComparison(
	missing []string,
	baselineScores VariantScorecard,
	candidateScores VariantScorecard,
	comparison VariantComparison,
) string {
	switch {
	case len(missing) > 0:
		return "evidence_insufficient"
	case len(comparison.GoldUnacceptableRegressions) > 0 || len(comparison.OperationalRegressions) > 0:
		return "improvement_not_supported"
	case candidateScores.DirectlyUsableRate > baselineScores.DirectlyUsableRate ||
		candidateScores.DirectlyUsableRate == baselineScores.DirectlyUsableRate &&
			candidateScores.CleanPassRate > baselineScores.CleanPassRate:
		return "improvement_supported"
	default:
		return "improvement_not_supported"
	}
}

func buildScorecard(
	bundleDir string,
	runs []RunReceipt,
	variant string,
	reviews map[string]CaseReview,
	patternBaseline PatternStatistics,
) VariantScorecard {
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
	score.Pattern = buildPatternScore(bundleDir, runs, variant, patternBaseline)
	return score
}

func compareVariant(runs []RunReceipt, baseline, candidate string, reviews map[string]CaseReview) VariantComparison {
	result := VariantComparison{
		CandidateVariant: candidate, FixedCases: []string{}, RegressedCases: []string{},
		GoldUnacceptableRegressions: []string{}, OperationalRegressions: []string{},
	}
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

func operationalRegressions(runs []RunReceipt, baseline, candidate string) []string {
	type statuses struct{ baseline, candidate string }
	values := map[string]statuses{}
	for _, run := range runs {
		if run.VariantVersion != baseline && run.VariantVersion != candidate {
			continue
		}
		key := fmt.Sprintf("%s/%d", run.CaseID, run.Repetition)
		value := values[key]
		if run.VariantVersion == baseline {
			value.baseline = run.Status
		} else {
			value.candidate = run.Status
		}
		values[key] = value
	}
	result := []string{}
	for key, value := range values {
		if value.baseline == "succeeded" && value.candidate != "succeeded" {
			result = append(result, key)
		}
	}
	sort.Strings(result)
	return result
}

func loadActualRunStages(bundleDir string, runs []RunReceipt) (map[string]map[string]bool, map[string]bool, error) {
	stages := map[string]map[string]bool{}
	succeeded := map[string]bool{}
	for _, run := range runs {
		if run.Status != "succeeded" {
			continue
		}
		key := reviewKey(run.CaseID, run.Repetition, run.VariantVersion)
		var manifest struct {
			Stages []string `json:"stages"`
		}
		if err := decodeJSONFile(filepath.Join(bundleDir, "cases", run.CaseID, "runs", run.RunID, "variant-manifest.json"), &manifest); err != nil {
			return nil, nil, err
		}
		stages[key] = map[string]bool{}
		for _, stage := range manifest.Stages {
			stage = strings.TrimSpace(stage)
			if stage != "" {
				stages[key][stage] = true
			}
		}
		succeeded[key] = true
	}
	return stages, succeeded, nil
}

func validateReviewsAgainstBundle(
	reviews []CaseReview,
	rubricVersion string,
	succeededRuns map[string]bool,
	stages map[string]map[string]bool,
	evidenceRefs map[string]map[string]bool,
) error {
	for _, review := range reviews {
		key := reviewKey(review.CaseID, review.Repetition, review.VariantVersion)
		if !succeededRuns[key] {
			return fmt.Errorf("review references unknown or incomplete run %s", key)
		}
		if review.RubricVersion != rubricVersion {
			return fmt.Errorf("review %s uses rubric %s; expected %s", key, review.RubricVersion, rubricVersion)
		}
		for _, issue := range review.Issues {
			if issue.FirstBadStage != "unresolved" && !stages[key][issue.FirstBadStage] {
				return fmt.Errorf("review %s references absent first_bad_stage %s", key, issue.FirstBadStage)
			}
			for _, ref := range issue.EvidenceRefs {
				if !evidenceRefs[review.CaseID][ref] {
					return fmt.Errorf("review %s references unknown evidence %s", key, ref)
				}
			}
		}
	}
	return nil
}

func reviewEvidenceReferences(dataset FrozenDataset) map[string]map[string]bool {
	result := make(map[string]map[string]bool, len(dataset.Manifest.Cases))
	for _, item := range dataset.Manifest.Cases {
		refs := map[string]bool{}
		for _, sourceItem := range dataset.Sources[item.CaseID].Items {
			for _, event := range sourceItem.Events {
				refs[event.EvidenceRef] = true
			}
		}
		for _, evidence := range item.EvidenceBaseline.Items {
			refs[evidence.EvidenceID] = true
		}
		result[item.CaseID] = refs
	}
	return result
}

func readReviewJSONL(path, source string) ([]CaseReview, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	result := []CaseReview{}
	seen := map[string]bool{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 2<<20)
	for scanner.Scan() {
		var review CaseReview
		if err := decodeStrictJSON(scanner.Bytes(), &review); err != nil {
			return nil, err
		}
		review.ReviewSource = source
		if !validReview(review, source) {
			return nil, fmt.Errorf("invalid %s review for %s", source, review.CaseID)
		}
		key := reviewKey(review.CaseID, review.Repetition, review.VariantVersion)
		if seen[key] {
			return nil, fmt.Errorf("duplicate %s review for %s", source, key)
		}
		seen[key] = true
		result = append(result, review)
	}
	return result, scanner.Err()
}

func validReview(review CaseReview, source string) bool {
	if review.CaseID == "" || review.Repetition < 1 || review.VariantVersion == "" ||
		(review.Grade != "pass" && review.Grade != "minor" && review.Grade != "unacceptable") ||
		review.Confidence < 0 || review.Confidence > 1 || review.ElapsedMS < 0 ||
		review.DirectlyUsable != (review.Grade == "pass" || review.Grade == "minor") ||
		review.RubricVersion == "" ||
		!isSHA256(review.InputSHA256) || !isSHA256(review.OutputSHA256) {
		return false
	}
	switch source {
	case "ai":
		if review.ReviewerKind != "model" || strings.TrimSpace(review.ReviewerModel) == "" ||
			review.ReviewerID != "" || review.HumanConfirmed ||
			!isSHA256(review.PromptSHA256) || !isSHA256(review.SkillSHA256) {
			return false
		}
	case "gold":
		if review.ReviewerKind != "human" || !safeIdentifierPattern.MatchString(review.ReviewerID) ||
			!review.HumanConfirmed || review.ReviewerModel != "" ||
			review.PromptSHA256 != "" || review.SkillSHA256 != "" {
			return false
		}
	default:
		return false
	}
	if (review.Grade == "pass") != (len(review.Issues) == 0) {
		return false
	}
	hasUnacceptableIssue := false
	for _, issue := range review.Issues {
		if !allowedErrorTypes[issue.ErrorType] || (issue.Severity != "minor" && issue.Severity != "unacceptable") ||
			strings.TrimSpace(issue.FirstBadStage) == "" || strings.TrimSpace(issue.Explanation) == "" ||
			len(issue.EvidenceRefs)+len(issue.FinalRefs) == 0 {
			return false
		}
		if issue.Severity == "unacceptable" {
			hasUnacceptableIssue = true
		}
	}
	if review.Grade == "minor" && hasUnacceptableIssue || review.Grade == "unacceptable" && !hasUnacceptableIssue {
		return false
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

func parseCaseRepetition(value string) (string, int, error) {
	separator := strings.LastIndex(value, "/")
	if separator < 1 {
		return "", 0, fmt.Errorf("invalid case repetition %s", value)
	}
	repetition, err := strconv.Atoi(value[separator+1:])
	if err != nil || repetition < 1 {
		return "", 0, fmt.Errorf("invalid case repetition %s", value)
	}
	return value[:separator], repetition, nil
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
	output.WriteString("| Variant | Pass | Minor | Unacceptable | Directly Usable | Clean Pass | Success | Avg ms | P95 ms | Chars P50/P90 | Items P50/P90 |\n")
	output.WriteString("|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, score := range result.Scorecards {
		fmt.Fprintf(&output, "| %s | %d | %d | %d | %.1f%% | %.1f%% | %.1f%% | %.0f | %.0f | %d/%d | %d/%d |\n",
			score.VariantVersion, score.Pass, score.Minor, score.Unacceptable,
			score.DirectlyUsableRate*100, score.CleanPassRate*100, score.SuccessRate*100,
			score.AverageDurationMS, score.P95DurationMS,
			score.Pattern.CharacterCount.GeneratedP50, score.Pattern.CharacterCount.GeneratedP90,
			score.Pattern.OrderedItemCount.GeneratedP50, score.Pattern.OrderedItemCount.GeneratedP90)
	}
	output.WriteString("\nProduction Pattern 仅描述形态分布，不参与事实等级判定。\n")
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
