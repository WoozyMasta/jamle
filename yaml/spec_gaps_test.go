package yaml

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSpec_AnchorsAliasesMerge(t *testing.T) {
	t.Parallel()

	type dbConfig struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	type cfg struct {
		Prod dbConfig `json:"prod"`
	}

	input := []byte(`
defaults: &def
  host: localhost
  port: 5432
prod:
  <<: *def
  host: db.prod
`)

	var got cfg
	if err := Unmarshal(input, &got); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	want := cfg{
		Prod: dbConfig{
			Host: "db.prod",
			Port: 5432,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merge decode mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestSpec_MultiDocumentStream(t *testing.T) {
	t.Parallel()

	type doc struct {
		A string `json:"a"`
	}

	input := []byte("---\na: first\n---\na: second\n")
	var got doc
	if err := Unmarshal(input, &got); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	if got.A != "first" {
		t.Fatalf("expected first document to be decoded, got: %q", got.A)
	}
}

func TestSpec_TimestampTag(t *testing.T) {
	t.Parallel()

	type cfg struct {
		When time.Time `json:"when"`
	}

	input := []byte("when: !!timestamp 2025-01-02T03:04:05Z\n")
	var got cfg
	if err := Unmarshal(input, &got); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	want := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	if !got.When.Equal(want) {
		t.Fatalf("timestamp mismatch:\n got: %s\nwant: %s", got.When.UTC(), want.UTC())
	}
}

func TestSpec_BinaryTagLossyInYAMLToJSON(t *testing.T) {
	t.Parallel()

	out, err := YAMLToJSON([]byte("a: !!binary gIGC\n"))
	if err != nil {
		t.Fatalf("YAMLToJSON returned error: %v", err)
	}
	// Binary content is currently not preserved through JSON conversion.
	if string(out) != "{\"a\":\"\\ufffd\\ufffd\\ufffd\"}" {
		t.Fatalf("unexpected !!binary conversion output: %q", string(out))
	}
}

func TestSpec_ComplexMapKeyRejectedInYAMLToJSON(t *testing.T) {
	t.Parallel()

	_, err := YAMLToJSON([]byte("? {a: b}\n: value\n"))
	if err == nil {
		t.Fatal("expected complex map key conversion error")
	}
	if !strings.Contains(err.Error(), "unsupported map key") &&
		!strings.Contains(err.Error(), "invalid map key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSpec_RemapConflictEmbeddedSameJSONTag(t *testing.T) {
	t.Parallel()

	type left struct {
		V string
	}
	type right struct {
		V string
	}
	type cfg struct {
		left
		right
	}

	var got cfg
	if err := Unmarshal([]byte("v: value\n"), &got); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	// Two promoted fields with the same effective key are ambiguous and must not be matched.
	if got.left.V != "" || got.right.V != "" {
		t.Fatalf("ambiguous tag should stay unset, got: %#v", got)
	}
}

func TestSpec_NullQuotedAndBlockScalars(t *testing.T) {
	t.Parallel()

	type cfg struct {
		PlainNull  *string `json:"plain_null"`
		TildeNull  *string `json:"tilde_null"`
		EmptyValue *string `json:"empty_value"`
		QuotedDq   string  `json:"quoted_dq"`
		QuotedSq   string  `json:"quoted_sq"`
		Literal    string  `json:"literal"`
		Folded     string  `json:"folded"`
	}

	input := []byte(`
plain_null: null
tilde_null: ~
empty_value:
quoted_dq: ""
quoted_sq: ''
literal: |+
  line1
  line2
folded: >-
  a
  b
`)

	var got cfg
	if err := Unmarshal(input, &got); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	if got.PlainNull != nil || got.TildeNull != nil || got.EmptyValue != nil {
		t.Fatalf("null/empty values must decode to nil pointers, got: %#v", got)
	}
	if got.QuotedDq != "" || got.QuotedSq != "" {
		t.Fatalf("quoted empty strings mismatch: %#v", got)
	}
	if got.Literal != "line1\nline2\n" {
		t.Fatalf("literal block scalar mismatch: %q", got.Literal)
	}
	if got.Folded != "a b" {
		t.Fatalf("folded block scalar mismatch: %q", got.Folded)
	}
}
