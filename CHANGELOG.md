# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog][],
and this project adheres to [Semantic Versioning][].

<!--
## Unreleased

### Added
### Changed
### Removed
-->

## [0.2.0][] - 2026-03-27

### Added

* New internal `yaml` package with JSON-tag-aware decoding, conversion helpers
  (`YAMLToJSON`, `JSONToYAML`), and file I/O helpers (`ReadFile`, `WriteFile`,
  `MarshalWith`, `UnmarshalAuto`) on top of `go.yaml.in/yaml/v3`.
* New options-based API for parsing:
  `UnmarshalWithOptions`, `UnmarshalAllWithOptions`, and `UnmarshalOptions`.
* CLI was reworked and received flags for control and management of input,
  output, formatting, limits, multi-document processing, and env expansion.

### Changed

* Core unmarshal flow was reworked to YAML AST processing without the old
  `invopop/yaml` dependency path.
* Variable expansion engine was rewritten from regex-based replacement to
  parser-style innermost resolution with escaping masks and pass limits.

### Fixed

* `${VAR:wrong}` no longer behaves like a default operator; it now follows
  plain-variable behavior.

[0.2.0]: https://github.com/WoozyMasta/jamle/compare/v0.1.3...v0.2.0

## [0.1.3][] - 2025-12-15

### Fixed

* Environment variable expansion is now applied only to YAML scalar values.
  Comments are no longer processed, preventing accidental expansion or errors
  from `${...}` sequences inside comments.

[0.1.3]: https://github.com/WoozyMasta/jamle/compare/v0.1.2...v0.1.3

## [0.1.2][] - 2025-12-01

### Added

* Variable escaping support. You can now use `$${VAR}` to output a literal
  `${VAR}` string. This is essential when your configuration value needs
  to contain syntax that looks like a variable but shouldn't be processed.

[0.1.2]: https://github.com/WoozyMasta/jamle/compare/v0.1.1...v0.1.2

## [0.1.1][] - 2025-12-01

### Changed

* Refactor Unmarshal loop to replace all regex matches per iteration
  instead of one. This fixes partial expansion in large YAML/JSON files.

[0.1.1]: https://github.com/WoozyMasta/jamle/compare/v0.1.0...v0.1.1

## [0.1.0][] - 2025-12-01

### Added

* First public release

[0.1.0]: https://github.com/WoozyMasta/jamle/tree/v0.1.0

<!--links-->
[Keep a Changelog]: https://keepachangelog.com/en/1.1.0/
[Semantic Versioning]: https://semver.org/spec/v2.0.0.html
