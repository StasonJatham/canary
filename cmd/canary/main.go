package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"canary/internal/config"
	"canary/internal/database"
	"canary/internal/handlers"
	"canary/internal/matcher"
	"canary/internal/models"
)

func main() {
	config.StartTime = time.Now()

	// Create data directory if it doesn't exist
	os.MkdirAll("data", 0755)

	// Initialize database
	db, err := database.Open("data/matches.db")
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	config.DB = db
	defer db.Close()

	if err := database.CreatePartitionTables(); err != nil {
		log.Fatalf("Failed to create partition tables: %v", err)
	}

	// Load keywords
	if err := matcher.Load(config.KeywordsFile); err != nil {
		log.Fatalf("Failed to load keywords on startup: %v", err)
	}

	// Start background workers
	config.MatchChan = make(chan models.Match, 10000)
	database.StartWorkers(4, 200, 200*time.Millisecond)

	// CORS middleware
	corsMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	}

	// Setup HTTP routes
	mux := http.NewServeMux()
	mux.HandleFunc("/", handlers.ServeUI)
	mux.HandleFunc("/docs", handlers.ServeAPIDocs)
	mux.HandleFunc("/openapi.yaml", handlers.ServeOpenAPISpec)
	mux.HandleFunc("/hook", handlers.Hook)
	mux.HandleFunc("/matches", handlers.GetMatches)
	mux.HandleFunc("/matches/clear", handlers.ClearMatches)
	mux.HandleFunc("/keywords/reload", handlers.ReloadKeywords)
	mux.HandleFunc("/keywords", handlers.AddKeywords)
	mux.HandleFunc("/matches/recent", handlers.GetRecentFromDB)
	mux.HandleFunc("/metrics", handlers.Metrics)
	mux.HandleFunc("/health", handlers.Health)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Enable debug mode if DEBUG=true
	if os.Getenv("DEBUG") == "true" {
		config.Debug = true
		log.Println("DEBUG mode enabled - will log all incoming webhook payloads")
	}

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      corsMiddleware(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	// Setup graceful shutdown
	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt, syscall.SIGTERM)
		<-sigint

		log.Println("Shutting down server...")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("Server shutdown error: %v", err)
		}

		// Close match channel to stop workers
		close(config.MatchChan)

		log.Println("Server stopped")
	}()

	log.Printf("Starting Canary CT Monitor on port %s", port)
	log.Printf("Endpoints:")
	log.Printf("  GET  /                  - Web UI Dashboard")
	log.Printf("  GET  /docs              - API Documentation (ReDoc)")
	log.Printf("  POST /hook              - Accept Certspotter webhooks")
	log.Printf("  GET  /matches           - Get recent matches from memory")
	log.Printf("  GET  /matches/recent    - Get matches from DB (param: minutes)")
	log.Printf("  POST /matches/clear     - Clear in-memory matches")
	log.Printf("  POST /keywords          - Add new keywords")
	log.Printf("  POST /keywords/reload   - Reload keywords from file")
	log.Printf("  GET  /metrics           - System metrics")
	log.Printf("  GET  /health            - Health check")

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}
