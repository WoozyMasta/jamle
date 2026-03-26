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

// runtimeOptions stores normalized expansion options for one unmarshal call.
type runtimeOptions struct {
	resolver        Resolver
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
	resolvedOpts := resolveOptions(opts)

	// Fast path: if there are no variable markers, decode directly.
	if !bytes.Contains(data, []byte("${")) {
		return jyaml.Unmarshal(data, v)
	}

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
	resolvedOpts := resolveOptions(opts)

	outValue := reflect.ValueOf(out)
	if !outValue.IsValid() {
		return fmt.Errorf("%w, got %T", ErrOutMustBePointerToSlice, out)
	}
	if outValue.Kind() != reflect.Ptr || outValue.Elem().Kind() != reflect.Slice {
		return fmt.Errorf("%w, got %T", ErrOutMustBePointerToSlice, out)
	}

	sliceValue := outValue.Elem()
	elemType := sliceValue.Type().Elem()
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
	var resolveErr error
	walkScalars(root, func(s string) string {
		if resolveErr != nil {
			return s
		}

		out, err := expandEnvInScalar(s, opts)
		if err != nil {
			resolveErr = err
			return s
		}

		return out
	})

	return resolveErr
}

// walkScalars walks the YAML AST recursively and applies fn to the Value of each ScalarNode.
// Comments (HeadComment, LineComment, FootComment) are intentionally not touched.
func walkScalars(n *goyaml.Node, fn func(string) string) {
	if n == nil {
		return
	}

	if n.Kind == goyaml.ScalarNode {
		oldStyle := n.Style
		oldTag := n.Tag
		oldVal := n.Value

		n.Value = fn(n.Value)

		// If the scalar was plain and implicitly a string only because it contained ${...},
		// clear the tag so YAML can re-resolve types (bool/int/float/null) after expansion.
		if oldStyle == 0 && oldTag == "!!str" && oldVal != n.Value {
			n.Tag = ""
		}
	}

	for _, c := range n.Content {
		walkScalars(c, fn)
	}
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

	sort.Slice(ranges, func(i, j int) bool {
		return ranges[i].start < ranges[j].start
	})

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
func resolveOptions(opts UnmarshalOptions) runtimeOptions {
	resolver := opts.Resolver
	if resolver == nil {
		resolver = envResolver{}
	}

	maxPasses := opts.MaxPasses
	if maxPasses <= 0 {
		maxPasses = defaultMaxPasses
	}

	return runtimeOptions{
		resolver:        resolver,
		maxPasses:       maxPasses,
		allowAssignment: !opts.DisableAssignment,
		enforceRequired: !opts.DisableRequiredErrors,
	}
}
