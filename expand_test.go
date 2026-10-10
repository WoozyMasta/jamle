// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package jamle

import (
	"errors"
	"testing"
)

// expansionTestResolver provides deterministic values for scalar tests.
type expansionTestResolver struct {
	values map[string]string
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
	}

	for _, test := range tests {
		test := test
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

	resolver := expansionTestResolver{values: map[string]string{
		"A": "${B}",
		"B": "value",
	}}
	got, err := expandEnvInScalar("${A}", runtimeOptions{
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

	f.Fuzz(func(t *testing.T, input string) {
		_, _ = expandEnvInScalar(input, runtimeOptions{
			resolver:        expansionTestResolver{values: map[string]string{}},
			maxDepth:        defaultMaxDepth,
			allowAssignment: true,
			enforceRequired: true,
		})
	})
}
