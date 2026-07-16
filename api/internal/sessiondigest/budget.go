package sessiondigest

import (
	"encoding/json"
	"unicode/utf8"
)

func EnforceItemBudget(input Digest, maxBytes int) (Digest, []byte, bool) {
	if maxBytes < 1024 {
		maxBytes = DefaultItemBytes
	}
	digest := cloneDigest(input)
	truncated := false
	truncated = trimStrings(&digest.Goals, 8) || truncated
	truncated = trimLastStrings(&digest.Outcomes, 12) || truncated
	truncated = trimStrings(&digest.FilesChanged, 50) || truncated
	if len(digest.Validations) > 20 {
		digest.Validations = compactValidations(digest.Validations, 20)
		truncated = true
	}
	truncated = trimLastStrings(&digest.Blockers, 10) || truncated
	encoded, _ := json.Marshal(digest)
	if len(encoded) <= maxBytes {
		return digest, encoded, truncated
	}
	truncated = true
	digest.Goals = firstStrings(digest.Goals, 2)
	digest.Outcomes = lastStrings(digest.Outcomes, 2)
	digest.FilesChanged = firstStrings(digest.FilesChanged, 8)
	digest.Validations = compactValidations(digest.Validations, 4)
	digest.Blockers = lastStrings(digest.Blockers, 2)
	encoded, _ = json.Marshal(digest)
	if len(encoded) <= maxBytes {
		return digest, encoded, truncated
	}
	for textLimit := 256; textLimit >= 64; textLimit /= 2 {
		truncateDigestText(&digest, textLimit)
		encoded, _ = json.Marshal(digest)
		if len(encoded) <= maxBytes {
			return digest, encoded, truncated
		}
	}
	digest.Goals = firstStrings(digest.Goals, 1)
	digest.Outcomes = lastStrings(digest.Outcomes, 1)
	digest.FilesChanged = firstStrings(digest.FilesChanged, 3)
	digest.Validations = compactValidations(digest.Validations, 1)
	digest.Blockers = lastStrings(digest.Blockers, 1)
	truncateDigestText(&digest, 64)
	encoded, _ = json.Marshal(digest)
	return digest, encoded, truncated
}

func CompactDigest(input Digest) Digest {
	digest := cloneDigest(input)
	digest.Goals = firstStrings(digest.Goals, 1)
	digest.Outcomes = lastStrings(digest.Outcomes, 1)
	digest.FilesChanged = firstStrings(digest.FilesChanged, 3)
	digest.Validations = compactValidations(digest.Validations, 2)
	digest.Blockers = lastStrings(digest.Blockers, 1)
	truncateDigestText(&digest, 192)
	return digest
}

func cloneDigest(input Digest) Digest {
	result := EmptyDigest()
	result.Goals = append(result.Goals, input.Goals...)
	result.Outcomes = append(result.Outcomes, input.Outcomes...)
	result.FilesChanged = append(result.FilesChanged, input.FilesChanged...)
	result.Validations = append(result.Validations, input.Validations...)
	result.Blockers = append(result.Blockers, input.Blockers...)
	return result
}

func trimStrings(values *[]string, limit int) bool {
	if len(*values) <= limit {
		return false
	}
	*values = append([]string(nil), (*values)[:limit]...)
	return true
}

func trimLastStrings(values *[]string, limit int) bool {
	if len(*values) <= limit {
		return false
	}
	*values = append([]string(nil), (*values)[len(*values)-limit:]...)
	return true
}

func firstStrings(values []string, limit int) []string {
	if len(values) <= limit {
		return values
	}
	return append([]string(nil), values[:limit]...)
}

func lastStrings(values []string, limit int) []string {
	if len(values) <= limit {
		return values
	}
	return append([]string(nil), values[len(values)-limit:]...)
}

func compactValidations(values []Validation, limit int) []Validation {
	failed := make([]Validation, 0, len(values))
	other := make([]Validation, 0, len(values))
	for _, value := range values {
		if value.Status == "failed" {
			failed = append(failed, value)
		} else {
			other = append(other, value)
		}
	}
	failed = append(failed, other...)
	if len(failed) > limit {
		failed = failed[:limit]
	}
	return failed
}

func truncateDigestText(digest *Digest, limit int) {
	for index := range digest.Goals {
		digest.Goals[index], _ = truncateUTF8Bytes(digest.Goals[index], limit)
	}
	for index := range digest.Outcomes {
		digest.Outcomes[index], _ = truncateUTF8Bytes(digest.Outcomes[index], limit)
	}
	for index := range digest.FilesChanged {
		digest.FilesChanged[index], _ = truncateUTF8Bytes(digest.FilesChanged[index], min(limit, 128))
	}
	for index := range digest.Validations {
		digest.Validations[index].Name, _ = truncateUTF8Bytes(digest.Validations[index].Name, min(limit, 128))
		digest.Validations[index].Summary, _ = truncateUTF8Bytes(digest.Validations[index].Summary, limit)
	}
	for index := range digest.Blockers {
		digest.Blockers[index], _ = truncateUTF8Bytes(digest.Blockers[index], limit)
	}
}

func truncateUTF8Bytes(value string, limit int) (string, bool) {
	if limit <= 0 || len(value) <= limit {
		return value, false
	}
	if limit <= 3 {
		end := min(limit, len(value))
		for end > 0 && !utf8.ValidString(value[:end]) {
			end--
		}
		return value[:end], true
	}
	end := limit - 3
	for end > 0 && !utf8.ValidString(value[:end]) {
		end--
	}
	return value[:end] + "...", true
}
