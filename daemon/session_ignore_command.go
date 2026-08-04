package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func cmdIgnore(args []string) int {
	if len(args) != 1 {
		writeIgnoreUsage(os.Stdout)
		return 2
	}
	config, err := loadSessionIgnoreConfig()
	if err != nil {
		fmt.Println("无法读取忽略配置，请检查 ~/.aida/ignore.json")
		return 1
	}
	switch args[0] {
	case "list":
		writeIgnoreList(os.Stdout, config)
		return 0
	case "session", "folder":
		sessions := discoverIgnoreCandidates()
		if len(sessions) == 0 {
			fmt.Println("没有找到可管理的本地 Session")
			return 0
		}
		var choices []*SessionInfo
		if args[0] == "session" {
			choices = sessions
		} else {
			choices = directoryIgnoreChoices(sessions)
		}
		selected, err := selectIgnoreChoices(choices)
		if err != nil {
			fmt.Println("无法完成忽略规则选择，请重试")
			return 1
		}
		if len(selected) == 0 {
			fmt.Println("未修改忽略规则")
			return 0
		}
		if args[0] == "session" {
			for _, session := range selected {
				config.Sessions = appendIgnoredSession(config.Sessions, ignoredSession{AgentType: normalizedAgentType(session.AgentType), SessionRef: session.SessionRef})
			}
		} else {
			for _, choice := range selected {
				config.Directories = appendIgnoredDirectory(config.Directories, choice.Cwd)
			}
		}
		if err := saveSessionIgnoreConfig(config); err != nil {
			fmt.Println("保存忽略规则失败，请重试")
			return 1
		}
		fmt.Printf("已保存 %d 条忽略规则\n", len(selected))
		return 0
	case "remove":
		selected, err := selectIgnoreChoices(ignoreRuleChoices(config))
		if err != nil {
			fmt.Println("无法完成忽略规则选择，请重试")
			return 1
		}
		if len(selected) == 0 {
			fmt.Println("未修改忽略规则")
			return 0
		}
		for _, choice := range selected {
			if choice.AgentType == "ignore_directory" {
				config.Directories = removeIgnoredDirectory(config.Directories, choice.Cwd)
			} else {
				config.Sessions = removeIgnoredSession(config.Sessions, ignoredSession{AgentType: choice.AgentType, SessionRef: choice.SessionRef})
			}
		}
		if err := saveSessionIgnoreConfig(config); err != nil {
			fmt.Println("保存忽略规则失败，请重试")
			return 1
		}
		fmt.Printf("已移除 %d 条忽略规则\n", len(selected))
		return 0
	default:
		writeIgnoreUsage(os.Stdout)
		return 2
	}
}

func discoverIgnoreCandidates() []*SessionInfo {
	home, _ := os.UserHomeDir()
	return scanSessionsForCommand(filepath.Join(home, ".claude", "projects"), filepath.Join(home, ".codex", "sessions"), true, false)
}

func selectIgnoreChoices(choices []*SessionInfo) ([]*SessionInfo, error) {
	if terminalSupportsTUI(os.Stdin, os.Stdout) {
		return selectSessionsWithTUI(choices)
	}
	return selectSessionsInteractively(choices, defaultSessionPageSize, bufio.NewReader(os.Stdin), os.Stdout)
}

func directoryIgnoreChoices(sessions []*SessionInfo) []*SessionInfo {
	paths := map[string]bool{}
	for _, session := range sessions {
		for _, member := range sessionIgnoreMembers(session) {
			if member != nil && strings.TrimSpace(member.Cwd) != "" {
				paths[filepath.Clean(member.Cwd)] = true
			}
		}
	}
	choices := make([]*SessionInfo, 0, len(paths))
	for path := range paths {
		choices = append(choices, &SessionInfo{AgentType: "directory", Cwd: path, ProjectDir: filepath.Base(path), Summary: "忽略该目录及其子目录的 Session", RecentSummary: "命中后完整上传组将跳过"})
	}
	sort.Slice(choices, func(i, j int) bool { return choices[i].Cwd < choices[j].Cwd })
	return choices
}

func ignoreRuleChoices(config sessionIgnoreConfig) []*SessionInfo {
	choices := make([]*SessionInfo, 0, len(config.Sessions)+len(config.Directories))
	for _, item := range config.Sessions {
		choices = append(choices, &SessionInfo{AgentType: normalizedAgentType(item.AgentType), SessionRef: item.SessionRef, Summary: "忽略的 Session", RecentSummary: "选择后移除该规则"})
	}
	for _, directory := range config.Directories {
		choices = append(choices, &SessionInfo{AgentType: "ignore_directory", Cwd: directory, ProjectDir: filepath.Base(directory), Summary: "忽略的工作目录", RecentSummary: "选择后移除该规则"})
	}
	return choices
}

func appendIgnoredSession(items []ignoredSession, item ignoredSession) []ignoredSession {
	for _, existing := range items {
		if normalizedAgentType(existing.AgentType) == normalizedAgentType(item.AgentType) && existing.SessionRef == item.SessionRef {
			return items
		}
	}
	return append(items, item)
}

func appendIgnoredDirectory(items []string, directory string) []string {
	for _, existing := range items {
		if filepath.Clean(existing) == filepath.Clean(directory) {
			return items
		}
	}
	return append(items, directory)
}

func removeIgnoredSession(items []ignoredSession, target ignoredSession) []ignoredSession {
	result := items[:0]
	for _, item := range items {
		if normalizedAgentType(item.AgentType) != normalizedAgentType(target.AgentType) || item.SessionRef != target.SessionRef {
			result = append(result, item)
		}
	}
	return result
}

func removeIgnoredDirectory(items []string, target string) []string {
	result := items[:0]
	for _, item := range items {
		if filepath.Clean(item) != filepath.Clean(target) {
			result = append(result, item)
		}
	}
	return result
}

func writeIgnoreUsage(output *os.File) {
	fmt.Fprint(output, "用法：aida ignore <session|folder|list|remove>\n")
}

func writeIgnoreList(output *os.File, config sessionIgnoreConfig) {
	if len(config.Sessions) == 0 && len(config.Directories) == 0 {
		fmt.Fprintln(output, "当前没有忽略规则")
		return
	}
	for _, item := range config.Sessions {
		fmt.Fprintf(output, "Session  %s %s\n", normalizedAgentType(item.AgentType), item.SessionRef)
	}
	for _, directory := range config.Directories {
		fmt.Fprintf(output, "目录     %s\n", directory)
	}
}
