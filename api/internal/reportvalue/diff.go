package reportvalue

import (
	"regexp"
	"strings"
)

const (
	ChangeUnchanged = "unchanged"
	ChangeLight     = "light"
	ChangeMedium    = "medium"
	ChangeHeavy     = "heavy"
)

var markdownHeadingPattern = regexp.MustCompile(`(?m)^##[ \t]+(.+?)[ \t]*$`)

type TextMetrics struct {
	GeneratedChars int      `json:"generated_chars"`
	UserChars      int      `json:"user_chars"`
	MatchedChars   int      `json:"matched_chars"`
	DiffRatio      *float64 `json:"text_diff_ratio"`
	DraftRetention *float64 `json:"draft_retention_rate"`
	UserAddition   *float64 `json:"user_addition_rate"`
	ChangeBand     string   `json:"change_band"`
}

type SummaryMetrics struct {
	GeneratedPresent bool         `json:"generated_present"`
	UserPresent      bool         `json:"user_present"`
	Outcome          string       `json:"outcome"`
	Reduced30        bool         `json:"reduced_30"`
	Text             *TextMetrics `json:"text,omitempty"`
}

type TopicMetrics struct {
	Generated []string `json:"generated"`
	User      []string `json:"user"`
	Deleted   []string `json:"deleted"`
	Added     []string `json:"added"`
}

type Result struct {
	Text    TextMetrics    `json:"text"`
	Summary SummaryMetrics `json:"summary"`
	Topics  TopicMetrics   `json:"topics"`
}

func Compare(generated, user string) Result {
	generated = Normalize(generated)
	user = Normalize(user)
	generatedRunes := []rune(generated)
	userRunes := []rune(user)
	matched := lcsLength(generatedRunes, userRunes)
	text := textMetrics(len(generatedRunes), len(userRunes), matched)

	generatedSummary, generatedSummaryOK := summaryRegion(generated)
	userSummary, userSummaryOK := summaryRegion(user)
	summary := SummaryMetrics{GeneratedPresent: generatedSummaryOK, UserPresent: userSummaryOK, Outcome: "not_applicable"}
	if generatedSummaryOK {
		switch {
		case !userSummaryOK || userSummary == "":
			summary.Outcome = "summary_removed"
		case generatedSummary == userSummary:
			summary.Outcome = "summary_unchanged"
		default:
			summary.Outcome = "summary_modified"
		}
		generatedSummaryRunes := []rune(generatedSummary)
		userSummaryRunes := []rune(userSummary)
		metrics := textMetrics(len(generatedSummaryRunes), len(userSummaryRunes), lcsLength(generatedSummaryRunes, userSummaryRunes))
		summary.Text = &metrics
		summary.Reduced30 = len(generatedSummaryRunes) > 0 && float64(len(userSummaryRunes))/float64(len(generatedSummaryRunes)) <= 0.70
	}

	generatedTopics := topicHeadings(generated)
	userTopics := topicHeadings(user)
	return Result{
		Text:    text,
		Summary: summary,
		Topics: TopicMetrics{
			Generated: generatedTopics,
			User:      userTopics,
			Deleted:   orderedDifference(generatedTopics, userTopics),
			Added:     orderedDifference(userTopics, generatedTopics),
		},
	}
}

func Normalize(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	lines := strings.Split(value, "\n")
	for index := range lines {
		lines[index] = strings.TrimRight(lines[index], " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func textMetrics(generatedChars, userChars, matchedChars int) TextMetrics {
	metrics := TextMetrics{
		GeneratedChars: generatedChars,
		UserChars:      userChars,
		MatchedChars:   matchedChars,
		ChangeBand:     "not_applicable",
	}
	if generatedChars+userChars > 0 {
		value := 1 - (2 * float64(matchedChars) / float64(generatedChars+userChars))
		metrics.DiffRatio = &value
		switch {
		case value == 0:
			metrics.ChangeBand = ChangeUnchanged
		case value <= 0.10+1e-12:
			metrics.ChangeBand = ChangeLight
		case value <= 0.30+1e-12:
			metrics.ChangeBand = ChangeMedium
		default:
			metrics.ChangeBand = ChangeHeavy
		}
	}
	if generatedChars > 0 {
		value := float64(matchedChars) / float64(generatedChars)
		metrics.DraftRetention = &value
	}
	if userChars > 0 {
		value := float64(userChars-matchedChars) / float64(userChars)
		metrics.UserAddition = &value
	}
	return metrics
}

func summaryRegion(value string) (string, bool) {
	matches := markdownHeadingPattern.FindAllStringSubmatchIndex(value, -1)
	for index, match := range matches {
		title := strings.TrimSpace(value[match[2]:match[3]])
		if !isSummaryHeading(title) {
			continue
		}
		end := len(value)
		if index+1 < len(matches) {
			end = matches[index+1][0]
		}
		return Normalize(value[match[1]:end]), true
	}
	return "", false
}

func topicHeadings(value string) []string {
	matches := markdownHeadingPattern.FindAllStringSubmatch(value, -1)
	result := make([]string, 0, len(matches))
	for _, match := range matches {
		title := strings.TrimSpace(match[1])
		if title != "" && !isSummaryHeading(title) {
			result = append(result, title)
		}
	}
	return result
}

func isSummaryHeading(title string) bool {
	return title == "工作概览" || title == "工作总结"
}

func orderedDifference(left, right []string) []string {
	counts := make(map[string]int, len(right))
	for _, value := range right {
		counts[value]++
	}
	result := make([]string, 0)
	for _, value := range left {
		if counts[value] > 0 {
			counts[value]--
			continue
		}
		result = append(result, value)
	}
	return result
}

// lcsLength derives LCS from Myers' shortest edit script. It is exact at the
// Unicode code-point level and is fast for the expected case of similar drafts.
func lcsLength(left, right []rune) int {
	leftLength, rightLength := len(left), len(right)
	if leftLength == 0 || rightLength == 0 {
		return 0
	}
	maxDistance := leftLength + rightLength
	offset := maxDistance
	frontier := make([]int, 2*maxDistance+1)
	frontier[offset+1] = 0
	for distance := 0; distance <= maxDistance; distance++ {
		for diagonal := -distance; diagonal <= distance; diagonal += 2 {
			index := offset + diagonal
			var leftIndex int
			if diagonal == -distance || (diagonal != distance && frontier[index-1] < frontier[index+1]) {
				leftIndex = frontier[index+1]
			} else {
				leftIndex = frontier[index-1] + 1
			}
			rightIndex := leftIndex - diagonal
			for leftIndex < leftLength && rightIndex < rightLength && left[leftIndex] == right[rightIndex] {
				leftIndex++
				rightIndex++
			}
			frontier[index] = leftIndex
			if leftIndex >= leftLength && rightIndex >= rightLength {
				return (leftLength + rightLength - distance) / 2
			}
		}
	}
	return 0
}
