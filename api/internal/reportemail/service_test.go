package reportemail

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeStore struct {
	personal   []PersonalCandidate
	teams      []TeamCandidate
	deliveries map[string]Delivery
	queue      []string
	sent       []string
	failed     []string
}

func (store *fakeStore) ListPersonalCandidates(context.Context, time.Time) ([]PersonalCandidate, error) {
	return store.personal, nil
}
func (store *fakeStore) ListTeamCandidates(context.Context, time.Time) ([]TeamCandidate, error) {
	return store.teams, nil
}
func (store *fakeStore) CreateDelivery(_ context.Context, delivery Delivery) error {
	if store.deliveries == nil {
		store.deliveries = map[string]Delivery{}
	}
	key := delivery.ReportDate.Format("2006-01-02") + ":" + delivery.Type + ":" + delivery.RecipientUserID
	if _, exists := store.deliveries[key]; exists {
		return nil
	}
	delivery.ID = key
	store.deliveries[key] = delivery
	if delivery.Status == "pending" {
		store.queue = append(store.queue, key)
	}
	return nil
}
func (store *fakeStore) ClaimDelivery(_ context.Context, _ time.Time, _ string, _ time.Duration) (Delivery, bool, error) {
	if len(store.queue) == 0 {
		return Delivery{}, false, nil
	}
	key := store.queue[0]
	store.queue = store.queue[1:]
	return store.deliveries[key], true, nil
}
func (store *fakeStore) MarkSent(_ context.Context, id, _ string, _ time.Time) error {
	store.sent = append(store.sent, id)
	return nil
}
func (store *fakeStore) MarkFailed(_ context.Context, id, _ string, _ time.Time, _ time.Duration, _ error) error {
	store.failed = append(store.failed, id)
	return nil
}

type fakeMailer struct {
	messages []Message
	failTo   string
}

func (mailer *fakeMailer) Send(_ context.Context, message Message) error {
	mailer.messages = append(mailer.messages, message)
	if message.To == mailer.failTo {
		return errors.New("SMTP unavailable")
	}
	return nil
}

func TestRunOnceSendsPersonalAndTeamSummaryAfterEight(t *testing.T) {
	store := &fakeStore{
		personal: []PersonalCandidate{{UserID: "1", DisplayName: "成员", Email: "member@example.com", Content: "完成 A"}},
		teams:    []TeamCandidate{{TeamID: "team-1", TeamName: "平台组", LeaderUserID: "2", LeaderName: "TL", LeaderEmail: "tl@example.com", Members: []TeamMember{{DisplayName: "成员", Content: "完成 A"}, {DisplayName: "缺日报成员"}}}},
	}
	mailer := &fakeMailer{}
	service, err := NewService(store, mailer, Config{Enabled: true, Timezone: "Asia/Shanghai", TimeOfDay: "08:00", WorkerID: "worker"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 6, 0, 1, 0, 0, time.UTC) // 08:01 Asia/Shanghai
	if err := service.RunOnce(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if len(mailer.messages) != 2 || len(store.sent) != 2 {
		t.Fatalf("messages=%d sent=%d, want 2", len(mailer.messages), len(store.sent))
	}
	if mailer.messages[0].To != "member@example.com" || mailer.messages[1].To != "tl@example.com" {
		t.Fatalf("recipients = %#v", mailer.messages)
	}
	if got := mailer.messages[1].HTMLBody; !containsAll(got, "平台组", "成员", "缺少日报") {
		t.Fatalf("team HTML = %q", got)
	}
	if err := service.RunOnce(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if len(mailer.messages) != 2 {
		t.Fatalf("idempotent rerun sent %d messages", len(mailer.messages))
	}
}

func TestRunOnceBeforeEightDoesNothing(t *testing.T) {
	store := &fakeStore{personal: []PersonalCandidate{{UserID: "1", Email: "member@example.com", Content: "report"}}}
	mailer := &fakeMailer{}
	service, err := NewService(store, mailer, Config{Enabled: true, Timezone: "Asia/Shanghai", TimeOfDay: "08:00", WorkerID: "worker"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RunOnce(context.Background(), time.Date(2026, 8, 5, 23, 59, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if len(store.deliveries) != 0 || len(mailer.messages) != 0 {
		t.Fatal("delivery was prepared before 08:00 Asia/Shanghai")
	}
}

func TestRunOnceIsolatesSendFailure(t *testing.T) {
	store := &fakeStore{personal: []PersonalCandidate{
		{UserID: "1", DisplayName: "A", Email: "fail@example.com", Content: "A"},
		{UserID: "2", DisplayName: "B", Email: "ok@example.com", Content: "B"},
	}}
	mailer := &fakeMailer{failTo: "fail@example.com"}
	service, _ := NewService(store, mailer, Config{Enabled: true, Timezone: "Asia/Shanghai", TimeOfDay: "08:00", WorkerID: "worker"})
	if err := service.RunOnce(context.Background(), time.Date(2026, 8, 6, 1, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if len(store.failed) != 1 || len(store.sent) != 1 {
		t.Fatalf("failed=%d sent=%d", len(store.failed), len(store.sent))
	}
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
