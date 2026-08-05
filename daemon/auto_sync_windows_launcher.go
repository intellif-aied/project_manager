package main

import (
	"fmt"
	"strings"
)

const autoSyncWindowsLauncherName = "run-hidden.vbs"

func windowsAutoSyncLauncherScript(executable string) string {
	executable = strings.ReplaceAll(executable, `"`, `""`)
	return fmt.Sprintf("Set shell = CreateObject(\"WScript.Shell\")\r\nexitCode = shell.Run(\"\"\"%s\"\" auto-sync tick\", 0, True)\r\nWScript.Quit exitCode\r\n", executable)
}

func windowsAutoSyncTaskAction(scriptPath string) string {
	return `wscript.exe //B //NoLogo "` + strings.ReplaceAll(scriptPath, `"`, `""`) + `"`
}
