package reportcontext

import (
	"context"
	"database/sql"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	continuityReportLimit    = 3
	continuityLookbackDays   = 14
	continuityThemeLimit     = 6
	continuityChildLimit     = 8
	continuityTitleRuneLimit = 160
)

var (
	markdownListLinePattern = regexp.MustCompile(`^(\s*)(?:[-+*]|\d+[.)])\s+(.+?)\s*$`)
	markdownLinkPattern     = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
)

func loadContinuityContext(ctx context.Context, tx *sql.Tx, request BuildRequest) (*ContinuityContext, error) {
	if request.ReportType != ReportTypePersonalDaily {
		return nil, nil
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT r.report_date::text,
		       COALESCE(NULLIF(r.submitted_content, ''), r.content)
		FROM daily_reports r
		WHERE r.user_id = $1
		  AND r.report_date < $2::date
		  AND r.report_date >= $2::date - $3::integer
		  AND r.status IN ('saved', 'submitted')
		  AND NULLIF(BTRIM(COALESCE(NULLIF(r.submitted_content, ''), r.content, '')), '') IS NOT NULL
		  AND (
			r.edited = true
			OR r.status = 'submitted'
			OR EXISTS (
				SELECT 1 FROM report_user_outcome_events outcome
				WHERE outcome.report_id = r.id AND outcome.action IN ('saved', 'submitted')
			)
		  )
		ORDER BY r.report_date DESC
		LIMIT $4`, request.Target.UserID, request.Period.Start, continuityLookbackDays, continuityReportLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	reports := make([]ContinuityReportOutline, 0, continuityReportLimit)
	for rows.Next() {
		var date, content string
		if err := rows.Scan(&date, &content); err != nil {
			return nil, err
		}
		themes := extractContinuityThemes(content)
		if len(themes) == 0 {
			continue
		}
		reports = append(reports, ContinuityReportOutline{Date: date, Themes: themes})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(reports) == 0 {
		return nil, nil
	}
	return &ContinuityContext{
		Purpose:       "识别跨日报持续工作的稳定项目名称、父子归属和常用别名。",
		EvidenceRule:  "仅用于归类当前 work_evidence；历史日报中的成果、状态、数据和结论不得写入当前日报，除非当前 Fact 独立支持。",
		GroupingRule:  "提交 Brief 前，将匹配历史子主题的 Subject 映射到父主题，并合并所有映射到同一父主题的 Workstream；子主题只在 Deliverable 中表达，无关 Fact 不得强行归入。",
		RecentReports: reports,
	}, nil
}

type parsedListLine struct {
	indent int
	text   string
}

func extractContinuityThemes(content string) []ContinuityTheme {
	content = normalizeContinuityMarkdown(content)
	if overview := extractOverviewSection(content); overview != "" {
		if themes := themesFromListLines(overview); len(themes) > 0 {
			return themes
		}
	}
	if themes := themesFromListLines(content); len(themes) > 0 {
		return themes
	}
	if themes := themesFromHeadings(content); len(themes) > 0 {
		return themes
	}
	return themesFromPlainText(content)
}

func normalizeContinuityMarkdown(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	if !strings.Contains(content, "\n") && strings.Contains(content, `\n`) {
		content = strings.ReplaceAll(content, `\n`, "\n")
	}
	return strings.TrimSpace(content)
}

func extractOverviewSection(content string) string {
	lines := strings.Split(content, "\n")
	start := -1
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") && strings.TrimSpace(strings.TrimPrefix(trimmed, "## ")) == "工作概览" {
			start = index + 1
			continue
		}
		if start >= 0 && strings.HasPrefix(trimmed, "## ") {
			return strings.Join(lines[start:index], "\n")
		}
	}
	if start >= 0 {
		return strings.Join(lines[start:], "\n")
	}
	return ""
}

func themesFromListLines(content string) []ContinuityTheme {
	lines := strings.Split(content, "\n")
	parsed := make([]parsedListLine, 0, len(lines))
	for _, line := range lines {
		match := markdownListLinePattern.FindStringSubmatch(line)
		if len(match) != 3 {
			continue
		}
		text := sanitizeContinuityTitle(match[2])
		if text == "" {
			continue
		}
		parsed = append(parsed, parsedListLine{indent: markdownIndent(match[1]), text: text})
	}
	if len(parsed) == 0 {
		return nil
	}
	rootIndent := parsed[0].indent
	for _, line := range parsed[1:] {
		if line.indent < rootIndent {
			rootIndent = line.indent
		}
	}

	themes := make([]ContinuityTheme, 0, continuityThemeLimit)
	current := -1
	childIndent := -1
	for _, line := range parsed {
		switch {
		case line.indent == rootIndent:
			if len(themes) >= continuityThemeLimit {
				continue
			}
			themes = append(themes, ContinuityTheme{Title: line.text})
			current = len(themes) - 1
			childIndent = -1
		case line.indent > rootIndent && current >= 0:
			if childIndent < 0 {
				childIndent = line.indent
			}
			if line.indent == childIndent && len(themes[current].Children) < continuityChildLimit && line.text != themes[current].Title {
				themes[current].Children = appendUnique(themes[current].Children, line.text)
			}
		}
	}
	return uniqueThemes(themes)
}

func themesFromHeadings(content string) []ContinuityTheme {
	lines := strings.Split(content, "\n")
	themes := make([]ContinuityTheme, 0, continuityThemeLimit)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "### ") {
			continue
		}
		title := sanitizeContinuityTitle(strings.TrimPrefix(trimmed, "### "))
		if title == "" || title == "工作概览" || title == "工作详情" {
			continue
		}
		themes = appendUniqueTheme(themes, ContinuityTheme{Title: title})
		if len(themes) >= continuityThemeLimit {
			break
		}
	}
	return themes
}

func themesFromPlainText(content string) []ContinuityTheme {
	for _, line := range strings.Split(content, "\n") {
		title := sanitizeContinuityTitle(line)
		if title == "" || strings.HasPrefix(title, "## ") {
			continue
		}
		return []ContinuityTheme{{Title: title}}
	}
	return nil
}

func sanitizeContinuityTitle(value string) string {
	value = markdownLinkPattern.ReplaceAllString(value, "$1")
	value = strings.TrimSpace(strings.Trim(value, "#*_` "))
	if value == "" || strings.HasPrefix(value, "[图片]") || strings.HasPrefix(value, "粘贴图片") {
		return ""
	}
	value = strings.Join(strings.Fields(value), " ")
	if utf8.RuneCountInString(value) > continuityTitleRuneLimit {
		runes := []rune(value)
		value = strings.TrimSpace(string(runes[:continuityTitleRuneLimit]))
	}
	return value
}

func markdownIndent(value string) int {
	return len(strings.ReplaceAll(value, "\t", "    "))
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func appendUniqueTheme(themes []ContinuityTheme, theme ContinuityTheme) []ContinuityTheme {
	for _, existing := range themes {
		if existing.Title == theme.Title {
			return themes
		}
	}
	return append(themes, theme)
}

func uniqueThemes(themes []ContinuityTheme) []ContinuityTheme {
	result := make([]ContinuityTheme, 0, len(themes))
	seen := make(map[string]struct{}, len(themes))
	for _, theme := range themes {
		if _, exists := seen[theme.Title]; exists {
			continue
		}
		seen[theme.Title] = struct{}{}
		result = append(result, theme)
	}
	return result
}
