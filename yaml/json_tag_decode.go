// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package yaml

import (
	"errors"
	"reflect"

	"github.com/woozymasta/jamle/internal/schema"
	nodeview "github.com/woozymasta/jamle/internal/yamlnode"
	goyaml "go.yaml.in/yaml/v3"
)

var (
	// errJSONTagAliasCycle reports a recursive alias graph during key remapping.
	errJSONTagAliasCycle = errors.New("yaml: alias cycle")
)

// UnmarshalNode decodes a YAML node into target using JSON-tag key mapping.
// The caller-owned node graph is not modified.
func UnmarshalNode(root *goyaml.Node, o any) error {
	return UnmarshalNodeWithOptions(root, o, DecodeOptions{})
}

// validateDecodeTarget rejects destinations that cannot receive decoded data.
func validateDecodeTarget(o any) error {
	if o == nil {
		return ErrInvalidDestination
	}

	target := reflect.ValueOf(o)
	if target.Kind() != reflect.Pointer || target.IsNil() {
		return ErrInvalidDestination
	}

	return nil
}

// unmarshalNode decodes a node after option-specific validation has completed.
func unmarshalNode(root *goyaml.Node, o any) error {
	if root == nil {
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

	if err := remapJSONTagKeys(node, target.Elem()); err != nil {
		return err
	}

	return node.Decode(o)
}

// remapJSONTagKeys rewrites mapping keys to match struct field names.
func remapJSONTagKeys(n *goyaml.Node, target reflect.Type) error {
	remapper := jsonTagRemapper{
		aliasSources: make(map[*goyaml.Node]*goyaml.Node),
		active:       make(map[*goyaml.Node]bool),
		activeSource: make(map[*goyaml.Node]bool),
	}
	remapper.registerAliasSources(cloneNodeGraph(n), n)

	return remapper.remap(n, target)
}

// jsonTagRemapper applies JSON-tag mapping without sharing alias mutations.
type jsonTagRemapper struct {
	// aliasSources maps remapped alias consumers to targets
	// in the immutable source graph used to materialize consumer-specific views.
	aliasSources map[*goyaml.Node]*goyaml.Node
	// active prevents recursive aliases from re-entering the same node.
	active map[*goyaml.Node]bool
	// activeSource prevents recursive aliases from re-materializing one target.
	activeSource map[*goyaml.Node]bool
}

// registerAliasSources pairs a mutable graph with its immutable source graph.
func (r *jsonTagRemapper) registerAliasSources(source, view *goyaml.Node) {
	visited := make(map[*goyaml.Node]*goyaml.Node)

	var register func(*goyaml.Node, *goyaml.Node)
	register = func(sourceNode, viewNode *goyaml.Node) {
		if sourceNode == nil || viewNode == nil {
			return
		}
		if previous, ok := visited[viewNode]; ok && previous == sourceNode {
			return
		}
		visited[viewNode] = sourceNode

		if viewNode.Kind == goyaml.AliasNode && sourceNode.Alias != nil {
			r.aliasSources[viewNode] = sourceNode.Alias
		}

		for index := range viewNode.Content {
			if index >= len(sourceNode.Content) {
				break
			}

			register(sourceNode.Content[index], viewNode.Content[index])
		}
		register(sourceNode.Alias, viewNode.Alias)
	}

	register(source, view)
}

// materializeAlias clones one immutable alias target and registers nested aliases.
func (r *jsonTagRemapper) materializeAlias(source *goyaml.Node) *goyaml.Node {
	clones := make(map[*goyaml.Node]*goyaml.Node)

	var clone func(*goyaml.Node) *goyaml.Node
	clone = func(node *goyaml.Node) *goyaml.Node {
		if node == nil {
			return nil
		}
		if copied, ok := clones[node]; ok {
			return copied
		}

		copied := *node
		copied.Content = nil
		copied.Alias = nil
		clones[node] = &copied

		if node.Kind == goyaml.AliasNode && node.Alias != nil {
			r.aliasSources[&copied] = node.Alias
		}

		if len(node.Content) > 0 {
			copied.Content = make([]*goyaml.Node, len(node.Content))
			for index, child := range node.Content {
				copied.Content[index] = clone(child)
			}
		}
		copied.Alias = clone(node.Alias)

		return &copied
	}

	return clone(source)
}

// remap rewrites one node and follows aliases using their consumer type.
func (r *jsonTagRemapper) remap(n *goyaml.Node, target reflect.Type) error {
	if n == nil {
		return nil
	}
	if r.active[n] {
		return errJSONTagAliasCycle
	}
	r.active[n] = true
	defer delete(r.active, n)

	target = derefType(target)
	if target == nil {
		return nil
	}
	if schema.IsOpaque(target) {
		return nil
	}

	switch n.Kind {
	case goyaml.DocumentNode:
		if len(n.Content) > 0 {
			return r.remap(n.Content[0], target)
		}

	case goyaml.AliasNode:
		if n.Alias == nil {
			return nil
		}

		source := r.aliasSources[n]
		if source == nil {
			return nil
		}
		if r.activeSource[source] {
			return errJSONTagAliasCycle
		}

		r.activeSource[source] = true
		defer delete(r.activeSource, source)
		n.Alias = r.materializeAlias(source)
		return r.remap(n.Alias, target)

	case goyaml.MappingNode:
		return r.remapMappingNodeKeys(n, target)

	case goyaml.SequenceNode:
		elem := target
		if target.Kind() == reflect.Slice || target.Kind() == reflect.Array {
			elem = target.Elem()
		}
		elem = derefType(elem)
		if elem == nil {
			return nil
		}

		for _, child := range n.Content {
			if err := r.remap(child, elem); err != nil {
				return err
			}
		}
	}

	return nil
}

// remapMappingNodeKeys remaps mapping keys for struct/map targets recursively.
func (r *jsonTagRemapper) remapMappingNodeKeys(n *goyaml.Node, target reflect.Type) error {
	switch target.Kind() {
	case reflect.Struct:
		return r.remapStructMappingNodeKeys(n, target)

	case reflect.Map:
		elem := derefType(target.Elem())
		if elem == nil {
			return nil
		}

		for i := 1; i < len(n.Content); i += 2 {
			if nodeview.IsMergeKey(n.Content[i-1]) {
				if err := r.remap(n.Content[i], target); err != nil {
					return err
				}
				continue
			}

			if err := r.remap(n.Content[i], elem); err != nil {
				return err
			}
		}
	}

	return nil
}

// remapStructMappingNodeKeys remaps one struct mapping node by JSON tag rules.
func (r *jsonTagRemapper) remapStructMappingNodeKeys(n *goyaml.Node, target reflect.Type) error {
	lookup := schema.For(target)
	promoted := make([]promotedMapping, 0)
	seen := make(map[*goyaml.Node]bool)
	content := make([]*goyaml.Node, 0, len(n.Content))

	for i := 0; i+1 < len(n.Content); i += 2 {
		keyNode := n.Content[i]
		valNode := n.Content[i+1]
		if nodeview.IsMergeKey(keyNode) {
			// A merge source supplies entries to the current container.
			// Remap it as that container, not as a single field value.
			if err := r.remap(valNode, target); err != nil {
				return err
			}
			content = append(content, keyNode, valNode)
			continue
		}

		decodedField, ok := lookup.Lookup(keyNode.Value)
		if !ok {
			if lookup.IsIgnoredKey(keyNode.Value) {
				continue
			}

			content = append(content, keyNode, valNode)
			continue
		}
		if decodedField.YAMLDecodeKey == "-" {
			continue
		}

		keyNode.Value = decodedField.YAMLDecodeKey
		if len(decodedField.YAMLDecodePath) == 0 {
			if err := r.remap(valNode, decodedField.Type); err != nil {
				return err
			}
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
		if err := r.remap(mapping.node, mapping.target); err != nil {
			return err
		}
	}

	return nil
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
