package rules

import (
	"fmt"
	"strings"
)

// Parse parses a boolean expression string into an Expression AST
// Operator precedence: NOT > AND > OR
// Parentheses can be used for explicit grouping
// Example: "login AND lufthansa AND NOT amazon"
func Parse(expr string) (Expression, error) {
	tokens := tokenize(expr)
	if len(tokens) == 0 {
		return nil, fmt.Errorf("empty expression")
	}

	stream := &tokenStream{tokens: tokens, pos: 0}
	result, err := parseOr(stream)
	if err != nil {
		return nil, err
	}

	if !stream.isEOF() {
		return nil, fmt.Errorf("unexpected token: %s", stream.peek())
	}

	return result, nil
}

// tokenStream helps with parsing
type tokenStream struct {
	tokens []string
	pos    int
}

func (s *tokenStream) peek() string {
	if s.pos >= len(s.tokens) {
		return ""
	}
	return s.tokens[s.pos]
}

func (s *tokenStream) consume() string {
	token := s.peek()
	s.pos++
	return token
}

func (s *tokenStream) isEOF() bool {
	return s.pos >= len(s.tokens)
}

// tokenize splits expression into tokens
func tokenize(expr string) []string {
	expr = strings.TrimSpace(expr)
	var tokens []string
	var current strings.Builder

	for i := 0; i < len(expr); i++ {
		ch := expr[i]

		switch ch {
		case '(', ')':
			// Save current token if any
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			tokens = append(tokens, string(ch))

		case ' ', '\t', '\n':
			// Whitespace - save current token
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}

		default:
			current.WriteByte(ch)
		}
	}

	// Save last token
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return tokens
}

// parseOr handles OR expressions (lowest precedence)
func parseOr(stream *tokenStream) (Expression, error) {
	left, err := parseAnd(stream)
	if err != nil {
		return nil, err
	}

	for stream.peek() == "OR" {
		stream.consume() // consume "OR"
		right, err := parseAnd(stream)
		if err != nil {
			return nil, err
		}
		left = OrExpr{Left: left, Right: right}
	}

	return left, nil
}

// parseAnd handles AND expressions
func parseAnd(stream *tokenStream) (Expression, error) {
	left, err := parseNot(stream)
	if err != nil {
		return nil, err
	}

	for stream.peek() == "AND" {
		stream.consume() // consume "AND"
		right, err := parseNot(stream)
		if err != nil {
			return nil, err
		}
		left = AndExpr{Left: left, Right: right}
	}

	return left, nil
}

// parseNot handles NOT expressions (highest precedence)
func parseNot(stream *tokenStream) (Expression, error) {
	if stream.peek() == "NOT" {
		stream.consume() // consume "NOT"
		expr, err := parseNot(stream) // Allow chaining: NOT NOT keyword
		if err != nil {
			return nil, err
		}
		return NotExpr{Expr: expr}, nil
	}

	return parsePrimary(stream)
}

// parsePrimary handles keywords and parenthesized expressions
func parsePrimary(stream *tokenStream) (Expression, error) {
	token := stream.peek()

	if token == "" {
		return nil, fmt.Errorf("unexpected end of expression")
	}

	// Handle parentheses
	if token == "(" {
		stream.consume() // consume "("
		expr, err := parseOr(stream) // Start from OR (lowest precedence)
		if err != nil {
			return nil, err
		}

		if stream.peek() != ")" {
			return nil, fmt.Errorf("expected ')', got '%s'", stream.peek())
		}
		stream.consume() // consume ")"
		return expr, nil
	}

	// Handle unexpected closing parenthesis
	if token == ")" {
		return nil, fmt.Errorf("unexpected ')'")
	}

	// Must be a keyword
	keyword := stream.consume()

	// Validate keyword doesn't contain special characters
	if strings.ContainsAny(keyword, "()") {
		return nil, fmt.Errorf("invalid keyword: %s", keyword)
	}

	return KeywordExpr{Keyword: strings.ToLower(keyword)}, nil
}
