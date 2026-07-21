package sessionadapter

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type Registry struct {
	adapters []Adapter
}

type DiscoveryResult struct {
	Sessions    []Descriptor
	Diagnostics []Diagnostic
}

func NewRegistry(adapters ...Adapter) (Registry, error) {
	if len(adapters) == 0 {
		return Registry{}, errors.New("at least one adapter is required")
	}
	seen := make(map[ClientType]struct{}, len(adapters))
	registered := make([]Adapter, 0, len(adapters))
	for _, adapter := range adapters {
		if adapter == nil {
			return Registry{}, errors.New("adapter is required")
		}
		id := ClientType(strings.TrimSpace(string(adapter.ID())))
		if id == "" {
			return Registry{}, errors.New("adapter ID is required")
		}
		if _, exists := seen[id]; exists {
			return Registry{}, fmt.Errorf("duplicate adapter ID %q", id)
		}
		seen[id] = struct{}{}
		registered = append(registered, adapter)
	}
	return Registry{adapters: registered}, nil
}

func (registry Registry) Discover(ctx context.Context, options DiscoverOptions) DiscoveryResult {
	result := DiscoveryResult{}
	for _, adapter := range registry.adapters {
		id := adapter.ID()
		descriptors, diagnostics := adapter.Discover(ctx, options)
		for _, descriptor := range descriptors {
			if descriptor.ClientType == "" {
				descriptor.ClientType = id
			}
			if descriptor.ClientType != id {
				result.Diagnostics = append(result.Diagnostics, Diagnostic{
					ClientType: id,
					Code:       "ADAPTER_CLIENT_TYPE_MISMATCH",
					Message:    "adapter returned a descriptor for another client type",
				})
				continue
			}
			result.Sessions = append(result.Sessions, descriptor)
		}
		for _, diagnostic := range diagnostics {
			if diagnostic.ClientType == "" {
				diagnostic.ClientType = id
			}
			result.Diagnostics = append(result.Diagnostics, diagnostic)
		}
	}
	return result
}
