// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package yaml

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/woozymasta/jamle/internal/schema"
	nodeview "github.com/woozymasta/jamle/internal/yamlnode"
	goyaml "go.yaml.in/yaml/v3"
)

var (
	// errStrictYAMLAliasCycle reports a recursive alias graph during validation.
	errStrictYAMLAliasCycle = errors.New("yaml: alias cycle")
)

// DecodeOptions configures YAML node decoding.
type DecodeOptions struct {
	// Strict rejects mapping keys that do not match the destination struct.
	Strict bool
}

// UnmarshalWithOptions decodes one YAML document with the supplied options.
func UnmarshalWithOptions(data []byte, v any, opts DecodeOptions) error {
	root, err := decodeSingleDocument(data)
	if err != nil {
		return err
	}

	return UnmarshalNodeWithOptions(root, v, opts)
}

// UnmarshalNodeWithOptions decodes a YAML node with the supplied options.
func UnmarshalNodeWithOptions(root *goyaml.Node, v any, opts DecodeOptions) error {
	if err := validateDecodeTarget(v); err != nil {
		return err
	}

	if opts.Strict && root != nil {
		if err := validateStrictYAML(root, reflect.TypeOf(v)); err != nil {
			return err
		}
	}

	return unmarshalNode(root, v)
}

// strictYAMLValidator validates node mappings against schema metadata.
type strictYAMLValidator struct {
	// active contains aliases currently being traversed.
	active map[*goyaml.Node]bool
}

// validateStrictYAML validates a node against the destination type.
func validateStrictYAML(root *goyaml.Node, target reflect.Type) error {
	validator := strictYAMLValidator{active: make(map[*goyaml.Node]bool)}
	return validator.validate(root, "$", target)
}

// validate recursively checks the node shape relevant to the destination type.
func (v *strictYAMLValidator) validate(node *goyaml.Node, path string, target reflect.Type) error {
	if node == nil {
		return nil
	}

	target = derefType(target)
	if target == nil || target.Kind() == reflect.Interface || schema.IsOpaque(target) {
		return nil
	}

	if node.Kind == goyaml.AliasNode {
		if node.Alias == nil {
			return nil
		}
		if v.active[node.Alias] {
			return v.positionError(node, path, errStrictYAMLAliasCycle)
		}

		v.active[node.Alias] = true
		defer delete(v.active, node.Alias)
		return v.validate(node.Alias, path, target)
	}

	switch node.Kind {
	case goyaml.DocumentNode:
		if len(node.Content) == 0 {
			return nil
		}
		return v.validate(node.Content[0], path, target)

	case goyaml.MappingNode:
		return v.validateMapping(node, path, target)

	case goyaml.SequenceNode:
		return v.validateSequence(node, path, target)

	default:
		return nil
	}
}

// validateMapping validates struct fields or recursively validates map values.
func (v *strictYAMLValidator) validateMapping(
	node *goyaml.Node,
	path string,
	target reflect.Type,
) error {
	switch target.Kind() {
	case reflect.Struct:
		return v.validateStructMapping(node, path, target)

	case reflect.Map:
		return v.validateMapMapping(node, path, target)

	default:
		return nil
	}
}

// validateStructMapping rejects keys absent from the shared schema metadata.
func (v *strictYAMLValidator) validateStructMapping(
	node *goyaml.Node,
	path string,
	target reflect.Type,
) error {
	lookup := schema.For(target)
	entries, err := nodeview.EffectiveMapping(node)
	if err != nil {
		return v.positionError(node, path, errStrictYAMLAliasCycle)
	}
	for _, entry := range entries {
		field, ok := lookup.Lookup(entry.Key.Value)
		if !ok || field.YAMLDecodeKey == "-" {
			return v.positionError(
				entry.Key,
				joinStrictYAMLPath(path, entry.Key.Value),
				fmt.Errorf("unknown field %q", entry.Key.Value),
			)
		}

		fieldPath := joinStrictYAMLPath(path, entry.Key.Value)
		if err := v.validate(entry.Value, fieldPath, field.Type); err != nil {
			return err
		}
	}

	return nil
}

// validateMapMapping validates each value while leaving map keys unrestricted.
func (v *strictYAMLValidator) validateMapMapping(
	node *goyaml.Node,
	path string,
	target reflect.Type,
) error {
	valueType := target.Elem()
	entries, err := nodeview.EffectiveMapping(node)
	if err != nil {
		return v.positionError(node, path, errStrictYAMLAliasCycle)
	}
	for _, entry := range entries {
		valuePath := joinStrictYAMLPath(path, entry.Key.Value)
		if err := v.validate(entry.Value, valuePath, valueType); err != nil {
			return err
		}
	}

	return nil
}

// validateSequence validates sequence elements against the destination type.
func (v *strictYAMLValidator) validateSequence(
	node *goyaml.Node,
	path string,
	target reflect.Type,
) error {
	if target.Kind() != reflect.Slice && target.Kind() != reflect.Array {
		return nil
	}

	for index, child := range node.Content {
		itemPath := fmt.Sprintf("%s[%d]", path, index)
		if err := v.validate(child, itemPath, target.Elem()); err != nil {
			return err
		}
	}

	return nil
}

// joinStrictYAMLPath appends one YAML key to a diagnostic path.
func joinStrictYAMLPath(path, key string) string {
	if path == "$" {
		return "$." + key
	}

	if strings.HasPrefix(key, "[") {
		return path + key
	}

	return path + "." + key
}

// positionError adds a node's source position to a validation error.
func (v *strictYAMLValidator) positionError(
	node *goyaml.Node,
	path string,
	err error,
) error {
	return fmt.Errorf("%s:%d:%d: %w", path, node.Line, node.Column, err)
}
