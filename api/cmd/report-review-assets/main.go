package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/aidashboard/api/model"
	"github.com/aidashboard/api/service"
)

func main() {
	var apply bool
	flag.BoolVar(&apply, "apply", false, "create or update the configured system assets")
	flag.Parse()
	if err := run(context.Background(), apply); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, apply bool) error {
	baseURL := strings.TrimSpace(os.Getenv("MANAGED_AGENT_URL"))
	token := strings.TrimSpace(os.Getenv("MANAGED_AGENT_TOKEN"))
	owner := strings.TrimSpace(os.Getenv("REPORT_REVIEW_SKILL_OWNER"))
	agentID := strings.TrimSpace(os.Getenv("REPORT_REVIEW_AGENT_ID"))
	mcpURL := strings.TrimSpace(os.Getenv("REPORT_REVIEW_MCP_URL"))
	modelID := strings.TrimSpace(os.Getenv("REPORT_REVIEW_MODEL_ID"))
	skillVersion := strings.TrimSpace(os.Getenv("REPORT_REVIEW_SKILL_VERSION"))
	if modelID == "" {
		modelID = "MiniMax-M2.5"
	}
	for key, value := range map[string]string{
		"MANAGED_AGENT_URL": baseURL, "MANAGED_AGENT_TOKEN": token,
		"REPORT_REVIEW_SKILL_OWNER": owner, "REPORT_REVIEW_AGENT_ID": agentID,
		"REPORT_REVIEW_MCP_URL": mcpURL, "REPORT_REVIEW_SKILL_VERSION": skillVersion,
	} {
		if value == "" {
			return fmt.Errorf("%s is required", key)
		}
	}
	if skillVersion != service.ReportReviewSkillVersion {
		return fmt.Errorf("REPORT_REVIEW_SKILL_VERSION must be %s for this build", service.ReportReviewSkillVersion)
	}
	plan := map[string]any{
		"owner": owner, "agent_id": agentID, "model_id": modelID,
		"skill":   service.ReportReviewSkillSlug + "@" + service.ReportReviewSkillVersion,
		"mcp":     service.ReportReviewMCPSlug + "@" + service.ReportReviewMCPVersion,
		"mcp_url": mcpURL, "apply": apply,
	}
	if !apply {
		payload, _ := json.MarshalIndent(plan, "", "  ")
		fmt.Println(string(payload))
		return nil
	}
	client := service.NewManagedAgentClient(baseURL, token)
	if err := ensureSkill(ctx, client, owner); err != nil {
		return err
	}
	if err := ensureMCP(ctx, client, owner, mcpURL); err != nil {
		return err
	}
	version, err := ensureAgent(ctx, client, owner, agentID, modelID)
	if err != nil {
		return err
	}
	plan["managed_version"] = version
	payload, _ := json.MarshalIndent(plan, "", "  ")
	fmt.Println(string(payload))
	return nil
}

func ensureSkill(ctx context.Context, client *service.ManagedAgentClient, owner string) error {
	listed, err := client.ListSkills(ctx, "mine")
	if err != nil {
		return err
	}
	for _, skill := range listed.Skills {
		if !skill.Archived && (skill.Owner == "" || skill.Owner == owner) &&
			skill.Slug == service.ReportReviewSkillSlug &&
			skill.Version == service.ReportReviewSkillVersion {
			return nil
		}
	}
	created, err := client.CreateSkill(ctx, service.CreateManagedSkillRequest{
		Slug: service.ReportReviewSkillSlug, Version: service.ReportReviewSkillVersion,
		Name:        service.ReportReviewSkillName,
		Description: "Independently verify one frozen Aida personal daily report proposal.",
		SkillMD:     service.ReportReviewSkillMarkdown(),
	})
	if err != nil {
		return err
	}
	if created == nil || strings.TrimSpace(created.SkillID) == "" {
		return errors.New("Agent platform returned an empty Report Review Skill")
	}
	return nil
}

func ensureMCP(ctx context.Context, client *service.ManagedAgentClient, owner, mcpURL string) error {
	listed, err := client.ListMCPEntries(ctx, "mine")
	if err != nil {
		return err
	}
	for _, entry := range listed.Entries {
		if !entry.Archived && (entry.Owner == "" || entry.Owner == owner) &&
			entry.Slug == service.ReportReviewMCPSlug &&
			entry.Version == service.ReportReviewMCPVersion {
			if strings.TrimRight(entry.URL, "/") != strings.TrimRight(mcpURL, "/") {
				return errors.New("existing Project Memory MCP version points to a different URL")
			}
			if !entry.RequiresCredential || entry.CredentialEnv != service.ReportReviewMCPCredentialSlot ||
				entry.AuthHeader != "Authorization" || entry.AuthScheme != "Bearer" {
				return errors.New("existing Report Review MCP authentication contract is invalid")
			}
			return nil
		}
	}
	created, err := client.CreateMCPEntry(ctx, model.CreateManagedMCPEntryRequest{
		Slug: service.ReportReviewMCPSlug, Version: service.ReportReviewMCPVersion,
		Name:        "Aida Report Review MCP",
		Description: "Read one frozen report review Context and write one bounded decision.",
		Transport:   "http", URL: mcpURL, RequiresCredential: true,
		CredentialEnv: service.ReportReviewMCPCredentialSlot,
		AuthHeader:    "Authorization", AuthScheme: "Bearer",
	})
	if err != nil {
		return err
	}
	if created == nil || strings.TrimSpace(created.EntryID) == "" {
		return errors.New("Agent platform returned an empty Report Review MCP")
	}
	return nil
}

func ensureAgent(ctx context.Context, client *service.ManagedAgentClient, owner, agentID, modelID string) (int, error) {
	request := model.UpsertManagedAgentRequest{
		AgentID: agentID, Name: "Aida Report Semantic Reviewer",
		Description: "Aida system-only independent semantic reviewer for personal daily reports.",
		Engine:      "claude-code", DefaultModelID: modelID,
		Instructions:        service.ReportReviewAgentInstructions(),
		StartPromptTemplate: service.ReportReviewAgentStartPrompt(),
		CredentialSlots: []model.ManagedCredentialSlot{{
			Name: service.ReportReviewMCPCredentialSlot, Required: true,
		}},
		Skills: []model.ManagedSkillRef{{
			Owner: owner, Slug: service.ReportReviewSkillSlug, Version: service.ReportReviewSkillVersion,
		}},
		MCPBindings: []model.ManagedMCPBinding{{
			Owner: owner, Slug: service.ReportReviewMCPSlug, Version: service.ReportReviewMCPVersion,
			CredentialSlot: service.ReportReviewMCPCredentialSlot,
		}},
		DefaultBindings:  map[string]string{},
		MCPServers:       []model.ManagedMCPServer{},
		ShareModelAccess: boolPointer(true),
	}
	listed, err := client.ListMyAgents(ctx)
	if err != nil {
		return 0, err
	}
	for _, agent := range listed.Agents {
		if agent.AgentID == agentID {
			updated, err := client.UpdateMyAgentWithExplicitPromptFields(ctx, agentID, request)
			if err != nil {
				return 0, err
			}
			return updated.ManagedVersion, nil
		}
	}
	created, err := client.CreateMyAgent(ctx, request)
	if err != nil {
		return 0, err
	}
	verified, err := client.ListMyAgents(ctx)
	if err != nil {
		return 0, err
	}
	for _, agent := range verified.Agents {
		if agent.AgentID == agentID {
			return created.ManagedVersion, nil
		}
	}
	return 0, errors.New("created Report Review Agent was not returned by owner listing")
}

func boolPointer(value bool) *bool {
	return &value
}
