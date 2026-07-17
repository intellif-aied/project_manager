package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCleanInjectedUserTextProviderMatrix(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "claude ide only", raw: `<ide_opened_file>The user opened c:\\work\\main.go</ide_opened_file>`, want: ""},
		{name: "malformed ide tag", raw: "ide_opened_file>The user opened c:\\work\\main.go\n修复上传摘要", want: "修复上传摘要"},
		{name: "multiple codex blocks", raw: `<environment_context><cwd>/work</cwd></environment_context>
<permissions instructions>full access</permissions instructions>
修复 Session 上传后的 Token 重复统计`, want: "修复 Session 上传后的 Token 重复统计"},
		{name: "ide request wrapper", raw: "# Context from my IDE setup:\n\n## Active file: main.go\n\n## My request for Codex:\n优化 Session 列表", want: "优化 Session 列表"},
		{name: "serialized skill", raw: `[{"text":"<skill><name>test</name></skill>"}]`, want: ""},
		{name: "codex skill command", raw: "$grill-with-docs 梳理多客户端支持方案", want: "梳理多客户端支持方案"},
		{name: "claude slash command", raw: "/review 检查上传逻辑", want: "检查上传逻辑"},
		{name: "absolute path", raw: "/home/aied/project 中的上传逻辑有问题", want: "/home/aied/project 中的上传逻辑有问题"},
		{name: "literal tag discussion", raw: "修复 <ide_opened_file> 标签被错误展示的问题", want: "修复 <ide_opened_file> 标签被错误展示的问题"},
		{name: "html request", raw: "请修改 <div> 内的按钮布局", want: "请修改 <div> 内的按钮布局"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := cleanInjectedUserText(test.raw)
			if got != test.want {
				t.Fatalf("cleaned=%q want=%q", got, test.want)
			}
			if cleanInjectedUserText(got) != got {
				t.Fatalf("cleaner is not idempotent: first=%q second=%q", got, cleanInjectedUserText(got))
			}
		})
	}
}

func TestParseClaudeJSONLCleansIDEInjectionAndKeepsLatestRequirement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude-summary.jsonl")
	lines := []string{
		`{"type":"user","sessionId":"claude-summary","timestamp":"2026-07-16T01:00:00Z","message":{"content":[{"type":"text","text":"<ide_opened_file>The user opened c:\\\\work\\\\main.go</ide_opened_file>"}]}}`,
		`{"type":"user","sessionId":"claude-summary","timestamp":"2026-07-16T01:01:00Z","message":{"content":[{"type":"text","text":"修复上传摘要。不要修改上传协议。"}]}}`,
		`{"type":"user","sessionId":"claude-summary","timestamp":"2026-07-16T01:02:00Z","message":{"content":[{"type":"text","text":"可以"}]}}`,
		`{"type":"user","sessionId":"claude-summary","timestamp":"2026-07-16T01:03:00Z","message":{"content":[{"type":"text","text":"重新检查列表展示"}]}}`,
		`{"type":"assistant","sessionId":"claude-summary","timestamp":"2026-07-16T01:04:00Z","message":{"model":"claude-test","content":[{"type":"text","text":"列表检查已经完成。"}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	session := parseJSONL(path)
	if session.Summary != "修复上传摘要。不要修改上传协议。" || session.RecentSummary != "列表检查已经完成。" {
		t.Fatalf("summaries=%q / %q", session.Summary, session.RecentSummary)
	}
}

func TestSessionSummaryCollectorSelectsFirstAndRecentMeaningfulMessages(t *testing.T) {
	collector := sessionSummaryCollector{}
	collector.Add(`<environment_context><cwd>/work</cwd></environment_context>`, time.Unix(1, 0), "event_msg.user_message")
	collector.Add("修复上传逻辑。补充测试。", time.Unix(2, 0), "event_msg.user_message")
	collector.Add("可以", time.Unix(3, 0), "event_msg.user_message")
	collector.Add("可以，开始吧", time.Unix(3, 0), "event_msg.user_message")
	collector.Add("重新上传 1066 数据并复查", time.Unix(4, 0), "event_msg.user_message")
	session := &SessionInfo{}
	collector.Apply(session)

	if session.Summary != "修复上传逻辑。补充测试。" || session.RecentSummary != "重新上传 1066 数据并复查" {
		t.Fatalf("session summaries=%q / %q", session.Summary, session.RecentSummary)
	}
	if session.SummarySource != "event_msg.user_message" || session.RecentSummaryAt != time.Unix(4, 0) {
		t.Fatalf("session diagnostics=%+v", session)
	}
}

func TestSummaryTruncationIsUnicodeSafe(t *testing.T) {
	collector := sessionSummaryCollector{}
	collector.Add(strings.Repeat("摘", 260), time.Now(), "user.message")
	session := &SessionInfo{}
	collector.Apply(session)
	if len([]rune(session.Summary)) != 200 || !strings.HasSuffix(session.Summary, "...") {
		t.Fatalf("summary rune length=%d suffix=%q", len([]rune(session.Summary)), session.Summary[len(session.Summary)-3:])
	}
}

func TestSessionSummaryKeepsListMarkersAndFollowingContent(t *testing.T) {
	collector := sessionSummaryCollector{}
	collector.Add("我们考虑优化用户细节： A. 正确获取用户名；B. 补充登录测试", time.Now(), "event_msg.user_message")
	session := &SessionInfo{}
	collector.Apply(session)
	if session.Summary != "我们考虑优化用户细节： A. 正确获取用户名；B. 补充登录测试" {
		t.Fatalf("summary=%q", session.Summary)
	}
}

func TestSessionSummaryUsesLastUserOrAgentReply(t *testing.T) {
	collector := sessionSummaryCollector{}
	collector.Add("检查上传逻辑", time.Unix(1, 0), "event_msg.user_message")
	collector.AddReply("我正在读取代码。", time.Unix(2, 0), "event_msg.agent_message")
	collector.Add("继续", time.Unix(3, 0), "event_msg.user_message")
	collector.AddReply(`{"outcome":"allow"}`, time.Unix(4, 0), "event_msg.agent_message")
	session := &SessionInfo{}
	collector.Apply(session)
	if session.Summary != "检查上传逻辑" || session.RecentSummary != "继续" {
		t.Fatalf("summaries=%q / %q", session.Summary, session.RecentSummary)
	}

	collector.AddReply("已经完成检查。", time.Unix(5, 0), "event_msg.agent_message")
	collector.Apply(session)
	if session.RecentSummary != "已经完成检查。" {
		t.Fatalf("recent summary=%q", session.RecentSummary)
	}
}
