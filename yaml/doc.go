// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

/*
Package yaml provides a JSON-tag-aware YAML codec and conversion helpers.

The package is useful when one struct model
with json tags must work for both YAML and JSON input.
[Unmarshal] decodes YAML into a Go value and [Marshal] encodes a Go value as YAML.
[JSONToYAML] and [YAMLToJSON] convert between the two formats.

[ReadFile], [WriteFile], [UnmarshalAuto],
and [MarshalWith] add format-aware file I/O and output options.
[FormatAuto] detects JSON/YAML from the path or content when the format is not forced.

[Unmarshal] decodes exactly one document
and returns [ErrMultipleDocuments] when the input contains more than one.
Use `jamle.UnmarshalAll` in the parent package when multiple documents must be decoded.
[YAMLToJSON] also rejects multi-document YAML input with [ErrMultipleDocuments].
YAML `!!binary` values are normalized to canonical base64 strings by [YAMLToJSON].
*/
package yaml
