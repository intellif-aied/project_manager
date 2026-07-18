package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aidashboard/api/internal/tokenanalytics"
	"github.com/aidashboard/api/model"
)

type fakeTokenAnalyticsService struct {
	createCalls   int
	sessionTokens []string
	sessionErrors []error
}

func (f *fakeTokenAnalyticsService) CreateSummary(context.Context, tokenanalytics.Actor, tokenanalytics.Filters) (tokenanalytics.Summary, error) {
	f.createCalls++
	return tokenanalytics.Summary{QuerySnapshotToken: "refreshed-snapshot"}, nil
}

func (f *fakeTokenAnalyticsService) Trends(context.Context, tokenanalytics.Actor, tokenanalytics.Filters, string) (tokenanalytics.Trends, error) {
	return tokenanalytics.Trends{}, nil
}

func (f *fakeTokenAnalyticsService) Rankings(context.Context, tokenanalytics.Actor, tokenanalytics.Filters, string, string) (tokenanalytics.Rankings, error) {
	return tokenanalytics.Rankings{}, nil
}

func (f *fakeTokenAnalyticsService) Sessions(_ context.Context, _ tokenanalytics.Actor, _ tokenanalytics.Filters, token string, page, pageSize int) (tokenanalytics.Sessions, error) {
	f.sessionTokens = append(f.sessionTokens, token)
	if len(f.sessionErrors) > 0 {
		err := f.sessionErrors[0]
		f.sessionErrors = f.sessionErrors[1:]
		if err != nil {
			return tokenanalytics.Sessions{}, err
		}
	}
	return tokenanalytics.Sessions{QuerySnapshotToken: token, Total: 1, Page: page, PageSize: pageSize,
		Items: []tokenanalytics.SessionItem{{
			SessionID: "00000000-0000-4000-8000-000000000001", SessionRef: "session-1",
			FamilyRootSessionRef: "session-1", UserID: "303", UserName: "Tester", AgentType: "codex",
			StartedAt: time.Now().UTC().Format(time.RFC3339Nano), ActivityFrom: "2026-07-18",
			ActivityDates: []string{"2026-07-18"}, Model: "gpt-test", RangeTotalTokens: "10",
			LifetimeTotalTokens: "10", FamilyTotalTokens: "10", SelfTotalTokens: "10",
			QualityStatus: "exact",
		}},
	}, nil
}

func TestResolvePeriodAtUsesBusinessTimezone(t *testing.T) {
	now := time.Date(2026, 7, 5, 16, 30, 0, 0, time.UTC) // 2026-07-06 00:30 Asia/Shanghai.

	start, end, err := resolvePeriodAt("today", "", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if start != "2026-07-06" || end != "2026-07-06" {
		t.Fatalf("today = %s..%s, want 2026-07-06..2026-07-06", start, end)
	}

	start, end, err = resolvePeriodAt("week", "", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if start != "2026-07-06" || end != "2026-07-06" {
		t.Fatalf("week = %s..%s, want 2026-07-06..2026-07-06", start, end)
	}

	start, end, err = resolvePeriodAt("month", "", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if start != "2026-07-01" || end != "2026-07-06" {
		t.Fatalf("month = %s..%s, want 2026-07-01..2026-07-06", start, end)
	}
}

func TestListSessionTokensWithoutDateRangeDoesNotApplyDefaultDateFilter(t *testing.T) {
	from, to := legacyTokenDateRange("", "")
	if from != "1970-01-01" || to != "9999-12-31" {
		t.Fatalf("unbounded date range = %s..%s", from, to)
	}
}

func TestListSessionTokensReusesAndRefreshesSnapshot(t *testing.T) {
	analytics := &fakeTokenAnalyticsService{}
	handler := &TokenHandler{analytics: analytics}
	request := httptest.NewRequest(http.MethodGet,
		"/tokens/sessions?scope=mine&page=2&page_size=10&query_snapshot_token=first-snapshot", nil)
	request = requestWithUser(request, &model.User{ID: "303", Role: "employee"})
	response := httptest.NewRecorder()

	handler.ListSessionTokens(response, request)
	if response.Code != http.StatusOK || analytics.createCalls != 0 ||
		len(analytics.sessionTokens) != 1 || analytics.sessionTokens[0] != "first-snapshot" {
		t.Fatalf("status=%d create=%d tokens=%v body=%s", response.Code, analytics.createCalls,
			analytics.sessionTokens, response.Body.String())
	}
	var payload model.PaginatedSessionTokens
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.QuerySnapshotToken != "first-snapshot" || payload.Page != 2 || payload.Total != 1 {
		t.Fatalf("payload=%+v", payload)
	}

	analytics = &fakeTokenAnalyticsService{sessionErrors: []error{tokenanalytics.ErrSnapshotExpired, nil}}
	handler = &TokenHandler{analytics: analytics}
	request = httptest.NewRequest(http.MethodGet,
		"/tokens/sessions?scope=mine&page=2&page_size=10&query_snapshot_token=expired-snapshot", nil)
	request = requestWithUser(request, &model.User{ID: "303", Role: "employee"})
	response = httptest.NewRecorder()
	handler.ListSessionTokens(response, request)
	if response.Code != http.StatusOK || analytics.createCalls != 1 ||
		len(analytics.sessionTokens) != 2 || analytics.sessionTokens[1] != "refreshed-snapshot" {
		t.Fatalf("refresh status=%d create=%d tokens=%v body=%s", response.Code,
			analytics.createCalls, analytics.sessionTokens, response.Body.String())
	}
}
