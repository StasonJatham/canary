package models

import (
	"time"

	ac "github.com/anknown/ahocorasick"
)

// Match represents a keyword match found in a certificate's domains
type Match struct {
	CertID      string    `json:"cert_id"`
	Domains     []string  `json:"domains"`
	Keyword     string    `json:"keyword"`
	MatchedRule string    `json:"matched_rule"` // Rule that triggered this match
	Priority    string    `json:"priority"`      // Priority level: critical, high, medium, low
	Timestamp   time.Time `json:"timestamp"`
	TbsSha256   string    `json:"tbs_sha256"`
	CertSha256  string    `json:"cert_sha256"`
}

// CertspotterEvent represents the webhook payload from Certspotter
type CertspotterEvent struct {
	ID       string `json:"id"`
	Issuance struct {
		DNSNames   []string `json:"dns_names"`
		TbsSha256  string   `json:"tbs_sha256"`
		CertSha256 string   `json:"cert_sha256"`
	} `json:"issuance"`
	Endpoints []struct {
		DNSName string `json:"dns_name"`
	} `json:"endpoints"`
}

// MatcherState holds the Aho-Corasick automaton and keywords list
type MatcherState struct {
	Machine  ac.Machine
	Keywords []string
}

// PerformanceMetrics tracks system performance statistics
type PerformanceMetrics struct {
	Timestamp           time.Time `json:"timestamp"`
	CertsPerMinute      int       `json:"certs_per_minute"`
	MatchesPerMinute    int       `json:"matches_per_minute"`
	AvgMatchTimeUs      int64     `json:"avg_match_time_us"` // microseconds
	CPUPercent          float64   `json:"cpu_percent"`
	MemoryUsedMB        float64   `json:"memory_used_mb"`
	MemoryTotalMB       float64   `json:"memory_total_mb"`
	GoroutineCount      int       `json:"goroutine_count"`
	RulesEvaluated      int       `json:"rules_evaluated"`
	KeywordsInAC        int       `json:"keywords_in_ac"`
	DatabaseSizeMB      float64   `json:"database_size_mb"`
}
