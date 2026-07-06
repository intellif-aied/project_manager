package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aidashboard/api/model"
)

type WorkItemEventRecorder struct {
	db *sql.DB
}

type WorkItemEventInput struct {
	TargetType    string
	TargetID      string
	RequirementID string
	TaskID        string
	Actor         *model.User
	EventType     string
	EventTitle    string
	BeforeData    map[string]any
	AfterData     map[string]any
	Metadata      map[string]any
}

func NewWorkItemEventRecorder(db *sql.DB) *WorkItemEventRecorder {
	return &WorkItemEventRecorder{db: db}
}

func (r *WorkItemEventRecorder) Record(ctx context.Context, input WorkItemEventInput) error {
	if r == nil || r.db == nil {
		return nil
	}
	beforeJSON, err := marshalEventJSON(input.BeforeData)
	if err != nil {
		return err
	}
	afterJSON, err := marshalEventJSON(input.AfterData)
	if err != nil {
		return err
	}
	metadataJSON, err := marshalEventJSON(input.Metadata)
	if err != nil {
		return err
	}

	actorID := sql.NullInt64{}
	actorName := ""
	actorRole := ""
	if input.Actor != nil {
		actorName = displayUserName(input.Actor)
		actorRole = input.Actor.Role
		if input.Actor.ID != "" {
			var parsed int64
			if _, scanErr := fmt.Sscan(input.Actor.ID, &parsed); scanErr == nil {
				actorID = sql.NullInt64{Int64: parsed, Valid: true}
			}
		}
	}

	_, err = r.db.ExecContext(ctx, `
		INSERT INTO work_item_events (
			target_type, target_id, requirement_id, task_id,
			actor_id, actor_name, actor_role,
			event_type, event_title, before_data, after_data, metadata
		)
		VALUES ($1, $2, NULLIF($3, '')::uuid, NULLIF($4, '')::uuid,
			$5, $6, $7, $8, $9, $10::jsonb, $11::jsonb, $12::jsonb)`,
		input.TargetType,
		input.TargetID,
		input.RequirementID,
		input.TaskID,
		actorID,
		actorName,
		actorRole,
		input.EventType,
		input.EventTitle,
		string(beforeJSON),
		string(afterJSON),
		string(metadataJSON),
	)
	return err
}

func marshalEventJSON(value map[string]any) ([]byte, error) {
	if value == nil {
		value = map[string]any{}
	}
	return json.Marshal(value)
}

func displayUserName(u *model.User) string {
	if u == nil {
		return ""
	}
	for _, value := range []string{u.Name, u.Nickname, u.Username, u.ID} {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
