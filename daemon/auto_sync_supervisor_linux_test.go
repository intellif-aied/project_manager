package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSystemdUserManagerSupportCheck(t *testing.T) {
	var calls [][]string
	runner := func(args ...string) error {
		calls = append(calls, append([]string(nil), args...))
		return nil
	}

	if err := checkSystemdUserManager(runner); err != nil {
		t.Fatalf("support check failed: %v", err)
	}
	want := [][]string{{"--user", "show-environment"}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("systemctl calls = %#v, want %#v", calls, want)
	}

	runner = func(...string) error { return errors.New("no user bus") }
	if err := checkSystemdUserManager(runner); !errors.Is(err, errAutoSyncSystemdUnavailable) {
		t.Fatalf("support check error = %v, want systemd unavailable", err)
	}
}

func TestInstallSystemdUserUnitStartsAutoSyncDaemon(t *testing.T) {
	home := t.TempDir()
	var calls [][]string
	runner := func(args ...string) error {
		calls = append(calls, append([]string(nil), args...))
		return nil
	}

	if err := installSystemdAutoSyncUnit(home, "/opt/Aida CLI/aida", runner); err != nil {
		t.Fatal(err)
	}
	unitPath := filepath.Join(home, ".config", "systemd", "user", "aida-auto-sync.service")
	data, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatal(err)
	}
	unit := string(data)
	for _, want := range []string{
		`ExecStart="/opt/Aida CLI/aida" auto-sync daemon-run`,
		"KillMode=control-group",
		"Restart=on-failure",
		"RestartSec=10s",
		"WantedBy=default.target",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("unit missing %q:\n%s", want, unit)
		}
	}
	wantCalls := [][]string{
		{"--user", "daemon-reload"},
		{"--user", "enable", "--now", "aida-auto-sync.service"},
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("systemctl calls = %#v, want %#v", calls, wantCalls)
	}
}

func TestAutoSyncFileLockIsExclusiveAndReleasable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")
	releaseFirst, err := acquireAutoSyncFileLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := acquireAutoSyncFileLock(path); !errors.Is(err, errAutoSyncLockHeld) {
		t.Fatalf("second lock error = %v, want lock held", err)
	}
	releaseFirst()

	releaseAgain, err := acquireAutoSyncFileLock(path)
	if err != nil {
		t.Fatalf("lock after release: %v", err)
	}
	releaseAgain()
}

func TestEnsureSystemdUnitLeavesActiveServiceRunning(t *testing.T) {
	home := t.TempDir()
	var calls [][]string
	runner := func(args ...string) error {
		calls = append(calls, append([]string(nil), args...))
		return nil
	}

	if err := ensureSystemdAutoSyncUnit(home, "/opt/aida", runner); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"--user", "is-active", "--quiet", "aida-auto-sync.service"}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("systemctl calls = %#v, want %#v", calls, want)
	}
	unitPath := filepath.Join(home, ".config", "systemd", "user", "aida-auto-sync.service")
	if _, err := os.Stat(unitPath); !os.IsNotExist(err) {
		t.Fatalf("active ensure rewrote unit: %v", err)
	}
}
