package sessiondigest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/aidashboard/api/internal/sessionsync"
)

const (
	Version          = "session-digest/v1"
	RedactionVersion = "report-redaction/v1"
	JobType          = sessionsync.JobBuildContentSliceDigest
	DefaultItemBytes = 4 << 10
)

var (
	ErrDigestUnavailable      = errors.New("session digest revision is unavailable")
	ErrStaleDigestSource      = errors.New("session digest source is stale")
	ErrDigestStatePersistence = errors.New("session digest state update must be retried")
)

type Validation struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Summary string `json:"summary,omitempty"`
}

type Digest struct {
	Goals        []string     `json:"goals"`
	Outcomes     []string     `json:"outcomes"`
	FilesChanged []string     `json:"files_changed"`
	Validations  []Validation `json:"validations"`
	Blockers     []string     `json:"blockers"`
}

func EmptyDigest() Digest {
	return Digest{
		Goals:        []string{},
		Outcomes:     []string{},
		FilesChanged: []string{},
		Validations:  []Validation{},
		Blockers:     []string{},
	}
}

type Event struct {
	StartCursor  int64
	EndCursor    int64
	EventType    string
	Summary      string
	Excerpt      string
	Payload      json.RawMessage
	ContentSHA   string
	PayloadBytes int64
}

type BuildResult struct {
	Digest             Digest
	SourceEventCount   int64
	IncludedEventCount int64
	OmittedEventCount  int64
	SourceBytes        int64
	DigestBytes        int
	Truncated          bool
	SourceSHA256       string
	DigestSHA256       string
	DigestJSON         []byte
}

type Revision struct {
	ID                   string
	SliceID              string
	SessionID            string
	ProjectionRevisionID string
	GenerationID         string
	ContentEpoch         int64
	StartCursor          int64
	EndCursor            int64
	Status               string
	DigestVersion        string
	RedactionVersion     string
}

type ProcessingTarget struct {
	Revision Revision
	Ready    bool
}

type Config struct {
	DigestVersion    string
	RedactionVersion string
	ItemMaxBytes     int
	ReconcileBatch   int
}

func DefaultConfig() Config {
	return Config{
		DigestVersion:    Version,
		RedactionVersion: RedactionVersion,
		ItemMaxBytes:     DefaultItemBytes,
		ReconcileBatch:   25,
	}
}

func (c Config) Normalized() (Config, error) {
	if c.DigestVersion == "" {
		c.DigestVersion = Version
	}
	if c.RedactionVersion == "" {
		c.RedactionVersion = RedactionVersion
	}
	if c.ItemMaxBytes == 0 {
		c.ItemMaxBytes = DefaultItemBytes
	}
	if c.ReconcileBatch == 0 {
		c.ReconcileBatch = 25
	}
	if c.DigestVersion != Version || c.RedactionVersion != RedactionVersion {
		return Config{}, errors.New("unsupported session digest or redaction version")
	}
	if c.ItemMaxBytes < 1024 || c.ItemMaxBytes > 64<<10 || c.ReconcileBatch < 1 || c.ReconcileBatch > 500 {
		return Config{}, errors.New("invalid session digest limits")
	}
	return c, nil
}

func HashBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
