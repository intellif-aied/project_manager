package workbuddy

import (
	"context"
	"errors"
	"github.com/aidashboard/daemon/internal/sessionadapter"
	"os/exec"
)

type Adapter struct{}

func New() *Adapter                            { return &Adapter{} }
func (*Adapter) ID() sessionadapter.ClientType { return "workbuddy" }
func (*Adapter) Detect(context.Context) sessionadapter.Detection {
	for _, name := range []string{"workbuddy", "WorkBuddy"} {
		if path, err := exec.LookPath(name); err == nil {
			return sessionadapter.Detection{Installed: true, Diagnostic: "WorkBuddy detected at " + path + "; official machine-readable export contract is unavailable"}
		}
	}
	return sessionadapter.Detection{Diagnostic: "WorkBuddy not detected; official machine-readable export contract is unavailable"}
}
func (a *Adapter) Discover(context.Context, sessionadapter.DiscoverOptions) ([]sessionadapter.Descriptor, []sessionadapter.Diagnostic) {
	return nil, []sessionadapter.Diagnostic{{ClientType: a.ID(), Code: "INPUT_CONTRACT_UNAVAILABLE", Message: "WorkBuddy remains detected-only; local databases and private cloud APIs are not read"}}
}
func (*Adapter) Materialize(context.Context, sessionadapter.Descriptor) (sessionadapter.MaterializedSession, error) {
	return sessionadapter.MaterializedSession{}, errors.New("WorkBuddy materialization is gated until an official export contract is available")
}

var _ sessionadapter.Adapter = (*Adapter)(nil)
