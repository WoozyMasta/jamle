// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package yaml

import (
	"errors"
	"testing"
)

func TestUnmarshal_SingleDocumentBoundaries(t *testing.T) {
	t.Parallel()

	type config struct {
		Value string `json:"value"`
	}

	tests := []struct {
		name      string
		input     string
		wantValue string
		wantMulti bool
	}{
		{name: "one document", input: "value: one\n", wantValue: "one"},
		{name: "comments after document", input: "value: comments\n# trailing\n", wantValue: "comments"},
		{name: "explicit document end", input: "value: ended\n...\n", wantValue: "ended"},
		{name: "two documents", input: "value: first\n---\nvalue: second\n", wantMulti: true},
		{name: "empty second document", input: "value: first\n---\n", wantMulti: true},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			var got config
			err := Unmarshal([]byte(test.input), &got)
			if test.wantMulti {
				if !errors.Is(err, ErrMultipleDocuments) {
					t.Fatalf("expected ErrMultipleDocuments, got: %v", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("Unmarshal returned error: %v", err)
			}
			if got.Value != test.wantValue {
				t.Fatalf("decoded value mismatch: got %q, want %q", got.Value, test.wantValue)
			}
		})
	}
}

func TestUnmarshalWithOptions_RejectsMultipleDocuments(t *testing.T) {
	t.Parallel()

	type config struct {
		Value string `json:"value"`
	}

	var got config
	err := UnmarshalWithOptions(
		[]byte("value: first\n---\nvalue: second\n"),
		&got,
		DecodeOptions{},
	)
	if !errors.Is(err, ErrMultipleDocuments) {
		t.Fatalf("expected ErrMultipleDocuments, got: %v", err)
	}
}
