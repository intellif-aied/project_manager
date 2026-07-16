package handler

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	reportUUIDPattern                     = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	reportRawIDFieldPattern               = regexp.MustCompile(`(?i)\b(?:user_id|team_id|department_id|leader_id|director_user_id|report_id|session_id|run_id|uuid)\b`)
	reportIDLabelPattern                  = regexp.MustCompile(`(?i)(?:用户|成员|团队|小组|部门|负责人|总监|报告|会话|运行)\s*(?:id|编号)`)
	reportISOWeekdayPattern               = regexp.MustCompile(`(20[0-9]{2})-([0-9]{1,2})-([0-9]{1,2})\s*(?:[（(]\s*)?(?:周|星期)([一二三四五六日天])\s*[）)]?`)
	reportChineseWeekdayPattern           = regexp.MustCompile(`(?:(20[0-9]{2})\s*年\s*)?([0-9]{1,2})\s*月\s*([0-9]{1,2})\s*日\s*(?:[（(]\s*)?(?:周|星期)([一二三四五六日天])\s*[）)]?`)
	reportNoPersonalDailyPattern          = regexp.MustCompile(`(?m)(?:(?:^|[。；;\n])\s*(?:(?:本)?(?:小组|团队)(?:当日|本周)?\s*)?无(?:任何)?个人日报(?:记录)?|个人日报\s*[:：]?\s*(?:0\s*份|零份|无人提交|均未提交))`)
	reportNoPersonalWeeklyPattern         = regexp.MustCompile(`(?m)(?:(?:^|[。；;\n])\s*(?:(?:本)?(?:小组|团队)(?:当日|本周)?\s*)?无(?:任何)?个人周报(?:记录)?|个人周报\s*[:：]?\s*(?:0\s*份|零份|无人提交|均未提交))`)
	reportNoTeamDailyPattern              = regexp.MustCompile(`(?m)(?:(?:^|[。；;\n])\s*(?:(?:本)?部门(?:当日|本周)?\s*)?无(?:任何)?(?:小组|团队)日报(?:记录)?|(?:小组|团队)日报\s*[:：]?\s*(?:0\s*份|零份|无人提交|均未提交))`)
	reportNoTeamWeeklyPattern             = regexp.MustCompile(`(?m)(?:(?:^|[。；;\n])\s*(?:(?:本)?部门(?:当日|本周)?\s*)?无(?:任何)?(?:小组|团队)周报(?:记录)?|(?:小组|团队)周报\s*[:：]?\s*(?:0\s*份|零份|无人提交|均未提交))`)
	reportNoSessionActivityPattern        = regexp.MustCompile(`(?im)(?:^|[。；;\n])\s*(?:(?:今日|本日|本周)\s*)?(?:无|暂无)(?:直接)?(?:工作|活动|会话|session)?(?:详细)?记录`)
	reportFullParticipationPattern        = regexp.MustCompile(`(?:全员参与|全部在岗|所有成员(?:均|都)?(?:参与|完成|有(?:工作|记录|产出))|全体成员(?:均|都)?(?:参与|完成|有(?:工作|记录|产出)))`)
	reportDailyCrossPeriodCoveragePattern = regexp.MustCompile(`(?:个人|小组|团队|部门)周报[^\n]{0,24}(?:缺失|提交|覆盖|无人|无记录)`)
)

func reportContentValidationIssues(content, date, weekStart, weekEnd string) []string {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	issues := []string{}
	seen := map[string]struct{}{}
	add := func(issue string) {
		if issue == "" {
			return
		}
		if _, ok := seen[issue]; ok {
			return
		}
		seen[issue] = struct{}{}
		issues = append(issues, issue)
	}

	plain := strings.NewReplacer("**", "", "__", "", "`", "").Replace(content)
	if reportRawIDFieldPattern.MatchString(plain) || reportIDLabelPattern.MatchString(plain) {
		add("报告正文包含内部 ID 字段或 ID/编号标签，请改用展示名称")
	}
	if reportUUIDPattern.MatchString(content) {
		add("报告正文包含 UUID，请删除内部标识")
	}

	periodStart, periodEnd := reportValidationPeriodBounds(date, weekStart, weekEnd)
	for _, match := range reportISOWeekdayPattern.FindAllStringSubmatch(content, -1) {
		year, _ := strconv.Atoi(match[1])
		month, _ := strconv.Atoi(match[2])
		day, _ := strconv.Atoi(match[3])
		if value, ok := validCalendarDate(year, month, day); ok {
			addWeekdayValidationIssue(add, value, match[4])
		}
	}
	for _, match := range reportChineseWeekdayPattern.FindAllStringSubmatch(content, -1) {
		month, _ := strconv.Atoi(match[2])
		day, _ := strconv.Atoi(match[3])
		var value time.Time
		var ok bool
		if match[1] != "" {
			year, _ := strconv.Atoi(match[1])
			value, ok = validCalendarDate(year, month, day)
		} else {
			value, ok = resolvePartialReportDate(month, day, periodStart, periodEnd)
		}
		if ok {
			addWeekdayValidationIssue(add, value, match[4])
		}
	}
	return issues
}

func reportPersonalSourceActivityIssues(ctx context.Context, db *sql.DB, inputRef map[string]any, reportType, content string) ([]string, error) {
	if reportType != reportTypePersonalDaily && reportType != reportTypePersonalWeekly {
		return nil, nil
	}
	if !reportNoSessionActivityPattern.MatchString(content) {
		return nil, nil
	}
	selectionID := strings.TrimSpace(stringFromAny(inputRef["report_source_selection_id"]))
	if selectionID == "" {
		return nil, nil
	}
	var hasWorkActivity bool
	var err error
	if strings.TrimSpace(stringFromAny(inputRef["report_source_read_mode"])) == "digest_v1" {
		err = db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM report_source_selection_items i
				JOIN session_slice_digest_revisions d ON d.id = i.digest_revision_id
				WHERE i.selection_id = $1 AND d.status = 'ready'
					AND d.included_event_count > 0
			)`, selectionID).Scan(&hasWorkActivity)
	} else {
		err = db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM report_source_selection_items i
			JOIN session_content_events e
				ON e.content_projection_revision_id = i.content_projection_revision_id
				AND e.source_start_cursor >= i.start_cursor
				AND e.source_end_cursor <= i.end_cursor
			WHERE i.selection_id = $1
				AND e.event_type IN ('event_msg.user_message', 'event_msg.agent_message', 'response_item.message')
				AND NULLIF(btrim(COALESCE(e.summary, e.excerpt, '')), '') IS NOT NULL
		)`, selectionID).Scan(&hasWorkActivity)
	}
	if err != nil {
		return nil, err
	}
	if !hasWorkActivity {
		return nil, nil
	}
	return []string{"本次 Session 来源快照包含工作记录，正文不能声称无活动或无记录；请依据完整快照事实重新生成"}, nil
}

func reportValidationPeriodBounds(date, weekStart, weekEnd string) (time.Time, time.Time) {
	if date != "" {
		value, _ := time.Parse("2006-01-02", date)
		return value, value
	}
	start, _ := time.Parse("2006-01-02", weekStart)
	end, _ := time.Parse("2006-01-02", weekEnd)
	return start, end
}

func validCalendarDate(year, month, day int) (time.Time, bool) {
	if year <= 0 || month <= 0 || day <= 0 {
		return time.Time{}, false
	}
	value := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if value.Year() != year || int(value.Month()) != month || value.Day() != day {
		return time.Time{}, false
	}
	return value, true
}

func resolvePartialReportDate(month, day int, periodStart, periodEnd time.Time) (time.Time, bool) {
	years := []int{periodStart.Year()}
	if periodEnd.Year() != 0 && periodEnd.Year() != periodStart.Year() {
		years = append(years, periodEnd.Year())
	}
	var first time.Time
	for _, year := range years {
		value, ok := validCalendarDate(year, month, day)
		if !ok {
			continue
		}
		if first.IsZero() {
			first = value
		}
		if !periodStart.IsZero() && !periodEnd.IsZero() && !value.Before(periodStart) && !value.After(periodEnd) {
			return value, true
		}
	}
	return first, !first.IsZero()
}

func addWeekdayValidationIssue(add func(string), value time.Time, shown string) {
	want := reportWeekdayLabel(value)
	shown = "周" + strings.ReplaceAll(shown, "天", "日")
	if shown == want {
		return
	}
	add(fmt.Sprintf("日期 %s 的星期应为%s，正文写成了%s；请修正或删除星期", value.Format("2006-01-02"), want, shown))
}

func reportSourceConsistencyIssues(ctx context.Context, db *sql.DB, reportType, date, weekStart, weekEnd string, target reportTarget, content string) ([]string, error) {
	issues := []string{}
	var periodValue, scopeID, sourceLabel, countQuery, rosterQuery, rosterUnit string
	zeroClaim := false
	switch reportType {
	case reportTypeTeamDaily:
		zeroClaim = reportNoPersonalDailyPattern.MatchString(content)
		periodValue, scopeID, sourceLabel, rosterUnit = date, target.TeamID, "个人日报", "人"
		countQuery = `
			SELECT COUNT(*)
			FROM daily_reports r
			JOIN users u ON u.id = r.user_id
			WHERE r.report_date = $1 AND u.team_id::text = $2
				AND r.status IS NOT NULL
				AND NULLIF(TRIM(COALESCE(r.content, '')), '') IS NOT NULL`
		rosterQuery = `SELECT COUNT(*) FROM users u WHERE u.team_id::text = $1`
	case reportTypeDepartmentDaily:
		zeroClaim = reportNoTeamDailyPattern.MatchString(content)
		periodValue, scopeID, sourceLabel, rosterUnit = date, target.DepartmentID, "小组日报", "个小组"
		countQuery = `
			SELECT COUNT(*)
			FROM team_reports r
			JOIN teams t ON t.id = r.team_id
			WHERE r.report_date = $1 AND t.department_id::text = $2
				AND r.status IS NOT NULL
				AND NULLIF(TRIM(COALESCE(r.content, '')), '') IS NOT NULL`
		rosterQuery = `SELECT COUNT(*) FROM teams t WHERE t.department_id::text = $1`
	case reportTypeTeamWeekly:
		zeroClaim = reportNoPersonalWeeklyPattern.MatchString(content)
		periodValue, scopeID, sourceLabel, rosterUnit = weekStart, target.TeamID, "个人周报", "人"
		countQuery = `
			SELECT COUNT(*)
			FROM personal_weekly_reports r
			JOIN users u ON u.id = r.user_id
			WHERE r.week_start = $1 AND u.team_id::text = $2
				AND r.status IS NOT NULL
				AND NULLIF(TRIM(COALESCE(r.content, '')), '') IS NOT NULL`
		rosterQuery = `SELECT COUNT(*) FROM users u WHERE u.team_id::text = $1`
	case reportTypeDepartmentWeekly:
		zeroClaim = reportNoTeamWeeklyPattern.MatchString(content)
		periodValue, scopeID, sourceLabel, rosterUnit = weekStart, target.DepartmentID, "小组周报", "个小组"
		countQuery = `
			SELECT COUNT(*)
			FROM team_weekly_reports r
			JOIN teams t ON t.id = r.team_id
			WHERE r.week_start = $1 AND t.department_id::text = $2
				AND NULLIF(TRIM(COALESCE(r.content, '')), '') IS NOT NULL`
		rosterQuery = `SELECT COUNT(*) FROM teams t WHERE t.department_id::text = $1`
	default:
		return nil, nil
	}
	if (reportType == reportTypeTeamDaily || reportType == reportTypeDepartmentDaily) && reportDailyCrossPeriodCoveragePattern.MatchString(content) {
		issues = append(issues, "日报正文不应统计个人/小组周报的提交或缺失覆盖；请只保留日报来源事实")
	}
	fullParticipationClaim := reportFullParticipationPattern.MatchString(content)
	if !zeroClaim && !fullParticipationClaim {
		return issues, nil
	}
	if scopeID == "" || periodValue == "" {
		return issues, nil
	}

	var count int
	if err := db.QueryRowContext(ctx, countQuery, periodValue, scopeID).Scan(&count); err != nil {
		return nil, err
	}
	period := periodValue
	if weekEnd != "" {
		period += " 至 " + weekEnd
	}
	if zeroClaim && count > 0 {
		issues = append(issues, fmt.Sprintf("数据源在 %s 实际包含 %d 份%s，正文却声称没有或为 0 份；请按下级报告正文修正覆盖情况", period, count, sourceLabel))
	}
	if fullParticipationClaim {
		var rosterCount int
		if err := db.QueryRowContext(ctx, rosterQuery, scopeID).Scan(&rosterCount); err != nil {
			return nil, err
		}
		if count < rosterCount {
			issues = append(issues, fmt.Sprintf("范围共有 %d%s，但 %s数据源仅覆盖 %d%s；禁止声称全部范围均有产出", rosterCount, rosterUnit, sourceLabel, count, rosterUnit))
		}
	}
	return issues, nil
}
