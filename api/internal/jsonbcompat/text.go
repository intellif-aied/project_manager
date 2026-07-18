// Package jsonbcompat reproduces PostgreSQL jsonb text formatting where legacy
// application behavior depends on #>> or jsonb::text output.
package jsonbcompat

import (
	"bytes"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// Text returns PostgreSQL-compatible jsonb text for values decoded with encoding/json.
func Text(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case bool:
		return strconv.FormatBool(typed)
	case json.Number:
		return number(typed.String())
	case float64:
		return number(strconv.FormatFloat(typed, 'g', -1, 64))
	case string:
		return jsonString(typed)
	case []any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			items = append(items, Text(item))
		}
		return "[" + strings.Join(items, ", ") + "]"
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(left, right int) bool {
			if len(keys[left]) != len(keys[right]) {
				return len(keys[left]) < len(keys[right])
			}
			return keys[left] < keys[right]
		})
		fields := make([]string, 0, len(keys))
		for _, key := range keys {
			fields = append(fields, jsonString(key)+": "+Text(typed[key]))
		}
		return "{" + strings.Join(fields, ", ") + "}"
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return ""
		}
		return string(encoded)
	}
}

func jsonString(value string) string {
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return `""`
	}
	return strings.TrimSuffix(encoded.String(), "\n")
}

func number(value string) string {
	lower := strings.ToLower(value)
	mantissa, exponentText, hasExponent := strings.Cut(lower, "e")
	exponent := 0
	if hasExponent {
		parsed, err := strconv.Atoi(strings.TrimPrefix(exponentText, "+"))
		if err != nil {
			return value
		}
		exponent = parsed
	}
	negative := strings.HasPrefix(mantissa, "-")
	mantissa = strings.TrimPrefix(mantissa, "-")
	integer, fraction, hasFraction := strings.Cut(mantissa, ".")
	if !hasFraction {
		fraction = ""
	}
	digits := strings.TrimLeft(integer+fraction, "0")
	scale := len(fraction) - exponent
	if digits == "" {
		if scale <= 0 {
			return "0"
		}
		return "0." + strings.Repeat("0", scale)
	}
	var normalized string
	switch {
	case scale <= 0:
		normalized = digits + strings.Repeat("0", -scale)
	case len(digits) > scale:
		point := len(digits) - scale
		normalized = digits[:point] + "." + digits[point:]
	default:
		normalized = "0." + strings.Repeat("0", scale-len(digits)) + digits
	}
	if negative {
		return "-" + normalized
	}
	return normalized
}
