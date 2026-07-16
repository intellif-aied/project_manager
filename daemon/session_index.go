package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const sessionIndexVersion = 2

type sessionIndex struct {
	Version int                          `json:"version"`
	Entries map[string]sessionIndexEntry `json:"entries"`
}

type sessionIndexEntry struct {
	Size         int64        `json:"size"`
	ModTimeNanos int64        `json:"mod_time_nanos"`
	AgentType    string       `json:"agent_type"`
	Session      *SessionInfo `json:"session"`
}

type localSessionFile struct {
	Path      string
	AgentType string
	Info      fs.FileInfo
}

func sessionIndexPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".aida", "session-index.json")
}

func sessionIndexExists() bool {
	_, err := os.Stat(sessionIndexPath())
	return err == nil
}

func loadSessionIndex() sessionIndex {
	index := sessionIndex{Version: sessionIndexVersion, Entries: map[string]sessionIndexEntry{}}
	content, err := os.ReadFile(sessionIndexPath())
	if err != nil {
		return index
	}
	if json.Unmarshal(content, &index) != nil || index.Version != sessionIndexVersion || index.Entries == nil {
		return sessionIndex{Version: sessionIndexVersion, Entries: map[string]sessionIndexEntry{}}
	}
	return index
}

func saveSessionIndex(index sessionIndex) error {
	if err := os.MkdirAll(filepath.Dir(sessionIndexPath()), 0700); err != nil {
		return err
	}
	content, err := json.Marshal(index)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(sessionIndexPath()), ".session-index-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, sessionIndexPath())
}

func scanLocalSessions(claudeDir, codexDir string, showAll bool, progress func(done, total, reused int)) []*SessionInfo {
	files := collectLocalSessionFiles(claudeDir, codexDir, showAll)
	index := loadSessionIndex()
	nextEntries := make(map[string]sessionIndexEntry, len(files))
	sessions := make([]*SessionInfo, 0, len(files))
	reused := 0
	for done, file := range files {
		entry, cached := index.Entries[file.Path]
		var session *SessionInfo
		if cached && entry.Size == file.Info.Size() && entry.ModTimeNanos == file.Info.ModTime().UnixNano() && entry.Session != nil {
			session = entry.Session
			session.FilePath = file.Path
			session.FileModifiedAt = file.Info.ModTime()
			if file.AgentType == "claude_code" {
				refreshClaudeSubFiles(session)
			}
			reused++
		} else if file.AgentType == "codex" {
			session = parseCodexJSONL(file.Path)
		} else {
			session = parseJSONL(file.Path)
		}
		if session == nil || session.SessionRef == "" {
			if progress != nil {
				progress(done+1, len(files), reused)
			}
			continue
		}
		session.FilePath = file.Path
		session.FileModifiedAt = file.Info.ModTime()
		if file.AgentType == "claude_code" {
			refreshClaudeSessionMetadata(session, claudeDir)
		}
		nextEntries[file.Path] = sessionIndexEntry{
			Size: file.Info.Size(), ModTimeNanos: file.Info.ModTime().UnixNano(), AgentType: file.AgentType, Session: session,
		}
		sessions = append(sessions, session)
		if progress != nil {
			progress(done+1, len(files), reused)
		}
	}
	index.Entries = nextEntries
	_ = saveSessionIndex(index)
	return groupSessionSelections(sessions, showAll, time.Now())
}

func scanSessionsForCommand(claudeDir, codexDir string, showAll, showProgress bool) []*SessionInfo {
	if !showProgress {
		return scanLocalSessions(claudeDir, codexDir, showAll, nil)
	}
	if sessionIndexExists() {
		fmt.Fprintln(os.Stdout, "\n正在检查新增或变化的 Session...")
	} else {
		fmt.Fprintln(os.Stdout, "\n正在建立本地 Session 索引，首次可能需要一些时间...")
	}
	sessions := scanLocalSessions(claudeDir, codexDir, showAll, func(done, total, reused int) {
		fmt.Fprintf(os.Stdout, "\r已检查 %d / %d 个 Session（复用缓存 %d 个）", done, total, reused)
	})
	fmt.Fprintln(os.Stdout)
	return sessions
}

func collectLocalSessionFiles(claudeDir, codexDir string, showAll bool) []localSessionFile {
	files := make([]localSessionFile, 0)
	filepath.WalkDir(claudeDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.Contains(path, string(filepath.Separator)+"subagents"+string(filepath.Separator)) || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err == nil && (showAll || time.Since(info.ModTime()) <= 48*time.Hour) {
			files = append(files, localSessionFile{Path: path, AgentType: "claude_code", Info: info})
		}
		return nil
	})
	filepath.WalkDir(codexDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasPrefix(d.Name(), "rollout-") || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err == nil {
			files = append(files, localSessionFile{Path: path, AgentType: "codex", Info: info})
		}
		return nil
	})
	return files
}

func refreshClaudeSessionMetadata(session *SessionInfo, claudeDir string) {
	rel, _ := filepath.Rel(claudeDir, session.FilePath)
	parts := strings.SplitN(rel, string(filepath.Separator), 2)
	if len(parts) > 0 {
		session.ProjectDir = decodeProjectDir(parts[0])
	}
	refreshClaudeSubFiles(session)
}

func refreshClaudeSubFiles(session *SessionInfo) {
	sessionDir := strings.TrimSuffix(session.FilePath, ".jsonl")
	subDir := filepath.Join(sessionDir, "subagents")
	entries, err := os.ReadDir(subDir)
	if err != nil {
		session.SubFiles = nil
		return
	}
	session.SubFiles = session.SubFiles[:0]
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".jsonl") {
			session.SubFiles = append(session.SubFiles, filepath.Join(subDir, entry.Name()))
		}
	}
}
