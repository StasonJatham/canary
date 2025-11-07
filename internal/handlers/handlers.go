package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"canary/internal/config"
	"canary/internal/database"
	"canary/internal/matcher"
	"canary/internal/models"
)

// Hook processes incoming Certspotter webhook events
func Hook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()

	// Read body once for both debug and parsing
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	if config.Debug {
		log.Printf("[DEBUG] Raw webhook body (%d bytes): %s", len(bodyBytes), string(bodyBytes))
	}

	// Parse JSON
	var event models.CertspotterEvent
	if err := json.Unmarshal(bodyBytes, &event); err != nil {
		if config.Debug {
			log.Printf("[DEBUG] Failed to decode JSON: %v", err)
		}
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if config.Debug {
		log.Printf("[DEBUG] Parsed webhook | ID: %s | DNS Names: %v", event.ID, event.Issuance.DNSNames)
	}

	allDomains := make([]string, 0, len(event.Issuance.DNSNames)+len(event.Endpoints))
	allDomains = append(allDomains, event.Issuance.DNSNames...)
	for _, ep := range event.Endpoints {
		if ep.DNSName != "" {
			allDomains = append(allDomains, ep.DNSName)
		}
	}

	matchedKeywords := matcher.Find(allDomains)
	now := time.Now()

	if len(matchedKeywords) > 0 {
		config.TotalCerts.Add(1)
		log.Printf("Match found: cert_id=%s keywords=%v domains=%v", event.ID, matchedKeywords, allDomains)
	}

	for _, kw := range matchedKeywords {
		config.TotalMatches.Add(1)
		m := models.Match{
			CertID:     event.ID,
			Domains:    allDomains,
			Keyword:    kw,
			Timestamp:  now,
			TbsSha256:  event.Issuance.TbsSha256,
			CertSha256: event.Issuance.CertSha256,
		}

		select {
		case config.MatchChan <- m:
		default:
			log.Printf("match channel full, dropping match cert_id=%s keyword=%s", m.CertID, m.Keyword)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"matches": len(matchedKeywords),
	})
}

// GetMatches returns recent matches from the in-memory cache
func GetMatches(w http.ResponseWriter, r *http.Request) {
	config.CacheMutex.RLock()
	defer config.CacheMutex.RUnlock()

	// Transform matches into UI-friendly format
	type UIMatch struct {
		DNSNames       []string  `json:"dns_names"`
		MatchedDomains []string  `json:"matched_domains"`
		TbsSha256      string    `json:"tbs_sha256"`
		CertSha256     string    `json:"cert_sha256"`
		DetectedAt     time.Time `json:"detected_at"`
	}

	// Group matches by cert_id
	matchMap := make(map[string]*UIMatch)
	for _, match := range config.RecentMatches {
		if existing, ok := matchMap[match.CertID]; ok {
			// Add keyword to matched domains if not already present
			found := false
			for _, kw := range existing.MatchedDomains {
				if kw == match.Keyword {
					found = true
					break
				}
			}
			if !found {
				existing.MatchedDomains = append(existing.MatchedDomains, match.Keyword)
			}
		} else {
			matchMap[match.CertID] = &UIMatch{
				DNSNames:       match.Domains,
				MatchedDomains: []string{match.Keyword},
				TbsSha256:      match.TbsSha256,
				CertSha256:     match.CertSha256,
				DetectedAt:     match.Timestamp,
			}
		}
	}

	// Convert map to slice
	matches := make([]UIMatch, 0, len(matchMap))
	for _, match := range matchMap {
		matches = append(matches, *match)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(matches)
}

// ClearMatches clears the in-memory matches cache
func ClearMatches(w http.ResponseWriter, r *http.Request) {
	config.CacheMutex.Lock()
	config.RecentMatches = nil
	config.CacheMutex.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "cleared"})
}

// AddKeywords adds keywords via API and reloads the matcher
func AddKeywords(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()

	var req struct {
		Keywords []string `json:"keywords"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if len(req.Keywords) == 0 {
		http.Error(w, "no keywords", http.StatusBadRequest)
		return
	}

	// Append keywords to file
	if err := matcher.AppendKeywords(config.KeywordsFile, req.Keywords); err != nil {
		http.Error(w, "failed to append keywords", http.StatusInternalServerError)
		return
	}

	// Reload keywords
	if err := matcher.Load(config.KeywordsFile); err != nil {
		http.Error(w, "failed to reload keywords", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":   "keywords added and reloaded",
		"countAdd": len(req.Keywords),
	})
}

// ReloadKeywords reloads keywords from the file
func ReloadKeywords(w http.ResponseWriter, r *http.Request) {
	err := matcher.Load(config.KeywordsFile)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		log.Printf("Failed to reload keywords: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	st := matcher.GetCurrent()
	cnt := 0
	if st != nil {
		cnt = len(st.Keywords)
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "keywords reloaded", "count": fmt.Sprintf("%d", cnt)})
}

// GetRecentFromDB retrieves matches from the last X minutes: /matches/recent?minutes=5&limit=50&offset=0
func GetRecentFromDB(w http.ResponseWriter, r *http.Request) {
	minStr := r.URL.Query().Get("minutes")
	if minStr == "" {
		minStr = "5"
	}
	minutes, err := time.ParseDuration(minStr + "m")
	if err != nil {
		http.Error(w, "bad minutes value", http.StatusBadRequest)
		return
	}
	since := time.Now().Add(-minutes)

	// Parse pagination parameters
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit := 50 // default
	offset := 0

	if limitStr != "" {
		fmt.Sscanf(limitStr, "%d", &limit)
	}
	if offsetStr != "" {
		fmt.Sscanf(offsetStr, "%d", &offset)
	}

	// Use pagination if limit/offset provided
	usePagination := r.URL.Query().Has("limit") || r.URL.Query().Has("offset")

	var all []models.Match
	var totalCount int

	if usePagination {
		all, totalCount, err = database.GetRecentPaginated(since, limit, offset)
	} else {
		all, err = database.GetRecent(since)
		totalCount = len(all)
	}

	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	// Transform to UI-friendly format
	type UIMatch struct {
		DNSNames       []string  `json:"dns_names"`
		MatchedDomains []string  `json:"matched_domains"`
		TbsSha256      string    `json:"tbs_sha256"`
		CertSha256     string    `json:"cert_sha256"`
		DetectedAt     time.Time `json:"detected_at"`
	}

	// When using pagination, we need to fetch all keywords for each cert_id
	var matches []UIMatch

	if usePagination {
		// Get unique cert_ids from paginated results
		certIDs := make([]string, 0, len(all))
		seenCerts := make(map[string]bool)
		for _, match := range all {
			if !seenCerts[match.CertID] {
				certIDs = append(certIDs, match.CertID)
				seenCerts[match.CertID] = true
			}
		}

		// Fetch all keywords for these cert_ids
		if len(certIDs) > 0 {
			allMatchesForCerts, err := database.GetMatchesByCertIDs(certIDs)
			if err != nil {
				log.Printf("Error fetching all keywords: %v", err)
				allMatchesForCerts = all // Fallback to paginated results
			}
			all = allMatchesForCerts
		}
	}

	// Group matches by cert_id
	matchMap := make(map[string]*UIMatch)
	for _, match := range all {
		if existing, ok := matchMap[match.CertID]; ok {
			// Add keyword to matched domains if not already present
			found := false
			for _, kw := range existing.MatchedDomains {
				if kw == match.Keyword {
					found = true
					break
				}
			}
			if !found {
				existing.MatchedDomains = append(existing.MatchedDomains, match.Keyword)
			}
		} else {
			matchMap[match.CertID] = &UIMatch{
				DNSNames:       match.Domains,
				MatchedDomains: []string{match.Keyword},
				TbsSha256:      match.TbsSha256,
				CertSha256:     match.CertSha256,
				DetectedAt:     match.Timestamp,
			}
		}
	}

	// Convert map to slice
	matches = make([]UIMatch, 0, len(matchMap))
	for _, match := range matchMap {
		matches = append(matches, *match)
	}

	w.Header().Set("Content-Type", "application/json")
	response := map[string]any{
		"count":   len(matches),
		"matches": matches,
	}

	if usePagination {
		response["total"] = totalCount
		response["limit"] = limit
		response["offset"] = offset
		response["has_more"] = (offset + len(matches)) < totalCount
	}

	_ = json.NewEncoder(w).Encode(response)
}

// Metrics returns system metrics
func Metrics(w http.ResponseWriter, r *http.Request) {
	queueLen := 0
	if config.MatchChan != nil {
		queueLen = len(config.MatchChan)
	}

	st := matcher.GetCurrent()
	keywordCount := 0
	if st != nil {
		keywordCount = len(st.Keywords)
	}

	uptime := time.Since(config.StartTime)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"queue_len":                queueLen,
		"total_matches":            config.TotalMatches.Load(),
		"total_certificates_checked": config.TotalCerts.Load(),
		"watched_domains":          keywordCount,
		"uptime_seconds":           int(uptime.Seconds()),
		"recent_matches":           len(config.RecentMatches),
	})
}

// Health checks system health
func Health(w http.ResponseWriter, r *http.Request) {
	// Check if database is accessible
	if err := config.DB.Ping(); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "unhealthy",
			"error":  "database unreachable",
		})
		return
	}

	// Check if matcher is loaded
	st := matcher.GetCurrent()
	if st == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "unhealthy",
			"error":  "matcher not loaded",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":   "healthy",
		"keywords": len(st.Keywords),
		"uptime":   int(time.Since(config.StartTime).Seconds()),
	})
}

// ServeUI serves the web UI
func ServeUI(w http.ResponseWriter, r *http.Request) {
	htmlPath := "web/index.html"

	// Try to read the file
	content, err := os.ReadFile(htmlPath)
	if err != nil {
		// If file doesn't exist, return a basic fallback
		w.WriteHeader(http.StatusNotFound)
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`
<!DOCTYPE html>
<html><head><title>Canary UI</title></head><body>
<h1>UI file not found</h1>
<p>Please ensure web/index.html exists in the project root.</p>
</body></html>
		`))
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(content)
}

// ServeAPIDocs serves the API documentation page
func ServeAPIDocs(w http.ResponseWriter, r *http.Request) {
	htmlPath := "web/docs.html"

	content, err := os.ReadFile(htmlPath)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`
<!DOCTYPE html>
<html><head><title>API Docs Not Found</title></head><body>
<h1>API Documentation not found</h1>
<p>Please ensure web/docs.html exists in the project root.</p>
</body></html>
		`))
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(content)
}

// ServeOpenAPISpec serves the OpenAPI specification YAML
func ServeOpenAPISpec(w http.ResponseWriter, r *http.Request) {
	yamlPath := "web/openapi.yaml"

	content, err := os.ReadFile(yamlPath)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("OpenAPI spec not found"))
		return
	}

	w.Header().Set("Content-Type", "application/x-yaml; charset=utf-8")
	w.Write(content)
}
