package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/its-the-vibe/CopilotBurn/pkg/copilotburn"
	"github.com/redis/go-redis/v9"
)

//go:embed static/*
var staticFS embed.FS

// Server is the HTTP server for the CopilotBurn dashboard and API.
type Server struct {
	rdb        redis.Cmdable
	keyPrefix  string
	quota      float64
	port       int
	httpServer *http.Server
}

// NewServer creates a new Server instance.
func NewServer(rdb redis.Cmdable, keyPrefix string, quota float64, port int) *Server {
	if quota <= 0 {
		quota = copilotburn.DefaultAICreditQuota
	}
	if port <= 0 {
		port = copilotburn.DefaultServerPort
	}
	if keyPrefix == "" {
		keyPrefix = copilotburn.DefaultKeyPrefix
	}

	s := &Server{
		rdb:       rdb,
		keyPrefix: keyPrefix,
		quota:     quota,
		port:      port,
	}

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      s.Handler(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return s
}

// Handler returns the HTTP handler for the server routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Sub-filesystem for static assets
	subStatic, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatalf("failed to create static sub-filesystem: %v", err)
	}

	// Static files handler (/static/*)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(subStatic))))

	// API routes
	mux.HandleFunc("/api/usage", s.handleUsageAPI)
	mux.HandleFunc("/api/health", s.handleHealthAPI)

	// Dashboard UI routes
	mux.HandleFunc("/dashboard", s.handleDashboard)
	mux.HandleFunc("/", s.handleRoot)

	return mux
}

// Start runs the HTTP server listening on the configured port.
func (s *Server) Start() error {
	log.Printf("CopilotBurn dashboard server listening on %s", s.httpServer.Addr)
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) serveIndexHTML(w http.ResponseWriter, r *http.Request) {
	data, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "dashboard template not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	s.serveIndexHTML(w, r)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	s.serveIndexHTML(w, r)
}

func (s *Server) handleHealthAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleUsageAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	now := time.Now()
	targetDate := now

	query := r.URL.Query()
	yearStr := strings.TrimSpace(query.Get("year"))
	monthStr := strings.TrimSpace(query.Get("month"))

	if yearStr != "" || monthStr != "" {
		year := now.Year()
		month := int(now.Month())

		if yearStr != "" {
			y, err := strconv.Atoi(yearStr)
			if err != nil || y < 2000 || y > 2100 {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid year parameter"})
				return
			}
			year = y
		}

		if monthStr != "" {
			m, err := strconv.Atoi(monthStr)
			if err != nil || m < 1 || m > 12 {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid month parameter"})
				return
			}
			month = m
		}

		targetDate = time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	}

	summary, err := copilotburn.GetMonthlyUsage(r.Context(), s.rdb, s.keyPrefix, targetDate, now, s.quota)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("failed to get usage data: %v", err),
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(summary); err != nil {
		log.Printf("error encoding usage response: %v", err)
	}
}
