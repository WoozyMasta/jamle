// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package jamle

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"reflect"
	"sync"

	"github.com/woozymasta/jamle/internal/schema"
	jyaml "github.com/woozymasta/jamle/yaml"
	goyaml "go.yaml.in/yaml/v3"
)

var (
	// errDefaultAliasCycle reports a recursive alias graph during default traversal.
	errDefaultAliasCycle = errors.New("yaml: alias cycle while applying defaults")
	// defaultsPresenceCache stores the immutable recursive-defaults result by type.
	defaultsPresenceCache sync.Map
	// defaultNodeCache stores immutable parsed YAML values by raw default tag.
	defaultNodeCache sync.Map
)

// defaultNodeResult stores a parsed default value or its stable parse error.
type defaultNodeResult struct {
	// node is the immutable YAML value parsed from a default tag.
	node *goyaml.Node
	// err is the parse error returned for an invalid default tag.
	err error
}

// hasDefaults reports whether t or any reachable schema field needs defaults.
func hasDefaults(t reflect.Type) bool {
	t = dereferenceExpansionType(t)
	if t == nil {
		return false
	}
	if cached, ok := defaultsPresenceCache.Load(t); ok {
		return cached.(bool)
	}

	result := hasDefaultsSeen(t, make(map[reflect.Type]bool))
	cached, _ := defaultsPresenceCache.LoadOrStore(t, result)

	return cached.(bool)
}

// hasDefaultsSeen walks shared schema metadata while preventing recursive types from looping.
func hasDefaultsSeen(t reflect.Type, seen map[reflect.Type]bool) bool {
	t = dereferenceExpansionType(t)
	if t == nil || schema.IsOpaque(t) || seen[t] {
		return false
	}

	seen[t] = true
	defer delete(seen, t)

	switch t.Kind() {
	case reflect.Struct:
		info := schema.For(t)
		if info.HasDefaults {
			return true
		}
		for _, field := range info.Fields {
			if !field.SkipDefaults && hasDefaultsSeen(field.Type, seen) {
				return true
			}
		}

	case reflect.Array, reflect.Slice, reflect.Map:
		return hasDefaultsSeen(t.Elem(), seen)
	}

	return false
}

// applyDefaults applies source-aware defaults to one decoded destination.
func applyDefaults(root *goyaml.Node, target any) error {
	if target == nil {
		return nil
	}

	return applyDefaultsValue(reflect.ValueOf(target), root, "$", make(map[*goyaml.Node]bool))
}

// applyDefaultsValue traverses decoded values and their source nodes together.
func applyDefaultsValue(
	dst reflect.Value,
	node *goyaml.Node,
	path string,
	active map[*goyaml.Node]bool,
) error {
	if !dst.IsValid() {
		return nil
	}

	if node != nil {
		for node.Kind == goyaml.DocumentNode && len(node.Content) > 0 {
			node = node.Content[0]
		}
		if node.Kind == goyaml.AliasNode {
			if node.Alias == nil {
				return nil
			}
			if active[node] {
				return fmt.Errorf("%s: %w", path, errDefaultAliasCycle)
			}

			active[node] = true
			defer delete(active, node)
			return applyDefaultsValue(dst, node.Alias, path, active)
		}
		if node.Tag == "!!null" {
			return nil
		}
	}

	if dst.Kind() == reflect.Pointer {
		if dst.IsNil() {
			return nil
		}

		return applyDefaultsValue(dst.Elem(), node, path, active)
	}
	if schema.IsOpaque(dst.Type()) {
		return nil
	}

	switch dst.Kind() {
	case reflect.Struct:
		return applyDefaultsStruct(dst, node, path, active)

	case reflect.Slice, reflect.Array:
		return applyDefaultsSequence(dst, node, path, active)

	case reflect.Map:
		return applyDefaultsMap(dst, node, path, active)

	default:
		return nil
	}
}

// applyDefaultsStruct applies field defaults using schema-selected source keys.
func applyDefaultsStruct(
	dst reflect.Value,
	node *goyaml.Node,
	path string,
	active map[*goyaml.Node]bool,
) error {
	info := schema.For(dst.Type())
	sources, err := collectDefaultSources(node, dst.Type())
	if err != nil {
		return err
	}

	for _, field := range info.Fields {
		fieldPath := joinDefaultPath(path, field.JSONName)
		source, present := sources[field.JSONName]
		fieldValue, ok := defaultFieldValue(dst, field.Index, false)
		if !ok {
			needsValue := present || field.HasDefault ||
				(!field.SkipDefaults && hasDefaults(field.Type))
			if !needsValue {
				continue
			}

			fieldValue, ok = defaultFieldValue(dst, field.Index, true)
			if !ok {
				continue
			}
		}

		if present {
			if field.SkipDefaults || field.Opaque {
				continue
			}

			if err := applyDefaultsValue(fieldValue, source, fieldPath, active); err != nil {
				return err
			}

			continue
		}

		if field.HasDefault {
			if fieldValue.IsZero() {
				defaultNode, err := parseDefaultNode(field.Default)
				if err != nil {
					return fmt.Errorf("default for %s: %w", fieldPath, err)
				}

				if err := decodeDefaultValue(fieldValue, defaultNode, fieldPath); err != nil {
					return err
				}

				if !field.SkipDefaults {
					if err := applyDefaultsValue(fieldValue, defaultNode, fieldPath, active); err != nil {
						return err
					}
				}

				continue
			}
		}

		if field.SkipDefaults || !hasDefaults(field.Type) {
			continue
		}
		if err := applyDefaultsValue(fieldValue, nil, fieldPath, active); err != nil {
			return err
		}
	}

	return nil
}

// applyDefaultsSequence applies nested defaults to decoded sequence elements.
func applyDefaultsSequence(
	dst reflect.Value,
	node *goyaml.Node,
	path string,
	active map[*goyaml.Node]bool,
) error {
	if node == nil || node.Kind != goyaml.SequenceNode {
		return nil
	}

	limit := min(dst.Len(), len(node.Content))
	for index := range limit {
		itemPath := fmt.Sprintf("%s[%d]", path, index)
		if err := applyDefaultsValue(dst.Index(index), node.Content[index], itemPath, active); err != nil {
			return err
		}
	}

	return nil
}

// applyDefaultsMap applies nested defaults to decoded map values.
func applyDefaultsMap(
	dst reflect.Value,
	node *goyaml.Node,
	path string,
	active map[*goyaml.Node]bool,
) error {
	if node == nil || node.Kind != goyaml.MappingNode || dst.IsNil() {
		return nil
	}

	for index := 0; index+1 < len(node.Content); index += 2 {
		keyNode := node.Content[index]
		valueNode := node.Content[index+1]
		key, err := decodeDefaultMapKey(keyNode, dst.Type().Key())
		if err != nil {
			return fmt.Errorf("default traversal at %s: %w", path, err)
		}

		value := dst.MapIndex(key)
		if !value.IsValid() || !value.CanInterface() {
			continue
		}

		updated := reflect.New(dst.Type().Elem()).Elem()
		updated.Set(value)
		if err := applyDefaultsValue(
			updated,
			valueNode,
			joinDefaultPath(path, keyNode.Value),
			active,
		); err != nil {
			return err
		}
		dst.SetMapIndex(key, updated)
	}

	return nil
}

// collectDefaultSources returns effective struct field sources, including YAML merges.
func collectDefaultSources(node *goyaml.Node, target reflect.Type) (map[string]*goyaml.Node, error) {
	return collectDefaultMappingSources(node, target, make(map[*goyaml.Node]bool))
}

// collectDefaultMappingSources returns effective field sources for one mapping.
func collectDefaultMappingSources(
	node *goyaml.Node,
	target reflect.Type,
	active map[*goyaml.Node]bool,
) (map[string]*goyaml.Node, error) {
	if node == nil {
		return nil, nil
	}

	for node.Kind == goyaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}
	if node.Kind == goyaml.AliasNode {
		if node.Alias == nil {
			return nil, nil
		}
		if active[node] {
			return nil, errDefaultAliasCycle
		}

		active[node] = true
		defer delete(active, node)
		return collectDefaultMappingSources(node.Alias, target, active)
	}

	if node.Kind != goyaml.MappingNode {
		return nil, nil
	}
	if len(node.Content) == 0 {
		return nil, nil
	}

	lookup := schema.For(target)
	sources := make(map[string]*goyaml.Node)
	explicit := make(map[string]*goyaml.Node)
	for index := 0; index+1 < len(node.Content); index += 2 {
		keyNode := node.Content[index]
		valueNode := node.Content[index+1]
		if isMergeKey(keyNode) {
			if valueNode.Kind == goyaml.SequenceNode {
				for _, source := range valueNode.Content {
					merged, err := collectDefaultMappingSources(source, target, active)
					if err != nil {
						return nil, err
					}

					for name, sourceNode := range merged {
						if _, exists := sources[name]; !exists {
							sources[name] = sourceNode
						}
					}
				}
				continue
			}

			merged, err := collectDefaultMappingSources(valueNode, target, active)
			if err != nil {
				return nil, err
			}

			for name, sourceNode := range merged {
				if _, exists := sources[name]; !exists {
					sources[name] = sourceNode
				}
			}

			continue
		}

		field, ok := lookup.Lookup(keyNode.Value)
		if ok && field.YAMLDecodeKey != "-" {
			explicit[field.JSONName] = valueNode
		}
	}

	maps.Copy(sources, explicit)
	return sources, nil
}

// defaultFieldValue resolves a schema field index and optionally allocates embedded pointers.
func defaultFieldValue(root reflect.Value, index []int, allocate bool) (reflect.Value, bool) {
	current := root
	for _, fieldIndex := range index {
		for current.Kind() == reflect.Pointer {
			if current.IsNil() {
				if !allocate || !current.CanSet() {
					return reflect.Value{}, false
				}

				current.Set(reflect.New(current.Type().Elem()))
			}
			current = current.Elem()
		}

		if current.Kind() != reflect.Struct || fieldIndex >= current.NumField() {
			return reflect.Value{}, false
		}

		current = current.Field(fieldIndex)
	}

	return current, current.IsValid() && current.CanSet()
}

// parseDefaultNode parses one default tag as a YAML value.
func parseDefaultNode(raw string) (*goyaml.Node, error) {
	if cached, ok := defaultNodeCache.Load(raw); ok {
		result := cached.(defaultNodeResult)
		return result.node, result.err
	}

	result := parseDefaultNodeUncached(raw)
	cached, _ := defaultNodeCache.LoadOrStore(raw, result)
	result = cached.(defaultNodeResult)

	return result.node, result.err
}

// parseDefaultNodeUncached parses one default tag without using the cache.
func parseDefaultNodeUncached(raw string) defaultNodeResult {
	if raw == "" {
		return defaultNodeResult{
			node: &goyaml.Node{Kind: goyaml.ScalarNode, Tag: "!!null", Value: "null"},
		}
	}

	var root goyaml.Node
	dec := goyaml.NewDecoder(bytes.NewReader([]byte(raw)))
	if err := dec.Decode(&root); err != nil {
		if errors.Is(err, io.EOF) {
			return defaultNodeResult{
				node: &goyaml.Node{Kind: goyaml.ScalarNode, Tag: "!!null", Value: "null"},
			}
		}

		return defaultNodeResult{err: err}
	}

	var extra goyaml.Node
	if err := dec.Decode(&extra); err == nil {
		return defaultNodeResult{err: jyaml.ErrMultipleDocuments}
	} else if !errors.Is(err, io.EOF) {
		return defaultNodeResult{err: err}
	}

	return defaultNodeResult{node: &root}
}

// decodeDefaultValue decodes one parsed default into a destination field.
func decodeDefaultValue(dst reflect.Value, node *goyaml.Node, path string) error {
	if !dst.IsValid() || !dst.CanAddr() {
		return fmt.Errorf("default for %s cannot be assigned", path)
	}

	var err error
	if node != nil && node.Kind == goyaml.ScalarNode {
		// Scalar defaults cannot contain nested JSON-tagged fields,
		// so a direct node decode avoids cloning the one-node graph first.
		err = node.Decode(dst.Addr().Interface())
	} else {
		err = jyaml.UnmarshalNode(node, dst.Addr().Interface())
	}
	if err != nil {
		return fmt.Errorf("default for %s: %w", path, err)
	}

	return nil
}

// decodeDefaultMapKey decodes a source key using the destination map key type.
func decodeDefaultMapKey(node *goyaml.Node, target reflect.Type) (reflect.Value, error) {
	key := reflect.New(target)
	if err := node.Decode(key.Interface()); err != nil {
		return reflect.Value{}, err
	}

	return key.Elem(), nil
}

// joinDefaultPath appends one field or key segment to a diagnostic path.
func joinDefaultPath(path, segment string) string {
	if path == "$" {
		return "$" + "." + segment
	}

	return path + "." + segment
}
