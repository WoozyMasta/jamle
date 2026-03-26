package yaml

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	goyaml "go.yaml.in/yaml/v3"
)

func TestUnmarshalNode_JSONTagToYAMLTagRemap(t *testing.T) {
	t.Parallel()

	type sample struct {
		Value string `json:"json_name" yaml:"yaml_name"`
	}

	var got sample
	err := unmarshalNodeFromString("json_name: ok\n", &got)
	if err != nil {
		t.Fatalf("UnmarshalNode returned error: %v", err)
	}
	if got.Value != "ok" {
		t.Fatalf("decoded value mismatch: got %q, want %q", got.Value, "ok")
	}
}

func TestUnmarshalNode_CaseInsensitiveKeys(t *testing.T) {
	t.Parallel()

	type sample struct {
		Port int `json:"port"`
	}

	var got sample
	err := unmarshalNodeFromString("PORT: 8080\n", &got)
	if err != nil {
		t.Fatalf("UnmarshalNode returned error: %v", err)
	}
	if got.Port != 8080 {
		t.Fatalf("decoded port mismatch: got %d, want %d", got.Port, 8080)
	}
}

func TestUnmarshalNode_AnonymousEmbeddedInline(t *testing.T) {
	t.Parallel()

	type Embedded struct {
		Name string `json:"name"`
	}
	type sample struct {
		Embedded `yaml:",inline"`
	}

	var got sample
	err := unmarshalNodeFromString("NAME: root\n", &got)
	if err != nil {
		t.Fatalf("UnmarshalNode returned error: %v", err)
	}
	if got.Name != "root" {
		t.Fatalf("embedded inline field mismatch: got %q, want %q", got.Name, "root")
	}
}

func TestUnmarshalNode_PointerSliceMap(t *testing.T) {
	t.Parallel()

	type Child struct {
		ID string `json:"id"`
	}
	type sample struct {
		Ptr   *Child            `json:"ptr"`
		Items []Child           `json:"items"`
		Dict  map[string]*Child `json:"dict"`
	}

	var got sample
	err := unmarshalNodeFromString(`
ptr:
  id: one
items:
  - id: two
dict:
  x:
    id: three
`, &got)
	if err != nil {
		t.Fatalf("UnmarshalNode returned error: %v", err)
	}
	if got.Ptr == nil || got.Ptr.ID != "one" {
		t.Fatalf("pointer field mismatch: got %#v", got.Ptr)
	}
	if len(got.Items) != 1 || got.Items[0].ID != "two" {
		t.Fatalf("slice field mismatch: got %#v", got.Items)
	}
	if got.Dict["x"] == nil || got.Dict["x"].ID != "three" {
		t.Fatalf("map field mismatch: got %#v", got.Dict)
	}
}

func TestUnmarshalNode_DuplicateAfterCaseFold(t *testing.T) {
	t.Parallel()

	type sample struct {
		Port int `json:"port"`
	}

	var got sample
	err := unmarshalNodeFromString("port: 1\nPORT: 2\n", &got)
	if err == nil {
		t.Fatal("expected duplicate-key error")
	}
	if !strings.Contains(err.Error(), `key "port" already defined`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConvertToJSONableObject_StringCoercion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   any
		want any
	}{
		{name: "int", in: int(1), want: "1"},
		{name: "int64", in: int64(2), want: "2"},
		{name: "float64", in: float64(1.5), want: "1.5"},
		{name: "uint64", in: uint64(3), want: "3"},
		{name: "bool true", in: true, want: "true"},
		{name: "bool false", in: false, want: "false"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			target := reflect.New(reflect.TypeOf("")).Elem()
			got, err := convertToJSONableObject(tc.in, &target)
			if err != nil {
				t.Fatalf("convertToJSONableObject returned error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("conversion mismatch: got %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestConvertToJSONableObject_StructAndSliceTargets(t *testing.T) {
	t.Parallel()

	type sample struct {
		A string `json:"a"`
		B int    `json:"b"`
	}

	structTarget := reflect.New(reflect.TypeOf(sample{})).Elem()
	gotStruct, err := convertToJSONableObject(map[string]any{"a": 1, "b": 2}, &structTarget)
	if err != nil {
		t.Fatalf("convertToJSONableObject(struct) returned error: %v", err)
	}
	wantStruct := map[string]any{"a": "1", "b": 2}
	if !reflect.DeepEqual(gotStruct, wantStruct) {
		t.Fatalf("struct-target conversion mismatch:\n got: %#v\nwant: %#v", gotStruct, wantStruct)
	}

	sliceTarget := reflect.Zero(reflect.TypeOf([]string{}))
	gotSlice, err := convertToJSONableObject([]any{1, true}, &sliceTarget)
	if err != nil {
		t.Fatalf("convertToJSONableObject(slice) returned error: %v", err)
	}
	wantSlice := []any{"1", "true"}
	if !reflect.DeepEqual(gotSlice, wantSlice) {
		t.Fatalf("slice-target conversion mismatch:\n got: %#v\nwant: %#v", gotSlice, wantSlice)
	}
}

func TestConvertToJSONableObject_EmbeddedStructTargets(t *testing.T) {
	t.Parallel()

	type EmbeddedPort struct {
		Port string `json:"port"`
	}
	type EmbeddedSimple struct {
		EmbeddedPort
	}

	type DeepLevel3 struct {
		Region string `json:"region"`
	}
	type DeepLevel2 struct {
		DeepLevel3
	}
	type DeepLevel1 struct {
		DeepLevel2
	}

	type PointerInner struct {
		Name string `json:"name"`
	}
	type PointerOuter struct {
		*PointerInner
	}

	type Mixed struct {
		ID int `json:"id"`
		EmbeddedPort
	}

	tests := []struct {
		name   string
		target any
		in     map[string]any
		want   map[string]any
	}{
		{
			name:   "single embedded level",
			target: EmbeddedSimple{},
			in:     map[string]any{"port": 8080},
			want:   map[string]any{"port": "8080"},
		},
		{
			name:   "multi embedded levels",
			target: DeepLevel1{},
			in:     map[string]any{"region": 42},
			want:   map[string]any{"region": "42"},
		},
		{
			name:   "embedded pointer level",
			target: PointerOuter{},
			in:     map[string]any{"name": true},
			want:   map[string]any{"name": "true"},
		},
		{
			name:   "mixed regular and embedded fields",
			target: Mixed{},
			in:     map[string]any{"id": 7, "port": 9090},
			want:   map[string]any{"id": 7, "port": "9090"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			target := reflect.New(reflect.TypeOf(tc.target)).Elem()

			got, err := convertToJSONableObject(tc.in, &target)
			if err != nil {
				t.Fatalf("convertToJSONableObject returned error: %v", err)
			}

			gotMap, ok := got.(map[string]any)
			if !ok {
				t.Fatalf("expected map output, got %T", got)
			}

			if !reflect.DeepEqual(gotMap, tc.want) {
				t.Fatalf("conversion mismatch:\n got: %#v\nwant: %#v", gotMap, tc.want)
			}
		})
	}
}

func TestConvertToJSONableObject_UnsupportedMapKey(t *testing.T) {
	t.Parallel()

	_, err := convertToJSONableObject(map[any]any{struct{}{}: "v"}, nil)
	if err == nil {
		t.Fatal("expected unsupported-map-key error")
	}
	if !strings.Contains(err.Error(), "unsupported map key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func unmarshalNodeFromString(src string, out any) error {
	var root goyaml.Node
	dec := goyaml.NewDecoder(bytes.NewReader([]byte(src)))
	if err := dec.Decode(&root); err != nil {
		return err
	}

	return UnmarshalNode(&root, out)
}
