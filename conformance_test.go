// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package jamle

import (
	"testing"

	goyaml "go.yaml.in/yaml/v3"
)

// conformanceItem combines source expansion, noexpand, and nested defaults.
type conformanceItem struct {
	Host   string `json:"host" default:"localhost"`
	Port   int    `json:"port" default:"8080"`
	Script string `json:"script" jamle:"noexpand"`
}

// conformanceConfig is the typed target used by cross-feature tests and fuzzing.
type conformanceConfig struct {
	Name    string                     `json:"name"`
	Base    conformanceItem            `json:"base"`
	Current conformanceItem            `json:"current"`
	Items   []conformanceItem          `json:"items"`
	Values  map[string]conformanceItem `json:"values"`
}

// conformanceEmbedded is promoted into an embedded strict/defaults target.
type conformanceEmbedded struct {
	Host string `json:"host" default:"embedded.local"`
}

// conformanceOpaque owns decoding for the custom-unmarshaler boundary test.
type conformanceOpaque struct {
	Value string
}

// UnmarshalYAML decodes the opaque payload without exposing its fields to strict validation.
func (v *conformanceOpaque) UnmarshalYAML(node *goyaml.Node) error {
	var values map[string]string
	if err := node.Decode(&values); err != nil {
		return err
	}

	v.Value = values["value"]
	return nil
}

func TestUnmarshalWithOptions_ConformanceMatrix(t *testing.T) {
	t.Parallel()

	t.Run("expansion strict defaults merge aliases", func(t *testing.T) {
		t.Parallel()

		input := []byte(`
base: &base
  host: ${HOST}
  script: "${SCRIPT:-literal}"
current:
  <<: *base
items:
  - {}
values:
  first: {}
`)
		var got conformanceConfig
		err := UnmarshalWithOptions(input, &got, UnmarshalOptions{
			Resolver: mapResolver{values: map[string]string{"HOST": "app.local"}},
			Strict:   true,
		})
		if err != nil {
			t.Fatalf("UnmarshalWithOptions returned error: %v", err)
		}

		if got.Base.Host != "app.local" || got.Base.Port != 8080 ||
			got.Current.Host != "app.local" || got.Current.Port != 8080 ||
			got.Base.Script != "${SCRIPT:-literal}" ||
			got.Current.Script != "${SCRIPT:-literal}" {
			t.Fatalf("base/current mismatch: %#v", got)
		}
		if len(got.Items) != 1 || got.Items[0].Host != "localhost" || got.Items[0].Port != 8080 {
			t.Fatalf("slice defaults mismatch: %#v", got.Items)
		}
		if got.Values["first"].Host != "localhost" || got.Values["first"].Port != 8080 {
			t.Fatalf("map defaults mismatch: %#v", got.Values)
		}
	})

	t.Run("all documents strict defaults and resolver", func(t *testing.T) {
		t.Parallel()

		input := []byte("---\nname: ${FIRST}\n---\nname: ${SECOND}\n")
		var got []struct {
			Name string `json:"name"`
			Port int    `json:"port" default:"8080"`
		}
		err := UnmarshalAllWithOptions(input, &got, UnmarshalOptions{
			Resolver: mapResolver{values: map[string]string{
				"FIRST":  "first",
				"SECOND": "second",
			}},
			Strict: true,
		})
		if err != nil {
			t.Fatalf("UnmarshalAllWithOptions returned error: %v", err)
		}
		if len(got) != 2 || got[0].Name != "first" || got[1].Name != "second" ||
			got[0].Port != 8080 || got[1].Port != 8080 {
			t.Fatalf("multi-document mismatch: %#v", got)
		}
	})

	t.Run("embedded strict defaults", func(t *testing.T) {
		t.Parallel()

		type config struct {
			conformanceEmbedded `yaml:",inline"`
			Port                int `json:"port" default:"8080"`
		}

		var got config
		err := UnmarshalWithOptions([]byte("host: ${HOST}\n"), &got, UnmarshalOptions{
			Resolver: mapResolver{values: map[string]string{"HOST": "embedded.local"}},
			Strict:   true,
		})
		if err != nil {
			t.Fatalf("UnmarshalWithOptions returned error: %v", err)
		}
		if got.Host != "embedded.local" || got.Port != 8080 {
			t.Fatalf("embedded mismatch: %#v", got)
		}
	})

	t.Run("custom unmarshaler is opaque to strict and expansion", func(t *testing.T) {
		t.Parallel()

		type config struct {
			Payload conformanceOpaque `json:"payload"`
		}
		input := []byte("payload:\n  value: \"${VALUE:-literal}\"\n  unknown: accepted\n")
		var got config
		err := UnmarshalWithOptions(input, &got, UnmarshalOptions{
			Resolver: mapResolver{values: map[string]string{"VALUE": "expanded"}},
			Strict:   true,
		})
		if err != nil {
			t.Fatalf("UnmarshalWithOptions returned error: %v", err)
		}
		if got.Payload.Value != "${VALUE:-literal}" {
			t.Fatalf("opaque payload was transformed: %#v", got.Payload)
		}
	})
}

func FuzzUnmarshalWithOptionsSchemaTraversal(f *testing.F) {
	for _, seed := range []string{
		"name: ${NAME:-default}\nitems:\n  - {}\n",
		"base: &base\n  host: ${HOST:-localhost}\ncurrent:\n  <<: *base\n",
		"values:\n  first:\n    script: \"${SCRIPT:-literal}\"\n",
		"${KEY}: value\n",
		"{name: value, items: [{host: nested}]}\n",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(_ *testing.T, input string) {
		var got conformanceConfig
		_ = UnmarshalWithOptions([]byte(input), &got, UnmarshalOptions{
			Resolver:          mapResolver{values: map[string]string{"KEY": "name"}},
			IgnoreExpandPaths: []string{"items.*.script", "current.host"},
			Assignment:        AssignmentIgnore,
			Strict:            true,
			ExpandKeys:        true,
		})
	})
}
