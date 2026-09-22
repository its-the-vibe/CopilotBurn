package copilotburn

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/its-the-vibe/CopilotBurn/pkg/poppit"
	"github.com/redis/go-redis/v9"
)

const (
	// DefaultKeyPrefix is the default Redis key prefix for daily usage data.
	DefaultKeyPrefix = "copilot-burn:"

	// DefaultTTLDays is the default time-to-live for daily usage records in days.
	DefaultTTLDays = 90

	// DefaultAICreditQuota is the default monthly AI credit quota limit.
	DefaultAICreditQuota = 1500.0

	// DefaultServerPort is the default HTTP server port.
	DefaultServerPort = 8080
)

// TimePeriod represents the year, month, and day of a GitHub usage response.
type TimePeriod struct {
	Year  int `json:"year"`
	Month int `json:"month"`
	Day   int `json:"day"`
}

// UsageItem represents individual product usage details in a GitHub usage response.
type UsageItem struct {
	Product          string  `json:"product"`
	SKU              string  `json:"sku"`
	Model            string  `json:"model"`
	UnitType         string  `json:"unitType"`
	PricePerUnit     float64 `json:"pricePerUnit"`
	GrossQuantity    float64 `json:"grossQuantity"`
	GrossAmount      float64 `json:"grossAmount"`
	DiscountQuantity float64 `json:"discountQuantity"`
	DiscountAmount   float64 `json:"discountAmount"`
	NetQuantity      float64 `json:"netQuantity"`
	NetAmount        float64 `json:"netAmount"`
}

// UsageResponse represents the response payload from `gh vibe usage`.
type UsageResponse struct {
	TimePeriod TimePeriod  `json:"timePeriod"`
	User       string      `json:"user"`
	UsageItems []UsageItem `json:"usageItems"`
}

// FormatRedisKey returns a formatted Redis key using the prefix and date components.
// For example: "copilot-burn:2026-09-18"
func FormatRedisKey(prefix string, year, month, day int) string {
	prefix = strings.TrimRight(prefix, ":")
	if prefix != "" {
		prefix = prefix + ":"
	}
	return fmt.Sprintf("%s%04d-%02d-%02d", prefix, year, month, day)
}

// BuildCommand returns the gh vibe usage command string for a given date.
func BuildCommand(year, month, day int) string {
	return fmt.Sprintf("gh vibe usage --year %d --month %d --day %d", year, month, day)
}

// IdentifyMissingDays identifies all days from the first of the month up to and
// including today that do not have records stored in Redis.
func IdentifyMissingDays(ctx context.Context, rdb redis.Cmdable, now time.Time, keyPrefix string) ([]time.Time, error) {
	year := now.Year()
	month := now.Month()
	today := now.Day()

	var missing []time.Time
	for day := 1; day <= today; day++ {
		key := FormatRedisKey(keyPrefix, year, int(month), day)
		exists, err := rdb.Exists(ctx, key).Result()
		if err != nil {
			log.Printf("Error checking Redis key %s: %v", key, err)
			continue
		}
		if exists == 0 {
			missing = append(missing, time.Date(year, month, day, 0, 0, 0, 0, now.Location()))
		}
	}

	return missing, nil
}

// FetchMissingDailyData identifies missing daily usage data for the current month up to today,
// builds commands for the missing days, and submits the batch to Poppit via Redis.
func FetchMissingDailyData(ctx context.Context, rdb redis.Cmdable, poppitListName string, now time.Time, keyPrefix string) ([]string, error) {
	missingDays, err := IdentifyMissingDays(ctx, rdb, now, keyPrefix)
	if err != nil {
		return nil, fmt.Errorf("identifying missing days: %w", err)
	}

	if len(missingDays) == 0 {
		return nil, nil
	}

	commands := make([]string, 0, len(missingDays))
	for _, d := range missingDays {
		commands = append(commands, BuildCommand(d.Year(), int(d.Month()), d.Day()))
	}

	err = poppit.SubmitCommands(ctx, rdb, poppitListName, commands, map[string]interface{}{
		"source": "copilotburn",
	})
	if err != nil {
		return nil, fmt.Errorf("submitting batch to poppit: %w", err)
	}

	return commands, nil
}

// TriggerRefresh invalidates cached data for today and yesterday from Redis,
// identifies missing days in the current month (plus yesterday if in the previous month),
// and submits commands to Poppit to re-fetch the missing daily usage data.
func TriggerRefresh(ctx context.Context, rdb redis.Cmdable, keyPrefix string, poppitListName string, now time.Time) ([]string, error) {
	todayKey := FormatRedisKey(keyPrefix, now.Year(), int(now.Month()), now.Day())
	yesterday := now.AddDate(0, 0, -1)
	yesterdayKey := FormatRedisKey(keyPrefix, yesterday.Year(), int(yesterday.Month()), yesterday.Day())

	// Delete today and yesterday from Redis
	if err := rdb.Del(ctx, todayKey, yesterdayKey).Err(); err != nil {
		return nil, fmt.Errorf("deleting cached data for refresh: %w", err)
	}

	missingDays, err := IdentifyMissingDays(ctx, rdb, now, keyPrefix)
	if err != nil {
		return nil, fmt.Errorf("identifying missing days after cache invalidation: %w", err)
	}

	// If yesterday was in previous month, check if it's missing and add to missingDays if so
	if yesterday.Month() != now.Month() {
		exists, err := rdb.Exists(ctx, yesterdayKey).Result()
		if err == nil && exists == 0 {
			missingDays = append(missingDays, time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 0, 0, 0, 0, now.Location()))
		}
	}

	if len(missingDays) == 0 {
		return nil, nil
	}

	commands := make([]string, 0, len(missingDays))
	for _, d := range missingDays {
		commands = append(commands, BuildCommand(d.Year(), int(d.Month()), d.Day()))
	}

	err = poppit.SubmitCommands(ctx, rdb, poppitListName, commands, map[string]interface{}{
		"source": "copilotburn",
	})
	if err != nil {
		return nil, fmt.Errorf("submitting refresh commands to poppit: %w", err)
	}

	return commands, nil
}

// ProcessCommandOutput parses a command output received from Poppit and stores
// the verbatim JSON response in Redis with the appropriate key and TTL.
func ProcessCommandOutput(ctx context.Context, rdb redis.Cmdable, cmdOutput poppit.CommandOutput, keyPrefix string, ttlDays int) error {
	// If Type is specified and not copilot-usage, ignore it
	if cmdOutput.Type != "" && cmdOutput.Type != "copilot-usage" {
		return nil
	}

	// If metadata source is specified and not copilotburn, ignore it
	if cmdOutput.Metadata != nil {
		if source, ok := cmdOutput.Metadata["source"].(string); ok && source != "copilotburn" {
			return nil
		}
	}

	if cmdOutput.StatusCode != 0 {
		return fmt.Errorf("command execution failed with exit code %d (command: %q): %s", cmdOutput.StatusCode, cmdOutput.Command, cmdOutput.StdErr)
	}

	outputStr := strings.TrimSpace(cmdOutput.Output)
	if outputStr == "" {
		return fmt.Errorf("empty command output for command %q", cmdOutput.Command)
	}

	var usageResp UsageResponse
	if err := json.Unmarshal([]byte(outputStr), &usageResp); err != nil {
		return fmt.Errorf("failed to parse JSON command output for command %q: %w", cmdOutput.Command, err)
	}

	year := usageResp.TimePeriod.Year
	month := usageResp.TimePeriod.Month
	day := usageResp.TimePeriod.Day
	if year == 0 || month == 0 || day == 0 {
		return fmt.Errorf("invalid timePeriod in usage response: %+v", usageResp.TimePeriod)
	}

	key := FormatRedisKey(keyPrefix, year, month, day)

	if ttlDays <= 0 {
		ttlDays = DefaultTTLDays
	}
	ttl := time.Duration(ttlDays) * 24 * time.Hour

	// Store JSON response verbatim in Redis
	if err := rdb.Set(ctx, key, cmdOutput.Output, ttl).Err(); err != nil {
		return fmt.Errorf("failed to store result in Redis key %s: %w", key, err)
	}

	log.Printf("Stored usage data in Redis key %s (TTL: %d days)", key, ttlDays)
	return nil
}

// StartOutputListenerWithBroadcaster subscribes to the Poppit command output channel, processes incoming outputs,
// and checks if all daily usage data up to today is available. When complete, it broadcasts the updated summary via SSE.
func StartOutputListenerWithBroadcaster(ctx context.Context, rdb redis.UniversalClient, channelName string, keyPrefix string, ttlDays int, quota float64, broadcaster *SSEBroadcaster, nowFunc func() time.Time) error {
	if channelName == "" {
		channelName = poppit.DefaultCommandOutputChannel
	}

	pubsub := rdb.Subscribe(ctx, channelName)

	// Wait for subscription confirmation
	if _, err := pubsub.Receive(ctx); err != nil {
		pubsub.Close()
		return fmt.Errorf("failed to subscribe to channel %s: %w", channelName, err)
	}
	log.Printf("Subscribed to Poppit command output channel: %s", channelName)

	ch := pubsub.Channel()
	go func() {
		defer pubsub.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				var cmdOutput poppit.CommandOutput
				if err := json.Unmarshal([]byte(msg.Payload), &cmdOutput); err != nil {
					log.Printf("Error unmarshaling command output message: %v", err)
					continue
				}

				if err := ProcessCommandOutput(ctx, rdb, cmdOutput, keyPrefix, ttlDays); err != nil {
					log.Printf("Error processing command output: %v", err)
					continue
				}

				now := time.Now()
				if nowFunc != nil {
					now = nowFunc()
				}

				missing, err := IdentifyMissingDays(ctx, rdb, now, keyPrefix)
				if err == nil && len(missing) == 0 {
					// Check if yesterday was in previous month and missing
					yesterday := now.AddDate(0, 0, -1)
					if yesterday.Month() != now.Month() {
						yesterdayKey := FormatRedisKey(keyPrefix, yesterday.Year(), int(yesterday.Month()), yesterday.Day())
						if exists, err := rdb.Exists(ctx, yesterdayKey).Result(); err == nil && exists == 0 {
							continue
						}
					}

					ResetRefreshState()

					if broadcaster != nil {
						summary, err := GetMonthlyUsageSummary(ctx, rdb, now, keyPrefix, quota)
						if err == nil && summary != nil {
							broadcaster.Broadcast(summary)
						} else if err != nil {
							log.Printf("Error generating usage summary for SSE broadcast: %v", err)
						}
					}
				}
			}
		}
	}()

	return nil
}

// StartOutputListener subscribes to the Poppit command output channel and processes incoming outputs.
func StartOutputListener(ctx context.Context, rdb redis.UniversalClient, channelName string, keyPrefix string, ttlDays int) error {
	return StartOutputListenerWithBroadcaster(ctx, rdb, channelName, keyPrefix, ttlDays, DefaultAICreditQuota, nil, nil)
}
