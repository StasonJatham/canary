package config

import (
	"database/sql"
	"sync"
	"sync/atomic"
	"time"

	"canary/internal/models"
)

// Global state
var (
	DB *sql.DB

	RuleEngine atomic.Value // *rules.Engine

	CacheMutex    sync.RWMutex
	RecentMatches []models.Match

	RulesFile = "data/rules.yaml"

	MatchChan chan models.Match

	MaxRecentMatches = 500

	// Debug mode - logs incoming webhook payloads
	Debug bool

	// Public dashboard - allows viewing without auth (no editing)
	PublicDashboard bool

	// Domain - if set, assumes HTTPS behind reverse proxy
	Domain string

	// Secure cookies - automatically enabled if Domain is set
	SecureCookies bool

	// CORS allowed origin - automatically set based on Domain
	CORSOrigin string

	// Statistics
	TotalMatches atomic.Int64
	TotalCerts   atomic.Int64
	StartTime    time.Time

	// Partition retention period (days)
	PartitionRetentionDays = 30

	// Cleanup interval (hours) - how often to run partition cleanup
	CleanupIntervalHours = 24 // Default: daily

	// Performance collector
	PerfCollector atomic.Value // *performance.Collector
)
