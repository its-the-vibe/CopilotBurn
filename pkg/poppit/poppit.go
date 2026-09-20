package poppit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/redis/go-redis/v9"
)

const (
	// DefaultNotificationListName is the default Redis list monitored by Poppit.
	DefaultNotificationListName = "poppit:notifications"

	// DefaultCommandOutputChannel is the default Redis Pub/Sub channel used by Poppit to publish command output.
	DefaultCommandOutputChannel = "poppit:command-output"
)

// Notification represents the payload sent to Poppit via a Redis list.
type Notification struct {
	Repo     string                 `json:"repo"`
	Branch   string                 `json:"branch"`
	Type     string                 `json:"type"`
	Dir      string                 `json:"dir"`
	Commands []string               `json:"commands"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// CommandOutput represents the execution output published by Poppit to Redis Pub/Sub.
type CommandOutput struct {
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
	Type       string                 `json:"type"`
	Command    string                 `json:"command"`
	Output     string                 `json:"output"`
	StdErr     string                 `json:"stderr"`
	StatusCode int                    `json:"status_code"`
}

// SubmitBatch pushes a Notification payload onto the specified Redis list using RPUSH.
func SubmitBatch(ctx context.Context, rdb redis.Cmdable, listName string, notification Notification) error {
	if listName == "" {
		listName = DefaultNotificationListName
	}
	payload, err := json.Marshal(notification)
	if err != nil {
		return fmt.Errorf("failed to marshal poppit notification: %w", err)
	}

	if err := rdb.RPush(ctx, listName, payload).Err(); err != nil {
		return fmt.Errorf("failed to push poppit notification to list %s: %w", listName, err)
	}
	return nil
}

// SubmitCommands builds a Notification and submits it to Poppit.
func SubmitCommands(ctx context.Context, rdb redis.Cmdable, listName string, commands []string, metadata map[string]interface{}) error {
	if len(commands) == 0 {
		return nil
	}
	if metadata == nil {
		metadata = map[string]interface{}{
			"source": "copilotburn",
		}
	}
	notification := Notification{
		Repo:     "its-the-vibe/CopilotBurn",
		Branch:   "main",
		Type:     "copilot-usage",
		Dir:      "/tmp",
		Commands: commands,
		Metadata: metadata,
	}
	return SubmitBatch(ctx, rdb, listName, notification)
}
