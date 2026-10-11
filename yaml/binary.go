// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package yaml

import (
	"encoding/base64"
	"fmt"
	"strings"
	"unicode"

	goyaml "go.yaml.in/yaml/v3"
)

// normalizeBinaryScalars converts !!binary scalars to canonical base64 strings
// while preserving their JSON-safe representation for later node decoding.
func normalizeBinaryScalars(root *goyaml.Node) error {
	visited := make(map[*goyaml.Node]bool)

	var visit func(*goyaml.Node) error
	visit = func(node *goyaml.Node) error {
		if node == nil || visited[node] {
			return nil
		}
		visited[node] = true

		if node.Kind == goyaml.ScalarNode && node.Tag == "!!binary" {
			encoded := strings.Map(func(r rune) rune {
				if unicode.IsSpace(r) {
					return -1
				}

				return r
			}, node.Value)

			decoded, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				return fmt.Errorf(
					"yaml: invalid !!binary value at line %d, column %d: %w",
					node.Line,
					node.Column,
					err,
				)
			}

			node.Tag = "!!str"
			node.Value = base64.StdEncoding.EncodeToString(decoded)
		}

		if err := visit(node.Alias); err != nil {
			return err
		}

		for _, child := range node.Content {
			if err := visit(child); err != nil {
				return err
			}
		}

		return nil
	}

	return visit(root)
}
