package main

import (
	"bufio"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Codex CLI persists rollouts at ~/.codex/sessions/YYYY/MM/DD/rollout-<ts>-<sessionId>.jsonl.
// Each line is {timestamp,type,payload}. The relevant inner types are:
//   - session_meta: {id,timestamp,cwd,originator,cli_version,...}
//   - turn_context: {turn_id,cwd,model,...}
//   - event_msg/task_started: {turn_id,started_at}
//   - event_msg/task_complete: {turn_id,completed_at,duration_ms,last_agent_message}
//   - event_msg/token_count: {info:{total_token_usage:{input_tokens,cached_input_tokens,output_tokens,reasoning_output_tokens,total_tokens}}}
//   - event_msg/user_message: {message}  (used as a summary fallback)
//   - response_item/function_call|custom_tool_call: {name,arguments,...}

type codexLine struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type codexSessionMeta struct {
	ID             string          `json:"id"`
	Timestamp      string          `json:"timestamp"`
	Cwd            string          `json:"cwd"`
	ForkedFromID   string          `json:"forked_from_id"`
	ParentThreadID string          `json:"parent_thread_id"`
	SessionID      string          `json:"session_id"`
	ThreadSource   json.RawMessage `json:"thread_source"`
	Source         json.RawMessage `json:"source"`
}

type codexTurnContext struct {
	Model string `json:"model"`
	Cwd   string `json:"cwd"`
}

type codexEvent struct {
	Type    string          `json:"type"`
	Message string          `json:"message"`
	Info    json.RawMessage `json:"info"`
}

type codexTokenInfo struct {
	Total struct {
		InputTokens           int64 `json:"input_tokens"`
		CachedInputTokens     int64 `json:"cached_input_tokens"`
		OutputTokens          int64 `json:"output_tokens"`
		ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
		TotalTokens           int64 `json:"total_tokens"`
	} `json:"total_token_usage"`
}

type codexResponseItem struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

// scanCodexSessions walks ~/.codex/sessions and returns parsed SessionInfo entries
// with AgentType="codex". Honors the same 48h cutoff as scanSessions when showAll=false.
func scanCodexSessions(codexDir string, showAll bool) []*SessionInfo {
	var sessions []*SessionInfo
	if _, err := os.Stat(codexDir); err != nil {
		return sessions
	}

	filepath.WalkDir(codexDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		if !strings.HasPrefix(d.Name(), "rollout-") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if !showAll && time.Since(info.ModTime()) > 48*time.Hour {
			return nil
		}
		s := parseCodexJSONL(path)
		if s == nil || s.SessionRef == "" {
			return nil
		}
		s.FilePath = path
		s.FileModifiedAt = info.ModTime()
		sessions = append(sessions, s)
		return nil
	})

	// newest first
	sortSessionsNewestFirst(sessions)
	return sessions
}

func parseCodexJSONL(path string) *SessionInfo {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	s := &SessionInfo{
		AgentType: "codex",
		ToolCalls: make(map[string]int),
	}

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	var lastTokens codexTokenInfo
	var sawTokens bool
	var prevTokens codexTokenInfo
	var hasPrevTokens bool
	var tokenCountersReset bool
	var sawRootMeta bool
	var forkBaselineReady bool
	var forkBaselineMissing bool
	var firstSummary string
	activitySlices := map[string]*ActivitySlice{}

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var cl codexLine
		if err := json.Unmarshal(line, &cl); err != nil {
			continue
		}
		s.NumLines++

		ts, _ := time.Parse(time.RFC3339Nano, cl.Timestamp)
		currentSlice := ensureActivitySlice(activitySlices, ts, "codex")
		if !ts.IsZero() {
			if s.StartedAt.IsZero() {
				s.StartedAt = ts
			}
			s.EndedAt = ts
		}

		switch cl.Type {
		case "session_meta":
			var meta codexSessionMeta
			if err := json.Unmarshal(cl.Payload, &meta); err == nil && !sawRootMeta {
				sawRootMeta = true
				s.SessionRef = meta.ID
				s.Cwd = meta.Cwd
				if meta.Cwd != "" {
					s.ProjectDir = filepath.Base(meta.Cwd)
				}
				if meta.Timestamp != "" {
					if mt, err := time.Parse(time.RFC3339Nano, meta.Timestamp); err == nil {
						s.StartedAt = mt
						s.ForkedAt = mt
					}
				}
				s.ParentSessionRef, s.ForkSource = codexParentMetadata(meta)
				s.ForkAgentPath = nestedJSONString(meta.Source, "subagent", "thread_spawn", "agent_path")
				if s.ParentSessionRef == "" {
					s.ForkedAt = time.Time{}
				}
			}
		case "turn_context":
			var tc codexTurnContext
			if err := json.Unmarshal(cl.Payload, &tc); err == nil {
				if tc.Model != "" {
					s.Model = tc.Model
					s.Models = appendDistinct(s.Models, tc.Model)
					if currentSlice != nil {
						currentSlice.Model = tc.Model
						currentSlice.Models = appendDistinct(currentSlice.Models, tc.Model)
					}
				}
				if tc.Cwd != "" && s.Cwd == "" {
					s.Cwd = tc.Cwd
					s.ProjectDir = filepath.Base(tc.Cwd)
				}
			}
		case "event_msg":
			var ev codexEvent
			if err := json.Unmarshal(cl.Payload, &ev); err == nil {
				switch ev.Type {
				case "token_count":
					if len(ev.Info) > 0 {
						var ti codexTokenInfo
						if err := json.Unmarshal(ev.Info, &ti); err == nil {
							if s.ParentSessionRef != "" && !s.ForkedAt.IsZero() {
								if ts.Before(s.ForkedAt) {
									prevTokens = ti
									hasPrevTokens = true
									forkBaselineReady = true
									continue
								}
								if !forkBaselineReady && !hasPrevTokens {
									prevTokens = ti
									hasPrevTokens = true
									forkBaselineReady = true
									forkBaselineMissing = true
									continue
								}
							}
							if currentSlice != nil {
								if addCodexTokenDelta(currentSlice, ti, prevTokens, hasPrevTokens) {
									tokenCountersReset = true
								}
							}
							prevTokens = ti
							hasPrevTokens = true
							lastTokens = ti
							sawTokens = true
						}
					}
				case "user_message":
					if firstSummary == "" && ev.Message != "" {
						firstSummary = ev.Message
					}
					if currentSlice != nil && ev.Message != "" {
						currentSlice.MessageCount++
						appendSliceText(currentSlice, ev.Message)
					}
				}
			}
		case "response_item":
			var ri codexResponseItem
			if err := json.Unmarshal(cl.Payload, &ri); err == nil {
				if ri.Type == "function_call" || ri.Type == "custom_tool_call" {
					name := ri.Name
					if name == "" {
						name = ri.Type
					}
					s.ToolCalls[name]++
					if currentSlice != nil {
						currentSlice.ToolCalls[name]++
					}
				}
			}
		}
	}

	if sawTokens && s.ParentSessionRef == "" {
		s.InputTok = lastTokens.Total.InputTokens
		s.CacheReadTok = lastTokens.Total.CachedInputTokens
		// Codex doesn't expose a separate cache-creation counter; leave at 0.
		s.OutputTok = lastTokens.Total.OutputTokens + lastTokens.Total.ReasoningOutputTokens
		s.TotalTok = lastTokens.Total.TotalTokens
		if s.TotalTok == 0 {
			s.TotalTok = s.InputTok + s.OutputTok
		}
	}
	if sawTokens {
		if tokenCountersReset {
			for _, slice := range activitySlices {
				slice.InputTokens = 0
				slice.OutputTokens = 0
				slice.CacheCreationTokens = 0
				slice.CacheReadTokens = 0
				slice.TotalTokens = 0
				slice.TokenSliceStrategy = "session_total_last_activity"
				slice.IsEstimated = true
			}
			fallback := ensureActivitySlice(activitySlices, s.EndedAt, "codex")
			if fallback != nil {
				fallback.InputTokens = s.InputTok
				fallback.OutputTokens = s.OutputTok
				fallback.CacheReadTokens = s.CacheReadTok
				fallback.TotalTokens = s.TotalTok
			}
		} else {
			for _, slice := range activitySlices {
				slice.TokenSliceStrategy = "delta"
			}
		}
		if s.ParentSessionRef != "" {
			s.InputTok, s.OutputTok, s.CacheReadTok, s.TotalTok = 0, 0, 0, 0
			for _, slice := range activitySlices {
				s.InputTok += slice.InputTokens
				s.OutputTok += slice.OutputTokens
				s.CacheReadTok += slice.CacheReadTokens
				s.TotalTok += slice.TotalTokens
				if forkBaselineMissing {
					slice.IsEstimated = true
					slice.TokenSliceStrategy = "fork_delta_missing_initial_baseline"
				} else {
					slice.TokenSliceStrategy = "fork_delta"
				}
			}
		}
	}

	if firstSummary != "" {
		s.Summary = truncate(firstSummary, 200)
		s.SummaryStatus = "ok"
		s.SummarySource = "event_msg.user_message"
	} else if scanner.Err() != nil {
		s.SummaryStatus = "parse_error"
	} else {
		s.SummaryStatus = "empty"
	}
	finalizeActivitySlices(s, activitySlices)

	return s
}

func codexParentMetadata(meta codexSessionMeta) (string, string) {
	if parent := nestedJSONString(meta.Source, "subagent", "thread_spawn", "parent_thread_id"); parent != "" {
		return parent, "source.subagent.thread_spawn.parent_thread_id"
	}
	if parent := strings.TrimSpace(meta.ForkedFromID); parent != "" {
		return parent, "forked_from_id"
	}
	if parent := strings.TrimSpace(meta.ParentThreadID); parent != "" {
		return parent, "parent_thread_id"
	}
	if codexThreadSourceIsSubagent(meta.ThreadSource) {
		if parent := strings.TrimSpace(meta.SessionID); parent != "" && parent != strings.TrimSpace(meta.ID) {
			return parent, "thread_source.subagent.session_id"
		}
	}
	return "", ""
}

func nestedJSONString(raw json.RawMessage, path ...string) string {
	if len(raw) == 0 {
		return ""
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	for _, key := range path {
		object, ok := value.(map[string]any)
		if !ok {
			return ""
		}
		value = object[key]
	}
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func codexThreadSourceIsSubagent(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.EqualFold(strings.TrimSpace(text), "subagent")
	}
	var object map[string]any
	if json.Unmarshal(raw, &object) == nil {
		_, ok := object["subagent"]
		return ok
	}
	return false
}

func addCodexTokenDelta(slice *ActivitySlice, current, previous codexTokenInfo, hasPrevious bool) bool {
	if slice == nil {
		return false
	}
	input := current.Total.InputTokens
	cacheRead := current.Total.CachedInputTokens
	output := current.Total.OutputTokens + current.Total.ReasoningOutputTokens
	total := current.Total.TotalTokens
	if hasPrevious {
		input -= previous.Total.InputTokens
		cacheRead -= previous.Total.CachedInputTokens
		output -= previous.Total.OutputTokens + previous.Total.ReasoningOutputTokens
		total -= previous.Total.TotalTokens
	}
	if input < 0 || cacheRead < 0 || output < 0 || total < 0 {
		return true
	}
	slice.InputTokens += input
	slice.CacheReadTokens += cacheRead
	slice.OutputTokens += output
	if total == 0 {
		total = input + cacheRead + output
	}
	slice.TotalTokens += total
	return false
}
