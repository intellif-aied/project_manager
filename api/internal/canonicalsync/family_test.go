package canonicalsync

import "testing"

func TestValidateFamilyRequiresClosedAcyclicParents(t *testing.T) {
	if err := validateFamily([]PrepareSession{{SessionRef: "child", AgentType: "opencode", ParentSessionRef: "missing"}}); err == nil {
		t.Fatal("expected missing same-family parent to be rejected")
	}
	if err := validateFamily([]PrepareSession{
		{SessionRef: "a", AgentType: "opencode", ParentSessionRef: "b"},
		{SessionRef: "b", AgentType: "opencode", ParentSessionRef: "a"},
	}); err == nil {
		t.Fatal("expected family cycle to be rejected")
	}
}
