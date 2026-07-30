package handler

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/aidashboard/api/internal/reporteval"
	"github.com/aidashboard/api/internal/reportsource"
	"github.com/go-chi/chi/v5"
)

type evaluationSourceCreator interface {
	CreateExplicit(context.Context, string, string, reportsource.Period, []reportsource.SourceInput) (reportsource.Selection, error)
}

type evaluationEvidenceFreezer interface {
	Freeze(context.Context, reportsource.Selection) (reporteval.SourceEvidence, error)
}

type evaluationArtifactLoader interface {
	Load(context.Context, string, string) (reporteval.RunArtifactEnvelope, error)
}

type ReportEvaluationHandler struct {
	attestation reporteval.RuntimeAttestation
	sources     evaluationSourceCreator
	freezer     evaluationEvidenceFreezer
	artifacts   evaluationArtifactLoader
}

func NewReportEvaluationHandler(
	environment, buildRevision, instanceID string,
	sources evaluationSourceCreator,
	freezer evaluationEvidenceFreezer,
	artifacts evaluationArtifactLoader,
) (*ReportEvaluationHandler, error) {
	attestation := reporteval.RuntimeAttestation{
		SchemaVersion: reporteval.RuntimeAttestationVersion, Enabled: true,
		Environment: strings.TrimSpace(environment), BuildRevision: strings.TrimSpace(buildRevision),
		InstanceID: strings.TrimSpace(instanceID),
	}
	if err := attestation.Validate(); err != nil {
		return nil, err
	}
	if sources == nil || freezer == nil || artifacts == nil {
		return nil, errors.New("evaluation sources, freezer, and artifacts are required")
	}
	return &ReportEvaluationHandler{attestation: attestation, sources: sources, freezer: freezer, artifacts: artifacts}, nil
}

func (handler *ReportEvaluationHandler) Runtime(w http.ResponseWriter, r *http.Request) {
	if getUser(r) == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	writeJSON(w, http.StatusOK, handler.attestation)
}

func (handler *ReportEvaluationHandler) FreezeSource(w http.ResponseWriter, r *http.Request) {
	user := getUser(r)
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var request struct {
		ReportType string   `json:"report_type"`
		ReportDate string   `json:"report_date"`
		SliceKeys  []string `json:"selected_session_slice_keys"`
	}
	if err := readJSON(r, &request); err != nil || strings.TrimSpace(request.ReportType) != "personal_daily" || len(request.SliceKeys) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_EVALUATION_SOURCE", "error": "personal daily report source is required"})
		return
	}
	period, err := reportsource.ReportPeriod("personal_daily", strings.TrimSpace(request.ReportDate), "", "")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_EVALUATION_SOURCE", "error": err.Error()})
		return
	}
	inputs := make([]reportsource.SourceInput, 0, len(request.SliceKeys))
	for _, sliceKey := range request.SliceKeys {
		sliceKey = strings.TrimSpace(sliceKey)
		if !isValidUUID(sliceKey) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_EVALUATION_SOURCE", "error": "invalid slice key"})
			return
		}
		inputs = append(inputs, reportsource.SourceInput{SliceKey: sliceKey})
	}
	selection, err := handler.sources.CreateExplicit(r.Context(), user.ID, "personal_daily", period, inputs)
	if err != nil {
		writeReportSourceError(w, err)
		return
	}
	source, err := handler.freezer.Freeze(r.Context(), selection)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "EVALUATION_SOURCE_FREEZE_FAILED", "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, source)
}

func (handler *ReportEvaluationHandler) RunArtifacts(w http.ResponseWriter, r *http.Request) {
	user := getUser(r)
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	runID := strings.TrimSpace(chi.URLParam(r, "runId"))
	if !isValidUUID(runID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_EVALUATION_RUN", "error": "invalid run id"})
		return
	}
	artifacts, err := handler.artifacts.Load(r.Context(), user.ID, runID)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"code": "EVALUATION_RUN_NOT_FOUND", "error": "run not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "EVALUATION_ARTIFACTS_FAILED", "error": "run artifacts are unavailable"})
		return
	}
	if !reporteval.IsTerminalRunStatus(artifacts.Status) {
		writeJSON(w, http.StatusConflict, map[string]string{"code": "EVALUATION_RUN_NOT_TERMINAL", "error": "run has not finished"})
		return
	}
	if err := artifacts.Validate(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "EVALUATION_ARTIFACTS_INVALID", "error": "run artifacts are incomplete"})
		return
	}
	writeJSON(w, http.StatusOK, artifacts)
}
