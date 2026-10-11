// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package jamle

import (
	"reflect"
	"testing"
	"time"

	goyaml "go.yaml.in/yaml/v3"
)

// defaultNamedInt verifies defaults for named scalar types.
type defaultNamedInt int

// defaultTextValue verifies defaults for encoding.TextUnmarshaler values.
type defaultTextValue string

// UnmarshalText decodes the test text value.
func (v *defaultTextValue) UnmarshalText(data []byte) error {
	*v = defaultTextValue("text:" + string(data))
	return nil
}

// defaultYAMLValue verifies defaults for yaml.Unmarshaler values.
type defaultYAMLValue string

// UnmarshalYAML decodes the test YAML value.
func (v *defaultYAMLValue) UnmarshalYAML(node *goyaml.Node) error {
	var raw string
	if err := node.Decode(&raw); err != nil {
		return err
	}
	*v = defaultYAMLValue("yaml:" + raw)
	return nil
}

func TestUnmarshalWithOptions_DefaultsPreserveSourcePresence(t *testing.T) {
	t.Parallel()

	type config struct {
		Workers int    `json:"workers" default:"4"`
		Enabled bool   `json:"enabled" default:"true"`
		Name    string `json:"name" default:"default"`
		Pointer *int   `json:"pointer" default:"7"`
	}

	var got config
	if err := Unmarshal([]byte("workers: 0\nenabled: false\nname: \"\"\npointer: null\n"), &got); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	want := config{Workers: 0, Enabled: false, Name: "", Pointer: nil}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded value mismatch: got %#v, want %#v", got, want)
	}

	var missing config
	if err := Unmarshal([]byte("{}\n"), &missing); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if missing.Workers != 4 || !missing.Enabled || missing.Name != "default" ||
		missing.Pointer == nil || *missing.Pointer != 7 {
		t.Fatalf("missing defaults mismatch: %#v", missing)
	}
}

func TestUnmarshalWithOptions_DefaultsPreservePrepopulatedValues(t *testing.T) {
	t.Parallel()

	type config struct {
		Port int    `json:"port" default:"8080"`
		Name string `json:"name" default:"default"`
	}

	got := config{Port: 9000}
	if err := Unmarshal([]byte("{}\n"), &got); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if got.Port != 9000 || got.Name != "default" {
		t.Fatalf("pre-populated value mismatch: %#v", got)
	}
}

func TestUnmarshalOptions_DisableDefaults(t *testing.T) {
	t.Parallel()

	type nested struct {
		Port int `json:"port" default:"8080"`
	}
	type config struct {
		Port   int    `json:"port" default:"9000"`
		Nested nested `json:"nested"`
	}
	options := UnmarshalOptions{DisableDefaults: true}

	var got config
	if err := UnmarshalWithOptions([]byte("{}\n"), &got, options); err != nil {
		t.Fatalf("UnmarshalWithOptions returned error: %v", err)
	}
	if got.Port != 0 || got.Nested.Port != 0 {
		t.Fatalf("disabled defaults changed value: %#v", got)
	}

	var all []config
	if err := UnmarshalAllWithOptions([]byte("---\n{}\n---\n{}\n"), &all, options); err != nil {
		t.Fatalf("UnmarshalAllWithOptions returned error: %v", err)
	}
	if len(all) != 2 || all[0].Port != 0 || all[1].Port != 0 ||
		all[0].Nested.Port != 0 || all[1].Nested.Port != 0 {
		t.Fatalf("disabled defaults changed all-document values: %#v", all)
	}
}

func TestUnmarshalWithOptions_DefaultsNestedValues(t *testing.T) {
	t.Parallel()

	type child struct {
		Port int `json:"port" default:"8080"`
	}
	type config struct {
		Value child  `json:"value"`
		Ptr   *child `json:"ptr"`
	}

	var got config
	if err := Unmarshal([]byte("ptr:\n  port: 9090\n"), &got); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if got.Value.Port != 8080 {
		t.Fatalf("nested value default mismatch: %#v", got.Value)
	}
	if got.Ptr == nil || got.Ptr.Port != 9090 {
		t.Fatalf("nested pointer mismatch: %#v", got.Ptr)
	}

	var absent config
	if err := Unmarshal([]byte("{}\n"), &absent); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if absent.Value.Port != 8080 || absent.Ptr != nil {
		t.Fatalf("absent nested defaults mismatch: %#v", absent)
	}
}

func TestUnmarshalWithOptions_DefaultsIgnorePointerCycles(t *testing.T) {
	t.Parallel()

	type node struct {
		Value int   `json:"value" default:"1"`
		Next  *node `json:"next"`
	}

	self := &node{}
	self.Next = self
	if err := Unmarshal([]byte("{}\n"), self); err != nil {
		t.Fatalf("self-cycle unmarshal returned error: %v", err)
	}
	if self.Value != 1 {
		t.Fatalf("self-cycle default mismatch: got %d, want 1", self.Value)
	}

	first := &node{}
	second := &node{}
	first.Next = second
	second.Next = first
	if err := Unmarshal([]byte("{}\n"), first); err != nil {
		t.Fatalf("two-node cycle unmarshal returned error: %v", err)
	}
	if first.Value != 1 || second.Value != 1 {
		t.Fatalf("two-node cycle defaults mismatch: first=%d second=%d",
			first.Value, second.Value)
	}
}

func TestUnmarshalWithOptions_DefaultsTraverseSharedPointerBranches(t *testing.T) {
	t.Parallel()

	type node struct {
		Value int `json:"value" default:"1"`
	}
	type config struct {
		Left  *node `json:"left"`
		Right *node `json:"right"`
	}

	shared := &node{}
	got := config{Left: shared, Right: shared}
	if err := Unmarshal([]byte("{}\n"), &got); err != nil {
		t.Fatalf("shared-pointer unmarshal returned error: %v", err)
	}
	if got.Left.Value != 1 || got.Right.Value != 1 {
		t.Fatalf("shared-pointer defaults mismatch: left=%d right=%d",
			got.Left.Value, got.Right.Value)
	}
}

func TestApplyDefaults_IgnorePointerCyclesInMapValues(t *testing.T) {
	t.Parallel()

	type node struct {
		Value int   `json:"value" default:"1"`
		Next  *node `json:"next"`
	}
	type config struct {
		Nodes map[string]*node `json:"nodes"`
	}

	self := &node{}
	self.Next = self
	got := config{Nodes: map[string]*node{"self": self}}
	root, err := parseDocument([]byte("nodes:\n  self: {}\n"))
	if err != nil {
		t.Fatalf("parseDocument returned error: %v", err)
	}
	if err := applyDefaults(root, &got); err != nil {
		t.Fatalf("applyDefaults returned error: %v", err)
	}
	if got.Nodes["self"].Value != 1 {
		t.Fatalf("map cycle default mismatch: got %d, want 1", got.Nodes["self"].Value)
	}
}

func TestUnmarshalWithOptions_DefaultsInCollections(t *testing.T) {
	t.Parallel()

	type item struct {
		Name string `json:"name" default:"item"`
	}
	type config struct {
		Items []item            `json:"items"`
		Map   map[string]item   `json:"map"`
		Empty []item            `json:"empty" default:"[{name: default}]"`
		Table map[string]string `json:"table" default:"{key: value}"`
	}

	var got config
	input := []byte("items:\n  - {}\nmap:\n  first: {}\nempty: []\ntable: {}\n")
	if err := Unmarshal(input, &got); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if got.Items[0].Name != "item" || got.Map["first"].Name != "item" || len(got.Empty) != 0 || len(got.Table) != 0 {
		t.Fatalf("collection defaults mismatch: %#v", got)
	}

	var missing config
	if err := Unmarshal([]byte("{}\n"), &missing); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if len(missing.Empty) != 1 || missing.Empty[0].Name != "default" || missing.Table["key"] != "value" {
		t.Fatalf("missing collection defaults mismatch: %#v", missing)
	}
}

func TestUnmarshalWithOptions_DefaultsAliasesAndMerge(t *testing.T) {
	t.Parallel()

	type settings struct {
		Name        string `json:"name" default:"default"`
		DisplayName string `json:"display_name" default:"display"`
		Port        int    `json:"port" default:"8080"`
	}
	type config struct {
		Base    settings `json:"base"`
		Current settings `json:"current"`
	}

	input := []byte("base: &base\n  name: shared\n  display_name: shared\ncurrent:\n  <<: *base\n")
	var got config
	if err := Unmarshal(input, &got); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if got.Base.Name != "shared" || got.Base.DisplayName != "shared" ||
		got.Base.Port != 8080 || got.Current.Name != "shared" ||
		got.Current.DisplayName != "shared" || got.Current.Port != 8080 {
		t.Fatalf("alias/merge defaults mismatch: %#v", got)
	}
}

func TestUnmarshalWithOptions_DefaultsInMapMerge(t *testing.T) {
	t.Parallel()

	type item struct {
		Name string `json:"name" default:"item"`
	}
	type config struct {
		Base  map[string]item `json:"base"`
		Items map[string]item `json:"items"`
	}

	input := []byte("base: &base\n  first: {}\nitems:\n  <<: *base\n")
	var got config
	if err := Unmarshal(input, &got); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if got.Base["first"].Name != "item" || got.Items["first"].Name != "item" {
		t.Fatalf("map merge defaults mismatch: %#v", got)
	}
}

func TestUnmarshalWithOptions_DefaultsSkipRecursiveAndSpecialTypes(t *testing.T) {
	t.Parallel()

	type nested struct {
		Value int `json:"value" default:"4"`
	}
	type config struct {
		Skipped  nested           `json:"skipped" default:"-"`
		Duration time.Duration    `json:"duration" default:"5s"`
		Number   defaultNamedInt  `json:"number" default:"6"`
		Text     defaultTextValue `json:"text" default:"hello"`
		YAML     defaultYAMLValue `json:"yaml" default:"world"`
	}

	var got config
	if err := Unmarshal([]byte("{}\n"), &got); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if got.Skipped.Value != 0 || got.Duration != 5*time.Second || got.Number != 6 ||
		got.Text != "text:hello" || got.YAML != "yaml:world" {
		t.Fatalf("special defaults mismatch: %#v", got)
	}
}

func TestUnmarshalWithOptions_DefaultsSkipPromotedFields(t *testing.T) {
	t.Parallel()

	type embedded struct {
		Port int `json:"port" default:"8080"`
	}
	type config struct {
		embedded `default:"-"`
	}

	var got config
	if err := Unmarshal([]byte("{}\n"), &got); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if got.Port != 0 {
		t.Fatalf("promoted default was applied below default skip: %#v", got)
	}
}

func TestParseDefaultNodeCacheIsolated(t *testing.T) {
	t.Parallel()

	first, err := parseDefaultNode("shared")
	if err != nil {
		t.Fatalf("parseDefaultNode returned error: %v", err)
	}
	if len(first.Content) != 1 {
		t.Fatalf("unexpected default node: %#v", first)
	}
	first.Content[0].Value = "mutated"

	second, err := parseDefaultNode("shared")
	if err != nil {
		t.Fatalf("parseDefaultNode returned error: %v", err)
	}
	if len(second.Content) != 1 || second.Content[0].Value != "shared" {
		t.Fatalf("default node was shared with caller: %#v", second)
	}
}
