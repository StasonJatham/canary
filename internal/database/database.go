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

// CreatePartitionTables creates all partitioned tables
func CreatePartitionTables() error {
	for _, tbl := range config.PartitionTables {
		schema := fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cert_id TEXT NOT NULL,
    keyword TEXT NOT NULL,
    domains TEXT NOT NULL,
    tbs_sha256 TEXT,
    cert_sha256 TEXT,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(cert_id, keyword)
);
CREATE INDEX IF NOT EXISTS %s_idx_timestamp ON %s(timestamp);
CREATE INDEX IF NOT EXISTS %s_idx_keyword ON %s(keyword);
`, tbl, tbl, tbl, tbl, tbl)
		if _, err := config.DB.Exec(schema); err != nil {
			return fmt.Errorf("create table %s: %w", tbl, err)
		}
	}
	return nil
}

// tableForKeyword determines which partition table to use based on the keyword's first letter
func tableForKeyword(kw string) string {
	if kw == "" {
		return "matches_other"
	}
	first := kw[0]
	if first >= 'a' && first <= 'z' {
		return "matches_" + string(first)
	}
	if first >= 'A' && first <= 'Z' {
		return "matches_" + strings.ToLower(string(first))
	}
	return "matches_other"
}

// StoreBatch stores matches in batches, partitioned by keyword
func StoreBatch(matches []models.Match) error {
	if len(matches) == 0 {
		return nil
	}

	// Group matches by their target table
	buckets := make(map[string][]models.Match)
	for _, m := range matches {
		tbl := tableForKeyword(m.Keyword)
		buckets[tbl] = append(buckets[tbl], m)
	}

	// Store each table in its own transaction
	for tbl, ms := range buckets {
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

	stmt, err := tx.Prepare(fmt.Sprintf(`INSERT OR IGNORE INTO %s (cert_id, keyword, domains, tbs_sha256, cert_sha256, timestamp) VALUES (?, ?, ?, ?, ?, ?)`, table))
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

// GetRecent retrieves matches from the database since a given time
func GetRecent(since time.Time) ([]models.Match, error) {
	all := make([]models.Match, 0, 256)

	// Fetch from all partition tables
	for _, tbl := range config.PartitionTables {
		rows, err := config.DB.Query(
			fmt.Sprintf(`SELECT cert_id, keyword, domains, tbs_sha256, cert_sha256, timestamp FROM %s WHERE timestamp >= ?`, tbl),
			since,
		)
		if err != nil {
			log.Printf("query recent from %s: %v", tbl, err)
			continue
		}
		for rows.Next() {
			var m models.Match
			var domainsJSON string
			var ts string
			if err := rows.Scan(&m.CertID, &m.Keyword, &domainsJSON, &m.TbsSha256, &m.CertSha256, &ts); err != nil {
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
	// Build UNION ALL query for all partition tables
	var countQueries []string
	var dataQueries []string

	for _, tbl := range config.PartitionTables {
		countQueries = append(countQueries, fmt.Sprintf("SELECT COUNT(DISTINCT cert_id) FROM %s WHERE timestamp >= ?", tbl))
		dataQueries = append(dataQueries, fmt.Sprintf(
			"SELECT cert_id, keyword, domains, tbs_sha256, cert_sha256, MAX(timestamp) as timestamp FROM %s WHERE timestamp >= ? GROUP BY cert_id",
			tbl,
		))
	}

	// Get total unique certificates count by summing from all partitions
	totalCount := 0
	for _, tbl := range config.PartitionTables {
		var count int
		query := fmt.Sprintf("SELECT COUNT(DISTINCT cert_id) FROM %s WHERE timestamp >= ?", tbl)
		if err := config.DB.QueryRow(query, since).Scan(&count); err != nil {
			log.Printf("Error counting from %s: %v", tbl, err)
			continue
		}
		totalCount += count
	}

	// Build unified query with LIMIT and OFFSET - order by cert_id as secondary for consistency
	unionQuery := fmt.Sprintf(
		"SELECT cert_id, keyword, domains, tbs_sha256, cert_sha256, timestamp FROM (%s) ORDER BY timestamp DESC, cert_id ASC LIMIT ? OFFSET ?",
		strings.Join(dataQueries, " UNION ALL "),
	)

	// Prepare arguments: one 'since' for each table, then limit and offset
	args := make([]interface{}, len(config.PartitionTables)+2)
	for i := range config.PartitionTables {
		args[i] = since
	}
	args[len(config.PartitionTables)] = limit
	args[len(config.PartitionTables)+1] = offset

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
		if err := rows.Scan(&m.CertID, &m.Keyword, &domainsJSON, &m.TbsSha256, &m.CertSha256, &ts); err != nil {
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

	// Query each partition table
	for _, tbl := range config.PartitionTables {
		query := fmt.Sprintf(
			`SELECT cert_id, keyword, domains, tbs_sha256, cert_sha256, timestamp FROM %s WHERE cert_id IN (%s)`,
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
			if err := rows.Scan(&m.CertID, &m.Keyword, &domainsJSON, &m.TbsSha256, &m.CertSha256, &ts); err != nil {
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
