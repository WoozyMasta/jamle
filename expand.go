// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package jamle

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/woozymasta/jamle/internal/schema"
	goyaml "go.yaml.in/yaml/v3"
)

// envLookup stores one resolver lookup result for a scalar expansion.
type envLookup struct {
	// value is the value returned by [Resolver.Lookup].
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
	// name is passed to [Resolver.Lookup] and, for :=, [Setter.Set].
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
	// setter is available only when assignment side effects are enabled
	// and the resolver implements [Setter].
	setter Setter
	// envCache avoids repeated lookups within one scalar expansion.
	envCache map[string]envLookup
	// active marks resolver values currently being evaluated.
	active map[string]bool
	// stack preserves the resolver evaluation path for cycle diagnostics.
	stack []string
	// opts contains the normalized resolver and expansion policies.
	opts runtimeOptions
}

// errAliasCycle reports recursive traversal of a YAML alias graph.
var errAliasCycle = errors.New("yaml alias cycle")

// expandEnvInNode applies schema-aware scalar expansion to a parsed YAML document.
// target is the destination type used to resolve field policies.
func expandEnvInNode(root *goyaml.Node, target reflect.Type, opts runtimeOptions) error {
	transformer := logicalNodeTransformer{
		opts:   opts,
		active: make(map[*goyaml.Node]bool),
	}

	transformed, err := transformer.transform(root, nil, target, true)
	if err != nil {
		return err
	}

	*root = *transformed
	return nil
}

// logicalNodeTransformer applies path-sensitive transforms
// without mutating shared YAML nodes referenced by aliases.
type logicalNodeTransformer struct {
	// active tracks the current node path and rejects recursive aliases.
	active map[*goyaml.Node]bool
	// opts contains the normalized resolver and expansion policies.
	opts runtimeOptions
}

// transform copies one logical YAML value for its consumer path.
func (t *logicalNodeTransformer) transform(
	n *goyaml.Node,
	path []string,
	target reflect.Type,
	expand bool,
) (*goyaml.Node, error) {
	if n == nil {
		return nil, nil
	}
	if t.active[n] {
		return nil, newExpansionSourceError(path, n.Line, n.Column, errAliasCycle)
	}

	t.active[n] = true
	defer delete(t.active, n)

	target = dereferenceExpansionType(target)
	if schema.IsOpaque(target) &&
		(!schema.IsTextUnmarshaler(target) ||
			schema.IsYAMLUnmarshaler(target) ||
			n.Kind != goyaml.ScalarNode) {
		// Text unmarshallers consume scalar text after expansion;
		// node-owned decoders still receive an untouched subtree.
		cloned, err := cloneOpaqueGraph(n)
		if err != nil {
			return nil, newExpansionSourceError(path, n.Line, n.Column, err)
		}

		return cloned, nil
	}

	if n.Kind == goyaml.AliasNode {
		if n.Alias == nil {
			return copyLogicalNode(n), nil
		}

		return t.transform(n.Alias, path, target, expand)
	}

	cloned := copyLogicalNode(n)
	switch n.Kind {
	case goyaml.DocumentNode:
		cloned.Content = make([]*goyaml.Node, len(n.Content))
		for index, child := range n.Content {
			transformed, err := t.transform(child, path, target, expand)
			if err != nil {
				return nil, err
			}
			cloned.Content[index] = transformed
		}

	case goyaml.MappingNode:
		return t.transformMapping(cloned, path, target, expand)

	case goyaml.SequenceNode:
		elementType := sequenceElementType(target)
		cloned.Content = make([]*goyaml.Node, len(n.Content))
		for index, child := range n.Content {
			nextPath := appendPathSegment(path, "*")
			transformed, err := t.transform(child, nextPath, elementType, expand)
			if err != nil {
				return nil, err
			}
			cloned.Content[index] = transformed
		}

	case goyaml.ScalarNode:
		if expand && !shouldIgnorePath(path, t.opts.ignorePathRules) {
			if err := expandScalarNodeValue(cloned, t.opts); err != nil {
				return nil, newExpansionSourceError(path, n.Line, n.Column, err)
			}
		}

	default:
		cloned.Content = make([]*goyaml.Node, len(n.Content))
		for index, child := range n.Content {
			transformed, err := t.transform(child, path, target, expand)
			if err != nil {
				return nil, err
			}
			cloned.Content[index] = transformed
		}
	}

	return cloned, nil
}

// transformMapping copies mapping values using their logical consumer paths.
func (t *logicalNodeTransformer) transformMapping(
	cloned *goyaml.Node,
	path []string,
	target reflect.Type,
	expand bool,
) (*goyaml.Node, error) {
	target = dereferenceExpansionType(target)
	var fields *schema.Info
	var mapElement reflect.Type

	if target != nil {
		switch target.Kind() {
		case reflect.Struct:
			fields = schema.For(target)
		case reflect.Map:
			mapElement = target.Elem()
		}
	}

	content := cloned.Content
	cloned.Content = make([]*goyaml.Node, len(content))
	for i := 0; i+1 < len(content); i += 2 {
		keyNode := content[i]
		if keyNode == nil {
			return nil, fmt.Errorf("nil YAML mapping key at path %q", strings.Join(path, "."))
		}

		if keyNode.Kind == goyaml.AliasNode && keyNode.Alias != nil {
			transformed, err := t.transform(keyNode, path, nil, false)
			if err != nil {
				return nil, err
			}

			keyNode = transformed
		} else if expand && t.opts.expandKeys && keyNode.Kind == goyaml.ScalarNode &&
			!isMergeKey(keyNode) {
			keyNode = copyLogicalNode(keyNode)
		}

		cloned.Content[i] = keyNode
		valueNode := content[i+1]

		if expand && t.opts.expandKeys && keyNode.Kind == goyaml.ScalarNode &&
			!isMergeKey(keyNode) {
			keyPath := appendPathSegment(path, keyNode.Value)
			if !shouldIgnorePath(keyPath, t.opts.ignorePathRules) {
				if err := expandScalarNodeValue(keyNode, t.opts); err != nil {
					return nil, newExpansionSourceError(keyPath, keyNode.Line, keyNode.Column, err)
				}
			}
		}

		nextPath := appendPathSegment(path, pathSegmentFromKeyNode(keyNode))
		var field schema.Field
		var fieldFound bool
		if fields != nil && keyNode.Kind == goyaml.ScalarNode {
			field, fieldFound = fields.Lookup(keyNode.Value)
		}

		if isMergeKey(keyNode) {
			transformed, err := t.transformMergeValue(valueNode, path, target, expand)
			if err != nil {
				return nil, err
			}

			cloned.Content[i+1] = transformed
			continue
		}

		childType := mapElement
		if fieldFound {
			childType = field.Type
		}

		childExpand := expand && (!fieldFound || !field.NoExpand)
		transformed, err := t.transform(valueNode, nextPath, childType, childExpand)
		if err != nil {
			return nil, err
		}

		cloned.Content[i+1] = transformed
	}

	return cloned, nil
}

// transformMergeValue traverses merge sources at the destination mapping path.
func (t *logicalNodeTransformer) transformMergeValue(
	n *goyaml.Node,
	path []string,
	target reflect.Type,
	expand bool,
) (*goyaml.Node, error) {
	if n == nil {
		return nil, nil
	}
	if n.Kind != goyaml.SequenceNode {
		return t.transform(n, path, target, expand)
	}

	cloned := copyLogicalNode(n)
	cloned.Content = make([]*goyaml.Node, len(n.Content))
	for index, child := range n.Content {
		transformed, err := t.transformMergeValue(child, path, target, expand)
		if err != nil {
			return nil, err
		}
		cloned.Content[index] = transformed
	}

	return cloned, nil
}

// copyLogicalNode copies node metadata without retaining shared graph edges.
func copyLogicalNode(n *goyaml.Node) *goyaml.Node {
	nodeCopy := *n
	nodeCopy.Anchor = ""
	nodeCopy.Content = append([]*goyaml.Node(nil), n.Content...)
	nodeCopy.Alias = nil
	return &nodeCopy
}

// cloneOpaqueGraph copies a decoder-owned subtree while preserving aliases.
func cloneOpaqueGraph(root *goyaml.Node) (*goyaml.Node, error) {
	clones := make(map[*goyaml.Node]*goyaml.Node)
	active := make(map[*goyaml.Node]bool)

	var clone func(*goyaml.Node) (*goyaml.Node, error)
	clone = func(n *goyaml.Node) (*goyaml.Node, error) {
		if n == nil {
			return nil, nil
		}
		if active[n] {
			return nil, errAliasCycle
		}

		if existing, ok := clones[n]; ok {
			return existing, nil
		}

		copied := *n
		copied.Content = nil
		copied.Alias = nil
		clones[n] = &copied
		active[n] = true

		if len(n.Content) > 0 {
			copied.Content = make([]*goyaml.Node, len(n.Content))
			for index, child := range n.Content {
				childCopy, err := clone(child)
				if err != nil {
					return nil, err
				}

				copied.Content[index] = childCopy
			}
		}

		aliasCopy, err := clone(n.Alias)
		if err != nil {
			return nil, err
		}

		copied.Alias = aliasCopy
		delete(active, n)

		return &copied, nil
	}

	return clone(root)
}

// isMergeKey reports whether n is a YAML merge mapping key.
func isMergeKey(n *goyaml.Node) bool {
	if n == nil || n.Kind != goyaml.ScalarNode || n.Value != "<<" {
		return false
	}

	return n.Tag == "" || n.Tag == "!" || n.Tag == "!!merge" ||
		n.Tag == "tag:yaml.org,2002:merge"
}

// sequenceElementType returns the destination element type for a sequence.
func sequenceElementType(target reflect.Type) reflect.Type {
	target = dereferenceExpansionType(target)
	if target == nil || (target.Kind() != reflect.Slice && target.Kind() != reflect.Array) {
		return nil
	}

	return target.Elem()
}

// dereferenceExpansionType strips pointer layers for schema traversal.
func dereferenceExpansionType(target reflect.Type) reflect.Type {
	for target != nil && target.Kind() == reflect.Pointer {
		target = target.Elem()
	}

	return target
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
	return fmt.Errorf("%w at byte %d", ErrInvalidExpansion, offset)
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

	value, exists := e.lookup(expression.name)
	switch expression.operator {
	case 0:
		// A plain lookup returns an empty string for an unset variable.
		if !exists {
			return "", nil
		}
		return e.evaluateResolvedValue(expression.name, value, depth)

	case '-':
		// Use the word only for an unset or empty variable.
		if exists && value != "" {
			return e.evaluateResolvedValue(expression.name, value, depth)
		}
		return e.evaluateTemplate(expression.word, depth+1)

	case '=':
		// := shares the :- fallback decision,
		// then optionally persists the resolved fallback through Setter.
		if exists && value != "" {
			return e.evaluateResolvedValue(expression.name, value, depth)
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
			return e.evaluateResolvedValue(expression.name, value, depth)
		}
		if !e.opts.enforceRequired {
			if !exists {
				return "", nil
			}
			return e.evaluateResolvedValue(expression.name, value, depth)
		}

		message, err := e.evaluateTemplate(expression.word, depth+1)
		if err != nil {
			return "", err
		}
		if message == "" {
			message = "is not set or empty"
		}

		return "", fmt.Errorf("variable %q: %w: %s", expression.name, ErrRequiredVariable, message)

	default:
		// `${VAR:word}` is intentionally outside the supported subset
		// and behaves like a plain variable for compatibility.
		if !exists {
			return "", nil
		}
		return e.evaluateResolvedValue(expression.name, value, depth)
	}
}

// evaluateResolvedValue recursively expands resolver values when needed.
func (e *expansionEvaluator) evaluateResolvedValue(
	name string,
	value string,
	depth int,
) (string, error) {
	if !strings.Contains(value, "${") {
		return value, nil
	}
	if e.active[name] {
		cycle := append(append([]string(nil), e.stack...), name)
		return "", fmt.Errorf("%w: %s", ErrExpansionCycle, strings.Join(cycle, " -> "))
	}

	e.active[name] = true
	e.stack = append(e.stack, name)
	defer func() {
		delete(e.active, name)
		e.stack = e.stack[:len(e.stack)-1]
	}()

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
