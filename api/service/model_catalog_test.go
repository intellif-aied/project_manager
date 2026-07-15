package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestModelCatalogClientForwardsUserTokenAndNormalizesModels(t *testing.T) {
	const token = "user-aihub-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/models" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			t.Fatalf("authorization = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"models": []map[string]any{
					{"name": " qwen3.2 ", "usage": map[string]any{"remain": "must-not-leak"}},
					{"name": "MiniMax-M2.5"},
					{"name": "qwen3.2"},
					{"name": ""},
				},
			},
		})
	}))
	defer server.Close()

	models, err := NewModelCatalogClient(server.URL+"/api/v2/models").ListAvailableModels(context.Background(), token)
	if err != nil {
		t.Fatalf("ListAvailableModels failed: %v", err)
	}
	want := []string{"MiniMax-M2.5", "qwen3.2"}
	if !reflect.DeepEqual(models, want) {
		t.Fatalf("models = %#v, want %#v", models, want)
	}
}

func TestModelCatalogClientRequiresConfiguration(t *testing.T) {
	_, err := NewModelCatalogClient("").ListAvailableModels(context.Background(), "token")
	if !errors.Is(err, ErrModelCatalogNotConfigured) {
		t.Fatalf("error = %v", err)
	}
}

func TestModelCatalogClientReturnsSanitizedUpstreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `token=secret upstream details`, http.StatusUnauthorized)
	}))
	defer server.Close()

	_, err := NewModelCatalogClient(server.URL).ListAvailableModels(context.Background(), "token")
	var upstreamErr *ModelCatalogUpstreamError
	if !errors.As(err, &upstreamErr) || upstreamErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("error = %v", err)
	}
	if got := err.Error(); got != "model catalog request failed with status 401" {
		t.Fatalf("error leaked upstream response: %q", got)
	}
}
