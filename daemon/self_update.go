package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	autoUpdateInterval = 24 * time.Hour
	updateHTTPTimeout  = 10 * time.Minute
	maxUpdateTextBytes = 1 << 20
)

func maybeAutoUpdate(cfg *Config) {
	if cfg == nil || !cfg.AutoUpdate || strings.TrimSpace(cfg.ReleaseURL) == "" || Version == "dev" {
		return
	}
	if last, err := time.Parse(time.RFC3339, cfg.LastUpdateCheck); err == nil && time.Since(last) < autoUpdateInterval {
		return
	}
	cfg.LastUpdateCheck = time.Now().UTC().Format(time.RFC3339)
	saveConfig(cfg)
	updated, err := performSelfUpdate(cfg)
	if err == nil && updated {
		fmt.Println("Aida updated in the background; the new version will be used on the next command.")
	}
}

func cmdUpdate() {
	cfg := loadConfig()
	if strings.TrimSpace(cfg.ReleaseURL) == "" {
		fmt.Println("Update unavailable: release_url is not configured. Re-run the installer once to enable self-update.")
		return
	}
	updated, err := performSelfUpdate(cfg)
	if err != nil {
		fmt.Printf("Update failed: %v\n", err)
		return
	}
	if !updated {
		fmt.Printf("aida v%s is already current.\n", Version)
		return
	}
	fmt.Println("Update installed. The new version will be used on the next command.")
}

func performSelfUpdate(cfg *Config) (bool, error) {
	releaseURL := strings.TrimRight(strings.TrimSpace(cfg.ReleaseURL), "/")
	latestBytes, err := fetchUpdateResource(releaseURL+"/aida-latest.txt", 128)
	if err != nil {
		return false, err
	}
	latest := strings.TrimSpace(string(latestBytes))
	if latest == "" {
		return false, errors.New("release version is empty")
	}
	if !versionGreater(latest, Version) {
		return false, nil
	}
	binaryName, err := releaseBinaryName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return false, err
	}
	manifest, err := fetchUpdateResource(releaseURL+"/SHA256SUMS.txt", maxUpdateTextBytes)
	if err != nil {
		return false, err
	}
	expectedHash, err := checksumForFile(manifest, binaryName)
	if err != nil {
		return false, err
	}
	executable, err := os.Executable()
	if err != nil {
		return false, err
	}
	temporary, err := os.CreateTemp(filepath.Dir(executable), ".aida-update-*")
	if err != nil {
		return false, fmt.Errorf("create update beside executable: %w", err)
	}
	temporaryPath := temporary.Name()
	cleanup := true
	defer func() {
		_ = temporary.Close()
		if cleanup {
			_ = os.Remove(temporaryPath)
		}
	}()

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, releaseURL+"/"+binaryName, nil)
	if err != nil {
		return false, err
	}
	response, err := (&http.Client{Timeout: updateHTTPTimeout}).Do(request)
	if err != nil {
		return false, fmt.Errorf("download update: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false, fmt.Errorf("download update HTTP %d", response.StatusCode)
	}
	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(temporary, hasher), response.Body)
	if err != nil {
		return false, fmt.Errorf("download update: %w", err)
	}
	if written < 1<<20 {
		return false, fmt.Errorf("downloaded update is unexpectedly small: %d bytes", written)
	}
	if actual := hex.EncodeToString(hasher.Sum(nil)); actual != expectedHash {
		return false, errors.New("downloaded update checksum mismatch")
	}
	if err := temporary.Sync(); err != nil {
		return false, err
	}
	if err := temporary.Close(); err != nil {
		return false, err
	}
	if err := os.Chmod(temporaryPath, 0755); err != nil {
		return false, err
	}
	if runtime.GOOS == "windows" {
		if err := scheduleWindowsReplacement(temporaryPath, executable); err != nil {
			return false, err
		}
		cleanup = false
		return true, nil
	}
	if err := os.Rename(temporaryPath, executable); err != nil {
		return false, fmt.Errorf("replace executable: %w", err)
	}
	cleanup = false
	return true, nil
}

func fetchUpdateResource(url string, limit int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	response, err := (&http.Client{Timeout: 15 * time.Second}).Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: HTTP %d", url, response.StatusCode)
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > limit {
		return nil, errors.New("update metadata exceeds limit")
	}
	return content, nil
}

func releaseBinaryName(goos, goarch string) (string, error) {
	switch goos + "/" + goarch {
	case "linux/amd64":
		return "aida-linux-amd64", nil
	case "darwin/arm64":
		return "aida-darwin-arm64", nil
	case "windows/amd64":
		return "aida-windows-amd64.exe", nil
	default:
		return "", fmt.Errorf("self-update is unsupported on %s/%s", goos, goarch)
	}
}

func checksumForFile(manifest []byte, filename string) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(manifest)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && strings.TrimPrefix(fields[len(fields)-1], "*") == filename {
			value := strings.ToLower(fields[0])
			decoded, err := hex.DecodeString(value)
			if err == nil && len(decoded) == sha256.Size {
				return value, nil
			}
			return "", errors.New("invalid SHA256SUMS entry")
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("SHA256SUMS has no entry for %s", filename)
}

func versionGreater(candidate, current string) bool {
	left, leftOK := numericVersion(candidate)
	right, rightOK := numericVersion(current)
	if !leftOK || !rightOK {
		return strings.TrimSpace(candidate) != strings.TrimSpace(current)
	}
	for index := 0; index < len(left) || index < len(right); index++ {
		var a, b int
		if index < len(left) {
			a = left[index]
		}
		if index < len(right) {
			b = right[index]
		}
		if a != b {
			return a > b
		}
	}
	return false
}

func numericVersion(value string) ([]int, bool) {
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(value), "v"), ".")
	if len(parts) == 0 {
		return nil, false
	}
	values := make([]int, len(parts))
	for index, part := range parts {
		parsed, err := strconv.Atoi(part)
		if err != nil || parsed < 0 {
			return nil, false
		}
		values[index] = parsed
	}
	return values, true
}

func scheduleWindowsReplacement(updatePath, executable string) error {
	scriptPath := updatePath + ".cmd"
	script := fmt.Sprintf("@echo off\r\n:retry\r\nmove /Y \"%s\" \"%s\" >nul 2>&1\r\nif errorlevel 1 (timeout /t 1 /nobreak >nul & goto retry)\r\ndel \"%%~f0\"\r\n", updatePath, executable)
	if err := os.WriteFile(scriptPath, []byte(script), 0600); err != nil {
		return err
	}
	command := exec.Command("cmd.exe", "/C", "start", "", "/B", scriptPath)
	if err := command.Start(); err != nil {
		_ = os.Remove(scriptPath)
		return err
	}
	return nil
}
