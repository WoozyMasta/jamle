// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package yamlnode

import (
	"bytes"
	"testing"

	goyaml "go.yaml.in/yaml/v3"
)

func TestEffectiveMappingAppliesMergePrecedence(t *testing.T) {
	t.Parallel()

	var root goyaml.Node
	data := []byte(`
first: &first
  a: one
  shared: first
second: &second
  shared: second
  b: two
result:
  <<: [*first, *second]
  shared: explicit
`)
	if err := goyaml.NewDecoder(bytes.NewReader(data)).Decode(&root); err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}

	result := root.Content[0].Content[5]
	entries, err := EffectiveMapping(result)
	if err != nil {
		t.Fatalf("EffectiveMapping returned error: %v", err)
	}

	got := make(map[string]string, len(entries))
	for _, entry := range entries {
		got[entry.Key.Value] = entry.Value.Value
	}
	want := map[string]string{"a": "one", "shared": "explicit", "b": "two"}
	if len(got) != len(want) {
		t.Fatalf("effective mapping length mismatch: got %#v, want %#v", got, want)
	}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("effective mapping value mismatch for %q: got %q, want %q", key, got[key], value)
		}
	}
}
