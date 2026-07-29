package reporteval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Runner struct {
	BaseURL      string
	BearerToken  string
	HTTPClient   *http.Client
	PollInterval time.Duration
}

type apiRun struct {
	ID           string         `json:"id"`
	Status       string         `json:"status"`
	InputRef     map[string]any `json:"input_ref_json"`
	ErrorMessage *string        `json:"error_message"`
}

func (runner Runner) Execute(
	ctx context.Context,
	dataset DatasetManifest,
	plan ExecutionPlan,
	onCompleted func(context.Context, EvaluationCase, VariantSpec, *RunReceipt) error,
) ([]RunReceipt, error) {
	if err := dataset.Validate(); err != nil {
		return nil, fmt.Errorf("invalid dataset: %w", err)
	}
	if err := plan.Validate(); err != nil {
		return nil, fmt.Errorf("invalid plan: %w", err)
	}
	if strings.TrimSpace(runner.BaseURL) == "" || strings.TrimSpace(runner.BearerToken) == "" {
		return nil, fmt.Errorf("base URL and bearer token are required")
	}
	if runner.HTTPClient == nil {
		runner.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	if runner.PollInterval <= 0 {
		runner.PollInterval = 2 * time.Second
	}
	receipts := make([]RunReceipt, 0, len(dataset.Cases)*len(plan.Variants)*plan.Repetitions)
	for _, item := range dataset.Cases {
		for _, variant := range plan.Variants {
			for repetition := 1; repetition <= plan.Repetitions; repetition++ {
				run, err := runner.start(ctx, dataset.ReportType, item, variant)
				if err != nil {
					return receipts, fmt.Errorf("start case %s variant %s repetition %d: %w", item.CaseID, variant.VariantVersion, repetition, err)
				}
				terminal, err := runner.wait(ctx, run.ID, time.Duration(plan.TimeoutSeconds)*time.Second)
				if err != nil {
					return receipts, fmt.Errorf("wait case %s variant %s repetition %d: %w", item.CaseID, variant.VariantVersion, repetition, err)
				}
				receipt := RunReceipt{
					CaseID: item.CaseID, VariantVersion: variant.VariantVersion,
					Repetition: repetition, RunID: terminal.ID, Status: terminal.Status,
					FailureStage: stringValue(terminal.InputRef, "failure_stage"),
					ErrorCode:    stringValue(terminal.InputRef, "error_code"),
				}
				if onCompleted != nil {
					if err := onCompleted(ctx, item, variant, &receipt); err != nil {
						return receipts, fmt.Errorf("export run %s: %w", receipt.RunID, err)
					}
				}
				receipts = append(receipts, receipt)
			}
		}
	}
	return receipts, nil
}

func (runner Runner) start(ctx context.Context, reportType string, item EvaluationCase, variant VariantSpec) (apiRun, error) {
	payload := map[string]any{
		"idempotency_key":             uuid.NewString(),
		"report_type":                 reportType,
		"period":                      map[string]string{"date": item.ReportDate},
		"selected_session_slice_keys": item.SelectedSessionSliceKeys,
	}
	if variant.ModelID != "" {
		payload["model_id"] = variant.ModelID
	}
	path := "/api/v1/ai-assets/report-agents/" + url.PathEscape(variant.AgentID) + "/runs"
	var run apiRun
	if err := runner.request(ctx, http.MethodPost, path, payload, &run); err != nil {
		return apiRun{}, err
	}
	if run.ID == "" {
		return apiRun{}, fmt.Errorf("start response is missing run id")
	}
	return run, nil
}

func (runner Runner) wait(ctx context.Context, runID string, timeout time.Duration) (apiRun, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(runner.PollInterval)
	defer ticker.Stop()
	for {
		var run apiRun
		if err := runner.request(ctx, http.MethodGet, "/api/v1/ai-assets/agent-runs/"+url.PathEscape(runID), nil, &run); err != nil {
			return apiRun{}, err
		}
		if run.Status == "succeeded" || run.Status == "failed" || run.Status == "timeout" {
			return run, nil
		}
		select {
		case <-ctx.Done():
			return apiRun{}, ctx.Err()
		case <-deadline.C:
			return apiRun{}, fmt.Errorf("run %s did not finish within %s", runID, timeout)
		case <-ticker.C:
		}
	}
}

func (runner Runner) request(ctx context.Context, method, path string, input any, output any) error {
	var body io.Reader
	if input != nil {
		payload, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(runner.BaseURL, "/")+path, body)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+runner.BearerToken)
	request.Header.Set("Accept", "application/json")
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := runner.HTTPClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(payload)))
	}
	if err := json.Unmarshal(payload, output); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func stringValue(values map[string]any, key string) string {
	if value, ok := values[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}
