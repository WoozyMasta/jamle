// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package jamle

import (
	"errors"
	"regexp"
	"strconv"
	"testing"

	goyaml "go.yaml.in/yaml/v3"
)

// expansionTestResolver provides deterministic values for scalar tests.
type expansionTestResolver struct {
	values map[string]string
}

// errExpansionSetter is returned by the failing setter test.
var errExpansionSetter = errors.New("setter failure")

// expansionFailingSetter rejects every assignment for error propagation tests.
type expansionFailingSetter struct{}

// Lookup reports every variable as unset.
func (expansionFailingSetter) Lookup(string) (string, bool) {
	return "", false
}

// Set returns the underlying assignment error.
func (expansionFailingSetter) Set(string, string) error {
	return errExpansionSetter
}

// Lookup returns a test variable value.
func (r expansionTestResolver) Lookup(name string) (string, bool) {
	value, ok := r.values[name]
	return value, ok
}

// Set updates a test variable value for assignment expressions.
func (r expansionTestResolver) Set(name, value string) error {
	r.values[name] = value
	return nil
}

func TestExpandEnvInScalar_LazyBranches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		values   map[string]string
		want     string
		wantErr  error
		checkVar string
	}{
		{
			name:     "required branch is lazy",
			input:    "${A:-${B:?boom}}",
			values:   map[string]string{"A": "ok"},
			want:     "ok",
			checkVar: "B",
		},
		{
			name:     "assignment branch is lazy",
			input:    "${A:-${B:=x}}",
			values:   map[string]string{"A": "ok"},
			want:     "ok",
			checkVar: "B",
		},
		{
			name:   "used nested branch",
			input:  "${A:-${B:?boom}}",
			values: map[string]string{"B": "x"},
			want:   "x",
		},
		{
			name:   "balanced literal braces",
			input:  `${A:-{"x":1}}`,
			values: map[string]string{"A": "ok"},
			want:   "ok",
		},
		{
			name:   "balanced literal braces default",
			input:  `${A:-{"x":1}}`,
			values: map[string]string{},
			want:   `{"x":1}`,
		},
		{
			name:   "quoted literal braces",
			input:  `${A:-{"x":"}"}}`,
			values: map[string]string{"A": "ok"},
			want:   "ok",
		},
		{
			name:   "quoted literal braces default",
			input:  `${A:-{"x":"}"}}`,
			values: map[string]string{},
			want:   `{"x":"}"}`,
		},
		{
			name:   "escaped quote before literal brace",
			input:  `${A:-{"x":"escaped \" }"}}`,
			values: map[string]string{},
			want:   `{"x":"escaped \" }"}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			resolver := expansionTestResolver{values: test.values}
			got, err := expandEnvInScalar(test.input, runtimeOptions{
				resolver:        resolver,
				maxDepth:        defaultMaxDepth,
				allowAssignment: true,
				enforceRequired: true,
			})
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("expandEnvInScalar error: got %v, want %v", err, test.wantErr)
				}
			} else if err != nil {
				t.Fatalf("expandEnvInScalar returned error: %v", err)
			}
			if got != test.want {
				t.Fatalf("expanded value mismatch: got %q, want %q", got, test.want)
			}
			if test.checkVar != "" {
				if _, ok := resolver.values[test.checkVar]; ok {
					t.Fatalf("lazy branch changed %s", test.checkVar)
				}
			}
		})
	}
}

func TestExpandEnvInScalar_RecursiveValuesAndCycles(t *testing.T) {
	t.Parallel()

	resolver := expansionTestResolver{values: map[string]string{}}
	got, err := expandEnvInScalar("${A:-${A:-x}}", runtimeOptions{
		resolver:        resolver,
		maxDepth:        defaultMaxDepth,
		allowAssignment: true,
		enforceRequired: true,
	})
	if err != nil || got != "x" {
		t.Fatalf("same-name fallback mismatch: got %q, err %v", got, err)
	}

	resolver = expansionTestResolver{values: map[string]string{}}
	got, err = expandEnvInScalar("${A:=${A:-x}}", runtimeOptions{
		resolver:        resolver,
		maxDepth:        defaultMaxDepth,
		allowAssignment: true,
		enforceRequired: true,
	})
	if err != nil || got != "x" {
		t.Fatalf("same-name assignment fallback mismatch: got %q, err %v", got, err)
	}
	if resolver.values["A"] != "x" {
		t.Fatalf("same-name assignment fallback did not set A: got %q", resolver.values["A"])
	}

	resolver = expansionTestResolver{values: map[string]string{
		"A": "${B}",
		"B": "value",
	}}
	got, err = expandEnvInScalar("${A}", runtimeOptions{
		resolver:        resolver,
		maxDepth:        defaultMaxDepth,
		allowAssignment: true,
		enforceRequired: true,
	})
	if err != nil || got != "value" {
		t.Fatalf("recursive resolver value mismatch: got %q, err %v", got, err)
	}

	resolver = expansionTestResolver{values: map[string]string{
		"A": "${B}",
		"B": "${A}",
	}}
	_, err = expandEnvInScalar("${A}", runtimeOptions{
		resolver:        resolver,
		maxDepth:        defaultMaxDepth,
		allowAssignment: true,
		enforceRequired: true,
	})
	if !errors.Is(err, ErrExpansionCycle) {
		t.Fatalf("expected ErrExpansionCycle, got %v", err)
	}

	resolver = expansionTestResolver{values: map[string]string{
		"A": "${B}",
		"B": "${C}",
		"C": "value",
	}}
	_, err = expandEnvInScalar("${A}", runtimeOptions{
		resolver:        resolver,
		maxDepth:        2,
		allowAssignment: true,
		enforceRequired: true,
	})
	if !errors.Is(err, ErrExpansionDepth) {
		t.Fatalf("expected ErrExpansionDepth, got %v", err)
	}
}

func TestExpandEnvInScalar_EscapingAndMalformedInput(t *testing.T) {
	t.Parallel()

	resolver := expansionTestResolver{values: map[string]string{
		"A": "a",
		"B": "b",
		"R": "$${A}",
	}}
	opts := runtimeOptions{
		resolver:        resolver,
		maxDepth:        defaultMaxDepth,
		allowAssignment: true,
		enforceRequired: true,
	}

	tests := []struct {
		input string
		want  string
	}{
		{input: "$${A}", want: "${A}"},
		{input: "$${A:-${B}}", want: "${A:-${B}}"},
		{input: "literal $${A} ${B}", want: "literal ${A} b"},
		{input: "${R}", want: "${A}"},
		{input: "${A}", want: "a"},
	}
	for _, test := range tests {
		got, err := expandEnvInScalar(test.input, opts)
		if err != nil {
			t.Fatalf("input %q returned error: %v", test.input, err)
		}
		if got != test.want {
			t.Fatalf("input %q: got %q, want %q", test.input, got, test.want)
		}
	}

	if got, err := expandEnvInScalar("$${A", opts); err != nil || got != "$${A" {
		t.Fatalf("escaped unmatched expression: got %q, err %v", got, err)
	}
	if got, err := expandEnvInScalar(`$${A:-${B:-${C}}}`, runtimeOptions{
		resolver:        resolver,
		maxDepth:        1,
		allowAssignment: true,
		enforceRequired: true,
	}); err != nil || got != `${A:-${B:-${C}}}` {
		t.Fatalf("deep escaped expression: got %q, err %v", got, err)
	}
	if _, err := expandEnvInScalar("${A", opts); !errors.Is(err, ErrExpansionSyntax) {
		t.Fatalf("expected ErrExpansionSyntax, got %v", err)
	}

	resolver.values["C"] = "\x00${A}\x01"
	got, err := expandEnvInScalar("${C}", opts)
	if err != nil || got != "\x00a\x01" {
		t.Fatalf("control-byte resolver value: got %q, err %v", got, err)
	}
}

func FuzzExpand(f *testing.F) {
	for _, seed := range []string{
		"${A}",
		"$${A}",
		"${A:-${B:-value}}",
		`${A:-{"x":1}}`,
		"literal",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(_ *testing.T, input string) {
		_, _ = expandEnvInScalar(input, runtimeOptions{
			resolver:        expansionTestResolver{values: map[string]string{}},
			maxDepth:        defaultMaxDepth,
			allowAssignment: true,
			enforceRequired: true,
		})
	})
}

func TestUnmarshalWithOptions_AliasConsumersUseTheirOwnPolicy(t *testing.T) {
	type source struct {
		Value string `json:"value"`
	}
	type consumer struct {
		Value string `json:"value" jamle:"noexpand"`
	}

	input := []byte(`
base: &value
  value: "${X:-expanded}"
a: *value
b: *value
`)

	type document struct {
		Base source   `json:"base"`
		A    consumer `json:"a"`
		B    source   `json:"b"`
	}

	var got document
	if err := UnmarshalWithOptions(input, &got, UnmarshalOptions{}); err != nil {
		t.Fatalf("UnmarshalWithOptions returned error: %v", err)
	}

	if got.Base.Value != "expanded" {
		t.Fatalf("anchor definition mismatch: got %q", got.Base.Value)
	}
	if got.A.Value != "${X:-expanded}" {
		t.Fatalf("noexpand alias consumer mismatch: got %q", got.A.Value)
	}
	if got.B.Value != "expanded" {
		t.Fatalf("expand alias consumer mismatch: got %q", got.B.Value)
	}
}

func TestUnmarshalWithOptions_ExpandKeysAliasConsumersUseTheirOwnPolicy(t *testing.T) {
	type document struct {
		A map[string]string `json:"a"`
		B map[string]string `json:"b"`
	}

	input := []byte(`
base: &value
  ${KEY}: value
a: *value
b: *value
`)
	resolver := ResolveFunc(func(name string) (string, bool) {
		if name == "KEY" {
			return "expanded", true
		}

		return "", false
	})

	var got document
	if err := UnmarshalWithOptions(input, &got, UnmarshalOptions{
		Resolver:          resolver,
		ExpandKeys:        true,
		IgnoreExpandPaths: []string{"a.${KEY}"},
	}); err != nil {
		t.Fatalf("UnmarshalWithOptions returned error: %v", err)
	}

	if got.A["${KEY}"] != "value" {
		t.Fatalf("noexpand alias key mismatch: %#v", got.A)
	}
	if got.B["expanded"] != "value" {
		t.Fatalf("expanded alias key mismatch: %#v", got.B)
	}
}

func TestUnmarshalWithOptions_MergeUsesConsumerSchemaAndPath(t *testing.T) {
	type config struct {
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
		Secret      string `json:"secret" jamle:"noexpand"`
	}

	input := []byte(`
defaults: &defaults
  name: "${NAME:-default}"
  display_name: "${DISPLAY_NAME:-display}"
  secret: "${SECRET:-literal}"
app:
  <<: *defaults
`)

	type document struct {
		Defaults config `json:"defaults"`
		App      config `json:"app"`
	}

	var got document
	if err := UnmarshalWithOptions(input, &got, UnmarshalOptions{}); err != nil {
		t.Fatalf("UnmarshalWithOptions returned error: %v", err)
	}

	if got.Defaults.Name != "default" || got.App.Name != "default" ||
		got.Defaults.DisplayName != "display" || got.App.DisplayName != "display" {
		t.Fatalf("merged names mismatch: %#v", got)
	}
	if got.Defaults.Secret != "${SECRET:-literal}" ||
		got.App.Secret != "${SECRET:-literal}" {
		t.Fatalf("noexpand merge mismatch: %#v", got)
	}
}

func TestUnmarshalWithOptions_NestedAndMultipleMerges(t *testing.T) {
	type config struct {
		Name  string `json:"name"`
		Outer string `json:"outer"`
		Local string `json:"local"`
	}

	input := []byte(`
outer: &outer
  outer: "${OUTER:-outer}"
first: &first
  name: "${FIRST:-first}"
  local: "${FIRST_LOCAL:-first-local}"
second: &second
  name: "${SECOND:-second}"
app:
  <<: [*first, *second]
  local: "${LOCAL:-local}"
nested:
  <<: *outer
`)

	type document struct {
		First  config `json:"first"`
		Second config `json:"second"`
		App    config `json:"app"`
		Nested config `json:"nested"`
	}

	var got document
	if err := UnmarshalWithOptions(input, &got, UnmarshalOptions{}); err != nil {
		t.Fatalf("UnmarshalWithOptions returned error: %v", err)
	}

	if got.App.Name != "first" {
		t.Fatalf("first merge source must take precedence: %#v", got.App)
	}
	if got.App.Local != "local" {
		t.Fatalf("explicit local field mismatch: %#v", got.App)
	}
	if got.Nested.Outer != "outer" {
		t.Fatalf("nested merge mismatch: %#v", got.Nested)
	}
}

func TestExpandEnvInNode_AliasCycleReturnsError(t *testing.T) {
	alias := &goyaml.Node{Kind: goyaml.AliasNode}
	alias.Alias = alias
	root := &goyaml.Node{Kind: goyaml.DocumentNode, Content: []*goyaml.Node{alias}}

	err := expandEnvInNode(root, nil, runtimeOptions{
		resolver:        expansionTestResolver{values: map[string]string{}},
		maxDepth:        defaultMaxDepth,
		allowAssignment: true,
		enforceRequired: true,
	})
	if err == nil {
		t.Fatal("expected alias cycle error")
	}
}

func TestUnmarshalWithOptions_AliasCycleReturnsError(t *testing.T) {
	input := []byte("value: &value [*value, \"${X}\"]\n")

	var got map[string]any
	err := UnmarshalWithOptions(input, &got, UnmarshalOptions{})
	if !errors.Is(err, errAliasCycle) {
		t.Fatalf("expected YAML alias cycle error, got %v", err)
	}
}

func TestUnmarshalWithOptions_ExpansionErrorContext(t *testing.T) {
	t.Run("required variable", func(t *testing.T) {
		input := []byte("server:\n  database:\n    password: \"${DB_PASSWORD:?password is required}\"\n")

		var got map[string]any
		err := UnmarshalWithOptions(input, &got, UnmarshalOptions{
			Resolver: expansionTestResolver{values: map[string]string{}},
		})
		if !errors.Is(err, ErrRequiredVariable) {
			t.Fatalf("expected ErrRequiredVariable, got %v", err)
		}
		assertExpansionSourceContext(t, err, "server.database.password", 3)
	})

	t.Run("invalid syntax", func(t *testing.T) {
		input := []byte("server:\n  value: \"${BROKEN\"\n")

		var got map[string]any
		err := UnmarshalWithOptions(input, &got, UnmarshalOptions{})
		if !errors.Is(err, ErrInvalidExpansion) {
			t.Fatalf("expected ErrInvalidExpansion, got %v", err)
		}
		assertExpansionSourceContext(t, err, "server.value", 2)
	})

	t.Run("setter error unwraps", func(t *testing.T) {
		input := []byte("value: \"${A:=fallback}\"\n")

		var got map[string]any
		err := UnmarshalWithOptions(input, &got, UnmarshalOptions{
			Resolver: expansionFailingSetter{},
		})
		if !errors.Is(err, errExpansionSetter) {
			t.Fatalf("expected underlying setter error, got %v", err)
		}
		assertExpansionSourceContext(t, err, "value", 1)
	})
}

func assertExpansionSourceContext(t *testing.T, err error, path string, line int) {
	t.Helper()
	if err == nil {
		t.Fatal("expected expansion error")
	}

	pattern := regexp.MustCompile(regexp.QuoteMeta(path) + `:` +
		strconv.Itoa(line) + `:[0-9]+:`)
	if !pattern.MatchString(err.Error()) {
		t.Fatalf("error lacks source context %q: %v", path, err)
	}
}
