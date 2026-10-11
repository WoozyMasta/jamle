// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package jamle

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"

	jyaml "github.com/woozymasta/jamle/yaml"
	goyaml "go.yaml.in/yaml/v3"
)

/*
Unmarshal parses YAML-encoded data and stores the result in the value pointed to by v.

Before decoding, it recursively expands supported environment expressions in scalar values.
Expansion is evaluated lazily, so nested := and :? words
that are not selected by their operator are not evaluated.
Resolver values may contain nested expressions;
cycles and [UnmarshalOptions.MaxDepth] exhaustion return errors instead of partial output.
*/
func Unmarshal(data []byte, v any) error {
	return UnmarshalWithOptions(data, v, UnmarshalOptions{})
}

// UnmarshalWithOptions parses YAML and expands ${...} using [UnmarshalOptions].
func UnmarshalWithOptions(data []byte, v any, opts UnmarshalOptions) error {
	containsVars := bytes.Contains(data, []byte("${"))
	// Direct decoding is safe only when no expansion and no strict validation is requested.
	if !containsVars && !opts.Strict && (opts.DisableDefaults || !hasDefaults(reflect.TypeOf(v))) {
		return jyaml.Unmarshal(data, v)
	}

	resolvedOpts := resolveOptions(opts)
	root, err := parseDocument(data)
	if err != nil {
		return err
	}

	return processDocument(root, v, reflect.TypeOf(v), resolvedOpts, containsVars, opts.Strict)
}

// UnmarshalAll parses all YAML documents from the input stream and appends
// decoded values into out, which must be a pointer to a slice.
// Use [Unmarshal] when the input must contain one document.
func UnmarshalAll(data []byte, out any) error {
	return UnmarshalAllWithOptions(data, out, UnmarshalOptions{})
}

// UnmarshalAllWithOptions parses all YAML documents using [UnmarshalOptions].
func UnmarshalAllWithOptions(data []byte, out any, opts UnmarshalOptions) error {
	outValue := reflect.ValueOf(out)
	if !outValue.IsValid() {
		return fmt.Errorf("%w, got %T", ErrOutMustBePointerToSlice, out)
	}
	if outValue.Kind() != reflect.Pointer || outValue.Elem().Kind() != reflect.Slice {
		return fmt.Errorf("%w, got %T", ErrOutMustBePointerToSlice, out)
	}

	sliceValue := outValue.Elem()
	elemType := sliceValue.Type().Elem()
	resolvedOpts := resolveOptions(opts)
	containsVars := bytes.Contains(data, []byte("${"))

	dec := goyaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(false)

	for {
		var root goyaml.Node
		err := dec.Decode(&root)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}

		elem, err := decodeDocument(&root, elemType, resolvedOpts, containsVars, opts.Strict)
		if err != nil {
			return err
		}

		sliceValue = reflect.Append(sliceValue, elem)
	}

	outValue.Elem().Set(sliceValue)
	return nil
}

// decodeDocument decodes one YAML document into a slice element value.
func decodeDocument(
	root *goyaml.Node,
	elemType reflect.Type,
	resolvedOpts runtimeOptions,
	containsVars bool,
	strict bool,
) (reflect.Value, error) {
	if elemType.Kind() == reflect.Pointer && isNullDocument(root) {
		return reflect.Zero(elemType), nil
	}

	if elemType.Kind() == reflect.Pointer {
		target := reflect.New(elemType.Elem())
		if err := processDocument(
			root,
			target.Interface(),
			elemType,
			resolvedOpts,
			containsVars,
			strict,
		); err != nil {
			return reflect.Value{}, err
		}

		return target, nil
	}

	target := reflect.New(elemType)
	if err := processDocument(
		root,
		target.Interface(),
		elemType,
		resolvedOpts,
		containsVars,
		strict,
	); err != nil {
		return reflect.Value{}, err
	}

	return target.Elem(), nil
}

// isNullDocument reports whether a document has no value or an explicit null value.
func isNullDocument(root *goyaml.Node) bool {
	for root != nil && root.Kind == goyaml.DocumentNode {
		if len(root.Content) == 0 {
			return true
		}
		root = root.Content[0]
	}

	return root != nil && root.Kind == goyaml.ScalarNode && root.Tag == "!!null"
}

// parseDocument parses the first YAML document from data into a node graph.
func parseDocument(data []byte) (*goyaml.Node, error) {
	var root goyaml.Node
	dec := goyaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(false)
	if err := dec.Decode(&root); errors.Is(err, io.EOF) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	var extra goyaml.Node
	if err := dec.Decode(&extra); err == nil {
		return nil, ErrMultipleDocuments
	} else if !errors.Is(err, io.EOF) {
		return nil, err
	}

	return &root, nil
}

// processDocument applies expansion and then decodes one parsed YAML document.
func processDocument(
	root *goyaml.Node,
	target any,
	targetType reflect.Type,
	resolvedOpts runtimeOptions,
	containsVars bool,
	strict bool,
) error {
	if containsVars && root != nil {
		if err := expandEnvInNode(root, targetType, resolvedOpts); err != nil {
			return err
		}
	}

	if err := jyaml.UnmarshalNodeWithOptions(
		root,
		target,
		jyaml.DecodeOptions{Strict: strict},
	); err != nil {
		return err
	}

	if !resolvedOpts.disableDefaults && hasDefaults(targetType) {
		return applyDefaults(root, target)
	}

	return nil
}

// resolveOptions normalizes options and applies defaults.
func resolveOptions(opts UnmarshalOptions) runtimeOptions {
	resolver := opts.Resolver
	if resolver == nil {
		resolver = envResolver{}
	}

	maxDepth := opts.MaxDepth
	if maxDepth <= 0 {
		maxDepth = defaultMaxDepth
	}

	ignorePaths := append([]string{}, opts.IgnoreExpandPaths...)

	runtime := runtimeOptions{
		resolver:        resolver,
		maxDepth:        maxDepth,
		allowAssignment: opts.Assignment != AssignmentIgnore,
		enforceRequired: opts.Required != RequiredIgnore,
		disableDefaults: opts.DisableDefaults,
		expandKeys:      opts.ExpandKeys,
	}
	if len(ignorePaths) == 0 {
		return runtime
	}

	runtime.ignorePathRules = compilePathRules(ignorePaths)
	return runtime
}
