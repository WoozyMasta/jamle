// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package jamle

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"

	jyaml "github.com/woozymasta/jamle/yaml"
	goyaml "go.yaml.in/yaml/v3"
)

// placeholders for masking braces in escaped variables.
const (
	maskStart        = "\x00"
	maskEnd          = "\x01"
	defaultMaxPasses = 10
)

// Resolver provides variable lookup for ${VAR} expansions.
type Resolver interface {
	Lookup(name string) (string, bool)
}

// Setter optionally supports ${VAR:=default} assignment behavior.
type Setter interface {
	Set(name, value string) error
}

// UnmarshalOptions controls variable expansion behavior for Unmarshal APIs.
type UnmarshalOptions struct {
	// Resolver provides values for `${VAR}` expansion.
	// When nil, process environment resolver is used.
	Resolver Resolver `json:"resolver" yaml:"resolver" jsonschema:"-"`

	// IgnoreExpandPaths skips expansion for scalar nodes whose YAML key path
	// matches one of these glob patterns (dot-separated, `*` for one segment).
	IgnoreExpandPaths []string `json:"ignoreExpandPaths,omitempty" yaml:"ignoreExpandPaths,omitempty"`

	// MaxPasses limits nested expansion passes.
	// When <= 0, default max pass count is used.
	MaxPasses int `json:"maxPasses,omitempty" yaml:"maxPasses,omitempty" jsonschema:"default=10,minimum=1,maximum=1000,example=20"`

	// DisableAssignment disables side effects of `${VAR:=default}`.
	// When true, `${VAR:=default}` behaves like `${VAR:-default}` and does not call Setter.
	DisableAssignment bool `json:"disableAssignment,omitempty" yaml:"disableAssignment,omitempty" jsonschema:"default=false,example=true"`

	// DisableRequiredErrors disables errors for `${VAR:?message}`.
	// When true, `${VAR:?message}` behaves like `${VAR}` and does not return an error.
	DisableRequiredErrors bool `json:"disableRequiredErrors,omitempty" yaml:"disableRequiredErrors,omitempty" jsonschema:"default=false,example=true"`
}

// ResolveFunc adapts a function to the Resolver interface.
type ResolveFunc func(name string) (string, bool)

// Lookup resolves a variable name via the underlying function.
func (f ResolveFunc) Lookup(name string) (string, bool) {
	return f(name)
}

// envResolver resolves and assigns variables via process environment.
type envResolver struct{}

// Lookup resolves a variable from process environment.
func (envResolver) Lookup(name string) (string, bool) {
	return os.LookupEnv(name)
}

// Set assigns a variable in process environment.
func (envResolver) Set(name, value string) error {
	return os.Setenv(name, value)
}

// unmaskReplacer restores masked escaped variables back to ${...}.
var unmaskReplacer = strings.NewReplacer(maskStart, "${", maskEnd, "}")

// noExpandPathCache stores compiled noexpand paths by root output type.
var noExpandPathCache sync.Map

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

// pathRule stores a compiled path matching rule.
type pathRule struct {
	segments []string
}

// runtimeOptions stores normalized expansion options for one unmarshal call.
type runtimeOptions struct {
	resolver        Resolver
	ignorePathRules []pathRule
	maxPasses       int
	allowAssignment bool
	enforceRequired bool
}

/*
Unmarshal parses the YAML-encoded data and stores the result in the value pointed to by v.

Before parsing, it recursively expands environment variables within the data.
The function performs up to 10 passes to resolve nested variables (e.g., ${A:=${B}})
and prevents infinite loops.
*/
func Unmarshal(data []byte, v any) error {
	return UnmarshalWithOptions(data, v, UnmarshalOptions{})
}

// UnmarshalWithOptions parses YAML and expands ${...} using configured options.
func UnmarshalWithOptions(data []byte, v any, opts UnmarshalOptions) error {
	// Fast path: if there are no variable markers, decode directly.
	if !bytes.Contains(data, []byte("${")) {
		return jyaml.Unmarshal(data, v)
	}

	resolvedOpts := resolveOptions(opts, reflect.TypeOf(v))

	// Parse into YAML AST (comments are stored in node fields, not in scalar values)
	var root goyaml.Node
	dec := goyaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(false)
	if err := dec.Decode(&root); err != nil {
		return err
	}

	if err := expandEnvInNode(&root, resolvedOpts); err != nil {
		return err
	}

	// Decode from transformed AST directly to avoid YAML re-encode/re-decode.
	return jyaml.UnmarshalNode(&root, v)
}

// UnmarshalAll parses all YAML documents from the input stream and appends
// decoded values into out, which must be a pointer to a slice.
func UnmarshalAll(data []byte, out any) error {
	return UnmarshalAllWithOptions(data, out, UnmarshalOptions{})
}

// UnmarshalAllWithOptions parses all YAML documents using configured options.
func UnmarshalAllWithOptions(data []byte, out any, opts UnmarshalOptions) error {
	outValue := reflect.ValueOf(out)
	if !outValue.IsValid() {
		return fmt.Errorf("%w, got %T", ErrOutMustBePointerToSlice, out)
	}
	if outValue.Kind() != reflect.Ptr || outValue.Elem().Kind() != reflect.Slice {
		return fmt.Errorf("%w, got %T", ErrOutMustBePointerToSlice, out)
	}

	sliceValue := outValue.Elem()
	elemType := sliceValue.Type().Elem()
	resolvedOpts := resolveOptions(opts, elemType)
	containsVars := bytes.Contains(data, []byte("${"))

	dec := goyaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(false)

	for {
		var root goyaml.Node
		err := dec.Decode(&root)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}

		if containsVars {
			if err := expandEnvInNode(&root, resolvedOpts); err != nil {
				return err
			}
		}

		elem, err := decodeDocument(&root, elemType)
		if err != nil {
			return err
		}

		sliceValue = reflect.Append(sliceValue, elem)
	}

	outValue.Elem().Set(sliceValue)
	return nil
}

// decodeDocument decodes one YAML document into a slice element value.
func decodeDocument(root *goyaml.Node, elemType reflect.Type) (reflect.Value, error) {
	if elemType.Kind() == reflect.Ptr {
		target := reflect.New(elemType.Elem())
		if err := jyaml.UnmarshalNode(root, target.Interface()); err != nil {
			return reflect.Value{}, err
		}

		return target, nil
	}

	target := reflect.New(elemType)
	if err := jyaml.UnmarshalNode(root, target.Interface()); err != nil {
		return reflect.Value{}, err
	}

	return target.Elem(), nil
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

// resolveOptions normalizes options and applies defaults.
func resolveOptions(opts UnmarshalOptions, outType reflect.Type) runtimeOptions {
	resolver := opts.Resolver
	if resolver == nil {
		resolver = envResolver{}
	}

	maxPasses := opts.MaxPasses
	if maxPasses <= 0 {
		maxPasses = defaultMaxPasses
	}

	ignorePaths := append([]string{}, opts.IgnoreExpandPaths...)
	if outType != nil {
		ignorePaths = append(ignorePaths, collectNoExpandPathsCached(outType)...)
	}

	runtime := runtimeOptions{
		resolver:        resolver,
		maxPasses:       maxPasses,
		allowAssignment: !opts.DisableAssignment,
		enforceRequired: !opts.DisableRequiredErrors,
	}
	if len(ignorePaths) == 0 {
		return runtime
	}

	runtime.ignorePathRules = compilePathRules(ignorePaths)
	return runtime
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

// collectNoExpandPaths builds ignore paths from `jamle:"noexpand"` tags.
func collectNoExpandPaths(outType reflect.Type) []string {
	root := indirectType(outType)
	if root == nil {
		return nil
	}

	paths := make([]string, 0, 8)
	visited := make(map[reflect.Type]bool)
	collectNoExpandPathsFromType(root, nil, visited, &paths)
	if len(paths) == 0 {
		return nil
	}

	sort.Strings(paths)
	return compactSortedStrings(paths)
}

// collectNoExpandPathsCached returns cached noexpand paths for output type.
func collectNoExpandPathsCached(outType reflect.Type) []string {
	root := indirectType(outType)
	if root == nil {
		return nil
	}

	cached, ok := noExpandPathCache.Load(root)
	if ok {
		return cached.([]string)
	}

	paths := collectNoExpandPaths(root)
	actual, _ := noExpandPathCache.LoadOrStore(root, paths)

	return actual.([]string)
}

// collectNoExpandPathsFromType walks reflect types and appends noexpand paths.
func collectNoExpandPathsFromType(
	t reflect.Type,
	path []string,
	visited map[reflect.Type]bool,
	out *[]string,
) {
	t = indirectType(t)
	if t == nil {
		return
	}

	switch t.Kind() {
	case reflect.Struct:
		if visited[t] {
			return
		}
		visited[t] = true
		defer delete(visited, t)

		for i := 0; i < t.NumField(); i++ {
			sf := t.Field(i)
			if shouldSkipStructField(sf) {
				continue
			}

			fieldPath, inline := pathForStructField(path, sf)
			if inline {
				collectNoExpandPathsFromType(sf.Type, path, visited, out)
				continue
			}
			if len(fieldPath) == 0 {
				continue
			}

			if hasNoExpandTag(sf.Tag.Get("jamle")) {
				*out = append(*out, strings.Join(fieldPath, "."))
			}

			collectNoExpandPathsFromType(sf.Type, fieldPath, visited, out)
		}

	case reflect.Slice, reflect.Array, reflect.Map:
		nextPath := appendPathSegment(path, "*")
		collectNoExpandPathsFromType(t.Elem(), nextPath, visited, out)
	}
}

// shouldSkipStructField reports whether field should be ignored for path build.
func shouldSkipStructField(sf reflect.StructField) bool {
	if sf.PkgPath != "" {
		return true
	}

	if jsonTagName(sf) == "-" {
		return true
	}

	return yamlTagName(sf) == "-"
}

// pathForStructField builds one path segment for a struct field.
func pathForStructField(path []string, sf reflect.StructField) ([]string, bool) {
	jsonName := jsonTagName(sf)
	if jsonName == "-" {
		return nil, false
	}

	fieldType := indirectType(sf.Type)
	if sf.Anonymous && jsonName == "" && fieldType != nil && fieldType.Kind() == reflect.Struct {
		return path, true
	}

	if jsonName != "" {
		return appendPathSegment(path, jsonName), false
	}

	yamlName := yamlTagName(sf)
	if yamlName == "-" {
		return nil, false
	}

	if yamlName != "" && yamlName != "-" {
		return appendPathSegment(path, yamlName), false
	}

	return appendPathSegment(path, strings.ToLower(sf.Name)), false
}

// hasNoExpandTag reports whether jamle tag has `noexpand` option.
func hasNoExpandTag(tag string) bool {
	for item := range strings.SplitSeq(tag, ",") {
		if strings.TrimSpace(item) == "noexpand" {
			return true
		}
	}

	return false
}

// jsonTagName returns first value token from `json` tag.
func jsonTagName(sf reflect.StructField) string {
	return firstTagValue(sf.Tag.Get("json"))
}

// yamlTagName returns first value token from `yaml` tag.
func yamlTagName(sf reflect.StructField) string {
	return firstTagValue(sf.Tag.Get("yaml"))
}

// firstTagValue returns first token before comma in a struct tag.
func firstTagValue(tag string) string {
	name, _, _ := strings.Cut(tag, ",")
	return strings.TrimSpace(name)
}

// indirectType dereferences pointers until a concrete type is reached.
func indirectType(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	return t
}

// compactSortedStrings compacts sorted slice values in-place.
func compactSortedStrings(in []string) []string {
	if len(in) < 2 {
		return in
	}

	out := in[:1]
	last := in[0]

	for i := 1; i < len(in); i++ {
		if in[i] == last {
			continue
		}

		last = in[i]
		out = append(out, in[i])
	}

	return out
}
