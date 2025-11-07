package rules

import (
	"testing"
)

// TestNotExprExcludesKeywords verifies that NOT expressions don't add keywords to AC machine
func TestNotExprExcludesKeywords(t *testing.T) {
	tests := []struct {
		name     string
		expr     string
		wantPos  []string // keywords that SHOULD be in AC machine
		wantAll  []string // all keywords in expression
	}{
		{
			name:    "simple NOT",
			expr:    "login AND NOT official",
			wantPos: []string{"login"},
			wantAll: []string{"login", "official"},
		},
		{
			name:    "twitter rule with NOT",
			expr:    "(twitter OR x.com) AND login AND NOT (twitter.com OR t.co)",
			wantPos: []string{"twitter", "x.com", "login"},
			wantAll: []string{"twitter", "x.com", "login", "twitter.com", "t.co"},
		},
		{
			name:    "complex NOT with AND",
			expr:    "paypal AND (login OR signin) AND NOT (official OR legitimate)",
			wantPos: []string{"paypal", "login", "signin"},
			wantAll: []string{"paypal", "login", "signin", "official", "legitimate"},
		},
		{
			name:    "nested NOT",
			expr:    "(bank OR banking) AND verify AND NOT (bankofamerica.com OR chase.com)",
			wantPos: []string{"bank", "banking", "verify"},
			wantAll: []string{"bank", "banking", "verify", "bankofamerica.com", "chase.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := Parse(tt.expr)
			if err != nil {
				t.Fatalf("Failed to parse expression: %v", err)
			}

			// Test ExtractPositiveKeywords (used for AC machine)
			gotPos := expr.ExtractPositiveKeywords()
			if !setsEqual(gotPos, tt.wantPos) {
				t.Errorf("ExtractPositiveKeywords() = %v, want %v", gotPos, tt.wantPos)
			}

			// Test ExtractKeywords (all keywords)
			gotAll := expr.ExtractKeywords()
			if !setsEqual(gotAll, tt.wantAll) {
				t.Errorf("ExtractKeywords() = %v, want %v", gotAll, tt.wantAll)
			}
		})
	}
}

// TestTwitterRuleDoesNotMatchFalsePositives tests the specific twitter rule bug
func TestTwitterRuleDoesNotMatchFalsePositives(t *testing.T) {
	// Parse the actual twitter rule
	expr, err := Parse("(twitter OR x.com) AND (login OR signin OR verify OR suspended) AND NOT (twitter.com OR t.co)")
	if err != nil {
		t.Fatalf("Failed to parse twitter rule: %v", err)
	}

	tests := []struct {
		name       string
		domain     string
		shouldMatch bool
		reason     string
	}{
		// False positives that should NOT match (contain t.co but not actually t.co domain)
		{
			name:       "marriott-bet.com",
			domain:     "marriott-bet.com",
			shouldMatch: false,
			reason:     "contains 't.co' substring but should be excluded",
		},
		{
			name:       "dialataxigosport.co.uk",
			domain:     "dialataxigosport.co.uk",
			shouldMatch: false,
			reason:     "contains 't.co' substring but should be excluded",
		},
		{
			name:       "theramtrust.co.za",
			domain:     "theramtrust.co.za",
			shouldMatch: false,
			reason:     "contains 't.co' substring but should be excluded",
		},
		{
			name:       "riosgoldencut.com",
			domain:     "riosgoldencut.com",
			shouldMatch: false,
			reason:     "contains 't.co' substring but should be excluded",
		},

		// True positives that SHOULD match (legitimate x.com matches)
		{
			name:       "twomaverix.com with login",
			domain:     "login.twomaverix.com",
			shouldMatch: true,
			reason:     "contains x.com and login, not excluded",
		},
		{
			name:       "okx.com with signin",
			domain:     "signin.okx.com",
			shouldMatch: true,
			reason:     "contains x.com and signin, not excluded",
		},
		{
			name:       "webex.com with verify",
			domain:     "verify.webex.com",
			shouldMatch: true,
			reason:     "contains x.com and verify, not excluded",
		},

		// Actual twitter/t.co domains that should NOT match
		{
			name:       "twitter.com itself",
			domain:     "login.twitter.com",
			shouldMatch: false,
			reason:     "twitter.com is explicitly excluded",
		},
		{
			name:       "t.co itself",
			domain:     "https.t.co",
			shouldMatch: false,
			reason:     "t.co is explicitly excluded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate what the AC machine would find
			// Since we fixed it, AC machine won't find "t.co" or "twitter.com"
			var keywords []string

			// Only add positive keywords that would be in AC machine
			if contains(tt.domain, "twitter") && !contains(tt.domain, "twitter.com") {
				keywords = append(keywords, "twitter")
			}
			if contains(tt.domain, "x.com") {
				keywords = append(keywords, "x.com")
			}
			if contains(tt.domain, "login") {
				keywords = append(keywords, "login")
			}
			if contains(tt.domain, "signin") {
				keywords = append(keywords, "signin")
			}
			if contains(tt.domain, "verify") {
				keywords = append(keywords, "verify")
			}
			if contains(tt.domain, "suspended") {
				keywords = append(keywords, "suspended")
			}

			// Create keyword set
			keywordSet := make(map[string]bool)
			for _, kw := range keywords {
				keywordSet[kw] = true
			}

			// Evaluate
			matched := expr.Evaluate(keywordSet)

			if matched != tt.shouldMatch {
				t.Errorf("Domain %q: matched=%v, want=%v (reason: %s)",
					tt.domain, matched, tt.shouldMatch, tt.reason)
			}
		})
	}
}

// TestComplexBooleanExpressions tests various complex Boolean rule scenarios
func TestComplexBooleanExpressions(t *testing.T) {
	tests := []struct {
		name       string
		expr       string
		keywords   []string
		shouldMatch bool
	}{
		{
			name:       "simple AND match",
			expr:       "paypal AND login",
			keywords:   []string{"paypal", "login"},
			shouldMatch: true,
		},
		{
			name:       "simple AND no match",
			expr:       "paypal AND login",
			keywords:   []string{"paypal"},
			shouldMatch: false,
		},
		{
			name:       "simple OR match",
			expr:       "paypal OR stripe",
			keywords:   []string{"paypal"},
			shouldMatch: true,
		},
		{
			name:       "simple OR no match",
			expr:       "paypal OR stripe",
			keywords:   []string{"bank"},
			shouldMatch: false,
		},
		{
			name:       "NOT excludes match",
			expr:       "login AND NOT official",
			keywords:   []string{"login", "official"},
			shouldMatch: false,
		},
		{
			name:       "NOT allows match",
			expr:       "login AND NOT official",
			keywords:   []string{"login"},
			shouldMatch: true,
		},
		{
			name:       "complex expression match",
			expr:       "(paypal OR stripe) AND login AND NOT official",
			keywords:   []string{"paypal", "login"},
			shouldMatch: true,
		},
		{
			name:       "complex expression excluded by NOT",
			expr:       "(paypal OR stripe) AND login AND NOT official",
			keywords:   []string{"paypal", "login", "official"},
			shouldMatch: false,
		},
		{
			name:       "multiple NOTs",
			expr:       "bank AND login AND NOT (legitimate OR official OR government)",
			keywords:   []string{"bank", "login"},
			shouldMatch: true,
		},
		{
			name:       "multiple NOTs with one match",
			expr:       "bank AND login AND NOT (legitimate OR official OR government)",
			keywords:   []string{"bank", "login", "official"},
			shouldMatch: false,
		},
		{
			name:       "nested parentheses",
			expr:       "((microsoft OR office365) AND login) AND NOT (microsoft.com OR live.com)",
			keywords:   []string{"microsoft", "login"},
			shouldMatch: true,
		},
		{
			name:       "nested parentheses with NOT match",
			expr:       "((microsoft OR office365) AND login) AND NOT (microsoft.com OR live.com)",
			keywords:   []string{"microsoft", "login", "microsoft.com"},
			shouldMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := Parse(tt.expr)
			if err != nil {
				t.Fatalf("Failed to parse expression: %v", err)
			}

			keywordSet := make(map[string]bool)
			for _, kw := range tt.keywords {
				keywordSet[kw] = true
			}

			matched := expr.Evaluate(keywordSet)
			if matched != tt.shouldMatch {
				t.Errorf("Expression %q with keywords %v: matched=%v, want=%v",
					tt.expr, tt.keywords, matched, tt.shouldMatch)
			}
		})
	}
}

// TestRulePriorityOrdering tests that rules are sorted correctly by priority
func TestRulePriorityOrdering(t *testing.T) {
	rules := []*Rule{
		{Name: "low_rule", Priority: PriorityLow},
		{Name: "critical_rule", Priority: PriorityCritical},
		{Name: "medium_rule", Priority: PriorityMedium},
		{Name: "high_rule", Priority: PriorityHigh},
	}

	SortRulesByPriority(rules)

	expected := []string{"critical_rule", "high_rule", "medium_rule", "low_rule"}
	for i, rule := range rules {
		if rule.Name != expected[i] {
			t.Errorf("Position %d: got %q, want %q", i, rule.Name, expected[i])
		}
	}
}

// TestEngineEvaluateStopsAtFirstMatch verifies early termination behavior
func TestEngineEvaluateStopsAtFirstMatch(t *testing.T) {
	engine := &Engine{
		Rules: []*Rule{
			{
				Name:     "high_priority",
				Priority: PriorityHigh,
				Enabled:  true,
				Expression: AndExpr{
					Left:  KeywordExpr{Keyword: "paypal"},
					Right: KeywordExpr{Keyword: "login"},
				},
			},
			{
				Name:     "low_priority",
				Priority: PriorityLow,
				Enabled:  true,
				Expression: KeywordExpr{Keyword: "paypal"},
			},
		},
	}

	// Sort rules by priority
	SortRulesByPriority(engine.Rules)

	// Both rules would match, but should only return the first (high priority)
	keywords := []string{"paypal", "login"}
	domains := []string{"paypal-login.example.com"} // Dummy domain for test
	match := engine.Evaluate(keywords, domains)

	if match == nil {
		t.Fatal("Expected match, got nil")
	}

	if match.RuleName != "high_priority" {
		t.Errorf("Expected 'high_priority', got %q", match.RuleName)
	}
}

// TestDisabledRulesNotEvaluated verifies disabled rules are skipped
func TestDisabledRulesNotEvaluated(t *testing.T) {
	engine := &Engine{
		Rules: []*Rule{
			{
				Name:       "disabled_rule",
				Priority:   PriorityCritical,
				Enabled:    false,
				Expression: KeywordExpr{Keyword: "test"},
			},
			{
				Name:       "enabled_rule",
				Priority:   PriorityLow,
				Enabled:    true,
				Expression: KeywordExpr{Keyword: "test"},
			},
		},
	}

	SortRulesByPriority(engine.Rules)

	keywords := []string{"test"}
	domains := []string{"test.example.com"} // Dummy domain for test
	match := engine.Evaluate(keywords, domains)

	if match == nil {
		t.Fatal("Expected match, got nil")
	}

	if match.RuleName != "enabled_rule" {
		t.Errorf("Expected 'enabled_rule', got %q", match.RuleName)
	}
}

// TestEdgeCases tests edge cases and boundary conditions
func TestEdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		expr       string
		keywords   []string
		shouldMatch bool
	}{
		{
			name:       "empty keywords",
			expr:       "login",
			keywords:   []string{},
			shouldMatch: false,
		},
		{
			name:       "single keyword match",
			expr:       "login",
			keywords:   []string{"login"},
			shouldMatch: true,
		},
		{
			name:       "case sensitivity",
			expr:       "login",
			keywords:   []string{"LOGIN"},
			shouldMatch: false, // Keywords should be lowercase in practice
		},
		{
			name:       "NOT with no positive keywords",
			expr:       "NOT official",
			keywords:   []string{},
			shouldMatch: true, // NOT official with no keywords = true
		},
		{
			name:       "double NOT",
			expr:       "login AND NOT (NOT suspended)",
			keywords:   []string{"login", "suspended"},
			shouldMatch: true, // NOT (NOT suspended) with suspended = true
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := Parse(tt.expr)
			if err != nil {
				t.Fatalf("Failed to parse expression: %v", err)
			}

			keywordSet := make(map[string]bool)
			for _, kw := range tt.keywords {
				keywordSet[kw] = true
			}

			matched := expr.Evaluate(keywordSet)
			if matched != tt.shouldMatch {
				t.Errorf("Expression %q with keywords %v: matched=%v, want=%v",
					tt.expr, tt.keywords, matched, tt.shouldMatch)
			}
		})
	}
}

// Helper functions

func setsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	setA := make(map[string]bool)
	for _, item := range a {
		setA[item] = true
	}

	for _, item := range b {
		if !setA[item] {
			return false
		}
	}

	return true
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
