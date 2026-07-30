package reporteval

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRunnerUsesEachVariantRuntimeAndAttestsBeforeStarting(t *testing.T) {
	dataset, err := LoadFrozenDataset(writeFrozenDatasetFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	plan := validPlan()
	var mutex sync.Mutex
	started := []string{}
	servers := make([]*httptest.Server, 0, 2)
	for index, variant := range plan.Variants {
		index, variant := index, variant
		runID := fmt.Sprintf("33333333-3333-4333-8333-%012d", index+1)
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			if request.Header.Get("Authorization") != "Bearer token-"+variant.VariantVersion {
				t.Fatalf("unexpected bearer token for %s", variant.VariantVersion)
			}
			switch {
			case request.URL.Path == "/api/v1/evaluation/runtime":
				_ = json.NewEncoder(response).Encode(RuntimeAttestation{
					SchemaVersion: RuntimeAttestationVersion, Enabled: true, Environment: "test",
					BuildRevision: "revision-" + variant.VariantVersion, InstanceID: "instance-" + variant.VariantVersion,
				})
			case request.Method == http.MethodPost:
				mutex.Lock()
				started = append(started, variant.VariantVersion)
				mutex.Unlock()
				_ = json.NewEncoder(response).Encode(apiRun{ID: runID, Status: "pending"})
			case strings.HasSuffix(request.URL.Path, "/artifacts"):
				manifest := json.RawMessage(`{"pipeline_profile":"digest_context_brief_final","stages":[]}`)
				var value any
				_ = json.Unmarshal(manifest, &value)
				hash, _ := CanonicalSHA256(value)
				_ = json.NewEncoder(response).Encode(RunArtifactEnvelope{
					SchemaVersion: RunArtifactsSchemaVersion, RunID: runID, Status: "succeeded", CreatedAt: time.Now().UTC(),
					SourceIdentitySHA256: dataset.Sources["case-001"].SourceIdentitySHA256,
					VariantManifest:      manifest, VariantSHA256: hash,
				})
			default:
				_ = json.NewEncoder(response).Encode(apiRun{ID: runID, Status: "succeeded"})
			}
		}))
		servers = append(servers, server)
		plan.Variants[index].Runtime.BaseURL = server.URL
	}
	defer func() {
		for _, server := range servers {
			server.Close()
		}
	}()
	tokens := map[string]string{
		"AIDA_EVAL_BASELINE_TOKEN": "token-baseline", "AIDA_EVAL_CANDIDATE_TOKEN": "token-candidate",
	}
	runner := Runner{RuntimeFactory: NewHTTPRuntimeFactory(func(name string) string { return tokens[name] }, nil, time.Millisecond)}
	var exported []string
	receipts, err := runner.Execute(context.Background(), dataset, plan, func(
		_ context.Context, _ EvaluationCase, variant VariantSpec, receipt *RunReceipt, artifacts RunArtifactEnvelope,
	) error {
		exported = append(exported, variant.VariantVersion+":"+receipt.RunID)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(receipts) != 2 || strings.Join(started, ",") != "baseline,candidate" {
		t.Fatalf("unexpected runs: receipts=%#v started=%#v", receipts, started)
	}
	if receipts[0].Runtime.InstanceID != "instance-baseline" || receipts[1].Runtime.InstanceID != "instance-candidate" {
		t.Fatalf("runtime attestations were not preserved: %#v", receipts)
	}
	if len(exported) != 2 {
		t.Fatalf("exported = %#v", exported)
	}
}

type rejectingRuntime struct {
	attestation RuntimeAttestation
	starts      *int
}

func (runtime rejectingRuntime) Attest(context.Context) (RuntimeAttestation, error) {
	return runtime.attestation, nil
}
func (runtime rejectingRuntime) Start(context.Context, string, EvaluationCase, VariantSpec) (apiRun, error) {
	(*runtime.starts)++
	return apiRun{}, nil
}
func (runtime rejectingRuntime) Wait(context.Context, string, time.Duration) (apiRun, error) {
	return apiRun{}, nil
}
func (runtime rejectingRuntime) Artifacts(context.Context, string) (RunArtifactEnvelope, error) {
	return RunArtifactEnvelope{}, nil
}

func TestRunnerRejectsProductionRuntimeBeforeAnyRun(t *testing.T) {
	dataset, err := LoadFrozenDataset(writeFrozenDatasetFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	starts := 0
	runner := Runner{RuntimeFactory: func(VariantSpec, EvaluationCase) (Runtime, error) {
		return rejectingRuntime{attestation: RuntimeAttestation{
			SchemaVersion: RuntimeAttestationVersion, Enabled: true, Environment: "production",
			BuildRevision: "revision", InstanceID: "production-instance",
		}, starts: &starts}, nil
	}}
	if _, err := runner.Execute(context.Background(), dataset, validPlan(), nil); err == nil || !strings.Contains(err.Error(), "reject runtime") {
		t.Fatalf("expected production rejection, got %v", err)
	}
	if starts != 0 {
		t.Fatalf("started %d runs before rejecting runtime", starts)
	}
}

func TestHTTPRuntimeFactoryUsesCaseTokenEnvironment(t *testing.T) {
	variant := VariantSpec{Runtime: RuntimeSpec{
		BaseURL: "http://127.0.0.1:18090",
		CaseTokenEnvs: map[string]string{
			"case-001": "AIDA_EVAL_CASE_001_TOKEN",
			"case-002": "AIDA_EVAL_CASE_002_TOKEN",
		},
	}}
	values := map[string]string{
		"AIDA_EVAL_CASE_001_TOKEN": "token-one",
		"AIDA_EVAL_CASE_002_TOKEN": "token-two",
	}
	factory := NewHTTPRuntimeFactory(func(name string) string { return values[name] }, nil, time.Second)
	runtime, err := factory(variant, EvaluationCase{CaseID: "case-002"})
	if err != nil {
		t.Fatal(err)
	}
	httpRuntime, ok := runtime.(*HTTPRuntime)
	if !ok || httpRuntime.BearerToken != "token-two" {
		t.Fatalf("runtime = %#v", runtime)
	}
}

func TestHTTPRuntimeFreezeSourceUsesConfiguredRequestTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		time.Sleep(50 * time.Millisecond)
		response.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	runtime := HTTPRuntime{
		BaseURL: server.URL, BearerToken: "token", RequestTimeout: 10 * time.Millisecond,
	}
	_, err := runtime.FreezeSource(
		context.Background(), "2026-07-30", []string{"11111111-1111-4111-8111-111111111111"},
	)
	if err == nil || !strings.Contains(err.Error(), "Client.Timeout") {
		t.Fatalf("expected configured request timeout, got %v", err)
	}
}
