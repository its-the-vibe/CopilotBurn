package copilotburn

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// DailyUsage represents the usage statistics for a specific day in the month.
type DailyUsage struct {
	Day               int         `json:"day"`
	Date              string      `json:"date"`
	Credits           float64     `json:"credits"`
	CumulativeCredits *float64    `json:"cumulative_credits"`
	IsToday           bool        `json:"is_today"`
	IsFuture          bool        `json:"is_future"`
	HasData           bool        `json:"has_data"`
	UsageItems        []UsageItem `json:"usage_items,omitempty"`
}

// MonthlyUsageSummary represents the aggregated usage metrics for a given month.
type MonthlyUsageSummary struct {
	Year           int          `json:"year"`
	Month          int          `json:"month"`
	MonthName      string       `json:"month_name"`
	CurrentDate    string       `json:"current_date"`
	TodayDay       int          `json:"today_day"`
	DaysInMonth    int          `json:"days_in_month"`
	Quota          float64      `json:"quota"`
	MonthlyTotal   float64      `json:"monthly_total"`
	TodayUsage     float64      `json:"today_usage"`
	RemainingQuota float64      `json:"remaining_quota"`
	PercentageUsed float64      `json:"percentage_used"`
	DailyUsage     []DailyUsage `json:"daily_usage"`
}

func round4(val float64) float64 {
	return math.Round(val*10000) / 10000
}

func round2(val float64) float64 {
	return math.Round(val*100) / 100
}

// ExtractDayCredits parses a raw JSON usage response and computes the total AI credits used.
// It also returns the list of UsageItems found in the response.
func ExtractDayCredits(rawJSON string) (float64, []UsageItem, error) {
	trimmed := strings.TrimSpace(rawJSON)
	if trimmed == "" || trimmed == "{}" {
		return 0, nil, nil
	}

	var resp UsageResponse
	if err := json.Unmarshal([]byte(trimmed), &resp); err != nil {
		return 0, nil, fmt.Errorf("parsing usage json: %w", err)
	}

	var total float64
	for _, item := range resp.UsageItems {
		qty := item.GrossQuantity
		if qty == 0 && item.NetQuantity > 0 {
			qty = item.NetQuantity
		}
		total += qty
	}

	return round4(total), resp.UsageItems, nil
}

// GetMonthlyUsage calculates the monthly AI credit usage up to today (or the end of month if in the past)
// by reading daily records from Redis.
func GetMonthlyUsage(ctx context.Context, rdb redis.Cmdable, keyPrefix string, targetDate time.Time, now time.Time, quota float64) (*MonthlyUsageSummary, error) {
	if quota <= 0 {
		quota = DefaultAICreditQuota
	}
	if now.IsZero() {
		now = time.Now()
	}
	if targetDate.IsZero() {
		targetDate = now
	}

	year := targetDate.Year()
	month := targetDate.Month()

	// Number of days in the month: Day 0 of the next month gives the last day of the current month
	daysInMonth := time.Date(year, month+1, 0, 0, 0, 0, 0, targetDate.Location()).Day()

	nowYear, nowMonth, nowDay := now.Date()

	isCurrentMonth := (year == nowYear && month == nowMonth)
	isPastMonth := (year < nowYear || (year == nowYear && month < nowMonth))

	var todayDay int
	var currentDateStr string
	if isCurrentMonth {
		todayDay = nowDay
		currentDateStr = now.Format("2006-01-02")
	} else if isPastMonth {
		todayDay = daysInMonth
		currentDateStr = fmt.Sprintf("%04d-%02d-%02d", year, int(month), daysInMonth)
	} else {
		// Future month
		todayDay = 0
		currentDateStr = now.Format("2006-01-02")
	}

	// Prepare keys for MGet
	keys := make([]string, daysInMonth)
	for d := 1; d <= daysInMonth; d++ {
		keys[d-1] = FormatRedisKey(keyPrefix, year, int(month), d)
	}

	rawValues, err := rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, fmt.Errorf("fetching daily usage from Redis: %w", err)
	}

	dailyList := make([]DailyUsage, daysInMonth)
	var cumulative float64
	var todayUsage float64

	for d := 1; d <= daysInMonth; d++ {
		dateStr := fmt.Sprintf("%04d-%02d-%02d", year, int(month), d)
		isToday := (isCurrentMonth && d == todayDay)
		isFuture := (isCurrentMonth && d > todayDay) || (!isCurrentMonth && !isPastMonth)

		rawVal := rawValues[d-1]
		var dayCredits float64
		var items []UsageItem
		hasData := false

		if rawVal != nil && !isFuture {
			var strVal string
			switch v := rawVal.(type) {
			case string:
				strVal = v
			case []byte:
				strVal = string(v)
			}
			if strings.TrimSpace(strVal) != "" {
				c, itms, err := ExtractDayCredits(strVal)
				if err == nil {
					hasData = true
					dayCredits = c
					items = itms
				}
			}
		}

		if !isFuture {
			cumulative += dayCredits
			cumCopy := round4(cumulative)
			dailyList[d-1] = DailyUsage{
				Day:               d,
				Date:              dateStr,
				Credits:           round4(dayCredits),
				CumulativeCredits: &cumCopy,
				IsToday:           isToday,
				IsFuture:          false,
				HasData:           hasData,
				UsageItems:        items,
			}
			if isToday {
				todayUsage = dayCredits
			}
		} else {
			dailyList[d-1] = DailyUsage{
				Day:               d,
				Date:              dateStr,
				Credits:           0,
				CumulativeCredits: nil,
				IsToday:           false,
				IsFuture:          true,
				HasData:           false,
			}
		}
	}

	monthlyTotal := round4(cumulative)
	remaining := round4(math.Max(0, quota-monthlyTotal))
	var pct float64
	if quota > 0 {
		pct = round2((monthlyTotal / quota) * 100)
	}

	return &MonthlyUsageSummary{
		Year:           year,
		Month:          int(month),
		MonthName:      month.String(),
		CurrentDate:    currentDateStr,
		TodayDay:       todayDay,
		DaysInMonth:    daysInMonth,
		Quota:          round4(quota),
		MonthlyTotal:   monthlyTotal,
		TodayUsage:     round4(todayUsage),
		RemainingQuota: remaining,
		PercentageUsed: pct,
		DailyUsage:     dailyList,
	}, nil
}
