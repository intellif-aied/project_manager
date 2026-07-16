package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

const internalRouteProbeTimeout = 1200 * time.Millisecond

func apiBaseURL(cfg *Config) string {
	if cfg == nil {
		return ""
	}
	if value := strings.TrimRight(strings.TrimSpace(cfg.ActiveAPIURL), "/"); value != "" {
		return value
	}
	return strings.TrimRight(strings.TrimSpace(cfg.APIURL), "/")
}

func resolveAPIEndpoint(cfg *Config) {
	if cfg == nil {
		return
	}
	cfg.ActiveAPIURL = strings.TrimRight(strings.TrimSpace(cfg.APIURL), "/")
	cfg.ActiveRoute = "public"
	internalURL := strings.TrimRight(strings.TrimSpace(cfg.InternalAPIURL), "/")
	if cfg.apiURLOverridden || !cfg.AutoRoute || internalURL == "" || strings.TrimSpace(cfg.Token) == "" {
		if cfg.apiURLOverridden {
			cfg.ActiveRoute = "override"
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), internalRouteProbeTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, internalURL+"/auth/me", nil)
	if err != nil {
		return
	}
	request.Header.Set("Authorization", "Bearer "+cfg.Token)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	response, err := (&http.Client{Transport: transport, Timeout: internalRouteProbeTimeout}).Do(request)
	if err != nil {
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return
	}
	var identity map[string]any
	if json.NewDecoder(response.Body).Decode(&identity) != nil || len(identity) == 0 {
		return
	}
	cfg.ActiveAPIURL = internalURL
	cfg.ActiveRoute = "internal"
}
