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

type apiRun struct {
	ID           string         `json:"id"`
	Status       string         `json:"status"`
	InputRef     map[string]any `json:"input_ref_json"`
	ErrorMessage *string        `json:"error_message"`
}

type Runtime interface {
	Attest(context.Context) (RuntimeAttestation, error)
	Start(context.Context, string, EvaluationCase, VariantSpec) (apiRun, error)
	Wait(context.Context, string, time.Duration) (apiRun, error)
	Artifacts(context.Context, string) (RunArtifactEnvelope, error)
}

type RuntimeFactory func(VariantSpec, EvaluationCase) (Runtime, error)

type Runner struct {
	RuntimeFactory RuntimeFactory
}

func (runner Runner) Execute(
	ctx context.Context,
	dataset FrozenDataset,
	plan ExecutionPlan,
	onCompleted func(context.Context, EvaluationCase, VariantSpec, *RunReceipt, RunArtifactEnvelope) error,
) ([]RunReceipt, error) {
	if err := dataset.Manifest.Validate(); err != nil {
		return nil, fmt.Errorf("invalid frozen dataset: %w", err)
	}
	if len(dataset.Sources) != len(dataset.Manifest.Cases) {
		return nil, fmt.Errorf("invalid frozen dataset: source evidence is incomplete")
	}
	if err := plan.ValidateForDataset(dataset.Manifest); err != nil {
		return nil, fmt.Errorf("invalid plan: %w", err)
	}
	if runner.RuntimeFactory == nil {
		return nil, fmt.Errorf("runtime factory is required")
	}

	// Preflight every runtime before creating the first report run. A plan that
	// accidentally points one variant at production therefore has no side effect.
	runtimes := make(map[string]map[string]Runtime, len(plan.Variants))
	attestations := make(map[string]map[string]RuntimeAttestation, len(plan.Variants))
	for _, variant := range plan.Variants {
		runtimes[variant.VariantVersion] = make(map[string]Runtime, len(dataset.Manifest.Cases))
		attestations[variant.VariantVersion] = make(map[string]RuntimeAttestation, len(dataset.Manifest.Cases))
		for _, item := range dataset.Manifest.Cases {
			runtime, err := runner.RuntimeFactory(variant, item)
			if err != nil {
				return nil, fmt.Errorf("create runtime for case %s variant %s: %w", item.CaseID, variant.VariantVersion, err)
			}
			attestation, err := runtime.Attest(ctx)
			if err != nil {
				return nil, fmt.Errorf("attest runtime for case %s variant %s: %w", item.CaseID, variant.VariantVersion, err)
			}
			if err := attestation.Validate(); err != nil {
				return nil, fmt.Errorf("reject runtime for case %s variant %s: %w", item.CaseID, variant.VariantVersion, err)
			}
			runtimes[variant.VariantVersion][item.CaseID] = runtime
			attestations[variant.VariantVersion][item.CaseID] = attestation
		}
	}

	receipts := make([]RunReceipt, 0, len(dataset.Manifest.Cases)*len(plan.Variants)*plan.Repetitions)
	for _, item := range dataset.Manifest.Cases {
		for _, variant := range plan.Variants {
			runtime := runtimes[variant.VariantVersion][item.CaseID]
			for repetition := 1; repetition <= plan.Repetitions; repetition++ {
				run, err := runtime.Start(ctx, dataset.Manifest.ReportType, item, variant)
				if err != nil {
					return receipts, fmt.Errorf("start case %s variant %s repetition %d: %w", item.CaseID, variant.VariantVersion, repetition, err)
				}
				terminal, err := runtime.Wait(ctx, run.ID, time.Duration(plan.TimeoutSeconds)*time.Second)
				if err != nil {
					return receipts, fmt.Errorf("wait case %s variant %s repetition %d: %w", item.CaseID, variant.VariantVersion, repetition, err)
				}
				artifacts, err := runtime.Artifacts(ctx, terminal.ID)
				if err != nil {
					return receipts, fmt.Errorf("read artifacts for run %s: %w", terminal.ID, err)
				}
				receipt := RunReceipt{
					CaseID: item.CaseID, VariantVersion: variant.VariantVersion,
					Repetition: repetition, RunID: terminal.ID, Status: terminal.Status,
					FailureStage: stringValue(terminal.InputRef, "failure_stage"),
					ErrorCode:    stringValue(terminal.InputRef, "error_code"),
					Runtime:      attestations[variant.VariantVersion][item.CaseID],
				}
				if onCompleted != nil {
					if err := onCompleted(ctx, item, variant, &receipt, artifacts); err != nil {
						return receipts, fmt.Errorf("export run %s: %w", receipt.RunID, err)
					}
				}
				receipts = append(receipts, receipt)
			}
		}
	}
	return receipts, nil
}

type HTTPRuntime struct {
	BaseURL        string
	BearerToken    string
	HTTPClient     *http.Client
	RequestTimeout time.Duration
	PollInterval   time.Duration
}

func (runtime *HTTPRuntime) FreezeSource(
	ctx context.Context,
	reportDate string,
	selectedSessionSliceKeys []string,
) (SourceEvidence, error) {
	if strings.TrimSpace(reportDate) == "" || len(selectedSessionSliceKeys) == 0 {
		return SourceEvidence{}, fmt.Errorf("report date and selected session slice keys are required")
	}
	payload := map[string]any{
		"report_type": "personal_daily", "report_date": reportDate,
		"selected_session_slice_keys": selectedSessionSliceKeys,
	}
	var source SourceEvidence
	if err := runtime.request(ctx, http.MethodPost, "/api/v1/evaluation/sources/freeze", payload, &source); err != nil {
		return SourceEvidence{}, err
	}
	if err := source.Validate(); err != nil {
		return SourceEvidence{}, err
	}
	return source, nil
}

func NewHTTPRuntimeFactory(
	lookupEnvironment func(string) string,
	httpClient *http.Client,
	pollInterval time.Duration,
) RuntimeFactory {
	return func(variant VariantSpec, item EvaluationCase) (Runtime, error) {
		if lookupEnvironment == nil {
			return nil, fmt.Errorf("environment lookup is required")
		}
		tokenEnv, err := variant.Runtime.TokenEnvironment(item.CaseID)
		if err != nil {
			return nil, err
		}
		token := strings.TrimSpace(lookupEnvironment(tokenEnv))
		if token == "" {
			return nil, fmt.Errorf("token environment variable %s is empty", tokenEnv)
		}
		return &HTTPRuntime{
			BaseURL: strings.TrimRight(variant.Runtime.BaseURL, "/"), BearerToken: token,
			HTTPClient: httpClient, PollInterval: pollInterval,
		}, nil
	}
}

func (runtime *HTTPRuntime) Attest(ctx context.Context) (RuntimeAttestation, error) {
	var attestation RuntimeAttestation
	if err := runtime.request(ctx, http.MethodGet, "/api/v1/evaluation/runtime", nil, &attestation); err != nil {
		return RuntimeAttestation{}, err
	}
	return attestation, nil
}

func (runtime *HTTPRuntime) Start(ctx context.Context, reportType string, item EvaluationCase, variant VariantSpec) (apiRun, error) {
	payload := map[string]any{
		"idempotency_key": uuid.NewString(), "report_type": reportType,
		"period":                      map[string]string{"date": item.ReportDate},
		"selected_session_slice_keys": item.SelectedSessionSliceKeys,
	}
	if variant.ModelID != "" {
		payload["model_id"] = variant.ModelID
	}
	path := "/api/v1/ai-assets/report-agents/" + url.PathEscape(variant.AgentID) + "/runs"
	var run apiRun
	if err := runtime.request(ctx, http.MethodPost, path, payload, &run); err != nil {
		return apiRun{}, err
	}
	if strings.TrimSpace(run.ID) == "" {
		return apiRun{}, fmt.Errorf("start response is missing run id")
	}
	return run, nil
}

func (runtime *HTTPRuntime) Wait(ctx context.Context, runID string, timeout time.Duration) (apiRun, error) {
	interval := runtime.PollInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		var run apiRun
		if err := runtime.request(ctx, http.MethodGet, "/api/v1/ai-assets/agent-runs/"+url.PathEscape(runID), nil, &run); err != nil {
			return apiRun{}, err
		}
		if terminalRunStatus(run.Status) {
			return run, nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return apiRun{}, ctx.Err()
		case <-deadline.C:
			timer.Stop()
			return apiRun{}, fmt.Errorf("run %s did not finish within %s", runID, timeout)
		case <-timer.C:
		}
	}
}

func (runtime *HTTPRuntime) Artifacts(ctx context.Context, runID string) (RunArtifactEnvelope, error) {
	var artifacts RunArtifactEnvelope
	if err := runtime.request(ctx, http.MethodGet, "/api/v1/evaluation/runs/"+url.PathEscape(runID)+"/artifacts", nil, &artifacts); err != nil {
		return RunArtifactEnvelope{}, err
	}
	return artifacts, nil
}

func (runtime *HTTPRuntime) request(ctx context.Context, method, path string, input any, output any) error {
	var body io.Reader
	if input != nil {
		payload, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(ctx, method, runtime.BaseURL+path, body)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+runtime.BearerToken)
	request.Header.Set("Accept", "application/json")
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	client := runtime.HTTPClient
	if client == nil {
		timeout := runtime.RequestTimeout
		if timeout <= 0 {
			timeout = 30 * time.Second
		}
		client = &http.Client{Timeout: timeout}
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 64<<20))
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
