package sessiondigestv2

import (
	"regexp"
	"strings"
)

var (
	reportCodeFencePattern    = regexp.MustCompile("(?s)```.*?```")
	reportMarkdownLinkPattern = regexp.MustCompile(
		`\[([^\]]+)\]\([^)]+\)`,
	)
	reportInlineCodePattern = regexp.MustCompile("`([^`]+)`")
	reportURLPattern        = regexp.MustCompile(`(?i)https?://\S+`)
	reportPathPattern       = regexp.MustCompile(
		`(?:^|\s)(?:/[a-zA-Z0-9._\-\p{Han}]+){2,}`,
	)
	reportHashPattern = regexp.MustCompile(`(?i)\b[0-9a-f]{7,64}\b`)
	reportUUIDPattern = regexp.MustCompile(
		`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`,
	)
	reportListPrefixPattern = regexp.MustCompile(
		`^\s*(?:[-*+]|\d+[.)、])\s*`,
	)
	reportInlineListPattern = regexp.MustCompile(
		`(?:\s+|[；;])(?:[-*+]|\d+[.)、])\s+`,
	)
	reportIPAddressPattern = regexp.MustCompile(
		`\b(?:\d{1,3}\.){3}\d{1,3}(?::\d+)?\b`,
	)
	reportInternalHostPattern = regexp.MustCompile(
		`\b(?:1[0-9]|[2-9][0-9])(?:\.\d{1,3}){1,3}(?::\d+)?\b`,
	)
	reportEnvironmentSegmentPattern = regexp.MustCompile(
		`(?i)^(?:\d{2,3})\s+(?:runtime|工作树|环境|已启用|top)`,
	)
	reportDocumentCountPattern = regexp.MustCompile(
		`(?i)(?:\d[\d,，]*|[一二三四五六七八九十百千]+)\s*(?:份文档|行(?:代码|文档)?|个文件)`,
	)
)

var reportProcessMarkers = []string{
	"go test", "go vet", "npm test", "npm run", "pnpm ", "pytest",
	"sha256", "健康检查", "回放状态", "worker 状态", "e2e",
	"evidence_ref", "evidence ref", "证据引用", "验证命令",
	"验证状态", "测试状态", "测试包", "构建命令", "命令为",
	"文件路径", "文件名", "改动文件", "变更文件", "提交记录",
	"commit ", "committed", "发布说明已写入", "相关代码已单独提交",
	"已统一整理到", "版本为", "当前下载版本", "测试版",
	"本地构建产物", "下载包", "验收不完整", "文件最后修改时间",
	"正在首次下载", "正在下载依赖", "尚未进入测试", "聚焦测试",
	"我检查了生产原始", "检查了生产原始", "共享工作区",
	"真实 tty", "非 tty", "aida upload",
	"实际验证流程", "重新发布到", "最终提交为", "commit：", "commit:",
	"旧交互包袱", "文档检查通过", "已记录 q", "我会等它结束",
	"一次性本地工具安装", "下载期间", "开发方案包含本轮",
	"已完成并提交", "未修改",
	"构建基线", "远端最新提交",
	"work unit", "结果证据链", "完成状态模型", "数据库兼容",
	"全量 session manifest", "全量结构回放", "分层 a/b",
	"人工 gold", "holdout", "开发方案", "不再筛 commit",
	"统一发布", "明确禁止 canary", "shadow", "账号灰度",
	"完整上线顺序", "回退步骤", "冒烟", "排除 doc/",
	"当前唯一待处理项", "尚未上线的",
	"registry owner", "skill id", "测试账号", "主机地址",
	"cwd", "projectdir", "endedat", "startedat",
}

func reportFacingClaim(unit WorkUnit, value string) string {
	value = resultFocusedClaim(value)
	if value == "" {
		return ""
	}
	if summary := reportProductOutcomeSummary(value); summary != "" {
		return summary
	}
	if release := reportArtifactReleaseSummary(unit, value); release != "" {
		return release
	}
	result := reportFacingText(value, 3, 300)
	return alignReportFacingText(unit.Goal.Text, result)
}

func reportProductOutcomeSummary(value string) string {
	lower := strings.ToLower(value)
	if strings.Contains(lower, "clipboard api") &&
		strings.Contains(lower, "execcommand") &&
		strings.Contains(lower, "已复制") {
		return "HTTP/IP 环境下复制按钮仍可能误报成功，需补充兼容复制处理。"
	}
	if strings.Contains(lower, "发布") &&
		(strings.Contains(lower, "完整最新前端代码") ||
			strings.Contains(lower, "完整的最新前端代码")) {
		return "最新前端代码已完整发布。"
	}
	return ""
}

func reportArtifactReleaseSummary(unit WorkUnit, claim string) string {
	if !deliveryCategory(unit.Category) {
		return ""
	}
	lower := strings.ToLower(claim)
	if !containsAny(lower,
		"发布", "上线", "部署", "升级", "更新", "release", "released",
	) {
		return ""
	}
	claimVersions := artifactVersions(claim)
	if len(claimVersions) == 0 {
		return ""
	}
	unitVersions := artifactVersions(workUnitEvidenceText(unit))
	if version, ok := claimVersions["aida-cli"]; ok {
		if latest, exists := unitVersions["aida-cli"]; exists {
			version = latest
		}
		summary := "Aida CLI " + version + " 已完成发布"
		if strings.Contains(lower, "自动升级") ||
			strings.Contains(lower, "自动更新") {
			summary += "，客户端自动升级已生效"
		}
		return summary + "。"
	}
	if version, ok := claimVersions["aida-report"]; ok {
		if latest, exists := unitVersions["aida-report"]; exists {
			version = latest
		}
		return "Aida Report " + version + " 已完成发布。"
	}
	if _, ok := claimVersions["session-digest"]; ok {
		return "日报 Session 摘要机制已完成本轮优化。"
	}
	return ""
}

func alignReportFacingText(goal, value string) string {
	if value == "" {
		return ""
	}
	goalLower := strings.ToLower(goal)
	goalIsAutoUpdate := strings.Contains(goalLower, "自动升级") ||
		strings.Contains(goalLower, "自动更新") ||
		strings.Contains(goalLower, "install.sh")
	parts := strings.Split(strings.TrimSuffix(value, "。"), "；")
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		lower := strings.ToLower(part)
		if !goalIsAutoUpdate &&
			(strings.Contains(lower, "自动升级") ||
				strings.Contains(lower, "自动更新")) {
			continue
		}
		kept = append(kept, part)
	}
	if len(kept) == 0 {
		return ""
	}
	return strings.Join(kept, "；") + "。"
}

func reportFacingResultKey(value string) string {
	versions := artifactVersions(value)
	for _, artifact := range []string{"aida-cli", "aida-report", "session-digest"} {
		if _, exists := versions[artifact]; exists {
			return "artifact:" + artifact
		}
	}
	return canonicalKey(value)
}

func hasReportFacingWorkUnit(unit WorkUnit) bool {
	if unit.Category == "verification" && !hasDeliveredOutcomeClaim(unit) {
		return false
	}
	for _, statement := range unit.ResultStatements {
		if reportFacingClaim(unit, statement.Text) != "" {
			return true
		}
	}
	return len(reportFacingUnresolved(unit.Unresolved, 1)) > 0
}

func reportFacingGoal(unit WorkUnit, results []ResultStatement) string {
	goal := reportFacingText(unit.Goal.Text, 1, 160)
	lower := strings.ToLower(goal)
	if goal == "" ||
		strings.HasSuffix(goal, "？") ||
		strings.HasSuffix(goal, "?") ||
		strings.Contains(lower, "是否") ||
		strings.Contains(lower, "可以借鉴么") ||
		strings.Contains(lower, "可以借鉴吗") ||
		isVagueReportSegment(lower) {
		if len(results) == 0 {
			return ""
		}
		return firstOutcomeSentence(results[0].Text)
	}
	return goal
}

func reportFacingUnresolved(values []Unresolved, limit int) []Unresolved {
	if limit <= 0 {
		return []Unresolved{}
	}
	result := make([]Unresolved, 0, min(len(values), limit))
	seen := map[string]struct{}{}
	for _, value := range values {
		text := reportFacingText(value.Text, 2, 240)
		key := canonicalKey(text)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, Unresolved{Text: text})
		if len(result) == limit {
			break
		}
	}
	return result
}

func reportFacingText(value string, segmentLimit, byteLimit int) string {
	value = reportCodeFencePattern.ReplaceAllString(value, "\n")
	value = reportMarkdownLinkPattern.ReplaceAllString(value, "$1")
	value = reportInlineCodePattern.ReplaceAllString(value, "$1")
	value = reportURLPattern.ReplaceAllString(value, " ")
	value = strings.ReplaceAll(value, "**", "")
	value = reportInlineListPattern.ReplaceAllString(value, "\n")
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")

	segments := make([]string, 0, segmentLimit)
	seen := map[string]struct{}{}
	validationTail := false
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "|") || strings.Count(line, "|") >= 3 {
			continue
		}
		line = reportListPrefixPattern.ReplaceAllString(line, "")
		for _, segment := range splitReportSegments(line) {
			if incompleteReportSegment(segment) {
				continue
			}
			segment = cleanReportSegment(segment)
			if segment == "" {
				continue
			}
			if incompleteReportSegment(segment) {
				continue
			}
			lower := strings.ToLower(segment)
			if isVagueReportSegment(lower) {
				continue
			}
			if isValidationHeading(lower) {
				validationTail = true
				continue
			}
			if validationTail && isReportProcessSegment(lower) {
				continue
			}
			if !isReportProcessSegment(lower) {
				validationTail = false
			}
			if isReportProcessSegment(lower) {
				continue
			}
			key := canonicalKey(segment)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			segments = append(segments, segment)
			if len(segments) == segmentLimit {
				break
			}
		}
		if len(segments) == segmentLimit {
			break
		}
	}
	if len(segments) == 0 {
		return ""
	}
	result := strings.Join(segments, "；")
	result = strings.TrimRight(result, "：:；;，,。.!！ ")
	if result == "" {
		return ""
	}
	if !strings.HasSuffix(result, "。") &&
		!strings.HasSuffix(result, "！") &&
		!strings.HasSuffix(result, "？") {
		result += "。"
	}
	result, _ = truncateUTF8Bytes(result, byteLimit)
	return result
}

func splitReportSegments(value string) []string {
	result := []string{}
	start := 0
	for index, current := range value {
		if current != '。' && current != '；' && current != '！' &&
			current != '？' {
			continue
		}
		result = append(result, value[start:index+len(string(current))])
		start = index + len(string(current))
	}
	if start < len(value) {
		result = append(result, value[start:])
	}
	return result
}

func cleanReportSegment(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimLeft(value, "#>* ")
	value = reportUUIDPattern.ReplaceAllString(value, "")
	value = reportHashPattern.ReplaceAllString(value, "")
	value = reportIPAddressPattern.ReplaceAllString(value, "")
	value = reportInternalHostPattern.ReplaceAllString(value, "")
	value = reportPathPattern.ReplaceAllString(value, " ")
	for _, marker := range []string{
		"，并部署至", "，部署至", "，并回灌到", "，回灌到",
		"，构建基线", "，远端最新提交",
	} {
		if index := strings.Index(value, marker); index >= 0 {
			value = strings.TrimSpace(value[:index])
		}
	}
	value = strings.Join(strings.Fields(value), " ")
	value = strings.Trim(value, "：:；;，,。.!！ ")
	if strings.HasPrefix(value, "运行时使用 ") {
		return ""
	}
	for _, marker := range []string{"，运行时使用", ", runtime uses"} {
		if index := strings.Index(strings.ToLower(value), marker); index >= 0 {
			value = strings.TrimSpace(value[:index])
		}
	}
	if value == "" {
		return ""
	}
	return value
}

func incompleteReportSegment(value string) bool {
	value = strings.TrimSpace(value)
	lower := strings.ToLower(value)
	return strings.HasSuffix(value, "(") ||
		strings.HasSuffix(value, "（") ||
		strings.HasSuffix(value, "如下") ||
		strings.HasSuffix(value, "结构是") ||
		strings.HasSuffix(value, "...") ||
		strings.HasSuffix(value, "…") ||
		strings.Contains(lower, "](")
}

func isValidationHeading(lower string) bool {
	lower = strings.TrimSpace(lower)
	return lower == "已验证" ||
		lower == "验证" ||
		lower == "测试" ||
		strings.HasPrefix(lower, "已验证：") ||
		strings.HasPrefix(lower, "验证：") ||
		strings.HasPrefix(lower, "测试：")
}

func isVagueReportSegment(lower string) bool {
	lower = strings.TrimSpace(strings.Trim(lower, "：:；;，,。.!！ "))
	switch lower {
	case "ok", "好的", "明白", "收到", "结论", "已完成", "完成",
		"已确认", "实际验证流程", "显示规则", "开始吧", "开始",
		"可以", "继续", "做吧", "可以开始":
		return true
	default:
		return false
	}
}

func isReportProcessSegment(lower string) bool {
	if reportEnvironmentSegmentPattern.MatchString(lower) {
		return true
	}
	if strings.Contains(lower, "已更新") && strings.Contains(lower, "文档") {
		return true
	}
	if strings.Contains(lower, "文档") &&
		reportDocumentCountPattern.MatchString(lower) {
		return true
	}
	for _, marker := range reportProcessMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	if strings.Contains(lower, ".md") ||
		strings.Contains(lower, ".go") ||
		strings.Contains(lower, ".ts") ||
		strings.Contains(lower, ".tsx") ||
		strings.Contains(lower, ".sh") ||
		strings.Contains(lower, "--") {
		return true
	}
	return false
}
