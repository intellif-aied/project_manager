package main

import (
	"sort"
	"strings"
	"time"
)

const defaultSessionActivityWindow = 48 * time.Hour

func groupSessionSelections(sessions []*SessionInfo, showAll bool, now time.Time) []*SessionInfo {
	codexByRef := make(map[string]*SessionInfo)
	result := make([]*SessionInfo, 0, len(sessions))
	for _, session := range sessions {
		if session == nil {
			continue
		}
		resetSessionSelection(session)
		if normalizedAgentType(session.AgentType) != "codex" || session.SessionRef == "" {
			result = append(result, session)
			continue
		}
		codexByRef[session.SessionRef] = session
	}

	groupMembers := make(map[string][]*SessionInfo)
	groupIssues := make(map[string]string)
	for _, session := range codexByRef {
		rootRef, issue := resolveCodexSelectionRoot(session, codexByRef)
		groupMembers[rootRef] = append(groupMembers[rootRef], session)
		if issue != "" {
			groupIssues[rootRef] = issue
		}
	}

	for rootRef, members := range groupMembers {
		root := codexByRef[rootRef]
		if root == nil {
			root = syntheticMissingCodexRoot(rootRef, members)
		}
		for _, member := range members {
			if member != root {
				root.SelectionChildren = append(root.SelectionChildren, member)
			}
		}
		sort.Slice(root.SelectionChildren, func(i, j int) bool {
			return root.SelectionChildren[i].LastActiveAt().After(root.SelectionChildren[j].LastActiveAt())
		})
		root.SelectionActiveAt = latestSessionActivity(members)
		root.SelectionIssue = groupIssues[rootRef]
		if showAll || now.Sub(root.SelectionActiveAt) <= defaultSessionActivityWindow {
			result = append(result, root)
		}
	}

	sort.SliceStable(result, func(i, j int) bool {
		return sessionSelectionLastActiveAt(result[i]).After(sessionSelectionLastActiveAt(result[j]))
	})
	return result
}

func resolveCodexSelectionRoot(session *SessionInfo, sessions map[string]*SessionInfo) (string, string) {
	visited := map[string]bool{}
	current := session
	for current != nil && strings.TrimSpace(current.ParentSessionRef) != "" {
		if visited[current.SessionRef] {
			return session.SessionRef, "parent_cycle"
		}
		visited[current.SessionRef] = true
		parentRef := strings.TrimSpace(current.ParentSessionRef)
		if visited[parentRef] {
			return session.SessionRef, "parent_cycle"
		}
		parent := sessions[parentRef]
		if parent == nil {
			return parentRef, "missing_root"
		}
		current = parent
	}
	if current == nil || current.SessionRef == "" {
		return session.SessionRef, "invalid_parent"
	}
	return current.SessionRef, ""
}

func syntheticMissingCodexRoot(rootRef string, members []*SessionInfo) *SessionInfo {
	root := &SessionInfo{
		SessionRef: rootRef, AgentType: "codex", Summary: "父会话文件不可用",
		SummaryStatus: "missing_root", SelectionMissingRoot: true, SelectionIssue: "missing_root",
	}
	if newest := newestSession(members); newest != nil {
		root.Cwd = newest.Cwd
		root.ProjectDir = newest.ProjectDir
		root.Model = newest.Model
		root.Models = append([]string(nil), newest.Models...)
	}
	return root
}

func resetSessionSelection(session *SessionInfo) {
	session.SelectionChildren = nil
	session.SelectionActiveAt = time.Time{}
	session.SelectionMissingRoot = false
	session.SelectionIssue = ""
}

func newestSession(sessions []*SessionInfo) *SessionInfo {
	var newest *SessionInfo
	for _, session := range sessions {
		if newest == nil || session.LastActiveAt().After(newest.LastActiveAt()) {
			newest = session
		}
	}
	return newest
}

func latestSessionActivity(sessions []*SessionInfo) time.Time {
	if newest := newestSession(sessions); newest != nil {
		return newest.LastActiveAt()
	}
	return time.Time{}
}

func sessionSelectionLastActiveAt(session *SessionInfo) time.Time {
	if session == nil {
		return time.Time{}
	}
	if !session.SelectionActiveAt.IsZero() {
		return session.SelectionActiveAt
	}
	return session.LastActiveAt()
}

func sessionSelectionChildCount(session *SessionInfo) int {
	if session == nil {
		return 0
	}
	return len(session.SubFiles) + len(session.SelectionChildren)
}

func sessionSelectionMemberRefs(session *SessionInfo) []string {
	if session == nil || len(session.SelectionChildren) == 0 {
		return nil
	}
	refs := make([]string, 0, len(session.SelectionChildren))
	for _, child := range session.SelectionChildren {
		if child != nil && child.SessionRef != "" {
			refs = append(refs, child.SessionRef)
		}
	}
	return refs
}

func sessionSelectionSearchText(session *SessionInfo) string {
	if session == nil {
		return ""
	}
	values := []string{
		session.SessionRef, session.AgentType, session.Summary,
		session.ProjectDir, session.Cwd, session.Model, session.ForkAgentPath,
	}
	for _, child := range session.SelectionChildren {
		if child == nil {
			continue
		}
		values = append(values,
			child.SessionRef, child.Summary, child.ProjectDir, child.Cwd,
			child.Model, child.ForkAgentPath,
		)
	}
	return strings.Join(values, "\n")
}

func sessionSelectionMatchesProject(session *SessionInfo, projectFilter string) bool {
	if session == nil {
		return false
	}
	projectFilter = strings.TrimSpace(projectFilter)
	if projectFilter == "" {
		return true
	}
	if strings.Contains(session.ProjectDir, projectFilter) || strings.Contains(session.Cwd, projectFilter) {
		return true
	}
	for _, child := range session.SelectionChildren {
		if child != nil &&
			(strings.Contains(child.ProjectDir, projectFilter) || strings.Contains(child.Cwd, projectFilter)) {
			return true
		}
	}
	return false
}
