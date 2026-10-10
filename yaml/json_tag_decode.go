// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package yaml

import (
	"reflect"

	"github.com/woozymasta/jamle/internal/schema"
	goyaml "go.yaml.in/yaml/v3"
)

// UnmarshalNode decodes a YAML node into target using JSON-tag key mapping.
// The caller-owned node graph is not modified.
func UnmarshalNode(root *goyaml.Node, o any) error {
	if root == nil {
		return nil
	}
	if o == nil {
		return nil
	}

	node := root
	if root.Kind == goyaml.DocumentNode && len(root.Content) > 0 {
		node = root.Content[0]
	}

	target := reflect.TypeOf(o)
	if target.Kind() != reflect.Pointer {
		return node.Decode(o)
	}

	node = cloneNodeGraph(root)
	if node.Kind == goyaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}

	remapJSONTagKeys(node, target.Elem())
	return node.Decode(o)
}

// remapJSONTagKeys rewrites mapping keys to match struct field names.
func remapJSONTagKeys(n *goyaml.Node, target reflect.Type) {
	if n == nil {
		return
	}

	target = derefType(target)
	if target == nil {
		return
	}
	if schema.IsOpaque(target) {
		return
	}

	switch n.Kind {
	case goyaml.DocumentNode:
		if len(n.Content) > 0 {
			remapJSONTagKeys(n.Content[0], target)
		}

	case goyaml.MappingNode:
		remapMappingNodeKeys(n, target)

	case goyaml.SequenceNode:
		elem := target
		if target.Kind() == reflect.Slice || target.Kind() == reflect.Array {
			elem = target.Elem()
		}
		elem = derefType(elem)
		if elem == nil {
			return
		}

		for _, child := range n.Content {
			remapJSONTagKeys(child, elem)
		}
	}
}

// remapMappingNodeKeys remaps mapping keys for struct/map targets recursively.
func remapMappingNodeKeys(n *goyaml.Node, target reflect.Type) {
	switch target.Kind() {
	case reflect.Struct:
		remapStructMappingNodeKeys(n, target)

	case reflect.Map:
		elem := derefType(target.Elem())
		if elem == nil {
			return
		}

		for i := 1; i < len(n.Content); i += 2 {
			remapJSONTagKeys(n.Content[i], elem)
		}
	}
}

// remapStructMappingNodeKeys remaps one struct mapping node by JSON tag rules.
func remapStructMappingNodeKeys(n *goyaml.Node, target reflect.Type) {
	lookup := schema.For(target)
	for i := 0; i+1 < len(n.Content); i += 2 {
		keyNode := n.Content[i]
		valNode := n.Content[i+1]

		decodedField, ok := lookup.Lookup(keyNode.Value)
		if !ok || decodedField.YAMLDecodeKey == "-" {
			continue
		}

		keyNode.Value = decodedField.YAMLDecodeKey
		remapJSONTagKeys(valNode, decodedField.Type)
	}
}

// derefType strips pointers until a concrete type is reached.
func derefType(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}
