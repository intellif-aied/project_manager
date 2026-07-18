package tokenrollup

import (
	"sort"
	"strings"
)

const (
	qualityExact    = "exact"
	qualityPending  = "pending"
	qualityConflict = "conflict"
)

type sessionNode struct {
	ID         string
	Ref        string
	AgentType  string
	ParentRef  string
	ForkSource string
}

type resolvedMembership struct {
	RootID         string
	ParentID       string
	Depth          int
	RelationSource string
	Quality        string
}

func resolveMemberships(nodes []sessionNode) map[string]resolvedMembership {
	byID := make(map[string]sessionNode, len(nodes))
	byAgentRef := make(map[string]string, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
		byAgentRef[agentRefKey(node.AgentType, node.Ref)] = node.ID
	}

	resolved := make(map[string]resolvedMembership, len(nodes))
	visiting := make(map[string]int, len(nodes))
	stack := make([]string, 0, len(nodes))
	var resolve func(string) resolvedMembership
	resolve = func(id string) resolvedMembership {
		if result, ok := resolved[id]; ok {
			return result
		}
		if start, ok := visiting[id]; ok {
			for _, cycleID := range stack[start:] {
				resolved[cycleID] = resolvedMembership{
					RootID: cycleID, Quality: qualityConflict,
					RelationSource: "parent_cycle",
				}
			}
			return resolved[id]
		}

		node := byID[id]
		visiting[id] = len(stack)
		stack = append(stack, id)
		defer func() {
			delete(visiting, id)
			stack = stack[:len(stack)-1]
		}()

		parentRef := strings.TrimSpace(node.ParentRef)
		if parentRef == "" {
			result := resolvedMembership{RootID: id, Quality: qualityExact, RelationSource: "self"}
			resolved[id] = result
			return result
		}
		parentID, ok := byAgentRef[agentRefKey(node.AgentType, parentRef)]
		if !ok {
			result := resolvedMembership{RootID: id, Quality: qualityPending, RelationSource: "parent_missing"}
			resolved[id] = result
			return result
		}
		if parentID == id {
			result := resolvedMembership{RootID: id, Quality: qualityConflict, RelationSource: "parent_cycle"}
			resolved[id] = result
			return result
		}

		parent := resolve(parentID)
		if result, ok := resolved[id]; ok {
			return result
		}
		if parent.Quality == qualityConflict {
			result := resolvedMembership{RootID: id, Quality: qualityConflict, RelationSource: "parent_conflict"}
			resolved[id] = result
			return result
		}
		source := strings.TrimSpace(node.ForkSource)
		if source == "" {
			source = "parent_session_ref"
		}
		result := resolvedMembership{
			RootID: parent.RootID, ParentID: parentID, Depth: parent.Depth + 1,
			RelationSource: source, Quality: parent.Quality,
		}
		resolved[id] = result
		return result
	}

	ids := make([]string, 0, len(nodes))
	for _, node := range nodes {
		ids = append(ids, node.ID)
	}
	sort.Strings(ids)
	for _, id := range ids {
		resolve(id)
	}
	return resolved
}

func agentRefKey(agentType, ref string) string {
	return strings.TrimSpace(agentType) + "\x00" + strings.TrimSpace(ref)
}
