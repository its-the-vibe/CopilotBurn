package copilotburn_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
}

func TestEndToEndRefreshAndSSENotification(t *testing.T) {
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
	quota := 1500.0
	nowFixed := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

	broadcaster := copilotburn.NewSSEBroadcaster()

	// 1. Start output listener with broadcaster
	err = copilotburn.StartOutputListenerWithBroadcaster(ctx, rdb, poppitChannel, keyPrefix, ttlDays, quota, broadcaster, func() time.Time { return nowFixed })
	if err != nil {
		t.Fatalf("StartOutputListenerWithBroadcaster failed: %v", err)
	}

	// 2. Connect an HTTP SSE client
	req := httptest.NewRequest("GET", "/api/events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	go broadcaster.ServeHTTP(rec, req)
	time.Sleep(50 * time.Millisecond)

	// 3. Seed day 1 in Redis
	day1JSON := `{"timePeriod":{"year":2026,"month":9,"day":1},"usageItems":[{"grossQuantity":5.0}]}`
	rdb.Set(ctx, "copilot-burn:2026-09-01", day1JSON, 0)

	// 4. Trigger refresh via HTTP POST /api/refresh
	copilotburn.ResetRefreshState()
	refreshHandler := copilotburn.HandleAPIRefresh(rdb, keyPrefix, poppitList, quota, broadcaster, func() time.Time { return nowFixed })

	postReq := httptest.NewRequest("POST", "/api/refresh", nil)
	postRec := httptest.NewRecorder()
	refreshHandler.ServeHTTP(postRec, postReq)

	if postRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from refresh handler, got %d", postRec.Code)
	}

	// 5. Verify today (day 3) and yesterday (day 2) were submitted to Poppit list
	rawNotification, err := rdb.LPop(ctx, poppitList).Result()
	if err != nil {
		t.Fatalf("failed to pop notification from Redis list: %v", err)
	}

	var notification poppit.Notification
	json.Unmarshal([]byte(rawNotification), &notification)
	if len(notification.Commands) != 2 {
		t.Fatalf("expected 2 commands in notification, got %d", len(notification.Commands))
	}

	// 6. Simulate Poppit publishing results for day 2 and day 3
	for day := 2; day <= 3; day++ {
		usagePayload := fmt.Sprintf(`{"timePeriod":{"year":2026,"month":9,"day":%d},"usageItems":[{"grossQuantity":%f}]}`, day, float64(day*10))
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

	// 7. Poll for SSE stream output to contain updated summary broadcast
	var body string
	for attempt := 0; attempt < 20; attempt++ {
		body = rec.Body.String()
		if strings.Contains(body, `"total_credits":55`) { // 5 (day 1) + 20 (day 2) + 30 (day 3) = 55
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !strings.Contains(body, `"total_credits":55`) {
		t.Errorf("expected SSE body to contain updated total_credits 55, got body: %q", body)
	}
}
