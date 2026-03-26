// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package yaml

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	goyaml "go.yaml.in/yaml/v3"
)

// Format is an input/output data format selector.
type Format string

const (
	// FormatAuto selects format by extension first, then by content.
	FormatAuto Format = "auto"
	// FormatYAML forces YAML format.
	FormatYAML Format = "yaml"
	// FormatJSON forces JSON format.
	FormatJSON Format = "json"
)

var (
	// ErrInvalidFormat reports unsupported input/output format option.
	ErrInvalidFormat = errors.New("yaml: invalid format")
	// ErrStrictYAMLUnsupported reports strict mode limitation for YAML input.
	ErrStrictYAMLUnsupported = errors.New(
		"yaml: strict mode is supported only for JSON input",
	)
	// ErrJSONTrailingData reports extra non-whitespace bytes after JSON value.
	ErrJSONTrailingData = errors.New("json: trailing data after top-level value")
)

// ReadOptions configures ReadFile behavior.
type ReadOptions struct {
	Format Format
	Strict bool
}

// AutoOptions configures UnmarshalAuto behavior.
type AutoOptions struct {
	Format Format
	Strict bool
}

// WriteOptions configures MarshalWith/WriteFile behavior.
type WriteOptions struct {
	Format Format
	Indent int
	Perm   os.FileMode
}

// ReadFile reads a file and decodes its content as YAML or JSON.
func ReadFile(path string, v any, opts ReadOptions) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	format, err := resolveFormat(path, data, opts.Format, true)
	if err != nil {
		return err
	}

	return unmarshalByFormat(data, v, format, opts.Strict)
}

// WriteFile marshals an object and writes it to a file.
func WriteFile(path string, v any, opts WriteOptions) error {
	data, err := MarshalWith(v, opts)
	if err != nil {
		return err
	}

	perm := opts.Perm
	if perm == 0 {
		perm = 0o644
	}

	return writeFileAtomic(path, data, perm)
}

// UnmarshalAuto decodes JSON or YAML based on options/path/content heuristics.
func UnmarshalAuto(data []byte, v any, opts AutoOptions) error {
	format, err := resolveFormat("", data, opts.Format, false)
	if err != nil {
		return err
	}

	return unmarshalByFormat(data, v, format, opts.Strict)
}

// MarshalWith marshals an object using selected format and formatting options.
func MarshalWith(v any, opts WriteOptions) ([]byte, error) {
	format := opts.Format
	if format == "" || format == FormatAuto {
		format = FormatYAML
	}

	switch format {
	case FormatYAML:
		return marshalYAMLWithIndent(v, opts.Indent)
	case FormatJSON:
		return marshalJSONWithIndent(v, opts.Indent)
	default:
		return nil, fmt.Errorf("%w: %q", ErrInvalidFormat, format)
	}
}

// resolveFormat resolves final format from explicit option or auto rules.
func resolveFormat(path string, data []byte, format Format, usePath bool) (Format, error) {
	if format == "" {
		format = FormatAuto
	}

	switch format {
	case FormatYAML, FormatJSON:
		return format, nil
	case FormatAuto:
		if usePath {
			if byPath, ok := detectFormatByPath(path); ok {
				return byPath, nil
			}
		}
		return detectFormatByContent(data), nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidFormat, format)
	}
}

// detectFormatByPath detects input format from file extension.
func detectFormatByPath(path string) (Format, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".yaml", ".yml":
		return FormatYAML, true
	case ".json":
		return FormatJSON, true
	default:
		return "", false
	}
}

// detectFormatByContent detects input format from first significant byte.
func detectFormatByContent(data []byte) Format {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return FormatYAML
	}

	// JSON and YAML flow-style can both start with '{'/'['.
	// Keep auto mode ambiguous and let decoder probing decide.
	switch trimmed[0] {
	case '{', '[':
		return FormatAuto
	default:
		return FormatYAML
	}
}

// unmarshalByFormat decodes data according to selected format.
func unmarshalByFormat(data []byte, v any, format Format, strict bool) error {
	switch format {
	case FormatYAML:
		if strict {
			return ErrStrictYAMLUnsupported
		}

		return Unmarshal(data, v)
	case FormatJSON:
		return unmarshalJSON(data, v, strict)
	case FormatAuto:
		if err := unmarshalJSON(data, v, strict); err == nil {
			return nil
		} else if errors.Is(err, ErrJSONTrailingData) {
			return err
		} else if strict {
			// In strict mode, YAML fallback is disabled. Keep JSON errors for
			// JSON-looking input and return explicit unsupported error for YAML.
			if detectFormatByContent(data) == FormatAuto {
				return err
			}

			return ErrStrictYAMLUnsupported
		}

		return Unmarshal(data, v)
	default:
		return fmt.Errorf("%w: %q", ErrInvalidFormat, format)
	}
}

// unmarshalJSON decodes JSON input with optional strict unknown-field mode.
func unmarshalJSON(data []byte, v any, strict bool) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	if strict {
		dec.DisallowUnknownFields()
	}

	if err := dec.Decode(v); err != nil {
		return err
	}

	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ErrJSONTrailingData
	}

	return nil
}

// marshalJSONWithIndent marshals object to JSON with optional indentation.
func marshalJSONWithIndent(v any, indent int) ([]byte, error) {
	if indent <= 0 {
		return json.Marshal(v)
	}

	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	indentStr := strings.Repeat(" ", indent)
	if err := json.Indent(&buf, raw, "", indentStr); err != nil {
		return nil, err
	}

	buf.WriteByte('\n')
	return buf.Bytes(), nil
}

// marshalYAMLWithIndent marshals object to YAML with optional indentation.
func marshalYAMLWithIndent(v any, indent int) ([]byte, error) {
	if indent <= 0 {
		return Marshal(v)
	}

	rawJSON, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}

	// Keep number typing behavior consistent with JSONToYAML.
	var normalized any
	if err := goyaml.Unmarshal(rawJSON, &normalized); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	enc := goyaml.NewEncoder(&buf)
	enc.SetIndent(indent)
	if err := enc.Encode(normalized); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// writeFileAtomic writes data to a temporary file and renames it into place.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	tmp, err := os.CreateTemp(dir, "."+base+".tmp-*")
	if err != nil {
		return err
	}

	tmpName := tmp.Name()
	cleanup := func() {
		_ = os.Remove(tmpName)
	}

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}

	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}

	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}

	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}

	if err := os.Rename(tmpName, path); err != nil {
		if replaceErr := replaceExistingFile(tmpName, path, err); replaceErr != nil {
			cleanup()
			return replaceErr
		}
	}

	return nil
}

// replaceExistingFile replaces an existing destination with rollback safety.
func replaceExistingFile(tmpName, path string, previousErr error) error {
	if _, statErr := os.Stat(path); statErr != nil {
		return previousErr
	}

	backupPath := tmpName + ".bak"
	if err := os.Rename(path, backupPath); err != nil {
		return previousErr
	}

	if err := os.Rename(tmpName, path); err != nil {
		if rollbackErr := os.Rename(backupPath, path); rollbackErr != nil {
			return fmt.Errorf(
				"failed to replace %q: %w (rollback failed: %v)",
				path,
				err,
				rollbackErr,
			)
		}

		return err
	}

	_ = os.Remove(backupPath)
	return nil
}
