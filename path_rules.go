// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package jamle

import (
	"strings"

	goyaml "go.yaml.in/yaml/v3"
)

// pathRule stores a compiled path matching rule.
type pathRule struct {
	segments []string
}

// shouldIgnorePath reports whether path matches at least one ignore rule.
func shouldIgnorePath(path []string, rules []pathRule) bool {
	for _, rule := range rules {
		if pathRuleMatches(path, rule) {
			return true
		}
	}

	return false
}

// pathRuleMatches reports whether compiled rule matches a scalar path.
func pathRuleMatches(path []string, rule pathRule) bool {
	if len(path) != len(rule.segments) {
		return false
	}

	for i := range path {
		if rule.segments[i] == "*" {
			continue
		}
		if rule.segments[i] != path[i] {
			return false
		}
	}

	return true
}

// pathSegmentFromKeyNode extracts one path segment from mapping key node.
func pathSegmentFromKeyNode(n *goyaml.Node) string {
	if n == nil {
		return "*"
	}
	if n.Kind != goyaml.ScalarNode {
		return "*"
	}

	return n.Value
}

// appendPathSegment returns a new path with one additional segment.
func appendPathSegment(path []string, segment string) []string {
	next := make([]string, len(path)+1)
	copy(next, path)
	next[len(path)] = segment
	return next
}

// compilePathRules parses and deduplicates path rules once per call.
func compilePathRules(patterns []string) []pathRule {
	rules := make([]pathRule, 0, len(patterns))
	seen := make(map[string]struct{}, len(patterns))

	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}

		segments := splitPathPattern(pattern)
		if len(segments) == 0 {
			continue
		}

		canonical := strings.Join(segments, ".")
		if _, ok := seen[canonical]; ok {
			continue
		}
		seen[canonical] = struct{}{}

		rules = append(rules, pathRule{segments: segments})
	}

	return rules
}

// splitPathPattern splits `a.b[0].c` into path segments and normalizes indexes.
func splitPathPattern(pattern string) []string {
	segments := make([]string, 0, 8)
	var current strings.Builder

	flushCurrent := func() {
		if current.Len() == 0 {
			return
		}

		segments = append(segments, current.String())
		current.Reset()
	}

	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '.':
			flushCurrent()
		case '[':
			flushCurrent()

			end := strings.IndexByte(pattern[i:], ']')
			if end <= 0 {
				continue
			}

			token := strings.TrimSpace(pattern[i+1 : i+end])
			if token == "" || token == "*" || isNumericPathIndex(token) {
				segments = append(segments, "*")
			} else {
				segments = append(segments, token)
			}

			i += end
		default:
			current.WriteByte(pattern[i])
		}
	}

	flushCurrent()

	return segments
}

// isNumericPathIndex reports whether token contains only decimal digits.
func isNumericPathIndex(token string) bool {
	if token == "" {
		return false
	}

	for i := 0; i < len(token); i++ {
		if token[i] < '0' || token[i] > '9' {
			return false
		}
	}

	return true
}
