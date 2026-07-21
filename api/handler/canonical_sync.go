package handler

import (
	"errors"
	"net/http"

	"github.com/aidashboard/api/internal/canonicalsync"
)

type CanonicalSyncHandler struct {
	preparer canonicalsync.Preparer
}

func NewCanonicalSyncHandler(preparer canonicalsync.Preparer) *CanonicalSyncHandler {
	return &CanonicalSyncHandler{preparer: preparer}
}

func (handler *CanonicalSyncHandler) Prepare(w http.ResponseWriter, r *http.Request) {
	user := getUser(r)
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if handler == nil || handler.preparer == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "CANONICAL_SYNC_UNAVAILABLE", "error": "canonical sync is unavailable"})
		return
	}
	var request canonicalsync.PrepareRequest
	if err := decodeLimitedJSON(w, r, &request); err != nil {
		return
	}
	if err := canonicalsync.ValidatePrepare(request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_CANONICAL_PREPARE", "error": err.Error()})
		return
	}
	results, err := handler.preparer.PrepareFamily(r.Context(), user.ID, request)
	if err != nil {
		status := http.StatusInternalServerError
		code := "CANONICAL_PREPARE_FAILED"
		if errors.Is(err, canonicalsync.ErrInvalidRequest) {
			status = http.StatusBadRequest
			code = "INVALID_CANONICAL_PREPARE"
		}
		writeJSON(w, status, map[string]string{"code": code, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}
