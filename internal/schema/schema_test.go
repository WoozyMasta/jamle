// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package schema

import (
	"encoding/json"
	"reflect"
	"testing"

	goyaml "go.yaml.in/yaml/v3"
)

// schemaEmbedded contains a field promoted by an anonymous embedding.
type schemaEmbedded struct {
	Name string `json:"name"`
}

// schemaPointerEmbedded contains a field promoted through a pointer.
type schemaPointerEmbedded struct {
	Port int `json:"port"`
}

// schemaLeft and schemaRight intentionally expose the same promoted name.
type schemaLeft struct {
	Value string
}

type schemaRight struct {
	Value string
}

// schemaTaggedLeft and schemaTaggedRight test tag-based dominance.
type schemaTaggedLeft struct {
	Value string
}

type schemaTaggedRight struct {
	Value string `json:"value"`
}

// schemaYAMLKey carries a YAML-only decode alias.
type schemaYAMLKey struct {
	Value string `json:"value" yaml:"yaml_value"`
}

// schemaOpaqueYAML implements node-owned decoding.
type schemaOpaqueYAML struct{}

// UnmarshalYAML marks schemaOpaqueYAML as an opaque decode boundary.
func (*schemaOpaqueYAML) UnmarshalYAML(*goyaml.Node) error {
	return nil
}

// schemaOpaqueText implements scalar text decoding.
type schemaOpaqueText string

// UnmarshalText marks schemaOpaqueText as an opaque decode boundary.
func (*schemaOpaqueText) UnmarshalText([]byte) error {
	return nil
}

func TestForJSONFieldRules(t *testing.T) {
	type config struct {
		Plain        string `json:"plain"`
		Ignored      string `json:"-"`
		IgnoredAlias string `json:"-" yaml:"hidden"`
		schemaEmbedded
		*schemaPointerEmbedded
		schemaLeft
		schemaRight
	}

	info := For(reflect.TypeFor[config]())
	if _, ok := info.Lookup("ignored"); ok {
		t.Fatal("json '-' field was included")
	}
	for _, name := range []string{"ignored", "hidden"} {
		if !info.IsIgnoredKey(name) {
			t.Fatalf("JSON-ignored key %q was not recorded", name)
		}
	}

	for _, name := range []string{"plain", "name", "port"} {
		if _, ok := info.Lookup(name); !ok {
			t.Fatalf("field %q was not discovered", name)
		}
	}

	if _, ok := info.Lookup("value"); ok {
		t.Fatal("conflicting promoted fields were not rejected")
	}
	if _, ok := info.Lookup("PLAIN"); !ok {
		t.Fatal("case-insensitive lookup failed")
	}
}

func TestForTaggedFieldWins(t *testing.T) {
	type config struct {
		schemaTaggedLeft
		schemaTaggedRight
	}

	field, ok := For(reflect.TypeFor[config]()).Lookup("value")
	if !ok {
		t.Fatal("tagged promoted field was not discovered")
	}
	if len(field.Index) != 2 || field.Index[0] != 1 {
		t.Fatalf("wrong dominant field index: %v", field.Index)
	}
}

func TestForAcceptsJSONTagPunctuation(t *testing.T) {
	t.Parallel()

	type config struct {
		Value string `json:"value;with:semicolon"`
	}

	field, ok := For(reflect.TypeFor[config]()).Lookup("value;with:semicolon")
	if !ok || field.JSONName != "value;with:semicolon" {
		t.Fatalf("semicolon JSON tag was not accepted: %#v, %t", field, ok)
	}
}

func TestLookupFoldedJSONNameUsesFirstField(t *testing.T) {
	t.Parallel()

	type config struct {
		Foo string `json:"Foo"`
		FOO string `json:"FOO"`
	}

	var decoded config
	if err := json.Unmarshal([]byte(`{"foo":"value"}`), &decoded); err != nil {
		t.Fatalf("encoding/json.Unmarshal returned error: %v", err)
	}
	if decoded.Foo != "value" || decoded.FOO != "" {
		t.Fatalf("unexpected encoding/json result: %#v", decoded)
	}

	field, ok := For(reflect.TypeFor[config]()).Lookup("foo")
	if !ok || len(field.Index) != 1 || field.Index[0] != 0 {
		t.Fatalf("unexpected folded JSON field: %#v, %t", field, ok)
	}
}

func TestLookupFoldedYAMLNameUsesFirstField(t *testing.T) {
	t.Parallel()

	type config struct {
		First  string `json:"first" yaml:"Foo"`
		Second string `json:"second" yaml:"FOO"`
	}

	field, ok := For(reflect.TypeFor[config]()).Lookup("foo")
	if !ok || len(field.Index) != 1 || field.Index[0] != 0 {
		t.Fatalf("unexpected folded YAML field: %#v, %t", field, ok)
	}
}

func TestForYAMLDecodePathForPromotedFields(t *testing.T) {
	type config struct {
		schemaEmbedded
	}

	field, ok := For(reflect.TypeFor[config]()).Lookup("name")
	if !ok {
		t.Fatal("promoted field was not discovered")
	}
	if len(field.YAMLDecodePath) != 1 ||
		field.YAMLDecodePath[0].Key != "schemaembedded" ||
		field.YAMLDecodePath[0].Type != reflect.TypeFor[schemaEmbedded]() {
		t.Fatalf("unexpected YAML decode path: %#v", field.YAMLDecodePath)
	}

	type inlineConfig struct {
		schemaEmbedded `yaml:",inline"`
	}
	inlineField, ok := For(reflect.TypeFor[inlineConfig]()).Lookup("name")
	if !ok || len(inlineField.YAMLDecodePath) != 0 {
		t.Fatalf("inline field unexpectedly needs a YAML path: %#v", inlineField.YAMLDecodePath)
	}
}

func TestForMetadataAndOpaqueBoundaries(t *testing.T) {
	type config struct {
		Value schemaYAMLKey    `json:"value" yaml:"yaml_value" jamle:"noexpand" default:"x"`
		Skip  string           `json:"skip" default:"-"`
		YAML  schemaOpaqueYAML `json:"yaml"`
		Text  schemaOpaqueText `json:"text"`
	}

	info := For(reflect.TypeFor[*config]())
	if !info.HasDefaults || !info.HasNoExpand {
		t.Fatal("subtree metadata flags were not populated")
	}

	value, ok := info.Lookup("value")
	if !ok {
		t.Fatal("value field was not discovered")
	}
	if value.YAMLDecodeKey != "yaml_value" || !value.NoExpand ||
		!value.HasDefault || value.Default != "x" {
		t.Fatalf("unexpected value metadata: %#v", value)
	}

	skip, ok := info.Lookup("skip")
	if !ok || !skip.SkipDefaults || skip.HasDefault {
		t.Fatalf("unexpected skip metadata: %#v", skip)
	}

	for _, name := range []string{"yaml", "text"} {
		field, ok := info.Lookup(name)
		if !ok || !field.Opaque {
			t.Fatalf("field %q is not marked opaque: %#v", name, field)
		}
	}
	if !IsOpaque(reflect.TypeFor[schemaOpaqueYAML]()) ||
		!IsOpaque(reflect.TypeFor[schemaOpaqueText]()) {
		t.Fatal("decode interface classification failed")
	}
	if !IsTextUnmarshaler(reflect.TypeFor[schemaOpaqueText]()) {
		t.Fatal("text decoder classification failed")
	}
	if !IsYAMLUnmarshaler(reflect.TypeFor[schemaOpaqueYAML]()) {
		t.Fatal("YAML decoder classification failed")
	}
}

func TestForCachesImmutableMetadata(t *testing.T) {
	type config struct {
		Value string `json:"value"`
	}

	typ := reflect.TypeFor[config]()
	if For(typ) != For(reflect.PointerTo(typ)) {
		t.Fatal("pointer and value lookups did not share cached metadata")
	}
}
