// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package yaml

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/woozymasta/jamle/internal/schema"
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
	var root goyaml.Node
	dec := goyaml.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&root); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}

	return UnmarshalNodeWithOptions(&root, v, opts)
}

// UnmarshalNodeWithOptions decodes a YAML node with the supplied options.
func UnmarshalNodeWithOptions(root *goyaml.Node, v any, opts DecodeOptions) error {
	if opts.Strict && root != nil && v != nil {
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
	for i := 0; i+1 < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]
		if isStrictYAMLMergeKey(keyNode) {
			if err := v.validateMerge(valueNode, path, target); err != nil {
				return err
			}

			continue
		}

		field, ok := lookup.Lookup(keyNode.Value)
		if !ok || field.YAMLDecodeKey == "-" {
			return v.positionError(
				keyNode,
				joinStrictYAMLPath(path, keyNode.Value),
				fmt.Errorf("unknown field %q", keyNode.Value),
			)
		}

		fieldPath := joinStrictYAMLPath(path, keyNode.Value)
		if err := v.validate(valueNode, fieldPath, field.Type); err != nil {
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
	for i := 0; i+1 < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valuePath := joinStrictYAMLPath(path, keyNode.Value)
		if err := v.validate(node.Content[i+1], valuePath, valueType); err != nil {
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

// validateMerge validates one or more merge sources against the consumer type.
func (v *strictYAMLValidator) validateMerge(
	node *goyaml.Node,
	path string,
	target reflect.Type,
) error {
	if node == nil {
		return nil
	}

	if node.Kind == goyaml.SequenceNode {
		for _, source := range node.Content {
			if err := v.validate(source, path, target); err != nil {
				return err
			}
		}
		return nil
	}

	return v.validate(node, path, target)
}

// isStrictYAMLMergeKey recognizes the YAML merge key without reflection logic.
func isStrictYAMLMergeKey(node *goyaml.Node) bool {
	if node == nil || node.Value != "<<" {
		return false
	}

	switch node.Tag {
	case "", "!", "!!merge", "tag:yaml.org,2002:merge":
		return true
	default:
		return false
	}
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
