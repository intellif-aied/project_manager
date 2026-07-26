package tokenanalytics

import (
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrForbidden        = errors.New("token analytics scope is forbidden")
	ErrInvalidFilter    = errors.New("invalid token analytics filter")
	ErrSnapshotExpired  = errors.New("token analytics query snapshot expired")
	ErrSnapshotMismatch = errors.New("token analytics query snapshot does not match filters")
	ErrSnapshotBusy     = errors.New("token analytics query snapshot is temporarily busy")
)

type Actor struct {
	ID     int64
	Role   string
	TeamID *string
}

type Filters struct {
	Scope        string `json:"scope"`
	From         string `json:"from"`
	To           string `json:"to"`
	TeamID       string `json:"team_id,omitempty"`
	DepartmentID string `json:"department_id,omitempty"`
	UserID       string `json:"user_id,omitempty"`
	Model        string `json:"model,omitempty"`
	Query        string `json:"q,omitempty"`
}

type Snapshot struct {
	ID                        string
	Token                     string
	Version                   string
	Scope                     string
	SearchMode                string
	Filters                   Filters
	MetricsSnapshotAt         time.Time
	ExpiresAt                 time.Time
	ComponentCount            int64
	RollupCount               int64
	PendingSourceCount        int64
	PricingPendingSourceCount int64
}

type Summary struct {
	QuerySnapshotToken        string  `json:"query_snapshot_token"`
	MetricsSnapshotAt         string  `json:"metrics_snapshot_at"`
	ExpiresAt                 string  `json:"expires_at"`
	SearchMode                string  `json:"search_mode"`
	Scope                     string  `json:"scope"`
	From                      string  `json:"from"`
	To                        string  `json:"to"`
	TotalTokens               string  `json:"total_tokens"`
	UncachedInputTokens       string  `json:"uncached_input_tokens"`
	CacheReadTokens           string  `json:"cache_read_tokens"`
	CacheWrite5mTokens        string  `json:"cache_write_5m_tokens"`
	CacheWrite1hTokens        string  `json:"cache_write_1h_tokens"`
	OutputTokens              string  `json:"output_tokens"`
	ActiveDays                string  `json:"active_days"`
	EstimatedCostUSD          *string `json:"estimated_cost_usd"`
	EstimatedCostCNY          *string `json:"estimated_cost_cny"`
	PricingStatus             string  `json:"pricing_status"`
	UnpricedTokens            string  `json:"unpriced_tokens"`
	QualityStatus             string  `json:"quality_status"`
	DataFreshness             string  `json:"data_freshness"`
	PendingSourceCount        string  `json:"pending_source_count"`
	PricingPendingSourceCount string  `json:"pricing_pending_source_count"`
	ComponentCount            string  `json:"component_count"`
	RollupCount               string  `json:"rollup_count"`
	SessionCount              string  `json:"session_count"`
}

type TrendPoint struct {
	Date             string  `json:"date"`
	TotalTokens      string  `json:"total_tokens"`
	EstimatedCostCNY *string `json:"estimated_cost_cny"`
	PricingStatus    string  `json:"pricing_status"`
}

type Trends struct {
	QuerySnapshotToken string       `json:"query_snapshot_token"`
	Items              []TrendPoint `json:"items"`
}

type RankingItem struct {
	Key              string  `json:"key"`
	Label            string  `json:"label"`
	TotalTokens      string  `json:"total_tokens"`
	SessionCount     string  `json:"session_count"`
	EstimatedCostCNY *string `json:"estimated_cost_cny"`
	PricingStatus    string  `json:"pricing_status"`
	LastActivityAt   *string `json:"last_activity_at"`
	IsZeroUsage      bool    `json:"is_zero_usage"`
}

type Rankings struct {
	QuerySnapshotToken string        `json:"query_snapshot_token"`
	GroupBy            string        `json:"group_by"`
	Items              []RankingItem `json:"items"`
}

type SessionItem struct {
	SessionID               string   `json:"session_id"`
	SessionRef              string   `json:"session_ref"`
	FamilyRootSessionRef    string   `json:"family_root_session_ref"`
	MatchedMemberSessionID  *string  `json:"matched_member_session_id,omitempty"`
	MatchedMemberSessionRef *string  `json:"matched_member_session_ref,omitempty"`
	UserID                  string   `json:"user_id"`
	UserName                string   `json:"user_name"`
	AgentType               string   `json:"agent_type"`
	UsageStatus             string   `json:"usage_status"`
	Summary                 *string  `json:"summary"`
	StartedAt               string   `json:"started_at"`
	ActivityFrom            string   `json:"activity_from"`
	ActivityTo              string   `json:"activity_to"`
	ActivityDates           []string `json:"activity_dates"`
	SliceCount              int      `json:"slice_count"`
	Model                   string   `json:"model"`
	TotalTokens             string   `json:"total_tokens"`
	UncachedInputTokens     string   `json:"uncached_input_tokens"`
	CacheReadTokens         string   `json:"cache_read_tokens"`
	CacheWrite5mTokens      string   `json:"cache_write_5m_tokens"`
	CacheWrite1hTokens      string   `json:"cache_write_1h_tokens"`
	OutputTokens            string   `json:"output_tokens"`
	SelfTotalTokens         string   `json:"self_total_tokens"`
	SubagentTotalTokens     string   `json:"subagent_total_tokens"`
	FamilyTotalTokens       string   `json:"family_total_tokens"`
	LifetimeTotalTokens     string   `json:"lifetime_total_tokens"`
	RangeTotalTokens        string   `json:"range_total_tokens"`
	MemberCount             int      `json:"member_count"`
	IncludedInFamilyTotal   bool     `json:"included_in_family_total"`
	EstimatedCostCNY        *string  `json:"estimated_cost_cny"`
	PricingStatus           string   `json:"pricing_status"`
	QualityStatus           string   `json:"quality_status"`
}

func (item SessionItem) MarshalJSON() ([]byte, error) {
	type sessionItemAlias SessionItem
	if item.UsageStatus != "unavailable" {
		return json.Marshal(sessionItemAlias(item))
	}
	return json.Marshal(struct {
		sessionItemAlias
		TotalTokens         *string `json:"total_tokens"`
		UncachedInputTokens *string `json:"uncached_input_tokens"`
		CacheReadTokens     *string `json:"cache_read_tokens"`
		CacheWrite5mTokens  *string `json:"cache_write_5m_tokens"`
		CacheWrite1hTokens  *string `json:"cache_write_1h_tokens"`
		OutputTokens        *string `json:"output_tokens"`
		SelfTotalTokens     *string `json:"self_total_tokens"`
		SubagentTotalTokens *string `json:"subagent_total_tokens"`
		FamilyTotalTokens   *string `json:"family_total_tokens"`
		LifetimeTotalTokens *string `json:"lifetime_total_tokens"`
		RangeTotalTokens    *string `json:"range_total_tokens"`
	}{sessionItemAlias: sessionItemAlias(item)})
}

type Sessions struct {
	QuerySnapshotToken string        `json:"query_snapshot_token"`
	SearchMode         string        `json:"search_mode"`
	Items              []SessionItem `json:"items"`
	Total              int           `json:"total"`
	Page               int           `json:"page"`
	PageSize           int           `json:"page_size"`
}
