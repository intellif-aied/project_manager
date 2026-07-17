package sessiondigest

import (
	"strings"
	"testing"
)

func TestRedactRemovesCommonSecrets(t *testing.T) {
	input := strings.Join([]string{
		"Authorization: Bearer abc.def.ghi",
		"standalone eyJhbGciOiJIUzI1NiJ9.eyJ1aWQiOjMwM30.signatureValue",
		`{"authorization":"Basic json-basic-secret"}`,
		"API_KEY=super-secret",
		"AIDA_REPORT_MCP_AUTH=credential-slot-secret",
		`{"password":"hunter2"}`,
		"https://alice:password@example.com/path",
		"postgres://dbuser:dbpass@db.example.com/app",
		"https://example.com/callback?access_token=query-secret&safe=1",
		"-----BEGIN PRIVATE KEY-----\nvery-secret\n-----END PRIVATE KEY-----",
		"账号是 user，密码是 qjk-secret",
		"login with user pass Liule@2024",
		"ordinary pass through remains readable",
	}, "\n")
	got := Redact(input)
	for _, secret := range []string{"abc.def.ghi", "signatureValue", "json-basic-secret", "super-secret", "credential-slot-secret", "hunter2", "alice:password", "dbuser:dbpass", "query-secret", "very-secret", "qjk-secret", "Liule@2024"} {
		if strings.Contains(got, secret) {
			t.Fatalf("redacted output still contains %q: %s", secret, got)
		}
	}
	if count := strings.Count(got, "[REDACTED]"); count < 12 {
		t.Fatalf("expected each secret family to be redacted, got %d replacements: %s", count, got)
	}
	if !strings.Contains(got, "pass through") {
		t.Fatalf("ordinary pass phrase was over-redacted: %s", got)
	}
}

func TestRedactPreservesOrdinaryEvidence(t *testing.T) {
	const input = "go test ./... passed; updated api/main.go"
	if got := Redact(input); got != input {
		t.Fatalf("ordinary evidence changed: %q", got)
	}
}
