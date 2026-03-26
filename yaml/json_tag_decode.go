// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package yaml

import (
	"reflect"
	"strings"
	"sync"

	goyaml "go.yaml.in/yaml/v3"
)

// structFieldLookupCache stores precomputed JSON-key to YAML-key mappings.
var structFieldLookupCache struct {
	sync.RWMutex
	m map[reflect.Type]structFieldLookup
}

// structFieldLookup stores exact and folded JSON-key lookup for one struct.
type structFieldLookup struct {
	exact  map[string]decodeField
	folded map[string]decodeField
}

// decodeField links JSON-key matches to YAML decode target metadata.
type decodeField struct {
	typ       reflect.Type
	decodeKey string
}

// UnmarshalNode decodes a YAML node into target using JSON-tag key mapping.
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
	if target.Kind() != reflect.Ptr {
		return node.Decode(o)
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
	lookup := cachedStructFieldLookup(target)
	for i := 0; i+1 < len(n.Content); i += 2 {
		keyNode := n.Content[i]
		valNode := n.Content[i+1]

		decodedField, ok := lookup.exact[keyNode.Value]
		if !ok {
			decodedField, ok = lookup.folded[strings.ToLower(keyNode.Value)]
		}
		if !ok {
			continue
		}

		keyNode.Value = decodedField.decodeKey
		remapJSONTagKeys(valNode, decodedField.typ)
	}
}

// cachedStructFieldLookup returns cached struct key lookup metadata.
func cachedStructFieldLookup(t reflect.Type) structFieldLookup {
	structFieldLookupCache.RLock()
	lookup, ok := structFieldLookupCache.m[t]
	structFieldLookupCache.RUnlock()
	if ok {
		return lookup
	}

	lookup = buildStructFieldLookup(t)

	structFieldLookupCache.Lock()
	if structFieldLookupCache.m == nil {
		structFieldLookupCache.m = map[reflect.Type]structFieldLookup{}
	}
	structFieldLookupCache.m[t] = lookup
	structFieldLookupCache.Unlock()

	return lookup
}

// buildStructFieldLookup builds mapping from JSON keys to target field metadata.
func buildStructFieldLookup(t reflect.Type) structFieldLookup {
	fields := cachedTypeFields(t)
	lookup := structFieldLookup{
		exact:  make(map[string]decodeField, len(fields)),
		folded: make(map[string]decodeField, len(fields)),
	}

	for i := range fields {
		ff := fields[i]
		if len(ff.index) == 0 {
			continue
		}

		sf := t.FieldByIndex(ff.index)
		df := decodeField{
			decodeKey: yamlDecodeKeyForField(sf),
			typ:       sf.Type,
		}

		if _, exists := lookup.exact[ff.name]; !exists {
			lookup.exact[ff.name] = df
		}

		folded := strings.ToLower(ff.name)
		if _, exists := lookup.folded[folded]; !exists {
			lookup.folded[folded] = df
		}
	}

	return lookup
}

// derefType strips pointers until a concrete type is reached.
func derefType(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t
}

// yamlDecodeKeyForField returns the key name go-yaml uses for struct field.
func yamlDecodeKeyForField(sf reflect.StructField) string {
	if tag := sf.Tag.Get("yaml"); tag != "" {
		name, _, _ := strings.Cut(tag, ",")
		if name != "" && name != "-" {
			return name
		}
	}

	return strings.ToLower(sf.Name)
}
