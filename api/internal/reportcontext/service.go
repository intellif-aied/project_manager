package reportcontext

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/aidashboard/api/internal/reportsource"
)

const SchemaVersion = "report-context/v1"

var (
	ErrInvalidRequest = errors.New("invalid report context request")
	ErrNotFound       = errors.New("report context not found")
	ErrIncomplete     = errors.New("report context source is incomplete")
)

type sourceReader interface {
	ReadAttachedSelection(ctx context.Context, userID, selectionID, runID, reportType string, period reportsource.Period, pageCursor string) (reportsource.ContentPage, error)
}

type Service struct {
	db     *sql.DB
	source sourceReader
}

func NewService(db *sql.DB, source *reportsource.Service) *Service {
	return &Service{db: db, source: source}
}

type Run struct {
	ID         string              `json:"run_id"`
	ReportType string              `json:"report_type"`
	Period     reportsource.Period `json:"period"`
	Target     any                 `json:"target"`
}

type SourceState struct {
	Mode             string `json:"mode"`
	CoverageComplete bool   `json:"coverage_complete"`
}

type Sources struct {
	SessionDigest json.RawMessage `json:"session_digest"`
}

type Payload struct {
	SchemaVersion string      `json:"schema_version"`
	Run           Run         `json:"run"`
	SourceState   SourceState `json:"source_state"`
	Sources       Sources     `json:"sources"`
}

type StoredContext struct {
	Payload []byte
	Hash    string
	Bytes   int
}

func (s *Service) BuildPersonal(ctx context.Context, userID, runID, selectionID, reportType string, period reportsource.Period, target any) (StoredContext, error) {
	if s == nil || s.db == nil || s.source == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(runID) == "" || strings.TrimSpace(selectionID) == "" {
		return StoredContext{}, ErrInvalidRequest
	}
	page, err := s.source.ReadAttachedSelection(ctx, userID, selectionID, runID, reportType, period, "")
	if err != nil {
		return StoredContext{}, err
	}
	digest, sourceMode, err := normalizeFrozenPayload(page.FrozenPayload)
	if err != nil {
		return StoredContext{}, err
	}
	payload, err := json.Marshal(Payload{
		SchemaVersion: SchemaVersion,
		Run:           Run{ID: runID, ReportType: reportType, Period: period, Target: target},
		SourceState:   SourceState{Mode: sourceMode, CoverageComplete: true},
		Sources:       Sources{SessionDigest: digest},
	})
	if err != nil {
		return StoredContext{}, err
	}
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO report_run_contexts (
			run_id, schema_version, source_selection_id, context_hash, context_payload, context_bytes
		) VALUES ($1, $2, $3, $4, $5::jsonb, $6)`,
		runID, SchemaVersion, selectionID, hash, payload, len(payload)); err != nil {
		return StoredContext{}, fmt.Errorf("store report context: %w", err)
	}
	return StoredContext{Payload: payload, Hash: hash, Bytes: len(payload)}, nil
}

func (s *Service) Get(ctx context.Context, userID, runID string) (StoredContext, error) {
	if s == nil || s.db == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(runID) == "" {
		return StoredContext{}, ErrInvalidRequest
	}
	var payload []byte
	var hash string
	var size int
	err := s.db.QueryRowContext(ctx, `
		SELECT c.context_payload, c.context_hash, c.context_bytes
		FROM report_run_contexts c
		JOIN ai_runs r ON r.id = c.run_id
		WHERE c.run_id = $1 AND r.user_id = $2 AND r.business_type = 'report_agent_run'`,
		runID, userID).Scan(&payload, &hash, &size)
	if errors.Is(err, sql.ErrNoRows) {
		return StoredContext{}, ErrNotFound
	}
	if err != nil {
		return StoredContext{}, err
	}
	if normalized, err := normalizeJSON(payload); err != nil || len(normalized) == 0 {
		return StoredContext{}, ErrIncomplete
	}
	return StoredContext{Payload: payload, Hash: hash, Bytes: size}, nil
}

func normalizeFrozenPayload(raw json.RawMessage) (json.RawMessage, string, error) {
	normalized, err := normalizeJSON(raw)
	if err != nil || len(normalized) == 0 || bytes.Equal(normalized, []byte("null")) {
		return nil, "", ErrIncomplete
	}
	var object map[string]any
	if err := json.Unmarshal(normalized, &object); err != nil || len(object) == 0 {
		return nil, "", ErrIncomplete
	}
	mode, _ := object["content_mode"].(string)
	if mode != reportsource.ReadModeDigestV1 && mode != reportsource.ReadModeDigestV2 {
		return nil, "", ErrIncomplete
	}
	return json.RawMessage(normalized), mode, nil
}

func normalizeJSON(raw []byte) ([]byte, error) {
	var buffer bytes.Buffer
	if err := json.Compact(&buffer, raw); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
