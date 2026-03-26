// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package yaml

import (
	"reflect"
	"sort"
	"sync"
)

// fieldCache stores computed struct field metadata by type.
var fieldCache struct {
	sync.RWMutex
	m map[reflect.Type][]field
}

// typeFields returns a list of fields that JSON should recognize for the given
// type.
// The algorithm is breadth-first search over the set of structs to include:
// the top struct and then any reachable anonymous structs.
func typeFields(t reflect.Type) []field {
	// Anonymous fields to explore at the current level and the next.
	current := []field{}
	next := []field{{typ: t}}

	// Count of queued names for current level and the next.
	var count, nextCount map[reflect.Type]int

	// Types already visited at an earlier level.
	visited := map[reflect.Type]bool{}

	// Fields found.
	var fields []field

	for len(next) > 0 {
		current, next = next, current[:0]
		count, nextCount = nextCount, map[reflect.Type]int{}

		for _, f := range current {
			if visited[f.typ] {
				continue
			}
			visited[f.typ] = true

			fields, next = scanStructTypeFields(f, count, nextCount, fields, next)
		}
	}

	sort.Sort(byName(fields))

	// Delete all fields that are hidden by the Go rules for embedded fields,
	// except that fields with JSON tags are promoted.

	// The fields are sorted in primary order of name, secondary order
	// of field index length. Loop over names; for each name, delete
	// hidden fields by choosing the one dominant field that survives.
	out := fields[:0]
	for advance, i := 0, 0; i < len(fields); i += advance {
		// One iteration per name.
		// Find the sequence of fields with the name of this first field.
		fi := fields[i]
		name := fi.name
		for advance = 1; i+advance < len(fields); advance++ {
			fj := fields[i+advance]
			if fj.name != name {
				break
			}
		}
		if advance == 1 { // Only one field with this name
			out = append(out, fi)
			continue
		}
		dominant, ok := dominantField(fields[i : i+advance])
		if ok {
			out = append(out, dominant)
		}
	}

	fields = out
	sort.Sort(byIndex(fields))

	return fields
}

// cachedTypeFields is like typeFields but uses a cache to avoid repeated work.
func cachedTypeFields(t reflect.Type) []field {
	fieldCache.RLock()
	f := fieldCache.m[t]
	fieldCache.RUnlock()
	if f != nil {
		return f
	}

	// Compute fields without lock.
	// Might duplicate effort but won't hold other computations back.
	f = typeFields(t)
	if f == nil {
		f = []field{}
	}

	fieldCache.Lock()
	if fieldCache.m == nil {
		fieldCache.m = map[reflect.Type][]field{}
	}
	fieldCache.m[t] = f
	fieldCache.Unlock()

	return f
}

// scanStructTypeFields scans one struct type and accumulates discovered fields.
func scanStructTypeFields(
	parent field,
	count map[reflect.Type]int,
	nextCount map[reflect.Type]int,
	fields []field,
	next []field,
) ([]field, []field) {
	for i := 0; i < parent.typ.NumField(); i++ {
		sf := parent.typ.Field(i)
		if shouldSkipStructField(sf) {
			continue
		}

		name, opts := parseJSONStructTag(sf.Tag.Get("json"))
		index := joinFieldIndex(parent.index, i)
		ft := derefEmbeddedPointerType(sf.Type)

		if isRegularJSONField(name, sf.Anonymous, ft.Kind()) {
			fields = appendScannedField(fields, count, parent.typ, sf, name, opts, index, ft)
			continue
		}

		next = enqueueAnonymousStruct(next, nextCount, ft, index)
	}

	return fields, next
}

// dominantField looks through the fields, all of which are known to
// have the same name, to find the single field that dominates the
// others using Go's embedding rules, modified by the presence of
// JSON tags. If there are multiple top-level fields, the boolean
// will be false: This condition is an error in Go and we skip all
// the fields.
func dominantField(fields []field) (field, bool) {
	// The fields are sorted in increasing index-length order. The winner
	// must therefore be one with the shortest index length. Drop all
	// longer entries, which is easy: just truncate the slice.
	length := len(fields[0].index)
	tagged := -1 // Index of first tagged field.
	for i, f := range fields {
		if len(f.index) > length {
			fields = fields[:i]
			break
		}
		if f.tag {
			if tagged >= 0 {
				// Multiple tagged fields at the same level: conflict.
				// Return no field.
				return field{}, false
			}
			tagged = i
		}
	}

	if tagged >= 0 {
		return fields[tagged], true
	}
	// All remaining fields have the same length. If there's more than one,
	// we have a conflict (two fields named "X" at the same level) and we
	// return no field.
	if len(fields) > 1 {
		return field{}, false
	}
	return fields[0], true
}

// shouldSkipStructField reports whether a struct field must be ignored.
func shouldSkipStructField(sf reflect.StructField) bool {
	if sf.PkgPath != "" {
		return true
	}

	return sf.Tag.Get("json") == "-"
}

// parseJSONStructTag parses and normalizes a json struct tag.
func parseJSONStructTag(tag string) (string, tagOptions) {
	name, opts := parseTag(tag)
	if !isValidTag(name) {
		name = ""
	}

	return name, opts
}

// joinFieldIndex returns child index path for an embedded field.
func joinFieldIndex(parent []int, child int) []int {
	index := make([]int, len(parent)+1)
	copy(index, parent)
	index[len(parent)] = child

	return index
}

// derefEmbeddedPointerType follows pointer for unnamed embedded fields.
func derefEmbeddedPointerType(t reflect.Type) reflect.Type {
	if t.Name() == "" && t.Kind() == reflect.Ptr {
		return t.Elem()
	}
	return t
}

// isRegularJSONField reports whether field should be directly added to results.
func isRegularJSONField(name string, anonymous bool, kind reflect.Kind) bool {
	return name != "" || !anonymous || kind != reflect.Struct
}

// appendScannedField adds a discovered JSON field and duplicate marker if
// needed.
func appendScannedField(
	fields []field,
	count map[reflect.Type]int,
	parentType reflect.Type,
	sf reflect.StructField,
	name string,
	opts tagOptions,
	index []int,
	ft reflect.Type,
) []field {
	tagged := name != ""
	if name == "" {
		name = sf.Name
	}

	fields = append(fields, fillField(field{
		name:      name,
		tag:       tagged,
		index:     index,
		typ:       ft,
		omitEmpty: opts.Contains("omitempty"),
		quoted:    opts.Contains("string"),
	}))

	if count[parentType] > 1 {
		// If there were multiple instances, add a second, so that the
		// annihilation code will see a duplicate.
		fields = append(fields, fields[len(fields)-1])
	}

	return fields
}

// enqueueAnonymousStruct schedules embedded struct type for next BFS level.
func enqueueAnonymousStruct(
	next []field,
	nextCount map[reflect.Type]int,
	ft reflect.Type,
	index []int,
) []field {
	nextCount[ft]++
	if nextCount[ft] == 1 {
		next = append(next, fillField(field{name: ft.Name(), index: index, typ: ft}))
	}

	return next
}
