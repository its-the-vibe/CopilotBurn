package copilotburn_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/its-the-vibe/CopilotBurn/pkg/copilotburn"
	"github.com/redis/go-redis/v9"
)

func TestGetMonthlyUsageSummary(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer s.Close()

	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer rdb.Close()

	ctx := context.Background()
	keyPrefix := "copilot-burn:"
	now := time.Date(2026, 9, 3, 14, 0, 0, 0, time.UTC)

	// Seed Day 1: 10.5 credits across 2 items
	day1JSON := `{
		"timePeriod": {"year": 2026, "month": 9, "day": 1},
		"user": "test-user",
		"usageItems": [
			{"grossQuantity": 7.5, "unitType": "ai-credits"},
			{"grossQuantity": 3.0, "unitType": "ai-credits"}
		]
	}`
	rdb.Set(ctx, "copilot-burn:2026-09-01", day1JSON, 0)

	// Day 2 is missing / empty (0 credits)

	// Seed Day 3 (today): 5.0 credits
	day3JSON := `{
		"timePeriod": {"year": 2026, "month": 9, "day": 3},
		"user": "test-user",
		"usageItems": [
			{"grossQuantity": 5.0, "unitType": "ai-credits"}
		]
	}`
	rdb.Set(ctx, "copilot-burn:2026-09-03", day3JSON, 0)

	summary, err := copilotburn.GetMonthlyUsageSummary(ctx, rdb, now, keyPrefix, 1500)
	if err != nil {
		t.Fatalf("unexpected error getting monthly summary: %v", err)
	}

	if summary.Quota != 1500 {
		t.Errorf("expected quota 1500, got %f", summary.Quota)
	}
	if summary.CurrentDate != "2026-09-03" {
		t.Errorf("expected currentDate '2026-09-03', got %q", summary.CurrentDate)
	}
	if summary.TotalCredits != 15.5 {
		t.Errorf("expected totalCredits 15.5, got %f", summary.TotalCredits)
	}
	if summary.TodayCredits != 5.0 {
		t.Errorf("expected todayCredits 5.0, got %f", summary.TodayCredits)
	}
	if len(summary.Daily) != 3 {
		t.Fatalf("expected 3 daily records, got %d", len(summary.Daily))
	}

	// Day 1 check
	if summary.Daily[0].Day != 1 || summary.Daily[0].DailyCredits != 10.5 || summary.Daily[0].CumulativeCredits != 10.5 {
		t.Errorf("day 1 incorrect: %+v", summary.Daily[0])
	}
	// Day 2 check (0 credits)
	if summary.Daily[1].Day != 2 || summary.Daily[1].DailyCredits != 0 || summary.Daily[1].CumulativeCredits != 10.5 {
		t.Errorf("day 2 incorrect: %+v", summary.Daily[1])
	}
	// Day 3 check
	if summary.Daily[2].Day != 3 || summary.Daily[2].DailyCredits != 5.0 || summary.Daily[2].CumulativeCredits != 15.5 {
		t.Errorf("day 3 incorrect: %+v", summary.Daily[2])
	}
}

func TestHandleAPIUsage(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer s.Close()

	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer rdb.Close()

	ctx := context.Background()
	keyPrefix := "copilot-burn:"
	nowFixed := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)

	// Seed Day 1 and Day 2
	rdb.Set(ctx, "copilot-burn:2026-09-01", `{"timePeriod":{"year":2026,"month":9,"day":1},"usageItems":[{"grossQuantity":20.0}]}`, 0)
	rdb.Set(ctx, "copilot-burn:2026-09-02", `{"timePeriod":{"year":2026,"month":9,"day":2},"usageItems":[{"grossQuantity":15.0}]}`, 0)

	handler := copilotburn.HandleAPIUsage(rdb, keyPrefix, 1500, func() time.Time { return nowFixed })

	// Test GET request
	req := httptest.NewRequest(http.MethodGet, "/api/usage", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status OK (200), got %d", rec.Code)
	}

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got %q", contentType)
	}

	var summary copilotburn.UsageSummary
	if err := json.NewDecoder(rec.Body).Decode(&summary); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}

	if summary.Quota != 1500 {
		t.Errorf("expected quota 1500, got %f", summary.Quota)
	}
	if summary.TotalCredits != 35.0 {
		t.Errorf("expected totalCredits 35.0, got %f", summary.TotalCredits)
	}
	if summary.TodayCredits != 15.0 {
		t.Errorf("expected todayCredits 15.0, got %f", summary.TodayCredits)
	}

	// Test non-GET method (POST)
	postReq := httptest.NewRequest(http.MethodPost, "/api/usage", nil)
	postRec := httptest.NewRecorder()
	handler.ServeHTTP(postRec, postReq)

	if postRec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status Method Not Allowed (405), got %d", postRec.Code)
	}
}

func TestWebHandler(t *testing.T) {
	handler := copilotburn.WebHandler()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status OK (200) for web handler, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "Ideal Burndown Rate") {
		t.Errorf("expected HTML body to contain 'Ideal Burndown Rate', but it did not")
	}
	if !strings.Contains(body, "#10b981") {
		t.Errorf("expected HTML body to contain '#10b981', but it did not")
	}
}

func TestHandleAPIRefresh(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer s.Close()

	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer rdb.Close()

	keyPrefix := "copilot-burn:"
	listName := "poppit:notifications"
	nowFixed := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	broadcaster := copilotburn.NewSSEBroadcaster()

	handler := copilotburn.HandleAPIRefresh(rdb, keyPrefix, listName, 1500, broadcaster, func() time.Time { return nowFixed })

	// Reset state
	copilotburn.ResetRefreshState()

	// 1. Non-POST method (GET)
	reqGet := httptest.NewRequest(http.MethodGet, "/api/refresh", nil)
	recGet := httptest.NewRecorder()
	handler.ServeHTTP(recGet, reqGet)

	if recGet.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 Method Not Allowed, got %d", recGet.Code)
	}

	// 2. Valid POST method
	reqPost := httptest.NewRequest(http.MethodPost, "/api/refresh", nil)
	recPost := httptest.NewRecorder()
	handler.ServeHTTP(recPost, reqPost)

	if recPost.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d (body: %s)", recPost.Code, recPost.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(recPost.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["status"] != "ok" {
		t.Errorf("expected status 'ok', got %v", resp["status"])
	}

	// 3. Concurrent request while refresh in progress
	reqPost2 := httptest.NewRequest(http.MethodPost, "/api/refresh", nil)
	recPost2 := httptest.NewRecorder()
	handler.ServeHTTP(recPost2, reqPost2)

	if recPost2.Code != http.StatusConflict {
		t.Errorf("expected 409 Conflict for concurrent refresh, got %d", recPost2.Code)
	}

	// Clean up state
	copilotburn.ResetRefreshState()
}

func TestSSEBroadcaster(t *testing.T) {
	broadcaster := copilotburn.NewSSEBroadcaster()

	if broadcaster.ClientCount() != 0 {
		t.Errorf("expected 0 clients initially, got %d", broadcaster.ClientCount())
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		broadcaster.ServeHTTP(rec, req)
		close(done)
	}()

	// Give time for client registration
	time.Sleep(50 * time.Millisecond)

	if broadcaster.ClientCount() != 1 {
		t.Errorf("expected 1 active client, got %d", broadcaster.ClientCount())
	}

	// Broadcast sample summary
	summary := &copilotburn.UsageSummary{
		Quota:        1500,
		CurrentDate:  "2026-09-03",
		TotalCredits: 25.0,
		TodayCredits: 10.0,
	}

	broadcaster.Broadcast(summary)

	// Cancel context to close stream
	cancel()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for ServeHTTP to finish")
	}

	if broadcaster.ClientCount() != 0 {
		t.Errorf("expected 0 clients after disconnection, got %d", broadcaster.ClientCount())
	}

	body := rec.Body.String()
	if !strings.Contains(body, ": connected") {
		t.Errorf("expected SSE body to contain ': connected', got %q", body)
	}
	if !strings.Contains(body, `"total_credits":25`) {
		t.Errorf("expected SSE body to contain broadcasted data, got %q", body)
	}
}
