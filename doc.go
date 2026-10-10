// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

/*
Package jamle (JSON and YAML with Env) provides YAML/JSON decoding
with Bash-style environment variable expansion.

The decoder uses the repository's JSON-tag-aware YAML codec,
so one struct model with json tags can be used for both YAML and JSON input.

[Unmarshal] and [UnmarshalWithOptions] decode one document.
[UnmarshalAll] and [UnmarshalAllWithOptions] decode YAML document streams into a slice.

Supported expansion forms:

  - `${VAR}` - value of `VAR`, or an empty string if it is unset.
  - `${VAR:-default}` - value of `VAR`, or `default` if it is unset or empty.
  - `${VAR:=default}` - value of `VAR`, or `default` if it is unset or empty;
    the fallback is also assigned through [Setter].
  - `${VAR:?message}` - value of `VAR`, or an error with `message` if it is unset or empty.
  - `$${VAR}` - literal `${VAR}` without expansion.

Nested expressions are evaluated lazily.
Resolver values are expanded recursively, with [UnmarshalOptions.MaxDepth]
and cycle detection preventing partial or unbounded evaluation.

The zero value of [UnmarshalOptions] uses the process environment,
enables assignment and required-variable errors,
leaves mapping keys literal, and uses the default expansion depth.
[UnmarshalOptions.IgnoreExpandPaths] and the `jamle:"noexpand"` tag
keep selected scalar values literal.

`${VAR:=default}` requires a resolver implementing [Setter].
Use [AssignmentIgnore] to disable the assignment side effect
and [RequiredIgnore] to suppress `${VAR:?message}` errors.
For literal expansion syntax in a value, use `$${...}`.
In JSON input, expressions containing `:` must be quoted as JSON strings.
*/
package jamle
