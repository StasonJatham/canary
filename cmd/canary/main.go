package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"canary/internal/auth"
	"canary/internal/config"
	"canary/internal/database"
	"canary/internal/handlers"
	"canary/internal/minifier"
	"canary/internal/models"
	"canary/internal/performance"
	"canary/internal/rules"
)

func main() {
	config.StartTime = time.Now()

	// Create data directory if it doesn't exist
	os.MkdirAll("data", 0755)

	// Build minified assets on startup
	if err := minifier.BuildDist("web", "dist"); err != nil {
		log.Printf("Warning: Failed to build minified assets: %v", err)
		log.Println("Will serve from web/ directory instead")
	}

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

	// Initialize auth database and create initial user
	if err := auth.InitializeAuthDB(db); err != nil {
		log.Fatalf("Failed to initialize auth database: %v", err)
	}

	username, password, created, err := auth.CreateInitialUser(db)
	if err != nil {
		log.Fatalf("Failed to create initial user: %v", err)
	}
	if created {
		log.Println("========================================")
		log.Printf("INITIAL USER CREATED")
		log.Printf("Username: %s", username)
		log.Printf("Password: %s", password)
		log.Println("Please save these credentials!")
		log.Println("Session expires after 30 days")
		log.Println("========================================")
	}

	// Run database migration for rule fields
	if err := database.MigrateAddRuleFields(); err != nil {
		log.Printf("Warning: Migration failed (may already be applied): %v", err)
	}

	// Cleanup old partition tables
	if err := database.CleanupOldPartitions(); err != nil {
		log.Printf("Warning: Failed to cleanup old partitions: %v", err)
	}

	// Schedule periodic cleanup of old partitions
	go func() {
		ticker := time.NewTicker(time.Duration(config.CleanupIntervalHours) * time.Hour)
		defer ticker.Stop()
		log.Printf("Partition cleanup scheduled every %d hours (retention: %d days)", config.CleanupIntervalHours, config.PartitionRetentionDays)
		for range ticker.C {
			if err := database.CleanupOldPartitions(); err != nil {
				log.Printf("Warning: Partition cleanup failed: %v", err)
			}
		}
	}()

	// Load rules (which includes building Aho-Corasick from rule keywords)
	ruleEngine, err := rules.LoadRules(config.RulesFile)
	if err != nil {
		log.Fatalf("Failed to load rules: %v", err)
	}

	log.Printf("Loaded %d rules (%d enabled)", len(ruleEngine.Rules), ruleEngine.GetEnabledRuleCount())
	log.Printf("Extracted %d unique keywords from rules", len(ruleEngine.Keywords))
	config.RuleEngine.Store(ruleEngine)

	// Initialize and start performance collector
	perfCollector := performance.NewCollector(db)
	config.PerfCollector.Store(perfCollector)
	perfCollector.Start(len(ruleEngine.Rules), len(ruleEngine.Keywords))
	log.Println("Performance monitoring started")

	// Start background workers
	config.MatchChan = make(chan models.Match, 10000)
	database.StartWorkers(4, 200, 200*time.Millisecond)

	// Start session cleanup
	handlers.StartSessionCleanup()

	// CORS middleware
	corsMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", config.CORSOrigin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

			// Enable credentials for cookie-based auth when not using wildcard origin
			if config.CORSOrigin != "*" {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	}

	// Setup HTTP routes
	mux := http.NewServeMux()

	// Public routes (no auth required)
	mux.HandleFunc("/login", handlers.ServeLogin)
	mux.HandleFunc("/auth/login", handlers.Login)
	mux.HandleFunc("/hook", handlers.Hook) // Webhook endpoint should be public
	mux.HandleFunc("/health", handlers.Health)
	mux.HandleFunc("/config", handlers.GetConfig) // Public config info

	// Create auth middleware
	authMW := auth.AuthMiddleware(db, config.SecureCookies)
	readOnlyMW := auth.ReadOnlyMiddleware(db, config.SecureCookies)

	// Choose middleware based on PUBLIC_DASHBOARD mode
	viewMW := authMW // Default: require auth for viewing
	if config.PublicDashboard {
		viewMW = readOnlyMW // Public mode: allow viewing, require auth for edits
	}

	// Routes that can be read-only in public mode
	mux.Handle("/", viewMW(http.HandlerFunc(handlers.ServeUI)))
	mux.Handle("/docs", viewMW(http.HandlerFunc(handlers.ServeAPIDocs)))
	mux.Handle("/openapi.yaml", viewMW(http.HandlerFunc(handlers.ServeOpenAPISpec)))
	mux.Handle("/matches", viewMW(http.HandlerFunc(handlers.GetMatches)))
	mux.Handle("/matches/recent", viewMW(http.HandlerFunc(handlers.GetRecentFromDB)))
	mux.Handle("/rules", viewMW(http.HandlerFunc(handlers.GetRules)))
	mux.Handle("/metrics", viewMW(http.HandlerFunc(handlers.Metrics)))
	mux.Handle("/metrics/performance", viewMW(http.HandlerFunc(handlers.GetPerformanceMetrics)))

	// Routes that always require full authentication (modifications)
	mux.Handle("/matches/clear", authMW(http.HandlerFunc(handlers.ClearMatches)))
	mux.Handle("/rules/reload", authMW(http.HandlerFunc(handlers.ReloadRules)))
	mux.Handle("/rules/create", authMW(http.HandlerFunc(handlers.CreateRule)))
	mux.Handle("/rules/update/", authMW(http.HandlerFunc(handlers.UpdateRule)))
	mux.Handle("/rules/delete/", authMW(http.HandlerFunc(handlers.DeleteRule)))
	mux.Handle("/rules/toggle/", authMW(http.HandlerFunc(handlers.ToggleRule)))
	mux.Handle("/auth/logout", authMW(http.HandlerFunc(handlers.Logout)))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Enable debug mode if DEBUG=true
	if os.Getenv("DEBUG") == "true" {
		config.Debug = true
		log.Println("DEBUG mode enabled - will log all incoming webhook payloads")
	}

	// Enable public dashboard mode
	if os.Getenv("PUBLIC_DASHBOARD") == "true" {
		config.PublicDashboard = true
		log.Println("PUBLIC_DASHBOARD mode enabled - dashboard is read-only without auth")
	}

	// Configure domain (for reverse proxy / HTTPS)
	config.Domain = os.Getenv("DOMAIN")
	if config.Domain != "" {
		// Assume HTTPS behind reverse proxy
		config.SecureCookies = true
		config.CORSOrigin = "https://" + config.Domain
		log.Printf("Domain configured: %s (secure cookies enabled, CORS origin: %s)", config.Domain, config.CORSOrigin)
	} else {
		// Local development mode
		config.SecureCookies = false
		config.CORSOrigin = "*"
		log.Println("Running in local mode (insecure cookies, CORS: *)")
	}

	// Configure partition retention from ENV
	if retentionDays := os.Getenv("PARTITION_RETENTION_DAYS"); retentionDays != "" {
		if days, err := time.ParseDuration(retentionDays + "h"); err == nil {
			config.PartitionRetentionDays = int(days.Hours() / 24)
		}
	}

	// Configure cleanup interval from ENV
	if cleanupInterval := os.Getenv("CLEANUP_INTERVAL_HOURS"); cleanupInterval != "" {
		if hours, err := time.ParseDuration(cleanupInterval + "h"); err == nil {
			config.CleanupIntervalHours = int(hours.Hours())
		}
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
	log.Printf("  GET  /rules             - List all loaded rules")
	log.Printf("  POST /rules/reload      - Reload rules from YAML file")
	log.Printf("  GET  /metrics           - System metrics")
	log.Printf("  GET  /health            - Health check")

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}
