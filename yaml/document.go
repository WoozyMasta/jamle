// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package yaml

import (
	"bytes"
	"errors"
	"io"

	goyaml "go.yaml.in/yaml/v3"
)

// decodeSingleDocument parses one YAML document and rejects additional documents.
func decodeSingleDocument(data []byte) (*goyaml.Node, error) {
	var root goyaml.Node
	dec := goyaml.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&root); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
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
