// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package yaml

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	goyaml "go.yaml.in/yaml/v3"
)

func TestUnmarshalNode_RejectsInvalidDestinations(t *testing.T) {
	t.Parallel()

	var value struct{}
	var nilPointer *struct{}
	node := &goyaml.Node{Kind: goyaml.ScalarNode, Tag: "!!null", Value: "null"}
	tests := []struct {
		name   string
		target any
	}{
		{name: "nil interface", target: nil},
		{name: "non-pointer", target: value},
		{name: "typed nil pointer", target: nilPointer},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if err := UnmarshalNode(node, test.target); !errors.Is(err, ErrInvalidDestination) {
				t.Fatalf("UnmarshalNode error: got %v, want ErrInvalidDestination", err)
			}
		})
	}
}

func TestUnmarshalNode_ValidatesDestinationForEmptyInput(t *testing.T) {
	t.Parallel()

	if err := Unmarshal(nil, nil); !errors.Is(err, ErrInvalidDestination) {
		t.Fatalf("Unmarshal error: got %v, want ErrInvalidDestination", err)
	}
}

// testNodeCustom records the first key received by a custom decoder.
type testNodeCustom struct {
	Key string
}

// UnmarshalYAML records the node shape without delegating to the standard decoder.
func (c *testNodeCustom) UnmarshalYAML(node *goyaml.Node) error {
	if node.Kind == goyaml.MappingNode && len(node.Content) > 0 {
		c.Key = node.Content[0].Value
	}

	return nil
}

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

func TestUnmarshalNode_DoesNotMutateInput(t *testing.T) {
	t.Parallel()

	type sample struct {
		Value string `json:"json_name" yaml:"yaml_name"`
	}

	var root goyaml.Node
	dec := goyaml.NewDecoder(bytes.NewReader([]byte("json_name: ok\n")))
	if err := dec.Decode(&root); err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}

	key := root.Content[0].Content[0].Value
	var got sample
	if err := UnmarshalNode(&root, &got); err != nil {
		t.Fatalf("UnmarshalNode returned error: %v", err)
	}
	if got.Value != "ok" {
		t.Fatalf("decoded value mismatch: got %q", got.Value)
	}
	if root.Content[0].Content[0].Value != key {
		t.Fatalf("UnmarshalNode mutated input key: got %q, want %q",
			root.Content[0].Content[0].Value, key)
	}
}

func TestUnmarshalNode_CanDecodeSameNodeTwice(t *testing.T) {
	t.Parallel()

	type first struct {
		Value string `json:"value" yaml:"first"`
	}
	type second struct {
		Value string `json:"value" yaml:"second"`
	}

	var root goyaml.Node
	dec := goyaml.NewDecoder(bytes.NewReader([]byte("value: ok\n")))
	if err := dec.Decode(&root); err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}

	var gotFirst first
	if err := UnmarshalNode(&root, &gotFirst); err != nil {
		t.Fatalf("first UnmarshalNode returned error: %v", err)
	}
	var gotSecond second
	if err := UnmarshalNode(&root, &gotSecond); err != nil {
		t.Fatalf("second UnmarshalNode returned error: %v", err)
	}
	if gotFirst.Value != "ok" || gotSecond.Value != "ok" {
		t.Fatalf("same-node decode mismatch: first=%#v second=%#v",
			gotFirst, gotSecond)
	}
}

func TestUnmarshalNode_PreservesAnchorAliasGraph(t *testing.T) {
	t.Parallel()

	type child struct {
		Value string `json:"value" yaml:"yaml_value"`
	}
	type sample struct {
		Defaults child `json:"defaults"`
		Current  child `json:"current"`
	}

	var root goyaml.Node
	dec := goyaml.NewDecoder(bytes.NewReader([]byte(
		"defaults: &base\n  value: ok\ncurrent: *base\n",
	)))
	if err := dec.Decode(&root); err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}

	originalAnchorKey := root.Content[0].Content[1].Content[0].Value
	var got sample
	if err := UnmarshalNode(&root, &got); err != nil {
		t.Fatalf("UnmarshalNode returned error: %v", err)
	}
	if got.Defaults.Value != "ok" || got.Current.Value != "ok" {
		t.Fatalf("alias decode mismatch: %#v", got)
	}
	if root.Content[0].Content[1].Content[0].Value != originalAnchorKey {
		t.Fatalf("anchor node was mutated: got %q, want %q",
			root.Content[0].Content[1].Content[0].Value, originalAnchorKey)
	}
}

func TestUnmarshalNode_AliasUsesConsumerSchema(t *testing.T) {
	t.Parallel()

	type owner struct {
		Value string `json:"value" yaml:"first_value"`
	}
	type consumer struct {
		Value string `json:"value" yaml:"second_value"`
	}
	type sample struct {
		Base   owner    `json:"base"`
		First  owner    `json:"first"`
		Second consumer `json:"second"`
	}

	input := []byte("base: &shared\n  value: ok\nfirst: *shared\nsecond: *shared\n")
	var got sample
	if err := Unmarshal(input, &got); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if got.Base.Value != "ok" || got.First.Value != "ok" || got.Second.Value != "ok" {
		t.Fatalf("alias consumer mismatch: %#v", got)
	}
}

func TestCloneNodeGraph_PreservesMetadataAndAliasIdentity(t *testing.T) {
	t.Parallel()

	anchor := &goyaml.Node{
		Kind:        goyaml.MappingNode,
		Anchor:      "base",
		HeadComment: "anchor comment",
		Style:       goyaml.FlowStyle,
		Content: []*goyaml.Node{
			{Kind: goyaml.ScalarNode, Value: "key", Line: 3, Column: 2},
		},
	}
	root := &goyaml.Node{
		Kind: goyaml.DocumentNode,
		Content: []*goyaml.Node{
			anchor,
			{Kind: goyaml.AliasNode, Alias: anchor},
		},
	}

	cloned := cloneNodeGraph(root)
	if cloned == root || cloned.Content[0] == anchor {
		t.Fatal("node graph was not copied")
	}
	if cloned.Content[1].Alias != cloned.Content[0] {
		t.Fatal("alias identity was not preserved")
	}
	if cloned.Content[0].Anchor != anchor.Anchor ||
		cloned.Content[0].HeadComment != anchor.HeadComment ||
		cloned.Content[0].Style != anchor.Style {
		t.Fatal("node metadata was not preserved")
	}
	if cloned.Content[0].Content[0].Line != 3 ||
		cloned.Content[0].Content[0].Column != 2 {
		t.Fatal("node position was not preserved")
	}
}

func TestUnmarshalNode_CustomUnmarshalerSeesOriginalKeys(t *testing.T) {
	t.Parallel()

	type sample struct {
		Payload testNodeCustom `json:"payload"`
	}

	var got sample
	err := unmarshalNodeFromString("payload:\n  json_name: ok\n", &got)
	if err != nil {
		t.Fatalf("UnmarshalNode returned error: %v", err)
	}
	if got.Payload.Key != "json_name" {
		t.Fatalf("custom unmarshaler saw remapped key %q", got.Payload.Key)
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

func TestUnmarshalNode_AnonymousEmbeddedUsesJSONPromotion(t *testing.T) {
	t.Parallel()

	type Embedded struct {
		Name string `json:"name"`
	}
	type sample struct {
		Embedded
	}

	var got sample
	err := unmarshalNodeFromString("NAME: root\n", &got)
	if err != nil {
		t.Fatalf("UnmarshalNode returned error: %v", err)
	}
	if got.Name != "root" {
		t.Fatalf("embedded JSON field mismatch: got %q, want %q", got.Name, "root")
	}
}

func TestUnmarshalNode_IgnoresJSONIgnoredFields(t *testing.T) {
	t.Parallel()

	type sample struct {
		Ignored       string `json:"-" yaml:"hidden"`
		IgnoredByName string `json:"-"`
	}

	var got sample
	if err := unmarshalNodeFromString("hidden: value\nignoredbyname: other\n", &got); err != nil {
		t.Fatalf("UnmarshalNode returned error: %v", err)
	}
	if got.Ignored != "" || got.IgnoredByName != "" {
		t.Fatalf("JSON-ignored field was decoded: %#v", got)
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
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			target := reflect.New(reflect.TypeFor[string]()).Elem()
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

	structTarget := reflect.New(reflect.TypeFor[sample]()).Elem()
	gotStruct, err := convertToJSONableObject(map[string]any{"a": 1, "b": 2}, &structTarget)
	if err != nil {
		t.Fatalf("convertToJSONableObject(struct) returned error: %v", err)
	}
	wantStruct := map[string]any{"a": "1", "b": 2}
	if !reflect.DeepEqual(gotStruct, wantStruct) {
		t.Fatalf("struct-target conversion mismatch:\n got: %#v\nwant: %#v", gotStruct, wantStruct)
	}

	sliceTarget := reflect.Zero(reflect.TypeFor[[]string]())
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
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

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
