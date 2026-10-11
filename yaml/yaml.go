// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package yaml

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"

	"github.com/woozymasta/jamle/internal/schema"
	"go.yaml.in/yaml/v3"
)

// Marshal encodes an object as YAML using JSON-compatible field and value semantics.
func Marshal(o any) ([]byte, error) {
	normalized, err := normalizeYAMLMarshalInput(o)
	if err != nil {
		return nil, fmt.Errorf("error normalizing object for YAML: %w", err)
	}

	y, err := yaml.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("error marshaling normalized object to YAML: %w", err)
	}

	return y, nil
}

// Unmarshal decodes YAML into an object while honoring JSON struct tags.
// It delegates node decoding to [UnmarshalNode].
func Unmarshal(y []byte, o any) error {
	return UnmarshalWithOptions(y, o, DecodeOptions{})
}

// JSONToYAML converts JSON to YAML using go-yaml's native numeric types.
func JSONToYAML(j []byte) ([]byte, error) {
	var raw json.RawMessage
	if err := json.Unmarshal(j, &raw); err != nil {
		return nil, err
	}

	// Convert the JSON to an object.
	var jsonObj any
	// We are using yaml.Unmarshal here (instead of json.Unmarshal)
	// because the Go JSON library doesn't try to pick the right number type (int, float, etc.)
	// when unmarshalling to interface{}, it just picks float64 universally.
	// go-yaml does go through the effort of picking the right number type,
	// so we can preserve number type throughout this process.
	err := yaml.Unmarshal(j, &jsonObj)
	if err != nil {
		return nil, err
	}

	// Marshal this object into YAML.
	return yaml.Marshal(jsonObj)
}

// YAMLToJSON converts YAML to JSON. Since JSON is a subset of YAML,
// passing JSON through this method should be a no-op.
//
// Things YAML can do that are not supported by JSON:
//   - Binary map keys are normalized to canonical base64 strings;
//     null and complex keys remain unsupported by JSON.
//     Int and float keys are converted to strings.
//   - Distinct YAML map keys that stringify to the same JSON key are rejected.
//   - YAML !!binary values are normalized to canonical base64 strings.
func YAMLToJSON(y []byte) ([]byte, error) { //nolint:revive
	root, err := decodeSingleDocument(y)
	if err != nil {
		return nil, err
	}

	return yamlToJSON(root, nil)
}

// yamlToJSON converts one decoded YAML document into JSON bytes.
func yamlToJSON(root *yaml.Node, jsonTarget *reflect.Value) ([]byte, error) {
	var yamlObj any
	if root != nil {
		// Normalize tagged binary scalars
		// before go-yaml converts them to a binary Go string.
		if err := normalizeBinaryScalars(root); err != nil {
			return nil, err
		}

		if err := root.Decode(&yamlObj); err != nil {
			return nil, err
		}
	}

	// YAML objects are not completely compatible with JSON objects
	// (e.g. you can have non-string keys in YAML).
	// So, convert the YAML-compatible object to a JSON-compatible object,
	// failing with an error if irrecoverable incompatibilities happen along the way.
	jsonObj, err := convertToJSONableObject(yamlObj, jsonTarget)
	if err != nil {
		return nil, err
	}

	// Convert this object to JSON and return the data.
	return json.Marshal(jsonObj)
}

// convertToJSONableObject recursively makes YAML values JSON-compatible.
func convertToJSONableObject(yamlObj any, jsonTarget *reflect.Value) (any, error) { //nolint:gocyclo
	resolvedTarget := resolveJSONTarget(jsonTarget)

	normalized, err := normalizeYAMLMapKeys(yamlObj)
	if err != nil {
		return nil, err
	}

	switch typedYAMLObj := normalized.(type) {
	case map[string]any:
		return convertMapToJSONable(typedYAMLObj, resolvedTarget)

	case []any:
		return convertSliceToJSONable(typedYAMLObj, resolvedTarget)

	default:
		return coerceScalarForStringTarget(typedYAMLObj, resolvedTarget), nil
	}
}

// resolveJSONTarget resolves pointers/interfaces
// and disables target-guided conversion for JSON/Text unmarshaler types.
func resolveJSONTarget(jsonTarget *reflect.Value) *reflect.Value {
	if jsonTarget == nil {
		return nil
	}

	ju, tu, pv := indirect(*jsonTarget, false)
	if ju != nil || tu != nil {
		return nil
	}

	return &pv
}

// normalizeYAMLMapKeys converts map[any]any into map[string]any when needed.
func normalizeYAMLMapKeys(yamlObj any) (any, error) {
	typed, ok := yamlObj.(map[any]any)
	if !ok {
		return yamlObj, nil
	}

	strMap := make(map[string]any, len(typed))
	originalKeys := make(map[string]any, len(typed))
	for k, v := range typed {
		key, err := stringifyYAMLMapKey(k, v)
		if err != nil {
			return nil, err
		}

		if previous, exists := originalKeys[key]; exists {
			return nil, fmt.Errorf(
				"yaml: map key collision after JSON string conversion for %q (%T and %T)",
				key,
				previous,
				k,
			)
		}

		originalKeys[key] = k
		strMap[key] = v
	}

	return strMap, nil
}

// stringifyYAMLMapKey converts supported YAML map key types to JSON keys.
func stringifyYAMLMapKey(k, v any) (string, error) {
	switch typedKey := k.(type) {
	case string:
		return typedKey, nil
	case int:
		return strconv.Itoa(typedKey), nil
	case int64:
		return strconv.FormatInt(typedKey, 10), nil
	case uint:
		return strconv.FormatUint(uint64(typedKey), 10), nil
	case uint8:
		return strconv.FormatUint(uint64(typedKey), 10), nil
	case uint16:
		return strconv.FormatUint(uint64(typedKey), 10), nil
	case uint32:
		return strconv.FormatUint(uint64(typedKey), 10), nil
	case uint64:
		return strconv.FormatUint(typedKey, 10), nil
	case uintptr:
		return strconv.FormatUint(uint64(typedKey), 10), nil
	case float64:
		return strconv.FormatFloat(typedKey, 'g', -1, 64), nil
	case bool:
		if typedKey {
			return "true", nil
		}
		return "false", nil

	default:
		return "", fmt.Errorf("unsupported map key of type: %s, key: %+#v, value: %+#v",
			reflect.TypeOf(k), k, v)
	}
}

// convertMapToJSONable recursively converts map values with key-aware targets.
func convertMapToJSONable(in map[string]any, jsonTarget *reflect.Value) (map[string]any, error) {
	for k, v := range in {
		target := childTargetForMapKey(jsonTarget, k)
		converted, err := convertToJSONableObject(v, target)
		if err != nil {
			return nil, err
		}

		in[k] = converted
	}

	return in, nil
}

// childTargetForMapKey resolves the per-key target value for struct/map targets.
func childTargetForMapKey(jsonTarget *reflect.Value, key string) *reflect.Value {
	if jsonTarget == nil {
		return nil
	}

	t := *jsonTarget
	switch t.Kind() {
	case reflect.Struct:
		return structFieldTargetByJSONKey(t, key)

	case reflect.Map:
		elem := reflect.Zero(t.Type().Elem())
		return &elem

	default:
		return nil
	}
}

// structFieldTargetByJSONKey finds the struct field target selected by JSON rules.
func structFieldTargetByJSONKey(structVal reflect.Value, key string) *reflect.Value {
	_ = structVal

	matched, ok := schema.For(structVal.Type()).Lookup(key)
	if !ok {
		return nil
	}

	// Use cached field type directly.
	// This avoids incorrect target selection for deep embedded index paths
	// and nil embedded pointer traversal pitfalls.
	fieldVal := reflect.Zero(matched.Type)
	return &fieldVal
}

// convertSliceToJSONable recursively converts slice elements.
func convertSliceToJSONable(in []any, jsonTarget *reflect.Value) ([]any, error) {
	elemTarget := sliceElemTarget(jsonTarget)
	out := make([]any, len(in))
	for i, v := range in {
		converted, err := convertToJSONableObject(v, elemTarget)
		if err != nil {
			return nil, err
		}

		out[i] = converted
	}

	return out, nil
}

// sliceElemTarget resolves the element target for a JSON slice target.
func sliceElemTarget(jsonTarget *reflect.Value) *reflect.Value {
	if jsonTarget == nil {
		return nil
	}

	t := *jsonTarget
	if t.Kind() != reflect.Slice {
		return nil
	}

	elem := reflect.Indirect(reflect.New(t.Type().Elem()))
	return &elem
}

// coerceScalarForStringTarget stringifies numeric/bool scalars for string target.
func coerceScalarForStringTarget(yamlObj any, jsonTarget *reflect.Value) any {
	if jsonTarget == nil || jsonTarget.Kind() != reflect.String {
		return yamlObj
	}

	var s string
	switch typedVal := yamlObj.(type) {
	case int:
		s = strconv.FormatInt(int64(typedVal), 10)
	case int64:
		s = strconv.FormatInt(typedVal, 10)
	case float64:
		s = strconv.FormatFloat(typedVal, 'g', -1, 64)
	case uint64:
		s = strconv.FormatUint(typedVal, 10)
	case bool:
		if typedVal {
			s = "true"
		} else {
			s = "false"
		}
	}

	if s == "" {
		return yamlObj
	}

	return s
}
