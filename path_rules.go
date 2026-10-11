// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package jamle

import (
	"strconv"
	"strings"

	goyaml "go.yaml.in/yaml/v3"
)

// pathRule stores a compiled path matching rule.
type pathRule struct {
	// segments contains literal path components and one-segment wildcards.
	segments []pathSegment
}

// pathSegment stores one literal path component or wildcard.
type pathSegment struct {
	// value is the literal component value.
	value string
	// wildcard matches any one path component.
	wildcard bool
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
		segment := rule.segments[i]
		if segment.wildcard {
			continue
		}
		if segment.value != path[i] {
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

		canonical := canonicalPathRule(segments)
		if _, ok := seen[canonical]; ok {
			continue
		}
		seen[canonical] = struct{}{}

		rules = append(rules, pathRule{segments: segments})
	}

	return rules
}

// splitPathPattern splits `a.b[0].c` into path segments and normalizes indexes.
// A backslash escapes the next character
// so literal delimiters and wildcards can be addressed, for example `a\.b.\*`.
func splitPathPattern(pattern string) []pathSegment {
	segments := make([]pathSegment, 0, 8)
	var current strings.Builder
	currentEscaped := false

	flushCurrent := func() {
		if current.Len() == 0 {
			return
		}

		value := current.String()
		segments = append(segments, pathSegment{
			value:    value,
			wildcard: value == "*" && !currentEscaped,
		})
		current.Reset()
		currentEscaped = false
	}

	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '\\' {
			if i+1 < len(pattern) {
				current.WriteByte(pattern[i+1])
				i++
			} else {
				current.WriteByte('\\')
			}
			currentEscaped = true
			continue
		}

		switch pattern[i] {
		case '.':
			flushCurrent()
		case '[':
			flushCurrent()

			token, end, escaped, ok := parseBracketSegment(pattern, i)
			if !ok {
				current.WriteByte('[')
				continue
			}

			token = strings.TrimSpace(token)
			if token == "" || (!escaped && token == "*") ||
				(!escaped && isNumericPathIndex(token)) {
				segments = append(segments, pathSegment{value: "*", wildcard: true})
			} else {
				segments = append(segments, pathSegment{value: token})
			}

			i = end
		default:
			current.WriteByte(pattern[i])
		}
	}

	flushCurrent()

	return segments
}

// parseBracketSegment parses one bracket segment and reports escaped content.
func parseBracketSegment(pattern string, start int) (string, int, bool, bool) {
	var token strings.Builder
	escaped := false
	for index := start + 1; index < len(pattern); index++ {
		switch pattern[index] {
		case '\\':
			if index+1 < len(pattern) {
				token.WriteByte(pattern[index+1])
				index++
			} else {
				token.WriteByte('\\')
			}
			escaped = true

		case ']':
			return token.String(), index, escaped, true

		default:
			token.WriteByte(pattern[index])
		}
	}

	return "", start, false, false
}

// canonicalPathRule creates a collision-free key for compiled path segments.
func canonicalPathRule(segments []pathSegment) string {
	var canonical strings.Builder
	for _, segment := range segments {
		canonical.WriteString(strconv.Itoa(len(segment.value)))
		canonical.WriteByte(':')
		if segment.wildcard {
			canonical.WriteByte('w')
		} else {
			canonical.WriteByte('l')
			canonical.WriteString(segment.value)
		}
		canonical.WriteByte(';')
	}

	return canonical.String()
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
