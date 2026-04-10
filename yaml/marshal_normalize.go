// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package yaml

import (
	"encoding/json"

	goyaml "go.yaml.in/yaml/v3"
)

// normalizeYAMLMarshalInput normalizes values through JSON semantics before YAML emit.
func normalizeYAMLMarshalInput(v any) (any, error) {
	rawJSON, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}

	// Keep number typing behavior consistent with JSONToYAML.
	var normalized any
	if err := goyaml.Unmarshal(rawJSON, &normalized); err != nil {
		return nil, err
	}

	return normalized, nil
}
