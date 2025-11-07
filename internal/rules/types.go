package rules

import (
	ac "github.com/anknown/ahocorasick"
)

// Priority levels for rule matches
type Priority string

const (
	PriorityCritical Priority = "critical"
	PriorityHigh     Priority = "high"
	PriorityMedium   Priority = "medium"
	PriorityLow      Priority = "low"
)

// Rule represents a single matching rule
type Rule struct {
	Name       string
	Expression Expression
	Keywords   string   // Original keywords expression string
	Priority   Priority
	Enabled    bool
	Order      int    // For sorting by priority
	Comment    string // Description/documentation for this rule
}

// RuleMatch represents a matched rule result
type RuleMatch struct {
	RuleName string
	Priority Priority
	Keywords []string // Keywords that triggered this rule
}

// Expression interface for boolean logic evaluation
type Expression interface {
	Evaluate(keywords map[string]bool) bool
	ExtractKeywords() []string         // Extract all keywords from this expression
	ExtractPositiveKeywords() []string // Extract only keywords NOT inside NOT expressions (for Aho-Corasick)
}

// KeywordExpr checks if a keyword exists in the set
type KeywordExpr struct {
	Keyword string
}

func (e KeywordExpr) Evaluate(keywords map[string]bool) bool {
	return keywords[e.Keyword]
}

func (e KeywordExpr) ExtractKeywords() []string {
	return []string{e.Keyword}
}

func (e KeywordExpr) ExtractPositiveKeywords() []string {
	return []string{e.Keyword}
}

// AndExpr represents logical AND
type AndExpr struct {
	Left  Expression
	Right Expression
}

func (e AndExpr) Evaluate(keywords map[string]bool) bool {
	// Short-circuit evaluation
	return e.Left.Evaluate(keywords) && e.Right.Evaluate(keywords)
}

func (e AndExpr) ExtractKeywords() []string {
	left := e.Left.ExtractKeywords()
	right := e.Right.ExtractKeywords()
	return append(left, right...)
}

func (e AndExpr) ExtractPositiveKeywords() []string {
	left := e.Left.ExtractPositiveKeywords()
	right := e.Right.ExtractPositiveKeywords()
	return append(left, right...)
}

// OrExpr represents logical OR
type OrExpr struct {
	Left  Expression
	Right Expression
}

func (e OrExpr) Evaluate(keywords map[string]bool) bool {
	// Short-circuit evaluation
	return e.Left.Evaluate(keywords) || e.Right.Evaluate(keywords)
}

func (e OrExpr) ExtractKeywords() []string {
	left := e.Left.ExtractKeywords()
	right := e.Right.ExtractKeywords()
	return append(left, right...)
}

func (e OrExpr) ExtractPositiveKeywords() []string {
	left := e.Left.ExtractPositiveKeywords()
	right := e.Right.ExtractPositiveKeywords()
	return append(left, right...)
}

// NotExpr represents logical NOT
type NotExpr struct {
	Expr Expression
}

func (e NotExpr) Evaluate(keywords map[string]bool) bool {
	return !e.Expr.Evaluate(keywords)
}

func (e NotExpr) ExtractKeywords() []string {
	return e.Expr.ExtractKeywords()
}

func (e NotExpr) ExtractPositiveKeywords() []string {
	// Do NOT include keywords from NOT expressions in Aho-Corasick machine
	// This prevents false matches on excluded terms
	return []string{}
}

// Engine holds all loaded rules and Aho-Corasick machine
type Engine struct {
	Rules    []*Rule
	Machine  ac.Machine // Aho-Corasick automaton built from rule keywords
	Keywords []string   // All unique keywords extracted from rules
}
