package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

var ErrModelCatalogNotConfigured = errors.New("model catalog is not configured")

type ModelCatalogUpstreamError struct {
	StatusCode int
}

func (e *ModelCatalogUpstreamError) Error() string {
	return fmt.Sprintf("model catalog request failed with status %d", e.StatusCode)
}

type ModelCatalogClient struct {
	endpoint   string
	httpClient *http.Client
}

type modelCatalogResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	} `json:"data"`
}

func NewModelCatalogClient(endpoint string) *ModelCatalogClient {
	return &ModelCatalogClient{
		endpoint: strings.TrimSpace(endpoint),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *ModelCatalogClient) Configured() bool {
	return c != nil && c.endpoint != ""
}

func (c *ModelCatalogClient) ListAvailableModels(ctx context.Context, token string) ([]string, error) {
	if !c.Configured() {
		return nil, ErrModelCatalogNotConfigured
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("model catalog token is missing")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request model catalog: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, &ModelCatalogUpstreamError{StatusCode: resp.StatusCode}
	}

	var payload modelCatalogResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode model catalog response: %w", err)
	}
	if payload.Code != 0 {
		return nil, &ModelCatalogUpstreamError{StatusCode: http.StatusBadGateway}
	}

	seen := make(map[string]struct{}, len(payload.Data.Models))
	models := make([]string, 0, len(payload.Data.Models))
	for _, item := range payload.Data.Models {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		models = append(models, name)
	}
	sort.Strings(models)
	return models, nil
}
