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
	return UnmarshalNodeWithOptions(root, o, DecodeOptions{})
}

// unmarshalNode decodes a node after option-specific validation has completed.
func unmarshalNode(root *goyaml.Node, o any) error {
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
	promoted := make([]promotedMapping, 0)
	seen := make(map[*goyaml.Node]bool)
	content := make([]*goyaml.Node, 0, len(n.Content))

	for i := 0; i+1 < len(n.Content); i += 2 {
		keyNode := n.Content[i]
		valNode := n.Content[i+1]

		decodedField, ok := lookup.Lookup(keyNode.Value)
		if !ok || decodedField.YAMLDecodeKey == "-" {
			content = append(content, keyNode, valNode)
			continue
		}

		keyNode.Value = decodedField.YAMLDecodeKey
		if len(decodedField.YAMLDecodePath) == 0 {
			remapJSONTagKeys(valNode, decodedField.Type)
			content = append(content, keyNode, valNode)
			continue
		}

		path := make([]string, 0, len(decodedField.YAMLDecodePath)+1)
		for _, element := range decodedField.YAMLDecodePath {
			path = append(path, element.Key)
		}
		path = append(path, decodedField.YAMLDecodeKey)

		mappings := insertYAMLMappingPath(&content, path, keyNode, valNode)
		for index, mapping := range mappings {
			if seen[mapping] {
				continue
			}
			seen[mapping] = true
			promoted = append(promoted, promotedMapping{
				node:   mapping,
				target: decodedField.YAMLDecodePath[index].Type,
			})
		}
	}

	n.Content = content
	for _, mapping := range promoted {
		remapJSONTagKeys(mapping.node, mapping.target)
	}
}

// promotedMapping identifies a generated mapping for an embedded struct.
type promotedMapping struct {
	// node contains fields moved below an anonymous struct field.
	node *goyaml.Node
	// target is the anonymous struct type represented by node.
	target reflect.Type
}

// insertYAMLMappingPath nests a promoted field below generated mappings.
func insertYAMLMappingPath(
	content *[]*goyaml.Node,
	path []string,
	keyNode, valueNode *goyaml.Node,
) []*goyaml.Node {
	if len(path) <= 1 {
		*content = append(*content, keyNode, valueNode)
		return nil
	}

	mappings := make([]*goyaml.Node, 0, len(path)-1)
	current := content
	for _, key := range path[:len(path)-1] {
		var mapping *goyaml.Node
		for index := 0; index+1 < len(*current); index += 2 {
			if (*current)[index].Value == key &&
				(*current)[index+1].Kind == goyaml.MappingNode {
				mapping = (*current)[index+1]
				break
			}
		}

		if mapping == nil {
			mapping = &goyaml.Node{Kind: goyaml.MappingNode}
			*current = append(
				*current,
				&goyaml.Node{Kind: goyaml.ScalarNode, Tag: "!!str", Value: key},
				mapping,
			)
		}

		mappings = append(mappings, mapping)
		current = &mapping.Content
	}

	keyNode.Value = path[len(path)-1]
	*current = append(*current, keyNode, valueNode)

	return mappings
}

// derefType strips pointers until a concrete type is reached.
func derefType(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}
