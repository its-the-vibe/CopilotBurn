package copilotburn_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/its-the-vibe/CopilotBurn/pkg/copilotburn"
	"github.com/its-the-vibe/CopilotBurn/pkg/poppit"
	"github.com/redis/go-redis/v9"
)

func TestEndToEndPoppitPipeline(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to run miniredis: %v", err)
	}
	defer s.Close()

	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer rdb.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	poppitList := "poppit:notifications"
	poppitChannel := "poppit:command-output"
	keyPrefix := "copilot-burn:"
	ttlDays := 90

	// 1. Start output listener
	err = copilotburn.StartOutputListener(ctx, rdb, poppitChannel, keyPrefix, ttlDays)
	if err != nil {
		t.Fatalf("StartOutputListener failed: %v", err)
	}

	// 2. Fetch missing data on startup for a 3-day month up to day 3
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	cmds, err := copilotburn.FetchMissingDailyData(ctx, rdb, poppitList, now, keyPrefix)
	if err != nil {
		t.Fatalf("FetchMissingDailyData failed: %v", err)
	}

	if len(cmds) != 3 {
		t.Fatalf("expected 3 commands submitted, got %d", len(cmds))
	}

	// 3. Poppit worker simulation: pop notification from Redis list
	rawNotification, err := rdb.LPop(ctx, poppitList).Result()
	if err != nil {
		t.Fatalf("failed to pop notification from Redis list: %v", err)
	}

	var notification poppit.Notification
	if err := json.Unmarshal([]byte(rawNotification), &notification); err != nil {
		t.Fatalf("failed to parse notification: %v", err)
	}

	if len(notification.Commands) != 3 {
		t.Fatalf("expected 3 commands in notification, got %d", len(notification.Commands))
	}

	// 4. Poppit executes commands and publishes outputs to poppit:command-output
	for day := 1; day <= 3; day++ {
		var usagePayload string
		if day == 2 {
			// Day 2 has no usage
			usagePayload = fmt.Sprintf(`{
  "timePeriod": {
    "year": 2026,
    "month": 9,
    "day": %d
  },
  "user": "test-user",
  "usageItems": []
}`, day)
		} else {
			// Day 1 and 3 have usage
			usagePayload = fmt.Sprintf(`{
  "timePeriod": {
    "year": 2026,
    "month": 9,
    "day": %d
  },
  "user": "test-user",
  "usageItems": [
    {
      "product": "Copilot",
      "sku": "Copilot AI Credits",
      "model": "Claude Haiku 4.5",
      "unitType": "ai-credits",
      "pricePerUnit": 0.01,
      "grossQuantity": 1.0,
      "grossAmount": 0.01,
      "discountQuantity": 1.0,
      "discountAmount": 0.01,
      "netQuantity": 0,
      "netAmount": 0
    }
  ]
}`, day)
		}

		outMsg := poppit.CommandOutput{
			Metadata:   notification.Metadata,
			Type:       notification.Type,
			Command:    fmt.Sprintf("gh vibe usage --year 2026 --month 9 --day %d", day),
			Output:     usagePayload,
			StatusCode: 0,
		}
		rawMsg, _ := json.Marshal(outMsg)
		rdb.Publish(ctx, poppitChannel, rawMsg)
	}

	// 5. Verify all 3 days are stored in Redis
	for day := 1; day <= 3; day++ {
		expectedKey := fmt.Sprintf("copilot-burn:2026-09-%02d", day)
		var val string
		for attempt := 0; attempt < 20; attempt++ {
			val, err = rdb.Get(ctx, expectedKey).Result()
			if err == nil && len(val) > 0 {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}

		if val == "" {
			t.Fatalf("expected key %s to be populated in Redis", expectedKey)
		}

		ttl := s.TTL(expectedKey)
		if ttl <= 0 {
			t.Errorf("expected positive TTL for %s, got %v", expectedKey, ttl)
		}
	}

	// 6. Running fetch again should report 0 missing days
	cmdsAfter, err := copilotburn.FetchMissingDailyData(ctx, rdb, poppitList, now, keyPrefix)
	if err != nil {
		t.Fatalf("FetchMissingDailyData second run failed: %v", err)
	}
	if len(cmdsAfter) != 0 {
		t.Fatalf("expected 0 commands on second run, got %d", len(cmdsAfter))
	}

	// 7. Verify GetMonthlyUsage accurately reflects the pipeline output
	summary, err := copilotburn.GetMonthlyUsage(ctx, rdb, keyPrefix, now, now, 1500)
	if err != nil {
		t.Fatalf("GetMonthlyUsage failed: %v", err)
	}

	if summary.MonthlyTotal != 2.0 {
		t.Errorf("expected monthly total 2.0, got %f", summary.MonthlyTotal)
	}
	if summary.TodayUsage != 1.0 {
		t.Errorf("expected today usage 1.0, got %f", summary.TodayUsage)
	}
	if summary.RemainingQuota != 1498.0 {
		t.Errorf("expected remaining quota 1498.0, got %f", summary.RemainingQuota)
	}
}
