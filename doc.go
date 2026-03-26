// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

/*
Package jamle (JSON and YAML with Env) provides a unified way to unmarshal
YAML/JSON data with Bash-style environment variable expansion.

It uses the local JSON-tag-aware YAML codec from this module
("github.com/woozymasta/jamle/yaml"), so one struct model with json tags
works for both JSON and YAML inputs.

Core APIs:
  - Unmarshal: decode a single document with env-based variable lookup.
  - UnmarshalWithOptions: decode with configurable resolver and behavior.
  - UnmarshalAll: decode all YAML documents from a stream into a slice.
  - UnmarshalAllWithOptions: decode all YAML documents with options.

Supported variable expansion syntax:

  - ${VAR}           Value of VAR, or empty string if unset.
  - ${VAR:-default}  Value of VAR, or "default" if VAR is unset or empty.
  - ${VAR:=default}  Value of VAR, or "default" if unset/empty. Also sets VAR to "default" in the current environment.
  - ${VAR:?error}    Value of VAR, or returns an error with "error" message if VAR is unset or empty.
  - $${VAR}          Escaping. Evaluates to the literal string ${VAR} without expansion.

Example (default behavior with process environment):

	type Config struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}

	data := []byte(`host: ${HOST:-localhost}`)
	var cfg Config
	err := jamle.Unmarshal(data, &cfg)
	if err != nil {
		panic(err)
	}

Example (custom resolver):

	type mapResolver map[string]string

	func (r mapResolver) Lookup(name string) (string, bool) {
		v, ok := r[name]
		return v, ok
	}

	var cfg Config
	err := jamle.UnmarshalWithOptions([]byte(`
	host: ${HOST:-localhost}
	port: ${PORT:-8080}
	`), &cfg, jamle.UnmarshalOptions{
		Resolver: mapResolver{
			"HOST": "svc.local",
			"PORT": "9000",
		},
	})
	if err != nil {
		panic(err)
	}

Example (multi-document YAML stream):

	var all []Config
	err := jamle.UnmarshalAll([]byte(`
	---
	host: a.local
	port: 8080
	---
	host: b.local
	port: 9090
	`), &all)
	if err != nil {
		panic(err)
	}

Behavior notes:
  - Unmarshal decodes one document. Use UnmarshalAll for YAML streams.
  - For JSON inputs, placeholders with ":" operators should be quoted
    as JSON strings.
  - Use UnmarshalOptions.DisableAssignment to make ${VAR:=default}
    behave like ${VAR:-default} without mutating resolver state.
  - Use UnmarshalOptions.DisableRequiredErrors to make ${VAR:?error}
    behave like ${VAR}.
  - ${VAR:=default} requires assignment support from resolver.
    With UnmarshalWithOptions/UnmarshalAllWithOptions, this operator returns
    an error unless the resolver also supports setting values.
*/
package jamle
