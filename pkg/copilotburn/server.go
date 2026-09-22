package copilotburn

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
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

// SSEBroadcaster manages live Server-Sent Events client connections and broadcasts real-time updates.
type SSEBroadcaster struct {
	mu      sync.RWMutex
	clients map[chan []byte]bool
}

// NewSSEBroadcaster creates a new SSEBroadcaster instance.
func NewSSEBroadcaster() *SSEBroadcaster {
	return &SSEBroadcaster{
		clients: make(map[chan []byte]bool),
	}
}

// ServeHTTP implements http.Handler for streaming Server-Sent Events.
func (b *SSEBroadcaster) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := make(chan []byte, 16)

	b.mu.Lock()
	b.clients[ch] = true
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		delete(b.clients, ch)
		close(ch)
		b.mu.Unlock()
	}()

	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		}
	}
}

// Broadcast sends the UsageSummary payload to all connected SSE clients.
func (b *SSEBroadcaster) Broadcast(summary *UsageSummary) {
	if summary == nil {
		return
	}
	payload, err := json.Marshal(summary)
	if err != nil {
		log.Printf("Error marshaling summary for SSE broadcast: %v", err)
		return
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	for ch := range b.clients {
		select {
		case ch <- payload:
		default:
			// Non-blocking write to prevent slow clients from blocking
		}
	}
}

// ClientCount returns the current number of active SSE client connections.
func (b *SSEBroadcaster) ClientCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.clients)
}

type refreshStateTracker struct {
	mu           sync.Mutex
	isRefreshing bool
	startTime    time.Time
}

var globalRefreshState refreshStateTracker

// ResetRefreshState resets the in-progress refresh flag.
func ResetRefreshState() {
	globalRefreshState.mu.Lock()
	globalRefreshState.isRefreshing = false
	globalRefreshState.mu.Unlock()
}

// HandleAPIRefresh returns an HTTP handler for POST /api/refresh.
func HandleAPIRefresh(rdb redis.Cmdable, keyPrefix string, poppitListName string, quota float64, broadcaster *SSEBroadcaster, nowFunc func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")

		if r.Method != http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusMethodNotAllowed)
			json.NewEncoder(w).Encode(map[string]string{"error": "Method not allowed"})
			return
		}

		globalRefreshState.mu.Lock()
		if globalRefreshState.isRefreshing && time.Since(globalRefreshState.startTime) < 30*time.Second {
			globalRefreshState.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{"error": "Data refresh is already in progress"})
			return
		}
		globalRefreshState.isRefreshing = true
		globalRefreshState.startTime = time.Now()
		globalRefreshState.mu.Unlock()

		now := time.Now()
		if nowFunc != nil {
			now = nowFunc()
		}

		commands, err := TriggerRefresh(r.Context(), rdb, keyPrefix, poppitListName, now)
		if err != nil {
			ResetRefreshState()
			log.Printf("Error triggering refresh: %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Failed to refresh data: %v", err)})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":             "ok",
			"message":            "Data refresh initiated",
			"commands_submitted": len(commands),
		})
	}
}
