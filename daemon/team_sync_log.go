package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const teamSyncUnresolvedFileName = "team-sync-unresolved.json"

type teamSyncUnresolvedFile struct {
	Version     int                       `json:"version"`
	Directories []teamSyncUnresolvedEntry `json:"directories"`
}

type teamSyncUnresolvedEntry struct {
	Path          string    `json:"path"`
	SessionCount  int       `json:"session_count"`
	FirstFoundAt  time.Time `json:"first_found_at"`
	LatestFoundAt time.Time `json:"latest_found_at"`
}

func teamSyncUnresolvedPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".aida", teamSyncUnresolvedFileName)
}

func loadTeamSyncUnresolved() (teamSyncUnresolvedFile, error) {
	state := teamSyncUnresolvedFile{Version: 1, Directories: []teamSyncUnresolvedEntry{}}
	content, err := os.ReadFile(teamSyncUnresolvedPath())
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(content, &state); err != nil {
		return state, fmt.Errorf("read team sync log: %w", err)
	}
	if state.Version != 1 {
		return state, fmt.Errorf("unsupported team sync log version %d", state.Version)
	}
	if state.Directories == nil {
		state.Directories = []teamSyncUnresolvedEntry{}
	}
	return state, nil
}

func updateTeamSyncUnresolved(discovered map[string]int, completeScan bool) error {
	state, err := loadTeamSyncUnresolved()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	byPath := map[string]teamSyncUnresolvedEntry{}
	if !completeScan {
		for _, entry := range state.Directories {
			byPath[entry.Path] = entry
		}
	}
	for path, count := range discovered {
		if count <= 0 {
			continue
		}
		entry, exists := byPath[path]
		if !exists {
			entry = teamSyncUnresolvedEntry{Path: path, FirstFoundAt: now}
		}
		entry.SessionCount = count
		entry.LatestFoundAt = now
		byPath[path] = entry
	}
	state.Version = 1
	state.Directories = make([]teamSyncUnresolvedEntry, 0, len(byPath))
	for _, entry := range byPath {
		state.Directories = append(state.Directories, entry)
	}
	sort.Slice(state.Directories, func(i, j int) bool {
		return state.Directories[i].Path < state.Directories[j].Path
	})
	return saveTeamSyncUnresolved(state)
}

func saveTeamSyncUnresolved(state teamSyncUnresolvedFile) error {
	path := teamSyncUnresolvedPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	content, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	temporary, err := os.CreateTemp(filepath.Dir(path), ".team-sync-unresolved-*")
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
	return os.Rename(temporaryPath, path)
}

func cmdTeamSyncLog(output io.Writer) int {
	state, err := loadTeamSyncUnresolved()
	if err != nil {
		fmt.Fprintf(output, "无法读取团队同步日志：%v\n", err)
		return 1
	}
	if len(state.Directories) == 0 {
		fmt.Fprintln(output, "团队同步没有待配置目录")
		return 0
	}
	fmt.Fprintln(output, "团队同步待配置目录：")
	for _, entry := range state.Directories {
		fmt.Fprintf(output, "- %s（%d 个 Session，最近发现 %s）\n",
			entry.Path, entry.SessionCount, entry.LatestFoundAt.Local().Format("2006-01-02 15:04"))
	}
	fmt.Fprintln(output, "请在 Aida Web 的“我的 Token”页面配置团队设备同步目录。")
	return 0
}
