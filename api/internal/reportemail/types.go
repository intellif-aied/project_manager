package reportemail

import (
	"context"
	"time"
)

const (
	DeliveryPersonal    = "personal"
	DeliveryTeamSummary = "team_summary"
)

type Config struct {
	Enabled      bool
	Timezone     string
	TimeOfDay    string
	WorkerID     string
	PollInterval time.Duration
	LeaseTTL     time.Duration
	RetryDelay   time.Duration
	BatchSize    int
}

type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	FromName string
	TLSMode  string
}

type Message struct {
	ID       string
	To       string
	Subject  string
	TextBody string
	HTMLBody string
}

type Mailer interface {
	Send(context.Context, Message) error
}

type PersonalCandidate struct {
	UserID      string
	DisplayName string
	Email       string
	Content     string
}

type TeamCandidate struct {
	TeamID       string
	TeamName     string
	LeaderUserID string
	LeaderName   string
	LeaderEmail  string
	Members      []TeamMember
}

type TeamMember struct {
	DisplayName string
	Content     string
}

type Delivery struct {
	ID              string
	ReportDate      time.Time
	Type            string
	RecipientUserID string
	TeamID          string
	RecipientEmail  string
	Subject         string
	TextBody        string
	HTMLBody        string
	Status          string
	SkipReason      string
	Attempts        int
}

type Store interface {
	ListPersonalCandidates(context.Context, time.Time) ([]PersonalCandidate, error)
	ListTeamCandidates(context.Context, time.Time) ([]TeamCandidate, error)
	CreateDelivery(context.Context, Delivery) error
	ClaimDelivery(context.Context, time.Time, string, time.Duration) (Delivery, bool, error)
	MarkSent(context.Context, string, string, time.Time) error
	MarkFailed(context.Context, string, string, time.Time, time.Duration, error) error
}
