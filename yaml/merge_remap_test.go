// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package yaml

import "testing"

func TestUnmarshal_MergeRemapsStructConsumerSchema(t *testing.T) {
	t.Parallel()

	type app struct {
		DisplayName string `json:"display_name"`
	}
	type config struct {
		Defaults app `json:"defaults"`
		App      app `json:"app"`
	}

	input := []byte("defaults: &defaults\n  display_name: hello\napp:\n  <<: *defaults\n")
	var got config
	if err := Unmarshal(input, &got); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if got.Defaults.DisplayName != "hello" || got.App.DisplayName != "hello" {
		t.Fatalf("merged struct mismatch: %#v", got)
	}
}

func TestUnmarshal_MergeRemapsMapElementSchema(t *testing.T) {
	t.Parallel()

	type item struct {
		DisplayName string `json:"display_name"`
	}
	type config struct {
		Defaults map[string]item `json:"defaults"`
		Items    map[string]item `json:"items"`
	}

	input := []byte("defaults: &defaults\n  primary:\n    display_name: hello\nitems:\n  <<: *defaults\n")
	var got config
	if err := Unmarshal(input, &got); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if got.Items["primary"].DisplayName != "hello" {
		t.Fatalf("merged map mismatch: %#v", got.Items)
	}
}

func TestUnmarshal_MergeSequenceRemapsStructConsumerSchema(t *testing.T) {
	t.Parallel()

	type app struct {
		DisplayName string `json:"display_name"`
	}
	type config struct {
		First  app `json:"first"`
		Second app `json:"second"`
		App    app `json:"app"`
	}

	input := []byte("first: &first\n  display_name: first\nsecond: &second\n  display_name: second\napp:\n  <<: [*first, *second]\n")
	var got config
	if err := Unmarshal(input, &got); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if got.App.DisplayName != "first" {
		t.Fatalf("merged sequence mismatch: %#v", got.App)
	}
}

func TestUnmarshalWithOptions_StrictMergeRemapsStructConsumerSchema(t *testing.T) {
	t.Parallel()

	type app struct {
		DisplayName string `json:"display_name"`
	}
	type config struct {
		Defaults app `json:"defaults"`
		App      app `json:"app"`
	}

	input := []byte("defaults: &defaults\n  display_name: hello\napp:\n  <<: *defaults\n")
	var got config
	if err := UnmarshalWithOptions(input, &got, DecodeOptions{Strict: true}); err != nil {
		t.Fatalf("UnmarshalWithOptions returned error: %v", err)
	}
	if got.App.DisplayName != "hello" {
		t.Fatalf("strict merged struct mismatch: %#v", got.App)
	}
}

func TestUnmarshal_NestedAliasUsesEachConsumerSchema(t *testing.T) {
	t.Parallel()

	type firstLeaf struct {
		Value string `json:"value" yaml:"first_value"`
	}
	type secondLeaf struct {
		Value string `json:"value" yaml:"second_value"`
	}
	type container struct {
		First  firstLeaf  `json:"first"`
		Second secondLeaf `json:"second"`
	}
	type config struct {
		Leaf      firstLeaf `json:"leaf"`
		Container container `json:"container"`
		Use       container `json:"use"`
	}

	input := []byte("leaf: &leaf\n  value: ok\ncontainer: &container\n  first: *leaf\n  second: *leaf\nuse: *container\n")
	var got config
	if err := Unmarshal(input, &got); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if got.Use.First.Value != "ok" || got.Use.Second.Value != "ok" {
		t.Fatalf("nested alias mismatch: %#v", got.Use)
	}
}
