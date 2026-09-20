package copilotburn_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/its-the-vibe/CopilotBurn/pkg/copilotburn"
	"github.com/redis/go-redis/v9"
)

func TestExtractDayCredits_ValidUsage(t *testing.T) {
	jsonPayload := `{
		"timePeriod": {"year": 2026, "month": 9, "day": 16},
		"user": "test-user",
		"usageItems": [
			{
				"product": "Copilot",
				"sku": "Copilot AI Credits",
				"model": "Claude Haiku 4.5",
				"unitType": "ai-credits",
				"pricePerUnit": 0.01,
				"grossQuantity": 3.7524,
				"grossAmount": 0.037524,
				"discountQuantity": 3.7524,
				"discountAmount": 0.037524,
				"netQuantity": 0,
				"netAmount": 0
			},
			{
				"product": "Copilot",
				"sku": "Copilot AI Credits",
				"model": "GPT-4o",
				"unitType": "ai-credits",
				"pricePerUnit": 0.01,
				"grossQuantity": 6.2476,
				"grossAmount": 0.062476,
				"discountQuantity": 6.2476,
				"discountAmount": 0.062476,
				"netQuantity": 0,
				"netAmount": 0
			}
		]
	}`

	credits, items, err := copilotburn.ExtractDayCredits(jsonPayload)
	if err != nil {
		t.Fatalf("ExtractDayCredits returned error: %v", err)
	}

	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	// 3.7524 + 6.2476 = 10.0
	if credits != 10.0 {
		t.Errorf("expected 10.0 credits, got %f", credits)
	}
}

func TestExtractDayCredits_NetQuantityFallback(t *testing.T) {
	jsonPayload := `{
		"timePeriod": {"year": 2026, "month": 9, "day": 16},
		"user": "test-user",
		"usageItems": [
			{
				"product": "Copilot",
				"sku": "Copilot AI Credits",
				"model": "Claude Haiku 4.5",
				"unitType": "ai-credits",
				"grossQuantity": 0,
				"netQuantity": 4.5
			}
		]
	}`

	credits, _, err := copilotburn.ExtractDayCredits(jsonPayload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if credits != 4.5 {
		t.Errorf("expected 4.5 credits, got %f", credits)
	}
}

func TestExtractDayCredits_EmptyAndCorrupt(t *testing.T) {
	// Empty string
	c1, items1, err1 := copilotburn.ExtractDayCredits("")
	if err1 != nil || c1 != 0 || len(items1) != 0 {
		t.Errorf("expected 0 credits and no error for empty string, got %f, err: %v", c1, err1)
	}

	// {} empty object
	c2, items2, err2 := copilotburn.ExtractDayCredits("{}")
	if err2 != nil || c2 != 0 || len(items2) != 0 {
		t.Errorf("expected 0 credits and no error for {}, got %f, err: %v", c2, err2)
	}

	// Empty usageItems array
	c3, items3, err3 := copilotburn.ExtractDayCredits(`{"timePeriod":{"year":2026,"month":9,"day":1},"usageItems":[]}`)
	if err3 != nil || c3 != 0 || len(items3) != 0 {
		t.Errorf("expected 0 credits for empty array, got %f, err: %v", c3, err3)
	}

	// Invalid JSON
	_, _, err4 := copilotburn.ExtractDayCredits(`not-valid-json`)
	if err4 == nil {
		t.Errorf("expected error for invalid JSON, got nil")
	}
}

func TestGetMonthlyUsage_CurrentMonth(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis run error: %v", err)
	}
	defer s.Close()

	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer rdb.Close()

	ctx := context.Background()
	prefix := "copilot-burn:"
	quota := 1500.0

	// Seed 3 days of data for September 2026
	// Day 1: 10.5 credits
	rdb.Set(ctx, "copilot-burn:2026-09-01", `{
		"timePeriod": {"year": 2026, "month": 9, "day": 1},
		"usageItems": [{"grossQuantity": 10.5}]
	}`, 0)

	// Day 2: 0 credits (empty items)
	rdb.Set(ctx, "copilot-burn:2026-09-02", `{
		"timePeriod": {"year": 2026, "month": 9, "day": 2},
		"usageItems": []
	}`, 0)

	// Day 3 (today): 15.25 credits
	rdb.Set(ctx, "copilot-burn:2026-09-03", `{
		"timePeriod": {"year": 2026, "month": 9, "day": 3},
		"usageItems": [{"grossQuantity": 15.25}]
	}`, 0)

	now := time.Date(2026, 9, 3, 14, 0, 0, 0, time.UTC)
	target := now

	summary, err := copilotburn.GetMonthlyUsage(ctx, rdb, prefix, target, now, quota)
	if err != nil {
		t.Fatalf("GetMonthlyUsage returned error: %v", err)
	}

	if summary.Year != 2026 || summary.Month != 9 {
		t.Errorf("expected year 2026, month 9, got %d, %d", summary.Year, summary.Month)
	}
	if summary.DaysInMonth != 30 {
		t.Errorf("expected 30 days in September, got %d", summary.DaysInMonth)
	}
	if summary.TodayDay != 3 {
		t.Errorf("expected todayDay 3, got %d", summary.TodayDay)
	}
	if summary.Quota != 1500.0 {
		t.Errorf("expected quota 1500.0, got %f", summary.Quota)
	}

	// Monthly total = 10.5 + 0 + 15.25 = 25.75
	if summary.MonthlyTotal != 25.75 {
		t.Errorf("expected monthly total 25.75, got %f", summary.MonthlyTotal)
	}

	// Today's usage = 15.25
	if summary.TodayUsage != 15.25 {
		t.Errorf("expected today's usage 15.25, got %f", summary.TodayUsage)
	}

	// Remaining quota = 1500 - 25.75 = 1474.25
	if summary.RemainingQuota != 1474.25 {
		t.Errorf("expected remaining quota 1474.25, got %f", summary.RemainingQuota)
	}

	// Percentage used = (25.75 / 1500) * 100 = 1.72%
	if summary.PercentageUsed != 1.72 {
		t.Errorf("expected percentage 1.72, got %f", summary.PercentageUsed)
	}

	// Check daily usage slice
	if len(summary.DailyUsage) != 30 {
		t.Fatalf("expected 30 daily usage items, got %d", len(summary.DailyUsage))
	}

	// Day 1
	d1 := summary.DailyUsage[0]
	if d1.Day != 1 || d1.Credits != 10.5 || *d1.CumulativeCredits != 10.5 || !d1.HasData || d1.IsToday || d1.IsFuture {
		t.Errorf("unexpected Day 1: %+v", d1)
	}

	// Day 2
	d2 := summary.DailyUsage[1]
	if d2.Day != 2 || d2.Credits != 0 || *d2.CumulativeCredits != 10.5 || !d2.HasData || d2.IsToday || d2.IsFuture {
		t.Errorf("unexpected Day 2: %+v", d2)
	}

	// Day 3 (today)
	d3 := summary.DailyUsage[2]
	if d3.Day != 3 || d3.Credits != 15.25 || *d3.CumulativeCredits != 25.75 || !d3.HasData || !d3.IsToday || d3.IsFuture {
		t.Errorf("unexpected Day 3: %+v", d3)
	}

	// Day 4 (future)
	d4 := summary.DailyUsage[3]
	if d4.Day != 4 || d4.Credits != 0 || d4.CumulativeCredits != nil || !d4.IsFuture || d4.IsToday {
		t.Errorf("unexpected Day 4: %+v", d4)
	}
}

func TestGetMonthlyUsage_PastMonth(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis run error: %v", err)
	}
	defer s.Close()

	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer rdb.Close()

	ctx := context.Background()
	prefix := "copilot-burn:"
	quota := 1000.0

	// August 2026 (31 days)
	// Seed day 31
	rdb.Set(ctx, "copilot-burn:2026-08-31", `{
		"timePeriod": {"year": 2026, "month": 8, "day": 31},
		"usageItems": [{"grossQuantity": 100.0}]
	}`, 0)

	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	target := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	summary, err := copilotburn.GetMonthlyUsage(ctx, rdb, prefix, target, now, quota)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if summary.DaysInMonth != 31 {
		t.Errorf("expected 31 days in August, got %d", summary.DaysInMonth)
	}
	if summary.TodayDay != 31 {
		t.Errorf("expected todayDay to be 31 for past month, got %d", summary.TodayDay)
	}
	if summary.MonthlyTotal != 100.0 {
		t.Errorf("expected monthly total 100.0, got %f", summary.MonthlyTotal)
	}
	if summary.TodayUsage != 0.0 {
		t.Errorf("expected todayUsage 0 for past month, got %f", summary.TodayUsage)
	}

	// In past month, no days should be marked is_future
	for _, d := range summary.DailyUsage {
		if d.IsFuture {
			t.Errorf("day %d should not be marked future in past month", d.Day)
		}
		if d.CumulativeCredits == nil {
			t.Errorf("day %d cumulative credits should not be nil", d.Day)
		}
	}
}

func TestGetMonthlyUsage_EmptyRedis(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis run error: %v", err)
	}
	defer s.Close()

	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer rdb.Close()

	ctx := context.Background()
	prefix := "copilot-burn:"
	now := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)

	summary, err := copilotburn.GetMonthlyUsage(ctx, rdb, prefix, now, now, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if summary.Quota != 1500.0 {
		t.Errorf("expected default quota 1500.0, got %f", summary.Quota)
	}
	if summary.MonthlyTotal != 0 {
		t.Errorf("expected 0 monthly total, got %f", summary.MonthlyTotal)
	}
	if summary.TodayUsage != 0 {
		t.Errorf("expected 0 today usage, got %f", summary.TodayUsage)
	}
	if summary.RemainingQuota != 1500.0 {
		t.Errorf("expected remaining quota 1500.0, got %f", summary.RemainingQuota)
	}
}
