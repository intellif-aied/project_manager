package handler

import (
	"database/sql"
	"encoding/json"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aidashboard/api/internal/pricing"
)

type PricingAdminHandler struct {
	db      *sql.DB
	service *pricing.Service
}

type savePriceBookRequest struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type saveModelAliasRequest struct {
	Provider        string `json:"provider"`
	RawModelPattern string `json:"raw_model_pattern"`
	CanonicalModel  string `json:"canonical_model"`
	Status          string `json:"status"`
}

type saveModelPriceRequest struct {
	ID                     string `json:"id"`
	PriceBookID            string `json:"price_book_id"`
	CanonicalModel         string `json:"canonical_model"`
	BillingVariant         string `json:"billing_variant"`
	InputPerMillion        string `json:"input_per_million"`
	CacheReadPerMillion    string `json:"cache_read_per_million"`
	CacheWrite5mPerMillion string `json:"cache_write_5m_per_million"`
	CacheWrite1hPerMillion string `json:"cache_write_1h_per_million"`
	OutputPerMillion       string `json:"output_per_million"`
	EffectiveFrom          string `json:"effective_from"`
	EffectiveTo            string `json:"effective_to"`
	SourceURL              string `json:"source_url"`
	SourceCheckedAt        string `json:"source_checked_at"`
	Notes                  string `json:"notes"`
	Status                 string `json:"status"`
	SupersedesID           string `json:"supersedes_id"`
}

type saveExchangeRateRequest struct {
	ID              string `json:"id"`
	Rate            string `json:"rate"`
	EffectiveFrom   string `json:"effective_from"`
	EffectiveTo     string `json:"effective_to"`
	SourceURL       string `json:"source_url"`
	SourceCheckedAt string `json:"source_checked_at"`
	Notes           string `json:"notes"`
	Status          string `json:"status"`
	SupersedesID    string `json:"supersedes_id"`
}

type recalculatePricingRequest struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Model  string `json:"model"`
	Reason string `json:"reason"`
}

type importPricingSuggestionsRequest struct {
	Aliases []saveModelAliasRequest `json:"aliases"`
}

func NewPricingAdminHandler(db *sql.DB, service *pricing.Service) *PricingAdminHandler {
	return &PricingAdminHandler{db: db, service: service}
}

func (h *PricingAdminHandler) ListPriceBooks(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.admin(w, r); !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT id::text, name, pricing_basis, source_currency, display_currency, status,
			created_by::text, created_at, updated_at
		FROM price_books ORDER BY created_at DESC`)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, name, basis, sourceCurrency, displayCurrency, status string
		var createdBy sql.NullString
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &name, &basis, &sourceCurrency, &displayCurrency, &status, &createdBy, &createdAt, &updatedAt); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		items = append(items, map[string]any{
			"id": id, "name": name, "pricing_basis": basis,
			"source_currency": sourceCurrency, "display_currency": displayCurrency,
			"status": status, "created_by": nullableSQLString(createdBy),
			"created_at": createdAt, "updated_at": updatedAt,
		})
	}
	writeJSON(w, 200, items)
}

func (h *PricingAdminHandler) SavePriceBook(w http.ResponseWriter, r *http.Request) {
	adminID, ok := h.admin(w, r)
	if !ok {
		return
	}
	var req savePriceBookRequest
	if readJSON(r, &req) != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request body"})
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Status = strings.TrimSpace(req.Status)
	if req.Name == "" || (req.Status != "draft" && req.Status != "active" && req.Status != "archived") {
		writeJSON(w, 400, map[string]string{"error": "invalid price book"})
		return
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer tx.Rollback()
	if req.Status == "active" {
		if _, err = tx.ExecContext(r.Context(), `UPDATE price_books SET status='archived', updated_at=now() WHERE status='active' AND ($1='' OR id::text <> $1)`, req.ID); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
	}
	var id string
	if strings.TrimSpace(req.ID) == "" {
		err = tx.QueryRowContext(r.Context(), `
			INSERT INTO price_books(name, status, created_by) VALUES($1, $2, $3)
			RETURNING id::text`, req.Name, req.Status, adminID).Scan(&id)
	} else {
		err = tx.QueryRowContext(r.Context(), `
			UPDATE price_books SET name=$2, status=$3, updated_at=now()
			WHERE id::text=$1 RETURNING id::text`, req.ID, req.Name, req.Status).Scan(&id)
	}
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if err := tx.Commit(); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"id": id})
}

func (h *PricingAdminHandler) ListModelAliases(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.admin(w, r); !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT id::text, provider, raw_model_pattern, canonical_model, status,
			reviewed_by::text, reviewed_at, created_at, updated_at
		FROM model_aliases ORDER BY provider, raw_model_pattern`)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, provider, rawModel, canonical, status string
		var reviewedBy sql.NullString
		var reviewedAt sql.NullTime
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &provider, &rawModel, &canonical, &status, &reviewedBy, &reviewedAt, &createdAt, &updatedAt); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		items = append(items, map[string]any{
			"id": id, "provider": provider, "raw_model_pattern": rawModel,
			"canonical_model": canonical, "status": status,
			"reviewed_by": nullableSQLString(reviewedBy), "reviewed_at": nullableSQLTime(reviewedAt),
			"created_at": createdAt, "updated_at": updatedAt,
		})
	}
	writeJSON(w, 200, items)
}

func (h *PricingAdminHandler) SaveModelAlias(w http.ResponseWriter, r *http.Request) {
	adminID, ok := h.admin(w, r)
	if !ok {
		return
	}
	var req saveModelAliasRequest
	if readJSON(r, &req) != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request body"})
		return
	}
	req.Provider = strings.TrimSpace(req.Provider)
	req.RawModelPattern = strings.TrimSpace(req.RawModelPattern)
	req.CanonicalModel = strings.TrimSpace(req.CanonicalModel)
	req.Status = strings.TrimSpace(req.Status)
	if req.Provider == "" || req.RawModelPattern == "" || req.CanonicalModel == "" ||
		(req.Status != "pending" && req.Status != "reviewed" && req.Status != "rejected") {
		writeJSON(w, 400, map[string]string{"error": "invalid model alias"})
		return
	}
	var id string
	err := h.db.QueryRowContext(r.Context(), `
		INSERT INTO model_aliases(
			provider, raw_model_pattern, canonical_model, status,
			reviewed_by, reviewed_at, created_by
		) VALUES (
			$1, $2, $3, $4,
			CASE WHEN $4='reviewed' THEN $5::bigint END,
			CASE WHEN $4='reviewed' THEN now() END, $5
		)
		ON CONFLICT(provider, raw_model_pattern) DO UPDATE
		SET canonical_model=EXCLUDED.canonical_model, status=EXCLUDED.status,
			reviewed_by=EXCLUDED.reviewed_by, reviewed_at=EXCLUDED.reviewed_at,
			updated_at=now()
		RETURNING id::text`, req.Provider, req.RawModelPattern, req.CanonicalModel, req.Status, adminID).Scan(&id)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"id": id})
}

func (h *PricingAdminHandler) ListModelPrices(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.admin(w, r); !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT version.id::text, version.price_book_id::text, book.name,
			version.canonical_model, version.billing_variant,
			version.input_per_million::text, version.cache_read_per_million::text,
			version.cache_write_5m_per_million::text, version.cache_write_1h_per_million::text,
			version.output_per_million::text, version.effective_from::text, version.effective_to::text,
			version.source_url, version.source_checked_at, version.notes, version.status,
			version.published_by::text, version.published_at,
			version.supersedes_id::text, version.superseded_at, version.created_at
		FROM model_price_versions version
		JOIN price_books book ON book.id = version.price_book_id
		ORDER BY version.canonical_model, version.effective_from DESC`)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, bookID, bookName, model, variant string
		var input, read, write5m, write1h, output, from, status string
		var to, sourceURL, notes, publishedBy, supersedesID sql.NullString
		var checkedAt, publishedAt, supersededAt sql.NullTime
		var createdAt time.Time
		if err := rows.Scan(&id, &bookID, &bookName, &model, &variant, &input, &read,
			&write5m, &write1h, &output, &from, &to, &sourceURL, &checkedAt, &notes,
			&status, &publishedBy, &publishedAt, &supersedesID, &supersededAt, &createdAt); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		items = append(items, map[string]any{
			"id": id, "price_book_id": bookID, "price_book_name": bookName,
			"canonical_model": model, "billing_variant": variant,
			"input_per_million": input, "cache_read_per_million": read,
			"cache_write_5m_per_million": write5m, "cache_write_1h_per_million": write1h,
			"output_per_million": output, "effective_from": from, "effective_to": nullableSQLString(to),
			"source_url": nullableSQLString(sourceURL), "source_checked_at": nullableSQLTime(checkedAt),
			"notes": nullableSQLString(notes), "status": status,
			"published_by": nullableSQLString(publishedBy), "published_at": nullableSQLTime(publishedAt),
			"supersedes_id": nullableSQLString(supersedesID), "superseded_at": nullableSQLTime(supersededAt),
			"created_at": createdAt,
		})
	}
	writeJSON(w, 200, items)
}

func (h *PricingAdminHandler) SaveModelPrice(w http.ResponseWriter, r *http.Request) {
	adminID, ok := h.admin(w, r)
	if !ok {
		return
	}
	var req saveModelPriceRequest
	if readJSON(r, &req) != nil || !validModelPriceRequest(req) {
		writeJSON(w, 400, map[string]string{"error": "invalid model price version"})
		return
	}
	checkedAt, err := optionalRFC3339(req.SourceCheckedAt)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid source_checked_at"})
		return
	}
	if req.SupersedesID != "" && (req.Status != "published" || strings.TrimSpace(req.ID) != "") {
		writeJSON(w, 400, map[string]string{"error": "a correction must publish a new superseding version"})
		return
	}
	tx, err := h.db.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer tx.Rollback()
	if req.SupersedesID != "" {
		result, updateErr := tx.ExecContext(r.Context(), `
			UPDATE model_price_versions SET superseded_at=now()
			WHERE id::text=$1 AND status='published' AND superseded_at IS NULL
				AND price_book_id::text=$2 AND canonical_model=$3 AND billing_variant=$4
				AND effective_from=$5::date
				AND effective_to IS NOT DISTINCT FROM NULLIF($6, '')::date`,
			req.SupersedesID, req.PriceBookID, strings.TrimSpace(req.CanonicalModel),
			normalizedVariant(req.BillingVariant), req.EffectiveFrom, req.EffectiveTo)
		if updateErr != nil {
			writeJSON(w, 400, map[string]string{"error": updateErr.Error()})
			return
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			writeJSON(w, 400, map[string]string{"error": "superseded price version does not match the correction interval"})
			return
		}
	}
	var id string
	if strings.TrimSpace(req.ID) == "" {
		err = tx.QueryRowContext(r.Context(), `
			INSERT INTO model_price_versions(
				price_book_id, canonical_model, billing_variant,
				input_per_million, cache_read_per_million,
				cache_write_5m_per_million, cache_write_1h_per_million, output_per_million,
				effective_from, effective_to, source_url, source_checked_at, notes,
				status, published_by, published_at, supersedes_id
			) VALUES (
				$1, $2, $3, $4::numeric, $5::numeric, $6::numeric, $7::numeric, $8::numeric,
				$9::date, NULLIF($10, '')::date, NULLIF($11, ''), $12, NULLIF($13, ''),
				$14, CASE WHEN $14='published' THEN $15::bigint END,
				CASE WHEN $14='published' THEN now() END, NULLIF($16, '')::uuid
			) RETURNING id::text`, req.PriceBookID, strings.TrimSpace(req.CanonicalModel), normalizedVariant(req.BillingVariant),
			req.InputPerMillion, req.CacheReadPerMillion, req.CacheWrite5mPerMillion,
			req.CacheWrite1hPerMillion, req.OutputPerMillion, req.EffectiveFrom, req.EffectiveTo,
			req.SourceURL, checkedAt, req.Notes, req.Status, adminID, req.SupersedesID).Scan(&id)
	} else {
		err = tx.QueryRowContext(r.Context(), `
			UPDATE model_price_versions SET
				price_book_id=$2, canonical_model=$3, billing_variant=$4,
				input_per_million=$5::numeric, cache_read_per_million=$6::numeric,
				cache_write_5m_per_million=$7::numeric, cache_write_1h_per_million=$8::numeric,
				output_per_million=$9::numeric, effective_from=$10::date,
				effective_to=NULLIF($11, '')::date, source_url=NULLIF($12, ''),
				source_checked_at=$13, notes=NULLIF($14, ''), status=$15,
				published_by=CASE WHEN $15='published' THEN $16::bigint END,
				published_at=CASE WHEN $15='published' THEN now() END
			WHERE id::text=$1 AND status='draft'
			RETURNING id::text`, req.ID, req.PriceBookID, strings.TrimSpace(req.CanonicalModel), normalizedVariant(req.BillingVariant),
			req.InputPerMillion, req.CacheReadPerMillion, req.CacheWrite5mPerMillion,
			req.CacheWrite1hPerMillion, req.OutputPerMillion, req.EffectiveFrom, req.EffectiveTo,
			req.SourceURL, checkedAt, req.Notes, req.Status, adminID).Scan(&id)
	}
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if err := tx.Commit(); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"id": id})
}

func (h *PricingAdminHandler) ListExchangeRates(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.admin(w, r); !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT id::text, rate::text, effective_from::text, effective_to::text,
			source_url, source_checked_at, notes, status,
			published_by::text, published_at, supersedes_id::text, superseded_at, created_at
		FROM usd_cny_rate_versions ORDER BY effective_from DESC`)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, rate, from, status string
		var to, sourceURL, notes, publishedBy, supersedesID sql.NullString
		var checkedAt, publishedAt, supersededAt sql.NullTime
		var createdAt time.Time
		if err := rows.Scan(&id, &rate, &from, &to, &sourceURL, &checkedAt, &notes,
			&status, &publishedBy, &publishedAt, &supersedesID, &supersededAt, &createdAt); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		items = append(items, map[string]any{
			"id": id, "rate": rate, "effective_from": from, "effective_to": nullableSQLString(to),
			"source_url": nullableSQLString(sourceURL), "source_checked_at": nullableSQLTime(checkedAt),
			"notes": nullableSQLString(notes), "status": status,
			"published_by": nullableSQLString(publishedBy), "published_at": nullableSQLTime(publishedAt),
			"supersedes_id": nullableSQLString(supersedesID), "superseded_at": nullableSQLTime(supersededAt),
			"created_at": createdAt,
		})
	}
	writeJSON(w, 200, items)
}

func (h *PricingAdminHandler) SaveExchangeRate(w http.ResponseWriter, r *http.Request) {
	adminID, ok := h.admin(w, r)
	if !ok {
		return
	}
	var req saveExchangeRateRequest
	if readJSON(r, &req) != nil || !validPositiveDecimal(req.Rate) ||
		!validDateRange(req.EffectiveFrom, req.EffectiveTo) ||
		(req.Status != "draft" && req.Status != "published") {
		writeJSON(w, 400, map[string]string{"error": "invalid exchange rate version"})
		return
	}
	checkedAt, err := optionalRFC3339(req.SourceCheckedAt)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid source_checked_at"})
		return
	}
	if req.SupersedesID != "" && (req.Status != "published" || strings.TrimSpace(req.ID) != "") {
		writeJSON(w, 400, map[string]string{"error": "a correction must publish a new superseding rate"})
		return
	}
	tx, err := h.db.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer tx.Rollback()
	if req.SupersedesID != "" {
		result, updateErr := tx.ExecContext(r.Context(), `
			UPDATE usd_cny_rate_versions SET superseded_at=now()
			WHERE id::text=$1 AND status='published' AND superseded_at IS NULL
				AND effective_from=$2::date
				AND effective_to IS NOT DISTINCT FROM NULLIF($3, '')::date`,
			req.SupersedesID, req.EffectiveFrom, req.EffectiveTo)
		if updateErr != nil {
			writeJSON(w, 400, map[string]string{"error": updateErr.Error()})
			return
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			writeJSON(w, 400, map[string]string{"error": "superseded rate version does not match the correction interval"})
			return
		}
	}
	var id string
	if strings.TrimSpace(req.ID) == "" {
		err = tx.QueryRowContext(r.Context(), `
			INSERT INTO usd_cny_rate_versions(
				rate, effective_from, effective_to, source_url, source_checked_at,
				notes, status, published_by, published_at, supersedes_id
			) VALUES (
				$1::numeric, $2::date, NULLIF($3, '')::date, NULLIF($4, ''), $5,
				NULLIF($6, ''), $7, CASE WHEN $7='published' THEN $8::bigint END,
				CASE WHEN $7='published' THEN now() END, NULLIF($9, '')::uuid
			) RETURNING id::text`, req.Rate, req.EffectiveFrom, req.EffectiveTo,
			req.SourceURL, checkedAt, req.Notes, req.Status, adminID, req.SupersedesID).Scan(&id)
	} else {
		err = tx.QueryRowContext(r.Context(), `
			UPDATE usd_cny_rate_versions SET
				rate=$2::numeric, effective_from=$3::date, effective_to=NULLIF($4, '')::date,
				source_url=NULLIF($5, ''), source_checked_at=$6, notes=NULLIF($7, ''),
				status=$8, published_by=CASE WHEN $8='published' THEN $9::bigint END,
				published_at=CASE WHEN $8='published' THEN now() END
			WHERE id::text=$1 AND status='draft'
			RETURNING id::text`, req.ID, req.Rate, req.EffectiveFrom, req.EffectiveTo,
			req.SourceURL, checkedAt, req.Notes, req.Status, adminID).Scan(&id)
	}
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if err := tx.Commit(); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"id": id})
}

func (h *PricingAdminHandler) ImportSuggestions(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.admin(w, r); !ok {
		return
	}
	var req importPricingSuggestionsRequest
	if readJSON(r, &req) != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request body"})
		return
	}
	created := 0
	for _, alias := range req.Aliases {
		if strings.TrimSpace(alias.Provider) == "" || strings.TrimSpace(alias.RawModelPattern) == "" || strings.TrimSpace(alias.CanonicalModel) == "" {
			continue
		}
		result, err := h.db.ExecContext(r.Context(), `
			INSERT INTO model_aliases(provider, raw_model_pattern, canonical_model, status)
			VALUES($1, $2, $3, 'pending')
			ON CONFLICT(provider, raw_model_pattern) DO NOTHING`, strings.TrimSpace(alias.Provider),
			strings.TrimSpace(alias.RawModelPattern), strings.TrimSpace(alias.CanonicalModel))
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		if affected, _ := result.RowsAffected(); affected > 0 {
			created++
		}
	}
	writeJSON(w, 200, map[string]int{"created": created})
}

func (h *PricingAdminHandler) RecalculatePreview(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.admin(w, r); !ok {
		return
	}
	filter, _, ok := readRecalculateFilter(w, r)
	if !ok {
		return
	}
	result, err := h.service.Preview(r.Context(), filter)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, result)
}

func (h *PricingAdminHandler) RecalculateApply(w http.ResponseWriter, r *http.Request) {
	adminID, ok := h.admin(w, r)
	if !ok {
		return
	}
	filter, reason, ok := readRecalculateFilter(w, r, true)
	if !ok {
		return
	}
	result, err := h.service.RecalculateWithAudit(r.Context(), filter, adminID, reason)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, result)
}

func (h *PricingAdminHandler) ListUnpricedModels(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.admin(w, r); !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT component.provider, COALESCE(component.raw_model, component.canonical_model, 'unknown'),
			COUNT(*)::text, SUM(component.normalized_total_tokens)::text,
			MAX(component.activity_date)::text
		FROM session_usage_components component
		JOIN session_metrics_revisions revision ON revision.id = component.revision_id AND revision.status='active'
		JOIN session_source_metrics_states state ON state.active_revision_id = component.revision_id
		LEFT JOIN session_activity_costs cost ON cost.usage_component_id = component.id
			AND cost.calculator_version=$1 AND cost.superseded_at IS NULL
		WHERE component.valid_to IS NULL AND COALESCE(cost.pricing_status, 'pricing_pending') <> 'priced'
		GROUP BY component.provider, COALESCE(component.raw_model, component.canonical_model, 'unknown')
		ORDER BY SUM(component.normalized_total_tokens) DESC`, pricing.CalculatorVersion)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	items := []map[string]string{}
	for rows.Next() {
		var provider, model, components, tokens, lastDate string
		if err := rows.Scan(&provider, &model, &components, &tokens, &lastDate); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		items = append(items, map[string]string{
			"provider": provider, "model": model, "component_count": components,
			"total_tokens": tokens, "last_activity_date": lastDate,
		})
	}
	writeJSON(w, 200, items)
}

func (h *PricingAdminHandler) ListRecalculationRuns(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.admin(w, r); !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT run.id::text, run.requested_by::text,
			COALESCE(NULLIF(user_row.nickname, ''), NULLIF(user_row.name, ''), user_row.username, run.requested_by::text),
			run.filter_json, run.result_json, run.reason, run.calculator_version, run.created_at
		FROM pricing_recalculation_runs run
		JOIN users user_row ON user_row.id = run.requested_by
		ORDER BY run.created_at DESC
		LIMIT 100`)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, requestedBy, requestedByName, reason, calculatorVersion string
		var filterJSON, resultJSON []byte
		var createdAt time.Time
		if err := rows.Scan(&id, &requestedBy, &requestedByName, &filterJSON, &resultJSON,
			&reason, &calculatorVersion, &createdAt); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		items = append(items, map[string]any{
			"id": id, "requested_by": requestedBy, "requested_by_name": requestedByName,
			"filter": json.RawMessage(filterJSON), "result": json.RawMessage(resultJSON),
			"reason": reason, "calculator_version": calculatorVersion, "created_at": createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, items)
}

func (h *PricingAdminHandler) admin(w http.ResponseWriter, r *http.Request) (int64, bool) {
	user := getUser(r)
	if user == nil {
		writeJSON(w, 401, map[string]string{"error": "unauthorized"})
		return 0, false
	}
	if user.Role != "admin" {
		writeJSON(w, 403, map[string]string{"error": "insufficient permissions"})
		return 0, false
	}
	id, err := parseInt64(user.ID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "invalid local user identity"})
		return 0, false
	}
	return id, true
}

func readRecalculateFilter(w http.ResponseWriter, r *http.Request, requireReason ...bool) (pricing.RecalculateFilter, string, bool) {
	var req recalculatePricingRequest
	if readJSON(r, &req) != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request body"})
		return pricing.RecalculateFilter{}, "", false
	}
	filter := pricing.RecalculateFilter{Model: strings.TrimSpace(req.Model)}
	if req.From != "" {
		value, err := time.Parse("2006-01-02", req.From)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid from date"})
			return pricing.RecalculateFilter{}, "", false
		}
		filter.From = &value
	}
	if req.To != "" {
		value, err := time.Parse("2006-01-02", req.To)
		if err != nil || (filter.From != nil && value.Before(*filter.From)) {
			writeJSON(w, 400, map[string]string{"error": "invalid to date"})
			return pricing.RecalculateFilter{}, "", false
		}
		filter.To = &value
	}
	reason := strings.TrimSpace(req.Reason)
	if len(requireReason) > 0 && requireReason[0] && reason == "" {
		writeJSON(w, 400, map[string]string{"error": "recalculation reason is required"})
		return pricing.RecalculateFilter{}, "", false
	}
	return filter, reason, true
}

func validModelPriceRequest(req saveModelPriceRequest) bool {
	if strings.TrimSpace(req.PriceBookID) == "" || strings.TrimSpace(req.CanonicalModel) == "" ||
		(req.Status != "draft" && req.Status != "published") || !validDateRange(req.EffectiveFrom, req.EffectiveTo) {
		return false
	}
	return validNonNegativeDecimal(req.InputPerMillion) &&
		validNonNegativeDecimal(req.CacheReadPerMillion) &&
		validNonNegativeDecimal(req.CacheWrite5mPerMillion) &&
		validNonNegativeDecimal(req.CacheWrite1hPerMillion) &&
		validNonNegativeDecimal(req.OutputPerMillion)
}

func validDateRange(from, to string) bool {
	start, err := time.Parse("2006-01-02", strings.TrimSpace(from))
	if err != nil {
		return false
	}
	if strings.TrimSpace(to) == "" {
		return true
	}
	end, err := time.Parse("2006-01-02", strings.TrimSpace(to))
	return err == nil && end.After(start)
}

func validNonNegativeDecimal(raw string) bool {
	value, ok := new(big.Rat).SetString(strings.TrimSpace(raw))
	return ok && value.Sign() >= 0
}

func validPositiveDecimal(raw string) bool {
	value, ok := new(big.Rat).SetString(strings.TrimSpace(raw))
	return ok && value.Sign() > 0
}

func optionalRFC3339(raw string) (any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	value, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, err
	}
	return value, nil
}

func normalizedVariant(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "any"
	}
	return value
}

func nullableSQLString(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}

func nullableSQLTime(value sql.NullTime) any {
	if !value.Valid {
		return nil
	}
	return value.Time
}

func parseInt64(value string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(value), 10, 64)
}
