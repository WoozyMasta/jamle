// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package yaml

import (
	"bytes"
	"strings"
	"testing"

	goyaml "go.yaml.in/yaml/v3"
)

func TestUnmarshalWithOptions_StrictYAML(t *testing.T) {
	t.Parallel()

	type child struct {
		Name string `json:"name"`
	}
	type Embedded struct {
		Name string `json:"name"`
	}
	type config struct {
		Name    string           `json:"name"`
		Child   child            `json:"child"`
		Items   map[string]child `json:"items"`
		Any     map[string]any   `json:"any"`
		List    []child          `json:"list"`
		Payload testNodeCustom   `json:"payload"`
	}

	t.Run("unknown field reports path and position", func(t *testing.T) {
		t.Parallel()

		var got config
		err := UnmarshalWithOptions([]byte("name: ok\nchild:\n  extra: no\n"), &got, DecodeOptions{Strict: true})
		if err == nil {
			t.Fatal("expected strict unknown-field error")
		}
		if !strings.Contains(err.Error(), "$.child.extra") ||
			!strings.Contains(err.Error(), ":3:") ||
			!strings.Contains(err.Error(), `unknown field "extra"`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("flow yaml", func(t *testing.T) {
		t.Parallel()

		var got config
		err := UnmarshalWithOptions([]byte("{name: ok, child: {name: nested}}\n"), &got, DecodeOptions{Strict: true})
		if err != nil {
			t.Fatalf("UnmarshalWithOptions returned error: %v", err)
		}
		if got.Name != "ok" || got.Child.Name != "nested" {
			t.Fatalf("decoded value mismatch: %#v", got)
		}
	})

	t.Run("embedded fields and json aliases", func(t *testing.T) {
		t.Parallel()

		type aliasConfig struct {
			Embedded    `yaml:",inline"`
			DisplayName string `json:"display_name" yaml:"friendly_name"`
		}

		var got aliasConfig
		err := UnmarshalWithOptions([]byte("name: ok\nfriendly_name: named\n"), &got, DecodeOptions{Strict: true})
		if err != nil {
			t.Fatalf("UnmarshalWithOptions returned error: %v", err)
		}
		if got.Name != "ok" || got.DisplayName != "named" {
			t.Fatalf("decoded value mismatch: %#v", got)
		}
	})

	t.Run("map values are validated", func(t *testing.T) {
		t.Parallel()

		var got config
		err := UnmarshalWithOptions([]byte("items:\n  first:\n    extra: no\n"), &got, DecodeOptions{Strict: true})
		if err == nil || !strings.Contains(err.Error(), "$.items.first.extra") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("map any values stay open", func(t *testing.T) {
		t.Parallel()

		var got config
		err := UnmarshalWithOptions([]byte("any:\n  arbitrary:\n    extra: ok\n"), &got, DecodeOptions{Strict: true})
		if err != nil {
			t.Fatalf("UnmarshalWithOptions returned error: %v", err)
		}
	})

	t.Run("custom unmarshaler is opaque", func(t *testing.T) {
		t.Parallel()

		var got config
		err := UnmarshalWithOptions([]byte("payload:\n  arbitrary: ok\n"), &got, DecodeOptions{Strict: true})
		if err != nil {
			t.Fatalf("UnmarshalWithOptions returned error: %v", err)
		}
		if got.Payload.Key != "arbitrary" {
			t.Fatalf("custom decoder did not receive original key: %#v", got.Payload)
		}
	})
}

func TestUnmarshalWithOptions_StrictYAMLAliasAndMerge(t *testing.T) {
	t.Parallel()

	type settings struct {
		Name string `json:"name"`
		Port int    `json:"port"`
	}
	type config struct {
		Defaults settings `json:"defaults"`
		Current  settings `json:"current"`
	}

	input := []byte("defaults: &base\n  name: app\n  port: 8080\ncurrent:\n  <<: *base\n")
	var got config
	if err := UnmarshalWithOptions(input, &got, DecodeOptions{Strict: true}); err != nil {
		t.Fatalf("UnmarshalWithOptions returned error: %v", err)
	}
	if got.Current.Name != "app" || got.Current.Port != 8080 {
		t.Fatalf("merged value mismatch: %#v", got.Current)
	}
}

func TestUnmarshalWithOptions_StrictJSON(t *testing.T) {
	t.Parallel()

	type config struct {
		Name string `json:"name"`
	}

	var got config
	err := UnmarshalWithOptions([]byte(`{"name":"ok","extra":true}`), &got, DecodeOptions{Strict: true})
	if err == nil || !strings.Contains(err.Error(), `unknown field "extra"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUnmarshalNodeWithOptions_Strict(t *testing.T) {
	t.Parallel()

	type config struct {
		Name string `json:"name"`
	}

	var node goyaml.Node
	decoder := goyaml.NewDecoder(bytes.NewReader([]byte("name: ok\n")))
	if err := decoder.Decode(&node); err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}

	var got config
	if err := UnmarshalNodeWithOptions(&node, &got, DecodeOptions{Strict: true}); err != nil {
		t.Fatalf("UnmarshalNodeWithOptions returned error: %v", err)
	}
	if got.Name != "ok" {
		t.Fatalf("decoded value mismatch: %#v", got)
	}
}

func TestUnmarshalAuto_StrictYAMLFlow(t *testing.T) {
	t.Parallel()

	type config struct {
		Name string `json:"name"`
	}

	var got config
	if err := UnmarshalAuto([]byte("{name: ok}\n"), &got, AutoOptions{Strict: true}); err != nil {
		t.Fatalf("UnmarshalAuto returned error: %v", err)
	}
	if got.Name != "ok" {
		t.Fatalf("decoded value mismatch: %#v", got)
	}

	err := UnmarshalAuto([]byte("{name: ok, extra: no}\n"), &got, AutoOptions{Strict: true})
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unexpected error: %v", err)
	}
}
