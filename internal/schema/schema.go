// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

// Package schema provides cached struct metadata shared by jamle transforms.
package schema

import (
	"encoding"
	"reflect"
	"sort"
	"strings"
	"sync"
	"unicode"

	goyaml "go.yaml.in/yaml/v3"
)

var (
	// cache stores immutable metadata by the dereferenced struct type.
	cache sync.Map

	// yamlUnmarshalerType identifies types that own YAML node decoding.
	yamlUnmarshalerType = reflect.TypeFor[goyaml.Unmarshaler]()

	// textUnmarshalerType identifies types that own scalar text decoding.
	textUnmarshalerType = reflect.TypeFor[encoding.TextUnmarshaler]()
)

// Field describes one field selected by encoding/json-compatible rules.
type Field struct {
	// Type is the declared Go type of the field.
	Type reflect.Type
	// JSONName is the canonical encoding/json field name.
	JSONName string
	// YAMLDecodeKey is the key accepted by the go-yaml decoder.
	YAMLDecodeKey string
	// YAMLDecodePath contains mapping levels needed for promoted fields.
	YAMLDecodePath []YAMLPathElement
	// Default contains the raw default tag value.
	Default string
	// Index is the path from the root struct to the field.
	Index []int
	// NoExpand reports whether the field has jamle:"noexpand".
	NoExpand bool
	// HasDefault reports whether Default is an active default value.
	HasDefault bool
	// SkipDefaults reports whether default processing is disabled below the field.
	SkipDefaults bool
	// Opaque reports whether the field owns its decode operation.
	Opaque bool
}

// Info contains immutable metadata for one dereferenced struct type returned by [For].
type Info struct {
	// Type is the dereferenced type represented by this metadata.
	Type reflect.Type
	// exact maps canonical JSON names to field indexes.
	exact map[string]int
	// yamlExact maps canonical YAML decode keys to field indexes.
	yamlExact map[string]int
	// folded maps lowercase names to field indexes for the common lookup path.
	folded map[string]int
	// yamlFolded maps lowercase YAML decode keys to field indexes.
	yamlFolded map[string]int
	// Fields contains fields selected by JSON embedding rules.
	Fields []Field
	// HasDefaults reports whether a selected field has a default tag.
	HasDefaults bool
	// HasNoExpand reports whether a selected field has a noexpand tag.
	HasNoExpand bool
	// Opaque reports whether the type owns its decode operation.
	Opaque bool
}

// YAMLPathElement describes one mapping level around a promoted field.
type YAMLPathElement struct {
	// Type is the embedded struct type represented by the mapping.
	Type reflect.Type
	// Key is the mapping key used by go-yaml.
	Key string
}

// For returns cached [Info] metadata for t. Pointer layers are ignored.
func For(t reflect.Type) *Info {
	t = dereference(t)
	if t == nil {
		return emptyInfo(nil)
	}

	if cached, ok := cache.Load(t); ok {
		return cached.(*Info)
	}

	info := buildInfo(t)
	actual, _ := cache.LoadOrStore(t, info)

	return actual.(*Info)
}

// Lookup resolves a JSON or YAML decode key using exact
// and case-insensitive rules and returns the matching [Field].
func (i *Info) Lookup(name string) (Field, bool) {
	if i == nil {
		return Field{}, false
	}

	if index, ok := i.exact[name]; ok {
		return i.Fields[index], true
	}
	if index, ok := i.yamlExact[name]; ok {
		return i.Fields[index], true
	}

	if index, ok := i.folded[strings.ToLower(name)]; ok {
		return i.Fields[index], true
	}
	if index, ok := i.yamlFolded[strings.ToLower(name)]; ok {
		return i.Fields[index], true
	}

	// Lowercasing is the common fast path.
	// EqualFold covers Unicode folds such as Kelvin sign
	// and long s without complicating the cache key.
	for index := range i.Fields {
		field := i.Fields[index]
		if strings.EqualFold(field.JSONName, name) ||
			strings.EqualFold(field.YAMLDecodeKey, name) {
			return i.Fields[index], true
		}
	}

	return Field{}, false
}

// IsOpaque reports whether t owns its decoding through a supported interface.
func IsOpaque(t reflect.Type) bool {
	t = dereference(t)
	if t == nil {
		return false
	}

	return implementsDecoder(t) || implementsDecoder(reflect.PointerTo(t))
}

// buildInfo discovers fields and derives lookup metadata for t.
func buildInfo(t reflect.Type) *Info {
	if t.Kind() != reflect.Struct {
		return emptyInfo(t)
	}

	info := &Info{
		Type:       t,
		exact:      make(map[string]int),
		yamlExact:  make(map[string]int),
		folded:     make(map[string]int),
		yamlFolded: make(map[string]int),
	}
	if IsOpaque(t) {
		info.Opaque = true
		return info
	}

	fields := discoverFields(t)
	info.Fields = make([]Field, 0, len(fields))

	for _, discovered := range fields {
		field := buildField(t, discovered)
		info.Fields = append(info.Fields, field)
		if field.HasDefault {
			info.HasDefaults = true
		}
		if field.NoExpand {
			info.HasNoExpand = true
		}
	}

	for index := range info.Fields {
		field := info.Fields[index]
		info.exact[field.JSONName] = index
		if field.YAMLDecodeKey != "-" {
			info.yamlExact[field.YAMLDecodeKey] = index
		}
		info.folded[strings.ToLower(field.JSONName)] = index
		if field.YAMLDecodeKey != "-" {
			info.yamlFolded[strings.ToLower(field.YAMLDecodeKey)] = index
		}
	}

	return info
}

// emptyInfo returns metadata for types without struct fields.
func emptyInfo(t reflect.Type) *Info {
	return &Info{
		Type:       t,
		exact:      make(map[string]int),
		yamlExact:  make(map[string]int),
		folded:     make(map[string]int),
		yamlFolded: make(map[string]int),
	}
}

// discoveredField is the intermediate field representation used by discovery.
type discoveredField struct {
	// name is the candidate JSON field name.
	name string
	// index is the path from the root struct to the candidate field.
	index []int
	// tag reports whether the candidate has an explicit JSON name.
	tag bool
}

// queuedField tracks one anonymous struct type at a breadth-first level.
type queuedField struct {
	// fieldType is the embedded struct type being discovered.
	fieldType reflect.Type
	// index is the path to the embedded field.
	index []int
	// count is the number of promotion paths reaching fieldType at this level.
	count int
}

// discoverFields applies encoding/json's breadth and dominance rules.
func discoverFields(t reflect.Type) []discoveredField {
	current := []queuedField{{fieldType: t}}
	visited := make(map[reflect.Type]bool)
	var fields []discoveredField

	for len(current) > 0 {
		next := make(map[reflect.Type]queuedField)

		for _, parent := range current {
			if visited[parent.fieldType] {
				continue
			}

			visited[parent.fieldType] = true

			for fieldIndex := 0; fieldIndex < parent.fieldType.NumField(); fieldIndex++ {
				sf := parent.fieldType.Field(fieldIndex)
				if shouldSkip(sf) {
					continue
				}

				fieldPath := appendIndex(parent.index, fieldIndex)
				fieldType := embeddedType(sf.Type)
				name, tagged := jsonName(sf)

				if name != "" || !sf.Anonymous || fieldType.Kind() != reflect.Struct {
					if name == "" {
						name = sf.Name
					}

					field := discoveredField{
						index: fieldPath,
						name:  name,
						tag:   tagged,
					}

					fields = append(fields, field)
					if parent.count > 1 {
						fields = append(fields, field)
					}

					continue
				}

				queued, ok := next[fieldType]
				if !ok {
					queued = queuedField{index: fieldPath, fieldType: fieldType}
				}
				queued.count++
				next[fieldType] = queued
			}
		}

		current = current[:0]
		for _, queued := range next {
			current = append(current, queued)
		}
	}

	return resolveDominance(fields)
}

// resolveDominance applies JSON name and embedding conflict rules.
func resolveDominance(fields []discoveredField) []discoveredField {
	sort.Slice(fields, func(i, j int) bool {
		if fields[i].name != fields[j].name {
			return fields[i].name < fields[j].name
		}

		if len(fields[i].index) != len(fields[j].index) {
			return len(fields[i].index) < len(fields[j].index)
		}

		if fields[i].tag != fields[j].tag {
			return fields[i].tag
		}

		return compareIndex(fields[i].index, fields[j].index)
	})

	selected := make([]discoveredField, 0, len(fields))
	for start := 0; start < len(fields); {
		end := start + 1
		for end < len(fields) && fields[end].name == fields[start].name {
			end++
		}

		if field, ok := dominant(fields[start:end]); ok {
			selected = append(selected, field)
		}
		start = end
	}

	sort.Slice(selected, func(i, j int) bool {
		return compareIndex(selected[i].index, selected[j].index)
	})

	return selected
}

// dominant returns the unique field allowed by JSON embedding rules.
func dominant(fields []discoveredField) (discoveredField, bool) {
	if len(fields) == 0 {
		return discoveredField{}, false
	}

	depth := len(fields[0].index)
	shortest := make([]discoveredField, 0, len(fields))
	for _, field := range fields {
		if len(field.index) != depth {
			break
		}

		shortest = append(shortest, field)
	}

	tagged := -1
	for index, field := range shortest {
		if !field.tag {
			continue
		}

		if tagged >= 0 {
			return discoveredField{}, false
		}

		tagged = index
	}

	if tagged >= 0 {
		return shortest[tagged], true
	}
	if len(shortest) != 1 {
		return discoveredField{}, false
	}

	return shortest[0], true
}

// buildField adds tags and final field metadata to a discovered field.
func buildField(root reflect.Type, discovered discoveredField) Field {
	sf := root.FieldByIndex(discovered.index)
	jsonName, _ := jsonName(sf)
	if jsonName == "" {
		jsonName = sf.Name
	}

	yamlKey := yamlDecodeKey(sf)
	defaultValue, hasDefault := sf.Tag.Lookup("default")

	return Field{
		Index:          append([]int(nil), discovered.index...),
		Type:           sf.Type,
		JSONName:       jsonName,
		YAMLDecodeKey:  yamlKey,
		YAMLDecodePath: yamlDecodePath(root, discovered.index),
		NoExpand:       hasNoExpandOption(root, discovered.index),
		Default:        defaultValue,
		HasDefault:     hasDefault && defaultValue != "-",
		SkipDefaults:   defaultValue == "-",
		Opaque:         IsOpaque(sf.Type),
	}
}

// yamlDecodePath returns mapping levels required by go-yaml for promotion.
func yamlDecodePath(root reflect.Type, index []int) []YAMLPathElement {
	if len(index) < 2 {
		return nil
	}

	current := dereference(root)
	path := make([]YAMLPathElement, 0, len(index))

	for _, fieldIndex := range index[:len(index)-1] {
		if current == nil || current.Kind() != reflect.Struct {
			return nil
		}

		field := current.Field(fieldIndex)
		if !tagOptions(field.Tag.Get("yaml")).contains("inline") {
			key := yamlDecodeKey(field)
			if key == "" || key == "-" {
				return nil
			}

			path = append(path, YAMLPathElement{Key: key, Type: field.Type})
		}

		current = dereference(field.Type)
	}

	return path
}

// hasNoExpandOption reports whether a field
// or one of its containing fields carries jamle:"noexpand".
func hasNoExpandOption(root reflect.Type, index []int) bool {
	current := root
	for _, fieldIndex := range index {
		current = dereference(current)
		if current == nil || current.Kind() != reflect.Struct {
			return false
		}

		field := current.Field(fieldIndex)
		if tagOptions(field.Tag.Get("jamle")).contains("noexpand") {
			return true
		}

		current = field.Type
	}

	return false
}

// shouldSkip reports whether sf is not part of the JSON field set.
func shouldSkip(sf reflect.StructField) bool {
	if sf.PkgPath != "" &&
		(!sf.Anonymous || embeddedType(sf.Type).Kind() != reflect.Struct) {
		return true
	}

	name, _ := jsonName(sf)
	return name == "-"
}

// jsonName returns the valid JSON tag name and whether it was explicitly set.
func jsonName(sf reflect.StructField) (string, bool) {
	name := splitTag(sf.Tag.Get("json"))
	if name == "" || !validTagName(name) {
		return "", false
	}

	return name, true
}

// yamlDecodeKey returns the compatibility key used by go-yaml decoding.
func yamlDecodeKey(sf reflect.StructField) string {
	name := splitTag(sf.Tag.Get("yaml"))
	if name != "" {
		return name
	}

	return strings.ToLower(sf.Name)
}

// splitTag separates a struct tag name from comma-separated options.
func splitTag(tag string) string {
	name, _, _ := strings.Cut(tag, ",")

	return name
}

// tagOptions stores comma-separated jamle options.
type tagOptions string

// contains reports whether a tag option is present.
func (o tagOptions) contains(want string) bool {
	for option := range strings.SplitSeq(string(o), ",") {
		if strings.TrimSpace(option) == want {
			return true
		}
	}

	return false
}

// validTagName reports whether a JSON tag name is valid.
func validTagName(name string) bool {
	if name == "" {
		return false
	}

	for _, char := range name {
		if strings.ContainsRune("!#$%&()*+-./:<=>?@[]^_{|}~ ", char) {
			continue
		}
		if !unicode.IsLetter(char) && !unicode.IsDigit(char) {
			return false
		}
	}

	return true
}

// embeddedType dereferences an anonymous pointer for promotion decisions.
func embeddedType(t reflect.Type) reflect.Type {
	if t.Kind() == reflect.Pointer {
		return t.Elem()
	}

	return t
}

// appendIndex returns a new field index path with one child index.
func appendIndex(parent []int, child int) []int {
	index := make([]int, len(parent)+1)
	copy(index, parent)
	index[len(parent)] = child

	return index
}

// compareIndex orders field paths lexicographically.
func compareIndex(left, right []int) bool {
	for index := 0; index < len(left) && index < len(right); index++ {
		if left[index] != right[index] {
			return left[index] < right[index]
		}
	}

	return len(left) < len(right)
}

// dereference strips pointer layers from a type.
func dereference(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	return t
}

// implementsDecoder checks both node and scalar decode interfaces.
func implementsDecoder(t reflect.Type) bool {
	return t.Implements(yamlUnmarshalerType) || t.Implements(textUnmarshalerType)
}
