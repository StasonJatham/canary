package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"canary/internal/auth"
	"canary/internal/config"
	"canary/internal/database"
	"canary/internal/models"
	"canary/internal/rules"

	"golang.org/x/net/idna"
	"gopkg.in/yaml.v3"
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

	// Expand domains with Punycode/Unicode variants to handle homoglyphs
	// This ensures that a rule for "münster" matches "xn--mnster-bxa" and vice versa
	expandedDomains := make([]string, 0, len(allDomains)*2)
	seen := make(map[string]bool)

	for _, domain := range allDomains {
		if !seen[domain] {
			expandedDomains = append(expandedDomains, domain)
			seen[domain] = true
		}

		// Try ToASCII (Punycode)
		if ascii, err := idna.ToASCII(domain); err == nil && ascii != domain {
			if !seen[ascii] {
				expandedDomains = append(expandedDomains, ascii)
				seen[ascii] = true
			}
		}

		// Try ToUnicode
		if unicode, err := idna.ToUnicode(domain); err == nil && unicode != domain {
			if !seen[unicode] {
				expandedDomains = append(expandedDomains, unicode)
				seen[unicode] = true
			}
		}
	}
	allDomains = expandedDomains

	// Use rule engine's Aho-Corasick directly (no separate keywords.txt)
	engineVal := config.RuleEngine.Load()
	if engineVal == nil {
		// No rules loaded - skip processing
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  "ok",
			"matches": 0,
		})
		return
	}

	engine := engineVal.(*rules.Engine)

	// Track performance
	startTime := time.Now()
	matchedKeywords := engine.Find(allDomains)
	now := time.Now()
	matchDuration := time.Since(startTime).Microseconds()

	// Record cert processed
	if perfVal := config.PerfCollector.Load(); perfVal != nil {
		if perf, ok := perfVal.(interface{ RecordCertProcessed() }); ok {
			perf.RecordCertProcessed()
		}
	}

	if len(matchedKeywords) > 0 {
		config.TotalCerts.Add(1)

		// Evaluate rules (stops after first match for performance)
		// Pass both keywords and domains so NOT clauses can be properly evaluated
		ruleMatch := engine.Evaluate(matchedKeywords, allDomains)

		if ruleMatch != nil {
			// Rule matched - create single match with rule info
			config.TotalMatches.Add(1)

			// Record match performance
			if perfVal := config.PerfCollector.Load(); perfVal != nil {
				if perf, ok := perfVal.(interface{ RecordMatch(int64) }); ok {
					perf.RecordMatch(matchDuration)
				}
			}

			m := models.Match{
				CertID:      event.ID,
				Domains:     allDomains,
				Keyword:     strings.Join(matchedKeywords, ","),
				MatchedRule: ruleMatch.RuleName,
				Priority:    string(ruleMatch.Priority),
				Timestamp:   now,
				TbsSha256:   event.Issuance.TbsSha256,
				CertSha256:  event.Issuance.CertSha256,
			}

			log.Printf("Rule match: cert_id=%s rule=%s priority=%s keywords=%v domains=%v",
				event.ID, ruleMatch.RuleName, ruleMatch.Priority, matchedKeywords, allDomains)

			select {
			case config.MatchChan <- m:
			default:
				log.Printf("match channel full, dropping match cert_id=%s rule=%s", m.CertID, m.MatchedRule)
			}
		}
		// No else block - only log and store rule-based matches
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
		MatchedRule    string    `json:"matched_rule"`
		Priority       string    `json:"priority"`
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
				MatchedRule:    match.MatchedRule,
				Priority:       match.Priority,
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
		log.Printf("Database error in GetRecentFromDB: %v", err)
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	// Transform to UI-friendly format
	type UIMatch struct {
		DNSNames       []string  `json:"dns_names"`
		MatchedDomains []string  `json:"matched_domains"`
		MatchedRule    string    `json:"matched_rule"`
		Priority       string    `json:"priority"`
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
				MatchedRule:    match.MatchedRule,
				Priority:       match.Priority,
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

	// Get keyword count from rules engine
	keywordCount := 0
	rulesCount := 0
	engineVal := config.RuleEngine.Load()
	if engineVal != nil {
		engine := engineVal.(*rules.Engine)
		keywordCount = len(engine.Keywords)
		rulesCount = len(engine.Rules)
	}

	uptime := time.Since(config.StartTime)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"queue_len":      queueLen,
		"total_matches":  config.TotalMatches.Load(),
		"total_certs":    config.TotalCerts.Load(),
		"watched_domains": keywordCount,
		"rules_count":    rulesCount,
		"uptime_seconds": int(uptime.Seconds()),
		"recent_matches": len(config.RecentMatches),
	})
}

// GetConfig returns public configuration info
func GetConfig(w http.ResponseWriter, r *http.Request) {
	// Check if user is authenticated by validating session cookie
	authenticated := false
	csrfToken := ""
	cookie, err := r.Cookie("canary_session")
	if err == nil {
		// Try to validate session
		_, err := auth.GetSessionByToken(config.DB, cookie.Value)
		authenticated = (err == nil)

		// Get CSRF token if authenticated
		if authenticated {
			token, err := auth.GetOrCreateCSRFToken(cookie.Value)
			if err == nil {
				csrfToken = token
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"public_dashboard": config.PublicDashboard,
		"authenticated":    authenticated,
		"csrf_token":       csrfToken,
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

	// Check if rules engine is loaded
	engineVal := config.RuleEngine.Load()
	if engineVal == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "unhealthy",
			"error":  "rules engine not loaded",
		})
		return
	}

	engine := engineVal.(*rules.Engine)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":   "healthy",
		"keywords": len(engine.Keywords),
		"rules":    len(engine.Rules),
		"uptime":   int(time.Since(config.StartTime).Seconds()),
	})
}

// ServeUI serves the web UI and static files from dist/ directory (minified)
func ServeUI(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Redirect root to index (which is dashboard)
	if path == "/" {
		path = "/index.html"
	}

	// Try to serve from dist first (minified), fallback to web
	filePath := "dist" + path
	content, err := os.ReadFile(filePath)
	if err != nil {
		// Fallback to web directory if dist doesn't exist
		filePath = "web" + path
		content, err = os.ReadFile(filePath)
		if err != nil {
			http.NotFound(w, r)
			return
		}
	}

	// Set content type based on file extension
	contentType := "application/octet-stream"
	if strings.HasSuffix(path, ".html") {
		contentType = "text/html; charset=utf-8"
	} else if strings.HasSuffix(path, ".png") {
		contentType = "image/png"
	} else if strings.HasSuffix(path, ".jpg") || strings.HasSuffix(path, ".jpeg") {
		contentType = "image/jpeg"
	} else if strings.HasSuffix(path, ".svg") {
		contentType = "image/svg+xml"
	} else if strings.HasSuffix(path, ".css") {
		contentType = "text/css"
	} else if strings.HasSuffix(path, ".js") {
		contentType = "application/javascript"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Add cache headers for static assets (not HTML)
	if !strings.HasSuffix(path, ".html") {
		w.Header().Set("Cache-Control", "public, max-age=31536000")
	}

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

// ReloadRules reloads rules from the YAML file
func ReloadRules(w http.ResponseWriter, r *http.Request) {
	engine, err := rules.LoadRules(config.RulesFile)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		log.Printf("Failed to reload rules: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":  err.Error(),
			"status": "failed",
		})
		return
	}

	config.RuleEngine.Store(engine)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":        "rules reloaded",
		"rules_loaded":  len(engine.Rules),
		"enabled_rules": engine.GetEnabledRuleCount(),
	})
}

// GetRules returns all loaded rules
func GetRules(w http.ResponseWriter, r *http.Request) {
	engineVal := config.RuleEngine.Load()
	w.Header().Set("Content-Type", "application/json")

	if engineVal == nil {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"rules": []string{},
			"count": 0,
		})
		return
	}

	engine := engineVal.(*rules.Engine)

	type RuleInfo struct {
		Name     string `json:"name"`
		Keywords string `json:"keywords"`
		Priority string `json:"priority"`
		Enabled  bool   `json:"enabled"`
		Order    int    `json:"order"`
		Comment  string `json:"comment"`
	}

	ruleInfos := make([]RuleInfo, 0, len(engine.Rules))
	for _, rule := range engine.Rules {
		ruleInfos = append(ruleInfos, RuleInfo{
			Name:     rule.Name,
			Keywords: rule.Keywords,
			Priority: string(rule.Priority),
			Enabled:  rule.Enabled,
			Order:    rule.Order,
			Comment:  rule.Comment,
		})
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"rules": ruleInfos,
		"count": len(ruleInfos),
	})
}

// CreateRule adds a new rule to rules.yaml
func CreateRule(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request body
	var newRule rules.RuleConfig
	if err := json.NewDecoder(r.Body).Decode(&newRule); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Read existing rules file
	data, err := os.ReadFile(config.RulesFile)
	if err != nil {
		http.Error(w, "failed to read rules file", http.StatusInternalServerError)
		return
	}

	var ruleFile rules.RuleFile
	if err := yaml.Unmarshal(data, &ruleFile); err != nil {
		http.Error(w, "failed to parse rules file", http.StatusInternalServerError)
		return
	}

	// Check if rule name already exists
	for _, rule := range ruleFile.Rules {
		if rule.Name == newRule.Name {
			http.Error(w, "rule with this name already exists", http.StatusConflict)
			return
		}
	}

	// Add new rule
	ruleFile.Rules = append(ruleFile.Rules, newRule)

	// Write back to file
	yamlData, err := yaml.Marshal(ruleFile)
	if err != nil {
		http.Error(w, "failed to marshal rules", http.StatusInternalServerError)
		return
	}

	if err := os.WriteFile(config.RulesFile, yamlData, 0644); err != nil {
		http.Error(w, "failed to write rules file", http.StatusInternalServerError)
		return
	}

	// Reload rules engine
	engine, err := rules.LoadRules(config.RulesFile)
	if err != nil {
		http.Error(w, "failed to reload rules: "+err.Error(), http.StatusInternalServerError)
		return
	}
	config.RuleEngine.Store(engine)

	json.NewEncoder(w).Encode(map[string]string{
		"status":  "rule created",
		"message": "Rule created and loaded successfully",
	})
}

// UpdateRule modifies an existing rule in rules.yaml
func UpdateRule(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get rule name from URL path: /rules/update/{name}
	pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/rules/update/"), "/")
	if len(pathParts) == 0 || pathParts[0] == "" {
		http.Error(w, "rule name required", http.StatusBadRequest)
		return
	}
	ruleName := pathParts[0]

	// Parse request body
	var updatedRule rules.RuleConfig
	if err := json.NewDecoder(r.Body).Decode(&updatedRule); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Read existing rules file
	data, err := os.ReadFile(config.RulesFile)
	if err != nil {
		http.Error(w, "failed to read rules file", http.StatusInternalServerError)
		return
	}

	var ruleFile rules.RuleFile
	if err := yaml.Unmarshal(data, &ruleFile); err != nil {
		http.Error(w, "failed to parse rules file", http.StatusInternalServerError)
		return
	}

	// Find and update the rule
	found := false
	for i, rule := range ruleFile.Rules {
		if rule.Name == ruleName {
			ruleFile.Rules[i] = updatedRule
			found = true
			break
		}
	}

	if !found {
		http.Error(w, "rule not found", http.StatusNotFound)
		return
	}

	// Write back to file
	yamlData, err := yaml.Marshal(ruleFile)
	if err != nil {
		http.Error(w, "failed to marshal rules", http.StatusInternalServerError)
		return
	}

	if err := os.WriteFile(config.RulesFile, yamlData, 0644); err != nil {
		http.Error(w, "failed to write rules file", http.StatusInternalServerError)
		return
	}

	// Reload rules engine
	engine, err := rules.LoadRules(config.RulesFile)
	if err != nil {
		http.Error(w, "failed to reload rules: "+err.Error(), http.StatusInternalServerError)
		return
	}
	config.RuleEngine.Store(engine)

	json.NewEncoder(w).Encode(map[string]string{
		"status":  "rule updated",
		"message": "Rule updated and reloaded successfully",
	})
}

// DeleteRule removes a rule from rules.yaml
func DeleteRule(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get rule name from URL path: /rules/delete/{name}
	pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/rules/delete/"), "/")
	if len(pathParts) == 0 || pathParts[0] == "" {
		http.Error(w, "rule name required", http.StatusBadRequest)
		return
	}
	ruleName := pathParts[0]

	// Read existing rules file
	data, err := os.ReadFile(config.RulesFile)
	if err != nil {
		http.Error(w, "failed to read rules file", http.StatusInternalServerError)
		return
	}

	var ruleFile rules.RuleFile
	if err := yaml.Unmarshal(data, &ruleFile); err != nil {
		http.Error(w, "failed to parse rules file", http.StatusInternalServerError)
		return
	}

	// Find and remove the rule
	newRules := make([]rules.RuleConfig, 0)
	found := false
	for _, rule := range ruleFile.Rules {
		if rule.Name == ruleName {
			found = true
			continue // Skip this rule (delete it)
		}
		newRules = append(newRules, rule)
	}

	if !found {
		http.Error(w, "rule not found", http.StatusNotFound)
		return
	}

	ruleFile.Rules = newRules

	// Write back to file
	yamlData, err := yaml.Marshal(ruleFile)
	if err != nil {
		http.Error(w, "failed to marshal rules", http.StatusInternalServerError)
		return
	}

	if err := os.WriteFile(config.RulesFile, yamlData, 0644); err != nil {
		http.Error(w, "failed to write rules file", http.StatusInternalServerError)
		return
	}

	// Reload rules engine
	engine, err := rules.LoadRules(config.RulesFile)
	if err != nil {
		http.Error(w, "failed to reload rules: "+err.Error(), http.StatusInternalServerError)
		return
	}
	config.RuleEngine.Store(engine)

	json.NewEncoder(w).Encode(map[string]string{
		"status":  "rule deleted",
		"message": "Rule deleted and rules reloaded successfully",
	})
}

// ToggleRule enables or disables a rule
func ToggleRule(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get rule name from URL path: /rules/toggle/{name}
	pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/rules/toggle/"), "/")
	if len(pathParts) == 0 || pathParts[0] == "" {
		http.Error(w, "rule name required", http.StatusBadRequest)
		return
	}
	ruleName := pathParts[0]

	// Read existing rules file
	data, err := os.ReadFile(config.RulesFile)
	if err != nil {
		http.Error(w, "failed to read rules file", http.StatusInternalServerError)
		return
	}

	var ruleFile rules.RuleFile
	if err := yaml.Unmarshal(data, &ruleFile); err != nil {
		http.Error(w, "failed to parse rules file", http.StatusInternalServerError)
		return
	}

	// Find and toggle the rule
	found := false
	for i, rule := range ruleFile.Rules {
		if rule.Name == ruleName {
			ruleFile.Rules[i].Enabled = !rule.Enabled
			found = true
			break
		}
	}

	if !found {
		http.Error(w, "rule not found", http.StatusNotFound)
		return
	}

	// Write back to file
	yamlData, err := yaml.Marshal(ruleFile)
	if err != nil {
		http.Error(w, "failed to marshal rules", http.StatusInternalServerError)
		return
	}

	if err := os.WriteFile(config.RulesFile, yamlData, 0644); err != nil {
		http.Error(w, "failed to write rules file", http.StatusInternalServerError)
		return
	}

	// Reload rules engine
	engine, err := rules.LoadRules(config.RulesFile)
	if err != nil {
		http.Error(w, "failed to reload rules: "+err.Error(), http.StatusInternalServerError)
		return
	}
	config.RuleEngine.Store(engine)

	json.NewEncoder(w).Encode(map[string]string{
		"status":  "rule toggled",
		"message": "Rule enabled/disabled status toggled successfully",
	})
}

// GetPerformanceMetrics returns performance statistics
func GetPerformanceMetrics(w http.ResponseWriter, r *http.Request) {
	perfVal := config.PerfCollector.Load()
	if perfVal == nil {
		http.Error(w, "Performance collector not initialized", http.StatusInternalServerError)
		return
	}

	// Parse minutes parameter (default: 60)
	minutesStr := r.URL.Query().Get("minutes")
	minutes := 60
	if minutesStr != "" {
		if m, err := time.ParseDuration(minutesStr + "m"); err == nil {
			minutes = int(m.Minutes())
		}
	}

	// Type assert to get metrics
	type MetricsGetter interface {
		GetCurrentMetrics() *models.PerformanceMetrics
		GetMetricsFromDB(int) ([]*models.PerformanceMetrics, error)
	}

	perf, ok := perfVal.(MetricsGetter)
	if !ok {
		http.Error(w, "Invalid performance collector", http.StatusInternalServerError)
		return
	}

	// Get current metrics
	current := perf.GetCurrentMetrics()

	// Get historical metrics
	historical, err := perf.GetMetricsFromDB(minutes)
	if err != nil {
		log.Printf("Failed to get historical metrics: %v", err)
		historical = []*models.PerformanceMetrics{}
	}

	response := map[string]any{
		"current":    current,
		"historical": historical,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}
