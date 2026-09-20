package copilotburn_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/its-the-vibe/CopilotBurn/pkg/copilotburn"
	"github.com/its-the-vibe/CopilotBurn/pkg/poppit"
	"github.com/redis/go-redis/v9"
)

func TestFormatRedisKey(t *testing.T) {
	tests := []struct {
		prefix string
		year   int
		month  int
		day    int
		want   string
	}{
		{"copilot-burn:", 2026, 9, 18, "copilot-burn:2026-09-18"},
		{"copilot-burn", 2026, 9, 18, "copilot-burn:2026-09-18"},
		{"copilot-burn::", 2026, 9, 18, "copilot-burn:2026-09-18"},
		{"", 2026, 9, 5, "2026-09-05"},
		{"custom-prefix:", 2025, 12, 1, "custom-prefix:2025-12-01"},
	}

	for _, tt := range tests {
		got := copilotburn.FormatRedisKey(tt.prefix, tt.year, tt.month, tt.day)
		if got != tt.want {
			t.Errorf("FormatRedisKey(%q, %d, %d, %d) = %q; want %q", tt.prefix, tt.year, tt.month, tt.day, got, tt.want)
		}
	}
}

func TestBuildCommand(t *testing.T) {
	cmd := copilotburn.BuildCommand(2026, 9, 16)
	want := "gh vibe usage --year 2026 --month 9 --day 16"
	if cmd != want {
		t.Errorf("BuildCommand = %q; want %q", cmd, want)
	}
}

func TestIdentifyMissingDays(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer s.Close()

	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer rdb.Close()

	ctx := context.Background()
	keyPrefix := "copilot-burn:"

	// Seed existing data for days 1 and 3 of September 2026
	rdb.Set(ctx, "copilot-burn:2026-09-01", "{}", 0)
	rdb.Set(ctx, "copilot-burn:2026-09-03", "{}", 0)

	// Simulate running on 2026-09-04
	now := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)

	missing, err := copilotburn.IdentifyMissingDays(ctx, rdb, now, keyPrefix)
	if err != nil {
		t.Fatalf("unexpected error identifying missing days: %v", err)
	}

	// Should identify day 2 and day 4
	if len(missing) != 2 {
		t.Fatalf("expected 2 missing days, got %d", len(missing))
	}
	if missing[0].Day() != 2 || missing[1].Day() != 4 {
		t.Errorf("expected days 2 and 4, got %d and %d", missing[0].Day(), missing[1].Day())
	}
}

func TestFetchMissingDailyData(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer s.Close()

	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer rdb.Close()

	ctx := context.Background()
	keyPrefix := "copilot-burn:"
	listName := "poppit:notifications"

	// Seed days 1 and 2
	rdb.Set(ctx, "copilot-burn:2026-09-01", "{}", 0)
	rdb.Set(ctx, "copilot-burn:2026-09-02", "{}", 0)

	// Simulate running on 2026-09-05 (days 3, 4, 5 missing)
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

	commands, err := copilotburn.FetchMissingDailyData(ctx, rdb, listName, now, keyPrefix)
	if err != nil {
		t.Fatalf("unexpected error fetching missing daily data: %v", err)
	}

	if len(commands) != 3 {
		t.Fatalf("expected 3 commands, got %d", len(commands))
	}

	expectedCmds := []string{
		"gh vibe usage --year 2026 --month 9 --day 3",
		"gh vibe usage --year 2026 --month 9 --day 4",
		"gh vibe usage --year 2026 --month 9 --day 5",
	}
	for i, want := range expectedCmds {
		if commands[i] != want {
			t.Errorf("command[%d] = %q; want %q", i, commands[i], want)
		}
	}

	// Verify notification in Redis
	val, err := rdb.LPop(ctx, listName).Result()
	if err != nil {
		t.Fatalf("expected notification in Redis list %s: %v", listName, err)
	}

	var notification poppit.Notification
	if err := json.Unmarshal([]byte(val), &notification); err != nil {
		t.Fatalf("failed to unmarshal notification: %v", err)
	}

	if len(notification.Commands) != 3 {
		t.Fatalf("expected 3 commands in notification, got %d", len(notification.Commands))
	}
	if notification.Type != "copilot-usage" {
		t.Errorf("expected type 'copilot-usage', got %q", notification.Type)
	}
}

func TestFetchMissingDailyData_NoneMissing(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer s.Close()

	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer rdb.Close()

	ctx := context.Background()
	keyPrefix := "copilot-burn:"
	listName := "poppit:notifications"

	// Seed all days up to 2
	rdb.Set(ctx, "copilot-burn:2026-09-01", "{}", 0)
	rdb.Set(ctx, "copilot-burn:2026-09-02", "{}", 0)

	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)

	commands, err := copilotburn.FetchMissingDailyData(ctx, rdb, listName, now, keyPrefix)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(commands) != 0 {
		t.Fatalf("expected 0 commands, got %d", len(commands))
	}

	// Nothing pushed to Poppit
	llen, err := rdb.LLen(ctx, listName).Result()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if llen != 0 {
		t.Errorf("expected empty list, got length %d", llen)
	}
}

func TestProcessCommandOutput_WithUsage(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer s.Close()

	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer rdb.Close()

	ctx := context.Background()
	keyPrefix := "copilot-burn:"

	jsonOutput := `{
  "timePeriod": {
    "year": 2026,
    "month": 9,
    "day": 16
  },
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
    }
  ]
}`

	cmdOutput := poppit.CommandOutput{
		Type:       "copilot-usage",
		Command:    "gh vibe usage --year 2026 --month 9 --day 16",
		Output:     jsonOutput,
		StatusCode: 0,
	}

	err = copilotburn.ProcessCommandOutput(ctx, rdb, cmdOutput, keyPrefix, 90)
	if err != nil {
		t.Fatalf("ProcessCommandOutput failed: %v", err)
	}

	expectedKey := "copilot-burn:2026-09-16"
	val, err := rdb.Get(ctx, expectedKey).Result()
	if err != nil {
		t.Fatalf("expected key %s to exist in Redis: %v", expectedKey, err)
	}
	if val != jsonOutput {
		t.Errorf("stored value = %q; want %q", val, jsonOutput)
	}

	ttl := s.TTL(expectedKey)
	if ttl <= 0 {
		t.Errorf("expected positive TTL for key %s, got %v", expectedKey, ttl)
	}
}

func TestProcessCommandOutput_NoUsage(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer s.Close()

	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer rdb.Close()

	ctx := context.Background()
	keyPrefix := "copilot-burn:"

	jsonOutput := `{
  "timePeriod": {
    "year": 2026,
    "month": 9,
    "day": 18
  },
  "user": "test-user",
  "usageItems": []
}`

	cmdOutput := poppit.CommandOutput{
		Type:       "copilot-usage",
		Command:    "gh vibe usage --year 2026 --month 9 --day 18",
		Output:     jsonOutput,
		StatusCode: 0,
	}

	err = copilotburn.ProcessCommandOutput(ctx, rdb, cmdOutput, keyPrefix, 90)
	if err != nil {
		t.Fatalf("ProcessCommandOutput failed with empty usage: %v", err)
	}

	expectedKey := "copilot-burn:2026-09-18"
	val, err := rdb.Get(ctx, expectedKey).Result()
	if err != nil {
		t.Fatalf("expected key %s in Redis: %v", expectedKey, err)
	}
	if val != jsonOutput {
		t.Errorf("stored value = %q; want %q", val, jsonOutput)
	}
}

func TestProcessCommandOutput_ErrorsAndFilters(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer s.Close()

	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer rdb.Close()

	ctx := context.Background()

	// 1. Non-zero status code
	err = copilotburn.ProcessCommandOutput(ctx, rdb, poppit.CommandOutput{
		StatusCode: 1,
		Command:    "gh vibe usage",
		StdErr:     "authentication required",
	}, "copilot-burn:", 90)
	if err == nil {
		t.Error("expected error for non-zero status code, got nil")
	}

	// 2. Empty output
	err = copilotburn.ProcessCommandOutput(ctx, rdb, poppit.CommandOutput{
		StatusCode: 0,
		Output:     "   ",
	}, "copilot-burn:", 90)
	if err == nil {
		t.Error("expected error for empty output, got nil")
	}

	// 3. Invalid JSON
	err = copilotburn.ProcessCommandOutput(ctx, rdb, poppit.CommandOutput{
		StatusCode: 0,
		Output:     "not json",
	}, "copilot-burn:", 90)
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}

	// 4. Missing/zero timePeriod
	err = copilotburn.ProcessCommandOutput(ctx, rdb, poppit.CommandOutput{
		StatusCode: 0,
		Output:     `{"timePeriod": {"year": 0, "month": 9, "day": 1}}`,
	}, "copilot-burn:", 90)
	if err == nil {
		t.Error("expected error for zero year in timePeriod, got nil")
	}

	// 5. Different Type (e.g. git-webhook) should be ignored without error
	err = copilotburn.ProcessCommandOutput(ctx, rdb, poppit.CommandOutput{
		Type:       "git-webhook",
		StatusCode: 0,
		Output:     "some output",
	}, "copilot-burn:", 90)
	if err != nil {
		t.Errorf("expected non-copilot type to be ignored safely, got err: %v", err)
	}

	// 6. Non-copilot metadata source should be ignored without error
	err = copilotburn.ProcessCommandOutput(ctx, rdb, poppit.CommandOutput{
		Metadata:   map[string]interface{}{"source": "other-service"},
		StatusCode: 0,
		Output:     "some output",
	}, "copilot-burn:", 90)
	if err != nil {
		t.Errorf("expected non-copilot source to be ignored safely, got err: %v", err)
	}
}

func TestStartOutputListener(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer s.Close()

	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer rdb.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	channel := "test:command-output"
	keyPrefix := "copilot-burn:"

	err = copilotburn.StartOutputListener(ctx, rdb, channel, keyPrefix, 90)
	if err != nil {
		t.Fatalf("StartOutputListener failed: %v", err)
	}

	jsonOutput := `{
  "timePeriod": {
    "year": 2026,
    "month": 9,
    "day": 20
  },
  "user": "test-user",
  "usageItems": []
}`

	cmdOutput := poppit.CommandOutput{
		Type:       "copilot-usage",
		Command:    "gh vibe usage --year 2026 --month 9 --day 20",
		Output:     jsonOutput,
		StatusCode: 0,
		Metadata:   map[string]interface{}{"source": "copilotburn"},
	}
	payload, err := json.Marshal(cmdOutput)
	if err != nil {
		t.Fatalf("failed to marshal cmdOutput: %v", err)
	}

	err = rdb.Publish(ctx, channel, payload).Err()
	if err != nil {
		t.Fatalf("failed to publish to channel: %v", err)
	}

	// Poll Redis for the key to be written
	expectedKey := "copilot-burn:2026-09-20"
	var val string
	for i := 0; i < 20; i++ {
		val, err = rdb.Get(ctx, expectedKey).Result()
		if err == nil && val == jsonOutput {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if val != jsonOutput {
		t.Errorf("listener did not store output in Redis in time; val = %q, want %q", val, jsonOutput)
	}
}
