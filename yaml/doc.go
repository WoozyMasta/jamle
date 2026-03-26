// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

/*
Package yaml provides a JSON-tag-aware YAML codec and conversion helpers.

The package is intended for projects that want one struct model with json
tags for both JSON and YAML inputs. It is used by jamle internally and can
also be used directly as a standalone codec.

Design goals:
  - decode YAML into structs using json tags;
  - keep a small, practical API for marshaling, unmarshaling, and
    YAML<->JSON conversion;
  - provide optional file I/O helpers with automatic format detection.

Core APIs:
  - Unmarshal decodes YAML into a Go value.
  - Marshal encodes a Go value as YAML.
  - YAMLToJSON and JSONToYAML convert between formats.

Optional I/O helpers:
  - ReadFile reads and decodes YAML/JSON from file.
  - WriteFile encodes and writes YAML/JSON to file.
  - UnmarshalAuto and MarshalWith provide format-aware behavior.

Example (decode YAML with json tags):

	type Config struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}

	var cfg Config
	err := yaml.Unmarshal([]byte("host: localhost\nport: 8080\n"), &cfg)
	if err != nil {
		panic(err)
	}

Example (auto-detect and read from file):

	var cfg Config
	err := yaml.ReadFile("config.auto", &cfg, yaml.ReadOptions{
		Format: yaml.FormatAuto,
	})
	if err != nil {
		panic(err)
	}

Example (write JSON with indentation):

	err := yaml.WriteFile("config.json", cfg, yaml.WriteOptions{
		Format: yaml.FormatJSON,
		Indent: 2,
	})
	if err != nil {
		panic(err)
	}

Behavior notes:
  - Unmarshal decodes only the first document from a multi-document YAML
    stream.
  - YAMLToJSON does not preserve !!binary payloads losslessly through JSON
    conversion.
*/
package yaml
