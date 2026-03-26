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
	"reflect"
	"strconv"

	"go.yaml.in/yaml/v3"
)

// Marshal the object into JSON then converts JSON to YAML and returns the
// YAML.
func Marshal(o any) ([]byte, error) {
	j, err := json.Marshal(o)
	if err != nil {
		return nil, fmt.Errorf("error marshaling into JSON: %w", err)
	}

	y, err := JSONToYAML(j)
	if err != nil {
		return nil, fmt.Errorf("error converting JSON to YAML: %w", err)
	}

	return y, nil
}

// Unmarshal decodes YAML into an object honoring JSON struct tags.
func Unmarshal(y []byte, o any) error {
	var root yaml.Node
	dec := yaml.NewDecoder(bytes.NewReader(y))
	if err := dec.Decode(&root); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}

	return UnmarshalNode(&root, o)
}

// JSONToYAML converts JSON to YAML.
func JSONToYAML(j []byte) ([]byte, error) {
	// Convert the JSON to an object.
	var jsonObj any
	// We are using yaml.Unmarshal here (instead of json.Unmarshal) because the
	// Go JSON library doesn't try to pick the right number type (int, float,
	// etc.) when unmarshalling to interface{}, it just picks float64
	// universally. go-yaml does go through the effort of picking the right
	// number type, so we can preserve number type throughout this process.
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
//   - In YAML you can have binary and null keys in your maps. These are invalid
//     in JSON. (int and float keys are converted to strings.)
//   - Binary data in YAML with the !!binary tag is not supported. If you want to
//     use binary data with this library, encode the data as base64 as usual but do
//     not use the !!binary tag in your YAML. This will ensure the original base64
//     encoded data makes it all the way through to the JSON.
func YAMLToJSON(y []byte) ([]byte, error) { //nolint:revive
	dec := yaml.NewDecoder(bytes.NewReader(y))

	return yamlToJSON(dec, nil)
}

// yamlToJSON decodes one YAML document and converts it into JSON bytes.
func yamlToJSON(dec *yaml.Decoder, jsonTarget *reflect.Value) ([]byte, error) {
	// Convert the YAML to an object.
	var yamlObj any
	if err := dec.Decode(&yamlObj); err != nil {
		// Functionality changed in v3 which means we need to ignore EOF error.
		// See https://github.com/go-yaml/yaml/issues/639
		if !errors.Is(err, io.EOF) {
			return nil, err
		}
	}

	// YAML objects are not completely compatible with JSON objects (e.g. you
	// can have non-string keys in YAML). So, convert the YAML-compatible object
	// to a JSON-compatible object, failing with an error if irrecoverable
	// incompatibilities happen along the way.
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

// resolveJSONTarget resolves pointers/interfaces and disables target-guided
// conversion for JSON/Text unmarshaler types.
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
	for k, v := range typed {
		key, err := stringifyYAMLMapKey(k, v)
		if err != nil {
			return nil, err
		}
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

	keyBytes := []byte(key)
	var matched *field
	fields := cachedTypeFields(structVal.Type())
	for i := range fields {
		ff := &fields[i]
		if bytes.Equal(ff.nameBytes, keyBytes) {
			matched = ff
			break
		}
		if matched == nil && ff.equalFold(ff.nameBytes, keyBytes) {
			matched = ff
		}
	}

	if matched == nil {
		return nil
	}

	// Use cached field type directly. This avoids incorrect target selection for
	// deep embedded index paths and nil embedded pointer traversal pitfalls.
	fieldVal := reflect.Zero(matched.typ)
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
