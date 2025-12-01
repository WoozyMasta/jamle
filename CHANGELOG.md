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
