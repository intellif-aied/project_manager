package sessiondigest

import (
	"regexp"
	"strings"
)

var redactionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?is)-----BEGIN [^-\r\n]*PRIVATE KEY-----.*?-----END [^-\r\n]*PRIVATE KEY-----`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\b`),
	regexp.MustCompile(`(?i)(["']?authorization["']?\s*[:=]\s*["']?(?:(?:bearer|basic)\s+)?)[^\s"',;]+`),
	regexp.MustCompile(`(?i)\b((?:api[_-]?key|access[_-]?token|refresh[_-]?token|token|password|passwd|secret|cookie|session)["']?\s*[:=]\s*["']?)[^\s"',;]+`),
	regexp.MustCompile(`(?i)\b([A-Z0-9_]*(?:TOKEN|SECRET|PASSWORD|PASSWD|API_KEY|AUTH|CREDENTIAL)[A-Z0-9_]*\s*=\s*)[^\s;]+`),
	regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^/@\s:]+:[^/@\s]+@`),
	regexp.MustCompile(`(?i)([?&](?:api[_-]?key|access[_-]?token|refresh[_-]?token|token|password|passwd|secret|session)=)[^&#\s]+`),
	regexp.MustCompile(`(?i)((?:密码|口令)\s*(?:是|为|[:：=])\s*["']?)[^\s"',;，。]+`),
	regexp.MustCompile(`(?i)\b((?:password|passwd)\s+(?:is\s+)?)[^\s"',;]+`),
	// "pass" is also a common English verb. Only treat the shorthand form as
	// a credential when the value has a password-like digit/@ marker (or is a
	// long numeric PIN), so ordinary phrases such as "pass through" survive.
	regexp.MustCompile(`(?i)\b(pass\s+)(?:[A-Za-z._+-]{2,}[0-9@][A-Za-z0-9._@+-]{2,}|[0-9]{6,})\b`),
}

func Redact(value string) string {
	value = strings.ReplaceAll(value, "\x00", "\uFFFD")
	for index, pattern := range redactionPatterns {
		switch index {
		case 0, 1:
			value = pattern.ReplaceAllString(value, "[REDACTED]")
		case 5:
			value = pattern.ReplaceAllString(value, `${1}[REDACTED]@`)
		default:
			value = pattern.ReplaceAllString(value, `${1}[REDACTED]`)
		}
	}
	return value
}
