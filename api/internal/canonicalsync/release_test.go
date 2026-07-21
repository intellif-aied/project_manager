package canonicalsync

import "testing"

func TestReleasedAdapterPolicyCannotBeClientPromoted(t *testing.T) {
	request := PrepareRequest{
		ClientVersion: "test",
		Sessions: []PrepareSession{{
			SessionRef: "s", AgentType: "opencode",
			Sources: []PrepareSource{{
				SourceRole: "main", SourceKey: "opencode:s:main", LocalSize: 0,
				PrefixCheckpointAlgorithmVersion: "sha256-prefix-v1", SourceFormat: "aida_event_v1",
				IngestionMetadata: IngestionMetadata{AdapterVersion: "opencode-v1", UsageCapability: "unavailable"},
			}},
		}},
	}
	policy := ReleasePolicy{"opencode": {
		ClientVersion: "test", AdapterVersion: "opencode-v1", MaximumUsageCapability: "unavailable",
	}}
	if err := ValidateReleasedPrepare(request, nil); err == nil {
		t.Fatal("canonical adapters must be disabled by default")
	}
	if err := ValidateReleasedPrepare(request, ReleasePolicy{"opencode": {}}); err == nil {
		t.Fatal("incomplete release policy must not enable an adapter")
	}
	if err := ValidateReleasedPrepare(request, policy); err != nil {
		t.Fatal(err)
	}
	request.Sessions[0].Sources[0].IngestionMetadata.UsageCapability = "exact"
	if err := ValidateReleasedPrepare(request, policy); err == nil {
		t.Fatal("client must not self-promote exact usage")
	}
	request.Sessions[0].Sources[0].IngestionMetadata.UsageCapability = "unavailable"
	request.ClientVersion = "other"
	if err := ValidateReleasedPrepare(request, policy); err == nil {
		t.Fatal("unreleased Aida client version must be rejected")
	}
	request.ClientVersion = "test"
	request.Sessions[0].Sources[0].IngestionMetadata.AdapterVersion = "other"
	if err := ValidateReleasedPrepare(request, policy); err == nil {
		t.Fatal("unreleased adapter version must be rejected")
	}
	request.Sessions[0].AgentType = "workbuddy"
	request.Sessions[0].Sources[0].IngestionMetadata.AdapterVersion = "workbuddy-v1"
	if err := ValidateReleasedPrepare(request, policy); err == nil {
		t.Fatal("WorkBuddy must remain unreleased")
	}
}

func TestReportOnlyReleasePolicyEnablesOnlyApprovedAdapters(t *testing.T) {
	policy := ReportOnlyReleasePolicy(" 0.1.17 ")
	for _, clientType := range []string{"opencode", "kimi_code", "openclaw"} {
		release, ok := policy[clientType]
		if !ok || release.ClientVersion != "0.1.17" || release.MaximumUsageCapability != "unavailable" {
			t.Fatalf("release[%s]=%+v ok=%v", clientType, release, ok)
		}
	}
	if _, ok := policy["workbuddy"]; ok {
		t.Fatal("WorkBuddy must remain detected-only")
	}
	if policy := ReportOnlyReleasePolicy(" "); len(policy) != 0 {
		t.Fatalf("empty client version must fail closed: %+v", policy)
	}
}
