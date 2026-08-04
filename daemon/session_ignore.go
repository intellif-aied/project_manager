package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const sessionIgnoreSchemaVersion = 1

type ignoredSession struct {
	AgentType  string `json:"agent_type"`
	SessionRef string `json:"session_ref"`
}

type sessionIgnoreConfig struct {
	SchemaVersion int              `json:"schema_version"`
	Sessions      []ignoredSession `json:"sessions,omitempty"`
	Directories   []string         `json:"directories,omitempty"`
}

func sessionIgnorePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".aida", "ignore.json")
}

func loadSessionIgnoreConfig() (sessionIgnoreConfig, error) {
	config := sessionIgnoreConfig{SchemaVersion: sessionIgnoreSchemaVersion}
	content, err := os.ReadFile(sessionIgnorePath())
	if os.IsNotExist(err) {
		return config, nil
	}
	if err != nil {
		return sessionIgnoreConfig{}, err
	}
	if err := json.Unmarshal(content, &config); err != nil {
		return sessionIgnoreConfig{}, fmt.Errorf("parse ignore configuration: %w", err)
	}
	if config.SchemaVersion != sessionIgnoreSchemaVersion {
		return sessionIgnoreConfig{}, fmt.Errorf("unsupported ignore configuration version %d", config.SchemaVersion)
	}
	return config, nil
}

func saveSessionIgnoreConfig(config sessionIgnoreConfig) error {
	config.SchemaVersion = sessionIgnoreSchemaVersion
	content, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	directory := filepath.Dir(sessionIgnorePath())
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".ignore-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, sessionIgnorePath())
}

func filterIgnoredSessionGroups(sessions []*SessionInfo, config sessionIgnoreConfig) []*SessionInfo {
	filtered := make([]*SessionInfo, 0, len(sessions))
	for _, session := range sessions {
		if !sessionGroupIsIgnored(session, config) {
			filtered = append(filtered, session)
		}
	}
	return filtered
}

func sessionGroupIsIgnored(session *SessionInfo, config sessionIgnoreConfig) bool {
	for _, member := range sessionIgnoreMembers(session) {
		if sessionMatchesIgnore(member, config) {
			return true
		}
	}
	return false
}

func sessionIgnoreMembers(session *SessionInfo) []*SessionInfo {
	if session == nil {
		return nil
	}
	members := []*SessionInfo{session}
	members = append(members, session.SelectionChildren...)
	for _, path := range session.SubFiles {
		if sub := parseJSONL(path); sub != nil {
			members = append(members, sub)
		}
	}
	return members
}

func sessionMatchesIgnore(session *SessionInfo, config sessionIgnoreConfig) bool {
	if session == nil {
		return false
	}
	for _, ignored := range config.Sessions {
		if normalizedAgentType(ignored.AgentType) == normalizedAgentType(session.AgentType) &&
			strings.TrimSpace(ignored.SessionRef) == session.SessionRef {
			return true
		}
	}
	for _, directory := range config.Directories {
		if directoryContainsSession(directory, session.Cwd) {
			return true
		}
	}
	return false
}

func directoryContainsSession(directory, cwd string) bool {
	if strings.TrimSpace(directory) == "" || strings.TrimSpace(cwd) == "" {
		return false
	}
	directory, err := filepath.Abs(filepath.Clean(directory))
	if err != nil {
		return false
	}
	cwd, err = filepath.Abs(filepath.Clean(cwd))
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(directory, cwd)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
