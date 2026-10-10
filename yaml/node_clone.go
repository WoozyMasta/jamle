// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package yaml

import goyaml "go.yaml.in/yaml/v3"

// cloneNodeGraph copies a YAML node graph while preserving alias identity.
func cloneNodeGraph(root *goyaml.Node) *goyaml.Node {
	clones := make(map[*goyaml.Node]*goyaml.Node)

	var clone func(*goyaml.Node) *goyaml.Node
	clone = func(node *goyaml.Node) *goyaml.Node {
		if node == nil {
			return nil
		}

		if copied, ok := clones[node]; ok {
			return copied
		}

		copied := *node
		copied.Content = nil
		copied.Alias = nil
		clones[node] = &copied

		if len(node.Content) > 0 {
			copied.Content = make([]*goyaml.Node, len(node.Content))
			for index, child := range node.Content {
				copied.Content[index] = clone(child)
			}
		}

		copied.Alias = clone(node.Alias)

		return &copied
	}

	return clone(root)
}
