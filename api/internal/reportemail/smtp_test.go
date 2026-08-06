package reportemail

import (
	"strings"
	"testing"
)

func TestSMTPMessageRejectsHeaderInjection(t *testing.T) {
	if err := validateMessage(Message{To: "a@example.com", Subject: "safe\nBcc: victim@example.com"}); err == nil {
		t.Fatal("expected subject newline to be rejected")
	}
}

func TestSMTPBuildsMultipartAlternative(t *testing.T) {
	mailer, err := NewSMTPMailer(SMTPConfig{Host: "smtp.example.com", Port: 587, From: "sender@example.com", FromName: "Aida", TLSMode: "starttls"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := mailer.buildMessage(Message{To: "user@example.com", Subject: "日报", TextBody: "plain", HTMLBody: "<p>html</p>"})
	if err != nil {
		t.Fatal(err)
	}
	value := string(raw)
	for _, expected := range []string{"multipart/alternative", "text/plain", "text/html", "plain", "<p>html</p>"} {
		if !strings.Contains(value, expected) {
			t.Fatalf("message missing %q: %s", expected, value)
		}
	}
}
