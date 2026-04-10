// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package jamle

import (
	"fmt"
	"strings"

	goyaml "go.yaml.in/yaml/v3"
)

// scalarRange describes one ${...} scalar segment in source string.
type scalarRange struct {
	start int
	end   int
}

// envLookup stores one environment variable lookup result.
type envLookup struct {
	value  string
	exists bool
}

// expandEnvInNode applies scalar env expansion to a parsed YAML document.
func expandEnvInNode(root *goyaml.Node, opts runtimeOptions) error {
	if len(opts.ignorePathRules) == 0 {
		return expandEnvInNodeFast(root, opts)
	}

	return expandEnvInNodeWithPath(root, nil, opts)
}

// expandEnvInNodeFast applies scalar env expansion without path tracking.
func expandEnvInNodeFast(root *goyaml.Node, opts runtimeOptions) error {
	var resolveErr error

	walkScalarNodes(root, func(n *goyaml.Node) {
		if resolveErr != nil {
			return
		}

		resolveErr = expandScalarNodeValue(n, opts)
	})

	return resolveErr
}

// walkScalarNodes walks YAML AST and calls fn for scalar nodes only.
func walkScalarNodes(n *goyaml.Node, fn func(*goyaml.Node)) {
	if n == nil {
		return
	}

	if n.Kind == goyaml.ScalarNode {
		fn(n)
	}

	for _, c := range n.Content {
		walkScalarNodes(c, fn)
	}
}

// expandEnvInNodeWithPath applies env expansion recursively with path tracking.
func expandEnvInNodeWithPath(n *goyaml.Node, path []string, opts runtimeOptions) error {
	if n == nil {
		return nil
	}

	switch n.Kind {
	case goyaml.DocumentNode:
		for _, child := range n.Content {
			if err := expandEnvInNodeWithPath(child, path, opts); err != nil {
				return err
			}
		}

		return nil

	case goyaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			keyNode := n.Content[i]
			valueNode := n.Content[i+1]

			nextPath := appendPathSegment(path, pathSegmentFromKeyNode(keyNode))

			if keyNode.Kind == goyaml.ScalarNode {
				if err := expandEnvInScalarNode(keyNode, nextPath, opts); err != nil {
					return err
				}

				nextPath[len(nextPath)-1] = keyNode.Value
			}

			if err := expandEnvInNodeWithPath(valueNode, nextPath, opts); err != nil {
				return err
			}
		}

		return nil

	case goyaml.SequenceNode:
		for _, child := range n.Content {
			nextPath := appendPathSegment(path, "*")
			if err := expandEnvInNodeWithPath(child, nextPath, opts); err != nil {
				return err
			}
		}

		return nil

	case goyaml.ScalarNode:
		return expandEnvInScalarNode(n, path, opts)

	default:
		for _, child := range n.Content {
			if err := expandEnvInNodeWithPath(child, path, opts); err != nil {
				return err
			}
		}

		return nil
	}
}

// expandEnvInScalarNode expands one scalar node value when path is not ignored.
func expandEnvInScalarNode(n *goyaml.Node, path []string, opts runtimeOptions) error {
	if shouldIgnorePath(path, opts.ignorePathRules) {
		return nil
	}

	return expandScalarNodeValue(n, opts)
}

// expandScalarNodeValue expands scalar value and restores implicit YAML tags.
func expandScalarNodeValue(n *goyaml.Node, opts runtimeOptions) error {
	oldStyle := n.Style
	oldTag := n.Tag
	oldValue := n.Value

	out, err := expandEnvInScalar(n.Value, opts)
	if err != nil {
		return err
	}

	n.Value = out

	// If scalar was plain and implicitly !!str due to ${...},
	// clear the tag so YAML can re-resolve native scalar types.
	if oldStyle == 0 && oldTag == "!!str" && oldValue != n.Value {
		n.Tag = ""
	}

	return nil
}

// expandEnvInScalar expands ${...} variables using resolved options.
func expandEnvInScalar(in string, opts runtimeOptions) (string, error) {
	if !strings.Contains(in, "${") {
		return in, nil
	}

	str := maskEscapedVars(in)
	envCache := make(map[string]envLookup)
	setter, _ := opts.resolver.(Setter)
	if !opts.allowAssignment {
		setter = nil
	}

	// Main expansion loop.
	for range opts.maxPasses {
		replacement, changed, err := replaceInnermostVars(
			str,
			envCache,
			opts.resolver,
			setter,
			opts.allowAssignment,
			opts.enforceRequired,
		)
		if err != nil {
			return "", err
		}

		if !changed {
			break
		}

		str = replacement
	}

	if strings.IndexByte(str, maskStart[0]) < 0 {
		return str, nil
	}

	// Unmask \x00VAR\x01 -> ${VAR}
	return unmaskReplacer.Replace(str), nil
}

// maskEscapedVars replaces $${...} segments (with balanced braces) by masked
// markers, so later expansion ignores them and unmasking restores literal ${...}.
func maskEscapedVars(in string) string {
	if !strings.Contains(in, "$${") {
		return in
	}

	var out strings.Builder
	out.Grow(len(in))

	for i := 0; i < len(in); {
		if i+2 >= len(in) || in[i] != '$' || in[i+1] != '$' || in[i+2] != '{' {
			out.WriteByte(in[i])
			i++
			continue
		}

		j, ok := findClosingBraceIndex(in, i+2)
		if !ok {
			out.WriteByte(in[i])
			i++
			continue
		}

		out.WriteString(maskStart)
		escapedContent := strings.ReplaceAll(in[i+3:j], "${", maskStart)
		out.WriteString(escapedContent)
		out.WriteString(maskEnd)
		i = j + 1
	}

	return out.String()
}

// findClosingBraceIndex finds matching '}' for an opening '{' at openPos.
func findClosingBraceIndex(in string, openPos int) (int, bool) {
	if openPos < 0 || openPos >= len(in) || in[openPos] != '{' {
		return 0, false
	}

	depth := 1

	for i := openPos + 1; i < len(in); i++ {
		switch in[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}

	return 0, false
}

// replaceInnermostVars resolves non-escaped innermost ${...} expressions.
func replaceInnermostVars(
	in string,
	envCache map[string]envLookup,
	resolver Resolver,
	setter Setter,
	allowAssignment bool,
	enforceRequired bool,
) (string, bool, error) {
	ranges := findInnermostVarRanges(in)
	if len(ranges) == 0 {
		return in, false, nil
	}

	var out strings.Builder
	out.Grow(len(in))

	changed := false
	cursor := 0
	for _, r := range ranges {
		if r.start > 0 && in[r.start-1] == '$' {
			continue
		}

		content := in[r.start+2 : r.end]
		resolved, err := resolveVariable(
			content,
			envCache,
			resolver,
			setter,
			allowAssignment,
			enforceRequired,
		)
		if err != nil {
			return "", false, err
		}

		out.WriteString(in[cursor:r.start])
		out.WriteString(resolved)
		cursor = r.end + 1
		changed = true
	}

	if !changed {
		return in, false, nil
	}

	out.WriteString(in[cursor:])
	return out.String(), true, nil
}

// findInnermostVarRanges finds innermost ${...} sections in the input string.
func findInnermostVarRanges(in string) []scalarRange {
	stack := make([]int, 0, 8)
	ranges := make([]scalarRange, 0, 8)
	hasChild := make([]bool, 0, 8)

	for i := 0; i < len(in); i++ {
		if i+1 < len(in) && in[i] == '$' && in[i+1] == '{' {
			if len(hasChild) > 0 {
				hasChild[len(hasChild)-1] = true
			}

			stack = append(stack, i)
			hasChild = append(hasChild, false)
			i++
			continue
		}

		if in[i] != '}' || len(stack) == 0 {
			continue
		}

		start := stack[len(stack)-1]
		child := hasChild[len(hasChild)-1]
		stack = stack[:len(stack)-1]
		hasChild = hasChild[:len(hasChild)-1]

		if child {
			continue
		}

		ranges = append(ranges, scalarRange{start: start, end: i})
	}

	return ranges
}

// resolveVariable parses the content inside ${...} and applies Bash-style logic.
// It handles default values, assignments, and error enforcement.
func resolveVariable(
	content string,
	envCache map[string]envLookup,
	resolver Resolver,
	setter Setter,
	allowAssignment bool,
	enforceRequired bool,
) (string, error) {
	name, val, hasColon := strings.Cut(content, ":")
	envVal, exists := lookupEnvWithCache(name, envCache, resolver)

	// Case 1: Simple variable ${VAR}
	if !hasColon {
		if exists {
			return envVal, nil
		}

		return "", nil
	}

	// Case 2: Variable with empty default ${VAR:}
	if val == "" {
		if exists {
			return envVal, nil
		}

		return "", nil
	}

	// Case 3: Variable with operator and value
	var operator byte
	var defaultVal string

	// Check the first character after the colon
	switch val[0] {
	case '-', '=', '?':
		operator = val[0]
		defaultVal = val[1:]

	default:
		// Bash-style note: ${VAR:default} is not a default-value operator.
		// It should not behave like ${VAR:-default}. Treat it as plain ${VAR}
		// in this simplified expansion model (no default substitution).
		if exists {
			return envVal, nil
		}

		return "", nil
	}

	switch operator {
	case '-': // ${VAR:-default} -> use default if unset or empty
		if exists && envVal != "" {
			return envVal, nil
		}
		return defaultVal, nil

	case '=': // ${VAR:=default} -> use default if unset and set env
		if exists && envVal != "" {
			return envVal, nil
		}

		if !allowAssignment {
			return defaultVal, nil
		}

		if setter == nil {
			return "", fmt.Errorf("%w for %q", ErrAssignmentUnsupported, name)
		}

		if err := setter.Set(name, defaultVal); err != nil {
			return "", fmt.Errorf("failed to set variable %s: %w", name, err)
		}

		envCache[name] = envLookup{value: defaultVal, exists: true}

		return defaultVal, nil

	case '?': // ${VAR:?message} -> error
		if !enforceRequired {
			if exists {
				return envVal, nil
			}

			return "", nil
		}

		if exists && envVal != "" {
			return envVal, nil
		}

		msg := defaultVal
		if msg == "" {
			msg = "is not set or empty"
		}

		return "", fmt.Errorf("environment variable %q %s", name, msg)
	}

	return "", nil
}

// lookupEnvWithCache reads env variable once per scalar expansion.
func lookupEnvWithCache(
	name string,
	cache map[string]envLookup,
	resolver Resolver,
) (string, bool) {
	if got, ok := cache[name]; ok {
		return got.value, got.exists
	}

	value, exists := resolver.Lookup(name)
	cache[name] = envLookup{value: value, exists: exists}
	return value, exists
}
