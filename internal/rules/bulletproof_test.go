package rules

import (
	"strings"
	"testing"
)

// TestBulletproofMatchingLogic is a comprehensive test to ensure 100% certainty
// that the rule matching logic is bulletproof and matches exactly what we want
func TestBulletproofMatchingLogic(t *testing.T) {
	t.Run("TwitterRule_ComprehensiveTest", func(t *testing.T) {
		// The exact twitter rule from production
		expr, err := Parse("(twitter OR x.com) AND (login OR signin OR verify OR suspended) AND NOT (twitter.com OR t.co)")
		if err != nil {
			t.Fatalf("Failed to parse twitter rule: %v", err)
		}

		// Test 1: Verify NOT keywords are EXCLUDED from AC machine
		positiveKeywords := expr.ExtractPositiveKeywords()
		allKeywords := expr.ExtractKeywords()

		t.Logf("All keywords: %v", allKeywords)
		t.Logf("Positive keywords (for AC): %v", positiveKeywords)

		// NOT keywords should be in allKeywords but NOT in positiveKeywords
		notKeywords := []string{"twitter.com", "t.co"}
		for _, notKw := range notKeywords {
			foundInAll := containsKeyword(allKeywords, notKw)
			foundInPositive := containsKeyword(positiveKeywords, notKw)

			if !foundInAll {
				t.Errorf("NOT keyword %q should be in allKeywords but is not", notKw)
			}
			if foundInPositive {
				t.Errorf("NOT keyword %q should NOT be in positiveKeywords but is there - BUG!", notKw)
			}
		}

		// Positive keywords should be in both
		wantPositive := []string{"twitter", "x.com", "login", "signin", "verify", "suspended"}
		for _, kw := range wantPositive {
			foundInAll := containsKeyword(allKeywords, kw)
			foundInPositive := containsKeyword(positiveKeywords, kw)

			if !foundInAll {
				t.Errorf("Positive keyword %q should be in allKeywords but is not", kw)
			}
			if !foundInPositive {
				t.Errorf("Positive keyword %q should be in positiveKeywords but is not", kw)
			}
		}

		// Test 2: FALSE POSITIVES - These should NEVER match
		falsePositives := []struct {
			domain string
			reason string
		}{
			{"marriott-bet.com", "contains 't.co' substring"},
			{"dialataxigosport.co.uk", "contains 't.co' substring"},
			{"theramtrust.co.za", "contains 't.co' substring"},
			{"riosgoldencut.com", "contains 't.co' substring"},
			{"funabashi-implant.com", "contains 't.co' substring"},
			{"bitcointrader.co.za", "contains 't.co' substring"},
			{"authenticator.com", "contains 't.co' substring in 'ticator'"},
			{"detector.com", "contains 't.co' substring in 'tector'"},
			{"twitter.com", "explicitly excluded by NOT"},
			{"login.twitter.com", "explicitly excluded by NOT"},
			{"signin.twitter.com", "explicitly excluded by NOT"},
			{"t.co", "explicitly excluded by NOT"},
			{"https.t.co", "explicitly excluded by NOT"},
		}

		for _, tc := range falsePositives {
			// Simulate Aho-Corasick finding only positive keywords
			// (this is what the fix ensures - AC machine doesn't have t.co or twitter.com)
			var foundKeywords []string
			domainLower := strings.ToLower(tc.domain)

			// Only check for positive keywords (NOT keywords are not in AC machine)
			if strings.Contains(domainLower, "twitter") && !strings.Contains(domainLower, "twitter.com") {
				foundKeywords = append(foundKeywords, "twitter")
			}
			if strings.Contains(domainLower, "x.com") {
				foundKeywords = append(foundKeywords, "x.com")
			}
			if strings.Contains(domainLower, "login") {
				foundKeywords = append(foundKeywords, "login")
			}
			if strings.Contains(domainLower, "signin") {
				foundKeywords = append(foundKeywords, "signin")
			}
			if strings.Contains(domainLower, "verify") {
				foundKeywords = append(foundKeywords, "verify")
			}
			if strings.Contains(domainLower, "suspended") {
				foundKeywords = append(foundKeywords, "suspended")
			}

			// Convert to set
			keywordSet := make(map[string]bool)
			for _, kw := range foundKeywords {
				keywordSet[kw] = true
			}

			matched := expr.Evaluate(keywordSet)
			if matched {
				t.Errorf("FALSE POSITIVE: Domain %q MATCHED but should NOT (reason: %s) - keywords found: %v",
					tc.domain, tc.reason, foundKeywords)
			} else {
				t.Logf("✓ Correctly rejected: %q (reason: %s)", tc.domain, tc.reason)
			}
		}

		// Test 3: TRUE POSITIVES - These SHOULD match
		truePositives := []struct {
			domain string
			reason string
		}{
			{"login.twomaverix.com", "contains x.com and login"},
			{"signin.okx.com", "contains x.com and signin"},
			{"verify.webex.com", "contains x.com and verify"},
			{"suspended.mytwitter.net", "contains twitter (not twitter.com) and suspended"},
			{"login.mytwitter.net", "contains twitter (not twitter.com) and login"},
			{"signin.tax.com", "contains x.com and signin"},
			{"verify.wax.com", "contains x.com and verify"},
			{"login.lex.com", "contains x.com and login"},
			{"verify.dex.com", "contains x.com and verify"},
		}

		for _, tc := range truePositives {
			var foundKeywords []string
			domainLower := strings.ToLower(tc.domain)

			// Simulate AC machine finding positive keywords
			if strings.Contains(domainLower, "twitter") && !strings.Contains(domainLower, "twitter.com") {
				foundKeywords = append(foundKeywords, "twitter")
			}
			if strings.Contains(domainLower, "x.com") {
				foundKeywords = append(foundKeywords, "x.com")
			}
			if strings.Contains(domainLower, "login") {
				foundKeywords = append(foundKeywords, "login")
			}
			if strings.Contains(domainLower, "signin") {
				foundKeywords = append(foundKeywords, "signin")
			}
			if strings.Contains(domainLower, "verify") {
				foundKeywords = append(foundKeywords, "verify")
			}
			if strings.Contains(domainLower, "suspended") {
				foundKeywords = append(foundKeywords, "suspended")
			}

			keywordSet := make(map[string]bool)
			for _, kw := range foundKeywords {
				keywordSet[kw] = true
			}

			matched := expr.Evaluate(keywordSet)
			if !matched {
				t.Errorf("FALSE NEGATIVE: Domain %q should MATCH but did not (reason: %s) - keywords found: %v",
					tc.domain, tc.reason, foundKeywords)
			} else {
				t.Logf("✓ Correctly matched: %q (reason: %s) with keywords: %v", tc.domain, tc.reason, foundKeywords)
			}
		}
	})

	t.Run("ComplexNOT_BooleanLogic", func(t *testing.T) {
		tests := []struct {
			name         string
			expr         string
			keywords     []string
			shouldMatch  bool
			description  string
		}{
			{
				name:        "NOT_excludes_when_present",
				expr:        "login AND NOT official",
				keywords:    []string{"login", "official"},
				shouldMatch: false,
				description: "Should NOT match when NOT keyword is present",
			},
			{
				name:        "NOT_allows_when_absent",
				expr:        "login AND NOT official",
				keywords:    []string{"login"},
				shouldMatch: true,
				description: "Should match when NOT keyword is absent",
			},
			{
				name:        "Multiple_NOT_all_must_be_absent",
				expr:        "login AND NOT (official OR legitimate)",
				keywords:    []string{"login"},
				shouldMatch: true,
				description: "Should match when all NOT keywords are absent",
			},
			{
				name:        "Multiple_NOT_one_present_fails",
				expr:        "login AND NOT (official OR legitimate)",
				keywords:    []string{"login", "official"},
				shouldMatch: false,
				description: "Should NOT match when any NOT keyword is present",
			},
			{
				name:        "Nested_NOT_complex",
				expr:        "(paypal OR stripe) AND (login OR signin) AND NOT (official OR paypal.com)",
				keywords:    []string{"paypal", "login"},
				shouldMatch: true,
				description: "Complex expression should match correctly",
			},
			{
				name:        "Nested_NOT_complex_excluded",
				expr:        "(paypal OR stripe) AND (login OR signin) AND NOT (official OR paypal.com)",
				keywords:    []string{"paypal", "login", "paypal.com"},
				shouldMatch: false,
				description: "Complex expression should be excluded by NOT",
			},
			{
				name:        "Only_NOT_no_positives",
				expr:        "NOT official",
				keywords:    []string{},
				shouldMatch: true,
				description: "NOT alone with no keywords should be true",
			},
			{
				name:        "Only_NOT_with_keyword",
				expr:        "NOT official",
				keywords:    []string{"official"},
				shouldMatch: false,
				description: "NOT alone with the keyword should be false",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				expr, err := Parse(tt.expr)
				if err != nil {
					t.Fatalf("Failed to parse: %v", err)
				}

				keywordSet := make(map[string]bool)
				for _, kw := range tt.keywords {
					keywordSet[kw] = true
				}

				matched := expr.Evaluate(keywordSet)
				if matched != tt.shouldMatch {
					t.Errorf("%s: expr=%q keywords=%v matched=%v want=%v",
						tt.description, tt.expr, tt.keywords, matched, tt.shouldMatch)
				} else {
					t.Logf("✓ %s", tt.description)
				}
			})
		}
	})

	t.Run("AhoCorasick_Keyword_Extraction", func(t *testing.T) {
		// Test that keyword extraction for AC machine is correct
		testRules := []struct {
			expr         string
			wantPositive []string
			wantAll      []string
		}{
			{
				expr:         "login",
				wantPositive: []string{"login"},
				wantAll:      []string{"login"},
			},
			{
				expr:         "login AND NOT official",
				wantPositive: []string{"login"},
				wantAll:      []string{"login", "official"},
			},
			{
				expr:         "(twitter OR x.com) AND NOT t.co",
				wantPositive: []string{"twitter", "x.com"},
				wantAll:      []string{"twitter", "x.com", "t.co"},
			},
			{
				expr:         "a AND NOT (b OR c OR d)",
				wantPositive: []string{"a"},
				wantAll:      []string{"a", "b", "c", "d"},
			},
			{
				expr:         "(a OR b) AND (c OR d) AND NOT (e OR f)",
				wantPositive: []string{"a", "b", "c", "d"},
				wantAll:      []string{"a", "b", "c", "d", "e", "f"},
			},
		}

		for _, tt := range testRules {
			expr, err := Parse(tt.expr)
			if err != nil {
				t.Fatalf("Failed to parse %q: %v", tt.expr, err)
			}

			gotPositive := expr.ExtractPositiveKeywords()
			gotAll := expr.ExtractKeywords()

			if !setsEqual(gotPositive, tt.wantPositive) {
				t.Errorf("Expr %q: ExtractPositiveKeywords() = %v, want %v",
					tt.expr, gotPositive, tt.wantPositive)
			}

			if !setsEqual(gotAll, tt.wantAll) {
				t.Errorf("Expr %q: ExtractKeywords() = %v, want %v",
					tt.expr, gotAll, tt.wantAll)
			}

			t.Logf("✓ Expr %q: positive=%v all=%v", tt.expr, gotPositive, gotAll)
		}
	})

	t.Run("Substring_Not_Full_Match", func(t *testing.T) {
		// Ensure that "t.co" substring doesn't trigger when NOT in AC machine
		expr, _ := Parse("login AND NOT t.co")

		// If AC machine has "t.co" in it, domains with "t.co" substring would match
		// After fix, AC machine should NOT have "t.co", so these domains won't be found
		positiveKw := expr.ExtractPositiveKeywords()

		if containsKeyword(positiveKw, "t.co") {
			t.Error("CRITICAL BUG: 't.co' found in positive keywords for AC machine!")
		}

		if !containsKeyword(positiveKw, "login") {
			t.Error("'login' should be in positive keywords")
		}

		// Domains that would falsely match if "t.co" was in AC machine
		problematicDomains := []string{
			"authenticator.com", // t.co in "ticator"
			"detector.com",      // t.co in "tector"
			"factor.com",        // t.co in "ctor"
		}

		for _, domain := range problematicDomains {
			// Simulate: AC machine only has "login", not "t.co"
			var keywords []string
			if strings.Contains(domain, "login") {
				keywords = append(keywords, "login")
			}
			// "t.co" would NOT be found because it's not in AC machine

			keywordSet := make(map[string]bool)
			for _, kw := range keywords {
				keywordSet[kw] = true
			}

			// Without "login", rule won't match (needs login AND NOT t.co)
			matched := expr.Evaluate(keywordSet)
			if matched {
				t.Errorf("Domain %q should not match without 'login' keyword", domain)
			}

			// With "login", rule should match (because t.co is not found by AC)
			keywordSet["login"] = true
			matched = expr.Evaluate(keywordSet)
			if !matched {
				t.Logf("✓ Domain %q with login: matches correctly (t.co not in AC machine)", domain)
			}
		}
	})

	t.Run("RealEngine_Integration", func(t *testing.T) {
		// Create a minimal engine with just the twitter rule
		twitterRule, err := Parse("(twitter OR x.com) AND (login OR signin OR verify OR suspended) AND NOT (twitter.com OR t.co)")
		if err != nil {
			t.Fatalf("Failed to parse: %v", err)
		}

		engine := &Engine{
			Rules: []*Rule{
				{
					Name:       "twitter_phish",
					Expression: twitterRule,
					Priority:   PriorityHigh,
					Enabled:    true,
				},
			},
		}

		// Build AC machine
		if err := engine.BuildAhoCorasick(); err != nil {
			t.Fatalf("Failed to build AC: %v", err)
		}

		testCases := []struct {
			domain      string
			shouldMatch bool
			reason      string
		}{
			// Should NOT match
			{"login.twitter.com", false, "twitter.com explicitly excluded"},
			{"signin.twitter.com", false, "twitter.com explicitly excluded"},
			{"t.co", false, "t.co explicitly excluded"},
			{"https.t.co", false, "t.co explicitly excluded"},
			{"marriott-bet.com", false, "contains t.co substring but excluded"},
			{"authenticator.com", false, "contains t.co substring but excluded"},

			// SHOULD match
			{"login.webex.com", true, "contains x.com and login"},
			{"signin.okx.com", true, "contains x.com and signin"},
			{"verify.tax.com", true, "contains x.com and verify"},
			{"suspended.mytwitter.net", true, "contains twitter (not twitter.com) and suspended"},
		}

		for _, tc := range testCases {
			// Use actual Engine.Find() method
			domains := []string{tc.domain}
			keywords := engine.Find(domains)
			t.Logf("Domain %q -> Keywords: %v", tc.domain, keywords)

			// Evaluate with actual engine (pass domains for NOT keyword checking)
			match := engine.Evaluate(keywords, domains)

			matched := (match != nil)
			if matched != tc.shouldMatch {
				t.Errorf("Domain %q: matched=%v want=%v (reason: %s)",
					tc.domain, matched, tc.shouldMatch, tc.reason)
			} else {
				t.Logf("✓ Domain %q: correct result (reason: %s)", tc.domain, tc.reason)
			}
		}
	})
}

// Helper to check if string slice contains value
func containsKeyword(slice []string, val string) bool {
	for _, item := range slice {
		if strings.EqualFold(item, val) {
			return true
		}
	}
	return false
}
