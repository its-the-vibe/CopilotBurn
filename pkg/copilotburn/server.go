package copilotburn

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

// DailyUsage represents AI credit usage for a single day.
type DailyUsage struct {
	Date              string  `json:"date"`
	Day               int     `json:"day"`
	DailyCredits      float64 `json:"daily_credits"`
	CumulativeCredits float64 `json:"cumulative_credits"`
}

// UsageSummary represents the monthly AI credit usage dashboard summary.
type UsageSummary struct {
	Quota        float64      `json:"quota"`
	CurrentDate  string       `json:"current_date"`
	TotalCredits float64      `json:"total_credits"`
	TodayCredits float64      `json:"today_credits"`
	Daily        []DailyUsage `json:"daily"`
}

// GetMonthlyUsageSummary calculates cumulative daily AI credit usage for the current month up to today.
func GetMonthlyUsageSummary(ctx context.Context, rdb redis.Cmdable, now time.Time, keyPrefix string, quota float64) (*UsageSummary, error) {
	year := now.Year()
	month := now.Month()
	today := now.Day()

	dailyList := make([]DailyUsage, 0, today)
	cumulative := 0.0
	todayCredits := 0.0

	for d := 1; d <= today; d++ {
		key := FormatRedisKey(keyPrefix, year, int(month), d)
		val, err := rdb.Get(ctx, key).Result()

		dailyCredits := 0.0
		if err == nil && val != "" {
			var resp UsageResponse
			if err := json.Unmarshal([]byte(val), &resp); err == nil {
				for _, item := range resp.UsageItems {
					if item.GrossQuantity > 0 {
						dailyCredits += item.GrossQuantity
					} else if item.NetQuantity > 0 {
						dailyCredits += item.NetQuantity
					}
				}
			} else {
				log.Printf("Warning: failed to parse Redis usage key %s: %v", key, err)
			}
		}

		cumulative += dailyCredits
		if d == today {
			todayCredits = dailyCredits
		}

		dailyList = append(dailyList, DailyUsage{
			Date:              fmt.Sprintf("%04d-%02d-%02d", year, int(month), d),
			Day:               d,
			DailyCredits:      dailyCredits,
			CumulativeCredits: cumulative,
		})
	}

	if quota <= 0 {
		quota = DefaultAICreditQuota
	}

	summary := &UsageSummary{
		Quota:        quota,
		CurrentDate:  fmt.Sprintf("%04d-%02d-%02d", year, int(month), today),
		TotalCredits: cumulative,
		TodayCredits: todayCredits,
		Daily:        dailyList,
	}

	return summary, nil
}

// HandleAPIUsage returns an HTTP handler for GET /api/usage.
func HandleAPIUsage(rdb redis.Cmdable, keyPrefix string, quota float64, nowFunc func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		now := time.Now()
		if nowFunc != nil {
			now = nowFunc()
		}

		summary, err := GetMonthlyUsageSummary(r.Context(), rdb, now, keyPrefix, quota)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error fetching usage summary: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if err := json.NewEncoder(w).Encode(summary); err != nil {
			log.Printf("Error encoding usage summary response: %v", err)
		}
	}
}
