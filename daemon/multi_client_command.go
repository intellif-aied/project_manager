package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aidashboard/daemon/internal/adapters/kimicode"
	"github.com/aidashboard/daemon/internal/adapters/openclaw"
	"github.com/aidashboard/daemon/internal/adapters/opencode"
	"github.com/aidashboard/daemon/internal/adapters/workbuddy"
	"github.com/aidashboard/daemon/internal/canonicalupload"
	"github.com/aidashboard/daemon/internal/sessionadapter"
)

func additionalAdapters() ([]sessionadapter.Adapter, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	spool := filepath.Join(home, ".aida", "canonical-spool")
	return []sessionadapter.Adapter{opencode.New(spool), kimicode.New("", spool), openclaw.New(spool), workbuddy.New()}, nil
}

var additionalUploadAdaptersFactory = additionalAdapters

func cmdClients() int {
	adapters, err := additionalAdapters()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Client detection failed: %v\n", err)
		return 1
	}
	ctx := context.Background()
	for _, adapter := range adapters {
		detection := adapter.Detect(ctx)
		status := "not detected"
		if detection.Installed {
			status = "detected"
		}
		fmt.Printf("%-12s %-12s", adapter.ID(), status)
		if detection.NativeVersion != "" {
			fmt.Printf(" version=%s", detection.NativeVersion)
		}
		if detection.Diagnostic != "" {
			fmt.Printf("  %s", detection.Diagnostic)
		}
		fmt.Println()
	}
	return 0
}

func cmdUploadClient(args []string) int {
	if uploadClientHelpRequested(args) {
		writeUploadClientUsage(os.Stdout)
		return 0
	}
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: aida upload-client <opencode|kimi_code|openclaw> [session-ref...] [--all]")
		return 2
	}
	clientType := sessionadapter.ClientType(strings.TrimSpace(args[0]))
	if clientType == "workbuddy" {
		fmt.Fprintln(os.Stderr, "WorkBuddy is detected-only until an official machine-readable export contract is available.")
		return 2
	}
	if clientType == "openclaw" {
		for _, arg := range args[1:] {
			if arg == "--all" || arg == "-a" {
				fmt.Fprintln(os.Stderr, "OpenClaw sessions may contain private or non-coding conversations; select session refs explicitly.")
				return 2
			}
		}
	}
	cfg := loadConfig()
	if err := requireAuth(cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	resolveAPIEndpoint(cfg)
	adapters, err := additionalUploadAdaptersFactory()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	var adapter sessionadapter.Adapter
	for _, candidate := range adapters {
		if candidate.ID() == clientType {
			adapter = candidate
			break
		}
	}
	if adapter == nil {
		fmt.Fprintf(os.Stderr, "Unsupported additional client: %s\n", clientType)
		return 2
	}
	ctx := context.Background()
	detection := adapter.Detect(ctx)
	if !detection.Installed {
		fmt.Fprintf(os.Stderr, "%s is not detected: %s\n", clientType, detection.Diagnostic)
		return 1
	}
	sessions, diagnostics := adapter.Discover(ctx, sessionadapter.DiscoverOptions{All: true})
	for _, diagnostic := range diagnostics {
		fmt.Fprintf(os.Stderr, "[%s] %s\n", diagnostic.Code, diagnostic.Message)
	}
	if len(sessions) == 0 {
		fmt.Printf("No %s sessions found.\n", clientType)
		return 0
	}
	all := false
	wanted := map[string]bool{}
	for _, arg := range args[1:] {
		if arg == "--all" || arg == "-a" {
			all = true
		} else {
			wanted[arg] = true
		}
	}
	byRef := map[string]sessionadapter.Descriptor{}
	for _, session := range sessions {
		byRef[session.NativeSessionRef] = session
	}
	selected := make([]sessionadapter.Descriptor, 0)
	if !all && len(wanted) == 0 {
		selected, err = selectAdditionalSessionsForCurrentTerminal(clientType, sessions)
		if err != nil {
			fmt.Fprintln(os.Stderr, "无法完成 Session 选择，请重试")
			return 1
		}
		if len(selected) == 0 {
			fmt.Println("未选择 Session")
			return 0
		}
	} else if all {
		selected = append(selected, sessions...)
	} else {
		for ref := range wanted {
			session, ok := byRef[ref]
			if !ok {
				fmt.Fprintf(os.Stderr, "Unknown %s session: %s\n", clientType, ref)
				return 2
			}
			selected = append(selected, session)
		}
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].NativeSessionRef < selected[j].NativeSessionRef })
	family := make([]sessionadapter.MaterializedSession, 0, len(selected))
	included := map[string]bool{}
	for _, descriptor := range selected {
		materialized, materializeErr := adapter.Materialize(ctx, descriptor)
		if materializeErr != nil {
			fmt.Fprintf(os.Stderr, "Materialize %s failed: %v\n", descriptor.NativeSessionRef, materializeErr)
			return 1
		}
		family = append(family, materialized)
		included[descriptor.NativeSessionRef] = true
	}
	for index := 0; index < len(family); index++ {
		parent := family[index].Descriptor.ParentRef
		if parent == "" || included[parent] {
			continue
		}
		descriptor, ok := byRef[parent]
		if !ok {
			fmt.Fprintf(os.Stderr, "Parent session %s is missing from discovery; refusing incomplete family upload.\n", parent)
			return 1
		}
		family = append(family, sessionadapter.MaterializedSession{Descriptor: descriptor})
		included[parent] = true
	}
	uploader, err := canonicalupload.New(canonicalupload.Config{BaseURL: apiBaseURL(cfg), Token: cfg.Token, ClientVersion: Version})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	releaseUpload, lockCode := beginSessionUpload(os.Stdout)
	if lockCode != 0 {
		return lockCode
	}
	defer releaseUpload()
	results, err := uploader.UploadFamily(ctx, family)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Canonical upload failed: %v\n", err)
		return 1
	}
	writeAdditionalUploadSuccess(os.Stdout, clientType, results)
	return 0
}

func writeAdditionalUploadSuccess(output io.Writer, clientType sessionadapter.ClientType, results []canonicalupload.UploadedSource) {
	for _, result := range results {
		fmt.Fprintf(output, "同步完成：%s %s\n", clientType, result.SessionRef)
	}
	fmt.Fprintf(output, "共 %d 个 %s Session 同步完成。\n", len(results), clientType)
}

func uploadClientHelpRequested(args []string) bool {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			return true
		}
	}
	return false
}

func writeUploadClientUsage(output io.Writer) {
	fmt.Fprint(output, `Aida additional client upload

Usage:
  aida upload-client <opencode|kimi_code|openclaw>
  aida upload-client <opencode|kimi_code> --all
  aida upload-client <client> <session-ref...>

Without session refs, Aida opens the same Session picker used by aida upload.
OpenClaw requires explicit per-Session selection and does not support --all.
`)
}

func selectAdditionalSessionsForCurrentTerminal(
	clientType sessionadapter.ClientType,
	sessions []sessionadapter.Descriptor,
) ([]sessionadapter.Descriptor, error) {
	if !terminalSupportsTUI(os.Stdin, os.Stdout) {
		return selectAdditionalSessionsInteractively(clientType, sessions, bufio.NewReader(os.Stdin), os.Stdout)
	}
	pickerSessions, byRef := additionalSessionsForPicker(clientType, sessions)
	selected, err := selectSessionsWithTUIOptions(pickerSessions, additionalSessionSelectionOptions(clientType))
	if err != nil {
		return nil, err
	}
	return selectedAdditionalDescriptors(selected, byRef), nil
}

func selectAdditionalSessionsInteractively(
	clientType sessionadapter.ClientType,
	sessions []sessionadapter.Descriptor,
	reader *bufio.Reader,
	output io.Writer,
) ([]sessionadapter.Descriptor, error) {
	pickerSessions, byRef := additionalSessionsForPicker(clientType, sessions)
	selected, err := selectSessionsInteractivelyWithOptions(
		pickerSessions,
		defaultSessionPageSize,
		reader,
		output,
		additionalSessionSelectionOptions(clientType),
	)
	if err != nil {
		return nil, err
	}
	return selectedAdditionalDescriptors(selected, byRef), nil
}

func additionalSessionSelectionOptions(clientType sessionadapter.ClientType) sessionSelectionOptions {
	options := defaultSessionSelectionOptions()
	if clientType == "openclaw" {
		options.AllowSelectAll = false
		options.SelectAllDisabledNotice = "OpenClaw 不支持全选，请逐项选择 Session"
	}
	return options
}

func additionalSessionsForPicker(
	clientType sessionadapter.ClientType,
	sessions []sessionadapter.Descriptor,
) ([]*SessionInfo, map[string]sessionadapter.Descriptor) {
	pickerSessions := make([]*SessionInfo, 0, len(sessions))
	byRef := make(map[string]sessionadapter.Descriptor, len(sessions))
	for _, descriptor := range sessions {
		byRef[descriptor.NativeSessionRef] = descriptor
		pickerSessions = append(pickerSessions, &SessionInfo{
			SessionRef:        descriptor.NativeSessionRef,
			ParentSessionRef:  descriptor.ParentRef,
			ForkedAt:          descriptor.ForkedAt,
			ForkSource:        descriptor.ForkSource,
			AgentType:         string(clientType),
			ProjectDir:        descriptor.ProjectName,
			Cwd:               descriptor.CWD,
			StartedAt:         descriptor.StartedAt,
			EndedAt:           descriptor.LastActivityAt,
			Summary:           descriptor.Summary,
			SelectionActiveAt: descriptor.LastActivityAt,
		})
	}
	return pickerSessions, byRef
}

func selectedAdditionalDescriptors(
	selected []*SessionInfo,
	byRef map[string]sessionadapter.Descriptor,
) []sessionadapter.Descriptor {
	result := make([]sessionadapter.Descriptor, 0, len(selected))
	for _, session := range selected {
		if session == nil {
			continue
		}
		if descriptor, ok := byRef[session.SessionRef]; ok {
			result = append(result, descriptor)
		}
	}
	return result
}
