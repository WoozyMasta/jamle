// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package jamle

import (
	"errors"
	"fmt"
	"strings"

	goyaml "go.yaml.in/yaml/v3"
)

// envLookup stores one resolver lookup result for a scalar expansion.
type envLookup struct {
	// value is the value returned by Resolver.Lookup.
	value string
	// exists distinguishes an unset variable from a set variable with an empty value.
	exists bool
}

// templateSegment is either literal text or one parsed expression.
type templateSegment struct {
	// expression is set for an expandable segment.
	expression *parsedExpression
	// literal is set for text that must be copied without evaluation.
	literal string
}

// parsedExpression stores one supported parameter expansion.
type parsedExpression struct {
	// name is the variable passed to Resolver.Lookup and, for :=, Setter.Set.
	name string
	// word contains the lazy fallback or required-error message.
	word []templateSegment
	// operator is '-', '=', '?' or zero for a plain ${VAR} lookup.
	operator byte
}

// expansionParser parses balanced expansion expressions without evaluating them.
type expansionParser struct {
	// input is the scalar currently being parsed.
	input string
	// maxDepth bounds nested expressions in input.
	maxDepth int
}

// expansionEvaluator resolves parsed expressions with lazy branch evaluation.
type expansionEvaluator struct {
	// setter is available only when assignment side effects are enabled and the resolver implements Setter.
	setter Setter
	// envCache avoids repeated lookups within one scalar expansion.
	envCache map[string]envLookup
	// active marks variables currently being evaluated.
	active map[string]bool
	// stack preserves the evaluation path for cycle diagnostics.
	stack []string
	// opts contains the normalized resolver and expansion policies.
	opts runtimeOptions
}

// expandEnvInNode applies scalar environment expansion to a parsed YAML document.
func expandEnvInNode(root *goyaml.Node, opts runtimeOptions) error {
	if len(opts.ignorePathRules) == 0 {
		return expandEnvInNodeFast(root, opts)
	}

	return expandEnvInNodeWithPath(root, nil, opts)
}

// expandEnvInNodeFast applies env expansion without tracking YAML key paths.
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

	for _, child := range n.Content {
		walkScalarNodes(child, fn)
	}
}

// expandEnvInNodeWithPath expands values while retaining their YAML key paths.
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

// expandEnvInScalarNode expands one scalar unless its path is ignored.
func expandEnvInScalarNode(n *goyaml.Node, path []string, opts runtimeOptions) error {
	if shouldIgnorePath(path, opts.ignorePathRules) {
		return nil
	}

	return expandScalarNodeValue(n, opts)
}

// expandScalarNodeValue expands a scalar and restores implicit YAML typing.
func expandScalarNodeValue(n *goyaml.Node, opts runtimeOptions) error {
	oldStyle := n.Style
	oldTag := n.Tag
	oldValue := n.Value

	out, err := expandEnvInScalar(n.Value, opts)
	if err != nil {
		return err
	}

	n.Value = out
	// Plain scalars with an implicit string tag must be re-resolved
	// after expansion so values such as ${PORT:-8080} can regain their native type.
	if oldStyle == 0 && oldTag == "!!str" && oldValue != n.Value {
		n.Tag = ""
	}

	return nil
}

// expandEnvInScalar parses and lazily evaluates one scalar value.
// Parsing is done before evaluation so malformed expandable syntax fails without partial output;
// unused words of := and :? expressions are never evaluated.
func expandEnvInScalar(in string, opts runtimeOptions) (string, error) {
	if !strings.Contains(in, "${") {
		return in, nil
	}
	if opts.maxDepth <= 0 {
		opts.maxDepth = defaultMaxDepth
	}
	if opts.resolver == nil {
		opts.resolver = envResolver{}
	}

	segments, err := parseExpansionTemplate(in, opts.maxDepth)
	if err != nil {
		return "", err
	}

	evaluator := expansionEvaluator{
		opts:     opts,
		envCache: make(map[string]envLookup),
		active:   make(map[string]bool),
	}
	if opts.allowAssignment {
		evaluator.setter, _ = opts.resolver.(Setter)
	}

	return evaluator.evaluateTemplate(segments, 1)
}

// parseExpansionTemplate parses literal and expression segments in input.
// Escaped $${...} sequences become literal ${...} segments and are not evaluated;
// an unmatched escaped expression remains literal.
func parseExpansionTemplate(input string, maxDepth int) ([]templateSegment, error) {
	parser := expansionParser{input: input, maxDepth: maxDepth}
	segments := make([]templateSegment, 0, 4)
	literalStart := 0

	flushLiteral := func(end int) {
		if end <= literalStart {
			return
		}
		segments = appendTemplateLiteral(segments, input[literalStart:end])
	}

	for i := 0; i < len(input); {
		if i+2 < len(input) && input[i] == '$' && input[i+1] == '$' && input[i+2] == '{' {
			// Keep $${...} literal, but parse it to locate its balanced end.
			_, end, err := parser.parseExpression(i+1, 1)
			if err != nil {
				if errors.Is(err, ErrExpansionSyntax) {
					flushLiteral(len(input))
					return segments, nil
				}

				return nil, err
			}

			flushLiteral(i)
			segments = appendTemplateLiteral(segments, input[i+1:end])
			i = end
			literalStart = end
			continue
		}

		if i+1 < len(input) && input[i] == '$' && input[i+1] == '{' {
			expression, end, err := parser.parseExpression(i, 1)
			if err != nil {
				return nil, err
			}

			flushLiteral(i)
			segments = append(segments, templateSegment{expression: expression})
			i = end
			literalStart = end
			continue
		}

		i++
	}

	flushLiteral(len(input))
	return segments, nil
}

// parseExpression parses one ${...} expression and returns its end offset.
// Only -, =, and ? after a colon are operators;
// other forms retain the historical plain-variable behavior.
func (p expansionParser) parseExpression(start, depth int) (*parsedExpression, int, error) {
	if start < 0 || start+1 >= len(p.input) || p.input[start:start+2] != "${" {
		return nil, 0, expansionSyntaxError(start)
	}
	if depth > p.maxDepth {
		return nil, 0, fmt.Errorf("%w: maximum depth %d exceeded", ErrExpansionDepth, p.maxDepth)
	}

	nameStart := start + 2
	for i := nameStart; i < len(p.input); i++ {
		switch p.input[i] {
		case '}':
			return &parsedExpression{name: p.input[nameStart:i]}, i + 1, nil

		case ':':
			expression := &parsedExpression{name: p.input[nameStart:i]}
			wordStart := i + 1
			if wordStart < len(p.input) && strings.ContainsRune("-=?", rune(p.input[wordStart])) {
				expression.operator = p.input[wordStart]
				wordStart++
			}

			word, end, err := p.parseWord(wordStart, depth)
			if err != nil {
				return nil, 0, err
			}
			expression.word = word

			return expression, end, nil

		case '{':
			return nil, 0, expansionSyntaxError(i)
		}
	}

	return nil, 0, expansionSyntaxError(start)
}

// parseWord parses an operator word until its balanced expression close.
// Nested expressions are parsed into segments but evaluated later by the selected operator branch.
func (p expansionParser) parseWord(start, depth int) ([]templateSegment, int, error) {
	segments := make([]templateSegment, 0, 2)
	literalStart := start
	braceDepth := 0

	flushLiteral := func(end int) {
		if end <= literalStart {
			return
		}
		segments = appendTemplateLiteral(segments, p.input[literalStart:end])
	}

	for i := start; i < len(p.input); {
		if i+2 < len(p.input) && p.input[i] == '$' && p.input[i+1] == '$' && p.input[i+2] == '{' {
			// Keep nested $${...} literal while finding its balanced end.
			_, end, err := p.parseExpression(i+1, depth+1)
			if err != nil {
				return nil, 0, err
			}

			flushLiteral(i)
			segments = appendTemplateLiteral(segments, p.input[i+1:end])
			i = end
			literalStart = end
			continue
		}

		if i+1 < len(p.input) && p.input[i] == '$' && p.input[i+1] == '{' {
			expression, end, err := p.parseExpression(i, depth+1)
			if err != nil {
				return nil, 0, err
			}

			flushLiteral(i)
			segments = append(segments, templateSegment{expression: expression})
			i = end
			literalStart = end
			continue
		}

		switch p.input[i] {
		case '{':
			braceDepth++

		case '}':
			if braceDepth > 0 {
				braceDepth--
				i++
				continue
			}

			flushLiteral(i)
			return segments, i + 1, nil
		}

		i++
	}

	return nil, 0, expansionSyntaxError(start)
}

// appendTemplateLiteral appends literal text and coalesces adjacent segments.
func appendTemplateLiteral(segments []templateSegment, literal string) []templateSegment {
	if literal == "" {
		return segments
	}

	if len(segments) > 0 && segments[len(segments)-1].expression == nil {
		segments[len(segments)-1].literal += literal
		return segments
	}

	return append(segments, templateSegment{literal: literal})
}

// expansionSyntaxError annotates malformed expansion syntax with its offset.
func expansionSyntaxError(offset int) error {
	return fmt.Errorf("%w at byte %d", ErrExpansionSyntax, offset)
}

// evaluateTemplate evaluates segments while keeping unused words untouched.
func (e *expansionEvaluator) evaluateTemplate(
	segments []templateSegment,
	depth int,
) (string, error) {
	var out strings.Builder

	for _, segment := range segments {
		if segment.expression == nil {
			out.WriteString(segment.literal)
			continue
		}

		value, err := e.evaluateExpression(segment.expression, depth)
		if err != nil {
			return "", err
		}

		out.WriteString(value)
	}

	return out.String(), nil
}

// evaluateExpression applies one operator after resolving only the used branch.
func (e *expansionEvaluator) evaluateExpression(
	expression *parsedExpression,
	depth int,
) (string, error) {
	if depth > e.opts.maxDepth {
		return "", fmt.Errorf("%w: maximum depth %d exceeded", ErrExpansionDepth, e.opts.maxDepth)
	}

	if e.active[expression.name] {
		cycle := append(append([]string(nil), e.stack...), expression.name)
		return "", fmt.Errorf("%w: %s", ErrExpansionCycle, strings.Join(cycle, " -> "))
	}

	e.active[expression.name] = true
	e.stack = append(e.stack, expression.name)
	defer func() {
		delete(e.active, expression.name)
		e.stack = e.stack[:len(e.stack)-1]
	}()

	value, exists := e.lookup(expression.name)
	switch expression.operator {
	case 0:
		// A plain lookup returns an empty string for an unset variable.
		if !exists {
			return "", nil
		}
		return e.evaluateResolvedValue(value, depth)

	case '-':
		// Use the word only for an unset or empty variable.
		if exists && value != "" {
			return e.evaluateResolvedValue(value, depth)
		}
		return e.evaluateTemplate(expression.word, depth+1)

	case '=':
		// := shares the :- fallback decision,
		// then optionally persists the resolved fallback through Setter.
		if exists && value != "" {
			return e.evaluateResolvedValue(value, depth)
		}

		resolved, err := e.evaluateTemplate(expression.word, depth+1)
		if err != nil {
			return "", err
		}
		if !e.opts.allowAssignment {
			return resolved, nil
		}
		if e.setter == nil {
			return "", fmt.Errorf("%w for %q", ErrAssignmentUnsupported, expression.name)
		}
		if err := e.setter.Set(expression.name, resolved); err != nil {
			return "", fmt.Errorf("failed to set variable %s: %w", expression.name, err)
		}

		e.envCache[expression.name] = envLookup{value: resolved, exists: true}
		return resolved, nil

	case '?':
		// A required expression checks the value before evaluating its diagnostic word,
		// preserving lazy error branches.
		if exists && value != "" {
			return e.evaluateResolvedValue(value, depth)
		}
		if !e.opts.enforceRequired {
			if !exists {
				return "", nil
			}
			return e.evaluateResolvedValue(value, depth)
		}

		message, err := e.evaluateTemplate(expression.word, depth+1)
		if err != nil {
			return "", err
		}
		if message == "" {
			message = "is not set or empty"
		}

		return "", fmt.Errorf("environment variable %q %s", expression.name, message)

	default:
		// `${VAR:word}` is intentionally outside the supported subset
		// and behaves like a plain variable for compatibility.
		if !exists {
			return "", nil
		}
		return e.evaluateResolvedValue(value, depth)
	}
}

// evaluateResolvedValue recursively expands resolver values when needed.
func (e *expansionEvaluator) evaluateResolvedValue(value string, depth int) (string, error) {
	if !strings.Contains(value, "${") {
		return value, nil
	}

	segments, err := parseExpansionTemplate(value, e.opts.maxDepth)
	if err != nil {
		return "", err
	}

	return e.evaluateTemplate(segments, depth+1)
}

// lookup reads one variable through the per-scalar resolver cache.
func (e *expansionEvaluator) lookup(name string) (string, bool) {
	if value, ok := e.envCache[name]; ok {
		return value.value, value.exists
	}

	value, exists := e.opts.resolver.Lookup(name)
	e.envCache[name] = envLookup{value: value, exists: exists}
	return value, exists
}
