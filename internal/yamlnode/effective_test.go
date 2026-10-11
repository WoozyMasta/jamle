// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package yamlnode

import (
	"bytes"
	"fmt"
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

// BenchmarkEffectiveMappingNoMerge measures the ordinary mapping fast path.
func BenchmarkEffectiveMappingNoMerge(b *testing.B) {
	for _, size := range []int{1000, 10000} {
		b.Run(fmt.Sprintf("entries-%d", size), func(b *testing.B) {
			root := plainMapping(size)
			b.ReportAllocs()
			b.ResetTimer()

			for range b.N {
				entries, err := EffectiveMapping(root)
				if err != nil {
					b.Fatal(err)
				}
				if len(entries) != size {
					b.Fatalf("entry count mismatch: got %d, want %d", len(entries), size)
				}
			}
		})
	}
}

// plainMapping builds a mapping without merge keys for benchmarks.
func plainMapping(size int) *goyaml.Node {
	content := make([]*goyaml.Node, 0, size*2)
	for index := range size {
		content = append(content,
			&goyaml.Node{Kind: goyaml.ScalarNode, Tag: "!!str", Value: fmt.Sprintf("key%d", index)},
			&goyaml.Node{Kind: goyaml.ScalarNode, Tag: "!!str", Value: "value"},
		)
	}

	return &goyaml.Node{Kind: goyaml.MappingNode, Content: content}
}
