package poppit_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/its-the-vibe/CopilotBurn/pkg/poppit"
	"github.com/redis/go-redis/v9"
)

func TestSubmitBatch(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer s.Close()

	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer rdb.Close()

	ctx := context.Background()
	notification := poppit.Notification{
		Repo:     "its-the-vibe/CopilotBurn",
		Branch:   "main",
		Type:     "copilot-usage",
		Dir:      "/tmp",
		Commands: []string{"gh vibe usage --year 2026 --month 9 --day 1"},
		Metadata: map[string]interface{}{"source": "copilotburn"},
	}

	err = poppit.SubmitBatch(ctx, rdb, "test:notifications", notification)
	if err != nil {
		t.Fatalf("SubmitBatch failed: %v", err)
	}

	val, err := rdb.LPop(ctx, "test:notifications").Result()
	if err != nil {
		t.Fatalf("failed to pop notification: %v", err)
	}

	var popped poppit.Notification
	if err := json.Unmarshal([]byte(val), &popped); err != nil {
		t.Fatalf("failed to unmarshal popped notification: %v", err)
	}

	if popped.Repo != notification.Repo || len(popped.Commands) != 1 || popped.Commands[0] != notification.Commands[0] {
		t.Errorf("popped notification does not match; got %+v, want %+v", popped, notification)
	}
}

func TestSubmitCommands(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer s.Close()

	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer rdb.Close()

	ctx := context.Background()

	// Empty commands should do nothing
	err = poppit.SubmitCommands(ctx, rdb, "", nil, nil)
	if err != nil {
		t.Fatalf("SubmitCommands with empty commands failed: %v", err)
	}

	// Submit non-empty commands with default list name
	cmds := []string{
		"gh vibe usage --year 2026 --month 9 --day 1",
		"gh vibe usage --year 2026 --month 9 --day 2",
	}
	err = poppit.SubmitCommands(ctx, rdb, "", cmds, nil)
	if err != nil {
		t.Fatalf("SubmitCommands failed: %v", err)
	}

	val, err := rdb.LPop(ctx, poppit.DefaultNotificationListName).Result()
	if err != nil {
		t.Fatalf("failed to pop notification from default list: %v", err)
	}

	var popped poppit.Notification
	if err := json.Unmarshal([]byte(val), &popped); err != nil {
		t.Fatalf("failed to unmarshal popped notification: %v", err)
	}

	if len(popped.Commands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(popped.Commands))
	}
	if popped.Commands[0] != cmds[0] || popped.Commands[1] != cmds[1] {
		t.Errorf("commands mismatch: got %v, want %v", popped.Commands, cmds)
	}
	if popped.Metadata["source"] != "copilotburn" {
		t.Errorf("expected metadata source copilotburn, got %v", popped.Metadata["source"])
	}
}
