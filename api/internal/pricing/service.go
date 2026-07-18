package pricing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/aidashboard/api/internal/tokenrollup"
)

const CalculatorVersion = "aida-cost-v1"

const calculateCostQuery = `
		SELECT calculated.cost_usd::numeric(30,12)::text,
			(calculated.cost_usd * $11::numeric)::numeric(30,12)::text
		FROM (
			SELECT (
				$1::numeric * $6::numeric +
					$2::numeric * $7::numeric +
					$3::numeric * $8::numeric +
					$4::numeric * $9::numeric +
					$5::numeric * $10::numeric
			) / 1000000::numeric AS cost_usd
		) calculated`

type Service struct {
	db *sql.DB
}

type RecalculateFilter struct {
	From       *time.Time `json:"from,omitempty"`
	To         *time.Time `json:"to,omitempty"`
	Model      string     `json:"model,omitempty"`
	RevisionID string     `json:"revision_id,omitempty"`
}

type RecalculateResult struct {
	Eligible  int64 `json:"eligible_components"`
	Priced    int64 `json:"priced_components"`
	Unpriced  int64 `json:"unpriced_components"`
	Changed   int64 `json:"changed_components"`
	Unchanged int64 `json:"unchanged_components"`
}

type usageComponent struct {
	ID                  string
	Provider            string
	RawModel            sql.NullString
	CanonicalModel      sql.NullString
	BillingVariant      string
	ActivityDate        time.Time
	UncachedInputTokens int64
	CacheReadTokens     int64
	CacheWrite5mTokens  int64
	CacheWrite1hTokens  int64
	OutputTokens        int64
	QualityStatus       string
	IsEstimated         bool
}

type resolvedPrice struct {
	ID               string
	CanonicalModel   string
	Input            string
	CacheRead        string
	CacheWrite5m     string
	CacheWrite1h     string
	Output           string
	MatchedVariant   string
	RequestedVariant string
}

type resolvedRate struct {
	ID   string
	Rate string
}

type costCandidate struct {
	PriceVersionID  sql.NullString
	RateVersionID   sql.NullString
	UnitPricesJSON  []byte
	RateSnapshot    sql.NullString
	CostUSD         sql.NullString
	CostCNY         sql.NullString
	PricingStatus   string
	Confidence      string
	AssumptionsJSON []byte
	Reason          sql.NullString
}

type activeCost struct {
	ID              string
	PriceVersionID  sql.NullString
	RateVersionID   sql.NullString
	UnitPricesJSON  []byte
	RateSnapshot    sql.NullString
	CostUSD         sql.NullString
	CostCNY         sql.NullString
	PricingStatus   string
	Confidence      string
	AssumptionsJSON []byte
	Reason          sql.NullString
}

func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

func (s *Service) Preview(ctx context.Context, filter RecalculateFilter) (RecalculateResult, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return RecalculateResult{}, err
	}
	defer tx.Rollback()

	components, err := loadEligibleComponents(ctx, tx, filter, false)
	if err != nil {
		return RecalculateResult{}, err
	}
	result := RecalculateResult{Eligible: int64(len(components))}
	for _, component := range components {
		candidate, err := buildCandidate(ctx, tx, component)
		if err != nil {
			return RecalculateResult{}, err
		}
		if candidate.PricingStatus == "priced" {
			result.Priced++
		} else {
			result.Unpriced++
		}
		current, err := loadActiveCost(ctx, tx, component.ID, false)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return RecalculateResult{}, err
		}
		if err == nil && candidatesEqual(current, candidate) {
			result.Unchanged++
		} else {
			result.Changed++
		}
	}
	return result, tx.Commit()
}

func (s *Service) Recalculate(ctx context.Context, filter RecalculateFilter) (RecalculateResult, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return RecalculateResult{}, err
	}
	defer tx.Rollback()
	result, err := RecalculateTx(ctx, tx, filter)
	if err != nil {
		return RecalculateResult{}, err
	}
	if err := recalculateContributionRollupsTx(ctx, tx, filter); err != nil {
		return RecalculateResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return RecalculateResult{}, err
	}
	return result, nil
}

func (s *Service) RecalculateWithAudit(
	ctx context.Context,
	filter RecalculateFilter,
	requestedBy int64,
	reason string,
) (RecalculateResult, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return RecalculateResult{}, errors.New("recalculation reason is required")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return RecalculateResult{}, err
	}
	defer tx.Rollback()
	result, err := RecalculateTx(ctx, tx, filter)
	if err != nil {
		return RecalculateResult{}, err
	}
	if err := recalculateContributionRollupsTx(ctx, tx, filter); err != nil {
		return RecalculateResult{}, err
	}
	filterJSON, _ := json.Marshal(filter)
	resultJSON, _ := json.Marshal(result)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO pricing_recalculation_runs(
			requested_by, filter_json, result_json, reason, calculator_version
		) VALUES($1, $2::jsonb, $3::jsonb, $4, $5)`, requestedBy,
		string(filterJSON), string(resultJSON), reason, CalculatorVersion); err != nil {
		return RecalculateResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return RecalculateResult{}, err
	}
	return result, nil
}

func RecalculateRevisionTx(ctx context.Context, tx *sql.Tx, revisionID string) (RecalculateResult, error) {
	return RecalculateTx(ctx, tx, RecalculateFilter{RevisionID: revisionID})
}

func recalculateContributionRollupsTx(ctx context.Context, tx *sql.Tx, filter RecalculateFilter) error {
	_, activations, err := recalculateActiveContributionsTx(ctx, tx, filter)
	if err != nil {
		return err
	}
	builder := tokenrollup.NewBuilder()
	for _, activation := range activations {
		if err := builder.BuildForActivation(ctx, tx, activation.SessionID, activation.SourceID,
			activation.RevisionID, CalculatorVersion); err != nil {
			return err
		}
	}
	return nil
}

func RecalculateTx(ctx context.Context, tx *sql.Tx, filter RecalculateFilter) (RecalculateResult, error) {
	components, err := loadEligibleComponents(ctx, tx, filter, true)
	if err != nil {
		return RecalculateResult{}, err
	}
	result := RecalculateResult{Eligible: int64(len(components))}
	for _, component := range components {
		candidate, err := buildCandidate(ctx, tx, component)
		if err != nil {
			return RecalculateResult{}, err
		}
		if candidate.PricingStatus == "priced" {
			result.Priced++
		} else {
			result.Unpriced++
		}
		current, err := loadActiveCost(ctx, tx, component.ID, true)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return RecalculateResult{}, err
		}
		if err == nil && candidatesEqual(current, candidate) {
			result.Unchanged++
			continue
		}

		var supersedesID any
		if err == nil {
			if _, updateErr := tx.ExecContext(ctx, `
				UPDATE session_activity_costs SET superseded_at = now()
				WHERE id = $1 AND superseded_at IS NULL`, current.ID); updateErr != nil {
				return RecalculateResult{}, updateErr
			}
			supersedesID = current.ID
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO session_activity_costs (
				usage_component_id, price_version_id, rate_version_id, calculator_version,
				unit_price_snapshot_json, usd_cny_rate_snapshot,
				estimated_cost_usd, estimated_cost_cny, pricing_status, confidence,
				assumptions_json, calculation_reason, supersedes_id
			) VALUES (
				$1, NULLIF($2, '')::uuid, NULLIF($3, '')::uuid, $4,
				$5::jsonb, NULLIF($6, '')::numeric,
				NULLIF($7, '')::numeric, NULLIF($8, '')::numeric, $9, $10,
				$11::jsonb, NULLIF($12, ''), NULLIF($13, '')::uuid
			)`, component.ID, nullStringValue(candidate.PriceVersionID), nullStringValue(candidate.RateVersionID),
			CalculatorVersion, string(candidate.UnitPricesJSON), nullStringValue(candidate.RateSnapshot),
			nullStringValue(candidate.CostUSD), nullStringValue(candidate.CostCNY), candidate.PricingStatus,
			candidate.Confidence, string(candidate.AssumptionsJSON), nullStringValue(candidate.Reason), valueString(supersedesID)); err != nil {
			return RecalculateResult{}, err
		}
		result.Changed++
	}
	return result, nil
}

func loadEligibleComponents(ctx context.Context, tx *sql.Tx, filter RecalculateFilter, lock bool) ([]usageComponent, error) {
	where := []string{
		"c.valid_to IS NULL",
		"revision.status = 'active'",
	}
	if strings.TrimSpace(filter.RevisionID) == "" {
		where = append(where, "state.active_revision_id = c.revision_id")
	}
	args := []any{}
	if filter.From != nil {
		args = append(args, filter.From.Format("2006-01-02"))
		where = append(where, fmt.Sprintf("c.activity_date >= $%d::date", len(args)))
	}
	if filter.To != nil {
		args = append(args, filter.To.Format("2006-01-02"))
		where = append(where, fmt.Sprintf("c.activity_date <= $%d::date", len(args)))
	}
	if strings.TrimSpace(filter.Model) != "" {
		args = append(args, strings.TrimSpace(filter.Model))
		where = append(where, fmt.Sprintf("COALESCE(c.canonical_model, c.raw_model, '') = $%d", len(args)))
	}
	if strings.TrimSpace(filter.RevisionID) != "" {
		args = append(args, strings.TrimSpace(filter.RevisionID))
		where = append(where, fmt.Sprintf("c.revision_id = $%d::uuid", len(args)))
	}
	query := `
		SELECT c.id, c.provider, c.raw_model, c.canonical_model, c.billing_variant,
			c.activity_date, c.uncached_input_tokens, c.cache_read_tokens,
			c.cache_write_5m_tokens, c.cache_write_1h_tokens, c.output_tokens,
			c.quality_status, c.is_estimated
		FROM session_usage_components c
		JOIN session_metrics_revisions revision ON revision.id = c.revision_id
		JOIN session_source_metrics_states state ON state.source_id = revision.source_id
		WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY c.id`
	if lock {
		query += " FOR UPDATE OF c"
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	components := []usageComponent{}
	for rows.Next() {
		var item usageComponent
		if err := rows.Scan(&item.ID, &item.Provider, &item.RawModel, &item.CanonicalModel,
			&item.BillingVariant, &item.ActivityDate, &item.UncachedInputTokens,
			&item.CacheReadTokens, &item.CacheWrite5mTokens, &item.CacheWrite1hTokens,
			&item.OutputTokens, &item.QualityStatus, &item.IsEstimated); err != nil {
			return nil, err
		}
		components = append(components, item)
	}
	return components, rows.Err()
}

func buildCandidate(ctx context.Context, tx *sql.Tx, component usageComponent) (costCandidate, error) {
	rawModel := strings.TrimSpace(component.RawModel.String)
	if rawModel == "" {
		rawModel = strings.TrimSpace(component.CanonicalModel.String)
	}
	var canonicalModel string
	err := tx.QueryRowContext(ctx, `
		SELECT canonical_model
		FROM model_aliases
		WHERE provider = $1 AND raw_model_pattern = $2 AND status = 'reviewed'`,
		component.Provider, rawModel).Scan(&canonicalModel)
	if errors.Is(err, sql.ErrNoRows) {
		return unpricedCandidate(component, "reviewed model alias is missing", rawModel), nil
	}
	if err != nil {
		return costCandidate{}, err
	}

	var price resolvedPrice
	err = tx.QueryRowContext(ctx, `
		SELECT version.id, version.canonical_model,
			version.input_per_million::text, version.cache_read_per_million::text,
			version.cache_write_5m_per_million::text, version.cache_write_1h_per_million::text,
			version.output_per_million::text, version.billing_variant
		FROM price_books book
		JOIN model_price_versions version ON version.price_book_id = book.id
		WHERE book.status = 'active' AND version.status = 'published' AND version.superseded_at IS NULL
			AND version.canonical_model = $1
			AND (version.billing_variant = $2 OR version.billing_variant = 'any')
			AND version.effective_from <= $3::date
			AND (version.effective_to IS NULL OR version.effective_to > $3::date)
		ORDER BY (version.billing_variant = $2) DESC, version.effective_from DESC
		LIMIT 1`, canonicalModel, component.BillingVariant, component.ActivityDate.Format("2006-01-02")).Scan(
		&price.ID, &price.CanonicalModel, &price.Input, &price.CacheRead, &price.CacheWrite5m,
		&price.CacheWrite1h, &price.Output, &price.MatchedVariant)
	if errors.Is(err, sql.ErrNoRows) {
		return unpricedCandidate(component, "published model price is missing", canonicalModel), nil
	}
	if err != nil {
		return costCandidate{}, err
	}
	price.RequestedVariant = component.BillingVariant

	var rate resolvedRate
	err = tx.QueryRowContext(ctx, `
		SELECT id, rate::text
		FROM usd_cny_rate_versions
		WHERE status = 'published' AND superseded_at IS NULL AND effective_from <= $1::date
			AND (effective_to IS NULL OR effective_to > $1::date)
		ORDER BY effective_from DESC LIMIT 1`, component.ActivityDate.Format("2006-01-02")).Scan(&rate.ID, &rate.Rate)
	if errors.Is(err, sql.ErrNoRows) {
		return unpricedCandidate(component, "published USD/CNY rate is missing", canonicalModel), nil
	}
	if err != nil {
		return costCandidate{}, err
	}

	var costUSD, costCNY string
	err = tx.QueryRowContext(ctx, calculateCostQuery, component.UncachedInputTokens, component.CacheReadTokens,
		component.CacheWrite5mTokens, component.CacheWrite1hTokens, component.OutputTokens,
		price.Input, price.CacheRead, price.CacheWrite5m, price.CacheWrite1h, price.Output, rate.Rate).Scan(&costUSD, &costCNY)
	if err != nil {
		return costCandidate{}, err
	}
	pricesJSON, _ := json.Marshal(map[string]string{
		"canonical_model":            canonicalModel,
		"billing_variant":            price.MatchedVariant,
		"input_per_million":          price.Input,
		"cache_read_per_million":     price.CacheRead,
		"cache_write_5m_per_million": price.CacheWrite5m,
		"cache_write_1h_per_million": price.CacheWrite1h,
		"output_per_million":         price.Output,
	})
	assumptionsJSON, _ := json.Marshal(map[string]string{
		"raw_model":                 rawModel,
		"canonical_model":           canonicalModel,
		"requested_billing_variant": component.BillingVariant,
		"matched_billing_variant":   price.MatchedVariant,
	})
	confidence := "exact"
	if component.QualityStatus != "exact" || component.IsEstimated {
		confidence = "estimated"
	}
	return costCandidate{
		PriceVersionID:  sql.NullString{String: price.ID, Valid: true},
		RateVersionID:   sql.NullString{String: rate.ID, Valid: true},
		UnitPricesJSON:  pricesJSON,
		RateSnapshot:    sql.NullString{String: rate.Rate, Valid: true},
		CostUSD:         sql.NullString{String: costUSD, Valid: true},
		CostCNY:         sql.NullString{String: costCNY, Valid: true},
		PricingStatus:   "priced",
		Confidence:      confidence,
		AssumptionsJSON: assumptionsJSON,
	}, nil
}

func unpricedCandidate(component usageComponent, reason, model string) costCandidate {
	assumptions, _ := json.Marshal(map[string]string{
		"provider":        component.Provider,
		"model":           model,
		"billing_variant": component.BillingVariant,
	})
	return costCandidate{
		UnitPricesJSON:  []byte(`{}`),
		PricingStatus:   "unpriced",
		Confidence:      "unknown",
		AssumptionsJSON: assumptions,
		Reason:          sql.NullString{String: reason, Valid: true},
	}
}

func loadActiveCost(ctx context.Context, tx *sql.Tx, componentID string, lock bool) (activeCost, error) {
	var item activeCost
	err := tx.QueryRowContext(ctx, activeCostQuery(lock), componentID, CalculatorVersion).Scan(&item.ID, &item.PriceVersionID,
		&item.RateVersionID, &item.UnitPricesJSON, &item.RateSnapshot, &item.CostUSD,
		&item.CostCNY, &item.PricingStatus, &item.Confidence, &item.AssumptionsJSON, &item.Reason)
	return item, err
}

func activeCostQuery(lock bool) string {
	query := `
		SELECT id, price_version_id::text, rate_version_id::text,
			unit_price_snapshot_json, usd_cny_rate_snapshot::text,
			estimated_cost_usd::text, estimated_cost_cny::text,
			pricing_status, confidence, assumptions_json, calculation_reason
		FROM session_activity_costs
		WHERE usage_component_id = $1 AND calculator_version = $2 AND superseded_at IS NULL`
	if lock {
		query += " FOR UPDATE"
	}
	return query
}

func candidatesEqual(current activeCost, candidate costCandidate) bool {
	return nullStringsEqual(current.PriceVersionID, candidate.PriceVersionID) &&
		nullStringsEqual(current.RateVersionID, candidate.RateVersionID) &&
		jsonEqual(current.UnitPricesJSON, candidate.UnitPricesJSON) &&
		nullDecimalsEqual(current.RateSnapshot, candidate.RateSnapshot) &&
		nullDecimalsEqual(current.CostUSD, candidate.CostUSD) &&
		nullDecimalsEqual(current.CostCNY, candidate.CostCNY) &&
		current.PricingStatus == candidate.PricingStatus && current.Confidence == candidate.Confidence &&
		jsonEqual(current.AssumptionsJSON, candidate.AssumptionsJSON) &&
		nullStringsEqual(current.Reason, candidate.Reason)
}

func jsonEqual(left, right []byte) bool {
	var leftValue, rightValue any
	if json.Unmarshal(left, &leftValue) != nil || json.Unmarshal(right, &rightValue) != nil {
		return string(left) == string(right)
	}
	leftJSON, _ := json.Marshal(leftValue)
	rightJSON, _ := json.Marshal(rightValue)
	return string(leftJSON) == string(rightJSON)
}

func nullStringsEqual(left, right sql.NullString) bool {
	return left.Valid == right.Valid && (!left.Valid || left.String == right.String)
}

func nullDecimalsEqual(left, right sql.NullString) bool {
	if left.Valid != right.Valid {
		return false
	}
	if !left.Valid {
		return true
	}
	leftValue, leftOK := new(big.Rat).SetString(left.String)
	rightValue, rightOK := new(big.Rat).SetString(right.String)
	return leftOK && rightOK && leftValue.Cmp(rightValue) == 0
}

func nullStringValue(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func valueString(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}
