package reporteval

import (
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

type PatternDistributionComparison struct {
	BaselineP50  int `json:"baseline_p50"`
	BaselineP90  int `json:"baseline_p90"`
	GeneratedP50 int `json:"generated_p50"`
	GeneratedP90 int `json:"generated_p90"`
	DeltaP50     int `json:"delta_p50"`
	DeltaP90     int `json:"delta_p90"`
}

type PatternScore struct {
	GeneratedDrafts   int                           `json:"generated_drafts"`
	FormatClassCounts map[string]int                `json:"format_class_counts"`
	CharacterCount    PatternDistributionComparison `json:"character_count"`
	LineCount         PatternDistributionComparison `json:"line_count"`
	OrderedItemCount  PatternDistributionComparison `json:"ordered_item_count"`
	HeadingCount      PatternDistributionComparison `json:"heading_count"`
}

type reportShape struct {
	FormatClass    string
	CharacterCount int
	LineCount      int
	OrderedItems   int
	UnorderedItems int
	HeadingCount   int
}

var (
	orderedListPattern   = regexp.MustCompile(`^\s*[0-9]+[.)、]\s+`)
	unorderedListPattern = regexp.MustCompile(`^\s*[-*+]\s+`)
	headingPattern       = regexp.MustCompile(`^\s*#{1,6}\s+`)
)

func buildPatternScore(bundleDir string, runs []RunReceipt, variant string, baseline PatternStatistics) PatternScore {
	result := PatternScore{FormatClassCounts: map[string]int{}}
	characters := []int{}
	lines := []int{}
	orderedItems := []int{}
	headings := []int{}
	for _, run := range runs {
		if run.VariantVersion != variant || run.Status != "succeeded" {
			continue
		}
		payload, err := os.ReadFile(filepath.Join(bundleDir, "cases", run.CaseID, "runs", run.RunID, "generated-draft.md"))
		if err != nil {
			continue
		}
		shape := measureReportShape(string(payload))
		result.GeneratedDrafts++
		result.FormatClassCounts[shape.FormatClass]++
		characters = append(characters, shape.CharacterCount)
		lines = append(lines, shape.LineCount)
		orderedItems = append(orderedItems, shape.OrderedItems)
		headings = append(headings, shape.HeadingCount)
	}
	result.CharacterCount = comparePatternDistribution(baseline.CharacterCount, characters)
	result.LineCount = comparePatternDistribution(baseline.LineCount, lines)
	result.OrderedItemCount = comparePatternDistribution(baseline.OrderedItemCount, orderedItems)
	result.HeadingCount = comparePatternDistribution(baseline.HeadingCount, headings)
	return result
}

func measureReportShape(content string) reportShape {
	content = strings.TrimSpace(strings.ReplaceAll(content, "\r\n", "\n"))
	if content == "" {
		return reportShape{FormatClass: "paragraph"}
	}
	shape := reportShape{CharacterCount: utf8.RuneCountInString(content)}
	allLines := strings.Split(content, "\n")
	shape.LineCount = len(allLines)
	for _, line := range allLines {
		switch {
		case headingPattern.MatchString(line):
			shape.HeadingCount++
		case orderedListPattern.MatchString(line):
			shape.OrderedItems++
		case unorderedListPattern.MatchString(line):
			shape.UnorderedItems++
		}
	}
	switch {
	case shape.HeadingCount > 0 && shape.OrderedItems+shape.UnorderedItems > 0:
		shape.FormatClass = "headings_with_lists"
	case shape.OrderedItems > 0:
		shape.FormatClass = "ordered_list"
	case shape.UnorderedItems > 0:
		shape.FormatClass = "unordered_list"
	default:
		shape.FormatClass = "paragraph"
	}
	return shape
}

func comparePatternDistribution(baseline PatternRange, generated []int) PatternDistributionComparison {
	result := PatternDistributionComparison{BaselineP50: baseline.P50, BaselineP90: baseline.P90}
	if len(generated) == 0 {
		return result
	}
	result.GeneratedP50 = nearestRank(generated, .5)
	result.GeneratedP90 = nearestRank(generated, .9)
	result.DeltaP50 = result.GeneratedP50 - result.BaselineP50
	result.DeltaP90 = result.GeneratedP90 - result.BaselineP90
	return result
}

func nearestRank(values []int, percentile float64) int {
	copyOfValues := append([]int(nil), values...)
	sort.Ints(copyOfValues)
	index := int(math.Ceil(float64(len(copyOfValues))*percentile)) - 1
	if index < 0 {
		index = 0
	}
	return copyOfValues[index]
}
