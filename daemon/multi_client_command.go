package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aidashboard/daemon/internal/adapters/kimicode"
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
	return []sessionadapter.Adapter{opencode.New(spool), kimicode.New("", spool), workbuddy.New()}, nil
}

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
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: aida upload-client <opencode|kimi_code> [session-ref...] [--all]")
		return 2
	}
	clientType := sessionadapter.ClientType(strings.TrimSpace(args[0]))
	if clientType == "workbuddy" {
		fmt.Fprintln(os.Stderr, "WorkBuddy is detected-only until an official machine-readable export contract is available.")
		return 2
	}
	cfg := loadConfig()
	if err := requireAuth(cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	resolveAPIEndpoint(cfg)
	adapters, err := additionalAdapters()
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
	if !all && len(wanted) == 0 {
		printAdditionalSessions(clientType, sessions)
		fmt.Printf("\nRun: aida upload-client %s <session-ref>\n", clientType)
		return 0
	}
	byRef := map[string]sessionadapter.Descriptor{}
	for _, session := range sessions {
		byRef[session.NativeSessionRef] = session
	}
	selected := make([]sessionadapter.Descriptor, 0)
	if all {
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
	results, err := uploader.UploadFamily(ctx, family)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Canonical upload failed: %v\n", err)
		return 1
	}
	for _, result := range results {
		fmt.Printf("[UPLOADED] %s generation=%s chunks=%d finalized=%t\n", result.SessionRef, result.GenerationID, result.UploadedChunks, result.Finalized)
	}
	fmt.Printf("Uploaded %d %s session(s). Token capability remains unavailable until exact fixture reconciliation passes.\n", len(results), clientType)
	return 0
}

func printAdditionalSessions(clientType sessionadapter.ClientType, sessions []sessionadapter.Descriptor) {
	fmt.Printf("%s sessions:\n", clientType)
	for _, session := range sessions {
		fmt.Printf("  %s  %s", session.NativeSessionRef, session.Summary)
		if session.ParentRef != "" {
			fmt.Printf(" parent=%s", session.ParentRef)
		}
		fmt.Println()
	}
}
