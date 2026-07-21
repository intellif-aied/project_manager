package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestHelpOnlyShowsUserFacingCommands(t *testing.T) {
	var output bytes.Buffer
	writeUsage(&output)

	for _, required := range []string{
		"登录 Aida",
		"选择并上传 Session",
		"开启自动同步",
		"检查登录、连接和自动同步状态",
	} {
		if !strings.Contains(output.String(), required) {
			t.Fatalf("help missing %q: %q", required, output.String())
		}
	}
	assertNoInternalOutput(t, output.String())
}

func TestLoginSuccessDoesNotExposeConnectionDetails(t *testing.T) {
	var output bytes.Buffer
	writeLoginSuccess(&output, "测试02")

	if output.String() != "登录成功，测试02\n" {
		t.Fatalf("login output = %q", output.String())
	}
	assertNoInternalOutput(t, output.String())
}

func TestSessionUploadResultsAreSeparatedAndHideInternalFields(t *testing.T) {
	first := &SessionInfo{
		SessionRef: "internal-session-ref-1",
		Summary:    "修复登录交互",
		EndedAt:    time.Date(2026, 7, 21, 7, 10, 0, 0, time.UTC),
	}
	second := &SessionInfo{
		SessionRef: "internal-session-ref-2",
		Summary:    "验证自动同步",
		EndedAt:    time.Date(2026, 7, 21, 7, 20, 0, 0, time.UTC),
	}

	var output bytes.Buffer
	printSessionUploadResult(&output, first, false)
	output.WriteString("\n")
	printSessionUploadResult(&output, second, true)

	if !strings.Contains(output.String(), "[完成]") || !strings.Contains(output.String(), "[失败]") {
		t.Fatalf("upload output = %q", output.String())
	}
	if !strings.Contains(output.String(), "修复登录交互\n\n[失败]") {
		t.Fatalf("sessions are not separated by a blank line: %q", output.String())
	}
	if strings.Contains(output.String(), "internal-session-ref") {
		t.Fatalf("upload output exposed SessionRef: %q", output.String())
	}
	assertNoInternalOutput(t, output.String())
}

func assertNoInternalOutput(t *testing.T, output string) {
	t.Helper()
	for _, forbidden := range []string{
		"http://",
		"https://",
		"api/v1",
		".aida",
		"systemd",
		"incremental=",
		"chunks=",
		"snapshot-only",
	} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("output exposed %q: %q", forbidden, output)
		}
	}
}
