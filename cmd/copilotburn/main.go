package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/its-the-vibe/CopilotBurn/pkg/copilotburn"
	"github.com/its-the-vibe/CopilotBurn/pkg/poppit"
	"github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
)

type Config struct {
	Redis struct {
		Host      string
		Port      int
		Password  string
		KeyPrefix string `mapstructure:"key_prefix"`
		TTLDays   int    `mapstructure:"ttl_days"`
	}
	Poppit struct {
		ListName      string `mapstructure:"list_name"`
		OutputChannel string `mapstructure:"output_channel"`
	}
	Server struct {
		Port int `mapstructure:"port"`
	}
	AICreditQuota       float64 `mapstructure:"ai_credit_quota"`
	PingIntervalSeconds int     `mapstructure:"ping_interval_seconds"`
}

func loadConfig() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("/")

	viper.SetDefault("redis.host", "localhost")
	viper.SetDefault("redis.port", 6379)
	viper.SetDefault("redis.key_prefix", copilotburn.DefaultKeyPrefix)
	viper.SetDefault("redis.ttl_days", copilotburn.DefaultTTLDays)
	viper.SetDefault("poppit.list_name", poppit.DefaultNotificationListName)
	viper.SetDefault("poppit.output_channel", poppit.DefaultCommandOutputChannel)
	viper.SetDefault("server.port", copilotburn.DefaultServerPort)
	viper.SetDefault("ai_credit_quota", copilotburn.DefaultAICreditQuota)
	viper.SetDefault("ping_interval_seconds", 5)

	// Environment variables
	viper.AutomaticEnv()
	viper.BindEnv("redis.password", "REDIS_PASSWORD")
	viper.BindEnv("redis.key_prefix", "REDIS_KEY_PREFIX")
	viper.BindEnv("redis.ttl_days", "REDIS_TTL_DAYS")
	viper.BindEnv("poppit.list_name", "POPPIT_SERVICE_REDIS_LIST_NAME", "POPPIT_LIST_NAME")
	viper.BindEnv("poppit.output_channel", "POPPIT_SERVICE_COMMAND_OUTPUT_CHANNEL", "POPPIT_OUTPUT_CHANNEL")
	viper.BindEnv("server.port", "PORT", "SERVER_PORT")
	viper.BindEnv("ai_credit_quota", "AI_CREDIT_QUOTA")

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config: %w", err)
		}
		log.Println("No config file found, using defaults and environment variables")
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshalling config: %w", err)
	}
	return &cfg, nil
}

func main() {
	fmt.Println("Gday World")

	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	addr := fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port)
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: cfg.Redis.Password,
	})
	defer rdb.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Test Redis connection
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Printf("Warning: initial Redis ping failed: %v", err)
	}

	broadcaster := copilotburn.NewSSEBroadcaster()

	// Start command output listener for Poppit
	if err := copilotburn.StartOutputListenerWithBroadcaster(ctx, rdb, cfg.Poppit.OutputChannel, cfg.Redis.KeyPrefix, cfg.Redis.TTLDays, cfg.AICreditQuota, broadcaster, nil); err != nil {
		log.Printf("Error starting output listener: %v", err)
	}

	// Fetch missing daily data on startup
	log.Println("Checking for missing daily Copilot usage data...")
	missingCmds, err := copilotburn.FetchMissingDailyData(ctx, rdb, cfg.Poppit.ListName, time.Now(), cfg.Redis.KeyPrefix)
	if err != nil {
		log.Printf("Error fetching missing daily data: %v", err)
	} else {
		log.Printf("Submitted %d missing daily data commands to Poppit", len(missingCmds))
	}

	// Start HTTP server for dashboard and API
	mux := http.NewServeMux()
	mux.HandleFunc("/api/usage", copilotburn.HandleAPIUsage(rdb, cfg.Redis.KeyPrefix, cfg.AICreditQuota, nil))
	mux.HandleFunc("/api/refresh", copilotburn.HandleAPIRefresh(rdb, cfg.Redis.KeyPrefix, cfg.Poppit.ListName, cfg.AICreditQuota, broadcaster, nil))
	mux.Handle("/api/events", broadcaster)
	mux.Handle("/api/sse", broadcaster)
	mux.Handle("/", copilotburn.WebHandler())

	serverAddr := fmt.Sprintf(":%d", cfg.Server.Port)
	srv := &http.Server{
		Addr:    serverAddr,
		Handler: mux,
	}

	go func() {
		log.Printf("Starting dashboard server on http://localhost%s", serverAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("HTTP server shutdown error: %v", err)
		}
	}()

	interval := time.Duration(cfg.PingIntervalSeconds) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("Pinging Redis at %s every %v", addr, interval)

	for {
		select {
		case <-ctx.Done():
			log.Println("Shutting down")
			return
		case <-ticker.C:
			if err := rdb.Ping(ctx).Err(); err != nil {
				log.Printf("Redis ping failed: %v", err)
			} else {
				// log.Println("Redis ping: PONG")
			}
		}
	}
}
