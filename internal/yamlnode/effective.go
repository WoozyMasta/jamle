// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

// Package yamlnode provides shared logical views over YAML node graphs.
package yamlnode

import (
	"errors"
	"reflect"

	goyaml "go.yaml.in/yaml/v3"
)

var (
	// ErrAliasCycle reports a recursive alias encountered while resolving a mapping.
	ErrAliasCycle = errors.New("yaml: alias cycle")
)

// MappingEntry stores one key-value pair from an effective YAML mapping.
type MappingEntry struct {
	// Key is the source mapping key node.
	Key *goyaml.Node
	// Value is the source mapping value node.
	Value *goyaml.Node
}

// EffectiveMapping returns mapping entries after applying YAML merge keys.
// Explicit entries override merged entries,
// while earlier merge sources win over later sources,
// matching go-yaml's merge behavior.
func EffectiveMapping(root *goyaml.Node) ([]MappingEntry, error) {
	return effectiveMapping(root, make(map[*goyaml.Node]bool))
}

// IsMergeKey reports whether node is a YAML merge key.
func IsMergeKey(node *goyaml.Node) bool {
	if node == nil || node.Value != "<<" {
		return false
	}

	switch node.Tag {
	case "", "!", "!!merge", "tag:yaml.org,2002:merge":
		return true
	default:
		return false
	}
}

// effectiveMapping resolves one mapping and its merge sources recursively.
func effectiveMapping(root *goyaml.Node, active map[*goyaml.Node]bool) ([]MappingEntry, error) {
	node := root
	for node != nil && node.Kind == goyaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}

	if node == nil {
		return nil, nil
	}

	if node.Kind == goyaml.AliasNode {
		if node.Alias == nil {
			return nil, nil
		}
		if active[node] {
			return nil, ErrAliasCycle
		}

		active[node] = true
		defer delete(active, node)
		return effectiveMapping(node.Alias, active)
	}

	if node.Kind != goyaml.MappingNode {
		return nil, nil
	}

	entries := make([]MappingEntry, 0, len(node.Content)/2)
	add := func(entry MappingEntry, override bool) {
		for index, existing := range entries {
			if sameMappingKey(existing.Key, entry.Key) {
				if override {
					entries[index] = entry
				}
				return
			}
		}

		entries = append(entries, entry)
	}

	for index := 0; index+1 < len(node.Content); index += 2 {
		key := node.Content[index]
		value := node.Content[index+1]
		if IsMergeKey(key) {
			if value.Kind == goyaml.SequenceNode {
				for _, source := range value.Content {
					merged, err := effectiveMapping(source, active)
					if err != nil {
						return nil, err
					}

					for _, entry := range merged {
						add(entry, false)
					}
				}

				continue
			}

			merged, err := effectiveMapping(value, active)
			if err != nil {
				return nil, err
			}

			for _, entry := range merged {
				add(entry, false)
			}

			continue
		}

		add(MappingEntry{Key: key, Value: value}, true)
	}

	return entries, nil
}

// sameMappingKey compares YAML keys by decoded value and falls back to node identity.
func sameMappingKey(left, right *goyaml.Node) bool {
	if left == nil || right == nil {
		return left == right
	}

	var leftValue, rightValue any
	if left.Decode(&leftValue) == nil && right.Decode(&rightValue) == nil {
		return reflect.DeepEqual(leftValue, rightValue)
	}

	return left.Kind == right.Kind && left.Tag == right.Tag && left.Value == right.Value
}
