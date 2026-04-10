// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package jamle

import (
	"reflect"
	"sort"
	"strings"
	"sync"
)

// noExpandPathCache stores compiled noexpand paths by root output type.
var noExpandPathCache sync.Map

// collectNoExpandPaths builds ignore paths from `jamle:"noexpand"` tags.
func collectNoExpandPaths(outType reflect.Type) []string {
	root := indirectType(outType)
	if root == nil {
		return nil
	}

	paths := make([]string, 0, 8)
	visited := make(map[reflect.Type]bool)
	collectNoExpandPathsFromType(root, nil, visited, &paths)
	if len(paths) == 0 {
		return nil
	}

	sort.Strings(paths)
	return compactSortedStrings(paths)
}

// collectNoExpandPathsCached returns cached noexpand paths for output type.
func collectNoExpandPathsCached(outType reflect.Type) []string {
	root := indirectType(outType)
	if root == nil {
		return nil
	}

	cached, ok := noExpandPathCache.Load(root)
	if ok {
		return cached.([]string)
	}

	paths := collectNoExpandPaths(root)
	actual, _ := noExpandPathCache.LoadOrStore(root, paths)

	return actual.([]string)
}

// collectNoExpandPathsFromType walks reflect types and appends noexpand paths.
func collectNoExpandPathsFromType(
	t reflect.Type,
	path []string,
	visited map[reflect.Type]bool,
	out *[]string,
) {
	t = indirectType(t)
	if t == nil {
		return
	}

	switch t.Kind() {
	case reflect.Struct:
		if visited[t] {
			return
		}
		visited[t] = true
		defer delete(visited, t)

		for i := 0; i < t.NumField(); i++ {
			sf := t.Field(i)
			if shouldSkipStructField(sf) {
				continue
			}

			fieldPath, inline := pathForStructField(path, sf)
			if inline {
				collectNoExpandPathsFromType(sf.Type, path, visited, out)
				continue
			}
			if len(fieldPath) == 0 {
				continue
			}

			if hasNoExpandTag(sf.Tag.Get("jamle")) {
				*out = append(*out, strings.Join(fieldPath, "."))
			}

			collectNoExpandPathsFromType(sf.Type, fieldPath, visited, out)
		}

	case reflect.Slice, reflect.Array, reflect.Map:
		nextPath := appendPathSegment(path, "*")
		collectNoExpandPathsFromType(t.Elem(), nextPath, visited, out)
	}
}

// shouldSkipStructField reports whether field should be ignored for path build.
func shouldSkipStructField(sf reflect.StructField) bool {
	if sf.PkgPath != "" {
		return true
	}

	if jsonTagName(sf) == "-" {
		return true
	}

	return yamlTagName(sf) == "-"
}

// pathForStructField builds one path segment for a struct field.
func pathForStructField(path []string, sf reflect.StructField) ([]string, bool) {
	jsonName := jsonTagName(sf)
	if jsonName == "-" {
		return nil, false
	}

	fieldType := indirectType(sf.Type)
	if sf.Anonymous && jsonName == "" && fieldType != nil && fieldType.Kind() == reflect.Struct {
		return path, true
	}

	if jsonName != "" {
		return appendPathSegment(path, jsonName), false
	}

	yamlName := yamlTagName(sf)
	if yamlName == "-" {
		return nil, false
	}

	if yamlName != "" && yamlName != "-" {
		return appendPathSegment(path, yamlName), false
	}

	return appendPathSegment(path, strings.ToLower(sf.Name)), false
}

// hasNoExpandTag reports whether jamle tag has `noexpand` option.
func hasNoExpandTag(tag string) bool {
	for item := range strings.SplitSeq(tag, ",") {
		if strings.TrimSpace(item) == "noexpand" {
			return true
		}
	}

	return false
}

// jsonTagName returns first value token from `json` tag.
func jsonTagName(sf reflect.StructField) string {
	return firstTagValue(sf.Tag.Get("json"))
}

// yamlTagName returns first value token from `yaml` tag.
func yamlTagName(sf reflect.StructField) string {
	return firstTagValue(sf.Tag.Get("yaml"))
}

// firstTagValue returns first token before comma in a struct tag.
func firstTagValue(tag string) string {
	name, _, _ := strings.Cut(tag, ",")
	return strings.TrimSpace(name)
}

// indirectType dereferences pointers until a concrete type is reached.
func indirectType(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	return t
}

// compactSortedStrings compacts sorted slice values in-place.
func compactSortedStrings(in []string) []string {
	if len(in) < 2 {
		return in
	}

	out := in[:1]
	last := in[0]

	for i := 1; i < len(in); i++ {
		if in[i] == last {
			continue
		}

		last = in[i]
		out = append(out, in[i])
	}

	return out
}
