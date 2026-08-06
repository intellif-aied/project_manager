package reportemail

import (
	"strings"
	"testing"
	"time"
)

func TestPersonalDeliverySkipsMissingInputs(t *testing.T) {
	date := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	if got := personalDelivery(date, PersonalCandidate{UserID: "1", Content: "report"}); got.Status != "skipped" || got.SkipReason != "缺少可信企业邮箱" {
		t.Fatalf("missing email delivery = %#v", got)
	}
	if got := personalDelivery(date, PersonalCandidate{UserID: "1", Email: "a@example.com"}); got.Status != "skipped" || got.SkipReason != "缺少日报" {
		t.Fatalf("missing report delivery = %#v", got)
	}
}

func TestRenderEscapesHTML(t *testing.T) {
	delivery := personalDelivery(time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC), PersonalCandidate{UserID: "1", DisplayName: "<TL>", Email: "a@example.com", Content: "<script>alert(1)</script>"})
	if strings.Contains(delivery.HTMLBody, "<script>") || !strings.Contains(delivery.HTMLBody, "&lt;script&gt;") {
		t.Fatalf("HTML body was not escaped: %s", delivery.HTMLBody)
	}
}
