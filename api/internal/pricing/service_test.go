package pricing

import (
	"database/sql"
	"strings"
	"testing"
)

func TestPreviewDoesNotLockRowsInReadOnlyTransaction(t *testing.T) {
	if query := activeCostQuery(false); strings.Contains(query, "FOR UPDATE") {
		t.Fatalf("read-only query contains FOR UPDATE: %s", query)
	}
	if query := activeCostQuery(true); !strings.Contains(query, "FOR UPDATE") {
		t.Fatalf("write query does not lock the active cost row: %s", query)
	}
}

func TestNullDecimalsEqualIgnoresScaleButNotValue(t *testing.T) {
	if !nullDecimalsEqual(sql.NullString{String: "0.111991000000", Valid: true}, sql.NullString{String: "0.11199100000000000000", Valid: true}) {
		t.Fatal("equal decimal values with different scales must compare equal")
	}
	if nullDecimalsEqual(sql.NullString{String: "0.111991", Valid: true}, sql.NullString{String: "0.111992", Valid: true}) {
		t.Fatal("different decimal values must not compare equal")
	}
	if nullDecimalsEqual(sql.NullString{}, sql.NullString{String: "0", Valid: true}) {
		t.Fatal("NULL and zero must not compare equal")
	}
}
