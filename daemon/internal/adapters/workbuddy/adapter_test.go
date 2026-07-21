package workbuddy

import (
	"context"
	"github.com/aidashboard/daemon/internal/sessionadapter"
	"testing"
)

func TestAdapterIsGatedWithoutReadingPrivateStorage(t *testing.T) {
	adapter := New()
	sessions, diagnostics := adapter.Discover(context.Background(), sessionadapter.DiscoverOptions{})
	if len(sessions) != 0 || len(diagnostics) != 1 || diagnostics[0].Code != "INPUT_CONTRACT_UNAVAILABLE" {
		t.Fatalf("sessions=%v diagnostics=%+v", sessions, diagnostics)
	}
	if _, err := adapter.Materialize(context.Background(), sessionadapter.Descriptor{}); err == nil {
		t.Fatal("expected gated materialization error")
	}
}
