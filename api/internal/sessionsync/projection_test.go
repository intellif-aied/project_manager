package sessionsync

import "testing"

func TestCORE016ProjectedReadsCannotPassContentWatermark(t *testing.T) {
	if ok, code := CanReadProjectedRange(100, 101); ok || code != ErrorContentProjectionNotReady {
		t.Fatalf("ok=%t code=%s", ok, code)
	}
	if ok, code := CanReadProjectedRange(100, 100); !ok || code != "" {
		t.Fatalf("ok=%t code=%s", ok, code)
	}
}

func TestCORE024ProjectionActivationRequiresCurrentSourceHighWater(t *testing.T) {
	tests := []struct {
		name    string
		source  int64
		indexed int64
		status  ProjectionActivationStatus
		code    string
	}{
		{name: "caught up", source: 200, indexed: 200, status: ProjectionReady},
		{name: "source advanced", source: 220, indexed: 200, status: ProjectionCatchUp, code: ErrorRevisionHighWaterChanged},
		{name: "invalid projection", source: 200, indexed: 220, status: ProjectionInvalid, code: ErrorContentProjectionNotReady},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecideProjectionActivation(tt.source, tt.indexed)
			if got.Status != tt.status || got.ErrorCode != tt.code {
				t.Fatalf("decision=%+v", got)
			}
		})
	}
}
