package reporteval

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRunnerExecutesCaseVariantPairsSequentially(t *testing.T) {
	var started []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("missing bearer token")
		}
		if request.Method == http.MethodPost {
			parts := strings.Split(request.URL.Path, "/")
			agentID := parts[len(parts)-2]
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			started = append(started, agentID)
			_ = json.NewEncoder(response).Encode(map[string]any{"id": fmt.Sprintf("run-%d", len(started)), "status": "pending"})
			return
		}
		_ = json.NewEncoder(response).Encode(map[string]any{"id": request.URL.Path[strings.LastIndex(request.URL.Path, "/")+1:], "status": "succeeded"})
	}))
	defer server.Close()

	dataset := validDataset()
	plan := validPlan()
	runner := Runner{BaseURL: server.URL, BearerToken: "test-token", PollInterval: time.Millisecond}
	var exported []string
	receipts, err := runner.Execute(context.Background(), dataset, plan, func(_ context.Context, _ EvaluationCase, variant VariantSpec, receipt *RunReceipt) error {
		exported = append(exported, variant.VariantVersion+":"+receipt.RunID)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(receipts) != 2 || strings.Join(started, ",") != "default,candidate-agent" {
		t.Fatalf("unexpected runs: receipts=%#v started=%#v", receipts, started)
	}
	if strings.Join(exported, ",") != "baseline:run-1,candidate:run-2" {
		t.Fatalf("unexpected export order: %#v", exported)
	}
}
