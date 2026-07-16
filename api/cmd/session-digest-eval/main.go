package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"encoding/json"

	"github.com/aidashboard/api/internal/biztime"
	"github.com/aidashboard/api/internal/sessiondigestv2"
	"github.com/aidashboard/api/internal/sessionsync"
)

type evaluation struct {
	File                 string                  `json:"file"`
	RawBytes             int64                   `json:"raw_bytes"`
	EventCount           int                     `json:"event_count"`
	MalformedEventCount  int64                   `json:"malformed_event_count"`
	SourceEventCount     int64                   `json:"source_event_count"`
	IncludedEventCount   int64                   `json:"included_event_count"`
	OmittedEventCount    int64                   `json:"omitted_event_count"`
	DigestBytes          int                     `json:"digest_bytes"`
	Truncated            bool                    `json:"truncated"`
	WorkUnitCount        int                     `json:"work_unit_count"`
	DailySummaryCount    int                     `json:"daily_summary_count"`
	PeriodDayCount       int                     `json:"period_day_count"`
	PeriodHighlightCount int                     `json:"period_highlight_count"`
	ResultStatementCount int                     `json:"result_statement_count"`
	VerifiedResultCount  int                     `json:"verified_result_count"`
	ChangeCount          int                     `json:"change_count"`
	ValidationCount      int                     `json:"validation_count"`
	UnresolvedCount      int                     `json:"unresolved_count"`
	SuspiciousGoalCount  int                     `json:"suspicious_goal_count"`
	ResultFocused        bool                    `json:"result_focused"`
	Digest               *sessiondigestv2.Digest `json:"digest,omitempty"`
}

func main() {
	maxBytes := flag.Int("max-bytes", sessiondigestv2.DefaultItemBytes, "maximum bytes for one digest")
	periodStart := flag.String("period-start", "", "inclusive business date, YYYY-MM-DD")
	periodEnd := flag.String("period-end", "", "inclusive business date, YYYY-MM-DD")
	summaryOnly := flag.Bool("summary-only", false, "omit the full digest")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: session-digest-eval [flags] session.jsonl [...]")
		os.Exit(2)
	}
	var start, end time.Time
	var annotate bool
	if strings.TrimSpace(*periodStart) != "" || strings.TrimSpace(*periodEnd) != "" {
		var err error
		start, err = biztime.ParseDate(*periodStart)
		if err != nil {
			fatalf("invalid period-start: %v", err)
		}
		end, err = biztime.ParseDate(*periodEnd)
		if err != nil || end.Before(start) {
			fatalf("invalid period-end: %v", err)
		}
		annotate = true
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	for _, path := range flag.Args() {
		result, err := evaluate(path, *maxBytes, start, end, annotate, *summaryOnly)
		if err != nil {
			fatalf("%s: %v", path, err)
		}
		if err := encoder.Encode(result); err != nil {
			fatalf("encode result: %v", err)
		}
	}
}

func evaluate(
	path string,
	maxBytes int,
	periodStart, periodEnd time.Time,
	annotate, summaryOnly bool,
) (evaluation, error) {
	file, err := os.Open(path)
	if err != nil {
		return evaluation{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return evaluation{}, err
	}
	fallback := info.ModTime().UTC()
	parsed, err := sessionsync.ParseContentChunk(file, 0, &fallback)
	if err != nil {
		return evaluation{}, err
	}
	extractor := sessiondigestv2.NewExtractor()
	for _, projected := range parsed.Events {
		extractor.Consume(sessiondigestv2.Event{
			StartCursor:  projected.SourceStartCursor,
			EndCursor:    projected.SourceEndCursor,
			OccurredAt:   projected.OccurredAt,
			EventType:    projected.EventType,
			Summary:      projected.Summary,
			Excerpt:      projected.Excerpt,
			Payload:      projected.Payload,
			ContentSHA:   projected.ContentSHA256,
			PayloadBytes: int64(len(projected.Payload)),
		})
	}
	digest, sourceEvents, includedEvents, omittedEvents, truncated, encoded :=
		extractor.Result(maxBytes)
	if annotate {
		digest, encoded, truncated = sessiondigestv2.PrepareForPeriod(
			digest,
			periodStart,
			periodEnd,
			biztime.Location(),
			sessiondigestv2.DefaultPeriodItemBytes,
		)
	}
	result := evaluation{
		File:                path,
		RawBytes:            info.Size(),
		EventCount:          len(parsed.Events),
		MalformedEventCount: parsed.MalformedEventCount,
		SourceEventCount:    sourceEvents,
		IncludedEventCount:  includedEvents,
		OmittedEventCount:   omittedEvents,
		DigestBytes:         len(encoded),
		Truncated:           truncated,
		WorkUnitCount:       len(digest.WorkUnits),
		DailySummaryCount:   len(digest.DailySummaries),
		VerifiedResultCount: digest.SessionSummary.VerifiedResultCount,
		UnresolvedCount:     digest.SessionSummary.UnresolvedCount,
	}
	for _, unit := range digest.WorkUnits {
		result.ResultStatementCount += len(unit.ResultStatements)
		result.ChangeCount += len(unit.Changes)
		result.ValidationCount += len(unit.Validations)
		if suspiciousGoal(unit.Goal.Text) {
			result.SuspiciousGoalCount++
		}
	}
	if digest.ReportPeriodSummary != nil {
		result.PeriodDayCount = len(digest.ReportPeriodSummary.Days)
		for _, day := range digest.ReportPeriodSummary.Days {
			result.PeriodHighlightCount += len(day.Highlights)
			for _, highlight := range day.Highlights {
				result.ResultStatementCount += len(highlight.ResultStatements)
				result.UnresolvedCount += len(highlight.Unresolved)
				if suspiciousGoal(highlight.Goal) {
					result.SuspiciousGoalCount++
				}
			}
		}
	}
	result.ResultFocused = result.ResultStatementCount > 0 ||
		result.ChangeCount > 0 || result.ValidationCount > 0
	if !summaryOnly {
		result.Digest = &digest
	}
	return result, nil
}

func suspiciousGoal(value string) bool {
	value = strings.ToLower(value)
	for _, marker := range []string{
		"agents.md instructions",
		"<environment_context>",
		"<permissions instructions>",
		"# skills",
		"you are codex",
	} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func fatalf(format string, values ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", values...)
	os.Exit(1)
}
