package reporteval

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareAnonymousReviewUsesReferencedSourceAndPattern(t *testing.T) {
	bundle := writeScorecardBundle(t, "1. 基线", "1. 候选")
	output := filepath.Join(t.TempDir(), "review")
	if err := PrepareAnonymousReview(bundle, output); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"review-input/review-control.json",
		"review-input/production-pattern-statistics.json",
		"review-input/cases/case-001/repetition-1/source-evidence.json",
		"review-input/cases/case-001/repetition-1/evidence-baseline.json",
		"pairing-map.json",
	} {
		if _, err := os.Stat(filepath.Join(output, path)); err != nil {
			t.Fatalf("missing %s: %v", path, err)
		}
	}
}
