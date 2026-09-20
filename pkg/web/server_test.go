package web_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"os/exec"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/its-the-vibe/CopilotBurn/pkg/copilotburn"
	"github.com/its-the-vibe/CopilotBurn/pkg/web"
	"github.com/redis/go-redis/v9"
)

func setupTestServer(t *testing.T) (*web.Server, *miniredis.Miniredis, *redis.Client) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}

	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})
	srv := web.NewServer(rdb, "copilot-burn:", 1500.0, 8080)
	return srv, s, rdb
}

func TestDashboardHTML_Served(t *testing.T) {
	srv, s, rdb := setupTestServer(t)
	defer s.Close()
	defer rdb.Close()

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	endpoints := []string{"/", "/dashboard"}
	for _, ep := range endpoints {
		resp, err := http.Get(ts.URL + ep)
		if err != nil {
			t.Fatalf("GET %s error: %v", ep, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s status = %d; want %d", ep, resp.StatusCode, http.StatusOK)
		}

		contentType := resp.Header.Get("Content-Type")
		if !strings.Contains(contentType, "text/html") {
			t.Errorf("GET %s content-type = %s; want text/html", ep, contentType)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("failed to read response body: %v", err)
		}
		html := string(body)

		// Required acceptance criteria elements
		requiredSubstrings := []string{
			"CopilotBurn",
			"viewport",             // responsive
			"usageChart",            // cumulative chart canvas
			"monthlyTotalVal",       // monthly total display
			"quotaTotalVal",         // quota comparison
			"todayUsageVal",         // today's usage highlight
			"todayStatusBadge",      // today's distinction
			"configuredQuotaVal",    // configured quota display
			"/static/dashboard.js",  // script inclusion
			"/static/chart.umd.min.js", // charting library
			"role=\"progressbar\"", // accessibility
			"role=\"img\"",         // accessibility for chart
			"aria-label",           // accessibility labels
		}

		for _, sub := range requiredSubstrings {
			if !strings.Contains(html, sub) {
				t.Errorf("GET %s HTML missing required substring %q", ep, sub)
			}
		}
	}
}

func TestStaticAssets_Served(t *testing.T) {
	srv, s, rdb := setupTestServer(t)
	defer s.Close()
	defer rdb.Close()

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	assets := []struct {
		path        string
		contentType string
	}{
		{"/static/dashboard.css", "text/css"},
		{"/static/dashboard.js", "javascript"},
		{"/static/chart.umd.min.js", "javascript"},
	}

	for _, a := range assets {
		resp, err := http.Get(ts.URL + a.path)
		if err != nil {
			t.Fatalf("GET %s error: %v", a.path, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s status = %d; want %d", a.path, resp.StatusCode, http.StatusOK)
		}

		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, a.contentType) {
			t.Errorf("GET %s content-type = %s; want %s", a.path, ct, a.contentType)
		}

		body, _ := io.ReadAll(resp.Body)
		if len(body) == 0 {
			t.Errorf("GET %s body was empty", a.path)
		}
	}
}

func TestNotFound(t *testing.T) {
	srv, s, rdb := setupTestServer(t)
	defer s.Close()
	defer rdb.Close()

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/nonexistent-path")
	if err != nil {
		t.Fatalf("GET error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", resp.StatusCode)
	}
}

func TestHealthAPI(t *testing.T) {
	srv, s, rdb := setupTestServer(t)
	defer s.Close()
	defer rdb.Close()

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/health")
	if err != nil {
		t.Fatalf("GET /api/health error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var data map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("failed to decode json: %v", err)
	}
	if data["status"] != "ok" {
		t.Errorf("expected status ok, got %q", data["status"])
	}
}

func TestUsageAPI_CurrentData(t *testing.T) {
	srv, s, rdb := setupTestServer(t)
	defer s.Close()
	defer rdb.Close()

	ctx := context.Background()

	// Seed data for current year/month
	now := time.Now()
	year, month, today := now.Date()

	// Seed Day 1: 25 credits
	keyDay1 := copilotburn.FormatRedisKey("copilot-burn:", year, int(month), 1)
	rdb.Set(ctx, keyDay1, `{
		"timePeriod": {"year": 2026, "month": 9, "day": 1},
		"usageItems": [{"grossQuantity": 25.0}]
	}`, 0)

	// If today > 1, seed today's usage as 12.5 credits
	keyToday := copilotburn.FormatRedisKey("copilot-burn:", year, int(month), today)
	if today == 1 {
		rdb.Set(ctx, keyToday, `{
			"timePeriod": {"year": 2026, "month": 9, "day": 1},
			"usageItems": [{"grossQuantity": 12.5}]
		}`, 0)
	} else {
		rdb.Set(ctx, keyToday, `{
			"timePeriod": {"year": 2026, "month": 9, "day": 2},
			"usageItems": [{"grossQuantity": 12.5}]
		}`, 0)
	}

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/usage")
	if err != nil {
		t.Fatalf("GET /api/usage error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/usage status = %d; want %d", resp.StatusCode, http.StatusOK)
	}

	var summary copilotburn.MonthlyUsageSummary
	if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
		t.Fatalf("failed to decode usage json: %v", err)
	}

	if summary.Year != year || summary.Month != int(month) {
		t.Errorf("summary year/month = %d/%d; want %d/%d", summary.Year, summary.Month, year, int(month))
	}
	if summary.Quota != 1500.0 {
		t.Errorf("summary quota = %f; want 1500", summary.Quota)
	}
	if summary.TodayUsage != 12.5 {
		t.Errorf("summary today_usage = %f; want 12.5", summary.TodayUsage)
	}
	if summary.TodayDay != today {
		t.Errorf("summary today_day = %d; want %d", summary.TodayDay, today)
	}

	// Verify daily breakdown exists and covers the month
	if len(summary.DailyUsage) != summary.DaysInMonth {
		t.Errorf("daily_usage length = %d; want %d", len(summary.DailyUsage), summary.DaysInMonth)
	}
}

func TestUsageAPI_CustomQueryParams(t *testing.T) {
	srv, s, rdb := setupTestServer(t)
	defer s.Close()
	defer rdb.Close()

	ctx := context.Background()

	// Seed August 2026 Day 15
	rdb.Set(ctx, "copilot-burn:2026-08-15", `{
		"timePeriod": {"year": 2026, "month": 8, "day": 15},
		"usageItems": [{"grossQuantity": 42.0}]
	}`, 0)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Valid query
	resp, err := http.Get(ts.URL + "/api/usage?year=2026&month=8")
	if err != nil {
		t.Fatalf("GET error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; want 200", resp.StatusCode)
	}

	var summary copilotburn.MonthlyUsageSummary
	if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
		t.Fatalf("decode json: %v", err)
	}

	if summary.Year != 2026 || summary.Month != 8 {
		t.Errorf("year/month = %d/%d; want 2026/8", summary.Year, summary.Month)
	}
	if summary.MonthlyTotal != 42.0 {
		t.Errorf("monthly_total = %f; want 42.0", summary.MonthlyTotal)
	}

	// Invalid year
	badYearResp, err := http.Get(ts.URL + "/api/usage?year=invalid")
	if err != nil {
		t.Fatalf("GET bad year error: %v", err)
	}
	badYearResp.Body.Close()
	if badYearResp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for bad year, got %d", badYearResp.StatusCode)
	}

	// Invalid month
	badMonthResp, err := http.Get(ts.URL + "/api/usage?month=15")
	if err != nil {
		t.Fatalf("GET bad month error: %v", err)
	}
	badMonthResp.Body.Close()
	if badMonthResp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for bad month, got %d", badMonthResp.StatusCode)
	}
}

func TestFrontendJS_Components(t *testing.T) {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available, skipping frontend JS unit tests")
	}

	cmd := exec.Command(nodePath, "--test", "frontend_test.js")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("frontend JS unit tests failed: %v\nOutput:\n%s", err, string(out))
	}
}
