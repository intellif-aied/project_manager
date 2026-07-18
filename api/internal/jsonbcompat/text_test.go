package jsonbcompat

import (
	"encoding/json"
	"testing"
)

func TestTextMatchesPostgresJSONBFormatting(t *testing.T) {
	value := map[string]any{
		"aa": json.Number("1.2300e2"),
		"b":  json.Number("1e-2"),
		"a":  []any{"x", true, nil},
	}
	actual := Text(value)
	expected := `{"a": ["x", true, null], "b": 0.01, "aa": 123.00}`
	if actual != expected {
		t.Fatalf("jsonb text mismatch\nactual:   %s\nexpected: %s", actual, expected)
	}
}
