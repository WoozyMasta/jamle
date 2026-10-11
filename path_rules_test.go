// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package jamle

import "testing"

func TestPathRules_EscapedSegments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
		path    []string
		want    bool
	}{
		{
			name:    "literal dot",
			pattern: `a\.b.*`,
			path:    []string{"a.b", "value"},
			want:    true,
		},
		{
			name:    "literal wildcard",
			pattern: `map.\*.value`,
			path:    []string{"map", "*", "value"},
			want:    true,
		},
		{
			name:    "literal brackets",
			pattern: `map.key\[x\].value`,
			path:    []string{"map", "key[x]", "value"},
			want:    true,
		},
		{
			name:    "numeric index wildcard",
			pattern: `items[0].name`,
			path:    []string{"items", "any", "name"},
			want:    true,
		},
		{
			name:    "escaped numeric index",
			pattern: `items[\0].name`,
			path:    []string{"items", "0", "name"},
			want:    true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			rules := compilePathRules([]string{test.pattern})
			if got := shouldIgnorePath(test.path, rules); got != test.want {
				t.Fatalf("path match mismatch: got %t, want %t", got, test.want)
			}
		})
	}
}

func TestCompilePathRules_DistinguishesLiteralWildcard(t *testing.T) {
	t.Parallel()

	rules := compilePathRules([]string{"a.*", `a.\*`})
	if len(rules) != 2 {
		t.Fatalf("literal and wildcard rules were deduplicated: %#v", rules)
	}
}
