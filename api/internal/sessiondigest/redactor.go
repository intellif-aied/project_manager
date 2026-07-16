package sessiondigest

import (
	"regexp"
	"strings"
)

var redactionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?is)-----BEGIN [^-\r\n]*PRIVATE KEY-----.*?-----END [^-\r\n]*PRIVATE KEY-----`),
	regexp.MustCompile(`(?i)(["']?authorization["']?\s*[:=]\s*["']?(?:(?:bearer|basic)\s+)?)[^\s"',;]+`),
	regexp.MustCompile(`(?i)\b((?:api[_-]?key|access[_-]?token|refresh[_-]?token|token|password|passwd|secret|cookie|session)["']?\s*[:=]\s*["']?)[^\s"',;]+`),
	regexp.MustCompile(`(?i)\b([A-Z0-9_]*(?:TOKEN|SECRET|PASSWORD|PASSWD|API_KEY|AUTH|CREDENTIAL)[A-Z0-9_]*\s*=\s*)[^\s;]+`),
	regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^/@\s:]+:[^/@\s]+@`),
	regexp.MustCompile(`(?i)([?&](?:api[_-]?key|access[_-]?token|refresh[_-]?token|token|password|passwd|secret|session)=)[^&#\s]+`),
}

func Redact(value string) string {
	value = strings.ReplaceAll(value, "\x00", "\uFFFD")
	for index, pattern := range redactionPatterns {
		switch index {
		case 0:
			value = pattern.ReplaceAllString(value, "[REDACTED]")
		case 4:
			value = pattern.ReplaceAllString(value, `${1}[REDACTED]@`)
		default:
			value = pattern.ReplaceAllString(value, `${1}[REDACTED]`)
		}
	}
	return value
}
