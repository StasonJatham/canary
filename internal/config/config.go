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

	CurrentMatcher atomic.Value // *models.MatcherState

	CacheMutex    sync.RWMutex
	RecentMatches []models.Match

	KeywordsFile = "data/keywords.txt"

	MatchChan chan models.Match

	MaxRecentMatches = 500

	// Debug mode - logs incoming webhook payloads
	Debug bool

	// Statistics
	TotalMatches atomic.Int64
	TotalCerts   atomic.Int64
	StartTime    time.Time

	// Partitioned tables for horizontal scaling
	PartitionTables = []string{
		"matches_a", "matches_b", "matches_c", "matches_d", "matches_e",
		"matches_f", "matches_g", "matches_h", "matches_i", "matches_j",
		"matches_k", "matches_l", "matches_m", "matches_n", "matches_o",
		"matches_p", "matches_q", "matches_r", "matches_s", "matches_t",
		"matches_u", "matches_v", "matches_w", "matches_x", "matches_y",
		"matches_z", "matches_other",
	}
)
