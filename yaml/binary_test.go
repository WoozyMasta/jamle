// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package yaml

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestYAMLToJSONBinaryCanonicalizesNestedAndAliasedValues(t *testing.T) {
	t.Parallel()

	input := []byte("defaults: &binary !!binary |\n  AAEC\nitems: [*binary, {child: !!binary 3q2+}]\n")

	out, err := YAMLToJSON(input)
	if err != nil {
		t.Fatalf("YAMLToJSON returned error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("YAMLToJSON produced invalid JSON: %v", err)
	}
	if got["defaults"] != "AAEC" {
		t.Fatalf("unexpected anchor value: %#v", got["defaults"])
	}
	items, ok := got["items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("unexpected aliased sequence: %#v", got["items"])
	}
	if items[0] != "AAEC" {
		t.Fatalf("unexpected alias value: %#v", items[0])
	}
	childMap, ok := items[1].(map[string]any)
	if !ok {
		t.Fatalf("unexpected nested mapping: %#v", items[1])
	}
	child, ok := childMap["child"]
	if !ok || child != "3q2+" {
		t.Fatalf("unexpected nested binary value: %#v", items[1])
	}
}

func TestYAMLToJSONBinaryCanonicalizesWhitespaceAndEmptyValues(t *testing.T) {
	t.Parallel()

	input := []byte("multiline: !!binary |\n  Zm9v\n  YmFy\nempty: !!binary \"\"\n")

	out, err := YAMLToJSON(input)
	if err != nil {
		t.Fatalf("YAMLToJSON returned error: %v", err)
	}

	if got, want := string(out), `{"empty":"","multiline":"Zm9vYmFy"}`; got != want {
		t.Fatalf("unexpected canonical binary JSON:\n got: %s\nwant: %s", got, want)
	}
}

func TestYAMLToJSONBinaryRejectsInvalidBase64(t *testing.T) {
	t.Parallel()

	_, err := YAMLToJSON([]byte("value: !!binary not-base64!\n"))
	if err == nil {
		t.Fatal("expected invalid !!binary error")
	}
	if !strings.Contains(err.Error(), "invalid !!binary value") {
		t.Fatalf("unexpected error: %v", err)
	}
}
