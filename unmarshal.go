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
Unmarshal parses the YAML-encoded data and stores the result in the value pointed to by v.

Before parsing, it recursively expands environment variables within the data.
The function performs up to 10 passes to resolve nested variables (e.g., ${A:=${B}})
and prevents infinite loops.
*/
func Unmarshal(data []byte, v any) error {
	return UnmarshalWithOptions(data, v, UnmarshalOptions{})
}

// UnmarshalWithOptions parses YAML and expands ${...} using configured options.
func UnmarshalWithOptions(data []byte, v any, opts UnmarshalOptions) error {
	// Fast path: if there are no variable markers, decode directly.
	if !bytes.Contains(data, []byte("${")) {
		return jyaml.Unmarshal(data, v)
	}

	resolvedOpts := resolveOptions(opts, reflect.TypeOf(v))

	// Parse into YAML AST (comments are stored in node fields, not in scalar values)
	var root goyaml.Node
	dec := goyaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(false)
	if err := dec.Decode(&root); err != nil {
		return err
	}

	if err := expandEnvInNode(&root, resolvedOpts); err != nil {
		return err
	}

	// Decode from transformed AST directly to avoid YAML re-encode/re-decode.
	return jyaml.UnmarshalNode(&root, v)
}

// UnmarshalAll parses all YAML documents from the input stream and appends
// decoded values into out, which must be a pointer to a slice.
func UnmarshalAll(data []byte, out any) error {
	return UnmarshalAllWithOptions(data, out, UnmarshalOptions{})
}

// UnmarshalAllWithOptions parses all YAML documents using configured options.
func UnmarshalAllWithOptions(data []byte, out any, opts UnmarshalOptions) error {
	outValue := reflect.ValueOf(out)
	if !outValue.IsValid() {
		return fmt.Errorf("%w, got %T", ErrOutMustBePointerToSlice, out)
	}
	if outValue.Kind() != reflect.Ptr || outValue.Elem().Kind() != reflect.Slice {
		return fmt.Errorf("%w, got %T", ErrOutMustBePointerToSlice, out)
	}

	sliceValue := outValue.Elem()
	elemType := sliceValue.Type().Elem()
	resolvedOpts := resolveOptions(opts, elemType)
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

		if containsVars {
			if err := expandEnvInNode(&root, resolvedOpts); err != nil {
				return err
			}
		}

		elem, err := decodeDocument(&root, elemType)
		if err != nil {
			return err
		}

		sliceValue = reflect.Append(sliceValue, elem)
	}

	outValue.Elem().Set(sliceValue)
	return nil
}

// decodeDocument decodes one YAML document into a slice element value.
func decodeDocument(root *goyaml.Node, elemType reflect.Type) (reflect.Value, error) {
	if elemType.Kind() == reflect.Ptr {
		target := reflect.New(elemType.Elem())
		if err := jyaml.UnmarshalNode(root, target.Interface()); err != nil {
			return reflect.Value{}, err
		}

		return target, nil
	}

	target := reflect.New(elemType)
	if err := jyaml.UnmarshalNode(root, target.Interface()); err != nil {
		return reflect.Value{}, err
	}

	return target.Elem(), nil
}

// resolveOptions normalizes options and applies defaults.
func resolveOptions(opts UnmarshalOptions, outType reflect.Type) runtimeOptions {
	resolver := opts.Resolver
	if resolver == nil {
		resolver = envResolver{}
	}

	maxPasses := opts.MaxPasses
	if maxPasses <= 0 {
		maxPasses = defaultMaxPasses
	}

	ignorePaths := append([]string{}, opts.IgnoreExpandPaths...)
	if outType != nil {
		ignorePaths = append(ignorePaths, collectNoExpandPathsCached(outType)...)
	}

	runtime := runtimeOptions{
		resolver:        resolver,
		maxPasses:       maxPasses,
		allowAssignment: !opts.DisableAssignment,
		enforceRequired: !opts.DisableRequiredErrors,
	}
	if len(ignorePaths) == 0 {
		return runtime
	}

	runtime.ignorePathRules = compilePathRules(ignorePaths)
	return runtime
}
