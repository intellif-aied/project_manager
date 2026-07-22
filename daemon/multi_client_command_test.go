package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aidashboard/daemon/internal/canonicalupload"
	"github.com/aidashboard/daemon/internal/sessionadapter"
)

type fakeAdditionalAdapter struct {
	sessions []sessionadapter.Descriptor
}

func (adapter fakeAdditionalAdapter) ID() sessionadapter.ClientType { return "openclaw" }
func (adapter fakeAdditionalAdapter) Detect(context.Context) sessionadapter.Detection {
	return sessionadapter.Detection{Installed: true, NativeVersion: "test"}
}
func (adapter fakeAdditionalAdapter) Discover(context.Context, sessionadapter.DiscoverOptions) ([]sessionadapter.Descriptor, []sessionadapter.Diagnostic) {
	return adapter.sessions, nil
}
func (fakeAdditionalAdapter) Materialize(context.Context, sessionadapter.Descriptor) (sessionadapter.MaterializedSession, error) {
	return sessionadapter.MaterializedSession{}, errors.New("Materialize must not run when selection is cancelled")
}

func TestUploadClientWithoutRefsOpensSharedSessionPicker(t *testing.T) {
	descriptor := sessionadapter.Descriptor{
		ClientType: "openclaw", NativeSessionRef: "openclaw-session-1", Summary: "修复新增客户端上传交互",
	}
	originalFactory := additionalUploadAdaptersFactory
	additionalUploadAdaptersFactory = func() ([]sessionadapter.Adapter, error) {
		return []sessionadapter.Adapter{fakeAdditionalAdapter{sessions: []sessionadapter.Descriptor{descriptor}}}, nil
	}
	t.Cleanup(func() { additionalUploadAdaptersFactory = originalFactory })
	t.Setenv("AIDA_TOKEN", "test-token")
	t.Setenv("AIDA_API_URL", "http://127.0.0.1:1/api/v1")

	inputPath := t.TempDir() + "/stdin"
	if err := os.WriteFile(inputPath, []byte("q\n"), 0600); err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(inputPath)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	originalInput, originalOutput := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = input, output
	t.Cleanup(func() { os.Stdin, os.Stdout = originalInput, originalOutput })

	if code := cmdUploadClient([]string{"openclaw"}); code != 0 {
		t.Fatalf("exit code=%d, want 0", code)
	}
	content, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if !strings.Contains(text, "选择要上传的 Session") || strings.Contains(text, "Run: aida upload-client") {
		t.Fatalf("upload-client did not use shared picker:\n%s", text)
	}
}

func TestUploadClientHelpDoesNotDetectOrUpload(t *testing.T) {
	originalFactory := additionalUploadAdaptersFactory
	additionalUploadAdaptersFactory = func() ([]sessionadapter.Adapter, error) {
		return nil, errors.New("adapter discovery must not run for help")
	}
	t.Cleanup(func() { additionalUploadAdaptersFactory = originalFactory })

	for _, args := range [][]string{{"--help"}, {"openclaw", "--help"}} {
		output, err := os.CreateTemp(t.TempDir(), "stdout")
		if err != nil {
			t.Fatal(err)
		}
		originalOutput := os.Stdout
		os.Stdout = output
		if code := cmdUploadClient(args); code != 0 {
			os.Stdout = originalOutput
			output.Close()
			t.Fatalf("args=%v exit code=%d, want 0", args, code)
		}
		os.Stdout = originalOutput
		if err := output.Close(); err != nil {
			t.Fatal(err)
		}
		content, err := os.ReadFile(output.Name())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(content), "aida upload-client") || !strings.Contains(string(content), "OpenClaw") {
			t.Fatalf("args=%v help=%q", args, content)
		}
	}
}

func TestAdditionalClientUsesSharedInteractiveSessionPicker(t *testing.T) {
	lastActive := time.Date(2026, 7, 22, 9, 30, 0, 0, time.Local)
	sessions := []sessionadapter.Descriptor{{
		ClientType:       "openclaw",
		NativeSessionRef: "openclaw-session-1",
		LastActivityAt:   lastActive,
		ProjectName:      "project-manager",
		Summary:          "修复新增客户端上传交互",
	}}
	input := bufio.NewReader(strings.NewReader("1\nd\n"))
	var output bytes.Buffer

	selected, err := selectAdditionalSessionsInteractively("openclaw", sessions, input, &output)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 1 || selected[0].NativeSessionRef != "openclaw-session-1" {
		t.Fatalf("selected=%+v", selected)
	}
	for _, expected := range []string{"选择要上传的 Session", "openclaw openclaw-session-1", "project-manager", "修复新增客户端上传交互"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("shared picker output missing %q:\n%s", expected, output.String())
		}
	}
}

func TestOpenClawSharedInteractivePickerRejectsSelectAll(t *testing.T) {
	sessions := []sessionadapter.Descriptor{
		{ClientType: "openclaw", NativeSessionRef: "openclaw-session-1", Summary: "first"},
		{ClientType: "openclaw", NativeSessionRef: "openclaw-session-2", Summary: "second"},
	}
	input := bufio.NewReader(strings.NewReader("all\nd\n"))
	var output bytes.Buffer

	selected, err := selectAdditionalSessionsInteractively("openclaw", sessions, input, &output)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 0 {
		t.Fatalf("OpenClaw select-all selected=%+v", selected)
	}
	if !strings.Contains(output.String(), "OpenClaw 不支持全选，请逐项选择 Session") {
		t.Fatalf("missing OpenClaw privacy guidance:\n%s", output.String())
	}
}

func TestOpenClawBulkUploadIsAlwaysRejectedBeforeDiscovery(t *testing.T) {
	if code := cmdUploadClient([]string{"openclaw", "--all"}); code != 2 {
		t.Fatalf("exit code=%d, want 2", code)
	}
	if code := cmdUploadClient([]string{"openclaw", "-a"}); code != 2 {
		t.Fatalf("short flag exit code=%d, want 2", code)
	}
}

func TestAdditionalUploadSuccessOutputHidesProtocolDetails(t *testing.T) {
	results := []canonicalupload.UploadedSource{{
		SessionRef:     "openclaw-session-1",
		GenerationID:   "internal-generation-id",
		UploadedChunks: 0,
		Finalized:      false,
	}}
	var output bytes.Buffer

	writeAdditionalUploadSuccess(&output, "openclaw", results)

	text := output.String()
	for _, expected := range []string{"openclaw-session-1", "同步完成", "共 1 个 openclaw Session"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("success output missing %q:\n%s", expected, text)
		}
	}
	for _, internal := range []string{"internal-generation-id", "generation=", "chunks=", "finalized=", "Token capability", "fixture reconciliation"} {
		if strings.Contains(text, internal) {
			t.Fatalf("success output exposes internal detail %q:\n%s", internal, text)
		}
	}
}
