package rules

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// RuleFile represents the YAML structure
type RuleFile struct {
	Rules []RuleConfig `yaml:"rules"`
}

// RuleConfig represents a single rule in YAML
type RuleConfig struct {
	Name     string `yaml:"name"`
	Keywords string `yaml:"keywords"`
	Priority string `yaml:"priority"`
	Enabled  bool   `yaml:"enabled"`
	Comment  string `yaml:"comment"`
}

// LoadRules loads rules from a YAML file
func LoadRules(path string) (*Engine, error) {
	// Read file
	data, err := os.ReadFile(path)
	if err != nil {
		// If file doesn't exist, return empty engine
		if os.IsNotExist(err) {
			return NewEmptyEngine(), nil
		}
		return nil, fmt.Errorf("failed to read rules file: %w", err)
	}

	// Parse YAML
	var ruleFile RuleFile
	if err := yaml.Unmarshal(data, &ruleFile); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	// Build engine
	engine := &Engine{
		Rules: make([]*Rule, 0, len(ruleFile.Rules)),
	}

	// Parse each rule
	var parseErrors []string
	for i, ruleConfig := range ruleFile.Rules {
		rule, err := parseRule(ruleConfig, i)
		if err != nil {
			parseErrors = append(parseErrors, fmt.Sprintf("rule %d (%s): %v", i, ruleConfig.Name, err))
			continue
		}
		engine.Rules = append(engine.Rules, rule)
	}

	// Return error if any rules failed to parse
	if len(parseErrors) > 0 {
		return nil, fmt.Errorf("failed to parse rules:\n%s", joinErrors(parseErrors))
	}

	// Sort rules by priority (critical first)
	SortRulesByPriority(engine.Rules)

	// Build Aho-Corasick automaton from rule keywords
	if err := engine.BuildAhoCorasick(); err != nil {
		return nil, fmt.Errorf("failed to build Aho-Corasick: %w", err)
	}

	return engine, nil
}

// parseRule converts a RuleConfig to a Rule
func parseRule(config RuleConfig, index int) (*Rule, error) {
	// Validate name
	if config.Name == "" {
		return nil, fmt.Errorf("rule name is required")
	}

	// Validate keywords
	if config.Keywords == "" {
		return nil, fmt.Errorf("keywords are required")
	}

	// Parse expression
	expr, err := Parse(config.Keywords)
	if err != nil {
		return nil, fmt.Errorf("failed to parse keywords: %w", err)
	}

	// Validate and normalize priority
	priority := ValidatePriority(config.Priority)

	return &Rule{
		Name:       config.Name,
		Expression: expr,
		Keywords:   config.Keywords,
		Priority:   priority,
		Enabled:    config.Enabled,
		Order:      index,
		Comment:    config.Comment,
	}, nil
}

// joinErrors joins error messages
func joinErrors(errors []string) string {
	result := ""
	for _, e := range errors {
		result += "  - " + e + "\n"
	}
	return result
}

// GetRuleNames returns all rule names
func (e *Engine) GetRuleNames() []string {
	names := make([]string, 0, len(e.Rules))
	for _, rule := range e.Rules {
		names = append(names, rule.Name)
	}
	return names
}

// GetEnabledRuleCount returns count of enabled rules
func (e *Engine) GetEnabledRuleCount() int {
	count := 0
	for _, rule := range e.Rules {
		if rule.Enabled {
			count++
		}
	}
	return count
}
