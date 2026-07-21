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
	if err := ValidateReleasedPrepare(request); err != nil {
		t.Fatal(err)
	}
	request.Sessions[0].Sources[0].IngestionMetadata.UsageCapability = "exact"
	if err := ValidateReleasedPrepare(request); err == nil {
		t.Fatal("client must not self-promote exact usage")
	}
	request.Sessions[0].AgentType = "workbuddy"
	request.Sessions[0].Sources[0].IngestionMetadata.UsageCapability = "unavailable"
	if err := ValidateReleasedPrepare(request); err == nil {
		t.Fatal("WorkBuddy must remain unreleased")
	}
}
