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
	reportMetadataDatePattern             = regexp.MustCompile(`(?m)(?:报告日期|日报日期|报告生成时间|生成时间)\s*[:：]\s*(20[0-9]{2})-([0-9]{1,2})-([0-9]{1,2})`)
	reportTodayDatePattern                = regexp.MustCompile(`(?m)(?:今日|当天)\s*(?:[（(]\s*)?(20[0-9]{2})-([0-9]{1,2})-([0-9]{1,2})`)
	reportYesterdayDatePattern            = regexp.MustCompile(`(?m)昨日\s*(?:[（(]\s*)?(20[0-9]{2})-([0-9]{1,2})-([0-9]{1,2})`)
	reportNoPersonalDailyPattern          = regexp.MustCompile(`(?m)(?:(?:^|[。；;\n])\s*(?:(?:本)?(?:小组|团队)(?:当日|本周)?\s*)?无(?:任何)?个人日报(?:记录)?|个人日报\s*[:：]?\s*(?:0\s*份|零份|无人提交|均未提交))`)
	reportNoPersonalWeeklyPattern         = regexp.MustCompile(`(?m)(?:(?:^|[。；;\n])\s*(?:(?:本)?(?:小组|团队)(?:当日|本周)?\s*)?无(?:任何)?个人周报(?:记录)?|个人周报\s*[:：]?\s*(?:0\s*份|零份|无人提交|均未提交))`)
	reportNoTeamDailyPattern              = regexp.MustCompile(`(?m)(?:(?:^|[。；;\n])\s*(?:(?:本)?部门(?:当日|本周)?\s*)?无(?:任何)?(?:小组|团队)日报(?:记录)?|(?:小组|团队)日报\s*[:：]?\s*(?:0\s*份|零份|无人提交|均未提交))`)
	reportNoTeamWeeklyPattern             = regexp.MustCompile(`(?m)(?:(?:^|[。；;\n])\s*(?:(?:本)?部门(?:当日|本周)?\s*)?无(?:任何)?(?:小组|团队)周报(?:记录)?|(?:小组|团队)周报\s*[:：]?\s*(?:0\s*份|零份|无人提交|均未提交))`)
	reportNoSessionActivityPattern        = regexp.MustCompile(`(?im)(?:^|[。；;\n])\s*(?:(?:今日|本日|本周)\s*)?(?:无|暂无)(?:直接)?(?:工作|活动|会话|session)?(?:详细)?记录`)
	reportFullParticipationPattern        = regexp.MustCompile(`(?:全员参与|全部在岗|所有成员(?:均|都)?(?:参与|完成|有(?:工作|记录|产出))|全体成员(?:均|都)?(?:参与|完成|有(?:工作|记录|产出)))`)
	reportDailyCrossPeriodCoveragePattern = regexp.MustCompile(`(?:个人|小组|团队|部门)周报[^\n]{0,24}(?:缺失|提交|覆盖|无人|无记录)`)
	reportEngineeringSectionPattern       = regexp.MustCompile(`(?im)^#{1,6}\s*(?:验证结果|验证状态|测试情况|测试结果|文件变更|代码变更|变更文件|技术验证)\s*$`)
	reportFileChangeMetricPattern         = regexp.MustCompile(`(?i)(?:共产生|产生|涉及|修改|变更)[^\n。；]{0,24}[0-9]+\s*(?:项|个|处)?\s*(?:文件变更|文件|代码变更)`)
	reportValidationAttemptPattern        = regexp.MustCompile(`(?i)(?:go test|go vet|pytest|npm test|pnpm test|测试|验证)[^\n。；]{0,28}[0-9]+\s*次(?:尝试|执行|测试|验证)?`)
	reportWorkItemAggregatePattern        = regexp.MustCompile(`(?i)(?:今日|当天)?[^\n。；]{0,12}[0-9]+\s*个有结果工作项|[0-9]+\s*个(?:已完成|进行中|pending)\b`)
	reportVersionedAssetPattern           = regexp.MustCompile(`(?i)\b([a-z][a-z0-9._-]{2,})@v?([0-9]+\.[0-9]+\.[0-9]+)\b`)
	reportNamedDigestVersionPattern       = regexp.MustCompile(`(?i)\b(?:session[\s_-]*)?digest\s+v?([0-9]+\.[0-9]+(?:\.[0-9]+)?)\b`)
	reportInternalAssetLabelPattern       = regexp.MustCompile(`(?i)\bRegistry\s+owner\b|Skill\s*ID\s*[:：]|\bSkill\s+v?[0-9]+\.[0-9]+|测试账[号户]`)
	reportInternalHostPattern             = regexp.MustCompile(`(?i)\b(?:192\.168\.\d{1,3}\.\d{1,3}|10\.\d{1,3}\.\d{1,3}\.\d{1,3}|172\.(?:1[6-9]|2[0-9]|3[01])\.\d{1,3}\.\d{1,3}|(?:14|18)\.\d{2,3})\b`)
	reportOperationalValidationPattern    = regexp.MustCompile(`(?i)(?:\bE2E\b|Top\s*[0-9]+[^\n。；]{0,24}回放|健康检查|启动日志|回放通过|(?:验证|测试|检查|回放|构建)[^\n。；]{0,16}(?:通过|正常|成功)|\b(?:worker|reconciler)\b)`)
	reportURLPattern                      = regexp.MustCompile(`(?i)https?://`)
	reportTechnicalArtifactPattern        = regexp.MustCompile("(?i)(?:`[^`\\n]*(?:/|\\.(?:go|md|sql|ts|tsx|js|json|ya?ml|sh|ps1))[^`\\n]*`|(?:^|[\\s（(])/(?:[^\\s/]+/)+[^\\s`，。；)）]+)")
	reportTopLevelOutcomePattern          = regexp.MustCompile(`(?m)^(?:[0-9]+[.)、]|[-*+])\s+`)
)

func reportContentValidationIssues(content, reportType, date, weekStart, weekEnd string) []string {
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
	if reportEngineeringSectionPattern.MatchString(content) {
		add("报告正文不得单列验证、测试或文件变更章节；请把必要验证收敛为简短成果描述")
	}
	if hasEmptyReportFollowupSection(content) {
		add("进行中与待跟进为空时必须整段省略，不得填写“无”或“暂无”")
	}
	if reportFileChangeMetricPattern.MatchString(content) {
		add("报告正文不得展示文件或代码变更数量；文件证据仅用于内部判断结果可信度")
	}
	if reportValidationAttemptPattern.MatchString(content) {
		add("报告正文不得展示测试命令或验证尝试次数；请只保留用户可理解的最终结果")
	}
	if reportWorkItemAggregatePattern.MatchString(content) {
		add("报告正文不得使用 Digest 工作项或状态汇总数量；请直接概括少量重要成果")
	}
	if reportInternalAssetLabelPattern.MatchString(content) {
		add("报告正文包含内部资产管理标识，请删除 Registry owner、Skill ID 等运维细节")
	}
	if reportInternalHostPattern.MatchString(content) {
		add("报告正文包含内部主机或网络地址，请改写为用户可理解的产品或环境描述")
	}
	if reportOperationalValidationPattern.MatchString(content) {
		add("报告正文包含验证通过、健康检查、启动日志、E2E、Worker 或回放状态；这些只用于内部置信判断，不应作为日报成果")
	}
	if reportURLPattern.MatchString(content) {
		add("报告正文包含原始 URL；请使用产品、项目或能力名称概括成果")
	}
	if reportTechnicalArtifactPattern.MatchString(content) {
		add("报告正文包含代码路径、文件名或脚本名；请改写为用户可理解的交付结果")
	}
	if asset := conflictingVersionedAsset(content); asset != "" {
		add(fmt.Sprintf("报告正文同时包含 %s 的多个版本；请只保留最新有效版本", asset))
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
	if date != "" {
		for _, match := range reportMetadataDatePattern.FindAllStringSubmatch(content, -1) {
			if shown, ok := dateFromMatch(match); ok && shown != date {
				add(fmt.Sprintf("日报元信息日期必须为 %s，正文写成了 %s", date, shown))
			}
		}
		for _, match := range reportTodayDatePattern.FindAllStringSubmatch(content, -1) {
			if shown, ok := dateFromMatch(match); ok && shown != date {
				add(fmt.Sprintf("日报中的今日/当天必须指 %s，正文写成了 %s", date, shown))
			}
		}
		if reportDay, err := time.Parse("2006-01-02", date); err == nil {
			expectedYesterday := reportDay.AddDate(0, 0, -1).Format("2006-01-02")
			for _, match := range reportYesterdayDatePattern.FindAllStringSubmatch(content, -1) {
				if shown, ok := dateFromMatch(match); ok && shown != expectedYesterday {
					add(fmt.Sprintf("日报中的昨日应指 %s，正文写成了 %s", expectedYesterday, shown))
				}
			}
		}
	}
	return issues
}

func hasEmptyReportFollowupSection(content string) bool {
	lines := strings.Split(content, "\n")
	for index, line := range lines {
		line = strings.NewReplacer("**", "", "__", "", "`", "").Replace(line)
		line = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#"))
		matchedHeading := ""
		for _, heading := range []string{"进行中与待跟进", "进行中", "待跟进"} {
			if line == heading || strings.HasPrefix(line, heading+"：") ||
				strings.HasPrefix(line, heading+":") {
				matchedHeading = heading
				break
			}
		}
		if matchedHeading == "" {
			continue
		}
		inline := strings.TrimSpace(strings.TrimLeft(
			strings.TrimPrefix(line, matchedHeading), "：:",
		))
		if inline != "" {
			return isEmptyReportFollowupValue(inline)
		}
		for next := index + 1; next < len(lines); next++ {
			value := strings.TrimSpace(lines[next])
			if value == "" {
				continue
			}
			if strings.HasPrefix(value, "#") || strings.HasPrefix(value, "---") {
				return true
			}
			value = reportTopLevelOutcomePattern.ReplaceAllString(value, "")
			return isEmptyReportFollowupValue(value)
		}
		return true
	}
	return false
}

func isEmptyReportFollowupValue(value string) bool {
	value = strings.Trim(value, " \t-*+0123456789.)、（）()：:")
	return value == "无" || value == "暂无" || value == "无待跟进事项"
}

func isPersonalReportType(reportType string) bool {
	return reportType == reportTypePersonalDaily || reportType == reportTypePersonalWeekly
}

func conflictingVersionedAsset(content string) string {
	versions := map[string]string{}
	for _, match := range reportVersionedAssetPattern.FindAllStringSubmatch(content, -1) {
		asset := strings.ToLower(match[1])
		version := match[2]
		if existing, ok := versions[asset]; ok && existing != version {
			return asset
		}
		versions[asset] = version
	}
	for _, match := range reportNamedDigestVersionPattern.FindAllStringSubmatch(content, -1) {
		version := match[1]
		if existing, ok := versions["session-digest"]; ok && existing != version {
			return "session-digest"
		}
		versions["session-digest"] = version
	}
	return ""
}

func dateFromMatch(match []string) (string, bool) {
	if len(match) < 4 {
		return "", false
	}
	year, _ := strconv.Atoi(match[1])
	month, _ := strconv.Atoi(match[2])
	day, _ := strconv.Atoi(match[3])
	value, ok := validCalendarDate(year, month, day)
	if !ok {
		return "", false
	}
	return value.Format("2006-01-02"), true
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
	readMode := strings.TrimSpace(stringFromAny(inputRef["report_source_read_mode"]))
	if readMode == "digest_v1" || readMode == "digest_v2" {
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
