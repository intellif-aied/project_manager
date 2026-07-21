package sessionadapter_test

import (
	"context"
	"errors"
	"testing"

	"github.com/aidashboard/daemon/internal/sessionadapter"
)

type fakeAdapter struct {
	id          sessionadapter.ClientType
	descriptors []sessionadapter.Descriptor
	diagnostics []sessionadapter.Diagnostic
}

func (adapter fakeAdapter) ID() sessionadapter.ClientType { return adapter.id }
func (adapter fakeAdapter) Detect(context.Context) sessionadapter.Detection {
	return sessionadapter.Detection{Installed: true}
}
func (adapter fakeAdapter) Discover(context.Context, sessionadapter.DiscoverOptions) ([]sessionadapter.Descriptor, []sessionadapter.Diagnostic) {
	return adapter.descriptors, adapter.diagnostics
}
func (adapter fakeAdapter) Materialize(context.Context, sessionadapter.Descriptor) (sessionadapter.MaterializedSession, error) {
	return sessionadapter.MaterializedSession{}, errors.New("not used")
}

func TestRegistryDiscoversAdaptersIndependently(t *testing.T) {
	opencode := fakeAdapter{
		id: sessionadapter.ClientType("opencode"),
		diagnostics: []sessionadapter.Diagnostic{{
			Code: "ONE_SESSION_SKIPPED", Message: "invalid session",
		}},
		descriptors: []sessionadapter.Descriptor{{NativeSessionRef: "open-1"}},
	}
	kimi := fakeAdapter{
		id:          sessionadapter.ClientType("kimi_code"),
		descriptors: []sessionadapter.Descriptor{{NativeSessionRef: "kimi-1"}},
	}
	registry, err := sessionadapter.NewRegistry(opencode, kimi)
	if err != nil {
		t.Fatal(err)
	}

	result := registry.Discover(context.Background(), sessionadapter.DiscoverOptions{All: true})
	if len(result.Sessions) != 2 || result.Sessions[0].ClientType != opencode.id || result.Sessions[1].ClientType != kimi.id {
		t.Fatalf("sessions=%+v", result.Sessions)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].ClientType != opencode.id {
		t.Fatalf("diagnostics=%+v", result.Diagnostics)
	}

	if _, err := sessionadapter.NewRegistry(opencode, opencode); err == nil {
		t.Fatal("duplicate adapter ID was accepted")
	}
}
