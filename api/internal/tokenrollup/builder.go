package tokenrollup

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/lib/pq"
)

type Builder struct{}

func NewBuilder() *Builder {
	return &Builder{}
}

// BuildForActivation rebuilds only relation families touched by one Metrics
// Revision activation. The caller owns the transaction so Metrics, pricing,
// family relation, and Rollup activation remain atomic.
func (b *Builder) BuildForActivation(
	ctx context.Context,
	tx *sql.Tx,
	sessionID, sourceID, revisionID, calculatorVersion string,
) error {
	if tx == nil || sessionID == "" || sourceID == "" || revisionID == "" || calculatorVersion == "" {
		return errors.New("token rollup activation identifiers are required")
	}
	var userID int64
	if err := tx.QueryRowContext(ctx, `SELECT user_id FROM sessions WHERE id = $1`, sessionID).Scan(&userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, fmt.Sprintf("token-family:%d", userID)); err != nil {
		return err
	}

	nodes, err := loadUserSessions(ctx, tx, userID)
	if err != nil {
		return err
	}
	resolved := resolveMemberships(nodes)
	if _, ok := resolved[sessionID]; !ok {
		return fmt.Errorf("activated session %s is missing from family graph", sessionID)
	}
	activeMemberships, oldFamilyMembers, err := loadActiveMemberships(ctx, tx, userID)
	if err != nil {
		return err
	}
	affected := affectedSessions(sessionID, resolved, activeMemberships, oldFamilyMembers)
	affectedRoots := map[string]struct{}{}
	for id := range affected {
		affectedRoots[resolved[id].RootID] = struct{}{}
	}

	families := make(map[string][]string, len(affectedRoots))
	for id, relation := range resolved {
		if _, ok := affectedRoots[relation.RootID]; ok {
			families[relation.RootID] = append(families[relation.RootID], id)
		}
	}
	for root := range families {
		sort.Strings(families[root])
	}

	familyVersions, err := b.activateFamilyVersions(ctx, tx, userID, families, resolved, activeMemberships, oldFamilyMembers)
	if err != nil {
		return err
	}
	roots := make([]string, 0, len(familyVersions))
	for root := range familyVersions {
		roots = append(roots, root)
	}
	sort.Strings(roots)
	for _, root := range roots {
		if err := b.buildRollup(ctx, tx, userID, root, familyVersions[root], families[root],
			sourceID, revisionID, calculatorVersion, resolved); err != nil {
			return err
		}
	}
	return nil
}

type activeMembership struct {
	FamilyVersionID string
	RootID          string
}

func loadUserSessions(ctx context.Context, tx *sql.Tx, userID int64) ([]sessionNode, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id::text, session_ref, agent_type,
			COALESCE(parent_session_ref, ''), COALESCE(fork_source, '')
		FROM sessions
		WHERE user_id = $1 AND content_status <> 'deleted'
		ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	nodes := []sessionNode{}
	for rows.Next() {
		var node sessionNode
		if err := rows.Scan(&node.ID, &node.Ref, &node.AgentType, &node.ParentRef, &node.ForkSource); err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return nodes, rows.Err()
}

func loadActiveMemberships(
	ctx context.Context,
	tx *sql.Tx,
	userID int64,
) (map[string]activeMembership, map[string][]string, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT membership.member_session_id::text, membership.family_version_id::text,
			membership.root_session_id::text
		FROM session_family_memberships membership
		JOIN session_family_versions family ON family.id = membership.family_version_id
		WHERE family.user_id = $1 AND family.status = 'active'
			AND membership.valid_to IS NULL
		ORDER BY membership.member_session_id`, userID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	byMember := map[string]activeMembership{}
	byFamily := map[string][]string{}
	for rows.Next() {
		var memberID string
		var membership activeMembership
		if err := rows.Scan(&memberID, &membership.FamilyVersionID, &membership.RootID); err != nil {
			return nil, nil, err
		}
		byMember[memberID] = membership
		byFamily[membership.FamilyVersionID] = append(byFamily[membership.FamilyVersionID], memberID)
	}
	return byMember, byFamily, rows.Err()
}

func affectedSessions(
	sessionID string,
	resolved map[string]resolvedMembership,
	active map[string]activeMembership,
	oldFamilies map[string][]string,
) map[string]struct{} {
	affected := map[string]struct{}{sessionID: {}}
	queue := []string{sessionID}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		root := resolved[id].RootID
		for candidate, relation := range resolved {
			if relation.RootID == root {
				if _, ok := affected[candidate]; !ok {
					affected[candidate] = struct{}{}
					queue = append(queue, candidate)
				}
			}
		}
		if old, ok := active[id]; ok {
			for _, candidate := range oldFamilies[old.FamilyVersionID] {
				if _, exists := affected[candidate]; !exists {
					affected[candidate] = struct{}{}
					queue = append(queue, candidate)
				}
			}
		}
	}
	return affected
}

func (b *Builder) activateFamilyVersions(
	ctx context.Context,
	tx *sql.Tx,
	userID int64,
	families map[string][]string,
	resolved map[string]resolvedMembership,
	active map[string]activeMembership,
	oldFamilyMembers map[string][]string,
) (map[string]string, error) {
	type desiredFamily struct {
		hash          string
		quality       string
		subagentCount int
		reuseID       string
	}
	desired := make(map[string]desiredFamily, len(families))
	reused := map[string]struct{}{}
	for root, members := range families {
		hash := relationHash(root, members, resolved)
		quality := qualityExact
		subagents := 0
		for _, member := range members {
			relation := resolved[member]
			quality = combineQuality(quality, relation.Quality)
			if relation.Depth > 0 {
				subagents++
			}
		}
		var reuseID string
		if current, ok := active[root]; ok && current.RootID == root {
			var currentHash string
			if err := tx.QueryRowContext(ctx, `
				SELECT relation_hash FROM session_family_versions
				WHERE id = $1 AND status = 'active'`, current.FamilyVersionID).Scan(&currentHash); err == nil && currentHash == hash {
				reuseID = current.FamilyVersionID
				reused[reuseID] = struct{}{}
			} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
		}
		desired[root] = desiredFamily{hash: hash, quality: quality, subagentCount: subagents, reuseID: reuseID}
	}

	touchedOld := map[string]struct{}{}
	for _, members := range families {
		for _, member := range members {
			if old, ok := active[member]; ok {
				touchedOld[old.FamilyVersionID] = struct{}{}
			}
		}
	}
	for familyID := range touchedOld {
		if _, keep := reused[familyID]; keep {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE session_family_memberships SET valid_to = now()
			WHERE family_version_id = $1 AND valid_to IS NULL`, familyID); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE session_family_versions
			SET status = 'superseded', superseded_at = now()
			WHERE id = $1 AND status = 'active'`, familyID); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE session_family_rollup_versions
			SET status = 'superseded', superseded_at = now()
			WHERE family_version_id = $1 AND status = 'active'`, familyID); err != nil {
			return nil, err
		}
		_ = oldFamilyMembers[familyID]
	}

	versions := make(map[string]string, len(families))
	roots := make([]string, 0, len(families))
	for root := range families {
		roots = append(roots, root)
	}
	sort.Strings(roots)
	for _, root := range roots {
		definition := desired[root]
		if definition.reuseID != "" {
			versions[root] = definition.reuseID
			continue
		}
		var familyID string
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO session_family_versions (
				user_id, root_session_id, relation_hash, status, quality_status,
				member_count, subagent_count, activated_at
			) VALUES ($1, $2, $3, 'active', $4, $5, $6, now())
			RETURNING id`, userID, root, definition.hash, definition.quality,
			len(families[root]), definition.subagentCount).Scan(&familyID); err != nil {
			return nil, err
		}
		for _, member := range families[root] {
			relation := resolved[member]
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO session_family_memberships (
					family_version_id, root_session_id, member_session_id,
					parent_session_id, depth, relation_source, quality_status
				) VALUES ($1, $2, $3, NULLIF($4, '')::uuid, $5, $6, $7)`,
				familyID, root, member, relation.ParentID, relation.Depth,
				relation.RelationSource, relation.Quality); err != nil {
				return nil, err
			}
		}
		versions[root] = familyID
	}
	return versions, nil
}

func relationHash(root string, members []string, resolved map[string]resolvedMembership) string {
	parts := make([]string, 0, len(members)+1)
	parts = append(parts, "root="+root)
	for _, member := range members {
		relation := resolved[member]
		parts = append(parts, strings.Join([]string{
			member, relation.ParentID, fmt.Sprintf("%d", relation.Depth),
			relation.RelationSource, relation.Quality,
		}, "|"))
	}
	return hashStrings(parts)
}

func hashStrings(parts []string) string {
	hash := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(hash[:])
}

func combineQuality(left, right string) string {
	if left == qualityConflict || right == qualityConflict {
		return qualityConflict
	}
	if left == qualityPending || right == qualityPending || left == "incomplete" || right == "incomplete" {
		return qualityPending
	}
	if left == "estimated" || right == "estimated" {
		return "estimated"
	}
	return qualityExact
}

type activeRevision struct {
	SourceID        string
	RevisionID      string
	GenerationID    string
	ValidatedCursor int64
	HighWaterCursor int64
}

func (b *Builder) buildRollup(
	ctx context.Context,
	tx *sql.Tx,
	userID int64,
	rootID, familyVersionID string,
	members []string,
	overrideSourceID, overrideRevisionID, calculatorVersion string,
	resolved map[string]resolvedMembership,
) error {
	revisions, err := loadActiveRevisions(ctx, tx, members, overrideSourceID, overrideRevisionID)
	if err != nil {
		return err
	}
	revisionIDs := make([]string, 0, len(revisions))
	revisionParts := make([]string, 0, len(revisions))
	for _, revision := range revisions {
		revisionIDs = append(revisionIDs, revision.RevisionID)
		revisionParts = append(revisionParts, fmt.Sprintf("%s|%s|%s|%d|%d",
			revision.SourceID, revision.RevisionID, revision.GenerationID,
			revision.ValidatedCursor, revision.HighWaterCursor))
	}
	revisionHash := hashStrings(revisionParts)
	costHash, contributionCount, dataThrough, contributionQuality, err := contributionSetMetadata(
		ctx, tx, revisionIDs, calculatorVersion)
	if err != nil {
		return err
	}
	rollupQuality := contributionQuality
	for _, member := range members {
		rollupQuality = combineQuality(rollupQuality, resolved[member].Quality)
	}

	var existingID string
	err = tx.QueryRowContext(ctx, `
		SELECT id::text
		FROM session_family_rollup_versions
		WHERE family_version_id = $1 AND revision_set_hash = $2
			AND cost_set_hash = $3 AND calculator_version = $4
			AND status = 'active'`, familyVersionID, revisionHash, costHash, calculatorVersion).Scan(&existingID)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	var rollupID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO session_family_rollup_versions (
			family_version_id, root_session_id, revision_set_hash, cost_set_hash,
			calculator_version, status, quality_status, member_count,
			source_count, contribution_count, data_through_at
		) VALUES ($1, $2, $3, $4, $5, 'building', $6, $7, $8, $9, $10)
		RETURNING id`, familyVersionID, rootID, revisionHash, costHash, calculatorVersion,
		rollupQuality, len(members), len(revisions), contributionCount, dataThrough).Scan(&rollupID); err != nil {
		return err
	}
	for _, revision := range revisions {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO session_family_rollup_revision_refs (
				rollup_version_id, source_id, revision_id, generation_id,
				validated_through_cursor, source_high_water_cursor
			) VALUES ($1, $2, $3, $4, $5, $6)`, rollupID, revision.SourceID,
			revision.RevisionID, revision.GenerationID, revision.ValidatedCursor,
			revision.HighWaterCursor); err != nil {
			return err
		}
	}
	if len(revisionIDs) > 0 {
		if err := insertFamilyTotals(ctx, tx, rollupID, rootID, userID, revisionIDs, calculatorVersion, rollupQuality); err != nil {
			return err
		}
		if err := insertFamilyDaily(ctx, tx, rollupID, rootID, userID, revisionIDs, calculatorVersion, rollupQuality); err != nil {
			return err
		}
		if err := insertChunkUsage(ctx, tx, rollupID, rootID, userID, revisionIDs, calculatorVersion, rollupQuality); err != nil {
			return err
		}
	}
	if err := validateRollup(ctx, tx, rollupID, revisionIDs, contributionCount); err != nil {
		if _, updateErr := tx.ExecContext(ctx, `
			UPDATE session_family_rollup_versions
			SET status = 'failed', failure_reason = $2 WHERE id = $1`, rollupID, err.Error()); updateErr != nil {
			return updateErr
		}
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_family_rollup_versions
		SET status = 'superseded', superseded_at = now()
		WHERE root_session_id = $1 AND status = 'active'`, rootID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE session_family_rollup_versions
		SET status = 'active', activated_at = now()
		WHERE id = $1 AND status = 'building'`, rollupID)
	return err
}

func loadActiveRevisions(
	ctx context.Context,
	tx *sql.Tx,
	members []string,
	overrideSourceID, overrideRevisionID string,
) ([]activeRevision, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT source.id::text, revision.id::text, revision.generation_id::text,
			revision.validated_through_cursor, revision.source_high_water_cursor
		FROM session_sources source
		LEFT JOIN session_source_metrics_states state ON state.source_id = source.id
		JOIN session_metrics_revisions target_revision ON target_revision.id = $3::uuid
		JOIN session_metrics_revisions revision ON revision.id = CASE
			WHEN source.id = $2::uuid THEN $3::uuid
			ELSE state.active_revision_id
		END
		WHERE source.session_id = ANY($1::uuid[]) AND revision.status = 'active'
			AND revision.parser_version = target_revision.parser_version
			AND revision.normalizer_version = target_revision.normalizer_version
		ORDER BY source.id, revision.id`, pq.Array(members), overrideSourceID, overrideRevisionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	revisions := []activeRevision{}
	for rows.Next() {
		var revision activeRevision
		if err := rows.Scan(&revision.SourceID, &revision.RevisionID, &revision.GenerationID,
			&revision.ValidatedCursor, &revision.HighWaterCursor); err != nil {
			return nil, err
		}
		revisions = append(revisions, revision)
	}
	return revisions, rows.Err()
}

func contributionSetMetadata(
	ctx context.Context,
	tx *sql.Tx,
	revisionIDs []string,
	calculatorVersion string,
) (hash string, count int64, dataThrough any, quality string, err error) {
	if len(revisionIDs) == 0 {
		return hashStrings(nil), 0, nil, qualityExact, nil
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT contribution.id::text, COALESCE(cost.id::text, ''),
			contribution.quality_status, contribution.is_estimated,
			contribution.occurred_at
		FROM session_usage_contributions contribution
		LEFT JOIN session_usage_contribution_costs cost
			ON cost.contribution_id = contribution.id
			AND cost.calculator_version = $2 AND cost.superseded_at IS NULL
		WHERE contribution.revision_id = ANY($1::uuid[])
		ORDER BY contribution.id`, pq.Array(revisionIDs), calculatorVersion)
	if err != nil {
		return "", 0, nil, "", err
	}
	defer rows.Close()
	parts := []string{}
	quality = qualityExact
	var latest sql.NullTime
	for rows.Next() {
		var contributionID, costID, itemQuality string
		var estimated bool
		var occurred sql.NullTime
		if err := rows.Scan(&contributionID, &costID, &itemQuality, &estimated, &occurred); err != nil {
			return "", 0, nil, "", err
		}
		if costID == "" {
			return "", 0, nil, "", fmt.Errorf("contribution %s has no active cost record", contributionID)
		}
		parts = append(parts, contributionID+"|"+costID)
		count++
		quality = combineQuality(quality, itemQuality)
		if estimated {
			quality = combineQuality(quality, "estimated")
		}
		if occurred.Valid && (!latest.Valid || occurred.Time.After(latest.Time)) {
			latest = occurred
		}
	}
	if err := rows.Err(); err != nil {
		return "", 0, nil, "", err
	}
	if latest.Valid {
		dataThrough = latest.Time
	}
	return hashStrings(parts), count, dataThrough, quality, nil
}

const aggregatePricingStatus = `CASE
	WHEN COUNT(*) FILTER (WHERE cost.id IS NULL) > 0 THEN 'pricing_pending'
	WHEN COUNT(*) FILTER (WHERE cost.pricing_status = 'priced') = COUNT(*) THEN 'priced'
	WHEN COUNT(*) FILTER (WHERE cost.pricing_status = 'priced') > 0 THEN 'partially_priced'
	ELSE 'unpriced'
END`

const aggregateQualityStatus = `CASE
	WHEN BOOL_OR(contribution.quality_status = 'conflict') THEN 'conflict'
	WHEN BOOL_OR(contribution.quality_status = 'incomplete') THEN 'pending'
	WHEN BOOL_OR(contribution.is_estimated OR contribution.quality_status = 'estimated') THEN 'estimated'
	ELSE $6
END`

const aggregateCanonicalModel = `COALESCE(
	NULLIF(cost.unit_price_snapshot_json->>'canonical_model', ''),
	NULLIF(cost.assumptions_json->>'canonical_model', ''),
	NULLIF(cost.assumptions_json->>'model', ''),
	NULLIF(contribution.canonical_model, ''), NULLIF(contribution.raw_model, ''), 'unknown'
)`

func insertFamilyTotals(ctx context.Context, tx *sql.Tx, rollupID, rootID string, userID int64, revisions []string, calculatorVersion, fallbackQuality string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO session_family_token_totals (
			rollup_version_id, root_session_id, user_id, team_id_snapshot,
			department_id_snapshot, provider, canonical_model, billing_variant,
			uncached_input_tokens, cache_read_tokens, cache_write_5m_tokens,
			cache_write_1h_tokens, output_tokens, total_tokens,
			self_total_tokens, subagent_total_tokens, estimated_cost_usd,
			estimated_cost_cny, pricing_status, contribution_count,
			unpriced_contribution_count, quality_status
		)
		SELECT $1, $2, $3, contribution.team_id_snapshot, contribution.department_id_snapshot,
			contribution.provider, `+aggregateCanonicalModel+`, contribution.billing_variant,
			SUM(contribution.uncached_input_tokens), SUM(contribution.cache_read_tokens),
			SUM(contribution.cache_write_5m_tokens), SUM(contribution.cache_write_1h_tokens),
			SUM(contribution.output_tokens), SUM(contribution.total_tokens),
			COALESCE(SUM(contribution.total_tokens) FILTER (WHERE contribution.member_session_id = $2::uuid), 0),
			COALESCE(SUM(contribution.total_tokens) FILTER (WHERE contribution.member_session_id <> $2::uuid), 0),
			SUM(cost.estimated_cost_usd), SUM(cost.estimated_cost_cny),
			`+aggregatePricingStatus+`,
			COUNT(*),
			COUNT(*) FILTER (WHERE cost.pricing_status <> 'priced' OR cost.id IS NULL),
			`+aggregateQualityStatus+`
		FROM session_usage_contributions contribution
		LEFT JOIN session_usage_contribution_costs cost
			ON cost.contribution_id = contribution.id
			AND cost.calculator_version = $5 AND cost.superseded_at IS NULL
		WHERE contribution.revision_id = ANY($4::uuid[])
		GROUP BY contribution.team_id_snapshot, contribution.department_id_snapshot,
			contribution.provider, `+aggregateCanonicalModel+`, contribution.billing_variant`,
		rollupID, rootID, userID, pq.Array(revisions), calculatorVersion, fallbackQuality)
	return err
}

func insertFamilyDaily(ctx context.Context, tx *sql.Tx, rollupID, rootID string, userID int64, revisions []string, calculatorVersion, fallbackQuality string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO session_family_daily_usage (
			rollup_version_id, root_session_id, user_id, team_id_snapshot,
			department_id_snapshot, activity_date, activity_start_at, activity_end_at,
			provider, canonical_model, billing_variant,
			uncached_input_tokens, cache_read_tokens, cache_write_5m_tokens,
			cache_write_1h_tokens, output_tokens, total_tokens,
			self_total_tokens, subagent_total_tokens, estimated_cost_usd,
			estimated_cost_cny, pricing_status, contribution_count,
			unpriced_contribution_count, quality_status
		)
		SELECT $1, $2, $3, contribution.team_id_snapshot, contribution.department_id_snapshot,
			contribution.activity_date, MIN(contribution.occurred_at), MAX(contribution.occurred_at),
			contribution.provider, `+aggregateCanonicalModel+`,
			contribution.billing_variant, SUM(contribution.uncached_input_tokens),
			SUM(contribution.cache_read_tokens), SUM(contribution.cache_write_5m_tokens),
			SUM(contribution.cache_write_1h_tokens), SUM(contribution.output_tokens),
			SUM(contribution.total_tokens),
			COALESCE(SUM(contribution.total_tokens) FILTER (WHERE contribution.member_session_id = $2::uuid), 0),
			COALESCE(SUM(contribution.total_tokens) FILTER (WHERE contribution.member_session_id <> $2::uuid), 0),
			SUM(cost.estimated_cost_usd),
			SUM(cost.estimated_cost_cny), `+aggregatePricingStatus+`,
			COUNT(*),
			COUNT(*) FILTER (WHERE cost.pricing_status <> 'priced' OR cost.id IS NULL),
			`+aggregateQualityStatus+`
		FROM session_usage_contributions contribution
		LEFT JOIN session_usage_contribution_costs cost
			ON cost.contribution_id = contribution.id
			AND cost.calculator_version = $5 AND cost.superseded_at IS NULL
		WHERE contribution.revision_id = ANY($4::uuid[])
		GROUP BY contribution.team_id_snapshot, contribution.department_id_snapshot,
			contribution.activity_date, contribution.provider,
			`+aggregateCanonicalModel+`, contribution.billing_variant`,
		rollupID, rootID, userID, pq.Array(revisions), calculatorVersion, fallbackQuality)
	return err
}

func insertChunkUsage(ctx context.Context, tx *sql.Tx, rollupID, rootID string, userID int64, revisions []string, calculatorVersion, fallbackQuality string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO session_chunk_usage (
			rollup_version_id, root_session_id, member_session_id, chunk_id, user_id,
			team_id_snapshot, department_id_snapshot, activity_date, provider,
			canonical_model, billing_variant, uncached_input_tokens, cache_read_tokens,
			cache_write_5m_tokens, cache_write_1h_tokens, output_tokens, total_tokens,
			estimated_cost_usd, estimated_cost_cny, pricing_status,
			contribution_count, unpriced_contribution_count, quality_status
		)
		SELECT $1, $2, contribution.member_session_id, contribution.chunk_id, $3,
			contribution.team_id_snapshot, contribution.department_id_snapshot,
			contribution.activity_date, contribution.provider, `+aggregateCanonicalModel+`,
			contribution.billing_variant, SUM(contribution.uncached_input_tokens),
			SUM(contribution.cache_read_tokens), SUM(contribution.cache_write_5m_tokens),
			SUM(contribution.cache_write_1h_tokens), SUM(contribution.output_tokens),
			SUM(contribution.total_tokens), SUM(cost.estimated_cost_usd),
			SUM(cost.estimated_cost_cny), `+aggregatePricingStatus+`,
			COUNT(*),
			COUNT(*) FILTER (WHERE cost.pricing_status <> 'priced' OR cost.id IS NULL),
			`+aggregateQualityStatus+`
		FROM session_usage_contributions contribution
		LEFT JOIN session_usage_contribution_costs cost
			ON cost.contribution_id = contribution.id
			AND cost.calculator_version = $5 AND cost.superseded_at IS NULL
		WHERE contribution.revision_id = ANY($4::uuid[])
		GROUP BY contribution.member_session_id, contribution.chunk_id,
			contribution.team_id_snapshot, contribution.department_id_snapshot,
			contribution.activity_date, contribution.provider,
			`+aggregateCanonicalModel+`, contribution.billing_variant`,
		rollupID, rootID, userID, pq.Array(revisions), calculatorVersion, fallbackQuality)
	return err
}

type tokenVector struct {
	Uncached  int64
	CacheRead int64
	Cache5m   int64
	Cache1h   int64
	Output    int64
	Total     int64
}

func (v tokenVector) equal(other tokenVector) bool {
	return v == other
}

func validateRollup(ctx context.Context, tx *sql.Tx, rollupID string, revisions []string, expectedCount int64) error {
	var revisionRefCount, expectedRevisionRefCount int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*), COUNT(*) FILTER (WHERE revision_id = ANY($2::uuid[]))
		FROM session_family_rollup_revision_refs WHERE rollup_version_id = $1`,
		rollupID, pq.Array(revisions)).Scan(&revisionRefCount, &expectedRevisionRefCount); err != nil {
		return err
	}
	if revisionRefCount != int64(len(revisions)) || expectedRevisionRefCount != int64(len(revisions)) {
		return fmt.Errorf("token rollup revision refs mismatch: rows=%d matched=%d expected=%d",
			revisionRefCount, expectedRevisionRefCount, len(revisions))
	}
	contribution, contributionCount, err := sumContributions(ctx, tx, revisions)
	if err != nil {
		return err
	}
	if contributionCount != expectedCount {
		return fmt.Errorf("token rollup contribution count mismatch: got=%d expected=%d", contributionCount, expectedCount)
	}
	component, err := sumComponents(ctx, tx, revisions)
	if err != nil {
		return err
	}
	if !contribution.equal(component) {
		return fmt.Errorf("token contribution/component reconciliation failed: contribution=%+v component=%+v", contribution, component)
	}
	for _, table := range []string{"session_family_token_totals", "session_family_daily_usage", "session_chunk_usage"} {
		rollup, err := sumRollupTable(ctx, tx, table, rollupID)
		if err != nil {
			return err
		}
		if !contribution.equal(rollup) {
			return fmt.Errorf("token rollup reconciliation failed for %s: contribution=%+v rollup=%+v", table, contribution, rollup)
		}
		var rollupContributionCount int64
		if err := tx.QueryRowContext(ctx, fmt.Sprintf(
			"SELECT COALESCE(SUM(contribution_count), 0) FROM %s WHERE rollup_version_id = $1", table),
			rollupID).Scan(&rollupContributionCount); err != nil {
			return err
		}
		if rollupContributionCount != contributionCount {
			return fmt.Errorf("token rollup contribution count mismatch for %s: got=%d expected=%d",
				table, rollupContributionCount, contributionCount)
		}
	}
	var selfTotal, subagentTotal, familyTotal int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(self_total_tokens), 0),
			COALESCE(SUM(subagent_total_tokens), 0), COALESCE(SUM(total_tokens), 0)
		FROM session_family_token_totals WHERE rollup_version_id = $1`, rollupID).Scan(
		&selfTotal, &subagentTotal, &familyTotal); err != nil {
		return err
	}
	if selfTotal+subagentTotal != familyTotal {
		return fmt.Errorf("token family self/subagent reconciliation failed: self=%d subagent=%d family=%d",
			selfTotal, subagentTotal, familyTotal)
	}
	return validateCostSums(ctx, tx, rollupID)
}

func sumContributions(ctx context.Context, tx *sql.Tx, revisions []string) (tokenVector, int64, error) {
	if len(revisions) == 0 {
		return tokenVector{}, 0, nil
	}
	var vector tokenVector
	var count int64
	err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(uncached_input_tokens), 0), COALESCE(SUM(cache_read_tokens), 0),
			COALESCE(SUM(cache_write_5m_tokens), 0), COALESCE(SUM(cache_write_1h_tokens), 0),
			COALESCE(SUM(output_tokens), 0), COALESCE(SUM(total_tokens), 0), COUNT(*)
		FROM session_usage_contributions WHERE revision_id = ANY($1::uuid[])`, pq.Array(revisions)).Scan(
		&vector.Uncached, &vector.CacheRead, &vector.Cache5m, &vector.Cache1h,
		&vector.Output, &vector.Total, &count)
	return vector, count, err
}

func sumComponents(ctx context.Context, tx *sql.Tx, revisions []string) (tokenVector, error) {
	if len(revisions) == 0 {
		return tokenVector{}, nil
	}
	var vector tokenVector
	err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(uncached_input_tokens), 0), COALESCE(SUM(cache_read_tokens), 0),
			COALESCE(SUM(cache_write_5m_tokens), 0), COALESCE(SUM(cache_write_1h_tokens), 0),
			COALESCE(SUM(output_tokens), 0), COALESCE(SUM(normalized_total_tokens), 0)
		FROM session_usage_components
		WHERE revision_id = ANY($1::uuid[]) AND valid_to IS NULL`, pq.Array(revisions)).Scan(
		&vector.Uncached, &vector.CacheRead, &vector.Cache5m, &vector.Cache1h,
		&vector.Output, &vector.Total)
	return vector, err
}

func sumRollupTable(ctx context.Context, tx *sql.Tx, table, rollupID string) (tokenVector, error) {
	allowed := map[string]bool{
		"session_family_token_totals": true,
		"session_family_daily_usage":  true,
		"session_chunk_usage":         true,
	}
	if !allowed[table] {
		return tokenVector{}, fmt.Errorf("unsupported token rollup table %q", table)
	}
	var vector tokenVector
	query := fmt.Sprintf(`
		SELECT COALESCE(SUM(uncached_input_tokens), 0), COALESCE(SUM(cache_read_tokens), 0),
			COALESCE(SUM(cache_write_5m_tokens), 0), COALESCE(SUM(cache_write_1h_tokens), 0),
			COALESCE(SUM(output_tokens), 0), COALESCE(SUM(total_tokens), 0)
		FROM %s WHERE rollup_version_id = $1`, table)
	err := tx.QueryRowContext(ctx, query, rollupID).Scan(
		&vector.Uncached, &vector.CacheRead, &vector.Cache5m, &vector.Cache1h,
		&vector.Output, &vector.Total)
	return vector, err
}

func validateCostSums(ctx context.Context, tx *sql.Tx, rollupID string) error {
	var familyUSD, familyCNY, dailyUSD, dailyCNY, chunkUSD, chunkCNY string
	err := tx.QueryRowContext(ctx, `
		SELECT
			COALESCE((SELECT SUM(estimated_cost_usd) FROM session_family_token_totals WHERE rollup_version_id = $1), 0)::text,
			COALESCE((SELECT SUM(estimated_cost_cny) FROM session_family_token_totals WHERE rollup_version_id = $1), 0)::text,
			COALESCE((SELECT SUM(estimated_cost_usd) FROM session_family_daily_usage WHERE rollup_version_id = $1), 0)::text,
			COALESCE((SELECT SUM(estimated_cost_cny) FROM session_family_daily_usage WHERE rollup_version_id = $1), 0)::text,
			COALESCE((SELECT SUM(estimated_cost_usd) FROM session_chunk_usage WHERE rollup_version_id = $1), 0)::text,
			COALESCE((SELECT SUM(estimated_cost_cny) FROM session_chunk_usage WHERE rollup_version_id = $1), 0)::text`, rollupID).Scan(
		&familyUSD, &familyCNY, &dailyUSD, &dailyCNY, &chunkUSD, &chunkCNY)
	if err != nil {
		return err
	}
	if familyUSD != dailyUSD || familyUSD != chunkUSD || familyCNY != dailyCNY || familyCNY != chunkCNY {
		return fmt.Errorf("token rollup cost reconciliation failed: family=%s/%s daily=%s/%s chunk=%s/%s",
			familyUSD, familyCNY, dailyUSD, dailyCNY, chunkUSD, chunkCNY)
	}
	return nil
}
