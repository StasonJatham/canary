package rules

import (
	"fmt"
	"sort"
	"strings"
)

// Evaluate evaluates all rules against matched keywords and domains
// Returns the FIRST matching rule (stops after first match for performance)
// Rules are evaluated in priority order: critical > high > medium > low
func (e *Engine) Evaluate(matchedKeywords []string, domains []string) *RuleMatch {
	if e == nil || len(e.Rules) == 0 || len(matchedKeywords) == 0 {
		return nil
	}

	// Convert keywords to set for O(1) lookup
	keywordSet := make(map[string]bool, len(matchedKeywords))
	for _, kw := range matchedKeywords {
		keywordSet[kw] = true
	}

	// Evaluate rules in priority order (already sorted when loaded)
	// Stop at first match
	for _, rule := range e.Rules {
		if !rule.Enabled {
			continue
		}

		// For this rule, check if any NOT keywords exist in domains
		// and add them to keywordSet for proper NOT evaluation
		ruleKeywordSet := make(map[string]bool)
		for k, v := range keywordSet {
			ruleKeywordSet[k] = v
		}

		// Get all keywords from the rule
		allRuleKeywords := rule.Expression.ExtractKeywords()
		positiveRuleKeywords := rule.Expression.ExtractPositiveKeywords()

		// Find NOT keywords (keywords not in positive list)
		positiveSet := make(map[string]bool)
		for _, kw := range positiveRuleKeywords {
			positiveSet[strings.ToLower(kw)] = true
		}

		notKeywords := []string{}
		for _, kw := range allRuleKeywords {
			kwLower := strings.ToLower(kw)
			if !positiveSet[kwLower] {
				notKeywords = append(notKeywords, kwLower)
			}
		}

		// Check domains for NOT keywords
		for _, domain := range domains {
			domainLower := strings.ToLower(domain)
			for _, notKw := range notKeywords {
				if strings.Contains(domainLower, notKw) {
					ruleKeywordSet[notKw] = true
				}
			}
		}

		if rule.Expression.Evaluate(ruleKeywordSet) {
			// First match found - return immediately
			return &RuleMatch{
				RuleName: rule.Name,
				Priority: rule.Priority,
				Keywords: matchedKeywords,
			}
		}
	}

	// No rules matched
	return nil
}

// SortRulesByPriority sorts rules by priority (critical first)
// This ensures we stop at the highest priority match
func SortRulesByPriority(rules []*Rule) {
	sort.Slice(rules, func(i, j int) bool {
		return priorityOrder(rules[i].Priority) < priorityOrder(rules[j].Priority)
	})

	// Update order field for reference
	for i, rule := range rules {
		rule.Order = i
	}
}

// priorityOrder returns sort order (lower = higher priority)
func priorityOrder(p Priority) int {
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

// ValidatePriority checks if priority string is valid
func ValidatePriority(p string) Priority {
	switch Priority(p) {
	case PriorityCritical:
		return PriorityCritical
	case PriorityHigh:
		return PriorityHigh
	case PriorityMedium:
		return PriorityMedium
	case PriorityLow:
		return PriorityLow
	default:
		return PriorityMedium // default
	}
}

// NewEmptyEngine creates an empty rule engine
func NewEmptyEngine() *Engine {
	return &Engine{
		Rules:    []*Rule{},
		Keywords: []string{},
	}
}

// ExtractAllKeywords extracts all unique keywords from all rules
// Only includes positive keywords (excludes keywords inside NOT expressions)
// This is used for building the Aho-Corasick automaton
func (e *Engine) ExtractAllKeywords() []string {
	keywordSet := make(map[string]bool)

	for _, rule := range e.Rules {
		// Use ExtractPositiveKeywords to exclude keywords from NOT expressions
		keywords := rule.Expression.ExtractPositiveKeywords()
		for _, kw := range keywords {
			keywordSet[strings.ToLower(kw)] = true
		}
	}

	// Convert set to slice
	uniqueKeywords := make([]string, 0, len(keywordSet))
	for kw := range keywordSet {
		uniqueKeywords = append(uniqueKeywords, kw)
	}

	return uniqueKeywords
}

// BuildAhoCorasick builds the Aho-Corasick automaton from extracted keywords
func (e *Engine) BuildAhoCorasick() error {
	// Extract keywords
	e.Keywords = e.ExtractAllKeywords()

	if len(e.Keywords) == 0 {
		return fmt.Errorf("no keywords found in rules")
	}

	// Build Aho-Corasick dictionary
	dict := make([][]rune, len(e.Keywords))
	for i, kw := range e.Keywords {
		dict[i] = []rune(kw)
	}

	// Build machine
	if err := e.Machine.Build(dict); err != nil {
		return fmt.Errorf("failed to build Aho-Corasick automaton: %w", err)
	}

	return nil
}

// ExtractAllNOTKeywords extracts all keywords from NOT expressions across all rules
// These are not in the AC machine, but we need to check for them manually
func (e *Engine) ExtractAllNOTKeywords() []string {
	notKeywordSet := make(map[string]bool)

	for _, rule := range e.Rules {
		if !rule.Enabled {
			continue
		}
		// Get all keywords from the expression
		allKeywords := rule.Expression.ExtractKeywords()
		// Get positive keywords (in AC machine)
		positiveKeywords := rule.Expression.ExtractPositiveKeywords()

		// NOT keywords = all - positive
		positiveSet := make(map[string]bool)
		for _, kw := range positiveKeywords {
			positiveSet[strings.ToLower(kw)] = true
		}

		for _, kw := range allKeywords {
			kwLower := strings.ToLower(kw)
			if !positiveSet[kwLower] {
				notKeywordSet[kwLower] = true
			}
		}
	}

	result := make([]string, 0, len(notKeywordSet))
	for kw := range notKeywordSet {
		result = append(result, kw)
	}
	return result
}

// Find searches for keyword matches in domains using built-in Aho-Corasick
// Returns ONLY positive keywords (excludes NOT keywords)
// NOT keywords will be checked separately during rule evaluation
func (e *Engine) Find(domains []string) []string {
	matchesMap := make(map[string]bool)

	// Use AC machine to find positive keywords only
	// (NOT keywords are not in the AC machine by design)
	for _, domain := range domains {
		if domain == "" {
			continue
		}
		lowered := strings.ToLower(domain)
		terms := e.Machine.MultiPatternSearch([]rune(lowered), false)
		for _, term := range terms {
			matchesMap[string(term.Word)] = true
		}
	}

	result := make([]string, 0, len(matchesMap))
	for k := range matchesMap {
		result = append(result, k)
	}
	return result
}
