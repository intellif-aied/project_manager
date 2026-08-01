package reportcontext

import (
	"encoding/json"
	"testing"

	"github.com/aidashboard/api/internal/reportsource"
)

func TestProjectDigestV2KeepsObservationsOutsideReportPeriod(t *testing.T) {
	var digest map[string]any
	if err := json.Unmarshal(validFrozenDigestV2(), &digest); err != nil {
		t.Fatal(err)
	}
	period := digest["report_period_summary"].(map[string]any)
	period["start_date"] = "2026-08-01"
	period["end_date"] = "2026-08-01"
	raw, err := json.Marshal(digest)
	if err != nil {
		t.Fatal(err)
	}

	projected, err := projectPayload(Payload{
		Run: Run{ReportType: ReportTypePersonalDaily},
		Sessions: []SessionSource{{
			SelectionID: "selection-explicit",
			Mode:        reportsource.ReadModeDigestV2,
			Digest:      raw,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	evidence := projected.WorkEvidence
	if evidence == nil || evidence.Period.StartDate != "2026-08-01" ||
		evidence.Period.EndDate != "2026-08-01" || len(evidence.Facts) == 0 {
		t.Fatalf("invalid projected evidence: %+v", evidence)
	}
	for _, fact := range evidence.Facts {
		if len(fact.Observations) == 0 {
			t.Fatalf("fact lost observations: %+v", fact)
		}
		for _, observation := range fact.Observations {
			if observation.Date != "2026-07-23" {
				t.Fatalf("out-of-period observation was changed or removed: %+v", observation)
			}
		}
	}
}
