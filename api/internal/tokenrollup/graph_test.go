package tokenrollup

import "testing"

func TestResolveMembershipsNestedFamily(t *testing.T) {
	nodes := []sessionNode{
		{ID: "root", Ref: "root-ref", AgentType: "codex"},
		{ID: "child", Ref: "child-ref", AgentType: "codex", ParentRef: "root-ref", ForkSource: "thread_spawn"},
		{ID: "grandchild", Ref: "grandchild-ref", AgentType: "codex", ParentRef: "child-ref"},
	}
	resolved := resolveMemberships(nodes)
	assertMembership(t, resolved["root"], "root", "", 0, qualityExact)
	assertMembership(t, resolved["child"], "root", "root", 1, qualityExact)
	assertMembership(t, resolved["grandchild"], "root", "child", 2, qualityExact)
}

func TestResolveMembershipsMissingParentPropagatesPending(t *testing.T) {
	nodes := []sessionNode{
		{ID: "pending-root", Ref: "pending-root-ref", AgentType: "codex", ParentRef: "not-uploaded"},
		{ID: "child", Ref: "child-ref", AgentType: "codex", ParentRef: "pending-root-ref"},
	}
	resolved := resolveMemberships(nodes)
	assertMembership(t, resolved["pending-root"], "pending-root", "", 0, qualityPending)
	assertMembership(t, resolved["child"], "pending-root", "pending-root", 1, qualityPending)
}

func TestResolveMembershipsCycleDoesNotMerge(t *testing.T) {
	nodes := []sessionNode{
		{ID: "a", Ref: "a-ref", AgentType: "codex", ParentRef: "b-ref"},
		{ID: "b", Ref: "b-ref", AgentType: "codex", ParentRef: "a-ref"},
		{ID: "descendant", Ref: "descendant-ref", AgentType: "codex", ParentRef: "a-ref"},
	}
	resolved := resolveMemberships(nodes)
	assertMembership(t, resolved["a"], "a", "", 0, qualityConflict)
	assertMembership(t, resolved["b"], "b", "", 0, qualityConflict)
	assertMembership(t, resolved["descendant"], "descendant", "", 0, qualityConflict)
}

func TestAffectedSessionsIncludesOldAndNewFamilies(t *testing.T) {
	resolved := map[string]resolvedMembership{
		"a": {RootID: "a"},
		"b": {RootID: "b"},
		"c": {RootID: "b"},
	}
	active := map[string]activeMembership{
		"a": {FamilyVersionID: "old", RootID: "a"},
		"b": {FamilyVersionID: "old", RootID: "a"},
	}
	oldFamilies := map[string][]string{"old": {"a", "b"}}
	affected := affectedSessions("b", resolved, active, oldFamilies)
	for _, id := range []string{"a", "b", "c"} {
		if _, ok := affected[id]; !ok {
			t.Fatalf("session %s was not included in affected closure: %#v", id, affected)
		}
	}
}

func assertMembership(t *testing.T, got resolvedMembership, root, parent string, depth int, quality string) {
	t.Helper()
	if got.RootID != root || got.ParentID != parent || got.Depth != depth || got.Quality != quality {
		t.Fatalf("membership=%+v want root=%s parent=%s depth=%d quality=%s", got, root, parent, depth, quality)
	}
}
