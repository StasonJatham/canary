package performance

import (
	"database/sql"
	"log"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"canary/internal/models"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

// Collector tracks performance metrics
type Collector struct {
	certsProcessed   atomic.Int64
	matchesFound     atomic.Int64
	totalMatchTimeUs atomic.Int64
	matchCount       atomic.Int64

	mu              sync.RWMutex
	currentMetrics  *models.PerformanceMetrics
	recentMetrics   []*models.PerformanceMetrics
	maxRecentMetrics int

	db *sql.DB
}

// NewCollector creates a new performance collector
func NewCollector(db *sql.DB) *Collector {
	return &Collector{
		db:               db,
		maxRecentMetrics: 60, // Keep last 60 minutes
		recentMetrics:    make([]*models.PerformanceMetrics, 0, 60),
	}
}

// RecordCertProcessed increments the certificate counter
func (c *Collector) RecordCertProcessed() {
	c.certsProcessed.Add(1)
}

// RecordMatch records a match and its processing time
func (c *Collector) RecordMatch(durationUs int64) {
	c.matchesFound.Add(1)
	c.totalMatchTimeUs.Add(durationUs)
	c.matchCount.Add(1)
}

// Start begins the metrics collection loop
func (c *Collector) Start(rulesCount, keywordsCount int) {
	// Initialize the performance_metrics table
	if err := c.initTable(); err != nil {
		log.Printf("Warning: Failed to initialize performance_metrics table: %v", err)
	}

	// Collect metrics every minute
	ticker := time.NewTicker(1 * time.Minute)
	go func() {
		for range ticker.C {
			metrics := c.collectMetrics(rulesCount, keywordsCount)

			// Store in memory
			c.mu.Lock()
			c.currentMetrics = metrics
			c.recentMetrics = append(c.recentMetrics, metrics)
			if len(c.recentMetrics) > c.maxRecentMetrics {
				c.recentMetrics = c.recentMetrics[1:]
			}
			c.mu.Unlock()

			// Store in database
			if err := c.saveToDatabase(metrics); err != nil {
				log.Printf("Warning: Failed to save metrics to database: %v", err)
			}

			// Reset per-minute counters
			c.certsProcessed.Store(0)
			c.matchesFound.Store(0)
			c.totalMatchTimeUs.Store(0)
			c.matchCount.Store(0)

			log.Printf("Performance: %d certs/min, %d matches/min, %.2f%% CPU, %.1f MB RAM, avg match time: %d μs",
				metrics.CertsPerMinute, metrics.MatchesPerMinute, metrics.CPUPercent,
				metrics.MemoryUsedMB, metrics.AvgMatchTimeUs)
		}
	}()
}

// collectMetrics gathers current performance statistics
func (c *Collector) collectMetrics(rulesCount, keywordsCount int) *models.PerformanceMetrics {
	// Get CPU usage
	cpuPercent, err := cpu.Percent(time.Second, false)
	var cpuUsage float64
	if err == nil && len(cpuPercent) > 0 {
		cpuUsage = cpuPercent[0]
	}

	// Get memory usage
	vmem, err := mem.VirtualMemory()
	var memUsed, memTotal float64
	if err == nil {
		memUsed = float64(vmem.Used) / 1024 / 1024    // MB
		memTotal = float64(vmem.Total) / 1024 / 1024  // MB
	}

	// Get database size
	dbSize := c.getDatabaseSize()

	// Calculate average match time
	matchCount := c.matchCount.Load()
	var avgMatchTime int64
	if matchCount > 0 {
		avgMatchTime = c.totalMatchTimeUs.Load() / matchCount
	}

	return &models.PerformanceMetrics{
		Timestamp:        time.Now(),
		CertsPerMinute:   int(c.certsProcessed.Load()),
		MatchesPerMinute: int(c.matchesFound.Load()),
		AvgMatchTimeUs:   avgMatchTime,
		CPUPercent:       cpuUsage,
		MemoryUsedMB:     memUsed,
		MemoryTotalMB:    memTotal,
		GoroutineCount:   runtime.NumGoroutine(),
		RulesEvaluated:   rulesCount,
		KeywordsInAC:     keywordsCount,
		DatabaseSizeMB:   dbSize,
	}
}

// GetCurrentMetrics returns the most recent metrics
func (c *Collector) GetCurrentMetrics() *models.PerformanceMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentMetrics
}

// GetRecentMetrics returns recent metrics history
func (c *Collector) GetRecentMetrics(minutes int) []*models.PerformanceMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if minutes <= 0 || minutes > len(c.recentMetrics) {
		// Return all available metrics
		result := make([]*models.PerformanceMetrics, len(c.recentMetrics))
		copy(result, c.recentMetrics)
		return result
	}

	// Return last N minutes
	start := len(c.recentMetrics) - minutes
	result := make([]*models.PerformanceMetrics, minutes)
	copy(result, c.recentMetrics[start:])
	return result
}

// initTable creates the performance_metrics table if it doesn't exist
func (c *Collector) initTable() error {
	query := `
		CREATE TABLE IF NOT EXISTS performance_metrics (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME NOT NULL,
			certs_per_minute INTEGER NOT NULL,
			matches_per_minute INTEGER NOT NULL,
			avg_match_time_us INTEGER NOT NULL,
			cpu_percent REAL NOT NULL,
			memory_used_mb REAL NOT NULL,
			memory_total_mb REAL NOT NULL,
			goroutine_count INTEGER NOT NULL,
			rules_evaluated INTEGER NOT NULL,
			keywords_in_ac INTEGER NOT NULL,
			database_size_mb REAL NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_perf_timestamp ON performance_metrics(timestamp);
	`
	_, err := c.db.Exec(query)
	return err
}

// saveToDatabase stores metrics in the database
func (c *Collector) saveToDatabase(m *models.PerformanceMetrics) error {
	query := `
		INSERT INTO performance_metrics (
			timestamp, certs_per_minute, matches_per_minute, avg_match_time_us,
			cpu_percent, memory_used_mb, memory_total_mb, goroutine_count,
			rules_evaluated, keywords_in_ac, database_size_mb
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := c.db.Exec(query,
		m.Timestamp, m.CertsPerMinute, m.MatchesPerMinute, m.AvgMatchTimeUs,
		m.CPUPercent, m.MemoryUsedMB, m.MemoryTotalMB, m.GoroutineCount,
		m.RulesEvaluated, m.KeywordsInAC, m.DatabaseSizeMB,
	)
	return err
}

// getDatabaseSize returns the database file size in MB
func (c *Collector) getDatabaseSize() float64 {
	var pageCount, pageSize int64
	err := c.db.QueryRow("PRAGMA page_count").Scan(&pageCount)
	if err != nil {
		return 0
	}
	err = c.db.QueryRow("PRAGMA page_size").Scan(&pageSize)
	if err != nil {
		return 0
	}
	return float64(pageCount*pageSize) / 1024 / 1024
}

// GetMetricsFromDB retrieves metrics from database
func (c *Collector) GetMetricsFromDB(minutes int) ([]*models.PerformanceMetrics, error) {
	query := `
		SELECT timestamp, certs_per_minute, matches_per_minute, avg_match_time_us,
		       cpu_percent, memory_used_mb, memory_total_mb, goroutine_count,
		       rules_evaluated, keywords_in_ac, database_size_mb
		FROM performance_metrics
		WHERE timestamp >= datetime('now', '-' || ? || ' minutes')
		ORDER BY timestamp ASC
	`

	rows, err := c.db.Query(query, minutes)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var metrics []*models.PerformanceMetrics
	for rows.Next() {
		m := &models.PerformanceMetrics{}
		err := rows.Scan(
			&m.Timestamp, &m.CertsPerMinute, &m.MatchesPerMinute, &m.AvgMatchTimeUs,
			&m.CPUPercent, &m.MemoryUsedMB, &m.MemoryTotalMB, &m.GoroutineCount,
			&m.RulesEvaluated, &m.KeywordsInAC, &m.DatabaseSizeMB,
		)
		if err != nil {
			return nil, err
		}
		metrics = append(metrics, m)
	}

	return metrics, rows.Err()
}

// CleanupOldMetrics removes metrics older than retention period
func (c *Collector) CleanupOldMetrics(retentionDays int) error {
	query := `
		DELETE FROM performance_metrics
		WHERE timestamp < datetime('now', '-' || ? || ' days')
	`
	_, err := c.db.Exec(query, retentionDays)
	return err
}
