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
	prefix = strings.TrimSuffix(prefix, ":")
	if prefix != "" {
		prefix = prefix + ":"
	}
	return fmt.Sprintf("%s%04d-%02d-%02d", prefix, year, month, day)
}

// FetchMissingDailyData identifies missing usage data for all days of the current month up to today,
// builds gh commands for those days, and submits them to the Poppit engine.
func FetchMissingDailyData(ctx context.Context, rdb redis.Cmdable, engine *poppit.Engine, now time.Time, keyPrefix string) ([]poppit.Command, error) {
	year := now.Year()
	month := int(now.Month())
	today := now.Day()

	var missingCmds []poppit.Command

	for day := 1; day <= today; day++ {
		key := FormatRedisKey(keyPrefix, year, month, day)
		exists, err := rdb.Exists(ctx, key).Result()
		if err != nil {
			log.Printf("Error checking Redis key %s: %v", key, err)
			// Continue attempting other days even if checking one fails
		}

		if exists == 0 {
			cmd := poppit.Command{
				ID:     fmt.Sprintf("gh-vibe-usage-%04d-%02d-%02d", year, month, day),
				Name:   "gh",
				Args:   []string{"vibe", "usage", "--year", fmt.Sprintf("%d", year), "--month", fmt.Sprintf("%d", month), "--day", fmt.Sprintf("%d", day)},
				Target: fmt.Sprintf("%04d-%02d-%02d", year, month, day),
			}
			missingCmds = append(missingCmds, cmd)
		}
	}

	if len(missingCmds) > 0 {
		engine.SubmitBatch(missingCmds)
	}

	return missingCmds, nil
}

// ProcessCommandResult parses a command result and stores the verbatim JSON response in Redis with TTL.
func ProcessCommandResult(ctx context.Context, rdb redis.Cmdable, res poppit.CommandResult, keyPrefix string, ttlDays int) error {
	if res.Err != nil {
		return fmt.Errorf("command execution failed for target %s: %w", res.Command.Target, res.Err)
	}

	if len(res.Stdout) == 0 {
		return fmt.Errorf("empty command output for target %s", res.Command.Target)
	}

	var usageResp UsageResponse
	if err := json.Unmarshal(res.Stdout, &usageResp); err != nil {
		return fmt.Errorf("failed to parse JSON command output for target %s: %w", res.Command.Target, err)
	}

	year := usageResp.TimePeriod.Year
	month := usageResp.TimePeriod.Month
	day := usageResp.TimePeriod.Day

	if year == 0 || month == 0 || day == 0 {
		return fmt.Errorf("invalid timePeriod in usage response: %v", usageResp.TimePeriod)
	}

	key := FormatRedisKey(keyPrefix, year, month, day)
	ttl := time.Duration(ttlDays) * 24 * time.Hour

	// Store JSON response verbatim in Redis
	if err := rdb.Set(ctx, key, string(res.Stdout), ttl).Err(); err != nil {
		return fmt.Errorf("failed to store result in Redis key %s: %w", key, err)
	}

	log.Printf("Stored usage data in Redis key %s (TTL: %d days)", key, ttlDays)
	return nil
}

// StartOutputListener listens for Poppit command execution results and stores them in Redis.
func StartOutputListener(ctx context.Context, rdb redis.Cmdable, results <-chan poppit.CommandResult, keyPrefix string, ttlDays int) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case res, ok := <-results:
				if !ok {
					return
				}
				if err := ProcessCommandResult(ctx, rdb, res, keyPrefix, ttlDays); err != nil {
					log.Printf("Error processing command result: %v", err)
				}
			}
		}
	}()
}
