package reportemail

import (
	"fmt"
	"html"
	"strings"
	"time"
)

func personalDelivery(date time.Time, candidate PersonalCandidate) Delivery {
	dateText := date.Format("2006-01-02")
	delivery := Delivery{
		ReportDate: date, Type: DeliveryPersonal, RecipientUserID: candidate.UserID,
		RecipientEmail: candidate.Email,
	}
	switch {
	case strings.TrimSpace(candidate.Email) == "":
		delivery.Status = "skipped"
		delivery.SkipReason = "缺少可信企业邮箱"
	case strings.TrimSpace(candidate.Content) == "":
		delivery.Status = "skipped"
		delivery.SkipReason = "缺少日报"
	default:
		delivery.Status = "pending"
		delivery.Subject = fmt.Sprintf("[Aida 日报] %s %s", dateText, candidate.DisplayName)
		delivery.TextBody = fmt.Sprintf("%s，您好：\n\n以下是您 %s 的个人日报：\n\n%s\n", candidate.DisplayName, dateText, candidate.Content)
		delivery.HTMLBody = fmt.Sprintf("<p>%s，您好：</p><p>以下是您 %s 的个人日报：</p><pre style=\"white-space:pre-wrap\">%s</pre>", html.EscapeString(candidate.DisplayName), dateText, html.EscapeString(candidate.Content))
	}
	return delivery
}

func teamDelivery(date time.Time, candidate TeamCandidate) Delivery {
	dateText := date.Format("2006-01-02")
	delivery := Delivery{
		ReportDate: date, Type: DeliveryTeamSummary, RecipientUserID: candidate.LeaderUserID,
		TeamID: candidate.TeamID, RecipientEmail: candidate.LeaderEmail,
	}
	if strings.TrimSpace(candidate.LeaderEmail) == "" {
		delivery.Status = "skipped"
		delivery.SkipReason = "TL 缺少可信企业邮箱"
		return delivery
	}
	delivery.Status = "pending"
	delivery.Subject = fmt.Sprintf("[Aida 小组日报汇总] %s %s", dateText, candidate.TeamName)
	var textBody strings.Builder
	fmt.Fprintf(&textBody, "%s，您好：\n\n以下是 %s 在 %s 的成员日报汇总：\n\n", candidate.LeaderName, candidate.TeamName, dateText)
	var rows strings.Builder
	for _, member := range candidate.Members {
		status := "已生成"
		content := strings.TrimSpace(member.Content)
		if content == "" {
			status = "缺少日报"
			content = "—"
		}
		fmt.Fprintf(&textBody, "[%s] %s\n%s\n\n", status, member.DisplayName, content)
		fmt.Fprintf(&rows, "<tr><td>%s</td><td>%s</td><td><pre style=\"white-space:pre-wrap;margin:0\">%s</pre></td></tr>", html.EscapeString(member.DisplayName), status, html.EscapeString(content))
	}
	delivery.TextBody = textBody.String()
	delivery.HTMLBody = fmt.Sprintf("<p>%s，您好：</p><p>以下是 %s 在 %s 的成员日报汇总：</p><table border=\"1\" cellspacing=\"0\" cellpadding=\"6\"><thead><tr><th>成员</th><th>状态</th><th>日报正文</th></tr></thead><tbody>%s</tbody></table>", html.EscapeString(candidate.LeaderName), html.EscapeString(candidate.TeamName), dateText, rows.String())
	return delivery
}
