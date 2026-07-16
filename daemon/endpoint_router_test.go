package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolveAPIEndpointPrefersAuthenticatedInternalRoute(t *testing.T) {
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/me" || r.Header.Get("Authorization") != "Bearer token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"42"}`))
	}))
	defer internal.Close()
	cfg := &Config{APIURL: "http://public.invalid/api/v1", InternalAPIURL: internal.URL, AutoRoute: true, Token: "token"}
	resolveAPIEndpoint(cfg)
	if apiBaseURL(cfg) != internal.URL || cfg.ActiveRoute != "internal" {
		t.Fatalf("url=%s route=%s", apiBaseURL(cfg), cfg.ActiveRoute)
	}
}

func TestResolveAPIEndpointFallsBackAndHonorsOverride(t *testing.T) {
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not this production environment", http.StatusUnauthorized)
	}))
	defer internal.Close()
	cfg := &Config{APIURL: "http://public.example/api/v1", InternalAPIURL: internal.URL, AutoRoute: true, Token: "token"}
	resolveAPIEndpoint(cfg)
	if apiBaseURL(cfg) != cfg.APIURL || cfg.ActiveRoute != "public" {
		t.Fatalf("url=%s route=%s", apiBaseURL(cfg), cfg.ActiveRoute)
	}
	cfg.apiURLOverridden = true
	resolveAPIEndpoint(cfg)
	if cfg.ActiveRoute != "override" {
		t.Fatalf("route=%s", cfg.ActiveRoute)
	}
}
