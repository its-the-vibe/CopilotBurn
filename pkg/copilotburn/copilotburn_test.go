package copilotburn_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/its-the-vibe/CopilotBurn/pkg/copilotburn"
	"github.com/its-the-vibe/CopilotBurn/pkg/poppit"
	"github.com/redis/go-redis/v9"
)

type MockExecutor struct{}

func (m *MockExecutor) Execute(ctx context.Context, cmd poppit.Command) poppit.CommandResult {
	return poppit.CommandResult{
		Command: cmd,
		Stdout:  []byte("OK"),
	}
}

func TestFormatRedisKey(t *testing.T) {
	tests := []struct {
		prefix   string
		year     int
		month    int
		day      int
		expected string
	}{
		{"copilot-burn:", 2026, 9, 18, "copilot-burn:2026-09-18"},
		{"copilot-burn", 2026, 9, 18, "copilot-burn:2026-09-18"},
		{"", 2026, 9, 18, "2026-09-18"},
	}

	for _, tt := range tests {
		got := copilotburn.FormatRedisKey(tt.prefix, tt.year, tt.month, tt.day)
		if got != tt.expected {
			t.Errorf("FormatRedisKey(%q, %d, %d, %d) = %q; want %q", tt.prefix, tt.year, tt.month, tt.day, got, tt.expected)
		}
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

	// Seed existing data for days 1 and 2
	rdb.Set(ctx, "copilot-burn:2026-09-01", "{}", 0)
	rdb.Set(ctx, "copilot-burn:2026-09-02", "{}", 0)

	engine := poppit.NewEngine(&MockExecutor{}, 2, 10)
	engine.Start(ctx)
	defer engine.Stop()

	// Simulate running on 2026-09-05 (5 days in current month)
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

	missingCmds, err := copilotburn.FetchMissingDailyData(ctx, rdb, engine, now, keyPrefix)
	if err != nil {
		t.Fatalf("unexpected error fetching missing data: %v", err)
	}

	// Should generate commands for days 3, 4, 5 (3 commands)
	if len(missingCmds) != 3 {
		t.Fatalf("expected 3 missing commands, got %d", len(missingCmds))
	}

	expectedTargets := map[string]bool{
		"2026-09-03": true,
		"2026-09-04": true,
		"2026-09-05": true,
	}

	for _, cmd := range missingCmds {
		if !expectedTargets[cmd.Target] {
			t.Errorf("unexpected target in command: %s", cmd.Target)
		}
		if cmd.Name != "gh" {
			t.Errorf("expected cmd Name 'gh', got %q", cmd.Name)
		}
	}
}

func TestProcessCommandResult_WithUsage(t *testing.T) {
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

	res := poppit.CommandResult{
		Command: poppit.Command{
			ID:     "cmd-1",
			Target: "2026-09-16",
		},
		Stdout: []byte(jsonOutput),
	}

	err = copilotburn.ProcessCommandResult(ctx, rdb, res, keyPrefix, 90)
	if err != nil {
		t.Fatalf("ProcessCommandResult failed: %v", err)
	}

	expectedKey := "copilot-burn:2026-09-16"
	val, err := rdb.Get(ctx, expectedKey).Result()
	if err != nil {
		t.Fatalf("key %s not found in redis: %v", expectedKey, err)
	}

	if val != jsonOutput {
		t.Errorf("redis value = %q; want %q", val, jsonOutput)
	}

	ttl := s.TTL(expectedKey)
	if ttl <= 0 {
		t.Errorf("expected positive TTL for key %s, got %v", expectedKey, ttl)
	}
}

func TestProcessCommandResult_NoUsage(t *testing.T) {
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

	res := poppit.CommandResult{
		Command: poppit.Command{
			ID:     "cmd-2",
			Target: "2026-09-18",
		},
		Stdout: []byte(jsonOutput),
	}

	err = copilotburn.ProcessCommandResult(ctx, rdb, res, keyPrefix, 90)
	if err != nil {
		t.Fatalf("ProcessCommandResult failed for empty usage: %v", err)
	}

	expectedKey := "copilot-burn:2026-09-18"
	val, err := rdb.Get(ctx, expectedKey).Result()
	if err != nil {
		t.Fatalf("key %s not found in redis: %v", expectedKey, err)
	}

	if val != jsonOutput {
		t.Errorf("redis value = %q; want %q", val, jsonOutput)
	}
}

func TestProcessCommandResult_Errors(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer s.Close()

	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer rdb.Close()

	ctx := context.Background()

	// Execution error
	errRes := poppit.CommandResult{
		Command: poppit.Command{ID: "cmd-err"},
		Err:     context.DeadlineExceeded,
	}
	if err := copilotburn.ProcessCommandResult(ctx, rdb, errRes, "copilot-burn:", 90); err == nil {
		t.Error("expected error for failed command result, got nil")
	}

	// Empty stdout
	emptyRes := poppit.CommandResult{
		Command: poppit.Command{ID: "cmd-empty"},
		Stdout:  []byte(""),
	}
	if err := copilotburn.ProcessCommandResult(ctx, rdb, emptyRes, "copilot-burn:", 90); err == nil {
		t.Error("expected error for empty stdout, got nil")
	}

	// Invalid JSON
	invalidJsonRes := poppit.CommandResult{
		Command: poppit.Command{ID: "cmd-invalid"},
		Stdout:  []byte("invalid json"),
	}
	if err := copilotburn.ProcessCommandResult(ctx, rdb, invalidJsonRes, "copilot-burn:", 90); err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}
