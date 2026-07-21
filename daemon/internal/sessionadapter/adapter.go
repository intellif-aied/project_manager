package sessionadapter

import (
	"context"
	"time"
)

type ClientType string

type UsageCapability string

const (
	UsageUnavailable UsageCapability = "unavailable"
	UsageEstimated   UsageCapability = "estimated"
	UsageExact       UsageCapability = "exact"
)

type Capability struct {
	Content bool
	Usage   UsageCapability
}

type Detection struct {
	Installed     bool
	NativeVersion string
	Diagnostic    string
}

type DiscoverOptions struct {
	All   bool
	Since *time.Time
}

type Diagnostic struct {
	ClientType ClientType
	Code       string
	Message    string
}

type Descriptor struct {
	ClientType       ClientType
	NativeSessionRef string
	ParentRef        string
	ForkedAt         time.Time
	ForkSource       string
	StartedAt        time.Time
	LastActivityAt   time.Time
	CWD              string
	ProjectName      string
	Summary          string
	NativeVersion    string
	Capability       Capability
	OpaqueLocator    string
}

type MaterializedSession struct {
	Descriptor
	SourceFormat    string
	CanonicalPath   string
	AdapterVersion  string
	UsageCapability UsageCapability
}

type Adapter interface {
	ID() ClientType
	Detect(context.Context) Detection
	Discover(context.Context, DiscoverOptions) ([]Descriptor, []Diagnostic)
	Materialize(context.Context, Descriptor) (MaterializedSession, error)
}
