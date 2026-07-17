package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestGroupSessionSelectionsCollapsesCodexChildren(t *testing.T) {
	root := testGroupedSession("root", "", 1)
	sessions := []*SessionInfo{root}
	for index := 0; index < 40; index++ {
		sessions = append(sessions, testGroupedSession(fmt.Sprintf("child-%02d", index), "root", index+2))
	}

	groups := groupSessionSelections(sessions, true, time.Now())
	if len(groups) != 1 || groups[0] != root || len(root.SelectionChildren) != 40 {
		t.Fatalf("groups=%d root=%+v children=%d", len(groups), groups[0], len(root.SelectionChildren))
	}
	if got := sessionSelectionLastActiveAt(root); !got.Equal(testGroupTime(41)) {
		t.Fatalf("last activity=%s", got)
	}
}

func TestGroupSessionSelectionsUsesTopLevelRootForNestedChildren(t *testing.T) {
	root := testGroupedSession("root", "", 1)
	child := testGroupedSession("child", "root", 2)
	grandchild := testGroupedSession("grandchild", "child", 3)

	groups := groupSessionSelections([]*SessionInfo{grandchild, child, root}, true, time.Now())
	if len(groups) != 1 || groups[0] != root || len(root.SelectionChildren) != 2 {
		t.Fatalf("groups=%+v children=%+v", groups, root.SelectionChildren)
	}
}

func TestGroupSessionSelectionsKeepsRecentChildWhenRootIsOld(t *testing.T) {
	now := time.Now()
	root := testGroupedSession("root", "", 1)
	root.EndedAt = now.Add(-7 * 24 * time.Hour)
	child := testGroupedSession("child", "root", 2)
	child.EndedAt = now.Add(-time.Hour)

	groups := groupSessionSelections([]*SessionInfo{root, child}, false, now)
	if len(groups) != 1 || groups[0] != root {
		t.Fatalf("groups=%+v", groups)
	}
}

func TestGroupSessionSelectionsCreatesMissingRootGroup(t *testing.T) {
	child := testGroupedSession("child", "missing-parent", 2)
	groups := groupSessionSelections([]*SessionInfo{child}, true, time.Now())
	if len(groups) != 1 || !groups[0].SelectionMissingRoot ||
		groups[0].SessionRef != "missing-parent" || len(groups[0].SelectionChildren) != 1 {
		t.Fatalf("groups=%+v", groups)
	}
}

func TestCollectSessionsWithFilesExpandsCodexSelectionGroupOnce(t *testing.T) {
	root := testGroupedSession("root", "", 1)
	child := testGroupedSession("child", "root", 2)
	root.SelectionChildren = []*SessionInfo{child, child}

	items := collectSessionsWithFiles(root)
	if len(items) != 2 || items[0].info.SessionRef != "root" || items[1].info.SessionRef != "child" {
		t.Fatalf("items=%+v", items)
	}
}

func TestGroupSessionSelectionsHandlesParentCycle(t *testing.T) {
	first := testGroupedSession("first", "second", 1)
	second := testGroupedSession("second", "first", 2)

	groups := groupSessionSelections([]*SessionInfo{first, second}, true, time.Now())
	if len(groups) != 2 {
		t.Fatalf("groups=%+v", groups)
	}
	for _, group := range groups {
		if group.SelectionIssue != "parent_cycle" {
			t.Fatalf("group=%+v", group)
		}
	}
}

func TestSessionSelectionSearchIncludesChildAgentPath(t *testing.T) {
	root := testGroupedSession("root", "", 1)
	child := testGroupedSession("child", "root", 2)
	child.ForkAgentPath = "/root/final_architecture_audit"
	root.SelectionChildren = []*SessionInfo{child}

	if !strings.Contains(sessionSelectionSearchText(root), "final_architecture_audit") {
		t.Fatalf("search text=%q", sessionSelectionSearchText(root))
	}
}

func TestSessionSelectionProjectFilterIncludesChildDirectory(t *testing.T) {
	root := testGroupedSession("root", "", 1)
	child := testGroupedSession("child", "root", 2)
	child.ProjectDir = "frontend-starter"
	root.SelectionChildren = []*SessionInfo{child}

	if !sessionSelectionMatchesProject(root, "frontend-starter") {
		t.Fatal("expected child project directory to match the group")
	}
}

func TestGroupSessionSelectionsKeepsRootRequirementAheadOfChildInternals(t *testing.T) {
	root := testGroupedSession("root", "", 1)
	root.RecentSummary = "root latest"
	root.RecentSummaryAt = testGroupTime(2)
	child := testGroupedSession("child", "root", 3)
	child.RecentSummary = "child latest"
	child.RecentSummaryAt = testGroupTime(4)

	groups := groupSessionSelections([]*SessionInfo{root, child}, true, time.Now())
	if len(groups) != 1 || groups[0].Summary != "root" || groups[0].RecentSummary != "root latest" {
		t.Fatalf("groups=%+v", groups)
	}
}

func TestGroupSessionSelectionsUsesChildSummaryWhenRootIsMissing(t *testing.T) {
	first := testGroupedSession("child-1", "missing", 1)
	first.RecentSummary = "first child request"
	first.RecentSummaryAt = testGroupTime(2)
	latest := testGroupedSession("child-2", "missing", 3)
	latest.RecentSummary = "latest child request"
	latest.RecentSummaryAt = testGroupTime(4)

	groups := groupSessionSelections([]*SessionInfo{first, latest}, true, time.Now())
	if len(groups) != 1 || groups[0].Summary != "child-1" || groups[0].RecentSummary != "latest child request" {
		t.Fatalf("groups=%+v", groups)
	}
}

func testGroupedSession(ref, parent string, minute int) *SessionInfo {
	return &SessionInfo{
		SessionRef: ref, ParentSessionRef: parent, AgentType: "codex",
		FilePath: "/tmp/" + ref + ".jsonl", Cwd: "/workspace/project",
		Summary: ref, EndedAt: testGroupTime(minute),
	}
}

func testGroupTime(minute int) time.Time {
	return time.Date(2026, 7, 16, 10, minute, 0, 0, time.UTC)
}
