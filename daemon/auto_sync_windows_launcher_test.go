package main

import (
	"strings"
	"testing"
)

func TestWindowsAutoSyncLauncherRunsWithoutConsoleWindow(t *testing.T) {
	t.Parallel()

	script := windowsAutoSyncLauncherScript(`C:\Program Files\AIDA\aida.exe`)
	if !strings.Contains(script, `""C:\Program Files\AIDA\aida.exe"" auto-sync tick`) {
		t.Fatalf("launcher script does not invoke the CLI safely: %q", script)
	}
	if !strings.Contains(script, `, 0, True`) {
		t.Fatalf("launcher script must hide the window and wait for completion: %q", script)
	}

	action := windowsAutoSyncTaskAction(`C:\Users\tester\.aida\auto-sync\run-hidden.vbs`)
	if !strings.HasPrefix(action, `wscript.exe //B //NoLogo `) {
		t.Fatalf("scheduled task must use the windowless launcher: %q", action)
	}
	if strings.Contains(action, `aida.exe`) {
		t.Fatalf("scheduled task must not launch the console CLI directly: %q", action)
	}
}
