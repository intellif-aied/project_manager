package handler

import (
	"errors"
	"net/http"

	"github.com/aidashboard/api/service"
)

type ModelCatalogHandler struct {
	client *service.ModelCatalogClient
}

func NewModelCatalogHandler(client *service.ModelCatalogClient) *ModelCatalogHandler {
	return &ModelCatalogHandler{client: client}
}

func (h *ModelCatalogHandler) List(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	user := getUser(r)
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	models, err := h.client.ListAvailableModels(r.Context(), bearerTokenFromRequest(r))
	if err == nil {
		writeJSON(w, http.StatusOK, map[string]any{"models": models})
		return
	}
	if errors.Is(err, service.ErrModelCatalogNotConfigured) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"code":  "MODEL_CATALOG_NOT_CONFIGURED",
			"error": "模型目录服务未配置",
		})
		return
	}
	var upstreamErr *service.ModelCatalogUpstreamError
	if errors.As(err, &upstreamErr) && (upstreamErr.StatusCode == http.StatusUnauthorized || upstreamErr.StatusCode == http.StatusForbidden) {
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"code":  "MODEL_CATALOG_AUTH_FAILED",
			"error": "模型服务未能验证当前用户身份",
		})
		return
	}
	writeJSON(w, http.StatusBadGateway, map[string]string{
		"code":  "MODEL_CATALOG_UNAVAILABLE",
		"error": "模型列表暂时不可用",
	})
}
