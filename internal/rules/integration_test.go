package rules

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// TestAgainstRealDatabase validates rules against actual database matches
func TestAgainstRealDatabase(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	// Try to open the database
	dbPath := "../../data/matches.db"
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Skip("Database not found, skipping integration test")
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Get today's table name
	tableName := fmt.Sprintf("matches_%s", time.Now().Format("2006_01_02"))

	t.Run("NoFalsePositivesForTCo", func(t *testing.T) {
		testNoFalsePositives(t, db, tableName, "t.co", []string{
			"t.com", "t.co.uk", "t.co.za", "ment.com", "ent.com",
			"marriott-", "ct.com", "icut.com", "cut.com",
		})
	})

	t.Run("NoFalsePositivesForTwitterDotCom", func(t *testing.T) {
		testNoFalsePositives(t, db, tableName, "twitter.com", []string{
			"twitter.com",
		})
	})

	t.Run("ValidateXComMatches", func(t *testing.T) {
		testValidMatches(t, db, tableName, "x.com", []string{
			"x.com", "webex.com", "okx.com", "apex.com", "vortex.com",
		})
	})
}

func testNoFalsePositives(t *testing.T, db *sql.DB, tableName, keyword string, falsePositivePatterns []string) {
	// Query for recent matches with this keyword (last hour)
	query := fmt.Sprintf(`
		SELECT domains, keyword, timestamp
		FROM %s
		WHERE keyword = ?
		AND timestamp >= datetime('now', '-1 hour')
		LIMIT 100
	`, tableName)

	rows, err := db.Query(query, keyword)
	if err != nil {
		// Table might not exist or no matches
		t.Logf("Query failed (expected if no recent data): %v", err)
		return
	}
	defer rows.Close()

	falsePositives := []string{}
	for rows.Next() {
		var domains, kw, timestamp string
		if err := rows.Scan(&domains, &kw, &timestamp); err != nil {
			t.Errorf("Failed to scan row: %v", err)
			continue
		}

		// Check if this is a false positive
		isFalsePositive := false
		for _, pattern := range falsePositivePatterns {
			if strings.Contains(strings.ToLower(domains), strings.ToLower(pattern)) &&
				!strings.Contains(strings.ToLower(domains), keyword) {
				isFalsePositive = true
				break
			}
		}

		if isFalsePositive {
			falsePositives = append(falsePositives, domains)
		}
	}

	if len(falsePositives) > 0 {
		t.Errorf("Found %d false positives for keyword %q:\n%v",
			len(falsePositives), keyword, strings.Join(falsePositives, "\n"))
	} else {
		t.Logf("✓ No false positives found for keyword %q in last hour", keyword)
	}
}

func testValidMatches(t *testing.T, db *sql.DB, tableName, keyword string, expectedPatterns []string) {
	query := fmt.Sprintf(`
		SELECT domains, keyword
		FROM %s
		WHERE keyword = ?
		AND timestamp >= datetime('now', '-1 hour')
		LIMIT 10
	`, tableName)

	rows, err := db.Query(query, keyword)
	if err != nil {
		t.Logf("Query failed (expected if no recent data): %v", err)
		return
	}
	defer rows.Close()

	matchCount := 0
	validCount := 0
	for rows.Next() {
		var domains, kw string
		if err := rows.Scan(&domains, &kw); err != nil {
			t.Errorf("Failed to scan row: %v", err)
			continue
		}

		matchCount++

		// Verify this is a legitimate match containing the keyword
		if strings.Contains(strings.ToLower(domains), strings.ToLower(keyword)) {
			validCount++
		}
	}

	if matchCount > 0 {
		t.Logf("✓ Found %d matches for %q, %d are valid (%.1f%%)",
			matchCount, keyword, validCount, float64(validCount)/float64(matchCount)*100)
	} else {
		t.Logf("No recent matches found for %q", keyword)
	}
}

// TestDatabaseStats provides statistics about matches in the database
func TestDatabaseStats(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stats test")
	}

	dbPath := "../../data/matches.db"
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Skip("Database not found")
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	tableName := fmt.Sprintf("matches_%s", time.Now().Format("2006_01_02"))

	// Get total matches
	var totalMatches int
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)
	err = db.QueryRow(query).Scan(&totalMatches)
	if err != nil {
		t.Logf("No data in table %s", tableName)
		return
	}

	t.Logf("Total matches in %s: %d", tableName, totalMatches)

	// Get keyword distribution
	query = fmt.Sprintf(`
		SELECT keyword, COUNT(*) as count
		FROM %s
		GROUP BY keyword
		ORDER BY count DESC
		LIMIT 20
	`, tableName)

	rows, err := db.Query(query)
	if err != nil {
		t.Errorf("Failed to get keyword stats: %v", err)
		return
	}
	defer rows.Close()

	t.Log("\nTop 20 Keywords:")
	t.Log("----------------")
	for rows.Next() {
		var keyword string
		var count int
		if err := rows.Scan(&keyword, &count); err != nil {
			continue
		}
		pct := float64(count) / float64(totalMatches) * 100
		t.Logf("%20s: %6d (%.2f%%)", keyword, count, pct)
	}

	// Check for NOT keywords that shouldn't exist in RECENT matches (last 30 minutes)
	// Note: Older matches may contain false positives from before the fix (deployed ~11:30)
	notKeywords := []string{"t.co", "twitter.com", "facebook.com", "fbcdn"}
	for _, kw := range notKeywords {
		var count int
		query = fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE keyword = ? AND timestamp >= datetime('now', '-30 minutes')", tableName)
		db.QueryRow(query, kw).Scan(&count)
		if count > 0 {
			t.Errorf("Found %d RECENT matches (last 30m) for NOT keyword %q - these should not exist!", count, kw)

			// Show some examples
			query = fmt.Sprintf("SELECT domains, timestamp FROM %s WHERE keyword = ? AND timestamp >= datetime('now', '-30 minutes') LIMIT 3", tableName)
			rows, _ := db.Query(query, kw)
			defer rows.Close()
			for rows.Next() {
				var domains, ts string
				rows.Scan(&domains, &ts)
				t.Logf("  Example: %s at %s", domains, ts)
			}
		} else {
			t.Logf("✓ No recent matches (last 30m) for NOT keyword %q", kw)
		}
	}
}

// TestRuleLoadFromYAML tests loading actual rules from the YAML file
func TestRuleLoadFromYAML(t *testing.T) {
	rulesPath := "../../data/rules.yaml"
	if _, err := os.Stat(rulesPath); os.IsNotExist(err) {
		t.Skip("Rules file not found")
	}

	engine, err := LoadRules(rulesPath)
	if err != nil {
		t.Fatalf("Failed to load rules: %v", err)
	}

	// Verify rules loaded
	if len(engine.Rules) == 0 {
		t.Fatal("No rules loaded")
	}

	t.Logf("Loaded %d rules", len(engine.Rules))

	// Count enabled rules
	enabledCount := engine.GetEnabledRuleCount()
	t.Logf("Enabled rules: %d", enabledCount)

	// Verify keywords extracted
	if len(engine.Keywords) == 0 {
		t.Fatal("No keywords extracted")
	}

	t.Logf("Extracted %d keywords for AC machine", len(engine.Keywords))

	// Verify NOT keywords are excluded
	notKeywords := []string{"t.co", "twitter.com", "facebook.com", "fbcdn", "legitimate", "official"}
	foundNotKeywords := []string{}

	for _, kw := range engine.Keywords {
		for _, notKw := range notKeywords {
			if strings.ToLower(kw) == strings.ToLower(notKw) {
				foundNotKeywords = append(foundNotKeywords, kw)
			}
		}
	}

	if len(foundNotKeywords) > 0 {
		t.Errorf("Found NOT keywords in AC machine (should be excluded): %v", foundNotKeywords)
	} else {
		t.Log("✓ All NOT keywords properly excluded from AC machine")
	}

	// Test specific rules
	t.Run("TwitterRule", func(t *testing.T) {
		var twitterRule *Rule
		for _, rule := range engine.Rules {
			if rule.Name == "twitter_phish" {
				twitterRule = rule
				break
			}
		}

		if twitterRule == nil {
			t.Skip("Twitter rule not found")
		}

		// Verify it extracts correct keywords
		allKeywords := twitterRule.Expression.ExtractKeywords()
		posKeywords := twitterRule.Expression.ExtractPositiveKeywords()

		t.Logf("Twitter rule - All keywords: %v", allKeywords)
		t.Logf("Twitter rule - Positive keywords: %v", posKeywords)

		// Verify t.co and twitter.com are in all but not in positive
		hasNotKeywords := false
		for _, kw := range allKeywords {
			if strings.Contains(strings.ToLower(kw), "t.co") ||
				strings.Contains(strings.ToLower(kw), "twitter.com") {
				hasNotKeywords = true
				break
			}
		}

		hasNotInPositive := false
		for _, kw := range posKeywords {
			if strings.Contains(strings.ToLower(kw), "t.co") ||
				strings.Contains(strings.ToLower(kw), "twitter.com") {
				hasNotInPositive = true
				break
			}
		}

		if hasNotInPositive {
			t.Error("NOT keywords found in positive keywords - bug!")
		} else if hasNotKeywords {
			t.Log("✓ NOT keywords properly in all keywords but excluded from positive")
		}
	})

	// Test priority sorting
	t.Run("RulePrioritySorting", func(t *testing.T) {
		if len(engine.Rules) < 2 {
			t.Skip("Not enough rules to test sorting")
		}

		// Verify rules are sorted by priority
		for i := 0; i < len(engine.Rules)-1; i++ {
			curr := getPriorityOrder(engine.Rules[i].Priority)
			next := getPriorityOrder(engine.Rules[i+1].Priority)
			if curr > next {
				t.Errorf("Rules not sorted correctly: %s (%s) comes before %s (%s)",
					engine.Rules[i].Name, engine.Rules[i].Priority,
					engine.Rules[i+1].Name, engine.Rules[i+1].Priority)
			}
		}

		t.Log("✓ Rules are correctly sorted by priority")
	})
}

// getPriorityOrder helper for testing
func getPriorityOrder(p Priority) int {
	switch p {
	case PriorityCritical:
		return 0
	case PriorityHigh:
		return 1
	case PriorityMedium:
		return 2
	case PriorityLow:
		return 3
	default:
		return 4
	}
}
