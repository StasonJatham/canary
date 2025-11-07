package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"canary/internal/config"
	"canary/internal/models"

	_ "github.com/mattn/go-sqlite3"
)

// Open opens a SQLite database with optimized settings
func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path+"?_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("db open error: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		return nil, fmt.Errorf("error enabling WAL mode: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000;"); err != nil {
		return nil, fmt.Errorf("error setting busy timeout: %w", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	return db, nil
}

// tableForDate returns the partition table name for a given date
func tableForDate(t time.Time) string {
	return fmt.Sprintf("matches_%s", t.Format("2006_01_02"))
}

// CreatePartitionTable creates a single partition table for a specific date
func CreatePartitionTable(tableName string) error {
	schema := fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cert_id TEXT NOT NULL,
    keyword TEXT NOT NULL,
    matched_rule TEXT DEFAULT '',
    priority TEXT DEFAULT 'medium',
    domains TEXT NOT NULL,
    tbs_sha256 TEXT,
    cert_sha256 TEXT,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(cert_id, keyword)
);
CREATE INDEX IF NOT EXISTS %s_idx_timestamp ON %s(timestamp);
CREATE INDEX IF NOT EXISTS %s_idx_keyword ON %s(keyword);
CREATE INDEX IF NOT EXISTS %s_idx_priority ON %s(priority);
`, tableName, tableName, tableName, tableName, tableName, tableName, tableName)

	if _, err := config.DB.Exec(schema); err != nil {
		return fmt.Errorf("create table %s: %w", tableName, err)
	}
	return nil
}

// CreatePartitionTables creates partition table for today (called at startup)
func CreatePartitionTables() error {
	// Create table for today
	today := time.Now()
	tableName := tableForDate(today)
	return CreatePartitionTable(tableName)
}

// GetExistingPartitionTables returns all existing matches_* tables
func GetExistingPartitionTables() ([]string, error) {
	rows, err := config.DB.Query("SELECT name FROM sqlite_master WHERE type='table' AND name LIKE 'matches_%'")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		tables = append(tables, name)
	}
	return tables, nil
}

// CleanupOldPartitions removes partition tables older than retention period
func CleanupOldPartitions() error {
	tables, err := GetExistingPartitionTables()
	if err != nil {
		return fmt.Errorf("get existing tables: %w", err)
	}

	cutoffDate := time.Now().AddDate(0, 0, -config.PartitionRetentionDays)
	deletedCount := 0

	for _, tbl := range tables {
		// Extract date from table name (format: matches_YYYY_MM_DD)
		if !strings.HasPrefix(tbl, "matches_") {
			continue
		}

		dateStr := strings.TrimPrefix(tbl, "matches_")
		tableDate, err := time.Parse("2006_01_02", dateStr)
		if err != nil {
			log.Printf("Warning: could not parse date from table %s: %v", tbl, err)
			continue
		}

		// Delete if older than cutoff
		if tableDate.Before(cutoffDate) {
			if _, err := config.DB.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", tbl)); err != nil {
				log.Printf("Warning: could not drop old table %s: %v", tbl, err)
				continue
			}
			log.Printf("Deleted old partition table: %s (date: %s)", tbl, tableDate.Format("2006-01-02"))
			deletedCount++
		}
	}

	if deletedCount > 0 {
		log.Printf("Cleaned up %d old partition tables (retention: %d days)", deletedCount, config.PartitionRetentionDays)
	}

	return nil
}

// MigrateAddRuleFields adds rule-related columns to existing tables
func MigrateAddRuleFields() error {
	tables, err := GetExistingPartitionTables()
	if err != nil {
		log.Printf("Warning: could not get existing tables for migration: %v", err)
		return nil
	}

	for _, tbl := range tables {
		// Try to add matched_rule column
		_, err := config.DB.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN matched_rule TEXT DEFAULT ''", tbl))
		if err != nil && !strings.Contains(err.Error(), "duplicate column") {
			log.Printf("Warning: could not add matched_rule to %s: %v", tbl, err)
		}

		// Try to add priority column
		_, err = config.DB.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN priority TEXT DEFAULT 'medium'", tbl))
		if err != nil && !strings.Contains(err.Error(), "duplicate column") {
			log.Printf("Warning: could not add priority to %s: %v", tbl, err)
		}

		// Add index on priority
		_, err = config.DB.Exec(fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s_idx_priority ON %s(priority)", tbl, tbl))
		if err != nil {
			log.Printf("Warning: could not create priority index on %s: %v", tbl, err)
		}
	}
	return nil
}

// StoreBatch stores matches in batches, partitioned by date
func StoreBatch(matches []models.Match) error {
	if len(matches) == 0 {
		return nil
	}

	// Group matches by their target date-based table
	buckets := make(map[string][]models.Match)
	for _, m := range matches {
		tbl := tableForDate(m.Timestamp)
		buckets[tbl] = append(buckets[tbl], m)
	}

	// Ensure tables exist and store
	for tbl, ms := range buckets {
		// Create partition table if it doesn't exist
		if err := CreatePartitionTable(tbl); err != nil {
			log.Printf("Warning: could not create partition table %s: %v", tbl, err)
			continue
		}

		// Store in this partition
		if err := storeSingleTable(tbl, ms); err != nil {
			return err
		}
	}
	return nil
}

// storeSingleTable stores matches in a single table within a transaction
func storeSingleTable(table string, matches []models.Match) error {
	tx, err := config.DB.Begin()
	if err != nil {
		return err
	}

	stmt, err := tx.Prepare(fmt.Sprintf(`INSERT OR IGNORE INTO %s (cert_id, keyword, matched_rule, priority, domains, tbs_sha256, cert_sha256, timestamp) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, table))
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()

	for _, m := range matches {
		domainsJSON, _ := json.Marshal(m.Domains)
		if _, err := stmt.Exec(
			m.CertID,
			m.Keyword,
			m.MatchedRule,
			m.Priority,
			string(domainsJSON),
			m.TbsSha256,
			m.CertSha256,
			m.Timestamp,
		); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// StartWorkers starts background workers to batch and write matches to the database
func StartWorkers(n int, batchSize int, batchTimeout time.Duration) {
	for i := 0; i < n; i++ {
		go func(workerID int) {
			batch := make([]models.Match, 0, batchSize)
			timer := time.NewTimer(batchTimeout)
			defer timer.Stop()

			for {
				select {
				case m, ok := <-config.MatchChan:
					if !ok {
						if len(batch) > 0 {
							if err := StoreBatch(batch); err != nil {
								log.Printf("worker %d: db batch err on close: %v", workerID, err)
							}
						}
						return
					}
					batch = append(batch, m)
					AddToRecent(m)

					if len(batch) >= batchSize {
						if err := StoreBatch(batch); err != nil {
							log.Printf("worker %d: db batch err: %v", workerID, err)
						}
						batch = batch[:0]
						if !timer.Stop() {
							<-timer.C
						}
						timer.Reset(batchTimeout)
					}
				case <-timer.C:
					if len(batch) > 0 {
						if err := StoreBatch(batch); err != nil {
							log.Printf("worker %d: db batch err (timeout): %v", workerID, err)
						}
						batch = batch[:0]
					}
					timer.Reset(batchTimeout)
				}
			}
		}(i)
	}
}

// AddToRecent adds a match to the in-memory recent matches cache
func AddToRecent(m models.Match) {
	config.CacheMutex.Lock()
	defer config.CacheMutex.Unlock()
	config.RecentMatches = append(config.RecentMatches, m)
	if len(config.RecentMatches) > config.MaxRecentMatches {
		config.RecentMatches = config.RecentMatches[len(config.RecentMatches)-config.MaxRecentMatches:]
	}
}

// getTablesForDateRange returns table names for the date range
func getTablesForDateRange(since time.Time) []string {
	var tables []string
	now := time.Now()

	// Generate table names for each day in range
	for d := since; d.Before(now) || d.Equal(now); d = d.AddDate(0, 0, 1) {
		tables = append(tables, tableForDate(d))
	}

	// Always include today's table
	todayTable := tableForDate(now)
	if len(tables) == 0 || tables[len(tables)-1] != todayTable {
		tables = append(tables, todayTable)
	}

	return tables
}

// GetRecent retrieves matches from the database since a given time
func GetRecent(since time.Time) ([]models.Match, error) {
	all := make([]models.Match, 0, 256)

	// Get relevant partition tables for the date range
	tables := getTablesForDateRange(since)

	// Fetch from relevant partition tables
	for _, tbl := range tables {
		rows, err := config.DB.Query(
			fmt.Sprintf(`SELECT cert_id, keyword, COALESCE(matched_rule, ''), COALESCE(priority, 'medium'), domains, tbs_sha256, cert_sha256, timestamp FROM %s WHERE timestamp >= ?`, tbl),
			since,
		)
		if err != nil {
			// Table might not exist yet, skip it
			continue
		}
		for rows.Next() {
			var m models.Match
			var domainsJSON string
			var ts string
			if err := rows.Scan(&m.CertID, &m.Keyword, &m.MatchedRule, &m.Priority, &domainsJSON, &m.TbsSha256, &m.CertSha256, &ts); err != nil {
				continue
			}
			_ = json.Unmarshal([]byte(domainsJSON), &m.Domains)

			// Try multiple timestamp formats
			parsed, err := time.Parse("2006-01-02 15:04:05", ts)
			if err != nil {
				parsed, err = time.Parse(time.RFC3339, ts)
			}
			if err != nil {
				parsed, err = time.Parse("2006-01-02T15:04:05Z", ts)
			}
			if err != nil {
				// If all parsing fails, use current time
				parsed = time.Now()
			}
			m.Timestamp = parsed
			all = append(all, m)
		}
		rows.Close()
	}

	return all, nil
}

// GetRecentPaginated retrieves matches with pagination support using UNION ALL for efficiency
func GetRecentPaginated(since time.Time, limit, offset int) ([]models.Match, int, error) {
	// Get relevant partition tables for the date range
	tables := getTablesForDateRange(since)

	// Filter to only existing tables
	var existingTables []string
	for _, tbl := range tables {
		// Check if table exists by querying it
		var count int
		query := fmt.Sprintf("SELECT COUNT(*) FROM %s LIMIT 1", tbl)
		if err := config.DB.QueryRow(query).Scan(&count); err != nil {
			// Table doesn't exist, skip
			continue
		}
		existingTables = append(existingTables, tbl)
	}

	// If no existing tables, return empty results
	if len(existingTables) == 0 {
		return []models.Match{}, 0, nil
	}

	// Build UNION ALL query for existing partition tables only
	var dataQueries []string

	for _, tbl := range existingTables {
		dataQueries = append(dataQueries, fmt.Sprintf(
			"SELECT cert_id, keyword, COALESCE(matched_rule, '') as matched_rule, COALESCE(priority, 'medium') as priority, domains, tbs_sha256, cert_sha256, MAX(timestamp) as timestamp FROM %s WHERE timestamp >= ? GROUP BY cert_id, keyword, matched_rule, priority, domains, tbs_sha256, cert_sha256",
			tbl,
		))
	}

	// Get total unique certificates count by summing from all partitions
	totalCount := 0
	for _, tbl := range existingTables {
		var count int
		query := fmt.Sprintf("SELECT COUNT(DISTINCT cert_id) FROM %s WHERE timestamp >= ?", tbl)
		if err := config.DB.QueryRow(query, since).Scan(&count); err != nil {
			// Table might not exist, skip
			continue
		}
		totalCount += count
	}

	// Build unified query with LIMIT and OFFSET - order by cert_id as secondary for consistency
	unionQuery := fmt.Sprintf(
		"SELECT cert_id, keyword, matched_rule, priority, domains, tbs_sha256, cert_sha256, timestamp FROM (%s) ORDER BY timestamp DESC, cert_id ASC LIMIT ? OFFSET ?",
		strings.Join(dataQueries, " UNION ALL "),
	)

	// Prepare arguments: one 'since' for each table, then limit and offset
	args := make([]interface{}, len(existingTables)+2)
	for i := range existingTables {
		args[i] = since
	}
	args[len(existingTables)] = limit
	args[len(existingTables)+1] = offset

	rows, err := config.DB.Query(unionQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query paginated: %w", err)
	}
	defer rows.Close()

	all := make([]models.Match, 0, limit)
	for rows.Next() {
		var m models.Match
		var domainsJSON string
		var ts string
		if err := rows.Scan(&m.CertID, &m.Keyword, &m.MatchedRule, &m.Priority, &domainsJSON, &m.TbsSha256, &m.CertSha256, &ts); err != nil {
			continue
		}
		_ = json.Unmarshal([]byte(domainsJSON), &m.Domains)

		// Try multiple timestamp formats
		parsed, err := time.Parse("2006-01-02 15:04:05", ts)
		if err != nil {
			parsed, err = time.Parse(time.RFC3339, ts)
		}
		if err != nil {
			parsed, err = time.Parse("2006-01-02T15:04:05Z", ts)
		}
		if err != nil {
			parsed = time.Now()
		}
		m.Timestamp = parsed
		all = append(all, m)
	}

	return all, totalCount, nil
}

// GetMatchesByCertIDs retrieves all matches for specific certificate IDs
func GetMatchesByCertIDs(certIDs []string) ([]models.Match, error) {
	if len(certIDs) == 0 {
		return nil, nil
	}

	all := make([]models.Match, 0, len(certIDs)*2) // Estimate 2 keywords per cert

	// Build placeholders for IN clause
	placeholders := make([]string, len(certIDs))
	args := make([]interface{}, len(certIDs))
	for i, id := range certIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	inClause := strings.Join(placeholders, ",")

	// Get all existing partition tables dynamically
	tables, err := GetExistingPartitionTables()
	if err != nil {
		log.Printf("failed to get partition tables: %v", err)
		return all, nil
	}

	// Query each partition table
	for _, tbl := range tables {
		query := fmt.Sprintf(
			`SELECT cert_id, keyword, COALESCE(matched_rule, ''), COALESCE(priority, 'medium'), domains, tbs_sha256, cert_sha256, timestamp FROM %s WHERE cert_id IN (%s)`,
			tbl, inClause,
		)

		rows, err := config.DB.Query(query, args...)
		if err != nil {
			log.Printf("query cert_ids from %s: %v", tbl, err)
			continue
		}

		for rows.Next() {
			var m models.Match
			var domainsJSON string
			var ts string
			if err := rows.Scan(&m.CertID, &m.Keyword, &m.MatchedRule, &m.Priority, &domainsJSON, &m.TbsSha256, &m.CertSha256, &ts); err != nil {
				continue
			}
			_ = json.Unmarshal([]byte(domainsJSON), &m.Domains)

			// Try multiple timestamp formats
			parsed, err := time.Parse("2006-01-02 15:04:05", ts)
			if err != nil {
				parsed, err = time.Parse(time.RFC3339, ts)
			}
			if err != nil {
				parsed, err = time.Parse("2006-01-02T15:04:05Z", ts)
			}
			if err != nil {
				parsed = time.Now()
			}
			m.Timestamp = parsed
			all = append(all, m)
		}
		rows.Close()
	}

	return all, nil
}
