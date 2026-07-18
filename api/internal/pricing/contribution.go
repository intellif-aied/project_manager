package pricing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type contributionActivation struct {
	RevisionID string
	SourceID   string
	SessionID  string
}

// RecalculateContributionRevisionTx prices immutable contribution deltas. It is
// intentionally separate from the legacy Component pricing path until R5B
// switches every Token consumer to contribution-backed Rollups.
func RecalculateContributionRevisionTx(
	ctx context.Context,
	tx *sql.Tx,
	revisionID string,
) (RecalculateResult, error) {
	return recalculateContributionRevisionTx(ctx, tx, revisionID, false)
}

// EnsureContributionRevisionCostsTx prices only new immutable Contributions.
// Explicit admin recalculation still uses RecalculateContributionRevisionTx.
func EnsureContributionRevisionCostsTx(
	ctx context.Context,
	tx *sql.Tx,
	revisionID string,
) (RecalculateResult, error) {
	return recalculateContributionRevisionTx(ctx, tx, revisionID, true)
}

func recalculateContributionRevisionTx(
	ctx context.Context,
	tx *sql.Tx,
	revisionID string,
	missingOnly bool,
) (RecalculateResult, error) {
	contributions, err := loadEligibleContributions(ctx, tx, revisionID, missingOnly)
	if err != nil {
		return RecalculateResult{}, err
	}
	result := RecalculateResult{Eligible: int64(len(contributions))}
	for _, contribution := range contributions {
		candidate, err := buildCandidate(ctx, tx, contribution)
		if err != nil {
			return RecalculateResult{}, err
		}
		if candidate.PricingStatus == "priced" {
			result.Priced++
		} else {
			result.Unpriced++
		}
		current, err := loadActiveContributionCost(ctx, tx, contribution.ID, true)
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
				UPDATE session_usage_contribution_costs SET superseded_at = now()
				WHERE id = $1 AND superseded_at IS NULL`, current.ID); updateErr != nil {
				return RecalculateResult{}, updateErr
			}
			supersedesID = current.ID
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO session_usage_contribution_costs (
				contribution_id, price_version_id, rate_version_id, calculator_version,
				unit_price_snapshot_json, usd_cny_rate_snapshot,
				estimated_cost_usd, estimated_cost_cny, pricing_status, confidence,
				assumptions_json, calculation_reason, supersedes_id
			) VALUES (
				$1, NULLIF($2, '')::uuid, NULLIF($3, '')::uuid, $4,
				$5::jsonb, NULLIF($6, '')::numeric,
				NULLIF($7, '')::numeric, NULLIF($8, '')::numeric, $9, $10,
				$11::jsonb, NULLIF($12, ''), NULLIF($13, '')::uuid
			)`, contribution.ID, nullStringValue(candidate.PriceVersionID),
			nullStringValue(candidate.RateVersionID), CalculatorVersion,
			string(candidate.UnitPricesJSON), nullStringValue(candidate.RateSnapshot),
			nullStringValue(candidate.CostUSD), nullStringValue(candidate.CostCNY),
			candidate.PricingStatus, candidate.Confidence, string(candidate.AssumptionsJSON),
			nullStringValue(candidate.Reason), valueString(supersedesID)); err != nil {
			return RecalculateResult{}, err
		}
		result.Changed++
	}
	return result, nil
}

func recalculateActiveContributionsTx(
	ctx context.Context,
	tx *sql.Tx,
	filter RecalculateFilter,
) (RecalculateResult, []contributionActivation, error) {
	where := []string{
		"revision.status = 'active'",
		"state.active_revision_id = contribution.revision_id",
	}
	args := []any{}
	if filter.From != nil {
		args = append(args, filter.From.Format("2006-01-02"))
		where = append(where, fmt.Sprintf("contribution.activity_date >= $%d::date", len(args)))
	}
	if filter.To != nil {
		args = append(args, filter.To.Format("2006-01-02"))
		where = append(where, fmt.Sprintf("contribution.activity_date <= $%d::date", len(args)))
	}
	if strings.TrimSpace(filter.Model) != "" {
		args = append(args, strings.TrimSpace(filter.Model))
		where = append(where, fmt.Sprintf(
			"COALESCE(contribution.canonical_model, contribution.raw_model, '') = $%d", len(args)))
	}
	if strings.TrimSpace(filter.RevisionID) != "" {
		args = append(args, strings.TrimSpace(filter.RevisionID))
		where = append(where, fmt.Sprintf("contribution.revision_id = $%d::uuid", len(args)))
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT contribution.id, contribution.provider,
			contribution.raw_model, contribution.canonical_model,
			contribution.billing_variant, contribution.activity_date,
			contribution.uncached_input_tokens, contribution.cache_read_tokens,
			contribution.cache_write_5m_tokens, contribution.cache_write_1h_tokens,
			contribution.output_tokens, contribution.quality_status,
			contribution.is_estimated, revision.id::text, revision.source_id::text,
			source.session_id::text
		FROM session_usage_contributions contribution
		JOIN session_metrics_revisions revision ON revision.id = contribution.revision_id
		JOIN session_source_metrics_states state ON state.source_id = revision.source_id
		JOIN session_sources source ON source.id = revision.source_id
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY contribution.id
		FOR UPDATE OF contribution`, args...)
	if err != nil {
		return RecalculateResult{}, nil, err
	}
	defer rows.Close()
	type item struct {
		component  usageComponent
		activation contributionActivation
	}
	items := []item{}
	for rows.Next() {
		var current item
		if err := rows.Scan(&current.component.ID, &current.component.Provider,
			&current.component.RawModel, &current.component.CanonicalModel,
			&current.component.BillingVariant, &current.component.ActivityDate,
			&current.component.UncachedInputTokens, &current.component.CacheReadTokens,
			&current.component.CacheWrite5mTokens, &current.component.CacheWrite1hTokens,
			&current.component.OutputTokens, &current.component.QualityStatus,
			&current.component.IsEstimated, &current.activation.RevisionID,
			&current.activation.SourceID, &current.activation.SessionID); err != nil {
			return RecalculateResult{}, nil, err
		}
		items = append(items, current)
	}
	if err := rows.Err(); err != nil {
		return RecalculateResult{}, nil, err
	}

	result := RecalculateResult{Eligible: int64(len(items))}
	affected := map[string]contributionActivation{}
	for _, current := range items {
		candidate, err := buildCandidate(ctx, tx, current.component)
		if err != nil {
			return RecalculateResult{}, nil, err
		}
		if candidate.PricingStatus == "priced" {
			result.Priced++
		} else {
			result.Unpriced++
		}
		active, err := loadActiveContributionCost(ctx, tx, current.component.ID, true)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return RecalculateResult{}, nil, err
		}
		if err == nil && candidatesEqual(active, candidate) {
			result.Unchanged++
			continue
		}
		if err := replaceContributionCost(ctx, tx, current.component.ID, active, err == nil, candidate); err != nil {
			return RecalculateResult{}, nil, err
		}
		result.Changed++
		affected[current.activation.RevisionID] = current.activation
	}
	activations := make([]contributionActivation, 0, len(affected))
	for _, activation := range affected {
		activations = append(activations, activation)
	}
	sort.Slice(activations, func(i, j int) bool {
		if activations[i].SessionID != activations[j].SessionID {
			return activations[i].SessionID < activations[j].SessionID
		}
		return activations[i].SourceID < activations[j].SourceID
	})
	return result, activations, nil
}

func replaceContributionCost(
	ctx context.Context,
	tx *sql.Tx,
	contributionID string,
	current activeCost,
	hasCurrent bool,
	candidate costCandidate,
) error {
	var supersedesID any
	if hasCurrent {
		if _, err := tx.ExecContext(ctx, `
			UPDATE session_usage_contribution_costs SET superseded_at = now()
			WHERE id = $1 AND superseded_at IS NULL`, current.ID); err != nil {
			return err
		}
		supersedesID = current.ID
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO session_usage_contribution_costs (
			contribution_id, price_version_id, rate_version_id, calculator_version,
			unit_price_snapshot_json, usd_cny_rate_snapshot,
			estimated_cost_usd, estimated_cost_cny, pricing_status, confidence,
			assumptions_json, calculation_reason, supersedes_id
		) VALUES (
			$1, NULLIF($2, '')::uuid, NULLIF($3, '')::uuid, $4,
			$5::jsonb, NULLIF($6, '')::numeric,
			NULLIF($7, '')::numeric, NULLIF($8, '')::numeric, $9, $10,
			$11::jsonb, NULLIF($12, ''), NULLIF($13, '')::uuid
		)`, contributionID, nullStringValue(candidate.PriceVersionID),
		nullStringValue(candidate.RateVersionID), CalculatorVersion,
		string(candidate.UnitPricesJSON), nullStringValue(candidate.RateSnapshot),
		nullStringValue(candidate.CostUSD), nullStringValue(candidate.CostCNY),
		candidate.PricingStatus, candidate.Confidence, string(candidate.AssumptionsJSON),
		nullStringValue(candidate.Reason), valueString(supersedesID))
	return err
}

func loadEligibleContributions(
	ctx context.Context,
	tx *sql.Tx,
	revisionID string,
	missingOnly bool,
) ([]usageComponent, error) {
	query := `
		SELECT contribution.id, contribution.provider,
			contribution.raw_model, contribution.canonical_model,
			contribution.billing_variant, contribution.activity_date,
			contribution.uncached_input_tokens, contribution.cache_read_tokens,
			contribution.cache_write_5m_tokens, contribution.cache_write_1h_tokens,
			contribution.output_tokens, contribution.quality_status,
			contribution.is_estimated
		FROM session_usage_contributions contribution
		WHERE contribution.revision_id = $1`
	if missingOnly {
		query += `
			AND NOT EXISTS (
				SELECT 1
				FROM session_usage_contribution_costs cost
				WHERE cost.contribution_id = contribution.id
					AND cost.calculator_version = $2
					AND cost.superseded_at IS NULL
			)`
	}
	query += `
		ORDER BY contribution.id
		FOR UPDATE OF contribution`
	args := []any{revisionID}
	if missingOnly {
		args = append(args, CalculatorVersion)
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []usageComponent{}
	for rows.Next() {
		var item usageComponent
		if err := rows.Scan(&item.ID, &item.Provider, &item.RawModel, &item.CanonicalModel,
			&item.BillingVariant, &item.ActivityDate, &item.UncachedInputTokens,
			&item.CacheReadTokens, &item.CacheWrite5mTokens, &item.CacheWrite1hTokens,
			&item.OutputTokens, &item.QualityStatus, &item.IsEstimated); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadActiveContributionCost(
	ctx context.Context,
	tx *sql.Tx,
	contributionID string,
	lock bool,
) (activeCost, error) {
	query := `
		SELECT id, price_version_id, rate_version_id, unit_price_snapshot_json,
			usd_cny_rate_snapshot::text, estimated_cost_usd::text,
			estimated_cost_cny::text, pricing_status, confidence,
			assumptions_json, calculation_reason
		FROM session_usage_contribution_costs
		WHERE contribution_id = $1 AND calculator_version = $2
			AND superseded_at IS NULL`
	if lock {
		query += " FOR UPDATE"
	}
	var current activeCost
	err := tx.QueryRowContext(ctx, query, contributionID, CalculatorVersion).Scan(
		&current.ID, &current.PriceVersionID, &current.RateVersionID,
		&current.UnitPricesJSON, &current.RateSnapshot, &current.CostUSD,
		&current.CostCNY, &current.PricingStatus, &current.Confidence,
		&current.AssumptionsJSON, &current.Reason)
	return current, err
}
