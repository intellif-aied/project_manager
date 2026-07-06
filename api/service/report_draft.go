package service

import (
	"fmt"

	"github.com/aidashboard/api/internal/biztime"
	"github.com/aidashboard/api/model"
)

const DefaultDailyReportSkillID = "default_daily"
const DefaultDailyReportSkillName = "默认日报 Skill"

func ValidateDraftSkillID(skillID string) error {
	if skillID == "" || skillID == DefaultDailyReportSkillID {
		return nil
	}
	return fmt.Errorf("unsupported skill_id: %s", skillID)
}

func NormalizeDraftResponse(
	resp model.GenerateReportDraftResponse,
	sessions []model.ReportDraftSession,
	tasks []model.ReportDraftTaskCandidate,
	includeTaskProgress bool,
) model.GenerateReportDraftResponse {
	selectedIDs := make([]string, 0, len(sessions))
	sessionTitleByID := make(map[string]string, len(sessions))
	for _, s := range sessions {
		selectedIDs = append(selectedIDs, s.ID)
		sessionTitleByID[s.ID] = ReportDraftSessionTitle(s)
	}

	resp.SelectedSessionIDs = selectedIDs
	if resp.SkillName == "" {
		resp.SkillName = DefaultDailyReportSkillName
	}
	if !includeTaskProgress {
		resp.TaskProgressSuggestions = []model.TaskProgressSuggestion{}
		return resp
	}

	taskByID := make(map[string]model.ReportDraftTaskCandidate, len(tasks))
	for _, task := range tasks {
		taskByID[task.TaskID] = task
	}

	normalized := make([]model.TaskProgressSuggestion, 0, len(resp.TaskProgressSuggestions))
	for _, suggestion := range resp.TaskProgressSuggestions {
		task, ok := taskByID[suggestion.TaskID]
		if !ok {
			continue
		}
		if !isStoredTaskStatusForDraft(suggestion.SuggestedStatus) {
			continue
		}

		evidenceIDs := make([]string, 0, len(suggestion.EvidenceSessionIDs))
		evidenceTitles := make([]string, 0, len(suggestion.EvidenceSessionIDs))
		seen := map[string]bool{}
		for _, sessionID := range suggestion.EvidenceSessionIDs {
			title, ok := sessionTitleByID[sessionID]
			if !ok || seen[sessionID] {
				continue
			}
			seen[sessionID] = true
			evidenceIDs = append(evidenceIDs, sessionID)
			evidenceTitles = append(evidenceTitles, title)
		}
		if len(evidenceIDs) == 0 {
			continue
		}

		suggestion.TaskTitle = task.TaskTitle
		suggestion.RequirementID = task.RequirementID
		suggestion.RequirementTitle = task.RequirementTitle
		suggestion.SuggestedProgress = clampProgress(suggestion.SuggestedProgress)
		suggestion.EvidenceSessionIDs = evidenceIDs
		suggestion.EvidenceSessionTitles = evidenceTitles
		normalized = append(normalized, suggestion)
	}
	resp.TaskProgressSuggestions = normalized
	return resp
}

func ReportDraftSessionTitle(s model.ReportDraftSession) string {
	start := s.StartedAt.Format("15:04")
	end := ""
	if s.EndedAt != nil {
		end = " - " + s.EndedAt.Format("15:04")
	}
	agent := s.AgentType
	if agent == "" {
		agent = "session"
	}
	if start == "00:00" && s.StartedAt.IsZero() {
		return agent
	}
	return fmt.Sprintf("%s %s%s", agent, start, end)
}

func TodayInLocalDate() string {
	return biztime.Today()
}

func clampProgress(progress int) int {
	if progress < 0 {
		return 0
	}
	if progress > 100 {
		return 100
	}
	return progress
}

func isStoredTaskStatusForDraft(status string) bool {
	return status == "todo" || status == "in_progress" || status == "done"
}
