package reporteval

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
)

type ReviewControl struct {
	SchemaVersion         string          `json:"schema_version"`
	DatasetVersion        string          `json:"dataset_version"`
	RubricVersion         string          `json:"rubric_version"`
	PatternDatasetVersion string          `json:"pattern_dataset_version"`
	Cases                 []AnonymousCase `json:"cases"`
}

type AnonymousCase struct {
	CaseID     string   `json:"case_id"`
	Repetition int      `json:"repetition"`
	Candidates []string `json:"candidates"`
}

type PairingMap struct {
	SchemaVersion string           `json:"schema_version"`
	Assignments   []PairAssignment `json:"assignments"`
}

type PairAssignment struct {
	CaseID         string `json:"case_id"`
	Alias          string `json:"alias"`
	VariantVersion string `json:"variant_version"`
	RunID          string `json:"run_id"`
	Repetition     int    `json:"repetition"`
}

func PrepareAnonymousReview(bundleDir, outputDir string) error {
	verification, err := VerifyBundle(bundleDir)
	if err != nil {
		return err
	}
	if !verification.Valid {
		return fmt.Errorf("bundle verification failed with %d errors", len(verification.Errors))
	}
	if _, err := os.Stat(outputDir); err == nil {
		return fmt.Errorf("review output already exists")
	} else if !os.IsNotExist(err) {
		return err
	}
	var manifest BundleManifest
	if err := decodeJSONFile(filepath.Join(bundleDir, "manifest.json"), &manifest); err != nil {
		return err
	}
	frozen, err := LoadFrozenDataset(filepath.Join(bundleDir, "dataset-manifest.json"))
	if err != nil {
		return err
	}
	control := ReviewControl{
		SchemaVersion: "daily-report-anonymous-review/v1", DatasetVersion: manifest.DatasetVersion,
		RubricVersion: manifest.RubricVersion, PatternDatasetVersion: frozen.Manifest.PatternBaseline.DatasetVersion,
	}
	pairing := PairingMap{SchemaVersion: "daily-report-pairing-map/v1"}
	byCase := map[string][]RunReceipt{}
	for _, run := range manifest.Runs {
		key := fmt.Sprintf("%s/%06d", run.CaseID, run.Repetition)
		byCase[key] = append(byCase[key], run)
	}
	caseIDs := make([]string, 0, len(byCase))
	for caseID := range byCase {
		caseIDs = append(caseIDs, caseID)
	}
	sort.Strings(caseIDs)
	for _, caseKey := range caseIDs {
		runs := byCase[caseKey]
		caseID := runs[0].CaseID
		repetition := runs[0].Repetition
		if len(runs) != len(manifest.Variants) {
			return fmt.Errorf("case %s repetition %d does not have exactly one run per variant", caseID, repetition)
		}
		if err := shuffleRuns(runs); err != nil {
			return err
		}
		caseOutput := filepath.Join(outputDir, "review-input", "cases", caseID, fmt.Sprintf("repetition-%d", repetition))
		if err := os.MkdirAll(caseOutput, 0o750); err != nil {
			return err
		}
		item, ok := evaluationCaseByID(frozen.Manifest.Cases, caseID)
		if !ok {
			return fmt.Errorf("case %s is missing from frozen dataset", caseID)
		}
		if err := copyFile(
			filepath.Join(bundleDir, filepath.FromSlash(item.SourceEvidence.Path)),
			filepath.Join(caseOutput, "source-evidence.json"),
		); err != nil {
			return err
		}
		if err := copyFile(
			filepath.Join(bundleDir, "cases", caseID, "evidence-baseline.json"),
			filepath.Join(caseOutput, "evidence-baseline.json"),
		); err != nil {
			return err
		}
		anonymous := AnonymousCase{CaseID: caseID, Repetition: repetition}
		for index, run := range runs {
			alias := fmt.Sprintf("candidate-%c", 'A'+index)
			anonymous.Candidates = append(anonymous.Candidates, alias)
			pairing.Assignments = append(pairing.Assignments, PairAssignment{
				CaseID: caseID, Repetition: repetition, Alias: alias, VariantVersion: run.VariantVersion, RunID: run.RunID,
			})
			from := filepath.Join(bundleDir, "cases", caseID, "runs", run.RunID)
			to := filepath.Join(caseOutput, alias)
			if err := os.MkdirAll(to, 0o750); err != nil {
				return err
			}
			for _, name := range []string{"digest.json", "context.json", "brief.json", "generated-draft.md"} {
				if _, err := os.Stat(filepath.Join(from, name)); err == nil {
					if err := copyFile(filepath.Join(from, name), filepath.Join(to, name)); err != nil {
						return err
					}
				}
			}
			if _, err := writeJSON(filepath.Join(to, "run-status.json"), map[string]any{
				"status": run.Status, "failure_stage": run.FailureStage, "error_code": run.ErrorCode,
			}); err != nil {
				return err
			}
		}
		control.Cases = append(control.Cases, anonymous)
	}
	if _, err := writeJSON(filepath.Join(outputDir, "review-input", "review-control.json"), control); err != nil {
		return err
	}
	if err := copyFile(
		filepath.Join(bundleDir, filepath.FromSlash(frozen.Manifest.PatternBaseline.Statistics.Path)),
		filepath.Join(outputDir, "review-input", "production-pattern-statistics.json"),
	); err != nil {
		return err
	}
	_, err = writeJSON(filepath.Join(outputDir, "pairing-map.json"), pairing)
	return err
}

func evaluationCaseByID(cases []EvaluationCase, caseID string) (EvaluationCase, bool) {
	for _, item := range cases {
		if item.CaseID == caseID {
			return item, true
		}
	}
	return EvaluationCase{}, false
}

func shuffleRuns(values []RunReceipt) error {
	for index := len(values) - 1; index > 0; index-- {
		random, err := rand.Int(rand.Reader, big.NewInt(int64(index+1)))
		if err != nil {
			return err
		}
		swap := int(random.Int64())
		values[index], values[swap] = values[swap], values[index]
	}
	return nil
}

func copyFile(from, to string) error {
	payload, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	return os.WriteFile(to, payload, 0o640)
}

func EncodeJSONLine(value any) ([]byte, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return append(payload, '\n'), nil
}
