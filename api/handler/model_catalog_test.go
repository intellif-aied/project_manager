package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aidashboard/api/model"
	"github.com/aidashboard/api/service"
)

func TestModelCatalogHandlerUsesAuthenticatedUserAndToken(t *testing.T) {
	const token = "current-user-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/models" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			t.Fatalf("authorization = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"models": []map[string]any{{"name": "qwen3.2"}}},
		})
	}))
	defer server.Close()

	handler := NewModelCatalogHandler(service.NewModelCatalogClient(server.URL + "/api/v2/models"))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai-assets/models", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req = req.WithContext(context.WithValue(req.Context(), userKey, &model.User{ID: "304"}))
	res := httptest.NewRecorder()

	handler.List(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	if got := res.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("cache-control = %q", got)
	}
	var payload struct {
		Models []string `json:"models"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Models) != 1 || payload.Models[0] != "qwen3.2" {
		t.Fatalf("models = %#v", payload.Models)
	}
}

func TestModelCatalogHandlerDoesNotReturnUpstreamUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	handler := NewModelCatalogHandler(service.NewModelCatalogClient(server.URL))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai-assets/models", nil)
	req.Header.Set("Authorization", "Bearer user-token")
	req = req.WithContext(context.WithValue(req.Context(), userKey, &model.User{ID: "304"}))
	res := httptest.NewRecorder()

	handler.List(res, req)
	if res.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	if got := res.Body.String(); got != "{\"code\":\"MODEL_CATALOG_AUTH_FAILED\",\"error\":\"模型服务未能验证当前用户身份\"}\n" {
		t.Fatalf("body = %q", got)
	}
}
